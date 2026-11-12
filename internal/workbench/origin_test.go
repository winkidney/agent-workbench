package workbench

import (
	"encoding/json"
	"fmt"
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

// A first prompt with a screenshot is longer than the head that used to be
// read; the cwd and title then came from the tail: a subproject the shell
// had cd'd into, and a tool's output.
func TestClaudeCreatedInSurvivesALargeFirstPrompt(t *testing.T) {
	posixOnly(t)
	root := realTempDir(t)
	launch := filepath.Join(root, "workspace")
	sub := filepath.Join(launch, "subproject")
	os.MkdirAll(sub, 0700)
	path := filepath.Join(root, "claude", "projects", claudeProjectName(launch), "s1.jsonl")
	records := []map[string]any{
		{"type": "permission-mode", "permissionMode": "auto"},
		{"type": "user", "uuid": "u", "cwd": launch, "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": strings.Repeat("A", 300<<10)}},
			map[string]any{"type": "text", "text": "analyse flicker"},
		}}},
	}
	for i := 0; i < 200; i++ {
		records = append(records, map[string]any{"type": "user", "uuid": fmt.Sprint("r", i), "cwd": sub, "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "t", "content": strings.Repeat("277: path := x ", 100)},
		}}})
	}
	writeJSONL(t, path, records...)
	if s, ok := readClaudeMetadata(path); !ok || s.Cwd != launch || s.Title != "analyse flicker" {
		t.Fatalf("cwd %q title %q ok=%v", s.Cwd, s.Title, ok)
	}
	// A rename since, in the tail, still names it.
	writeJSONL(t, path, append(records, map[string]any{"type": "custom-title", "customTitle": "Flicker"})...)
	if s, _ := readClaudeMetadata(path); s.Title != "Flicker" || s.Cwd != launch {
		t.Fatalf("renamed: cwd %q title %q", s.Cwd, s.Title)
	}
}

// A session forked from another project starts with that project's
// records; it was created where its project folder says.
func TestClaudeCreatedInIsItsProjectFolder(t *testing.T) {
	posixOnly(t)
	root := realTempDir(t)
	launch := filepath.Join(root, "workspace")
	other := filepath.Join(root, "elsewhere")
	os.MkdirAll(launch, 0700)
	os.MkdirAll(other, 0700)
	path := filepath.Join(root, "claude", "projects", claudeProjectName(launch), "fork.jsonl")
	writeJSONL(t, path,
		map[string]any{"type": "user", "uuid": "t", "cwd": other, "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "content": "ok"}}}},
		map[string]any{"type": "user", "uuid": "u", "cwd": other, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "carry on"}}}},
	)
	s, ok := readClaudeMetadata(path)
	if !ok || s.Cwd != launch {
		t.Fatalf("cwd %q ok=%v", s.Cwd, ok)
	}
	// A tool result is not a prompt.
	if s.Title != "carry on" {
		t.Fatalf("title %q", s.Title)
	}
}

// Codex keeps the cwd of a thread's latest turn in its index; the thread
// belongs where its record says it was created.
func TestCodexWorkspaceIsWhereTheThreadWasCreated(t *testing.T) {
	posixOnly(t)
	root := realTempDir(t)
	created := filepath.Join(root, "created")
	moved := filepath.Join(root, "moved")
	os.MkdirAll(created, 0700)
	os.MkdirAll(moved, 0700)
	home := filepath.Join(root, "codex")
	record := func(cwd string) []byte {
		var b strings.Builder
		for _, v := range []map[string]any{
			{"type": "session_meta", "payload": map[string]any{"id": "x", "cwd": cwd, "base_instructions": strings.Repeat("i", 100<<10)}},
			{"type": "turn_context", "payload": map[string]any{"cwd": moved}},
		} {
			raw, _ := json.Marshal(v)
			b.Write(raw)
			b.WriteByte('\n')
		}
		return []byte(b.String())
	}
	dir := filepath.Join(home, "sessions", "2026", "11", "01")
	os.MkdirAll(dir, 0700)
	plain := filepath.Join(dir, "rollout-a.jsonl")
	os.WriteFile(plain, record(created), 0600)
	packed := filepath.Join(dir, "rollout-b.jsonl")
	writeZstd(t, packed+".zst", record(created))
	codexIndex(t, home,
		[]any{"a", plain, 1, 30, "cli", moved, "Plain", "", 0, "hi", 1, "paginated"},
		[]any{"b", packed, 1, 20, "cli", moved, "Packed", "", 0, "hi", 1, "paginated"},
		[]any{"c", filepath.Join(dir, "rollout-gone.jsonl"), 1, 10, "cli", moved, "Gone", "", 0, "hi", 1, "legacy"},
	)
	c := NewNativeCatalog()
	c.CodexHome = home
	c.ClaudeHome = filepath.Join(root, "claude")
	list, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"codex:a": created, "codex:b": created, "codex:c": moved}
	for _, s := range list {
		w, ok := want[s.ID]
		if !ok {
			continue
		}
		// It still runs where Codex last worked.
		if s.Workspace != workspaceNamespace(w) || s.Cwd != moved {
			t.Fatalf("%s: workspace %q cwd %q", s.ID, s.Workspace, s.Cwd)
		}
		delete(want, s.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing %v", want)
	}
}
