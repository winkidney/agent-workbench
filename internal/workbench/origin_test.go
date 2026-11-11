package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeJSONL(t *testing.T, path string, records ...map[string]any) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0700)
	var b strings.Builder
	for _, r := range records {
		raw, _ := json.Marshal(r)
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
}

// A Claude session launched at the workspace root keeps that directory even
// after it cd's into a subproject.
func TestClaudeLaunchDirectoryIsTheFirstCwd(t *testing.T) {
	posixOnly(t)
	root := realTempDir(t)
	launch := filepath.Join(root, "workspace")
	sub := filepath.Join(launch, "subproject")
	os.MkdirAll(sub, 0700)
	path := filepath.Join(root, "claude", "projects", claudeProjectName(launch), "s1.jsonl")
	writeJSONL(t, path,
		map[string]any{"type": "user", "uuid": "u", "cwd": launch, "message": map[string]any{"content": "plan"}},
		map[string]any{"type": "assistant", "uuid": "a", "parentUuid": "u", "cwd": sub, "message": map[string]any{"content": "done"}},
	)
	s, ok := readClaudeMetadata(path)
	if !ok || s.Cwd != launch {
		t.Fatalf("launch dir %q ok=%v", s.Cwd, ok)
	}
	// Without any recorded cwd the folder name is decoded back to the directory.
	bare := filepath.Join(root, "claude", "projects", claudeProjectName(launch), "s2.jsonl")
	writeJSONL(t, bare, map[string]any{"type": "user", "uuid": "u", "message": map[string]any{"content": "hi"}})
	if s, ok := readClaudeMetadata(bare); !ok || s.Cwd != launch {
		t.Fatalf("decoded %q ok=%v", s.Cwd, ok)
	}
	if decodeClaudeProject("-no-such-place-anywhere") != "" {
		t.Fatal("decoded a missing directory")
	}
}

func TestSessionOrigin(t *testing.T) {
	posixOnly(t)
	home, _ := os.UserHomeDir()
	home = workspaceNamespace(home)
	cases := []struct {
		s    Session
		want string
	}{
		{Session{Agent: "codex", Source: "exec", Workspace: filepath.Join(home, "work", "app")}, "automation"},
		{Session{Agent: "codex", Source: "cli", Workspace: workspaceNamespace(os.TempDir()) + "/x"}, "temporary"},
		{Session{Agent: "claude", Workspace: "/var/tmp/run"}, "temporary"},
		{Session{Agent: "codex", Source: "vscode", Workspace: home}, "chat"},
		{Session{Agent: "codex", Source: "vscode", Workspace: filepath.Join(home, "Documents", "Codex", "2026-10-09", "gao")}, "chat"},
		{Session{Agent: "codex", Source: "cli", Workspace: filepath.Join(home, "work", "app", "sub")}, "project"},
	}
	for _, c := range cases {
		if got := sessionOrigin(c.s); got != c.want {
			t.Fatalf("%+v: %s, want %s", c.s, got, c.want)
		}
	}
}

func TestClaudeSubagentsAreReadOnlyChildren(t *testing.T) {
	root := t.TempDir()
	launch := filepath.Join(root, "workspace")
	os.MkdirAll(launch, 0700)
	project := filepath.Join(root, "claude", "projects", claudeProjectName(launch))
	writeJSONL(t, filepath.Join(project, "parent.jsonl"),
		map[string]any{"type": "user", "uuid": "u", "cwd": launch, "message": map[string]any{"content": "port the library"}})
	agent := filepath.Join(project, "parent", "subagents", "agent-a1.jsonl")
	writeJSONL(t, agent,
		map[string]any{"type": "user", "uuid": "s1", "isSidechain": true, "cwd": launch + "/lib", "message": map[string]any{"content": "port holy"}},
		map[string]any{"type": "assistant", "uuid": "s2", "parentUuid": "s1", "isSidechain": true, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "ported"}}}},
	)
	os.WriteFile(strings.TrimSuffix(agent, ".jsonl")+".meta.json", []byte(`{"description":"Port holy category","agentType":"general-purpose"}`), 0600)
	c := NewNativeCatalog()
	c.CodexHome = filepath.Join(root, "codex")
	c.ClaudeHome = filepath.Join(root, "claude")
	list, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	var child *Session
	for i := range list {
		if list[i].Source == "subagent" {
			child = &list[i]
		}
	}
	if child == nil || child.ParentID != "claude:parent" || child.Title != "Port holy category" || !child.ReadOnly || child.Workspace != workspaceNamespace(launch) {
		t.Fatalf("subagent %+v", child)
	}
	page, err := c.History(*child, "", 50)
	if err != nil || len(page.Events) != 2 {
		t.Fatalf("subagent history %d %v", len(page.Events), err)
	}
	m, _ := fixture(t)
	m.catalog = c
	if err := m.Attach(child.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(child.ID); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("started a subagent: %v", err)
	}
}

func TestWorkspacesSkipTemporaryAndChatRoots(t *testing.T) {
	posixOnly(t)
	root := t.TempDir()
	saved := temporaryRoots
	temporaryRoots = []string{"/tmp/scratch"}
	t.Cleanup(func() { temporaryRoots = saved })
	c := NewNativeCatalog()
	c.CodexHome = filepath.Join(root, "codex")
	c.ClaudeHome = filepath.Join(root, "claude")
	home, _ := os.UserHomeDir()
	work := filepath.Join(root, "work")
	os.MkdirAll(work, 0700)
	for _, dir := range []string{work, "/tmp/scratch", home} {
		project := filepath.Join(c.ClaudeHome, "projects", claudeProjectName(dir))
		writeJSONL(t, filepath.Join(project, claudeProjectName(dir)[1:8]+".jsonl"), map[string]any{"type": "user", "uuid": "u", "cwd": dir, "message": map[string]any{"content": "x"}})
	}
	list, err := c.Workspaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0] != workspaceNamespace(work) {
		t.Fatalf("workspaces %v", list)
	}
}
