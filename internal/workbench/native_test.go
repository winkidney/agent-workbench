package workbench

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeCatalogAndHistory(t *testing.T) {
	posixOnly(t)
	root := t.TempDir()
	codex := filepath.Join(root, "codex")
	claude := filepath.Join(root, "claude")
	os.MkdirAll(codex, 0700)
	os.MkdirAll(filepath.Join(claude, "projects", "encoded-name"), 0700)
	db, err := sql.Open("sqlite", filepath.Join(codex, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE threads(id TEXT,cwd TEXT,name TEXT,title TEXT,updated_at INTEGER,source TEXT,rollout_path TEXT,archived INTEGER);INSERT INTO threads VALUES('one','/tmp/work','Native','',10,'cli','',0),('hidden','/tmp','', '',20,'cli','',1);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	file := filepath.Join(claude, "projects", "encoded-name", "two.jsonl")
	os.WriteFile(file, []byte("{\"type\":\"user\",\"uuid\":\"u\",\"sessionId\":\"two\",\"cwd\":\"/tmp/work\",\"message\":{\"content\":\"hello\"}}\n{\"type\":\"assistant\",\"uuid\":\"a\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"answer\"}]}}\n{partial"), 0600)
	c := NewNativeCatalog()
	c.CodexHome = codex
	c.ClaudeHome = claude
	list, err := c.List()
	// The archived thread is listed, marked so the sidebar can keep it apart.
	if err != nil || len(list) != 3 {
		t.Fatal(list, err)
	}
	if archived, _ := c.Find("codex:hidden"); !archived.Archived {
		t.Fatal("archived thread not marked")
	}
	s, ok := c.Find("claude:two")
	if !ok || s.Cwd != "/tmp/work" {
		t.Fatal("native identity/cwd")
	}
	page, err := c.History(s, "", 200)
	if err != nil || len(page.Events) != 2 || page.HasMore {
		t.Fatal(page, err)
	}
	if c.Busy(s) {
		t.Fatal("false active")
	}
	os.MkdirAll(filepath.Join(codex, "thread-writer-locks"), 0700)
	f, _ := os.Create(filepath.Join(codex, "thread-writer-locks", "one.lock"))
	defer f.Close()
	release := holdLock(f)
	native, _ := c.Find("codex:one")
	if !c.Busy(native) {
		t.Fatal("writer lock ignored")
	}
	release()
	if c.Busy(native) {
		t.Fatal("stale lock treated as owner")
	}
}
func TestNewSessionRequiresFirstMessage(t *testing.T) {
	posixOnly(t)
	m, _ := fixture(t)
	server := pairedServer(m, nil, "agent.localhost:17880", "pair")
	login := request(t, server, server.Host, "POST", "/api/login", `{"code":"pair"}`, nil)
	w := request(t, server, server.Host, "POST", "/api/sessions", `{"agent":"claude","cwd":"/tmp"}`, login.Result().Cookies()[0])
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestClaudeBranchSelection(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.jsonl")
	os.WriteFile(p, []byte("{\"type\":\"user\",\"uuid\":\"u\",\"message\":{\"content\":\"question\"}}\n{\"type\":\"assistant\",\"uuid\":\"abandoned\",\"parentUuid\":\"u\",\"message\":{\"content\":\"old\"}}\n{\"type\":\"assistant\",\"uuid\":\"kept\",\"parentUuid\":\"u\",\"message\":{\"content\":\"new\"}}\n"), 0600)
	page, err := readClaudeHistory(p, Session{ID: "claude:x"}, "", 200)
	if err != nil || len(page.Events) != 2 {
		t.Fatal(page, err)
	}
	events := page.Events
	if !strings.Contains(string(events[1].Data), "kept") || strings.Contains(string(events[1].Data), "abandoned") {
		t.Fatal("wrong native branch")
	}
}

func TestNativeCreationRetryUsesSavedIdentity(t *testing.T) {
	posixOnly(t)
	m, _ := fixture(t)
	server := pairedServer(m, nil, "agent.localhost:17880", "pair")
	login := request(t, server, server.Host, "POST", "/api/login", `{"code":"pair"}`, nil)
	input := struct{ Agent, Cwd, Title, InitialMessage, RequestID string }{"claude", "/tmp", "test", "hello", "retry-key"}
	raw, _ := json.Marshal(input)
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	_, err := m.store.db.Exec("INSERT INTO native_creations VALUES(?,?,?)", input.RequestID, hash, `{"id":"claude:original","nativeId":"original"}`)
	if err != nil {
		t.Fatal(err)
	}
	w := request(t, server, server.Host, "POST", "/api/sessions", `{"agent":"claude","cwd":"/tmp","title":"test","initialMessage":"hello","requestID":"retry-key"}`, login.Result().Cookies()[0])
	if w.Code != 200 || !strings.Contains(w.Body.String(), "claude:original") {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(m.sessions) != 0 {
		t.Fatal("retry created another session")
	}
}

func TestNativeNamespaceMergesAgentsAndPathAliases(t *testing.T) {
	posixOnly(t)
	root := realTempDir(t)
	actual := filepath.Join(root, "workspace")
	alias := filepath.Join(root, "alias")
	os.MkdirAll(actual, 0700)
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	codex := filepath.Join(root, "codex")
	claude := filepath.Join(root, "claude")
	os.MkdirAll(codex, 0700)
	os.MkdirAll(filepath.Join(claude, "projects", "encoded"), 0700)
	db, _ := sql.Open("sqlite", filepath.Join(codex, "state_5.sqlite"))
	_, err := db.Exec(`CREATE TABLE threads(id TEXT,cwd TEXT,name TEXT,title TEXT,updated_at INTEGER,source TEXT,rollout_path TEXT,archived INTEGER);INSERT INTO threads VALUES('one',?,'one','',10,'cli','',0)`, actual+string(os.PathSeparator))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	data, _ := json.Marshal(map[string]any{"type": "user", "uuid": "u", "cwd": alias, "message": map[string]any{"content": "hello"}})
	os.WriteFile(filepath.Join(claude, "projects", "encoded", "two.jsonl"), append(data, '\n'), 0600)
	c := NewNativeCatalog()
	c.CodexHome = codex
	c.ClaudeHome = claude
	sessions, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	namespaces := map[string]bool{}
	for _, session := range sessions {
		raw, _ := json.Marshal(session)
		var v map[string]any
		json.Unmarshal(raw, &v)
		key, _ := v["workspace"].(string)
		if key == "" {
			key = session.Cwd
		}
		namespaces[key] = true
		if session.Agent == "claude" && session.Cwd != alias {
			t.Fatal("launch cwd was rewritten")
		}
	}
	if len(sessions) != 2 || len(namespaces) != 1 || !namespaces[actual] {
		t.Fatalf("same directory split into %d namespaces", len(namespaces))
	}
}

func TestWorkspaceNamespaceKeepsDistinctDirectoriesAndMissingLeaves(t *testing.T) {
	posixOnly(t)
	root := realTempDir(t)
	real := filepath.Join(root, "real")
	alias := filepath.Join(root, "alias")
	os.MkdirAll(real, 0700)
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	if workspaceNamespace(filepath.Join(alias, "deleted")) != filepath.Join(real, "deleted") {
		t.Fatal("missing leaf lost namespace alias")
	}
	a := filepath.Join(root, "a", "project")
	b := filepath.Join(root, "b", "project")
	if workspaceNamespace(a) == workspaceNamespace(b) {
		t.Fatal("same basename merged different directories")
	}
}
