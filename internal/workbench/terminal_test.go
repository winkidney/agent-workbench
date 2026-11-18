package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// termServer is a workbench on a real port (WebSockets need one) with one
// session, its shell kept plain: no user profile.
func termServer(t *testing.T) (*Manager, *Server, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if goruntime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	m, _ := fixture(t)
	session, err := m.Create("codex", t.TempDir(), "term")
	if err != nil {
		t.Fatal(err)
	}
	s := pairedServer(m, nil, "agent.localhost:17880", "pair")
	hs := httptest.NewServer(s)
	t.Cleanup(func() {
		m.endTerminals("", "test over")
		hs.Close()
	})
	return m, s, hs.Listener.Addr().String(), session.ID
}

// call makes a request the way a page on host does; remote: through a
// relay, as another device would.
func call(t *testing.T, host, method, path, body string, remote bool, cookie *http.Cookie) (*http.Response, []byte) {
	t.Helper()
	r, _ := http.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	r.Header.Set("Origin", "http://"+host)
	if remote {
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func openTerm(t *testing.T, host, session string, remote bool, cookie *http.Cookie) TerminalInfo {
	t.Helper()
	res, b := call(t, host, "POST", "/api/sessions/"+session+"/terminals", "", remote, cookie)
	if res.StatusCode != 200 {
		t.Fatalf("open: %d %s", res.StatusCode, b)
	}
	var info TerminalInfo
	json.Unmarshal(b, &info)
	return info
}

func dial(t *testing.T, host, id, origin string, remote bool, cookie *http.Cookie) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	h := http.Header{}
	h.Set("Origin", origin)
	if remote {
		h.Set("X-Forwarded-For", "203.0.113.9")
	}
	if cookie != nil {
		h.Set("Cookie", cookie.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return websocket.Dial(ctx, "ws://"+host+"/api/terminals/"+id+"/socket", &websocket.DialOptions{HTTPHeader: h})
}

// screen reads a socket until want matches what it showed, or a control
// message arrives; it returns both.
func screen(t *testing.T, c *websocket.Conn, want *regexp.Regexp) (string, map[string]any) {
	t.Helper()
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read (%v); so far: %q", err, out.String())
		}
		if typ == websocket.MessageText {
			var v map[string]any
			json.Unmarshal(data, &v)
			return out.String(), v
		}
		out.Write(data)
		if want != nil && want.Match(out.Bytes()) {
			return out.String(), nil
		}
	}
}

func send(t *testing.T, c *websocket.Conn, text string) {
	t.Helper()
	if err := c.Write(context.Background(), websocket.MessageText, []byte(text)); err != nil {
		t.Fatal(err)
	}
}

// A terminal runs a shell in the session's directory: keys in, output out,
// its screen again on reconnect, its status when the shell exits; then it
// is remembered only as that status until its tab is closed.
func TestTerminalShellRoundTrip(t *testing.T) {
	m, _, host, session := termServer(t)
	info := openTerm(t, host, session, false, nil)
	if !info.Running || info.Title != "Terminal 1" {
		t.Fatalf("info: %+v", info)
	}
	c, _, err := dial(t, host, info.ID, "http://"+host, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	send(t, c, "echo mark$((6*7))er\r")
	if goruntime.GOOS == "windows" {
		send(t, c, "echo mark42er\r")
	}
	screen(t, c, regexp.MustCompile(`mark42er`))
	if goruntime.GOOS != "windows" {
		if res, b := call(t, host, "POST", "/api/terminals/"+info.ID+"/resize", `{"cols":101,"rows":31}`, false, nil); res.StatusCode != 200 {
			t.Fatalf("resize: %s", b)
		}
		send(t, c, "stty size\r")
		screen(t, c, regexp.MustCompile(`31 101`))
	}
	c.Close(websocket.StatusNormalClosure, "")

	// Back again: what it showed comes first.
	c, _, err = dial(t, host, info.ID, "http://"+host, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shown, _ := screen(t, c, regexp.MustCompile(`mark42er`)); !strings.Contains(shown, "mark42er") {
		t.Fatalf("history: %q", shown)
	}
	send(t, c, "exit 3\r")
	_, ctl := screen(t, c, nil)
	if ctl["exit"] != float64(3) {
		t.Fatalf("exit: %v", ctl)
	}
	running, code := m.terminalState(session, info.ID)
	if running || code == nil || *code != 3 {
		t.Fatalf("state: %v %v", running, code)
	}
	tabs, err := m.SetPreviewTabs(session, PreviewTabs{Tabs: []PreviewTab{{Kind: "terminal", Terminal: info.ID}}, Active: 0})
	if err != nil || tabs.Tabs[0].Code == nil || *tabs.Tabs[0].Code != 3 {
		t.Fatalf("tab: %+v %v", tabs, err)
	}
	if res, _ := call(t, host, "DELETE", "/api/terminals/"+info.ID, "", false, nil); res.StatusCode != 200 {
		t.Fatalf("forget: %d", res.StatusCode)
	}
	if tabs := m.PreviewTabsOf(session); !tabs.Tabs[0].Missing {
		t.Fatalf("forgotten: %+v", tabs.Tabs[0])
	}
	// What the terminal showed was never stored.
	var n int
	m.store.db.QueryRow(`SELECT (SELECT count(*) FROM events WHERE data LIKE '%mark42er%') + (SELECT count(*) FROM terminal_audit WHERE action LIKE '%mark%' OR device LIKE '%mark%')`).Scan(&n)
	if n != 0 {
		t.Fatalf("output stored: %d rows", n)
	}
	audit, _ := m.TerminalAudit(10)
	actions := []string{}
	for _, e := range audit {
		actions = append(actions, e.Action+"/"+e.Device)
	}
	if strings.Join(actions, ",") != "exited (3)/workbench,opened/this computer" {
		t.Fatalf("audit: %v", actions)
	}
}

// Other sites cannot open a shell through the user's browser, and other
// devices need their sign-in; a socket ends when its sign-in does.
func TestTerminalSocketSecurity(t *testing.T) {
	m, _, host, session := termServer(t)
	info := openTerm(t, host, session, false, nil)
	if _, res, err := dial(t, host, info.ID, "http://evil.example", false, nil); err == nil || res == nil || res.StatusCode != 403 {
		t.Fatalf("foreign origin: %v %v", err, res)
	}
	if _, res, err := dial(t, host, info.ID, "http://"+host, true, nil); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatalf("stranger: %v %v", err, res)
	}
	if res, _ := call(t, host, "POST", "/api/sessions/"+session+"/terminals", "", true, nil); res.StatusCode != 401 {
		t.Fatalf("stranger opens: %d", res.StatusCode)
	}
	res, _ := call(t, host, "POST", "/api/login", `{"code":"pair"}`, true, nil)
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no sign-in")
	}
	c, _, err := dial(t, host, info.ID, "http://"+host, true, cookie)
	if err != nil {
		t.Fatal(err)
	}
	// Recorded as another device, by a mark that cannot sign in.
	openTerm(t, host, session, true, cookie)
	audit, _ := m.TerminalAudit(1)
	if !regexp.MustCompile(`^another device · sign-in [0-9a-f]{6}$`).MatchString(audit[0].Device) || strings.Contains(audit[0].Device, cookie.Value) {
		t.Fatalf("device: %q", audit[0].Device)
	}
	// This computer signs every other device out.
	if res, b := call(t, host, "POST", "/api/settings/signout-others", "", false, nil); res.StatusCode != 200 {
		t.Fatalf("sign out: %s", b)
	}
	signedOut(t, c)
	// Signed out the moment the socket opens: not missed either.
	res, _ = call(t, host, "POST", "/api/login", `{"code":"pair"}`, true, nil)
	for _, k := range res.Cookies() {
		if k.Name == sessionCookie {
			cookie = k
		}
	}
	c, _, err = dial(t, host, info.ID, "http://"+host, true, cookie)
	if err != nil {
		t.Fatal(err)
	}
	call(t, host, "POST", "/api/settings/signout-others", "", false, nil)
	signedOut(t, c)
}

// signedOut waits for the socket to end as signed out, within 5 s (well
// before the 30 s backstop).
func signedOut(t *testing.T, c *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := c.Read(ctx); err != nil {
			if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Fatalf("closed with %v", err)
			}
			return
		}
	}
}

// Closing a terminal ends what runs in it, and leaves nothing behind:
// goroutines and open files return to what they were.
func TestTerminalCloseFreesEverything(t *testing.T) {
	posixOnly(t)
	m, _, host, session := termServer(t)
	fds := func() int {
		list, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			return -1
		}
		return len(list)
	}
	// Warm up the client and server connection pools.
	warm := openTerm(t, host, session, false, nil)
	call(t, host, "DELETE", "/api/terminals/"+warm.ID, "", false, nil)
	time.Sleep(100 * time.Millisecond)
	baseG, baseF := goruntime.NumGoroutine(), fds()

	info := openTerm(t, host, session, false, nil)
	c, _, err := dial(t, host, info.ID, "http://"+host, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	send(t, c, "sleep 300 & echo pid=$!\r")
	shown, _ := screen(t, c, regexp.MustCompile(`pid=\d+`))
	pid, _ := strconv.Atoi(regexp.MustCompile(`pid=(\d+)`).FindStringSubmatch(shown)[1])
	// One that ignores the hangup, as under nohup.
	send(t, c, "(trap '' HUP; exec sleep 301) & echo deaf=$!\r")
	shown, _ = screen(t, c, regexp.MustCompile(`deaf=\d+`))
	deaf, _ := strconv.Atoi(regexp.MustCompile(`deaf=(\d+)`).FindStringSubmatch(shown)[1])
	if !processAlive(pid) || !processAlive(deaf) {
		t.Fatalf("jobs not running: %d %d", pid, deaf)
	}
	if res, _ := call(t, host, "DELETE", "/api/terminals/"+info.ID, "", false, nil); res.StatusCode != 200 {
		t.Fatalf("close: %d", res.StatusCode)
	}
	for deadline := time.Now().Add(5 * time.Second); processAlive(pid) || processAlive(deaf); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("jobs outlived their terminal: %d %v, %d %v", pid, processAlive(pid), deaf, processAlive(deaf))
		}
	}
	c.CloseNow()

	for i := 0; i < 20; i++ {
		info := openTerm(t, host, session, false, nil)
		c, _, err := dial(t, host, info.ID, "http://"+host, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		send(t, c, "echo cycle\r")
		screen(t, c, regexp.MustCompile(`cycle\r?\n`))
		c.Close(websocket.StatusNormalClosure, "")
		call(t, host, "DELETE", "/api/terminals/"+info.ID, "", false, nil)
	}
	http.DefaultClient.CloseIdleConnections()
	var g, f int
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		g, f = goruntime.NumGoroutine(), fds()
		if g <= baseG+1 && f <= baseF {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("left behind: goroutines %d→%d, files %d→%d", baseG, g, baseF, f)
		}
	}
	m.terms.mu.Lock()
	live, ended := len(m.terms.live), len(m.terms.ended)
	m.terms.mu.Unlock()
	if live != 0 || ended != 0 {
		t.Fatalf("kept: %d live, %d ended", live, ended)
	}
}

// fakeShell stands in for a shell where only the bookkeeping is tested.
type fakeShell struct {
	once sync.Once
	done chan struct{}
	in   chan []byte
}

func newFakeShell() *fakeShell { return &fakeShell{done: make(chan struct{}), in: make(chan []byte)} }

func (f *fakeShell) Read(p []byte) (int, error) {
	<-f.done
	return 0, io.EOF
}
func (f *fakeShell) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakeShell) Resize(int, int) error       { return nil }
func (f *fakeShell) Done() <-chan struct{}       { return f.done }
func (f *fakeShell) Code() int                   { return 0 }
func (f *fakeShell) Kill()                       { f.once.Do(func() { close(f.done) }) }
func (f *fakeShell) Close() error                { return nil }

// The history keeps the last 1 MiB however much a terminal prints, and a
// page that stops reading is dropped rather than buffered for.
func TestTerminalHistoryAndSlowPagesAreBounded(t *testing.T) {
	term := &terminal{id: "x", shell: newFakeShell(), socks: map[*termSock]struct{}{}}
	sock, _, _ := term.attach()
	chunk := bytes.Repeat([]byte("y"), 32<<10)
	for i := 0; i < 160; i++ { // 5 MiB
		term.publish(chunk)
	}
	if term.size > termHistoryCap || term.size < termHistoryCap-len(chunk) {
		t.Fatalf("history %d bytes", term.size)
	}
	select {
	case <-sock.gone:
	default:
		t.Fatal("a stalled page kept its queue")
	}
	if len(term.socks) != 0 || len(sock.out) != termSendQueue {
		t.Fatalf("socks %d, queued %d", len(term.socks), len(sock.out))
	}
}

// A session holds at most eight terminals; tabs keep terminals besides the
// six most recent files and services, and an unknown terminal shows gone.
func TestTerminalCapsAndTabs(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("codex", t.TempDir(), "caps")
	m.terms.live = map[string]*terminal{}
	m.terms.ended = map[string]termEnded{}
	for i := 0; i < termPerSession; i++ {
		id := strconv.Itoa(i)
		m.terms.live[id] = &terminal{id: id, session: s.ID, title: "Terminal " + id, shell: newFakeShell(), socks: map[*termSock]struct{}{}}
	}
	if _, err := m.OpenTerminal(s.ID, ""); err == nil || !strings.Contains(err.Error(), "8 terminals") {
		t.Fatalf("cap: %v", err)
	}
	tabs := []PreviewTab{{Kind: "terminal", Terminal: strings.Repeat("a", 48)}}
	for i := 0; i < 7; i++ {
		tabs = append(tabs, PreviewTab{Kind: "file", Path: "f" + strconv.Itoa(i) + ".txt"})
	}
	v, err := m.SetPreviewTabs(s.ID, PreviewTabs{Tabs: tabs, Active: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Tabs) != 7 || v.Tabs[0].Kind != "terminal" || !v.Tabs[0].Missing || v.Tabs[6].Path != "f5.txt" {
		t.Fatalf("tabs: %+v", v.Tabs)
	}
	if _, err := m.SetPreviewTabs(s.ID, PreviewTabs{Tabs: []PreviewTab{{Kind: "terminal", Terminal: "../x"}}}); err == nil {
		t.Fatal("a bad terminal id was kept")
	}
	m.endTerminals(s.ID, "test over")
	if len(m.terms.live) != 0 {
		t.Fatal("session terminals kept")
	}
}

// The open/close record keeps a week.
func TestTerminalAuditKeepsAWeek(t *testing.T) {
	m, _ := fixture(t)
	old := time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	m.store.db.Exec(`INSERT INTO terminal_audit VALUES(?,?,?,?,?,?)`, old, "s", "t", "Terminal 1", "opened", "this computer")
	m.auditTerminal(&terminal{id: "t2", session: "s", title: "Terminal 2"}, "opened", "")
	if err := m.pruneTerminalAudit(time.Now().Add(-eventRetention)); err != nil {
		t.Fatal(err)
	}
	list, _ := m.TerminalAudit(10)
	if len(list) != 1 || list[0].Terminal != "t2" {
		t.Fatalf("audit: %+v", list)
	}
}
