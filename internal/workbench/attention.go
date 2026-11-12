package workbench

import "strings"

// Attention: what a session waits for from the user, so the page can point to
// it from anywhere (the sidebar, the tab title, a desktop notification).
//
//   - approval: a tool, command or file change awaits approval
//   - question: the agent asked something (Claude AskUserQuestion, Codex
//     input requests and its async questions, which Codex may wait for
//     while the turn runs or after it ended)
//   - plan: a plan awaits a decision (Claude ExitPlanMode; a Codex plan-mode
//     turn that ended with a plan, "Implement this plan?")
//
// It is computed from the runtime, never stored: pending requests end with
// the engine, and the async-question and plan flags with the next message.

var attentionRank = map[string]int{"": 0, "plan": 1, "question": 2, "approval": 3}

// pendingKind is what a pending native request asks of the user.
func pendingKind(agent string, request map[string]any) string {
	if agent == "codex" {
		method := str(request["method"])
		if strings.Contains(method, "requestUserInput") || strings.Contains(method, "elicitation") {
			return "question"
		}
		return "approval"
	}
	req := obj(request["request"])
	switch str(req["tool_name"]) {
	case "AskUserQuestion":
		return "question"
	case "ExitPlanMode":
		return "plan"
	}
	if str(req["subtype"]) != "" && str(req["subtype"]) != "can_use_tool" {
		return "question"
	}
	return "approval"
}

func attentionOf(r *runtime) string {
	best := ""
	pick := func(kind string) {
		if attentionRank[kind] > attentionRank[best] {
			best = kind
		}
	}
	for _, v := range r.pending {
		pick(pendingKind(r.session.Agent, obj(v)))
	}
	if r.asking {
		pick("question")
	}
	if r.planReady {
		pick("plan")
	}
	return best
}

// syncAttention tells the page when what the session waits for changed
// without a status change (m.mu held). It leaves r.last alone, so a waiting
// session does not stay awake longer for it.
func (m *Manager) syncAttention(r *runtime) {
	if a := attentionOf(r); a != r.session.Attention {
		r.session.Attention = a
		m.emit(r.session.ID, "status", r.session)
	}
}

// trackAttention follows the Codex records that leave something for the
// user once the request itself is gone (m.mu held).
func trackAttention(r *runtime, method string, params map[string]any) {
	switch method {
	case "turn/started":
		r.planSeen, r.planReady = false, false
	case "item/completed":
		item := obj(params["item"])
		switch str(item["type"]) {
		case "agentMessage":
			if str(item["delivery"]) == "async" && len(asList(item["questions"])) > 0 {
				r.asking = true
			}
		case "plan":
			if strings.TrimSpace(str(item["text"])) != "" {
				r.planSeen = true
			}
		}
	case "turn/completed":
		r.planReady = r.planSeen && str(obj(params["turn"])["status"]) == "completed"
		r.planSeen = false
	}
}

// userActed clears what the user answered by writing to the session: a
// message, a steer or an interrupt answers or sets aside the async questions
// and the plan decision (m.mu held).
func userActed(r *runtime) {
	r.asking, r.planReady = false, false
}

// DismissPlan records that the user chose about a Codex plan in the page
// (keep planning, or implement it in a new session).
func (m *Manager) DismissPlan(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(id)
	if r == nil {
		return nil
	}
	r.planReady = false
	m.syncAttention(r)
	return nil
}
