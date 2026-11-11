package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// workspace builds a launch directory with a subproject the agent worked in.
func workspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "game", "video"), 0o755)
	os.WriteFile(filepath.Join(root, "game", "video", "plan.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("root"), 0o644)
	os.WriteFile(filepath.Join(root, "game", "README.md"), []byte("sub"), 0o644)
	return root
}

func TestResolveSessionPathUsesWhereTheAgentWas(t *testing.T) {
	posixOnly(t)
	root := workspace(t)
	sub := filepath.Join(root, "game")
	want := filepath.Join(sub, "video", "plan.json")
	for _, c := range []struct {
		name, base, path string
		related          []string
	}{
		{"record cwd", sub, "video/plan.json", nil},
		{"related subproject", "", "video/plan.json", []string{"game"}},
		{"absolute inside", "", want, nil},
		{"file URL", "", "file://" + want, nil},
		{"dot-dot within root", sub, "../game/video/plan.json", nil},
	} {
		full, rel, err := resolveSessionPath(root, c.base, c.path, c.related)
		real, _ := filepath.EvalSymlinks(want)
		if err != nil || full != real || rel != filepath.Join("game", "video", "plan.json") {
			t.Fatalf("%s: %q %q %v", c.name, full, rel, err)
		}
	}
	// The record's directory wins over the root for the same name.
	if full, _, _ := resolveSessionPath(root, sub, "README.md", nil); filepath.Base(filepath.Dir(full)) != "game" {
		t.Fatalf("base not preferred: %s", full)
	}
	if full, _, _ := resolveSessionPath(root, "", "README.md", []string{"game"}); filepath.Dir(full) == sub {
		t.Fatalf("root not preferred over subprojects: %s", full)
	}

	_, _, err := resolveSessionPath(root, "", "video/plan.json", nil)
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "not found: video/plan.json (looked in ./)") {
		t.Fatalf("missing: %v", err)
	}
	if strings.Contains(err.Error(), "lstat") {
		t.Fatalf("raw lstat error leaked: %v", err)
	}

	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644)
	os.Symlink(outside, filepath.Join(root, "link"))
	for _, p := range []string{"../" + filepath.Base(outside) + "/secret.txt", filepath.Join(outside, "secret.txt"), "link/secret.txt", "/etc/hostname"} {
		if _, _, err := resolveSessionPath(root, "", p, nil); err == nil || errors.Is(err, fs.ErrNotExist) && !strings.HasPrefix(p, "/etc") {
			t.Fatalf("%s escaped the root: %v", p, err)
		}
	}
	// A base outside the root is ignored rather than trusted.
	if _, _, err := resolveSessionPath(root, outside, "secret.txt", nil); err == nil {
		t.Fatal("base outside root trusted")
	}
}

func TestClaudeHistoryCarriesRecordCwd(t *testing.T) {
	posixOnly(t)
	path := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(path, []byte(`{"type":"assistant","uuid":"a","cwd":"/w/sub","message":{"id":"m","content":[{"type":"text","text":"see video/plan.json"}]}}`+"\n"), 0o600)
	page, err := readClaudeHistory(path, Session{ID: "claude:x"}, "", 10)
	if err != nil || len(page.Events) != 1 {
		t.Fatal(page, err)
	}
	var v map[string]any
	json.Unmarshal(page.Events[0].Data, &v)
	if v["cwd"] != "/w/sub" {
		t.Fatalf("cwd: %v", v["cwd"])
	}
}

func mediaServer(t *testing.T) (*Server, Session, string, func(string) string) {
	m, _ := fixture(t)
	root := workspace(t)
	session, _ := m.Create("claude", root, "media")
	s := pairedServer(m, nil, "agent.localhost:17880", "pair")
	cookie := request(t, s, s.Host, "POST", "/api/login", `{"code":"pair"}`, nil).Result().Cookies()[0]
	get := func(q string) string {
		r := httptest.NewRequest("GET", "http://"+s.Host+"/api/media?session="+url.QueryEscape(session.ID)+"&"+q, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Host = s.Host
		r.AddCookie(cookie)
		r.Header.Set("Range", "bytes=0-9")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return fmt.Sprintf("%d %s %s", w.Code, w.Header().Get("Content-Type"), w.Header().Get("Content-Range"))
	}
	return s, session, root, get
}

func TestMediaServesVideoRangesAndRefusesActiveContent(t *testing.T) {
	_, _, root, get := mediaServer(t)
	mp4 := append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 2, 0, 'i', 's', 'o', 'm', 'm', 'p', '4', '1'}, make([]byte, 1000)...)
	os.WriteFile(filepath.Join(root, "game", "video", "clip.mp4"), mp4, 0o644)
	base := url.QueryEscape(filepath.Join(root, "game"))
	if got := get("path=video/clip.mp4&base=" + base); !strings.HasPrefix(got, "206 video/mp4 bytes 0-9/1024") {
		t.Fatalf("video range: %s", got)
	}
	os.WriteFile(filepath.Join(root, "a.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o644)
	os.WriteFile(filepath.Join(root, "fake.mp4"), []byte("<html><script>alert(1)</script></html>"), 0o644)
	for q, want := range map[string]string{
		"path=a.svg":               "415",
		"path=fake.mp4":            "415",
		"path=video/clip.mp4":      "404", // no base, not related: not found, quietly
		"path=../etc/passwd.png":   "400",
		"path=game/README.md":      "415",
		"path=missing/nothing.png": "404",
	} {
		if got := get(q); !strings.HasPrefix(got, want) {
			t.Fatalf("%s: %s", q, got)
		}
	}
	big, _ := os.Create(filepath.Join(root, "huge.png"))
	big.Truncate(33 << 20)
	big.Close()
	if got := get("path=huge.png"); !strings.HasPrefix(got, "404") {
		t.Fatalf("image cap: %s", got)
	}
}

func TestRevealShowsTheFileInTheFileManager(t *testing.T) {
	posixOnly(t)
	s, session, root, _ := mediaServer(t)
	var calls [][]string
	fail := map[string]bool{}
	origRun, origOS := revealRun, revealOS
	revealRun = func(_ context.Context, name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		if fail[name] {
			return errors.New("no file manager on the bus")
		}
		return nil
	}
	revealOS = "linux"
	t.Cleanup(func() { revealRun, revealOS = origRun, origOS })
	cookie := request(t, s, s.Host, "POST", "/api/login", `{"code":"pair"}`, nil).Result().Cookies()[0]
	post := func(body string, c *http.Cookie) int {
		return request(t, s, s.Host, "POST", "/api/sessions/"+url.PathEscape(session.ID)+"/reveal", body, c).Code
	}
	body := fmt.Sprintf(`{"path":"video/plan.json","base":%q}`, filepath.Join(root, "game"))
	if code := post(body, cookie); code != 200 {
		t.Fatalf("reveal: %d", code)
	}
	real, _ := filepath.EvalSymlinks(filepath.Join(root, "game", "video", "plan.json"))
	got := strings.Join(calls[0], " ")
	if want := "busctl --user call org.freedesktop.FileManager1 /org/freedesktop/FileManager1 org.freedesktop.FileManager1 ShowItems ass 1 file://" + real + " "; got != want {
		t.Fatalf("busctl args:\n%s\n%s", got, want)
	}
	calls = nil
	post(`{"path":"game"}`, cookie)
	if calls[0][6] != "ShowFolders" {
		t.Fatalf("folder: %v", calls[0])
	}
	// Without a FileManager1 service, the folder (never the file) is opened.
	calls, fail["busctl"] = nil, true
	post(body, cookie)
	if len(calls) != 2 || calls[1][0] != "xdg-open" || calls[1][1] != filepath.Dir(real) {
		t.Fatalf("fallback: %v", calls)
	}
	calls = nil
	if code := post(`{"path":"../../etc/hostname"}`, cookie); code == 200 || len(calls) != 0 {
		t.Fatalf("outside root revealed: %d %v", code, calls)
	}
	if code := lan(t, s, "POST", "/api/sessions/"+url.PathEscape(session.ID)+"/reveal", body, nil).Code; code != 401 || len(calls) != 0 {
		t.Fatalf("unauthenticated: %d", code)
	}
	r := httptest.NewRequest("POST", "http://"+s.Host+"/api/sessions/"+url.PathEscape(session.ID)+"/reveal", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Host = s.Host
	r.Header.Set("Origin", "http://evil.localhost:17880")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 || len(calls) != 0 {
		t.Fatalf("cross-origin: %d", w.Code)
	}
}

// A session's directory is stored resolved; paths written through a symlink
// to it (macOS's /var and /tmp) still resolve, and only inside it.
func TestResolveSessionPathThroughASymlinkedRoot(t *testing.T) {
	posixOnly(t)
	parent := realTempDir(t)
	root := filepath.Join(parent, "real")
	os.MkdirAll(filepath.Join(root, "game", "video"), 0o755)
	os.WriteFile(filepath.Join(root, "game", "video", "plan.json"), []byte("{}"), 0o644)
	alias := filepath.Join(parent, "alias")
	outside := filepath.Join(parent, "outside")
	os.MkdirAll(outside, 0o755)
	os.WriteFile(filepath.Join(outside, "secret.json"), []byte("{}"), 0o644)
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "game", "video", "plan.json")
	if got, rel, err := resolveSessionPath(root, filepath.Join(alias, "game"), "video/plan.json", nil); err != nil || got != want || rel != "game/video/plan.json" {
		t.Fatalf("base through alias: %q %q %v", got, rel, err)
	}
	if got, _, err := resolveSessionPath(root, "", filepath.Join(alias, "game", "video", "plan.json"), nil); err != nil || got != want {
		t.Fatalf("absolute through alias: %q %v", got, err)
	}
	if _, _, err := resolveSessionPath(root, "", filepath.Join(outside, "secret.json"), nil); err == nil {
		t.Fatal("outside path resolved")
	}
	os.Symlink(outside, filepath.Join(parent, "sneaky"))
	if _, _, err := resolveSessionPath(root, filepath.Join(parent, "sneaky"), "secret.json", nil); err == nil {
		t.Fatal("base outside resolved")
	}
}
