package workbench

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// A running side chat says since when and what it waits on: the parent
// engine waking, then the answer. Both are gone once it is idle again.
func TestSideSaysWhatItWaitsOn(t *testing.T) {
	m, p, parent := sideFixture(t, "codex")
	m.mu.Lock()
	m.find(parent).ready = false
	m.mu.Unlock()
	sc, _ := m.OpenSide(parent)
	p.reply = func(v map[string]any) {
		method := str(v["method"])
		if method == "" {
			return
		}
		result := map[string]any{}
		switch method {
		case "thread/fork":
			result = map[string]any{"thread": map[string]any{"id": "side-native"}}
		case "turn/start":
			m.receive(parent, 1, map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "side-native", "turn": map[string]any{"id": "side-turn"}}})
		}
		m.receive(parent, 1, map[string]any{"id": v["id"], "result": result})
	}
	if _, err := m.SendSide(sc.ID, "question", "once"); err != nil {
		t.Fatal(err)
	}
	s, _ := m.Side(sc.ID)
	if s.Status != "running" || s.Phase != "waking" || s.Since == "" {
		t.Fatalf("waking: %+v", s)
	}
	m.mu.Lock()
	m.find(parent).ready = true
	m.mu.Unlock()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(2 * time.Millisecond) {
		if s, _ = m.Side(sc.ID); s.Phase == "answering" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("answering: %+v", s)
		}
	}
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(2 * time.Millisecond) {
		m.mu.Lock()
		turn := m.sides[sc.ID].turns["side-native"]
		m.mu.Unlock()
		if turn != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no side turn")
		}
	}
	m.receive(parent, 1, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "side-native", "turn": map[string]any{"id": "side-turn", "status": "completed"}}})
	awaitSide(t, m, sc.ID, "idle")
	if s, _ = m.Side(sc.ID); s.Since != "" || s.Phase != "" {
		t.Fatalf("idle keeps running fields: %+v", s)
	}
	m.CloseSide(sc.ID, true)
	awaitSide(t, m, sc.ID, "closed")
}

// The side stream sends a new event as soon as it is written, not on the
// next poll, and repeats the status only when it changed.
func TestSideStreamPushesChanges(t *testing.T) {
	m, _, parent := sideFixture(t, "claude")
	sc, _ := m.OpenSide(parent)
	s := NewServer(m, fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}, "agent.localhost:17880", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", "http://agent.localhost:17880/api/side-chats/x/events", nil).WithContext(ctx)
	r.Host, r.RemoteAddr = s.Host, "127.0.0.1:1234"
	pr, pw := io.Pipe()
	w := &pipeWriter{header: http.Header{}, w: pw}
	go func() {
		s.sideStream(w, r, sc.ID)
		pw.Close()
	}()
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	expect := func(what string, ok func(string) bool, within time.Duration) {
		t.Helper()
		timeout := time.After(within)
		for {
			select {
			case l := <-lines:
				if ok(l) {
					return
				}
			case <-timeout:
				t.Fatalf("no %s within %s", what, within)
			}
		}
	}
	expect("first status", func(l string) bool { return l == "event: status" }, time.Second)
	start := time.Now()
	m.sideEmit(sc.ID, "notice", map[string]any{"text": "pushed"})
	expect("the new event", func(l string) bool { return strings.Contains(l, "pushed") }, time.Second)
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("event took %s", d)
	}
	// Unrelated changes wake the stream but repeat no status.
	m.changes.Broadcast()
	m.changes.Broadcast()
	select {
	case l := <-lines:
		if l == "event: status" {
			t.Fatal("unchanged status sent again")
		}
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	m.CloseSide(sc.ID, true)
}

type pipeWriter struct {
	header http.Header
	w      *io.PipeWriter
}

func (p *pipeWriter) Header() http.Header         { return p.header }
func (p *pipeWriter) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeWriter) WriteHeader(int)             {}
func (p *pipeWriter) Flush()                      {}
