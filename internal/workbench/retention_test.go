package workbench

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestIdleRuntimesAndCachesAreDropped(t *testing.T) {
	m, _ := fixture(t)
	old := time.Now().Add(-runtimeIdle - time.Minute)
	keep := func(id string, r *runtime) {
		r.session.ID, r.session.Agent, r.last = id, "claude", old
		m.sessions[id] = r
	}
	m.mu.Lock()
	keep("asleep", &runtime{session: Session{Status: "sleeping"}})
	keep("recent", &runtime{session: Session{Status: "sleeping"}})
	m.sessions["recent"].last = time.Now()
	keep("engine", &runtime{session: Session{Status: "idle"}, proc: &fakeProcess{}})
	keep("queued", &runtime{session: Session{Status: "sleeping"}, queue: []queued{{Text: "later"}}})
	keep("asking", &runtime{session: Session{Status: "sleeping", Attention: "question"}})
	keep("service", &runtime{session: Session{Status: "sleeping"}, previews: map[string]*Preview{"p": {}}})
	m.mu.Unlock()
	m.metadata = map[string]metadata{"fresh": {at: time.Now()}, "stale": {at: time.Now().Add(-metadataKeep - time.Second)}}
	m.external = map[string]*externalActivity{"seen": {used: time.Now()}, "gone": {used: old}}

	m.evictIdle()
	m.mu.Lock()
	var left []string
	for id := range m.sessions {
		left = append(left, id)
	}
	m.mu.Unlock()
	if len(left) != 5 || m.sessions["asleep"] != nil {
		t.Fatalf("runtimes left %v", left)
	}
	if _, ok := m.metadata["stale"]; ok || len(m.metadata) != 1 {
		t.Fatalf("metadata %v", m.metadata)
	}
	if _, ok := m.external["gone"]; ok || len(m.external) != 1 {
		t.Fatalf("external %v", m.external)
	}
}

func TestStoppedEnginesLetGoOfWhatOnlyTheyNeeded(t *testing.T) {
	m, p := fixture(t)
	s, _ := m.Create("codex", t.TempDir(), "heavy")
	if err := m.Start(s.ID); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	r := m.find(s.ID)
	r.turnRequest = map[string]any{"text": string(make([]byte, 1<<20))}
	r.taskOutputs = map[string]string{"t": "/tmp/out"}
	r.samples = []sample{{}}
	r.ready = true
	m.mu.Unlock()
	_ = p
	if err := m.Stop(s.ID, true); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.turnRequest != nil || r.taskOutputs != nil || r.samples != nil {
		t.Fatal("a sleeping session kept its engine's leftovers")
	}
}

func TestReconcileDropsWhatSessionsGoneLeftBehind(t *testing.T) {
	posixOnly(t)
	m, _ := fixture(t)
	home := t.TempDir()
	codexIndex(t, home, []any{"here", nil, 1, 30, "cli", "/work", "Here", "", 0, "", 0, "paginated"})
	m.catalog.CodexHome, m.catalog.ClaudeHome = home, t.TempDir()
	for _, id := range []string{"codex:here", "codex:gone", "token-live"} {
		m.store.setting("options:"+id, Options{Model: "m"})
		m.store.setting("telemetry:"+id, Telemetry{Scope: "s"})
		m.store.cacheSet("history_usage:"+id, map[string]any{"Stamp": "x"})
		m.store.setPreviewTabs(id, PreviewTabs{Tabs: []PreviewTab{{Kind: "file", Path: "a.html"}}})
	}
	m.mu.Lock()
	m.sessions["token-live"] = &runtime{session: Session{ID: "token-live", Agent: "claude"}}
	m.mu.Unlock()
	p := m.Prefs()
	p.Aliases["codex:here"], p.Aliases["codex:gone"] = "Mine", "Old"
	m.savePrefs(p)
	// Gone longer than the retention: last seen eight days ago.
	m.store.db.Exec(`INSERT INTO session_seen(id,seen) VALUES('codex:gone',?)`, time.Now().Add(-8*24*time.Hour).UTC().Format(time.RFC3339Nano))

	if err := m.Reconcile(); err != nil {
		t.Fatal(err)
	}
	count := func(q string, args ...any) int {
		var n int
		m.store.db.QueryRow(q, args...).Scan(&n)
		return n
	}
	for _, id := range []string{"codex:here", "token-live"} {
		if count(`SELECT count(*) FROM settings WHERE key IN ('options:'||?1,'telemetry:'||?1)`, id) != 2 || count(`SELECT count(*) FROM cache WHERE key='history_usage:'||?`, id) != 1 || count(`SELECT count(*) FROM preview_tabs WHERE session=?`, id) != 1 {
			t.Fatalf("%s lost its rows", id)
		}
	}
	if count(`SELECT count(*) FROM settings WHERE key LIKE '%codex:gone'`) != 0 || count(`SELECT count(*) FROM cache WHERE key LIKE '%codex:gone'`) != 0 || count(`SELECT count(*) FROM preview_tabs WHERE session='codex:gone'`) != 0 {
		t.Fatal("a session gone for the retention kept its rows")
	}
	if a := m.Prefs().Aliases; a["codex:here"] != "Mine" || a["codex:gone"] != "" {
		t.Fatalf("aliases %v", a)
	}
	// An empty list (a home missing) changes nothing.
	m.catalog.CodexHome = t.TempDir()
	m.catalog.updated = time.Time{}
	m.mu.Lock()
	delete(m.sessions, "token-live")
	m.mu.Unlock()
	before := count(`SELECT count(*) FROM settings`)
	if err := m.Reconcile(); err != nil || count(`SELECT count(*) FROM settings`) != before {
		t.Fatalf("an empty list deleted rows: %v", err)
	}
}

func TestCachesExpireAndOldStoresMigrate(t *testing.T) {
	dir := t.TempDir()
	// A store from an older build: three columns, caches among settings.
	db, err := sql.Open("sqlite", filepath.Join(dir, "workbench.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);
		CREATE TABLE native_creations(request_id TEXT PRIMARY KEY,payload_hash TEXT,response TEXT);
		INSERT INTO native_creations VALUES('old','h','{}');
		INSERT INTO settings VALUES('history_usage:x','{}'),('claude-init:/w','{}'),('goal:x','null'),('goal:y','{"objective":"o"}'),('options:x','{}');`)
	db.Close()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var keys []string
	rows, _ := s.db.Query(`SELECT key FROM settings ORDER BY key`)
	for rows.Next() {
		var k string
		rows.Scan(&k)
		keys = append(keys, k)
	}
	rows.Close()
	if b, _ := json.Marshal(keys); string(b) != `["goal:y","options:x"]` {
		t.Fatalf("settings after migration %s", b)
	}
	// Caches and creation records expire.
	s.cacheSet("claude-init:/w", claudeInitInfo{Skills: []string{"a"}})
	var info claudeInitInfo
	if !s.cacheGet("claude-init:/w", &info) || len(info.Skills) != 1 {
		t.Fatal("cache not read back")
	}
	s.db.Exec(`UPDATE cache SET used=?`, time.Now().Add(-8*24*time.Hour).UTC().Format(time.RFC3339Nano))
	if n, err := s.pruneKept(time.Now().Add(-eventRetention)); err != nil || n != 2 {
		t.Fatalf("pruned %d %v", n, err)
	}
	if s.cacheGet("claude-init:/w", &info) {
		t.Fatal("expired cache read")
	}
}

func TestGoalsAndRenamesLeaveNothingBehind(t *testing.T) {
	m, _ := fixture(t)
	r := &runtime{session: Session{ID: "codex:t", Agent: "codex"}}
	m.mu.Lock()
	m.setGoal(r, &Goal{Objective: "ship", Status: "active"})
	m.setGoal(r, nil)
	m.mu.Unlock()
	var n int
	m.store.db.QueryRow(`SELECT count(*) FROM settings WHERE key='goal:codex:t'`).Scan(&n)
	if n != 0 {
		t.Fatal("an ended goal is still stored")
	}
	m.store.setting("options:token", Options{Model: "m"})
	m.store.Save(Session{ID: "token", Agent: "codex"})
	m.moveSessionRows("token", "codex:native")
	m.store.db.QueryRow(`SELECT count(*) FROM settings WHERE key='options:token'`).Scan(&n)
	var rows int
	m.store.db.QueryRow(`SELECT count(*) FROM sessions WHERE id='token'`).Scan(&rows)
	if n != 0 || rows != 0 || m.options("codex:native").Model != "m" {
		t.Fatalf("rename left %d settings, %d rows", n, rows)
	}
}

func TestFollowingRemembersOnlyTheTail(t *testing.T) {
	tail := &externalTail{sent: map[string]uint64{}}
	ev := func(id, text string) Event {
		b, _ := json.Marshal(map[string]any{"uuid": id, "text": text})
		return Event{Data: b}
	}
	first := []Event{ev("a", "1"), ev("b", "2")}
	if got, reset := tail.fresh(first); len(got) != 2 || reset {
		t.Fatalf("first %d", len(got))
	}
	// The page moves on: older records are forgotten.
	next := []Event{ev("b", "2"), ev("c", "3")}
	if got, _ := tail.fresh(next); len(got) != 1 {
		t.Fatalf("next %d", len(got))
	}
	if _, ok := tail.sent["a"]; ok || len(tail.sent) != 2 {
		t.Fatalf("sent %v", tail.sent)
	}
}
