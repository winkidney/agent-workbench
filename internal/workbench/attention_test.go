package workbench

import "testing"

func attentionNow(m *Manager, r *runtime) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return r.session.Attention
}

// lastStatusAttention is the attention the page last heard of.
func lastStatusAttention(m *Manager, id string) string {
	events := eventsOf(m, id, "status")
	if len(events) == 0 {
		return "(none)"
	}
	return str(events[len(events)-1]["attention"])
}

func TestPendingKinds(t *testing.T) {
	for _, c := range []struct {
		agent   string
		request map[string]any
		want    string
	}{
		{"codex", map[string]any{"method": "item/commandExecution/requestApproval"}, "approval"},
		{"codex", map[string]any{"method": "item/fileChange/requestApproval"}, "approval"},
		{"codex", map[string]any{"method": "item/tool/requestUserInput"}, "question"},
		{"codex", map[string]any{"method": "mcpServer/elicitation/request"}, "question"},
		{"claude", map[string]any{"request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash"}}, "approval"},
		{"claude", map[string]any{"request": map[string]any{"subtype": "can_use_tool", "tool_name": "AskUserQuestion"}}, "question"},
		{"claude", map[string]any{"request": map[string]any{"subtype": "can_use_tool", "tool_name": "ExitPlanMode"}}, "plan"},
	} {
		if got := pendingKind(c.agent, c.request); got != c.want {
			t.Errorf("%s %v: %q, want %q", c.agent, c.request, got, c.want)
		}
	}
}

func TestClaudeApprovalsAndQuestionsNeedTheUser(t *testing.T) {
	m, _, r := claudeReady(t)
	id := r.session.ID
	m.receive(id, r.generation, map[string]any{"type": "control_request", "request_id": "q1", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "AskUserQuestion", "input": map[string]any{}}})
	if attentionNow(m, r) != "question" || lastStatusAttention(m, id) != "question" {
		t.Fatalf("question: %q %q", attentionNow(m, r), lastStatusAttention(m, id))
	}
	// An approval outranks a question.
	m.receive(id, r.generation, map[string]any{"type": "control_request", "request_id": "a1", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{}}})
	if attentionNow(m, r) != "approval" {
		t.Fatalf("approval: %q", attentionNow(m, r))
	}
	if err := m.ReplyDecision(id, "a1", "allow", "", nil, ""); err != nil {
		t.Fatal(err)
	}
	if attentionNow(m, r) != "question" || lastStatusAttention(m, id) != "question" {
		t.Fatalf("after approving: %q %q", attentionNow(m, r), lastStatusAttention(m, id))
	}
	// The CLI withdrew it.
	m.receive(id, r.generation, map[string]any{"type": "control_cancel_request", "request_id": "q1"})
	if attentionNow(m, r) != "" || lastStatusAttention(m, id) != "" {
		t.Fatalf("withdrawn: %q %q", attentionNow(m, r), lastStatusAttention(m, id))
	}
}

func TestCodexAsyncQuestionsAndPlansNeedTheUser(t *testing.T) {
	m, p, r, _ := codexReady(t)
	id := r.session.ID
	respond := responder(t, m, p, r)
	respond("thread/goal/get", map[string]any{})
	turn := func(id2 string) {
		m.receive(id, r.generation, map[string]any{"method": "turn/started", "params": map[string]any{"turn": map[string]any{"id": id2}}})
	}
	done := func(id2 string) {
		m.receive(id, r.generation, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": id2, "status": "completed"}}})
	}

	// An async question in a reply waits for an answer, also after the turn.
	turn("t1")
	m.receive(id, r.generation, map[string]any{"method": "item/completed", "params": map[string]any{"item": map[string]any{"type": "agentMessage", "text": "Which one?", "delivery": "async", "questions": []any{map[string]any{"title": "Pick", "options": []any{"a", "b"}}}}}})
	if attentionNow(m, r) != "question" || lastStatusAttention(m, id) != "question" {
		t.Fatalf("async question: %q %q", attentionNow(m, r), lastStatusAttention(m, id))
	}
	done("t1")
	if attentionNow(m, r) != "question" {
		t.Fatalf("after the turn: %q", attentionNow(m, r))
	}
	// The listed sessions carry it.
	list, err := m.NativeList()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range list {
		if s.ID == id {
			found = s.Attention == "question"
		}
	}
	if !found {
		t.Fatal("the session list does not show the question")
	}
	// Answering (any message) clears it.
	if err := m.Send(id, "a"); err != nil {
		t.Fatal(err)
	}
	if attentionNow(m, r) != "" {
		t.Fatalf("answered: %q", attentionNow(m, r))
	}

	// A plan-mode turn that ends with a plan awaits a decision.
	turn("t2")
	m.receive(id, r.generation, map[string]any{"method": "item/completed", "params": map[string]any{"item": map[string]any{"type": "plan", "text": "1. Do it"}}})
	if attentionNow(m, r) != "" {
		t.Fatalf("plan during the turn: %q", attentionNow(m, r))
	}
	done("t2")
	if attentionNow(m, r) != "plan" || lastStatusAttention(m, id) != "plan" {
		t.Fatalf("plan: %q %q", attentionNow(m, r), lastStatusAttention(m, id))
	}
	if err := m.DismissPlan(id); err != nil {
		t.Fatal(err)
	}
	if attentionNow(m, r) != "" || lastStatusAttention(m, id) != "" {
		t.Fatalf("plan dismissed: %q %q", attentionNow(m, r), lastStatusAttention(m, id))
	}
	// An interrupted plan turn asks nothing.
	turn("t3")
	m.receive(id, r.generation, map[string]any{"method": "item/completed", "params": map[string]any{"item": map[string]any{"type": "plan", "text": "1. Do it"}}})
	m.receive(id, r.generation, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": "t3", "status": "interrupted"}}})
	if attentionNow(m, r) != "" {
		t.Fatalf("interrupted plan: %q", attentionNow(m, r))
	}

	// A command approval while the turn runs, gone when answered.
	turn("t4")
	m.receive(id, r.generation, map[string]any{"id": 7.0, "method": "item/commandExecution/requestApproval", "params": map[string]any{"command": "curl example.com"}})
	if attentionNow(m, r) != "approval" {
		t.Fatalf("approval: %q", attentionNow(m, r))
	}
	if err := m.ReplyDecision(id, "7", "allow", "", nil, ""); err != nil {
		t.Fatal(err)
	}
	if attentionNow(m, r) != "" || lastStatusAttention(m, id) != "" {
		t.Fatalf("approved: %q %q", attentionNow(m, r), lastStatusAttention(m, id))
	}
}

func TestAttentionLeavesTheIdleTimerAlone(t *testing.T) {
	m, _, r, _ := codexReady(t)
	id := r.session.ID
	m.mu.Lock()
	before := r.last
	r.asking = true
	m.syncAttention(r)
	after := r.last
	m.mu.Unlock()
	if !after.Equal(before) || lastStatusAttention(m, id) != "question" {
		t.Fatalf("last %v → %v, attention %q", before, after, lastStatusAttention(m, id))
	}
}
