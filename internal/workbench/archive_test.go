package workbench

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codexSession is a workbench Codex session with a native thread id, and
// the thread requests it causes.
func codexSession(t *testing.T, m *Manager) (Session, *[]string) {
	t.Helper()
	m.catalog.CodexHome = t.TempDir()
	m.catalog.ClaudeHome = t.TempDir()
	var calls []string
	m.codexThread = func(method string, params map[string]any) error {
		calls = append(calls, method+" "+str(params["threadId"]))
		return nil
	}
	s, err := m.Create("codex", t.TempDir(), "c")
	if err != nil {
		t.Fatal(err)
	}
	m.sessions[s.ID].session.NativeID = "thread-1"
	return s, &calls
}

// Resuming reads only the thread's settings: asking for all turns makes
// Codex 0.167 send a deprecation notice on every resume of a paginated thread.
func TestCodexResumeSkipsTheFullHistory(t *testing.T) {
	m, p := fixture(t)
	s, _ := codexSession(t, m)
	if err := m.Start(s.ID); err != nil {
		t.Fatal(err)
	}
	m.receive(s.ID, m.sessions[s.ID].generation, map[string]any{"id": "init", "result": map[string]any{}})
	resume := obj(sentMethod(p, "thread/resume")["params"])
	if resume["threadId"] != "thread-1" || resume["excludeTurns"] != true {
		t.Fatalf("thread/resume params: %v", resume)
	}
}

func TestArchiveUnarchiveAndDeleteUseCodexThreadRequests(t *testing.T) {
	m, p := fixture(t)
	s, calls := codexSession(t, m)
	if err := m.Start(s.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Archive(s.ID); err != nil {
		t.Fatal(err)
	}
	if !p.stopped {
		t.Fatal("the engine kept the thread loaded")
	}
	if strings.Join(*calls, ",") != "thread/archive thread-1" || !m.sessions[s.ID].session.Archived {
		t.Fatalf("archive: %v %+v", *calls, m.sessions[s.ID].session)
	}
	if err := m.Send(s.ID, "hi"); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("sent to an archived session: %v", err)
	}
	if err := m.Unarchive(s.ID); err != nil || m.sessions[s.ID].session.Archived {
		t.Fatalf("unarchive: %v", err)
	}

	m.UpdatePrefs("pinSession", s.ID)
	m.Rename(s.ID, "")
	m.store.setting("options:"+s.ID, Options{Model: "x"})
	m.store.Append(s.ID, "notice", map[string]any{"text": "x"})
	if err := m.Delete(s.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*calls, ",") != "thread/archive thread-1,thread/unarchive thread-1,thread/delete thread-1" {
		t.Fatalf("calls: %v", *calls)
	}
	if _, ok := m.sessions[s.ID]; ok {
		t.Fatal("runtime kept")
	}
	for _, pinned := range m.Prefs().PinnedSessions {
		if pinned == s.ID {
			t.Fatal("pin kept")
		}
	}
	var o Options
	m.store.readSetting("options:"+s.ID, &o)
	events, _ := m.store.Events(s.ID, 0, 10)
	saved, _ := m.store.List()
	for _, v := range saved {
		if v.ID == s.ID {
			t.Fatal("session row kept")
		}
	}
	// Only the notice that tells open pages it is gone.
	if o.Model != "" || len(events) != 1 || events[0].Kind != "deleted" {
		t.Fatalf("workbench data kept: %+v %v", o, events)
	}
}

func TestThreadOperationsRefuseWhatTheyCannotChange(t *testing.T) {
	m, _ := fixture(t)
	s, calls := codexSession(t, m)
	claude, _ := m.Create("claude", t.TempDir(), "k")
	if err := m.Archive(claude.ID); err == nil || !strings.Contains(err.Error(), "Claude Code") {
		t.Fatalf("claude archive: %v", err)
	}
	if err := m.Delete(claude.ID); err == nil {
		t.Fatal("claude delete")
	}
	m.sessions[s.ID].turn = true
	if err := m.Archive(s.ID); err == nil || !strings.Contains(err.Error(), "turn") {
		t.Fatalf("archive during a turn: %v", err)
	}
	m.sessions[s.ID].turn = false
	// Another client holds the thread (Codex's writer lock).
	locks := filepath.Join(m.catalog.CodexHome, "thread-writer-locks")
	os.MkdirAll(locks, 0o700)
	f, _ := os.Create(filepath.Join(locks, "thread-1.lock"))
	defer f.Close()
	release := holdLock(f)
	defer release()
	if err := m.Delete(s.ID); err == nil || !strings.Contains(err.Error(), "another client") {
		t.Fatalf("delete while held elsewhere: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("requests sent: %v", *calls)
	}
}

func TestCatalogListsArchivedCodexThreads(t *testing.T) {
	codex := t.TempDir()
	db, _ := sql.Open("sqlite", filepath.Join(codex, "state_5.sqlite"))
	cwd := t.TempDir()
	_, err := db.Exec(`CREATE TABLE threads(id TEXT,cwd TEXT,name TEXT,title TEXT,updated_at INTEGER,source TEXT,rollout_path TEXT,archived INTEGER);
INSERT INTO threads VALUES('live',?1,'live','',20,'cli','',0),('old',?1,'old','',10,'cli','',1)`, cwd)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	c := NewNativeCatalog()
	c.CodexHome = codex
	c.ClaudeHome = t.TempDir()
	list, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, s := range list {
		got[s.NativeID] = s.Archived
	}
	if len(got) != 2 || got["live"] || !got["old"] {
		t.Fatalf("archived flags: %v", got)
	}
}

func TestCodexHistoryMarksSteeredUserMessages(t *testing.T) {
	turns := []any{map[string]any{"id": "t1", "items": []any{
		map[string]any{"id": "u1", "type": "userMessage"},
		map[string]any{"id": "a1", "type": "agentMessage"},
		map[string]any{"id": "u2", "type": "userMessage"},
	}}}
	var marks []any
	for _, e := range codexTurnEvents(Session{ID: "codex:x"}, turns) {
		var v map[string]any
		json.Unmarshal(e.Data, &v)
		if it := obj(obj(v["params"])["item"]); it["type"] == "userMessage" {
			marks = append(marks, it["workbenchSteered"])
		}
	}
	if len(marks) != 2 || marks[0] != nil || marks[1] != true {
		t.Fatalf("marks: %v", marks)
	}
}
