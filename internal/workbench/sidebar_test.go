package workbench

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestSidebarPrefsOperations(t *testing.T) {
	m, _ := fixture(t)
	steps := []struct{ op, id string }{
		{"pinSession", "codex:a"}, {"pinSession", "claude:b"}, {"pinSession", "codex:a"},
		{"pinWorkspace", "/w/one"}, {"hide", "claude:b"}, {"hide", "codex:z"}, {"unhide", "codex:z"},
	}
	var p SidebarPrefs
	var err error
	for _, s := range steps {
		if p, err = m.UpdatePrefs(s.op, s.id); err != nil {
			t.Fatal(err)
		}
	}
	// Idempotent pins, hiding unpins, unhide restores.
	if strings.Join(p.PinnedSessions, ",") != "codex:a" || strings.Join(p.Hidden, ",") != "claude:b" || strings.Join(p.PinnedWorkspaces, ",") != "/w/one" {
		t.Fatalf("prefs %+v", p)
	}
	if got := m.Prefs(); strings.Join(got.PinnedSessions, ",") != "codex:a" {
		t.Fatalf("not persisted: %+v", got)
	}
	if _, err := m.UpdatePrefs("explode", "x"); err == nil {
		t.Fatal("unknown op accepted")
	}
	if _, err := m.UpdatePrefs("pinSession", " "); err == nil {
		t.Fatal("empty id accepted")
	}
	events, _ := m.store.Events("", 0, 1000)
	prefsEvents := 0
	for _, e := range events {
		if e.Kind == "prefs" {
			prefsEvents++
		}
	}
	if prefsEvents != len(steps) {
		t.Fatalf("prefs events %d", prefsEvents)
	}
	for i := 0; i < prefsLimit+5; i++ {
		m.UpdatePrefs("pinWorkspace", fmt.Sprint("/w/", i))
	}
	if got := m.Prefs().PinnedWorkspaces; len(got) != prefsLimit || got[len(got)-1] != fmt.Sprint("/w/", prefsLimit+4) {
		t.Fatalf("cap: %d", len(got))
	}
}

func TestSidebarPrefsConcurrentUpdates(t *testing.T) {
	m, _ := fixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.UpdatePrefs("pinSession", fmt.Sprint("codex:", i))
		}(i)
	}
	wg.Wait()
	if n := len(m.Prefs().PinnedSessions); n != 40 {
		t.Fatalf("lost updates: %d", n)
	}
}

func TestRenameUsesNativeCodexOrAlias(t *testing.T) {
	m, _ := fixture(t)
	var calls []map[string]any
	fail := false
	m.renameCodex = func(p map[string]any) error {
		calls = append(calls, p)
		if fail {
			return errors.New("thread not loaded")
		}
		return nil
	}
	codex, _ := m.Create("codex", t.TempDir(), "c")
	m.sessions[codex.ID].session.NativeID = "thread-1"
	claude, _ := m.Create("claude", t.TempDir(), "k")

	if native, err := m.Rename(codex.ID, "  Native   name "); err != nil || !native || len(calls) != 1 || calls[0]["name"] != "Native name" || calls[0]["threadId"] != "thread-1" {
		t.Fatalf("codex native rename: %v %v %v", native, err, calls)
	}
	if _, ok := m.Prefs().Aliases[codex.ID]; ok {
		t.Fatal("native rename left an alias")
	}
	fail = true
	if native, _ := m.Rename(codex.ID, "Fallback"); native || m.Prefs().Aliases[codex.ID] != "Fallback" {
		t.Fatal("failed native rename did not fall back to alias")
	}
	if native, _ := m.Rename(claude.ID, "Display only"); native || m.Prefs().Aliases[claude.ID] != "Display only" {
		t.Fatal("claude alias missing")
	}
	if len(calls) != 2 {
		t.Fatal("claude rename touched codex")
	}
	m.Rename(claude.ID, "")
	if _, ok := m.Prefs().Aliases[claude.ID]; ok {
		t.Fatal("empty title did not clear alias")
	}
	if _, err := m.Rename(claude.ID, strings.Repeat("x", 201)); err == nil {
		t.Fatal("long title accepted")
	}
}

func TestSidebarRoutesRequireAuthAndOrigin(t *testing.T) {
	m, _ := fixture(t)
	s := pairedServer(m, nil, "agent.localhost:17880", "pair")
	if w := lan(t, s, "GET", "/api/sidebar", "", nil); w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	cookie := request(t, s, s.Host, "POST", "/api/login", `{"code":"pair"}`, nil).Result().Cookies()[0]
	if w := request(t, s, s.Host, "POST", "/api/sidebar", `{"op":"pinSession","id":"codex:a"}`, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "codex:a") {
		t.Fatalf("pin: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("POST", "http://"+s.Host+"/api/sidebar", strings.NewReader(`{"op":"hide","id":"codex:a"}`))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Host = s.Host
	r.Header.Set("Origin", "http://evil.localhost:17880")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin accepted: %d", w.Code)
	}
}
