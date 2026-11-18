package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Terminal tabs (docs/adr/0006): a shell on a pseudo-terminal in a session's
// directory, shown in the preview panel. A terminal lives until it is closed,
// its shell exits, its session is deleted or the workbench stops; hiding or
// reloading the page never ends it. What it holds is taken as needed (its
// output history grows with output, to 1 MiB) and all of it is let go when it
// ends. Its output stays in memory only: never stored, logged or sent
// anywhere but to the pages attached to it.

const (
	termHistoryCap = 1 << 20
	termPerSession = 8
	termTotal      = 32
	termInputMax   = 64 << 10
	// Chunks (≤ 32 KiB each) a socket may have waiting before it is dropped
	// as stalled; the page reconnects and gets the history again.
	termSendQueue = 64
	// Ended terminals remembered for their tabs ("Exited (N)").
	termEndedKeep = 64
)

// shellPty is a shell on a pseudo-terminal: creack/pty on Unix, ConPTY on
// Windows (terminal_unix.go, terminal_windows.go).
type shellPty interface {
	io.Reader
	io.Writer
	Resize(cols, rows int) error
	// Done is closed when the shell has exited; Code is then its status.
	Done() <-chan struct{}
	Code() int
	// Kill ends the shell and everything it started, and waits for it.
	Kill()
	// Close frees the pseudo-terminal.
	Close() error
}

// TerminalInfo is what the page is told of a terminal.
type TerminalInfo struct {
	ID      string `json:"id"`
	Session string `json:"session"`
	Title   string `json:"title"`
	Running bool   `json:"running"`
	Code    *int   `json:"code,omitempty"`
}

type terminal struct {
	id, session, title string
	shell              shellPty

	mu      sync.Mutex
	history [][]byte
	size    int
	socks   map[*termSock]struct{}
	ended   bool
}

// termSock is one page attached to a terminal.
type termSock struct {
	out  chan []byte
	exit chan int
	gone chan struct{}
	once sync.Once
}

func (s *termSock) drop() { s.once.Do(func() { close(s.gone) }) }

type termEnded struct {
	session, title string
	code           int
}

// terminals are the workbench's terminals; the maps are made on first use.
type terminals struct {
	mu    sync.Mutex
	live  map[string]*terminal
	ended map[string]termEnded
	order []string
}

// terminalEnv is the workbench's environment for a shell: its own service
// variables left out, a capable terminal named.
func terminalEnv() []string {
	out := []string{}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "INVOCATION_ID", "SYSTEMD_EXEC_PID", "JOURNAL_STREAM", "NOTIFY_SOCKET", "LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES", "TERM", "COLORTERM":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "TERM=xterm-256color", "COLORTERM=truecolor")
}

// OpenTerminal starts a shell in a session's directory.
func (m *Manager) OpenTerminal(session, device string) (TerminalInfo, error) {
	dir := m.sessionDir(session)
	if dir == "" {
		return TerminalInfo{}, errors.New("session not found")
	}
	ts := &m.terms
	ts.mu.Lock()
	if ts.live == nil {
		ts.live = map[string]*terminal{}
		ts.ended = map[string]termEnded{}
	}
	used := map[string]bool{}
	mine := 0
	for _, t := range ts.live {
		if t.session == session {
			mine++
			used[t.title] = true
		}
	}
	for _, e := range ts.ended {
		if e.session == session {
			used[e.title] = true
		}
	}
	// Tabs of terminals that ended with an earlier run keep their names.
	if v, ok := m.store.previewTabs(session); ok {
		for _, t := range v.Tabs {
			if t.Kind == "terminal" {
				used[t.Title] = true
			}
		}
	}
	if mine >= termPerSession {
		ts.mu.Unlock()
		return TerminalInfo{}, fmt.Errorf("a session can have %d terminals open; close one first", termPerSession)
	}
	if len(ts.live) >= termTotal {
		ts.mu.Unlock()
		return TerminalInfo{}, fmt.Errorf("the workbench can have %d terminals open; close one first", termTotal)
	}
	title := ""
	for n := 1; ; n++ {
		if title = "Terminal " + strconv.Itoa(n); !used[title] {
			break
		}
	}
	// Reserved while the shell starts, so two creates count each other.
	t := &terminal{id: token(), session: session, title: title, socks: map[*termSock]struct{}{}}
	ts.live[t.id] = t
	ts.mu.Unlock()
	shell, err := startShell(defaultShell(), dir, terminalEnv(), 80, 24)
	if err != nil {
		ts.mu.Lock()
		delete(ts.live, t.id)
		ts.mu.Unlock()
		return TerminalInfo{}, fmt.Errorf("start a shell: %w", err)
	}
	ts.mu.Lock()
	t.shell = shell
	ts.mu.Unlock()
	m.auditTerminal(t, "opened", device)
	go m.pumpTerminal(t)
	return TerminalInfo{ID: t.id, Session: session, Title: title, Running: true}, nil
}

// pumpTerminal reads the shell's output until it ends. The pseudo-terminal
// must always be drained, or the shell stalls on a full buffer.
func (m *Manager) pumpTerminal(t *terminal) {
	read := make(chan struct{})
	go func() {
		defer close(read)
		buf := make([]byte, 32<<10)
		for {
			n, err := t.shell.Read(buf)
			if n > 0 {
				t.publish(append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-read:
	case <-t.shell.Done():
		// What the shell wrote last is still on its way.
		select {
		case <-read:
		case <-time.After(300 * time.Millisecond):
		}
	}
	m.endTerminal(t, "exited", "")
	<-read
}

// publish keeps output in the history and hands it to attached pages.
func (t *terminal) publish(chunk []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ended {
		return
	}
	t.history = append(t.history, chunk)
	t.size += len(chunk)
	for t.size > termHistoryCap && len(t.history) > 1 {
		t.size -= len(t.history[0])
		t.history[0] = nil
		t.history = t.history[1:]
	}
	for s := range t.socks {
		select {
		case s.out <- chunk:
		default:
			// A page that does not keep up is dropped, not buffered for.
			delete(t.socks, s)
			s.drop()
		}
	}
}

// endTerminal ends a terminal and lets go of everything it holds. Ended by
// its shell, it is remembered (title, status) until its tab is closed.
func (m *Manager) endTerminal(t *terminal, how, device string) {
	t.mu.Lock()
	if t.ended {
		t.mu.Unlock()
		return
	}
	t.ended = true
	socks := t.socks
	t.socks, t.history, t.size = nil, nil, 0
	t.mu.Unlock()
	t.shell.Kill()
	_ = t.shell.Close()
	code := t.shell.Code()
	for s := range socks {
		s.exit <- code
	}
	ts := &m.terms
	ts.mu.Lock()
	delete(ts.live, t.id)
	if how == "exited" {
		ts.ended[t.id] = termEnded{session: t.session, title: t.title, code: code}
		ts.order = append(ts.order, t.id)
		for len(ts.order) > termEndedKeep {
			delete(ts.ended, ts.order[0])
			ts.order = ts.order[1:]
		}
	}
	ts.mu.Unlock()
	if how == "exited" {
		how = fmt.Sprintf("exited (%d)", code)
	}
	m.auditTerminal(t, how, device)
}

func (m *Manager) findTerminal(id string) (*terminal, *termEnded) {
	ts := &m.terms
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if t := ts.live[id]; t != nil && t.shell != nil {
		return t, nil
	}
	if e, ok := ts.ended[id]; ok {
		return nil, &e
	}
	return nil, nil
}

// CloseTerminal ends a terminal at the page's request, or forgets one that
// had ended.
func (m *Manager) CloseTerminal(id, device string) error {
	t, e := m.findTerminal(id)
	if t != nil {
		m.endTerminal(t, "closed", device)
		return nil
	}
	if e != nil {
		ts := &m.terms
		ts.mu.Lock()
		delete(ts.ended, id)
		ts.order = slices.DeleteFunc(ts.order, func(k string) bool { return k == id })
		ts.mu.Unlock()
		return nil
	}
	return errors.New("terminal not found")
}

// endTerminals ends every terminal of a session ("" for all), and forgets
// the ended ones too.
func (m *Manager) endTerminals(session, how string) {
	ts := &m.terms
	ts.mu.Lock()
	list := []*terminal{}
	for _, t := range ts.live {
		if (session == "" || t.session == session) && t.shell != nil {
			list = append(list, t)
		}
	}
	for id, e := range ts.ended {
		if session == "" || e.session == session {
			delete(ts.ended, id)
		}
	}
	ts.order = slices.DeleteFunc(ts.order, func(k string) bool { _, ok := ts.ended[k]; return !ok })
	ts.mu.Unlock()
	var wg sync.WaitGroup
	for _, t := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.endTerminal(t, how, "")
		}()
	}
	wg.Wait()
}

// Terminals lists a session's terminals, running and ended.
func (m *Manager) Terminals(session string) []TerminalInfo {
	ts := &m.terms
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := []TerminalInfo{}
	for id, t := range ts.live {
		if t.session == session && t.shell != nil {
			out = append(out, TerminalInfo{ID: id, Session: session, Title: t.title, Running: true})
		}
	}
	for id, e := range ts.ended {
		if e.session == session {
			code := e.code
			out = append(out, TerminalInfo{ID: id, Session: session, Title: e.title, Code: &code})
		}
	}
	slices.SortFunc(out, func(a, b TerminalInfo) int { return strings.Compare(a.Title, b.Title) })
	return out
}

// terminalState is what a terminal tab finds: running, ended with a status,
// or gone (the workbench restarted since).
func (m *Manager) terminalState(session, id string) (running bool, code *int) {
	t, e := m.findTerminal(id)
	if t != nil && t.session == session {
		return true, nil
	}
	if e != nil && e.session == session {
		c := e.code
		return false, &c
	}
	return false, nil
}

func (m *Manager) ResizeTerminal(id string, cols, rows int) error {
	if cols < 2 || cols > 1000 || rows < 1 || rows > 500 {
		return errors.New("a terminal is 2–1000 columns by 1–500 rows")
	}
	t, _ := m.findTerminal(id)
	if t == nil {
		return errors.New("terminal not found")
	}
	return t.shell.Resize(cols, rows)
}

// attach registers a page and returns what the terminal has shown so far;
// both under one lock, so nothing is missed or repeated.
func (t *terminal) attach() (*termSock, []byte, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ended {
		return nil, nil, false
	}
	s := &termSock{out: make(chan []byte, termSendQueue), exit: make(chan int, 1), gone: make(chan struct{})}
	t.socks[s] = struct{}{}
	snapshot := make([]byte, 0, t.size)
	for _, c := range t.history {
		snapshot = append(snapshot, c...)
	}
	return s, snapshot, true
}

func (t *terminal) detach(s *termSock) {
	t.mu.Lock()
	delete(t.socks, s)
	t.mu.Unlock()
}

// Audit: who opened and ended terminals, never what they showed or were
// sent. Kept for the retention like the rest (retention.go).
func (m *Manager) auditTerminal(t *terminal, action, device string) {
	if device == "" {
		device = "workbench"
	}
	if _, err := m.store.db.Exec(`INSERT INTO terminal_audit(time,session,terminal,title,action,device) VALUES(?,?,?,?,?,?)`, nowStamp(), t.session, t.id, t.title, action, device); err != nil {
		fmt.Fprintln(os.Stderr, "terminal audit:", err)
	}
}

func (m *Manager) pruneTerminalAudit(before time.Time) error {
	_, err := m.store.db.Exec(`DELETE FROM terminal_audit WHERE time < ?`, before.UTC().Format(time.RFC3339Nano))
	return err
}

// TerminalAuditEntry is one line of the record, newest first.
type TerminalAuditEntry struct {
	Time     string `json:"time"`
	Session  string `json:"session"`
	Terminal string `json:"terminal"`
	Title    string `json:"title"`
	Action   string `json:"action"`
	Device   string `json:"device"`
}

func (m *Manager) TerminalAudit(limit int) ([]TerminalAuditEntry, error) {
	rows, err := m.store.db.Query(`SELECT time,session,terminal,title,action,device FROM terminal_audit ORDER BY time DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TerminalAuditEntry{}
	for rows.Next() {
		var e TerminalAuditEntry
		if err := rows.Scan(&e.Time, &e.Session, &e.Terminal, &e.Title, &e.Action, &e.Device); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// deviceLabel names who acted, without anything that would sign in: this
// computer, or another device with a short one-way mark of its sign-in.
func deviceLabel(r *http.Request, access hostAccess) string {
	if access == hostLocal {
		return "this computer"
	}
	label := "another device"
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		sum := sha256.Sum256([]byte(c.Value))
		label += " · sign-in " + hex.EncodeToString(sum[:3])
	}
	return label
}

// terminalAPI answers sessions/{id}/terminals and terminals/{tid}[/resize].
func (s *Server) terminalAPI(w http.ResponseWriter, r *http.Request, path string, access hostAccess) {
	m := s.Manager
	parts := strings.Split(path, "/")
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case parts[0] == "sessions" && len(parts) == 3:
		id := parts[1]
		if _, ok := m.catalog.Find(id); !ok && m.findUnlocked(id) == nil {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case "GET":
			writeJSON(w, m.Terminals(id))
		case "POST":
			info, err := m.OpenTerminal(id, deviceLabel(r, access))
			if err != nil {
				bad(w, err)
				return
			}
			writeJSON(w, info)
		default:
			http.Error(w, "method not allowed", 405)
		}
	case parts[0] == "terminals" && len(parts) == 2 && r.Method == "DELETE":
		if err := m.CloseTerminal(parts[1], deviceLabel(r, access)); err != nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case parts[0] == "terminals" && len(parts) == 3 && parts[2] == "resize" && r.Method == "POST":
		var v struct{ Cols, Rows int }
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		if err := m.ResizeTerminal(parts[1], v.Cols, v.Rows); err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

// terminalSocket attaches a page to a terminal over a WebSocket: what the
// terminal has shown first, then its output as it comes (binary frames);
// the page's keys go to the shell. Browsers send cookies with cross-site
// WebSocket requests, so the page's origin is checked like a change is;
// and the socket ends as soon as the sign-in that opened it does.
func (s *Server) terminalSocket(w http.ResponseWriter, r *http.Request, access hostAccess) {
	if !s.originAllowed(r, access) {
		http.Error(w, originRefusal(r, access), http.StatusForbidden)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/terminals/"), "/socket")
	// Watched from before the upgrade: a sign-in that ends while the socket
	// is set up is not missed.
	changed := s.authChanged.C()
	req := r.Clone(context.Background())
	// The origin was checked above, by the workbench's own rules.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(termInputMax)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	control := func(v any) {
		b, _ := json.Marshal(v)
		_ = c.Write(ctx, websocket.MessageText, b)
	}
	t, e := s.Manager.findTerminal(id)
	if t == nil {
		if e != nil {
			control(map[string]any{"exit": e.code})
		} else {
			control(map[string]any{"missing": true})
		}
		c.Close(websocket.StatusNormalClosure, "")
		return
	}
	sock, snapshot, ok := t.attach()
	if !ok {
		control(map[string]any{"missing": true})
		c.Close(websocket.StatusNormalClosure, "")
		return
	}
	defer t.detach(sock)
	if len(snapshot) > 0 {
		if err := c.Write(ctx, websocket.MessageBinary, snapshot); err != nil {
			return
		}
	}
	snapshot = nil
	// Keys from the page.
	go func() {
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if _, err := t.shell.Write(data); err != nil {
				return
			}
		}
	}()
	check := time.NewTicker(30 * time.Second)
	defer check.Stop()
	if !s.authorized(req) {
		c.Close(websocket.StatusPolicyViolation, "signed out")
		return
	}
	for {
		select {
		case b := <-sock.out:
			if err := c.Write(ctx, websocket.MessageBinary, b); err != nil {
				return
			}
		case code := <-sock.exit:
			control(map[string]any{"exit": code})
			c.Close(websocket.StatusNormalClosure, "")
			return
		case <-sock.gone:
			c.Close(websocket.StatusTryAgainLater, "fell behind")
			return
		case <-changed:
			changed = s.authChanged.C()
			if !s.authorized(req) {
				c.Close(websocket.StatusPolicyViolation, "signed out")
				return
			}
		case <-check.C:
			if !s.authorized(req) {
				c.Close(websocket.StatusPolicyViolation, "signed out")
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
