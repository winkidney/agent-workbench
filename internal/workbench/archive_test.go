package workbench

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
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

// A folder's sessions: Codex threads and a Claude session in a project,
// its subfolder, a sibling with the same prefix, and the assistants under it.
func folderFixture(t *testing.T) (*Manager, string, *[]string) {
	t.Helper()
	posixOnly(t)
	saved := temporaryRoots
	temporaryRoots = []string{"/nonexistent-temporary-root"}
	t.Cleanup(func() { temporaryRoots = saved })
	m, _ := fixture(t)
	root := realTempDir(t)
	app := filepath.Join(root, "app")
	for _, d := range []string{"app/sub", "app2", "app/assistants/main"} {
		os.MkdirAll(filepath.Join(root, d), 0o700)
	}
	_ = m.store.setting("assistants", assistantsSetting{Root: filepath.Join(app, "assistants")})
	codex := filepath.Join(root, "codex")
	claude := filepath.Join(root, "claude")
	os.MkdirAll(codex, 0o700)
	os.MkdirAll(filepath.Join(claude, "projects", "app"), 0o700)
	db, _ := sql.Open("sqlite", filepath.Join(codex, "state_5.sqlite"))
	_, err := db.Exec(`CREATE TABLE threads(id TEXT,cwd TEXT,name TEXT,title TEXT,updated_at INTEGER,source TEXT,rollout_path TEXT,archived INTEGER);
INSERT INTO threads VALUES('one',?1,'one','',10,'cli','',0),('two',?2,'two','',10,'cli','',0),('old',?1,'old','',10,'cli','',1),
('busy',?1,'busy','',10,'cli','',0),('next',?3,'next','',10,'cli','',0),('moss',?4,'moss','',10,'cli','',0)`,
		app, filepath.Join(app, "sub"), filepath.Join(root, "app2"), filepath.Join(app, "assistants", "main"))
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"type": "user", "uuid": "u", "cwd": app, "message": map[string]any{"content": "hello"}})
	os.WriteFile(filepath.Join(claude, "projects", "app", "k.jsonl"), append(data, '\n'), 0o600)
	m.catalog.CodexHome, m.catalog.ClaudeHome = codex, claude
	m.sessions["codex:busy"] = &runtime{session: Session{ID: "codex:busy", Agent: "codex", NativeID: "busy"}, turn: true, pending: map[string]any{}, previews: map[string]*Preview{}}
	var calls []string
	m.codexThreads = func(method string, threads []string) map[string]error {
		failed := map[string]error{}
		for _, th := range threads {
			calls = append(calls, method+" "+th)
			if method == "thread/delete" && th == "two" {
				failed[th] = errors.New("refused")
			}
		}
		return failed
	}
	return m, app, &calls
}

func TestFolderArchiveTakesTheFolderAndItsSubfolders(t *testing.T) {
	m, app, calls := folderFixture(t)
	m.UpdatePrefs("pinSession", "claude:k")
	plan, err := m.FolderSessions(app+"/", "archive", true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Codex != 2 || plan.Claude != 1 || len(plan.Skipped) != 1 || plan.Skipped[0].ID != "codex:busy" || len(*calls) != 0 {
		t.Fatalf("dry run: %+v %v", plan, *calls)
	}
	done, err := m.FolderSessions(app, "archive", false)
	if err != nil {
		t.Fatal(err)
	}
	if done.Codex != 2 || done.Claude != 1 || strings.Join(*calls, ",") != "thread/archive one,thread/archive two" {
		t.Fatalf("archive: %+v %v", done, *calls)
	}
	p := m.Prefs()
	if len(p.Hidden) != 1 || p.Hidden[0] != "claude:k" || len(p.PinnedSessions) != 0 {
		t.Fatalf("prefs: %+v", p)
	}
	if _, err := m.FolderSessions(app, "rename", true); err == nil {
		t.Fatal("an unknown op was accepted")
	}
	if _, err := m.FolderSessions("app", "archive", true); err == nil {
		t.Fatal("a relative folder was accepted")
	}
}

func TestFolderDeleteReportsWhatFailed(t *testing.T) {
	m, app, calls := folderFixture(t)
	m.Rename("codex:one", "")
	done, err := m.FolderSessions(app, "delete", false)
	if err != nil {
		t.Fatal(err)
	}
	// The archived thread is deleted too; one refused stays.
	sort.Strings(*calls)
	if strings.Join(*calls, ",") != "thread/delete old,thread/delete one,thread/delete two" {
		t.Fatalf("calls: %v", *calls)
	}
	if done.Codex != 2 || len(done.Failed) != 1 || done.Failed[0].ID != "codex:two" || done.Failed[0].Reason != "refused" {
		t.Fatalf("delete: %+v", done)
	}
}
