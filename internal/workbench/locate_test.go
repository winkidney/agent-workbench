package workbench

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The page shows a path a session wrote in its file list: resolved to the
// session's directory, and listed there even when the list hides it.
func TestLocateResolvesAPathAndListsIt(t *testing.T) {
	posixOnly(t)
	s, session, root, _ := mediaServer(t)
	cookie := request(t, s, s.Host, "POST", "/api/login", `{"code":"pair"}`, nil).Result().Cookies()[0]
	get := func(path string) (int, string) {
		r := httptest.NewRequest("GET", "http://"+s.Host+path, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Host = s.Host
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	resolve := func(q string) (int, string) {
		return get("/api/resolve?session=" + url.QueryEscape(session.ID) + "&" + q)
	}
	base := url.QueryEscape(filepath.Join(root, "game"))
	for q, want := range map[string]string{
		"path=video/plan.json&base=" + base:                             `{"directory":false,"path":"game/video/plan.json"}`,
		"path=" + url.QueryEscape(filepath.Join(root, "game", "video")): `{"directory":true,"path":"game/video"}`,
		"path=README.md": `{"directory":false,"path":"README.md"}`,
	} {
		code, body := resolve(q)
		if code != 200 || strings.TrimSpace(body) != want {
			t.Fatalf("%s: %d %s", q, code, body)
		}
	}
	if code, _ := resolve("path=missing.bin"); code != 404 {
		t.Fatalf("missing: %d", code)
	}
	if code, _ := resolve("path=../../etc/hostname"); code != 400 {
		t.Fatalf("outside: %d", code)
	}
	if code := lan(t, s, "GET", "/api/resolve?session="+url.QueryEscape(session.ID)+"&path=README.md", "", nil).Code; code != 401 {
		t.Fatalf("unauthenticated: %d", code)
	}

	os.WriteFile(filepath.Join(root, ".env"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(root, "node_modules"), 0o755)
	list := func(q string) []string {
		code, body := get("/api/files?session=" + url.QueryEscape(session.ID) + "&path=&" + q)
		if code != 200 {
			t.Fatalf("files %s: %d %s", q, code, body)
		}
		var entries []struct{ Name string }
		json.Unmarshal([]byte(body), &entries)
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name)
		}
		return names
	}
	if got := fmt.Sprint(list("")); strings.Contains(got, ".env") || strings.Contains(got, "node_modules") {
		t.Fatalf("hidden listed: %s", got)
	}
	if got := fmt.Sprint(list("show=.env")); !strings.Contains(got, ".env") || strings.Contains(got, "node_modules") {
		t.Fatalf("show .env: %s", got)
	}
	if got := fmt.Sprint(list("show=node_modules")); !strings.Contains(got, "node_modules") {
		t.Fatalf("show node_modules: %s", got)
	}
}

// Past the first 1000 entries, the one shown is still listed.
func TestFilesShowsTheEntryPastTheCap(t *testing.T) {
	posixOnly(t)
	s, session, root, _ := mediaServer(t)
	dir := filepath.Join(root, "many")
	os.MkdirAll(dir, 0o755)
	for i := range 1005 {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d", i)), nil, 0o644)
	}
	cookie := request(t, s, s.Host, "POST", "/api/login", `{"code":"pair"}`, nil).Result().Cookies()[0]
	r := httptest.NewRequest("GET", "http://"+s.Host+"/api/files?session="+url.QueryEscape(session.ID)+"&path=many&show=f1004", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Host = s.Host
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var entries []struct{ Name string }
	json.Unmarshal(w.Body.Bytes(), &entries)
	if len(entries) != 1001 || entries[1000].Name != "f1004" {
		t.Fatalf("got %d entries, last %v", len(entries), entries[len(entries)-1])
	}
}
