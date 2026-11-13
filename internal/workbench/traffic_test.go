package workbench

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func listOf(n int) []Session {
	list := make([]Session, n)
	for i := range list {
		list[i] = Session{ID: fmt.Sprintf("claude:%04d", i), Agent: "claude", Title: fmt.Sprintf("session %d", i), Updated: fmt.Sprintf("2026-11-13T00:%02d:00Z", 59-i%60), Status: "sleeping"}
	}
	return list
}

func ids(raws []json.RawMessage) []string {
	out := []string{}
	for _, r := range raws {
		var s Session
		_ = json.Unmarshal(r, &s)
		out = append(out, s.ID)
	}
	return out
}

func TestSessionListSendsWhatChanged(t *testing.T) {
	var l sessionLists
	list := listOf(5)
	first, err := l.delta(list, "")
	if err != nil || !first.Full || len(first.Changed) != 5 || first.Token == "" {
		t.Fatalf("first: %+v %v", first, err)
	}
	// Nothing changed: nothing but the token.
	same, _ := l.delta(list, first.Token)
	if same.Full || len(same.Changed) != 0 || len(same.Removed) != 0 || same.Token != first.Token {
		t.Fatalf("unchanged: %+v", same)
	}
	if same.Count != 5 {
		t.Fatalf("count: %d", same.Count)
	}
	if b, _ := json.Marshal(same); len(b) > 50 {
		t.Fatalf("unchanged answer is %d bytes: %s", len(b), b)
	}
	// One session moved on, one gone, one new.
	next := append([]Session{}, list...)
	next[1].Updated, next[1].Status = "2026-11-13T01:00:00Z", "running"
	next = append(next[:3], next[4:]...)
	next = append(next, Session{ID: "codex:new", Agent: "codex", Updated: "2026-11-13T02:00:00Z"})
	d, _ := l.delta(next, first.Token)
	if d.Full || d.Token == first.Token || strings.Join(ids(d.Changed), ",") != "claude:0001,codex:new" || strings.Join(d.Removed, ",") != "claude:0003" {
		t.Fatalf("delta: full %v changed %v removed %v", d.Full, ids(d.Changed), d.Removed)
	}
	// The same list has the same token, whoever asks.
	var other sessionLists
	if o, _ := other.delta(next, ""); o.Token != d.Token {
		t.Fatalf("token differs for the same list: %s %s", o.Token, d.Token)
	}
	// Unknown tokens, and ones too old, get the whole list.
	if u, _ := l.delta(next, "nope"); !u.Full || len(u.Changed) != len(next) {
		t.Fatalf("unknown token: %+v", u.Full)
	}
	for i := 0; i < listSnapshots; i++ {
		more := append([]Session{}, next...)
		more[0].Title = fmt.Sprintf("renamed %d", i)
		_, _ = l.delta(more, "")
	}
	if old, _ := l.delta(next, first.Token); !old.Full {
		t.Fatal("a token past the last snapshots still answered with changes")
	}
	if len(l.kept) > listSnapshots {
		t.Fatalf("kept %d snapshots", len(l.kept))
	}
}

func TestSessionListOverTheAPI(t *testing.T) {
	s, _ := authServer(t)
	w := request(t, s, s.Host, "GET", "/api/sessions", "", nil)
	if w.Code != 200 || !strings.HasPrefix(strings.TrimSpace(w.Body.String()), "[") {
		t.Fatalf("plain list: %d %s", w.Code, w.Body.String())
	}
	w = request(t, s, s.Host, "GET", "/api/sessions?since=", "", nil)
	var d struct {
		Token   string
		Full    bool
		Changed []json.RawMessage
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil || !d.Full || d.Token == "" {
		t.Fatalf("first delta: %d %s", w.Code, w.Body.String())
	}
	w = request(t, s, s.Host, "GET", "/api/sessions?since="+d.Token, "", nil)
	var same map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &same) != nil || same["token"] != d.Token || len(same) != 2 || same["count"] != float64(len(d.Changed)) {
		t.Fatalf("unchanged: %s", w.Body.String())
	}
}

func TestPolledJSONIsNotSentAgain(t *testing.T) {
	s, _ := authServer(t)
	get := func(match string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://"+s.Host+"/api/workspaces", nil)
		r.Host, r.RemoteAddr = s.Host, "127.0.0.1:1234"
		if match != "" {
			r.Header.Set("If-None-Match", match)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := get("")
	tag := w.Header().Get("ETag")
	if w.Code != 200 || tag == "" || w.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("first: %d %q %q", w.Code, tag, w.Header().Get("Cache-Control"))
	}
	for _, m := range []string{tag, "W/" + tag, `"other", ` + tag} {
		if w := get(m); w.Code != 304 || w.Body.Len() != 0 {
			t.Fatalf("If-None-Match %s: %d", m, w.Code)
		}
	}
	if w := get(`"other"`); w.Code != 200 {
		t.Fatalf("changed: %d", w.Code)
	}
}

func TestTextIsCompressedMediaAndStreamsAreNot(t *testing.T) {
	m, _ := fixture(t)
	script := strings.Repeat("console.log('workbench');\n", 200)
	s := NewServer(m, fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>x</title>")},
		"assets/app.js": {Data: []byte(script)},
	}, "agent.localhost:17880", "")
	root := realTempDir(t)
	_ = os.WriteFile(filepath.Join(root, "long.txt"), []byte(strings.Repeat("a line of text\n", 400)), 0o600)
	session, err := m.Create("claude", root, "traffic")
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string, ctx context.Context) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://"+s.Host+path, nil).WithContext(ctx)
		r.Host, r.RemoteAddr = s.Host, "127.0.0.1:1234"
		r.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	bg := context.Background()
	js := get("/assets/app.js", bg)
	if js.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(js.Header().Get("Vary"), "Accept-Encoding") || js.Body.Len() >= len(script)/4 {
		t.Fatalf("script: %q %q %d bytes", js.Header().Get("Content-Encoding"), js.Header().Get("Vary"), js.Body.Len())
	}
	zr, err := gzip.NewReader(js.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(zr); string(got) != script {
		t.Fatal("script does not unpack to itself")
	}
	if js.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Fatalf("script cache: %q", js.Header().Get("Cache-Control"))
	}
	q := "?session=" + session.ID + "&path=long.txt"
	if w := get("/api/text"+q, bg); w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("JSON: %d %q", w.Code, w.Header().Get("Content-Encoding"))
	}
	// Small answers, downloads and the stream go as they are.
	if w := get("/api/auth/status", bg); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("a small answer was compressed")
	}
	if w := get("/api/download"+q, bg); w.Code != 200 || w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("download: %d %q", w.Code, w.Header().Get("Content-Encoding"))
	}
	ctx, cancel := context.WithTimeout(bg, 200*time.Millisecond)
	defer cancel()
	if w := get("/api/events", ctx); w.Header().Get("Content-Encoding") != "" || !strings.Contains(w.Body.String(), "event: hello") {
		t.Fatalf("stream: %q %q", w.Header().Get("Content-Encoding"), w.Body.String())
	}
}

func TestEventStreamSendsWhatThePageUses(t *testing.T) {
	s, m := authServer(t)
	start, _ := m.store.Append("", "prefs", map[string]any{"x": 1})
	_, _ = m.store.Append("claude:A", "native", map[string]any{"text": "for A"})
	_, _ = m.store.Append("claude:B", "status", map[string]any{"id": "claude:B"})
	last, _ := m.store.Append("claude:B", "native", map[string]any{"text": "for B"})
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest("GET", fmt.Sprintf("http://%s/api/events?session=claude:A&after=%d", s.Host, start.Seq), nil).WithContext(ctx)
	r.Host, r.RemoteAddr = s.Host, "127.0.0.1:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	body := w.Body.String()
	if !strings.Contains(body, `"for A"`) || !strings.Contains(body, `"kind":"status"`) {
		t.Fatalf("the page's events missing: %s", body)
	}
	if strings.Contains(body, `"for B"`) {
		t.Fatalf("another session's record was sent: %s", body)
	}
	if want := fmt.Sprintf("id: %d\nevent: cursor\ndata: %d\n", last.Seq, last.Seq); !strings.Contains(body, want) {
		t.Fatalf("cursor missing: %s", body)
	}
	// Telemetry unchanged for 2.5 s: sent once. No heartbeat yet.
	if n := strings.Count(body, "event: telemetry"); n != 1 {
		t.Fatalf("telemetry sent %d times", n)
	}
	if strings.Contains(body, "heartbeat") {
		t.Fatal("a heartbeat while there was traffic")
	}
}
