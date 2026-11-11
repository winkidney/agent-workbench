package workbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivityIndexCountsToolCallsOnly(t *testing.T) {
	posixOnly(t)
	m, _ := fixture(t)
	root := t.TempDir()
	for _, repo := range []string{"api-ctl", "api-server", "slides", "notes"} {
		os.MkdirAll(filepath.Join(root, repo, ".git"), 0700)
	}
	os.MkdirAll(filepath.Join(root, "plain-dir"), 0700)
	line := func(v map[string]any) string { b, _ := json.Marshal(v); return string(b) + "\n" }
	codexCall := func(cmd, workdir string) string {
		args, _ := json.Marshal(map[string]any{"cmd": cmd, "workdir": workdir})
		return line(map[string]any{"type": "response_item", "payload": map[string]any{"type": "function_call", "name": "exec_command", "arguments": string(args)}})
	}
	var b strings.Builder
	b.WriteString(line(map[string]any{"type": "session_meta", "payload": map[string]any{"cwd": root}}))
	for i := 0; i < 12; i++ {
		b.WriteString(codexCall("git status", filepath.Join(root, "api-ctl")))
	}
	for i := 0; i < 6; i++ {
		b.WriteString(codexCall("cd api-server && make test", root))
	}
	b.WriteString(codexCall("ls slides/", root))    // below threshold
	b.WriteString(codexCall("ls plain-dir/", root)) // not a project
	// Output text listing every project must not count.
	for i := 0; i < 20; i++ {
		b.WriteString(line(map[string]any{"type": "response_item", "payload": map[string]any{"type": "function_call_output", "output": "notes/ slides/ " + root + "/notes"}}))
	}
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	os.WriteFile(path, []byte(b.String()), 0600)
	s := Session{ID: "codex:t", Agent: "codex", Workspace: root, Origin: "project"}
	m.activity.Scan([]Session{s}, map[string]string{s.ID: path}, 1<<30)
	if got := strings.Join(m.activity.Related(s.ID), ","); got != "api-ctl,api-server" {
		t.Fatalf("related %q", got)
	}
	// Incremental: appended Claude-style tool_use records are counted from the stored offset.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	for i := 0; i < 30; i++ {
		fmt.Fprint(f, line(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": "Edit", "input": map[string]any{"file_path": root + "/slides/deck.md"}}}}}))
	}
	f.Close()
	m.activity.Scan([]Session{s}, map[string]string{s.ID: path}, 1<<30)
	if got := m.activity.Related(s.ID); len(got) == 0 || got[0] != "slides" {
		t.Fatalf("incremental %v", got)
	}
	var offset int64
	m.store.db.QueryRow(`SELECT offset FROM session_activity WHERE id=?`, s.ID).Scan(&offset)
	if info, _ := os.Stat(path); offset != info.Size() {
		t.Fatalf("offset %d of %d", offset, info.Size())
	}
	// A new index instance restores results from the database.
	if got := NewActivityIndex(m.store).Related(s.ID); len(got) == 0 {
		t.Fatal("index not persisted")
	}
	// Non-project and child sessions are skipped.
	other := Session{ID: "codex:x", Agent: "codex", Workspace: root, Origin: "automation"}
	m.activity.Scan([]Session{other}, map[string]string{other.ID: path}, 1<<30)
	if len(m.activity.Related(other.ID)) != 0 {
		t.Fatal("indexed an automation session")
	}
}

func TestRelatedProjectsThreshold(t *testing.T) {
	got := relatedProjects(map[string]int{"a": 232, "b": 133, "c": 8, "d": 3, "e": 40})
	if strings.Join(got, ",") != "a,b,e" {
		t.Fatalf("%v", got)
	}
	if len(relatedProjects(map[string]int{"a": 4})) != 0 {
		t.Fatal("below minimum kept")
	}
}
