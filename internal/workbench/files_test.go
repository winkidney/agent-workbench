package workbench

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// filesFixture: a session whose directory holds an HTML report and more.
func filesFixture(t *testing.T) (*Server, *Manager, Session) {
	t.Helper()
	posixOnly(t)
	s, m := authServer(t)
	root := t.TempDir()
	for name, body := range map[string]string{
		"report/index.html": `<script src="app.js"></script>`,
		"report/app.js":     `console.log(1)`,
		"doc.pdf":           "%PDF-1.7\n...",
		"fake.pdf":          "<script>alert(1)</script>",
		"my notes.html":     "notes",
	} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700)
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(outside, []byte("secret"), 0600)
	_ = os.Symlink(outside, filepath.Join(root, "escape.txt"))
	session, _ := m.Create("claude", root, "files")
	return s, m, session
}

// open asks for a file preview the way a page does and returns it with
// the URL its grant opens.
func open(t *testing.T, w *httptest.ResponseRecorder) Preview {
	t.Helper()
	var v struct{ Preview Preview }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatalf("open: %d %s", w.Code, w.Body.String())
	}
	return v.Preview
}

func previewBody(session, path string) string {
	return fmt.Sprintf(`{"session":%q,"kind":"file","path":%q}`, session, path)
}

func TestFilePreviewOnAnotherDevice(t *testing.T) {
	s, m, session := filesFixture(t)
	setAccess(t, s, `{"remoteAccess":"password","username":"me","password":"correct horse"}`)
	p := open(t, lan(t, s, "POST", "/api/previews", previewBody(session.ID, "report/index.html"), nil, "me", "correct horse"))
	if !strings.HasPrefix(p.FileURL, filesPrefix) || !strings.HasSuffix(p.FileURL, "/report/index.html") {
		t.Fatalf("file URL: %q", p.FileURL)
	}
	base := strings.TrimSuffix(p.FileURL, "report/index.html")
	// The sandboxed page's own requests carry no password: the grant signs
	// them in.
	w := lan(t, s, "GET", p.FileURL, "", nil)
	if w.Code != 200 || w.Body.String() != `<script src="app.js"></script>` {
		t.Fatalf("page: %d %s", w.Code, w.Body.String())
	}
	h := w.Header()
	if csp := h.Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox allow-scripts") || strings.Contains(csp, "allow-same-origin") || !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Fatalf("sandbox: %q", csp)
	}
	if h.Get("Cache-Control") != "private, no-store" || h.Get("Referrer-Policy") != "no-referrer" || h.Get("Access-Control-Allow-Origin") != "*" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", h)
	}
	if w := lan(t, s, "GET", base+"report/app.js", "", nil); w.Code != 200 {
		t.Fatalf("its own files: %d", w.Code)
	}
	// A real PDF is left to the browser's viewer; a fake one is sandboxed.
	if w := lan(t, s, "GET", base+"doc.pdf", "", nil); w.Header().Get("Content-Type") != "application/pdf" || strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("pdf: %v", w.Header())
	}
	if w := lan(t, s, "GET", base+"fake.pdf", "", nil); !strings.HasPrefix(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("fake pdf: %v", w.Header())
	}
	// Names with spaces are escaped.
	q := open(t, lan(t, s, "POST", "/api/previews", previewBody(session.ID, "my notes.html"), nil, "me", "correct horse"))
	if !strings.HasSuffix(q.FileURL, "/my%20notes.html") || lan(t, s, "GET", q.FileURL, "", nil).Code != 200 {
		t.Fatalf("escaped name: %q", q.FileURL)
	}
	// Nothing outside the session directory, no guessing, no writes.
	for _, bad := range []string{base + "../../../../etc/passwd", base + "/etc/passwd", base + "escape.txt", base + "missing.txt"} {
		if w := lan(t, s, "GET", bad, "", nil); w.Code != 404 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
	for _, bad := range []string{filesPrefix + "0123456789abcdef0123456789abcdef/report/index.html", filesPrefix + "short/x", filesPrefix} {
		if w := lan(t, s, "GET", bad, "", nil); w.Code != 403 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
	if w := lan(t, s, "POST", p.FileURL, "", nil); w.Code != 405 {
		t.Fatalf("POST: %d", w.Code)
	}
	// The preview list never hands out grants.
	if strings.Contains(lan(t, s, "GET", "/api/previews", "", nil, "me", "correct horse").Body.String(), filesPrefix) {
		t.Fatal("grant in the preview list")
	}
	// Changing the password ends the grants issued under it.
	setAccess(t, s, `{"remoteAccess":"password","username":"me","password":"new horse battery"}`)
	if w := lan(t, s, "GET", p.FileURL, "", nil); w.Code != 403 {
		t.Fatalf("after a password change: %d", w.Code)
	}
	p = open(t, lan(t, s, "POST", "/api/previews/"+p.ID+"/authorize", "{}", nil, "me", "new horse battery"))
	if w := lan(t, s, "GET", p.FileURL, "", nil); w.Code != 200 {
		t.Fatalf("opened again: %d", w.Code)
	}
	// So do access off, closing the preview, and time.
	setAccess(t, s, `{"remoteAccess":"off"}`)
	if w := lan(t, s, "GET", p.FileURL, "", nil); w.Code != 403 {
		t.Fatalf("access off: %d", w.Code)
	}
	setAccess(t, s, `{"remoteAccess":"password"}`)
	p = open(t, lan(t, s, "POST", "/api/previews/"+p.ID+"/authorize", "{}", nil, "me", "new horse battery"))
	tok := strings.Split(strings.TrimPrefix(p.FileURL, filesPrefix), "/")[0]
	s.mu.Lock()
	g := s.fileGrants[tok]
	g.expires = time.Now().Add(-time.Second)
	s.fileGrants[tok] = g
	s.mu.Unlock()
	if w := lan(t, s, "GET", p.FileURL, "", nil); w.Code != 403 {
		t.Fatalf("expired: %d", w.Code)
	}
	p = open(t, lan(t, s, "POST", "/api/previews/"+p.ID+"/authorize", "{}", nil, "me", "new horse battery"))
	if err := m.RemovePreview(p.ID); err != nil {
		t.Fatal(err)
	}
	if w := lan(t, s, "GET", p.FileURL, "", nil); w.Code != 403 {
		t.Fatalf("after closing: %d", w.Code)
	}
	// Only signed-in pages are given a grant.
	if w := lan(t, s, "POST", "/api/previews", previewBody(session.ID, "doc.pdf"), nil); w.Code != 401 {
		t.Fatalf("anonymous open: %d", w.Code)
	}
}

func TestFileGrantsFollowTheirSignIn(t *testing.T) {
	s, _, session := filesFixture(t)
	// Issued to this computer while it skips sign-in: only works here.
	setAccess(t, s, `{"remoteAccess":"pairing"}`)
	here := open(t, request(t, s, s.Host, "POST", "/api/previews", previewBody(session.ID, "report/index.html"), nil))
	if w := request(t, s, s.Host, "GET", here.FileURL, "", nil); w.Code != 200 {
		t.Fatalf("this computer: %d", w.Code)
	}
	if w := lan(t, s, "GET", here.FileURL, "", nil); w.Code != 403 {
		t.Fatalf("this computer's grant on another device: %d", w.Code)
	}
	// Under pairing: as long as the pairing session that asked.
	body, _ := json.Marshal(map[string]string{"code": s.Pairing})
	cookie := lan(t, s, "POST", "/api/login", string(body), nil).Result().Cookies()[0]
	p := open(t, lan(t, s, "POST", "/api/previews", previewBody(session.ID, "report/index.html"), cookie))
	if w := lan(t, s, "GET", p.FileURL, "", nil); w.Code != 200 {
		t.Fatalf("paired device: %d", w.Code)
	}
	s.mu.Lock()
	delete(s.auth, cookie.Value)
	s.mu.Unlock()
	if w := lan(t, s, "GET", p.FileURL, "", nil); w.Code != 403 {
		t.Fatalf("after the pairing session ended: %d", w.Code)
	}
	// Sign-in on this computer too: its grants follow its sign-in as well.
	setAccess(t, s, `{"remoteAccess":"password","localSignin":true,"username":"me","password":"correct horse"}`)
	r := httptest.NewRequest("POST", "http://localhost:17880/api/previews", strings.NewReader(previewBody(session.ID, "report/index.html")))
	r.Host, r.RemoteAddr = "localhost:17880", "127.0.0.1:5000"
	r.Header.Set("Origin", "http://localhost:17880")
	r.SetBasicAuth("me", "correct horse")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	signed := open(t, w)
	get := func(url string) int {
		r := httptest.NewRequest("GET", "http://localhost:17880"+url, nil)
		r.Host, r.RemoteAddr = "localhost:17880", "127.0.0.1:5000"
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	if code := get(signed.FileURL); code != 200 {
		t.Fatalf("signed-in grant here: %d", code)
	}
	if code := get(here.FileURL); code != 403 {
		t.Fatalf("grant from before sign-in was required: %d", code)
	}
	// Service previews have no file URL.
	svc := open(t, local(t, s, "POST", "/api/previews", fmt.Sprintf(`{"session":%q,"kind":"service","target":"http://127.0.0.1:1/"}`, session.ID), nil, nil, "me", "correct horse"))
	if svc.FileURL != "" {
		t.Fatalf("service preview file URL: %q", svc.FileURL)
	}
}
