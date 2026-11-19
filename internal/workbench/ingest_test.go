package workbench

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func native(v map[string]any) Event {
	b, _ := json.Marshal(v)
	return Event{Kind: "native", Data: b}
}

func TestExportKeepsTheConversation(t *testing.T) {
	events := []Event{
		// Claude: a typed message, a reply with a tool call, tool results
		// (not a turn), a subagent's line and a compact summary (left out).
		native(map[string]any{"type": "user", "message": map[string]any{"content": "Plan my week"}}),
		native(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{
			map[string]any{"type": "thinking", "thinking": "hmm"},
			map[string]any{"type": "tool_use", "name": "Read", "input": map[string]any{"file_path": "state/loops.md"}},
			map[string]any{"type": "text", "text": "Three things are due."},
		}}}),
		native(map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "content": "secret file"}}}}),
		native(map[string]any{"type": "assistant", "parent_tool_use_id": "t1", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "subagent"}}}}),
		native(map[string]any{"type": "user", "isCompactSummary": true, "message": map[string]any{"content": "summary"}}),
		// Codex: a message with an image, a command, a reply, a child's item.
		native(map[string]any{"method": "item/completed", "params": map[string]any{"item": map[string]any{"type": "userMessage", "content": []any{map[string]any{"type": "text", "text": "And this?"}, map[string]any{"type": "image"}}}}}),
		native(map[string]any{"method": "item/completed", "params": map[string]any{"item": map[string]any{"type": "commandExecution", "command": "ls   -la"}}}),
		native(map[string]any{"method": "item/completed", "workbenchChild": "c", "params": map[string]any{"item": map[string]any{"type": "agentMessage", "text": "child"}}}),
		native(map[string]any{"method": "item/completed", "params": map[string]any{"item": map[string]any{"type": "agentMessage", "text": "A photo of a cat."}}}),
	}
	turns := []exportTurn{}
	for _, e := range events {
		turns = addExported(turns, e)
	}
	if len(turns) != 2 || turns[0].User != "Plan my week" || turns[1].User != "And this?\n[image]" {
		t.Fatalf("turns: %+v", turns)
	}
	want := []string{"- `Read` {\"file_path\":\"state/loops.md\"}", "Three things are due."}
	if strings.Join(turns[0].Reply, "|") != strings.Join(want, "|") {
		t.Fatalf("reply: %q", turns[0].Reply)
	}
	if strings.Join(turns[1].Reply, "|") != "- `shell` ls -la|A photo of a cat." {
		t.Fatalf("codex reply: %q", turns[1].Reply)
	}
	md := exportMarkdown(Session{ID: "claude:x", Agent: "claude", Title: "Week"}, turns, 1, time.Date(2026, 11, 19, 9, 0, 0, 0, time.Local))
	if strings.Contains(md, "Plan my week") || !strings.Contains(md, "## 2. User") || !strings.Contains(md, "turns 2–2") {
		t.Fatalf("from the second turn:\n%s", md)
	}
}

func TestRedactHidesSecrets(t *testing.T) {
	for in, want := range map[string]string{
		"key sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123 here":                        "key [redacted] here",
		"ghp_abcdefghijklmnopqrstuvwxyz0123456789":                                    "[redacted]",
		"AKIAABCDEFGHIJKLMNOP":                                                        "[redacted]",
		"password: hunter2222":                                                        "password: [redacted]",
		`"api_key": "abc123def456"`:                                                   `"api_key": "[redacted]"`,
		"MAIL_PASSWORD=s3cretvalue":                                                   "MAIL_PASSWORD=[redacted]",
		"https://me:pa55word@example.com/x":                                           "https://[redacted]@example.com/x",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz":                            "Authorization: [redacted]",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----": "[redacted]",
		// Ordinary text stays.
		"the token budget is fine": "the token budget is fine",
		"password reset tomorrow":  "password reset tomorrow",
	} {
		if got := redact(in); got != want {
			t.Errorf("redact(%q) = %q, want %q", in, got, want)
		}
	}
}

// assistantsWithGit sets up a repository of assistants (main keeps the
// wiki) and a Claude home whose transcript is a conversation with main.
func assistantsWithGit(t *testing.T) (*Manager, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	posixOnly(t)
	m, _ := fixture(t)
	root := filepath.Join(t.TempDir(), "assistants")
	if err := m.SetUpAssistants(root, "claude"); err != nil {
		t.Fatal(err)
	}
	return m, root
}

func writeTranscript(t *testing.T, m *Manager, cwd, id string, turns ...string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "claude", "projects", strings.NewReplacer("/", "-", ".", "-").Replace(cwd))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(home, "codex"), 0o700)
	var b strings.Builder
	for i, text := range turns {
		for _, rec := range []map[string]any{
			{"type": "user", "uuid": "u" + string(rune('a'+i)), "sessionId": id, "cwd": cwd, "timestamp": "2026-11-19T09:00:00Z", "message": map[string]any{"role": "user", "content": text}},
			{"type": "assistant", "uuid": "a" + string(rune('a'+i)), "sessionId": id, "cwd": cwd, "timestamp": "2026-11-19T09:00:01Z", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Noted."}}}},
		} {
			line, _ := json.Marshal(rec)
			b.Write(line)
			b.WriteByte('\n')
		}
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	m.catalog.CodexHome, m.catalog.ClaudeHome = filepath.Join(home, "codex"), filepath.Join(home, "claude")
}

func gitLog(t *testing.T, root string) string {
	out, err := gitRun(root, "log", "--format=%s")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestIngestRecordsCommitsAndUndoes(t *testing.T) {
	m, root := assistantsWithGit(t)
	main := filepath.Join(root, "main")
	writeTranscript(t, m, main, "s1", "I prefer tea. My key is sk-abcdefghijklmnopqrstuvwxyz", "Meet Ana on Friday")
	s := Session{ID: "claude:s1", NativeID: "s1", Agent: "claude", Cwd: main, Title: "Tea and Ana", Status: "sleeping"}
	m.as.lookup = func(id string) (Session, bool) { return s, id == s.ID }
	var prompts []string
	m.as.background = func(a Assistant, prompt string) (string, error) {
		prompts = append(prompts, prompt)
		writeToml(t, filepath.Join(root, "shared", "wiki", "preferences.md"), "# Preferences\n\n- Tea\n")
		return "Done.\nSummary: added preferences.md", nil
	}

	m.maybeIngest(s.ID)
	list, _ := m.AssistantChanges()
	if len(list) != 1 || list[0].Status != "done" || list[0].Summary != "added preferences.md" || list[0].Kind != "ingest" || list[0].To != 2 {
		t.Fatalf("change: %+v", list)
	}
	c := list[0]
	if len(c.Files) != 1 || c.Files[0] != "shared/wiki/preferences.md" {
		t.Fatalf("files: %v", c.Files)
	}
	record, err := os.ReadFile(filepath.Join(main, filepath.FromSlash(c.Source)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), "Meet Ana on Friday") || strings.Contains(string(record), "sk-abc") {
		t.Fatalf("record:\n%s", record)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], c.Source) || !strings.HasPrefix(prompts[0], "You are running in the background") {
		t.Fatalf("prompt: %q", prompts)
	}
	// The record is committed apart from what the run made of it.
	if log := gitLog(t, root); !strings.HasPrefix(log, "ingest: Tea and Ana\nchore: keep changes made before Tea and Ana\n") {
		t.Fatalf("log:\n%s", log)
	}

	// Nothing new: nothing happens.
	m.maybeIngest(s.ID)
	if list, _ := m.AssistantChanges(); len(list) != 1 {
		t.Fatalf("ingested again: %+v", list)
	}

	// Undo takes the wiki change back and keeps the record.
	if _, err := m.UndoChange(c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "shared", "wiki", "preferences.md")); !os.IsNotExist(err) {
		t.Fatal("undo left the page")
	}
	if _, err := os.Stat(filepath.Join(main, filepath.FromSlash(c.Source))); err != nil {
		t.Fatal("undo took the record")
	}
	if v, _ := m.store.change(c.ID); v.Status != "undone" || v.Reverted == "" {
		t.Fatalf("after undo: %+v", v)
	}
	if _, err := m.UndoChange(c.ID); err == nil {
		t.Fatal("undid twice")
	}

	// A new turn: only it is recorded; a failed run keeps its place, and a
	// retry from the same record moves on.
	writeTranscript(t, m, main, "s1", "I prefer tea. My key is sk-abcdefghijklmnopqrstuvwxyz", "Meet Ana on Friday", "Book the train")
	m.catalog.updated = time.Time{}
	m.as.background = func(Assistant, string) (string, error) { return "", errors.New("claude: exit status 1") }
	m.maybeIngest(s.ID)
	list, _ = m.AssistantChanges()
	failed := list[0]
	if failed.Status != "failed" || failed.From != 2 || failed.To != 3 {
		t.Fatalf("failed: %+v", failed)
	}
	var done ingestProgress
	m.store.readSetting(ingestKey(s.ID), &done)
	if done.Turns != 2 {
		t.Fatalf("progress moved on a failure: %d", done.Turns)
	}
	record, _ = os.ReadFile(filepath.Join(main, filepath.FromSlash(failed.Source)))
	if strings.Contains(string(record), "Meet Ana") || !strings.Contains(string(record), "Book the train") {
		t.Fatalf("second record:\n%s", record)
	}
	m.as.background = func(Assistant, string) (string, error) { return "Summary: nothing lasting", nil }
	retry, err := m.RetryChange(failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Its progress is kept just after the change is recorded.
	waitUntil(t, "the retry", func() bool {
		var now ingestProgress
		m.store.readSetting(ingestKey(s.ID), &now)
		return now.Turns == 3
	})
	if v, _ := m.store.change(retry.ID); v.Status != "done" {
		t.Fatalf("retry: %+v", v)
	}
}

func TestIngestOnlyForTheWiki(t *testing.T) {
	m, root := assistantsWithGit(t)
	a, err := m.NewAssistant("Report", "", "claude", "per-task")
	if err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, m, a.Path, "s2", "Draft it")
	s := Session{ID: "claude:s2", NativeID: "s2", Agent: "claude", Cwd: a.Path, Title: "Draft", Status: "sleeping"}
	m.as.lookup = func(id string) (Session, bool) { return s, id == s.ID }
	m.as.background = func(Assistant, string) (string, error) { t.Fatal("ran"); return "", nil }
	m.sessionSlept(s.ID, s.Cwd)
	// Outside the repository: not even looked up.
	m.sessionSlept("other", filepath.Dir(root))
	if list, _ := m.AssistantChanges(); len(list) != 0 {
		t.Fatalf("changes: %+v", list)
	}
}

func TestBackgroundRoutineCommitsItsChanges(t *testing.T) {
	m, root := assistantsWithGit(t)
	writeToml(t, filepath.Join(root, "mail", "assistant.toml"), `name = "Mail"
agent = "codex"
[[routines]]
name = "Sort mail"
every = "30m"
command = "echo 'From: a@example.com Subject: hi'"
message = "Sort these into state/inbox.md."
background = true
`)
	var got string
	m.as.background = func(a Assistant, prompt string) (string, error) {
		got = prompt
		writeToml(t, filepath.Join(a.Path, "state", "inbox.md"), "- hi from a\n")
		return "Summary: one new mail", nil
	}
	list := m.Assistants()
	var mail Assistant
	for _, a := range list {
		if a.Dir == "mail" {
			mail = a
		}
	}
	if mail.Error != "" || mail.Routines != 1 {
		t.Fatalf("mail: %+v", mail)
	}
	run, err := m.RunRoutine(fileRoutineID("mail", "Sort mail"))
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the run", func() bool { v, _ := m.store.run(run.ID); return v.Status == "done" || v.Status == "failed" })
	v, _ := m.store.run(run.ID)
	if v.Status != "done" || v.Output != "one new mail" || !v.Notify || v.Session != "" {
		t.Fatalf("run: %+v", v)
	}
	if !strings.Contains(got, "[Routine: Sort mail]") || !strings.Contains(got, "Subject: hi") {
		t.Fatalf("prompt: %q", got)
	}
	changes, _ := m.AssistantChanges()
	if len(changes) != 1 || changes[0].Kind != "routine" || changes[0].Run != run.ID || len(changes[0].Files) != 1 || changes[0].Files[0] != "mail/state/inbox.md" {
		t.Fatalf("changes: %+v", changes)
	}
	if log := gitLog(t, root); !strings.HasPrefix(log, "routine: Sort mail\n") {
		t.Fatalf("log:\n%s", log)
	}
	// background without a message is refused.
	p := filepath.Join(t.TempDir(), "a.toml")
	writeToml(t, p, "name = \"x\"\nagent = \"codex\"\n[[routines]]\nname = \"r\"\nevery = \"1h\"\ncommand = \"true\"\nbackground = true")
	if _, err := readAssistant(p); err == nil {
		t.Fatal("background without a message was accepted")
	}
}

func TestTopicFollowsTheSessionsNativeID(t *testing.T) {
	m, _ := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root})
	writeToml(t, filepath.Join(root, "main", "assistant.toml"), "name = \"A\"\nagent = \"claude\"\nsessions = \"continuous\"")
	m.saveTopic("main", assistantState{Current: "tok", Previous: []string{"old", "tok"}})
	m.topicMoved("tok", "claude:n")
	if st := m.topicState("main"); st.Current != "claude:n" || st.Previous[1] != "claude:n" || st.Previous[0] != "old" {
		t.Fatalf("state: %+v", st)
	}
}
