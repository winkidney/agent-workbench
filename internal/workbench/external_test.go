package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func claudeLine(uuid, parent, kind, ts, content, stop string) string {
	msg := map[string]any{"role": kind, "content": json.RawMessage(content)}
	if kind == "assistant" {
		msg["id"] = "m-" + uuid
		msg["stop_reason"] = stop
		msg["usage"] = map[string]any{"input_tokens": 10, "output_tokens": 5}
	}
	b, _ := json.Marshal(map[string]any{"type": kind, "uuid": uuid, "parentUuid": parent, "sessionId": "ext", "cwd": "/tmp/work", "timestamp": ts, "message": msg})
	return string(b) + "\n"
}

func TestActivityStateRules(t *testing.T) {
	steps := []struct {
		line    string
		running bool
		since   string
	}{
		{claudeLine("u1", "", "user", "T1", `"build it"`, ""), true, "T1"},
		{claudeLine("a1", "u1", "assistant", "T2", `[{"type":"tool_use","id":"x","name":"Bash","input":{}}]`, "tool_use"), true, "T1"},
		{claudeLine("r1", "a1", "user", "T3", `[{"type":"tool_result","tool_use_id":"x","content":"ok"}]`, ""), true, "T1"},
		{`{"type":"user","isSidechain":true,"uuid":"s","message":{"content":"[Request interrupted by user]"}}` + "\n", true, "T1"},
		{claudeLine("a2", "r1", "assistant", "T4", `[{"type":"text","text":"done"}]`, "end_turn"), false, ""},
		{claudeLine("u2", "a2", "user", "T5", `"again"`, ""), true, "T5"},
		{claudeLine("u3", "u2", "user", "T6", `[{"type":"text","text":"[Request interrupted by user]"}]`, ""), false, ""},
	}
	st := activityState{}
	for i, s := range steps {
		st.apply("claude", []byte(s.line))
		if st.running != s.running || st.since != s.since {
			t.Fatalf("claude step %d: running=%v since=%q", i, st.running, st.since)
		}
	}
	st = activityState{}
	st.apply("codex", []byte(`{"type":"event_msg","payload":{"type":"task_started","started_at":1700000000}}`))
	if !st.running || st.since != "2023-11-14T22:13:20Z" {
		t.Fatalf("codex start: %+v", st)
	}
	st.apply("codex", []byte(`{"type":"response_item","payload":{"type":"message","content":"task_complete"}}`))
	if !st.running {
		t.Fatal("non-marker record ended the turn")
	}
	st.apply("codex", []byte(`{"type":"event_msg","payload":{"type":"task_complete"}}`))
	if st.running || !st.decided {
		t.Fatalf("codex complete: %+v", st)
	}
}

func TestExternalActivityLooksBackThenFollows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	var b strings.Builder
	b.WriteString(claudeLine("u1", "", "user", "T1", `"long task"`, ""))
	// More than the first 1 MiB window of tool traffic after the prompt.
	filler := strings.Repeat("x", 4096)
	for i := 0; i < 400; i++ {
		b.WriteString(claudeLine(fmt.Sprint("a", i), "", "assistant", "T2", `[{"type":"tool_use","id":"x","name":"Bash","input":{}}]`, "tool_use"))
		b.WriteString(claudeLine(fmt.Sprint("r", i), "", "user", "T2", `[{"type":"tool_result","tool_use_id":"x","content":"`+filler+`"}]`, ""))
	}
	os.WriteFile(path, []byte(b.String()), 0o600)
	a := &externalActivity{path: path}
	if err := a.read("claude"); err != nil || !a.state.running || a.state.since != "T1" {
		t.Fatalf("look back: %+v %v", a.state, err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(claudeLine("z", "", "assistant", "T9", `[{"type":"text","text":"done"}]`, "end_turn"))
	// A record still being written is not consumed.
	f.WriteString(`{"type":"user","uuid":"p"`)
	f.Close()
	if a.read("claude"); a.state.running {
		t.Fatal("finished reply not followed")
	}
	os.WriteFile(path, []byte(claudeLine("n", "", "user", "T10", `"new"`, "")), 0o600)
	if a.read("claude"); !a.state.running || a.state.since != "T10" {
		t.Fatalf("truncated file not re-read: %+v", a.state)
	}
}

func TestClaudeIndexIncrementalMatchesFullRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		claudeLine("u1", "", "user", "T1", `"hi"`, ""),
		claudeLine("a1", "u1", "assistant", "T2", `[{"type":"tool_use","id":"x","name":"Bash","input":{}}]`, "tool_use"),
		claudeLine("r1", "a1", "user", "T3", `[{"type":"tool_result","tool_use_id":"x","content":"ok"}]`, ""),
		claudeLine("a2", "r1", "assistant", "T4", `[{"type":"text","text":"done"}]`, "end_turn"),
	}
	full := strings.Join(lines, "")
	cut := len(lines[0]) + len(lines[1]) + 10
	os.WriteFile(path, []byte(full[:cut]), 0o600)
	s := Session{ID: "claude:ext"}
	x := newClaudeIndex(s)
	f, _ := os.Open(path)
	if err := x.scan(f, false); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if len(x.order) != 2 {
		t.Fatalf("partial record indexed: %d", len(x.order))
	}
	os.WriteFile(path, []byte(full), 0o600)
	f, _ = os.Open(path)
	defer f.Close()
	x.scan(f, false)
	got, _ := x.page(f, s, "", 80)
	want, _ := readClaudeHistory(path, s, "", 80)
	if len(got.Events) != 4 || len(got.Events) != len(want.Events) {
		t.Fatalf("got %d want %d", len(got.Events), len(want.Events))
	}
	for i := range want.Events {
		if string(got.Events[i].Data) != string(want.Events[i].Data) {
			t.Fatalf("event %d differs", i)
		}
	}
}

// externalClaude puts a Claude transcript in a fresh config dir that this
// test process holds, standing in for another client. It returns a writer
// for new records and the ownership file.
func externalClaude(t *testing.T, m *Manager) (func(string), string) {
	t.Helper()
	home := t.TempDir()
	m.catalog.ClaudeHome = home
	m.catalog.CodexHome = t.TempDir()
	dir := filepath.Join(home, "projects", "-tmp-work")
	os.MkdirAll(dir, 0o700)
	os.MkdirAll(filepath.Join(home, "sessions"), 0o700)
	path := filepath.Join(dir, "ext.jsonl")
	appendLine := func(line string) {
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		f.WriteString(line)
		f.Close()
	}
	appendLine(claudeLine("u1", "", "user", "T1", `"hi"`, ""))
	appendLine(claudeLine("a1", "u1", "assistant", "T2", `[{"type":"tool_use","id":"x","name":"Bash","input":{}}]`, "tool_use"))
	owner := filepath.Join(home, "sessions", "1.json")
	os.WriteFile(owner, []byte(fmt.Sprintf(`{"pid":%d,"procStart":"any","sessionId":"ext"}`, os.Getpid())), 0o600)
	return appendLine, owner
}

func TestExternalTailFollowsClaude(t *testing.T) {
	m, _ := fixture(t)
	appendLine, owner := externalClaude(t, m)
	list, _ := m.NativeList()
	if len(list) != 1 || list[0].Status != "external_running" {
		t.Fatalf("list: %+v", list)
	}
	tail := m.followExternal("claude:ext")
	defer tail.close()
	poll := func() *externalUpdate {
		tail.polled = time.Time{}
		return tail.poll()
	}
	if u := poll(); u == nil || !u.Running || u.StartedAt != "T1" || u.Events != nil {
		t.Fatalf("first poll: %+v", u)
	}
	appendLine(claudeLine("r1", "a1", "user", "T3", `[{"type":"tool_result","tool_use_id":"x","content":"ok"}]`, ""))
	u := poll()
	if u == nil || u.Reset || len(u.Events) != 3 {
		t.Fatalf("first change sends the tail page: %+v", u)
	}
	appendLine(claudeLine("a2", "r1", "assistant", "T4", `[{"type":"text","text":"done"}]`, "end_turn"))
	u = poll()
	if u == nil || u.Running || u.Status != "external" || len(u.Events) != 1 || eventKey(u.Events[0]) != "a2" {
		t.Fatalf("only the new record: %+v", u)
	}
	if u := poll(); u != nil {
		t.Fatalf("unchanged record: %+v", u)
	}
	// Rewinding to an earlier message starts a branch the stream did not continue.
	appendLine(claudeLine("u9", "a1", "user", "T5", `"different"`, ""))
	if u := poll(); u == nil || !u.Reset {
		t.Fatalf("branch switch: %+v", u)
	}
	os.Remove(owner)
	m.catalog.updated = time.Time{}
	if u := poll(); u == nil || u.External || u.Status == "external" {
		t.Fatalf("client let go: %+v", u)
	}
	if u := poll(); u != nil {
		t.Fatalf("after release: %+v", u)
	}
}

func TestUsageScanIsIncremental(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.jsonl")
	token := func(n int) string {
		return fmt.Sprintf(`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":%d},"last_token_usage":{"input_tokens":%d},"model_context_window":1000}}}`+"\n", n, n)
	}
	os.WriteFile(path, []byte(token(10)+`{"type":"turn_context","payload":{"model":"m","effort":"high"}}`+"\n"+token(20)[:30]), 0o600)
	u := &usageScan{seen: map[string]map[string]any{}}
	f, _ := os.Open(path)
	u.scan(f, "codex")
	f.Close()
	if got := u.result(); got.Total["total_tokens"] != float64(10) || got.Effective.Model != "m" {
		t.Fatalf("first: %+v", got)
	}
	os.WriteFile(path, []byte(token(10)+`{"type":"turn_context","payload":{"model":"m","effort":"high"}}`+"\n"+token(20)), 0o600)
	f, _ = os.Open(path)
	u.scan(f, "codex")
	f.Close()
	if got := u.result(); got.Total["total_tokens"] != float64(20) {
		t.Fatalf("appended: %+v", got)
	}
}

func TestHistoryReportsRunStateAndStreamAnnouncesBuild(t *testing.T) {
	m, _ := fixture(t)
	appendLine, _ := externalClaude(t, m)
	assets := fstest.MapFS{"index.html": {Data: []byte("<html>v1</html>")}}
	s := pairedServer(m, assets, "agent.localhost:17880", "pair")
	cookie := request(t, s, s.Host, "POST", "/api/login", `{"code":"pair"}`, nil).Result().Cookies()[0]
	running := func() bool {
		w := request(t, s, s.Host, "GET", "/api/history?session=claude%3Aext", "", cookie)
		var page HistoryPage
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 {
			t.Fatalf("history: %d %s", w.Code, w.Body.String())
		}
		return page.Running
	}
	// The other client is mid-tool: the page must not settle that tool.
	if !running() {
		t.Fatal("running turn in another client reported as finished")
	}
	appendLine(claudeLine("r1", "a1", "user", "T3", `[{"type":"tool_result","tool_use_id":"x","content":"ok"}]`, ""))
	appendLine(claudeLine("a2", "r1", "assistant", "T4", `[{"type":"text","text":"done"}]`, "end_turn"))
	if running() {
		t.Fatal("finished turn reported as running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest("GET", "http://"+s.Host+"/api/events", nil).WithContext(ctx)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Host = s.Host
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	want := "event: hello\ndata: {\"build\":\"" + assetBuild(assets) + "\"}"
	if assetBuild(assets) == "" || !strings.Contains(w.Body.String(), want) {
		t.Fatalf("hello: %q", w.Body.String())
	}
	if assetBuild(fstest.MapFS{"index.html": {Data: []byte("<html>v2</html>")}}) == assetBuild(assets) {
		t.Fatal("a new web build keeps the old id")
	}
}

func TestClaudeProcessStatusWinsOverTheRecord(t *testing.T) {
	m, _ := fixture(t)
	appendLine, owner := externalClaude(t, m)
	status := func() string {
		m.catalog.mu.Lock()
		m.catalog.updated = time.Time{}
		m.catalog.mu.Unlock()
		list, _ := m.NativeList()
		return list[0].Status
	}
	// The record ends with a tool call, but Claude says it is waiting
	// (as after a compaction summary, which reads like a prompt).
	os.WriteFile(owner, []byte(fmt.Sprintf(`{"pid":%d,"procStart":"any","sessionId":"ext","status":"idle"}`, os.Getpid())), 0o600)
	if got := status(); got != "external" {
		t.Fatalf("idle process: %s", got)
	}
	// The record ends the turn, but Claude reports it is working.
	appendLine(claudeLine("a2", "a1", "assistant", "T4", `[{"type":"text","text":"done"}]`, "end_turn"))
	os.WriteFile(owner, []byte(fmt.Sprintf(`{"pid":%d,"procStart":"any","sessionId":"ext","status":"busy"}`, os.Getpid())), 0o600)
	if got := status(); got != "external_running" {
		t.Fatalf("busy process: %s", got)
	}
	// Without a status, the record decides.
	os.WriteFile(owner, []byte(fmt.Sprintf(`{"pid":%d,"procStart":"any","sessionId":"ext"}`, os.Getpid())), 0o600)
	if got := status(); got != "external" {
		t.Fatalf("no status: %s", got)
	}
}
