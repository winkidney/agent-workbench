package workbench

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tool results are native user records; history must keep their block structure
// so the browser renders them as tool output instead of user prompts.
func TestClaudeHistoryKeepsNativeBlocksAndPagesFromNewest(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.jsonl")
	lines := []map[string]any{
		{"type": "user", "uuid": "u1", "message": map[string]any{"role": "user", "content": "run tests"}},
		{"type": "assistant", "uuid": "a1", "parentUuid": "u1", "message": map[string]any{"id": "m1", "role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": "plan"}}}},
		{"type": "assistant", "uuid": "a2", "parentUuid": "a1", "message": map[string]any{"id": "m1", "role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "toolu_1", "name": "Bash", "input": map[string]any{"command": "npm test"}}}}},
		{"type": "user", "uuid": "r1", "parentUuid": "a2", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": strings.Repeat("x", 40000)}}}},
		{"type": "assistant", "uuid": "a3", "parentUuid": "r1", "message": map[string]any{"id": "m2", "role": "assistant", "content": []any{map[string]any{"type": "text", "text": "done"}}}},
	}
	var b strings.Builder
	for _, line := range lines {
		raw, _ := json.Marshal(line)
		b.Write(raw)
		b.WriteByte('\n')
	}
	os.WriteFile(p, []byte(b.String()), 0600)
	page, err := readClaudeHistory(p, Session{ID: "claude:x"}, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || !page.HasMore || page.Before != "3" {
		t.Fatalf("newest page: %+v", page)
	}
	var result map[string]any
	json.Unmarshal(page.Events[0].Data, &result)
	block := obj(obj(result["message"])["content"].([]any)[0])
	if result["type"] != "user" || block["type"] != "tool_result" || block["tool_use_id"] != "toolu_1" {
		t.Fatalf("tool result flattened: %v", result)
	}
	if len(str(block["content"])) > historyStringLimit+200 {
		t.Fatal("tool output not bounded")
	}
	older, err := readClaudeHistory(p, Session{ID: "claude:x"}, page.Before, 2)
	if err != nil || len(older.Events) != 2 || older.Before != "1" {
		t.Fatalf("older page: %+v %v", older, err)
	}
	var tool map[string]any
	json.Unmarshal(older.Events[1].Data, &tool)
	if tool["uuid"] != "a2" || !strings.Contains(string(older.Events[1].Data), `"name":"Bash"`) {
		t.Fatalf("tool use lost: %s", older.Events[1].Data)
	}
	first, _ := readClaudeHistory(p, Session{ID: "claude:x"}, older.Before, 2)
	if len(first.Events) != 1 || first.HasMore {
		t.Fatalf("first page: %+v", first)
	}
}

func TestClassifyLiveCoalescesDeltasAndKeepsUnknown(t *testing.T) {
	cases := []struct {
		v       map[string]any
		class   int
		channel string
	}{
		{map[string]any{"type": "stream_event", "api_message_id": "m", "event": map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "text_delta", "text": "hi"}}}, liveDelta, "text"},
		{map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": "hm"}}}, liveDelta, "thinking"},
		{map[string]any{"type": "stream_event", "event": map[string]any{"type": "message_start"}}, liveDrop, ""},
		{map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"itemId": "i", "delta": "x"}}, liveDelta, "text"},
		{map[string]any{"method": "item/reasoning/summaryTextDelta", "params": map[string]any{"itemId": "i", "delta": "x"}}, liveDelta, "thinking"},
		{map[string]any{"method": "item/commandExecution/outputDelta"}, liveDrop, ""},
		{map[string]any{"method": "item/somethingNew/started"}, liveKeep, ""},
		{map[string]any{"type": "assistant"}, liveKeep, ""},
		{map[string]any{"type": "brand_new_event"}, liveKeep, ""},
	}
	for i, c := range cases {
		d, class := classifyLive(c.v)
		if class != c.class || d.Channel != c.channel {
			t.Fatalf("case %d: class %d channel %q", i, class, d.Channel)
		}
	}
	d, _ := classifyLive(cases[0].v)
	if d.Item != "m:1" {
		t.Fatalf("claude item key %q", d.Item)
	}
}

func TestDeltaCoalescingSkipsStreamPersistence(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("claude", t.TempDir(), "test")
	_ = m.Start(s.ID)
	r := m.sessions[s.ID]
	for i := 0; i < 50; i++ {
		m.receive(s.ID, r.generation, map[string]any{"type": "stream_event", "api_message_id": "m", "event": map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": fmt.Sprint(i, " ")}}})
	}
	m.receive(s.ID, r.generation, map[string]any{"type": "assistant", "uuid": "a", "message": map[string]any{"id": "m", "content": []any{}}})
	events, _ := m.store.Events(s.ID, 0, 1000)
	deltas, natives := 0, 0
	text := ""
	for _, e := range events {
		switch e.Kind {
		case "delta":
			deltas++
			var v map[string]any
			json.Unmarshal(e.Data, &v)
			text += str(v["text"])
			if v["item"] != "m:0" || v["channel"] != "text" {
				t.Fatalf("delta metadata %s", e.Data)
			}
		case "native":
			natives++
			if strings.Contains(string(e.Data), "stream_event") {
				t.Fatal("stream event persisted")
			}
		}
	}
	if deltas == 0 || deltas > 3 || natives != 1 || !strings.HasPrefix(text, "0 1 2") || !strings.HasSuffix(text, "49 ") {
		t.Fatalf("deltas=%d natives=%d text=%q", deltas, natives, text)
	}
}

func TestEventStreamPushesWithoutPolling(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("codex", t.TempDir(), "test")
	start := time.Now()
	ch := m.changes.C()
	go func() {
		time.Sleep(10 * time.Millisecond)
		m.emit(s.ID, "notice", map[string]any{})
	}()
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("notifier did not wake reader")
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("push latency too high")
	}
}

func TestBoundJSONOmitsLargeImages(t *testing.T) {
	small := map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "AAAA"}}
	large := map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": strings.Repeat("A", historyImageLimit+1)}}
	out := boundJSON([]any{small, large}).([]any)
	if obj(obj(out[0])["source"])["data"] != "AAAA" {
		t.Fatal("small image dropped")
	}
	if obj(out[1])["omitted"] != true {
		t.Fatal("large image kept")
	}
}

func TestThumbServesOnlyConfinedRasterImages(t *testing.T) {
	posixOnly(t)
	m, _ := fixture(t)
	root := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	os.WriteFile(filepath.Join(root, "ok.png"), png, 0600)
	os.WriteFile(filepath.Join(root, "page.html"), []byte("<script>alert(1)</script>"), 0600)
	os.WriteFile(filepath.Join(root, "fake.png"), []byte("<svg xmlns='http://www.w3.org/2000/svg'><script>x</script></svg>"), 0600)
	os.WriteFile(filepath.Join(root, "icon.svg"), []byte("<svg/>"), 0600)
	outside := filepath.Join(t.TempDir(), "secret.png")
	os.WriteFile(outside, png, 0600)
	os.Symlink(outside, filepath.Join(root, "escape.png"))
	session, _ := m.Create("claude", root, "thumb")
	s := pairedServer(m, nil, "agent.localhost:17880", "pair")
	cookie := request(t, s, s.Host, "POST", "/api/login", `{"code":"pair"}`, nil).Result().Cookies()[0]
	get := func(p string) *http.Response {
		return request(t, s, s.Host, "GET", "/api/thumb?session="+session.ID+"&path="+p, "", cookie).Result()
	}
	ok := get("ok.png")
	if ok.StatusCode != 200 || ok.Header.Get("Content-Type") != "image/png" || ok.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(ok.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("png: %d %v", ok.StatusCode, ok.Header)
	}
	if r := get("missing.png"); r.StatusCode != 404 {
		t.Fatalf("missing image: %d", r.StatusCode)
	}
	for _, p := range []string{"page.html", "icon.svg", "fake.png", "escape.png", "../secret.png", "/etc/passwd"} {
		if r := get(p); r.StatusCode == 200 {
			t.Fatalf("%s served", p)
		}
	}
	if r := lan(t, s, "GET", "/api/thumb?session="+session.ID+"&path=ok.png", "", nil).Result(); r.StatusCode != 401 {
		t.Fatalf("unauthenticated thumb: %d", r.StatusCode)
	}
}

// Claude writes parallel tool results as leaves off each tool_use record; only
// the last result is on the parent chain.
func TestClaudeHistoryKeepsParallelToolResults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.jsonl")
	rec := func(uuid, parent, typ string, content any) map[string]any {
		return map[string]any{"type": typ, "uuid": uuid, "parentUuid": parent, "message": map[string]any{"content": content}}
	}
	use := func(id string) []any {
		return []any{map[string]any{"type": "tool_use", "id": id, "name": "WebSearch", "input": map[string]any{}}}
	}
	result := func(id string) []any {
		return []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": "ok " + id}}
	}
	lines := []map[string]any{
		rec("u", "", "user", "search"),
		rec("a1", "u", "assistant", use("t1")),
		rec("a2", "a1", "assistant", use("t2")),
		rec("a3", "a2", "assistant", use("t3")),
		rec("r1", "a1", "user", result("t1")),
		rec("r2", "a2", "user", result("t2")),
		rec("r3", "a3", "user", result("t3")),
		rec("done", "r3", "assistant", []any{map[string]any{"type": "text", "text": "found"}}),
		// An abandoned branch is still dropped.
		rec("old", "u", "assistant", []any{map[string]any{"type": "text", "text": "abandoned"}}),
	}
	lines[8], lines[7] = lines[7], lines[8]
	// Claude hangs file attachments off a result; that does not continue it.
	lines = append(lines, map[string]any{"type": "attachment", "uuid": "att", "parentUuid": "r1"})
	var b strings.Builder
	for _, line := range lines {
		raw, _ := json.Marshal(line)
		b.Write(raw)
		b.WriteByte('\n')
	}
	os.WriteFile(p, []byte(b.String()), 0600)
	page, err := readClaudeHistory(p, Session{ID: "claude:x"}, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range page.Events {
		var v map[string]any
		json.Unmarshal(e.Data, &v)
		got = append(got, str(v["uuid"]))
	}
	if strings.Join(got, ",") != "u,a1,a2,a3,r1,r2,r3,done" {
		t.Fatalf("history order %v", got)
	}
}

func TestReplyDecisionsMapToNativeResponses(t *testing.T) {
	cases := []struct {
		agent, decision string
		check           func(map[string]any) bool
	}{
		{"codex", "allowAlways", func(v map[string]any) bool { return obj(v["result"])["decision"] == "acceptForSession" }},
		{"codex", "cancel", func(v map[string]any) bool { return obj(v["result"])["decision"] == "cancel" }},
		{"claude", "allowAlways", func(v map[string]any) bool {
			resp := obj(obj(obj(v["response"]))["response"])
			perms, _ := resp["updatedPermissions"].([]any)
			return resp["behavior"] == "allow" && len(perms) == 1
		}},
		{"claude", "deny", func(v map[string]any) bool {
			resp := obj(obj(obj(v["response"]))["response"])
			return resp["behavior"] == "deny" && resp["message"] == "use rg instead" && resp["interrupt"] == nil
		}},
		{"claude", "cancel", func(v map[string]any) bool {
			return obj(obj(obj(v["response"]))["response"])["interrupt"] == true
		}},
	}
	for _, c := range cases {
		t.Run(c.agent+"/"+c.decision, func(t *testing.T) {
			m, p := fixture(t)
			s, _ := m.Create(c.agent, t.TempDir(), "x")
			_ = m.Start(s.ID)
			r := m.sessions[s.ID]
			key := "7"
			if c.agent == "codex" {
				m.receive(s.ID, r.generation, map[string]any{"id": 7, "method": "item/commandExecution/requestApproval", "params": map[string]any{"command": "ls"}})
			} else {
				m.receive(s.ID, r.generation, map[string]any{"type": "control_request", "request_id": "7", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": "grep"}, "permission_suggestions": []any{map[string]any{"type": "addRules"}}}})
			}
			if err := m.ReplyDecision(s.ID, key, c.decision, "use rg instead", nil); err != nil {
				t.Fatal(err)
			}
			if last := p.in[len(p.in)-1]; !c.check(last) {
				t.Fatalf("reply %v", last)
			}
		})
	}
	m, _ := fixture(t)
	s, _ := m.Create("codex", t.TempDir(), "x")
	_ = m.Start(s.ID)
	m.receive(s.ID, m.sessions[s.ID].generation, map[string]any{"id": 1, "method": "item/commandExecution/requestApproval"})
	if err := m.ReplyDecision(s.ID, "1", "maybe", "", nil); err == nil {
		t.Fatal("accepted unknown decision")
	}
}

func TestLiveReportsPendingApprovals(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("claude", t.TempDir(), "x")
	_ = m.Start(s.ID)
	m.receive(s.ID, m.sessions[s.ID].generation, map[string]any{"type": "control_request", "request_id": "p", "request": map[string]any{"subtype": "can_use_tool"}})
	live := m.Live(s.ID)
	pending := live["pending"].([]map[string]any)
	if len(pending) != 1 || pending[0]["id"] != "p" {
		t.Fatalf("live %v", live)
	}
}

func TestQueueSendsAfterTurnAndPausesAfterInterrupt(t *testing.T) {
	m, p := fixture(t)
	s, _ := m.Create("codex", t.TempDir(), "q")
	_ = m.Start(s.ID)
	r := m.sessions[s.ID]
	gen := r.generation
	m.receive(s.ID, gen, map[string]any{"id": "init", "result": map[string]any{}})
	m.receive(s.ID, gen, map[string]any{"id": "thread", "result": map[string]any{"thread": map[string]any{"id": "n"}}})
	if queued, err := m.Enqueue(s.ID, "first"); err != nil || queued {
		t.Fatalf("idle session should send directly: %v %v", queued, err)
	}
	if queued, err := m.Enqueue(s.ID, "second"); err != nil || !queued {
		t.Fatalf("busy session should queue: %v %v", queued, err)
	}
	m.mu.Lock()
	r.last = time.Now().Add(-time.Hour)
	m.mu.Unlock()
	sent := func() []string {
		p.mu.Lock()
		defer p.mu.Unlock()
		var out []string
		for _, in := range p.in {
			if in["method"] == "turn/start" {
				out = append(out, str(obj(obj(in["params"])["input"].([]any)[0])["text"]))
			}
		}
		return out
	}
	m.receive(s.ID, gen, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})
	deadline := time.Now().Add(2 * time.Second)
	for len(sent()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := sent(); strings.Join(got, ",") != "first,second" {
		t.Fatalf("sent %v", got)
	}
	// Interrupting a turn pauses the rest of the queue.
	if queued, _ := m.Enqueue(s.ID, "third"); !queued {
		t.Fatal("third not queued")
	}
	m.mu.Lock()
	m.sessions[s.ID].turnID = "t"
	m.mu.Unlock()
	if err := m.Interrupt(s.ID); err != nil {
		t.Fatal(err)
	}
	m.receive(s.ID, gen, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "interrupted"}}})
	time.Sleep(50 * time.Millisecond)
	m.mu.Lock()
	paused, left := r.queuePaused, len(r.queue)
	m.mu.Unlock()
	if !paused || left != 1 || len(sent()) != 2 {
		t.Fatalf("paused=%v left=%d sent=%v", paused, left, sent())
	}
	live := m.Live(s.ID)
	if live["queuePaused"] != true || len(live["queue"].([]queued)) != 1 {
		t.Fatalf("live %v", live)
	}
	if err := m.ResumeQueue(s.ID); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for len(sent()) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := sent(); len(got) != 3 || got[2] != "third" {
		t.Fatalf("resume sent %v", got)
	}
	// Removing an entry works and unknown IDs are rejected.
	m.Enqueue(s.ID, "fourth")
	m.mu.Lock()
	qid := r.queue[0].ID
	m.mu.Unlock()
	if err := m.RemoveQueued(s.ID, qid); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveQueued(s.ID, "missing"); err == nil {
		t.Fatal("removed unknown entry")
	}
}

func TestDropLiveTurnSummary(t *testing.T) {
	ev := func(method, status string) Event {
		b, _ := json.Marshal(map[string]any{"method": method, "params": map[string]any{"turn": map[string]any{"status": status}}})
		return Event{Data: b}
	}
	events := []Event{ev("turn/completed", "completed"), ev("item/completed", ""), ev("turn/completed", "interrupted")}
	if got := dropLiveTurnSummary(events); len(got) != 2 {
		t.Fatalf("live summary kept: %d", len(got))
	}
	done := []Event{ev("item/completed", ""), ev("turn/completed", "completed")}
	if got := dropLiveTurnSummary(done); len(got) != 2 {
		t.Fatal("completed summary dropped")
	}
}

func TestClaudeHistoryKeepsPromptOrigin(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history.jsonl")
	line, _ := json.Marshal(map[string]any{"type": "user", "uuid": "n", "promptSource": "system", "origin": map[string]any{"kind": "task-notification"}, "message": map[string]any{"content": "<task-notification></task-notification>"}})
	os.WriteFile(p, append(line, '\n'), 0600)
	page, err := readClaudeHistory(p, Session{ID: "claude:x"}, "", 10)
	if err != nil || len(page.Events) != 1 {
		t.Fatal(page, err)
	}
	var v map[string]any
	json.Unmarshal(page.Events[0].Data, &v)
	if v["promptSource"] != "system" || obj(v["origin"])["kind"] != "task-notification" {
		t.Fatalf("origin dropped: %s", page.Events[0].Data)
	}
}
