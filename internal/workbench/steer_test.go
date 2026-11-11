package workbench

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// claudeReady is a running Claude engine that advertised caps.
func claudeReady(t *testing.T, caps ...string) (*Manager, *fakeProcess, *runtime) {
	t.Helper()
	m, p := fixture(t)
	s, _ := m.Create("claude", t.TempDir(), "steer")
	_ = m.Start(s.ID)
	r := m.sessions[s.ID]
	m.receive(s.ID, r.generation, map[string]any{"type": "control_response", "response": map[string]any{"request_id": "init", "subtype": "success"}})
	m.receive(s.ID, r.generation, map[string]any{"type": "system", "subtype": "init", "session_id": "native-1", "capabilities": toAny(caps)})
	return m, p, r
}

func toAny(list []string) []any {
	out := []any{}
	for _, s := range list {
		out = append(out, s)
	}
	return out
}

// sent finds the last input matching keep, waiting a little for it.
func sent(p *fakeProcess, keep func(map[string]any) bool) map[string]any {
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		p.mu.Lock()
		for i := len(p.in) - 1; i >= 0; i-- {
			if keep(p.in[i]) {
				v := p.in[i]
				p.mu.Unlock()
				return v
			}
		}
		p.mu.Unlock()
	}
	return nil
}

func eventsOf(m *Manager, id, kind string) []map[string]any {
	events, _ := m.store.Events(id, 0, 1000)
	var out []map[string]any
	for _, e := range events {
		if e.Kind == kind {
			var v map[string]any
			json.Unmarshal(e.Data, &v)
			out = append(out, v)
		}
	}
	return out
}

func TestSteerCodexAddsToTheRunningTurn(t *testing.T) {
	m, p, r, answer := codexReady(t)
	id := r.session.ID
	if err := m.Send(id, "first"); err != nil {
		t.Fatal(err)
	}
	m.receive(id, r.generation, map[string]any{"method": "turn/started", "params": map[string]any{"turn": map[string]any{"id": "turn-1"}}})
	answer("turn/steer", map[string]any{"turnId": "turn-1"})
	res, err := m.Steer(id, "go left")
	if err != nil || !res.Steered {
		t.Fatalf("steer: %+v %v", res, err)
	}
	params := obj(sentMethod(p, "turn/steer")["params"])
	input := list(params["input"])
	if params["threadId"] != "th" || params["expectedTurnId"] != "turn-1" || str(obj(input[len(input)-1])["text"]) != "go left" {
		t.Fatalf("turn/steer params: %v", params)
	}
	users := eventsOf(m, id, "user")
	if last := users[len(users)-1]; last["text"] != "go left" || last["steered"] != true {
		t.Fatalf("shown as: %v", users)
	}

	// The turn ended before Codex took it: the message waits in the queue.
	go func() {
		v := sent(p, func(v map[string]any) bool {
			return v["method"] == "turn/steer" && obj(v["params"])["input"] != nil && strings.Contains(str(obj(list(obj(v["params"])["input"])[0])["text"]), "too late")
		})
		m.receive(id, r.generation, map[string]any{"id": v["id"], "error": map[string]any{"message": "expected turn mismatch"}})
	}()
	res, err = m.Steer(id, "too late")
	if err != nil || !res.Queued || !strings.Contains(res.Reason, "Codex did not take it") {
		t.Fatalf("refused steer: %+v %v", res, err)
	}
	if retracted := eventsOf(m, id, "retract"); len(retracted) != 1 || retracted[0]["text"] != "too late" {
		t.Fatalf("retract: %v", retracted)
	}
	if len(r.queue) != 1 {
		t.Fatalf("queue: %v", r.queue)
	}

	// No turn running: steering sends a turn.
	m.mu.Lock()
	r.queue = nil
	m.mu.Unlock()
	m.receive(id, r.generation, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": "turn-1", "status": "completed"}}})
	res, err = m.Steer(id, "next one")
	start := obj(sentMethod(p, "turn/start")["params"])
	if err != nil || !res.Sent || str(obj(list(start["input"])[0])["text"]) != "next one" {
		t.Fatalf("idle steer: %+v %v %v", res, err, start)
	}
}

func TestSteerClaudeWaitsForTheNextStepAndCanSendNow(t *testing.T) {
	m, p, r := claudeReady(t, "interrupt_send_now_v1", "interrupt_cancel_queued_v1")
	id := r.session.ID
	if err := m.Send(id, "first"); err != nil {
		t.Fatal(err)
	}
	res, err := m.Steer(id, "go left")
	if err != nil || !res.Steered {
		t.Fatalf("steer: %+v %v", res, err)
	}
	msg := sent(p, func(v map[string]any) bool { return v["type"] == "user" && obj(v["message"])["content"] == "go left" })
	u := str(msg["uuid"])
	users := eventsOf(m, id, "user")
	if last := users[len(users)-1]; u == "" || last["uuid"] != u || last["sendNow"] != true {
		t.Fatalf("steer shown as: %v (uuid %q)", users, u)
	}
	// Like Enter in Claude's TUI: no send-now unless asked.
	time.Sleep(50 * time.Millisecond)
	p.mu.Lock()
	for _, v := range p.in {
		if obj(v["request"])["subtype"] == "interrupt" {
			t.Fatalf("send-now without asking: %v", v)
		}
	}
	p.mu.Unlock()
	m.receive(id, r.generation, map[string]any{"type": "command_lifecycle", "command_uuid": u, "state": "queued"})
	if states := eventsOf(m, id, "steer"); len(states) != 1 || states[0]["state"] != "queued" {
		t.Fatalf("steer state: %v", states)
	}
	go func() {
		req := sent(p, func(v map[string]any) bool { return obj(v["request"])["subtype"] == "interrupt" })
		m.receive(id, r.generation, map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": req["request_id"], "response": map[string]any{"send_now": "aborted"}}})
	}()
	answer, err := m.SendNow(id, u)
	req := sent(p, func(v map[string]any) bool { return obj(v["request"])["subtype"] == "interrupt" })
	if err != nil || answer != "aborted" || obj(req["request"])["send_now"] != true || obj(req["request"])["message_uuid"] != u {
		t.Fatalf("send now: %q %v %v", answer, err, req)
	}
	// Send-now ended the turn to make room: the session keeps working.
	m.receive(id, r.generation, map[string]any{"type": "result", "is_error": true, "user_message_uuids": []any{"first-uuid"}})
	if !r.turn || r.session.Status != "running" || len(eventsOf(m, id, "error")) != 0 {
		t.Fatalf("went idle with a steer waiting: %+v", r.session)
	}
	m.receive(id, r.generation, map[string]any{"type": "command_lifecycle", "command_uuid": u, "state": "started"})
	if _, err := m.SendNow(id, u); err == nil {
		t.Fatal("send-now for a message already taken in")
	}
	m.receive(id, r.generation, map[string]any{"type": "command_lifecycle", "command_uuid": u, "state": "completed"})
	m.receive(id, r.generation, map[string]any{"type": "result", "user_message_uuids": []any{u}})
	if r.turn || r.session.Status != "idle" {
		t.Fatalf("not idle after the steer's turn: %+v", r.session)
	}
}

func TestSendNowNeedsTheCapability(t *testing.T) {
	m, p, r := claudeReady(t)
	id := r.session.ID
	m.Send(id, "first")
	m.Steer(id, "go left")
	msg := sent(p, func(v map[string]any) bool { return obj(v["message"])["content"] == "go left" })
	if _, err := m.SendNow(id, str(msg["uuid"])); err == nil || !strings.Contains(err.Error(), "no send-now") {
		t.Fatalf("send-now without the capability: %v", err)
	}
}

func TestSteerRefusalsAndQueuedSteering(t *testing.T) {
	m, p, r := claudeReady(t)
	id := r.session.ID
	m.Send(id, "first")
	if _, err := m.Steer(id, "/compact"); err == nil {
		t.Fatal("a command steered")
	}
	m.mu.Lock()
	r.pending["perm"] = map[string]any{}
	m.mu.Unlock()
	if res, err := m.Steer(id, "while asked"); err != nil || !res.Queued || !strings.Contains(res.Reason, "pending request") {
		t.Fatalf("steer during an approval: %+v %v", res, err)
	}
	m.mu.Lock()
	r.pending = map[string]any{}
	qid := r.queue[0].ID
	m.mu.Unlock()
	if res, err := m.SteerQueued(id, qid); err != nil || !res.Steered || len(r.queue) != 0 {
		t.Fatalf("steer queued: %+v %v %v", res, err, r.queue)
	}
	if sent(p, func(v map[string]any) bool { return obj(v["message"])["content"] == "while asked" }) == nil {
		t.Fatal("queued message not sent to Claude")
	}
	// No send-now capability: the message is written and nothing else.
	for _, v := range p.in {
		if obj(v["request"])["subtype"] == "interrupt" {
			t.Fatalf("send-now without the capability: %v", v)
		}
	}
	// A queued command cannot steer and goes back to the queue.
	m.Enqueue(id, "/compact")
	m.mu.Lock()
	qid = r.queue[0].ID
	m.mu.Unlock()
	if _, err := m.SteerQueued(id, qid); err == nil || len(r.queue) != 1 {
		t.Fatalf("command steered from the queue: %v", r.queue)
	}
}

func TestStopReturnsWaitingSteersToTheQueue(t *testing.T) {
	m, p, r := claudeReady(t, "interrupt_send_now_v1", "interrupt_cancel_queued_v1")
	id := r.session.ID
	m.Send(id, "first")
	m.Steer(id, "go left")
	msg := sent(p, func(v map[string]any) bool { return obj(v["message"])["content"] == "go left" })
	if err := m.Interrupt(id); err != nil {
		t.Fatal(err)
	}
	stop := sent(p, func(v map[string]any) bool {
		return obj(v["request"])["subtype"] == "interrupt" && obj(v["request"])["send_now"] == nil
	})
	if obj(stop["request"])["cancel_queued"] != true {
		t.Fatalf("stop: %v", stop)
	}
	m.receive(id, r.generation, map[string]any{"type": "command_lifecycle", "command_uuid": msg["uuid"], "state": "cancelled"})
	m.receive(id, r.generation, map[string]any{"type": "result", "is_error": true})
	q := r.queue
	if len(q) != 1 || q[0].Text != "go left" || !r.queuePaused || r.turn {
		t.Fatalf("after stop: %v paused=%v turn=%v", q, r.queuePaused, r.turn)
	}
}

func TestClaudeHistoryShowsSteeredMessages(t *testing.T) {
	path := t.TempDir() + "/s.jsonl"
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"message":{"role":"user","content":"read the files"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"role":"assistant","content":[{"type":"text","text":"reading"}]}}`,
		`{"type":"attachment","uuid":"s1","parentUuid":"a1","attachment":{"type":"queued_command","prompt":"end with BANANA","commandMode":"prompt"}}`,
		`{"type":"attachment","uuid":"n1","parentUuid":"s1","attachment":{"type":"queued_command","prompt":"task done","commandMode":"task-notification"}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"n1","message":{"role":"assistant","content":[{"type":"text","text":"done BANANA"}]}}`,
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	page, err := readClaudeHistory(path, Session{ID: "claude:x"}, "", 10)
	if err != nil || len(page.Events) != 4 {
		t.Fatalf("%d events: %v", len(page.Events), err)
	}
	var v map[string]any
	json.Unmarshal(page.Events[2].Data, &v)
	if v["type"] != "user" || v["steered"] != true || obj(v["message"])["content"] != "end with BANANA" {
		t.Fatalf("steered record: %v", v)
	}
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}
