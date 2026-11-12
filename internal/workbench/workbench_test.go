package workbench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

type fakeProcess struct {
	mu      sync.Mutex
	in      []map[string]any
	stopped bool
}

func (p *fakeProcess) Input(v any) error {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.in = append(p.in, m)
	return nil
}
func (p *fakeProcess) Stop() error { p.stopped = true; return nil }
func fixture(t *testing.T) (*Manager, *fakeProcess) {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	p := &fakeProcess{}
	m, err := NewManager(s, func(_ Session, _ func(map[string]any), _ func(error)) (Process, error) { return p, nil }, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	return m, p
}
func TestLifecycle100Cycles(t *testing.T) {
	m, p := fixture(t)
	s, err := m.Create("codex", t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := m.Start(s.ID); err != nil {
			t.Fatal(err)
		}
		r := m.sessions[s.ID]
		gen := r.generation
		m.receive(s.ID, gen, map[string]any{"id": "init", "result": map[string]any{}})
		m.receive(s.ID, gen, map[string]any{"id": "thread", "result": map[string]any{"thread": map[string]any{"id": "native"}}})
		if err := m.Send(s.ID, "hello"); err != nil {
			t.Fatal(err)
		}
		if err := m.Stop(s.ID, true); err == nil {
			t.Fatal("slept with active task")
		}
		m.receive(s.ID, gen, map[string]any{"method": "turn/completed"})
		r.last = time.Now().Add(-time.Second)
		m.Sweep()
		if r.proc != nil || r.session.Status != "sleeping" || !p.stopped {
			t.Fatal("did not reclaim idle engine")
		}
		m.exited(s.ID, gen-1, fmt.Errorf("stale"))
		if r.session.Status != "sleeping" {
			t.Fatal("stale process changed state")
		}
	}
	if m.sessions[s.ID].session.NativeID != "native" {
		t.Fatal("resume identity lost")
	}
}
func TestApprovals(t *testing.T) {
	for _, agent := range []string{"codex", "claude"} {
		t.Run(agent, func(t *testing.T) {
			m, p := fixture(t)
			s, _ := m.Create(agent, t.TempDir(), agent)
			_ = m.Start(s.ID)
			r := m.sessions[s.ID]
			var request map[string]any
			if agent == "codex" {
				request = map[string]any{"id": 12, "method": "item/commandExecution/requestApproval", "params": map[string]any{"command": "touch test"}}
			} else {
				request = map[string]any{"type": "control_request", "request_id": "permission", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": "touch test"}}}
			}
			m.receive(s.ID, r.generation, request)
			if err := m.Stop(s.ID, true); err == nil {
				t.Fatal("slept with approval")
			}
			key := "12"
			if agent == "claude" {
				key = "permission"
			}
			if err := m.Reply(s.ID, key, false, nil); err != nil {
				t.Fatal(err)
			}
			if len(r.pending) != 0 || len(p.in) < 2 {
				t.Fatal("approval not resolved")
			}
			if err := m.Reply(s.ID, key, true, nil); err == nil {
				t.Fatal("accepted stale approval")
			}
		})
	}
}
func request(t *testing.T, s *Server, host, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	r.Host = host
	// A browser on this machine, unless a test says otherwise.
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Origin", "http://"+s.Host)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestSecurityPreviewRange(t *testing.T) {
	posixOnly(t)
	m, _ := fixture(t)
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "clip.mp4"), []byte("0123456789"), 0600)
	outside := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(outside, []byte("secret"), 0600)
	_ = os.Symlink(outside, filepath.Join(root, "escape"))
	session, _ := m.Create("claude", root, "preview")
	s := pairedServer(m, fstest.MapFS{"index.html": {Data: []byte("app")}}, "agent.localhost:17880", "pair")
	if w := lan(t, s, "GET", "/api/sessions", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(t, s, "evil.test:17880", "GET", "/api/sessions", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	login := request(t, s, s.Host, "POST", "/api/login", `{"code":"pair"}`, nil)
	if login.Header().Get("Referrer-Policy") != "strict-origin" {
		t.Fatal("cross-origin preview forms need Origin preserved")
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].Domain != "" {
		t.Fatal("bad auth cookie")
	}
	cookie := cookies[0]
	r := httptest.NewRequest("POST", "http://"+s.Host+"/api/sessions", bytes.NewBufferString(`{}`))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Host = s.Host
	r.Header.Set("Origin", "http://evil.localhost:17880")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	if _, err := confined(root, "escape"); err == nil {
		t.Fatal("symlink escaped")
	}
	if _, err := confined(root, "../secret"); err == nil {
		t.Fatal("traversal accepted")
	}
	// A working directory reached through a link still holds its files.
	linked := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(root, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := confined(linked, "clip.mp4"); err != nil {
		t.Fatalf("linked working directory: %v", err)
	}
	if _, err := confined(linked, "escape"); err == nil {
		t.Fatal("symlink escaped through a linked working directory")
	}
	payload := fmt.Sprintf(`{"session":%q,"kind":"file","path":"clip.mp4"}`, session.ID)
	w = request(t, s, s.Host, "POST", "/api/previews", payload, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var result struct {
		Preview Preview `json:"preview"`
		Grant   string  `json:"grant"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	host := strings.TrimPrefix(result.Preview.URL, "http://")
	if w = request(t, s, host, "GET", "/clip.mp4", "", cookie); w.Code != 401 {
		t.Fatal("control cookie authorized preview")
	}
	auth := httptest.NewRequest("POST", result.Preview.URL+"/__authorize", strings.NewReader("grant="+result.Grant))
	auth.Host = host
	auth.Header.Set("Origin", "http://"+s.Host)
	auth.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, auth)
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	pc := w.Result().Cookies()[0]
	r = httptest.NewRequest("GET", result.Preview.URL+"/clip.mp4", nil)
	r.Host = host
	r.AddCookie(pc)
	r.Header.Set("Range", "bytes=2-5")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "2345" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = lan(t, s, "GET", "/api/sessions", "", pc); w.Code != 401 {
		t.Fatal("preview cookie authorized API")
	}
	if err := m.Stop(session.ID, true); err != nil {
		t.Fatal("static preview pinned engine:", err)
	}
	if m.FindPreview(result.Preview.ID) == nil {
		t.Fatal("static preview lost on sleep")
	}
	if err := m.Stop(session.ID, false); err != nil {
		t.Fatal(err)
	}
	if m.FindPreview(result.Preview.ID) != nil {
		t.Fatal("preview not removed")
	}
}
func TestPersistenceAndPagination(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := Session{ID: "id", Agent: "codex", Cwd: dir, NativeID: "keep", Status: "running"}
	_ = s.Save(v)
	for i := 0; i < 205; i++ {
		_, _ = s.Append(v.ID, "delta", map[string]any{"text": "a"})
	}
	first, _ := s.Events(v.ID, 0, 200)
	second, _ := s.Events(v.ID, first[199].Seq, 200)
	if len(first) != 200 || len(second) != 5 {
		t.Fatal("pagination")
	}
	_ = s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, _ := s.List()
	if list[0].NativeID != "keep" || list[0].Status != "sleeping" {
		t.Fatal("recovery")
	}
}
func TestProxyCredentialIsolation(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials forwarded")
		}
		w.Write([]byte("upstream"))
	}))
	defer up.Close()
	m, _ := fixture(t)
	v, _ := m.Create("codex", t.TempDir(), "")
	p, err := m.CreatePreview(v.ID, "service", "", "", up.URL, "", 0, "agent.localhost:17880")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", p.URL+"/", nil)
	r.Header.Set("Cookie", "secret=value")
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	b, _ := io.ReadAll(w.Result().Body)
	if string(b) != "upstream" {
		t.Fatal(string(b))
	}
}

func TestWebSocketUpgrade(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		rw.Flush()
		line, _ := rw.ReadString('\n')
		rw.WriteString(line)
		rw.Flush()
	}))
	defer up.Close()
	m, _ := fixture(t)
	session, _ := m.Create("codex", t.TempDir(), "")
	p, err := m.CreatePreview(session.ID, "service", "", "", up.URL, "", 0, "agent.localhost:17880")
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(p)
	defer proxy.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprint(conn, "GET / HTTP/1.1\r\nHost: test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil || response.StatusCode != 101 {
		t.Fatalf("upgrade: %v %v", response, err)
	}
	fmt.Fprint(conn, "ping\n")
	line, err := reader.ReadString('\n')
	if err != nil || line != "ping\n" {
		t.Fatalf("tunnel: %q %v", line, err)
	}
}

func TestDelayedMCPStartup(t *testing.T) {
	m, _ := fixture(t)
	v, _ := m.Create("codex", t.TempDir(), "")
	_ = m.Start(v.ID)
	r := m.sessions[v.ID]
	m.receive(v.ID, r.generation, map[string]any{"method": "mcpServer/startupStatus/updated", "params": map[string]any{"name": "server", "status": "starting"}})
	if mcpSettled(r) {
		t.Fatal("MCP startup treated as settled")
	}
	m.receive(v.ID, r.generation, map[string]any{"method": "mcpServer/startupStatus/updated", "params": map[string]any{"name": "server", "status": "ready"}})
	if !mcpSettled(r) {
		t.Fatal("MCP completion not tracked")
	}
}

func pairedServer(m *Manager, assets fs.FS, host, pairing string) *Server {
	_, err := m.store.db.Exec("INSERT OR REPLACE INTO settings VALUES('pairing_required','true')")
	if err != nil {
		panic(err)
	}
	return NewServer(m, assets, host, pairing)
}
