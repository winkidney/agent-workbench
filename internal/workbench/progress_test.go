package workbench

import (
	"encoding/json"
	"testing"
	"time"
)

func feed(t *testing.T, m *Manager, r *runtime, raw string) {
	t.Helper()
	v := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	m.track(r, v)
}

func TestProgressFollowsClaudeThinkingWithoutText(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("claude", t.TempDir(), "p")
	r := m.sessions[s.ID]
	r.turn, r.began, r.phase = true, time.Now(), "requesting"
	steps := []struct {
		event  string
		phase  string
		tokens float64
	}{
		// As recorded from Claude Code: thinking arrives only as estimates.
		{`{"type":"system","subtype":"status","status":"requesting"}`, "requesting", 0},
		{`{"type":"system","subtype":"thinking_tokens","estimated_tokens":50,"estimated_tokens_delta":50}`, "thinking", 50},
		{`{"type":"system","subtype":"thinking_tokens","estimated_tokens":150,"estimated_tokens_delta":100}`, "thinking", 150},
		// Streamed thinking text is already counted by its estimates.
		{`{"type":"stream_event","event":{"index":0,"delta":{"type":"thinking_delta","thinking":"plan it"}}}`, "thinking", 150},
		{`{"type":"stream_event","event":{"index":1,"delta":{"type":"text_delta","text":"abcdefgh"}}}`, "responding", 152},
		{`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"x","name":"Bash","input":{}}]}}`, "tool", 152},
		{`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"x","content":"ok"}]}}`, "requesting", 152},
		{`{"type":"result","usage":{"output_tokens":160}}`, "", 152},
	}
	for i, s := range steps {
		feed(t, m, r, s.event)
		if r.phase != s.phase || r.turnOut != s.tokens {
			t.Fatalf("step %d: phase %q tokens %v", i, r.phase, r.turnOut)
		}
	}
	if m.Telemetry(s.ID).Speed == 0 {
		t.Fatal("thinking tokens left the live speed at zero")
	}
}

func TestProgressFollowsCodexItems(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("codex", t.TempDir(), "p")
	r := m.sessions[s.ID]
	r.turn, r.began, r.baseline = true, time.Now(), 1000
	for i, s := range []struct {
		event  string
		phase  string
		tokens float64
	}{
		{`{"method":"item/started","params":{"item":{"type":"reasoning","id":"r"}}}`, "thinking", 0},
		{`{"method":"item/reasoning/summaryTextDelta","params":{"itemId":"r","delta":"abcd"}}`, "thinking", 1},
		{`{"method":"item/started","params":{"item":{"type":"commandExecution","id":"c"}}}`, "tool", 1},
		{`{"method":"item/completed","params":{"item":{"type":"commandExecution","id":"c"}}}`, "requesting", 1},
		// Reported usage (total minus the turn's baseline) wins once larger.
		{`{"method":"thread/tokenUsage/updated","params":{"tokenUsage":{"total":{"outputTokens":1300},"last":{}}}}`, "requesting", 300},
		{`{"method":"item/agentMessage/delta","params":{"itemId":"m","delta":"abcdefgh"}}`, "responding", 302},
	} {
		feed(t, m, r, s.event)
		if r.phase != s.phase || r.turnOut != s.tokens {
			t.Fatalf("step %d: phase %q tokens %v", i, r.phase, r.turnOut)
		}
	}
	got := m.Telemetry(s.ID)
	if got.TurnTokens != 302 || got.Phase != "responding" {
		t.Fatalf("telemetry: %v %q", got.TurnTokens, got.Phase)
	}
	feed(t, m, r, `{"method":"turn/completed","params":{"turn":{"status":"completed"}}}`)
	if r.phase != "" {
		t.Fatalf("phase after turn: %q", r.phase)
	}
	r.turn = false
	if got := m.Telemetry(s.ID); got.TurnTokens != 0 || got.Phase != "" {
		t.Fatalf("idle telemetry still shows progress: %+v", got)
	}
}

func TestSendShowsMcpStartupAndNeverFailsOnASlowServer(t *testing.T) {
	m, p := fixture(t)
	s, _ := m.Create("codex", t.TempDir(), "mcp")
	_ = m.Start(s.ID)
	r := m.sessions[s.ID]
	gen := r.generation
	m.receive(s.ID, gen, map[string]any{"id": "init", "result": map[string]any{}})
	m.receive(s.ID, gen, map[string]any{"id": "thread", "result": map[string]any{"thread": map[string]any{"id": "n"}}})
	mcp := func(name, status string) {
		m.receive(r.session.ID, gen, map[string]any{"method": "mcpServer/startupStatus/updated", "params": map[string]any{"name": name, "status": status}})
	}
	mcp("slow", "starting")
	sent := func() int {
		p.mu.Lock()
		defer p.mu.Unlock()
		n := 0
		for _, in := range p.in {
			if in["method"] == "turn/start" {
				n++
			}
		}
		return n
	}
	done := make(chan error, 1)
	go func() { done <- m.Send(r.session.ID, "hello") }()
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for " + what)
			}
		}
	}
	waitFor("starting status", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return r.session.Status == "starting"
	})
	if got := m.Telemetry(r.session.ID); got.Phase != "mcp" || got.McpDone != 0 || got.McpTotal != 1 || sent() != 0 {
		t.Fatalf("while waiting: %+v sent=%d", got, sent())
	}
	mcp("slow", "ready")
	if err := <-done; err != nil || sent() != 1 {
		t.Fatalf("after ready: %v sent=%d", err, sent())
	}
	m.mu.Lock()
	status, phase := r.session.Status, r.phase
	m.mu.Unlock()
	if status != "running" || phase != "requesting" {
		t.Fatalf("turn: %s %s", status, phase)
	}

	// A server that never finishes starting does not fail the message.
	m.receive(r.session.ID, gen, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})
	mcp("stuck", "starting")
	old := mcpWait
	mcpWait = 50 * time.Millisecond
	t.Cleanup(func() { mcpWait = old })
	if err := m.Send(r.session.ID, "again"); err != nil || sent() != 2 {
		t.Fatalf("stuck server: %v sent=%d", err, sent())
	}
}
