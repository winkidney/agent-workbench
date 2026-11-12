package workbench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPreviewTabsKeepTheTargetsOfASession(t *testing.T) {
	posixOnly(t)
	s, m := authServer(t)
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "site"), 0o700)
	page := filepath.Join(root, "site", "index.html")
	os.WriteFile(page, []byte("<h1>one</h1>"), 0o600)
	session, _ := m.Create("claude", root, "tabs")
	url := "/api/sessions/" + session.ID + "/preview-tabs"
	read := func() PreviewTabs {
		w := request(t, s, s.Host, "GET", url, "", nil)
		var v PreviewTabs
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatalf("GET %d %s", w.Code, w.Body.String())
		}
		return v
	}
	if v := read(); len(v.Tabs) != 0 || v.Active != -1 {
		t.Fatalf("empty %+v", v)
	}

	// Kept as targets, with what each finds now.
	put := `{"active":0,"tabs":[{"kind":"file","path":"/site/index.html","stamp":"forged"},{"kind":"file","path":"gone.html"},{"kind":"service","target":"http://127.0.0.1:5173/app"},{"kind":"service","command":"npm run dev","port":5174,"preview":"nope"}]}`
	if w := request(t, s, s.Host, "PUT", url, put, nil); w.Code != 200 {
		t.Fatalf("PUT %d %s", w.Code, w.Body.String())
	}
	v := read()
	if len(v.Tabs) != 4 || v.Active != 0 {
		t.Fatalf("tabs %+v", v)
	}
	file := v.Tabs[0]
	if file.Path != "site/index.html" || file.Stamp == "" || file.Stamp == "forged" || file.Missing {
		t.Fatalf("file tab %+v", file)
	}
	if !v.Tabs[1].Missing || v.Tabs[2].Running || v.Tabs[3].Running || v.Tabs[3].Command != "npm run dev" {
		t.Fatalf("other tabs %+v", v.Tabs[1:])
	}
	// A change to the file changes its stamp.
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(page, []byte("<h1>two, longer</h1>"), 0o600)
	if read().Tabs[0].Stamp == file.Stamp {
		t.Fatal("stamp did not follow the file")
	}

	// Six at most; only targets inside the session and on loopback.
	var many []string
	for i := 0; i < 9; i++ {
		many = append(many, fmt.Sprintf(`{"kind":"file","path":"f%d.html"}`, i))
	}
	body := `{"active":8,"tabs":[` + join(many) + `]}`
	if w := request(t, s, s.Host, "PUT", url, body, nil); w.Code != 200 {
		t.Fatalf("many %d", w.Code)
	}
	if v := read(); len(v.Tabs) != 6 || v.Active != -1 || v.Tabs[5].Path != "f5.html" {
		t.Fatalf("six %+v", v)
	}
	for _, bad := range []string{
		`{"tabs":[{"kind":"file","path":"../secret"}]}`,
		`{"tabs":[{"kind":"service","target":"http://example.com:80"}]}`,
		`{"tabs":[{"kind":"service","command":"x","port":80}]}`,
		`{"tabs":[{"kind":"shell"}]}`,
	} {
		if w := request(t, s, s.Host, "PUT", url, bad, nil); w.Code != 400 {
			t.Fatalf("%s: %d", bad, w.Code)
		}
	}
	if w := request(t, s, s.Host, "GET", "/api/sessions/nobody/preview-tabs", "", nil); w.Code != 404 {
		t.Fatalf("unknown session %d", w.Code)
	}
	// Signed-out devices see nothing.
	setAccess(t, s, `{"remoteAccess":"pairing"}`)
	if w := lan(t, s, "GET", url, "", nil); w.Code != 401 {
		t.Fatalf("signed out %d", w.Code)
	}
}

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

func TestFilePreviewsAreRowsThatExpire(t *testing.T) {
	posixOnly(t)
	m, _ := fixture(t)
	m.previewPort = "17880"
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.html"), []byte("a"), 0o600)
	os.WriteFile(filepath.Join(root, "b.html"), []byte("b"), 0o600)
	session, _ := m.Create("claude", root, "rows")
	first, err := m.CreatePreview(session.ID, "file", "a.html", "", "", "", 0, "agent.localhost:17880")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := m.CreatePreview(session.ID, "file", "a.html", "", "", "", 0, "agent.localhost:17880")
	other, _ := m.CreatePreview(session.ID, "file", "b.html", "", "", "", 0, "agent.localhost:17880")
	if again.ID != first.ID || other.ID == first.ID || again.secret != first.secret {
		t.Fatalf("previews %s %s %s", first.ID, again.ID, other.ID)
	}
	// Nothing kept in memory: found from the store.
	m.mu.Lock()
	n := len(m.find(session.ID).previews)
	m.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d previews in memory", n)
	}
	found := m.FindPreview(first.ID)
	if found == nil || found.Entry != "/a.html" || found.root != session.Cwd || found.secret != first.secret || found.URL != first.URL {
		t.Fatalf("found %+v", found)
	}
	// Removed with its row.
	if err := m.RemovePreview(other.ID); err != nil || m.FindPreview(other.ID) != nil {
		t.Fatalf("remove: %v", err)
	}
	// Unused for the retention: gone, with tabs unused as long.
	m.store.setPreviewTabs(session.ID, PreviewTabs{Tabs: []PreviewTab{{Kind: "file", Path: "a.html"}}})
	old := time.Now().Add(-eventRetention - time.Hour).UTC().Format(time.RFC3339Nano)
	m.store.db.Exec(`UPDATE file_previews SET used=?`, old)
	m.store.db.Exec(`UPDATE preview_tabs SET used=?`, old)
	if n, err := m.store.PrunePreviews(time.Now().Add(-eventRetention)); err != nil || n != 2 {
		t.Fatalf("pruned %d %v", n, err)
	}
	if m.FindPreview(first.ID) != nil || len(m.PreviewTabsOf(session.ID).Tabs) != 0 {
		t.Fatal("expired previews or tabs kept")
	}
	// Forgetting a session forgets its previews.
	p, _ := m.CreatePreview(session.ID, "file", "a.html", "", "", "", 0, "agent.localhost:17880")
	m.store.Forget(session.ID)
	if m.FindPreview(p.ID) != nil {
		t.Fatal("forgotten session's preview kept")
	}
}
