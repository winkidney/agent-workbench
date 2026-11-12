package workbench

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestOtherDevicesDownloadWhatASessionWrote(t *testing.T) {
	posixOnly(t)
	s, session, root, _ := mediaServer(t)
	cookie := request(t, s, s.Host, "POST", "/api/login", `{"code":"pair"}`, nil).Result().Cookies()[0]
	game := filepath.Join(root, "game")
	os.WriteFile(filepath.Join(game, "notes 報告.txt"), []byte("0123456789"), 0o644)
	os.MkdirAll(filepath.Join(game, "out", "deep"), 0o755)
	os.WriteFile(filepath.Join(game, "out", "a.txt"), bytes.Repeat([]byte("a"), 1000), 0o644)
	os.WriteFile(filepath.Join(game, "out", "deep", "b.png"), []byte("png"), 0o644)
	secret := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(secret, []byte("secret"), 0o600)
	os.Symlink(secret, filepath.Join(game, "out", "link"))
	os.Symlink(filepath.Dir(secret), filepath.Join(game, "out", "elsewhere"))

	// From another device on the network, signed in.
	get := func(method, q string, c *http.Cookie, header ...string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://"+lanHost+"/api/download?session="+url.QueryEscape(session.ID)+"&"+q, nil)
		r.Host, r.RemoteAddr = lanHost, "192.168.1.20:5000"
		if c != nil {
			r.AddCookie(c)
		}
		for i := 0; i+1 < len(header); i += 2 {
			r.Header.Set(header[i], header[i+1])
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	filename := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		kind, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
		if err != nil || kind != "attachment" {
			t.Fatalf("disposition %q: %v", w.Header().Get("Content-Disposition"), err)
		}
		return params["filename"]
	}

	// A file: as it is, named, never shown in the page; ranges resume it.
	file := "path=" + url.QueryEscape("notes 報告.txt") + "&base=" + url.QueryEscape(game)
	w := get("GET", file, cookie)
	if w.Code != 200 || w.Body.String() != "0123456789" || filename(w) != "notes 報告.txt" || w.Header().Get("Content-Type") != "application/octet-stream" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("file: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	if w := get("GET", file, cookie, "Range", "bytes=2-4"); w.Code != 206 || w.Body.String() != "234" {
		t.Fatalf("range: %d %q", w.Code, w.Body.String())
	}
	if w := get("HEAD", file, cookie); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "10" {
		t.Fatalf("head: %d %d %v", w.Code, w.Body.Len(), w.Header())
	}

	// A folder: a zip made while it is sent, without the links in it.
	w = get("GET", "path=game/out", cookie)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" || filename(w) != "out.zip" {
		t.Fatalf("folder: %d %v", w.Code, w.Header())
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range z.File {
		names = append(names, f.Name)
		if f.Name == "out/a.txt" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			if len(b) != 1000 || f.Method != zip.Deflate {
				t.Fatalf("a.txt: %d bytes, method %d", len(b), f.Method)
			}
		}
		if f.Name == "out/deep/b.png" && f.Method != zip.Store {
			t.Fatal("a compressed format was deflated again")
		}
	}
	sort.Strings(names)
	if got := strings.Join(names, " "); got != "out/ out/a.txt out/deep/ out/deep/b.png" {
		t.Fatalf("zip entries: %s", got)
	}
	if w := get("HEAD", "path=game/out", cookie); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("folder head: %d %d", w.Code, w.Body.Len())
	}

	// Only what the session can reach, only for a signed-in device.
	for q, want := range map[string]int{
		"path=../../etc/hostname":         400,
		"path=" + url.QueryEscape(secret): 400,
		"path=game/missing.txt":           404,
	} {
		if w := get("GET", q, cookie); w.Code != want {
			t.Fatalf("%s: %d", q, w.Code)
		}
	}
	if w := get("GET", file, nil); w.Code != 401 {
		t.Fatalf("not signed in: %d", w.Code)
	}

	// There "Show in Files" would open a window nobody there sees.
	body := `{"path":"game/out"}`
	if w := lan(t, s, "POST", "/api/sessions/"+url.PathEscape(session.ID)+"/reveal", body, cookie); w.Code != 400 || !strings.Contains(w.Body.String(), "download") {
		t.Fatalf("reveal from another device: %d %s", w.Code, w.Body.String())
	}
}

func TestAuthStatusSaysWhetherThePageIsOnThisComputer(t *testing.T) {
	s, _ := authServer(t)
	setAccess(t, s, `{"remoteAccess":"password","username":"me","password":"correct horse"}`)
	here := func(w *httptest.ResponseRecorder) bool {
		t.Helper()
		var v struct{ ThisComputer bool }
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		return v.ThisComputer
	}
	if !here(request(t, s, s.Host, "GET", "/api/auth/status", "", nil)) {
		t.Fatal("this computer")
	}
	if here(lan(t, s, "GET", "/api/auth/status", "", nil, "me", "correct horse")) {
		t.Fatal("another device")
	}
	if here(fromTunnel(t, s, "ai.example.com", "https://ai.example.com", "GET", "/api/auth/status", "", "me", "correct horse")) {
		t.Fatal("through a tunnel")
	}
}
