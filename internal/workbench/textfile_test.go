package workbench

import (
	"encoding/json"
	"image/color"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

// textFixture: a session whose directory holds text files and others.
func textFixture(t *testing.T) (*Server, Session, string) {
	t.Helper()
	s, m := authServer(t)
	root := realTempDir(t)
	for name, body := range map[string]string{
		"notes.md":       "hello\n",
		"crlf.txt":       "a\r\nb\r\n",
		"page.html":      "<h1>Hi</h1>",
		"binary.dat":     "a\x00b",
		"latin1.txt":     "caf\xe9",
		"doc.pdf":        "%PDF-1.7\n",
		"big.txt":        strings.Repeat("a", textEditMax+1),
		"sub/readme.txt": "in a folder",
	} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o700)
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(root, "image.png"), pngOf(t, 2, 2, color.RGBA{0, 0, 255, 255}), 0o600)
	session, err := m.Create("claude", root, "text")
	if err != nil {
		t.Fatal(err)
	}
	return s, session, root
}

func textURL(session Session, path string, stamp ...string) string {
	u := "/api/text?session=" + url.QueryEscape(session.ID) + "&path=" + url.QueryEscape(path)
	if len(stamp) > 0 {
		u += "&stamp=" + url.QueryEscape(stamp[0])
	}
	return u
}

func readText(t *testing.T, s *Server, session Session, path string) (int, string, string) {
	t.Helper()
	w := request(t, s, s.Host, "GET", textURL(session, path), "", nil)
	var v struct{ Text, Stamp string }
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("cache: %q", w.Header().Get("Cache-Control"))
		}
	}
	return w.Code, v.Text, v.Stamp
}

func saveText(t *testing.T, s *Server, session Session, path, text string, stamp ...string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, s, s.Host, "PUT", textURL(session, path, stamp...), text, nil)
}

func TestTextFilesAreReadAndSavedInPlace(t *testing.T) {
	s, session, root := textFixture(t)
	code, text, stamp := readText(t, s, session, "notes.md")
	if code != 200 || text != "hello\n" || stamp == "" {
		t.Fatalf("read: %d %q %q", code, text, stamp)
	}
	file := filepath.Join(root, "notes.md")
	before, _ := os.Stat(file)
	posix := goruntime.GOOS != "windows"
	if posix {
		// A hard link sees the save: the file is written in place.
		if err := os.Link(file, filepath.Join(root, "linked.md")); err != nil {
			t.Fatal(err)
		}
	}
	w := saveText(t, s, session, "notes.md", "hello, 世界\n", stamp)
	var saved struct{ Stamp string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &saved) != nil || saved.Stamp == "" || saved.Stamp == stamp {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(file); string(got) != "hello, 世界\n" {
		t.Fatalf("saved: %q", got)
	}
	after, _ := os.Stat(file)
	if !os.SameFile(before, after) || (posix && after.Mode().Perm() != 0o640) {
		t.Fatalf("not saved in place: %v → %v", before.Mode(), after.Mode())
	}
	if posix {
		if got, _ := os.ReadFile(filepath.Join(root, "linked.md")); string(got) != "hello, 世界\n" {
			t.Fatalf("hard link: %q", got)
		}
	}
	if _, _, again := readText(t, s, session, "notes.md"); again != saved.Stamp {
		t.Fatalf("stamp after save: %q, read %q", saved.Stamp, again)
	}
	// Changed on disk since: not overwritten, and the page learns its stamp.
	_ = os.WriteFile(file, []byte("the agent's version\n"), 0o640)
	w = saveText(t, s, session, "notes.md", "mine\n", saved.Stamp)
	var conflict struct{ Error, Stamp string }
	if w.Code != 409 || json.Unmarshal(w.Body.Bytes(), &conflict) != nil || conflict.Error != "changed" || conflict.Stamp == saved.Stamp {
		t.Fatalf("conflict: %d %s", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(file); string(got) != "the agent's version\n" {
		t.Fatalf("overwritten on conflict: %q", got)
	}
	// Overwrite on purpose: no stamp.
	if w := saveText(t, s, session, "notes.md", "mine\n"); w.Code != 200 {
		t.Fatalf("overwrite: %d %s", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(file); string(got) != "mine\n" {
		t.Fatalf("overwrite: %q", got)
	}
	// Line endings and bytes are as sent.
	_, crlf, crlfStamp := readText(t, s, session, "crlf.txt")
	if crlf != "a\r\nb\r\n" {
		t.Fatalf("crlf read: %q", crlf)
	}
	if w := saveText(t, s, session, "crlf.txt", "\xef\xbb\xbfa\r\nc\r\n", crlfStamp); w.Code != 200 {
		t.Fatalf("crlf save: %d", w.Code)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "crlf.txt")); string(got) != "\xef\xbb\xbfa\r\nc\r\n" {
		t.Fatalf("crlf saved: %q", got)
	}
	// In a folder.
	if code, text, _ := readText(t, s, session, "sub/readme.txt"); code != 200 || text != "in a folder" {
		t.Fatalf("sub: %d %q", code, text)
	}
}

func TestTextFilesRefuseWhatIsNotText(t *testing.T) {
	s, session, root := textFixture(t)
	_ = os.WriteFile(filepath.Join(filepath.Dir(root), "beside.txt"), []byte("beside"), 0o600)
	for path, want := range map[string]int{
		"binary.dat":       415,
		"latin1.txt":       415,
		"image.png":        415,
		"big.txt":          413,
		"missing.txt":      404,
		"../beside.txt":    400,
		"../missing.txt":   404,
		"sub":              400,
		"":                 400,
		root + "/notes.md": 400,
	} {
		if code, _, _ := readText(t, s, session, path); code != want {
			t.Errorf("read %q: %d, want %d", path, code, want)
		}
	}
	_, _, stamp := readText(t, s, session, "notes.md")
	if w := saveText(t, s, session, "notes.md", "caf\xe9", stamp); w.Code != 415 {
		t.Errorf("save not UTF-8: %d", w.Code)
	}
	if w := saveText(t, s, session, "notes.md", "a\x00b", stamp); w.Code != 415 {
		t.Errorf("save with NUL: %d", w.Code)
	}
	if w := saveText(t, s, session, "notes.md", strings.Repeat("a", textEditMax+1), stamp); w.Code != 413 {
		t.Errorf("save too large: %d", w.Code)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "notes.md")); string(got) != "hello\n" {
		t.Fatalf("refused saves changed the file: %q", got)
	}
	// Nothing is created.
	if w := saveText(t, s, session, "new.txt", "x"); w.Code != 404 {
		t.Errorf("save a new file: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err == nil {
		t.Fatal("a new file was created")
	}
	if goruntime.GOOS == "windows" {
		return
	}
	// Links out of the directory, and read-only files.
	outside := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(outside, []byte("secret"), 0o600)
	_ = os.Symlink(outside, filepath.Join(root, "escape.txt"))
	if code, _, _ := readText(t, s, session, "escape.txt"); code != 400 {
		t.Errorf("read through a link out: %d", code)
	}
	if w := saveText(t, s, session, "escape.txt", "gotcha"); w.Code != 400 {
		t.Errorf("save through a link out: %d", w.Code)
	}
	if got, _ := os.ReadFile(outside); string(got) != "secret" {
		t.Fatalf("outside file changed: %q", got)
	}
	if os.Geteuid() != 0 {
		ro := filepath.Join(root, "ro.txt")
		_ = os.WriteFile(ro, []byte("fixed"), 0o444)
		w := saveText(t, s, session, "ro.txt", "changed")
		if w.Code != 403 || !strings.Contains(w.Body.String(), "read-only") {
			t.Errorf("read-only: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestTextFilesNeedSignInAndTheOrigin(t *testing.T) {
	s, session, root := textFixture(t)
	setAccess(t, s, `{"remoteAccess":"pairing"}`)
	if w := lan(t, s, "GET", textURL(session, "notes.md"), "", nil); w.Code != 401 {
		t.Fatalf("anonymous read: %d", w.Code)
	}
	if w := lan(t, s, "PUT", textURL(session, "notes.md"), "x", nil); w.Code != 401 {
		t.Fatalf("anonymous save: %d", w.Code)
	}
	r := httptest.NewRequest("PUT", "http://"+s.Host+textURL(session, "notes.md"), strings.NewReader("x"))
	r.Host, r.RemoteAddr = s.Host, "127.0.0.1:1234"
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin save: %d", w.Code)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "notes.md")); string(got) != "hello\n" {
		t.Fatalf("refused saves changed the file: %q", got)
	}
}

func TestPreviewsSayWhichFilesAreText(t *testing.T) {
	s, session, _ := textFixture(t)
	for path, want := range map[string]bool{
		"notes.md":       true,
		"page.html":      true,
		"sub/readme.txt": true,
		"image.png":      false,
		"doc.pdf":        false,
		"binary.dat":     false,
		"big.txt":        false,
	} {
		p := open(t, request(t, s, s.Host, "POST", "/api/previews", previewBody(session.ID, path), nil))
		if p.Text != want {
			t.Errorf("%s: text %v, want %v", path, p.Text, want)
		}
	}
}

func TestLooksText(t *testing.T) {
	cut := []byte(strings.Repeat("a", 10) + "世")
	cut = cut[:len(cut)-1] // the head ends inside 世
	for _, c := range []struct {
		name  string
		data  []byte
		whole bool
		want  bool
	}{
		{"empty", nil, true, true},
		{"ascii", []byte("hello"), true, true},
		{"chinese", []byte("你好"), true, true},
		{"nul", []byte("a\x00"), true, false},
		{"latin-1", []byte("caf\xe9"), true, false},
		{"head cut in a character", cut, false, true},
		{"whole file cut in a character", cut, true, false},
		{"head with a bad byte before the cut", append([]byte("\xff"), cut...), false, false},
		{"head ending in a stray continuation byte", []byte("ab\x80"), false, false},
	} {
		if got := looksText(c.data, c.whole); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
