package workbench

import (
	"testing"
	"time"
)

func lastInput(p *fakeProcess, keep func(map[string]any) bool) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.in) - 1; i >= 0; i-- {
		if keep(p.in[i]) {
			return p.in[i]
		}
	}
	return nil
}

func replyTo(p *fakeProcess, id any) map[string]any {
	return lastInput(p, func(v map[string]any) bool { return v["id"] == id && v["result"] != nil })
}

func TestCodexPermissionRequests(t *testing.T) {
	m, p, r, _ := codexReady(t)
	id := r.session.ID
	asked := map[string]any{"network": map[string]any{"enabled": true}}
	request := func(n float64) {
		m.receive(id, r.generation, map[string]any{"id": n, "method": "item/permissions/requestApproval", "params": map[string]any{"itemId": "i1", "reason": "fetch the docs", "permissions": asked, "cwd": "/w", "threadId": "th", "turnId": "t1", "startedAtMs": 1.0}})
	}
	m.receive(id, r.generation, map[string]any{"method": "turn/started", "params": map[string]any{"turn": map[string]any{"id": "t1"}}})
	request(1)
	if attentionNow(m, r) != "approval" {
		t.Fatalf("attention: %q", attentionNow(m, r))
	}
	if err := m.ReplyDecision(id, "1", "allow", "", nil, ""); err != nil {
		t.Fatal(err)
	}
	if res := obj(replyTo(p, 1.0)["result"]); res["scope"] != "turn" || obj(obj(res["permissions"])["network"])["enabled"] != true {
		t.Fatalf("allow: %v", res)
	}
	request(2)
	_ = m.ReplyDecision(id, "2", "allowAlways", "", nil, "")
	if res := obj(replyTo(p, 2.0)["result"]); res["scope"] != "session" {
		t.Fatalf("for the session: %v", res)
	}
	request(3)
	_ = m.ReplyDecision(id, "3", "deny", "", nil, "")
	if res := obj(replyTo(p, 3.0)["result"]); len(obj(res["permissions"])) != 0 || res["scope"] != nil {
		t.Fatalf("deny: %v", res)
	}
	request(4)
	_ = m.ReplyDecision(id, "4", "cancel", "", nil, "")
	if interrupt := sentMethod(p, "turn/interrupt"); obj(interrupt["params"])["turnId"] != "t1" {
		t.Fatalf("cancel stops the turn: %v", interrupt)
	}
}

func TestMCPElicitation(t *testing.T) {
	// Codex.
	m, p, r, _ := codexReady(t)
	id := r.session.ID
	m.receive(id, r.generation, map[string]any{"id": 5.0, "method": "mcpServer/elicitation/request", "params": map[string]any{"serverName": "deploy", "threadId": "th", "mode": "form", "message": "Which environment?", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"env": map[string]any{"type": "string", "enum": []any{"staging", "prod"}}}}}})
	if attentionNow(m, r) != "question" {
		t.Fatalf("attention: %q", attentionNow(m, r))
	}
	if err := m.ReplyDecision(id, "5", "allow", "", map[string]any{"env": "staging"}, ""); err != nil {
		t.Fatal(err)
	}
	if res := obj(replyTo(p, 5.0)["result"]); res["action"] != "accept" || obj(res["content"])["env"] != "staging" {
		t.Fatalf("accept: %v", res)
	}
	m.receive(id, r.generation, map[string]any{"id": 6.0, "method": "mcpServer/elicitation/request", "params": map[string]any{"serverName": "deploy", "threadId": "th", "mode": "url", "message": "Sign in", "url": "https://example.com/login", "elicitationId": "e1"}})
	_ = m.ReplyDecision(id, "6", "deny", "", nil, "")
	if res := obj(replyTo(p, 6.0)["result"]); res["action"] != "decline" || res["content"] != nil {
		t.Fatalf("decline: %v", res)
	}

	// Claude.
	mc, pc, rc := claudeReady(t)
	cid := rc.session.ID
	mc.receive(cid, rc.generation, map[string]any{"type": "control_request", "request_id": "el1", "request": map[string]any{"subtype": "elicitation", "mcp_server_name": "deploy", "message": "Which environment?", "mode": "form", "requested_schema": map[string]any{"type": "object", "properties": map[string]any{"env": map[string]any{"type": "string"}}}}})
	if attentionNow(mc, rc) != "question" {
		t.Fatalf("claude attention: %q", attentionNow(mc, rc))
	}
	if err := mc.ReplyDecision(cid, "el1", "allow", "", map[string]any{"env": "prod"}, ""); err != nil {
		t.Fatal(err)
	}
	reply := lastInput(pc, func(v map[string]any) bool { return v["type"] == "control_response" })
	response := obj(obj(reply["response"])["response"])
	if obj(reply["response"])["request_id"] != "el1" || response["action"] != "accept" || obj(response["content"])["env"] != "prod" {
		t.Fatalf("claude accept: %v", reply)
	}
}

func TestCodexCurrentTimeIsAnswered(t *testing.T) {
	m, p, r, _ := codexReady(t)
	id := r.session.ID
	before := time.Now().Unix()
	m.receive(id, r.generation, map[string]any{"id": 9.0, "method": "currentTime/read", "params": map[string]any{"threadId": "th"}})
	reply := replyTo(p, 9.0)
	if at := num(obj(reply["result"])["currentTimeAt"]); at < float64(before) || at > float64(time.Now().Unix()) {
		t.Fatalf("time: %v", reply)
	}
	m.mu.Lock()
	pending := len(r.pending)
	m.mu.Unlock()
	if pending != 0 || len(eventsOf(m, id, "approval")) != 0 {
		t.Fatal("asked the user for the time")
	}
}

func TestTurnDetails(t *testing.T) {
	m, _, r := claudeReady(t)
	id := r.session.ID
	feed := func(v map[string]any) { m.receive(id, r.generation, v) }
	if err := m.Send(id, "go"); err != nil {
		t.Fatal(err)
	}
	detail := func() string { return m.Telemetry(id).Detail }
	feed(map[string]any{"type": "system", "subtype": "api_retry", "attempt": 2.0, "max_retries": 10.0, "retry_delay_ms": 4000.0, "error_status": 529.0})
	if got := detail(); got != "API error 529 · retrying (attempt 2 of 10)" {
		t.Fatalf("retry: %q", got)
	}
	feed(map[string]any{"type": "stream_event", "event": map[string]any{"type": "message_start"}})
	if got := detail(); got != "" {
		t.Fatalf("after the retry: %q", got)
	}
	feed(map[string]any{"type": "system", "subtype": "api_retry", "attempt": 1.0, "max_retries": 10.0, "error_status": nil})
	if got := detail(); got != "Connection error · retrying (attempt 1 of 10)" {
		t.Fatalf("connection: %q", got)
	}
	feed(map[string]any{"type": "system", "subtype": "hook_started", "hook_event": "PreToolUse", "hook_name": "PreToolUse:Bash"})
	if got := detail(); got != "Running the PreToolUse hook: PreToolUse:Bash" {
		t.Fatalf("hook: %q", got)
	}
	feed(map[string]any{"type": "system", "subtype": "hook_response", "hook_event": "PreToolUse", "hook_name": "PreToolUse:Bash", "outcome": "success"})
	feed(map[string]any{"type": "system", "subtype": "permission_check_status", "tool_use_id": "toolu_1", "status": "checking"})
	if got := detail(); got != "Auto mode is checking this step" {
		t.Fatalf("check: %q", got)
	}
	feed(map[string]any{"type": "result", "subtype": "success"})
	m.mu.Lock()
	left := r.detail
	m.mu.Unlock()
	if left != "" {
		t.Fatalf("after the turn: %q", left)
	}

	mc, _, rc, _ := codexReady(t)
	cid := rc.session.ID
	mc.receive(cid, rc.generation, map[string]any{"method": "turn/started", "params": map[string]any{"turn": map[string]any{"id": "t1"}}})
	mc.receive(cid, rc.generation, map[string]any{"method": "item/autoApprovalReview/started", "params": map[string]any{"action": map[string]any{"type": "command", "command": `/usr/bin/zsh -lc "curl -s https://example.com"`}}})
	if got := mc.Telemetry(cid).Detail; got != "Auto-review is checking curl -s https://example.com" {
		t.Fatalf("review: %q", got)
	}
	mc.receive(cid, rc.generation, map[string]any{"method": "item/autoApprovalReview/completed", "params": map[string]any{"review": map[string]any{"status": "approved"}}})
	mc.receive(cid, rc.generation, map[string]any{"method": "hook/started", "params": map[string]any{"run": map[string]any{"eventName": "preToolUse"}}})
	if got := mc.Telemetry(cid).Detail; got != "Running the preToolUse hook" {
		t.Fatalf("codex hook: %q", got)
	}
}

func TestSettingsAndNamesFromTheCLI(t *testing.T) {
	m, _, r, _ := codexReady(t)
	id := r.session.ID
	m.receive(id, r.generation, map[string]any{"method": "thread/settings/updated", "params": map[string]any{"threadId": "th", "threadSettings": map[string]any{"model": "gpt-x", "effort": "high", "serviceTier": "priority", "collaborationMode": map[string]any{"mode": "plan"}, "sandboxPolicy": map[string]any{"type": "workspaceWrite"}, "approvalsReviewer": "auto_review"}}})
	m.mu.Lock()
	effective := r.effective
	m.mu.Unlock()
	if effective != (Options{Model: "gpt-x", Effort: "high", Speed: "priority", Mode: "plan", Permissions: codexAutoReview}) {
		t.Fatalf("settings: %+v", effective)
	}
	m.receive(id, r.generation, map[string]any{"method": "thread/name/updated", "params": map[string]any{"threadId": "th", "threadName": "Fix the login test"}})
	if events := eventsOf(m, id, "status"); str(events[len(events)-1]["title"]) != "Fix the login test" {
		t.Fatalf("codex title: %v", events[len(events)-1])
	}

	mc, _, rc := claudeReady(t)
	cid := rc.session.ID
	mc.receive(cid, rc.generation, map[string]any{"type": "system", "subtype": "session_title_changed", "title": "Release notes"})
	if events := eventsOf(mc, cid, "status"); str(events[len(events)-1]["title"]) != "Release notes" {
		t.Fatalf("claude title: %v", events[len(events)-1])
	}
}
