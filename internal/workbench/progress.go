package workbench

import "time"

// trackProgress keeps the live status line moving between visible output,
// like the CLIs' own spinners: an estimate of the tokens produced this turn
// and what the agent is doing (thinking, responding, running a tool,
// waiting for the model). Thinking counts even when its text is not shown:
// Claude reports it as token estimates, and Codex may stream no reasoning
// text at all.
func trackProgress(r *runtime, v map[string]any) {
	add := func(tokens float64) {
		if tokens > 0 {
			r.turnOut += tokens
			r.samples = append(r.samples, sample{time.Now(), tokens})
		}
	}
	p := obj(v["params"])
	switch str(v["method"]) {
	case "item/agentMessage/delta":
		add(estimate(str(p["delta"])))
		r.phase = "responding"
	case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
		add(estimate(str(p["delta"])))
		r.phase = "thinking"
	case "item/started":
		switch str(obj(p["item"])["type"]) {
		case "reasoning":
			r.phase = "thinking"
		case "agentMessage":
			r.phase = "responding"
		case "userMessage":
		default:
			r.phase = "tool"
		}
	case "item/completed":
		switch str(obj(p["item"])["type"]) {
		case "reasoning", "agentMessage", "userMessage":
		default:
			// A tool finished; the model is next.
			r.phase = "requesting"
		}
	case "thread/tokenUsage/updated":
		// Reported usage replaces the estimate once it is larger.
		if total, ok := obj(obj(p["tokenUsage"])["total"])["outputTokens"].(float64); ok && total-r.baseline > r.turnOut {
			r.turnOut = total - r.baseline
		}
	case "turn/completed":
		r.phase = ""
	}
	switch str(v["type"]) {
	case "stream_event":
		d := obj(obj(v["event"])["delta"])
		switch str(d["type"]) {
		case "text_delta":
			add(estimate(str(d["text"])))
			r.phase = "responding"
		case "thinking_delta":
			// Counted by the thinking_tokens estimates Claude sends alongside.
			r.phase = "thinking"
		}
	case "system":
		switch str(v["subtype"]) {
		case "thinking_tokens":
			d, _ := v["estimated_tokens_delta"].(float64)
			add(d)
			r.phase = "thinking"
		case "status":
			switch str(v["status"]) {
			case "requesting":
				r.phase = "requesting"
			case "compacting":
				r.phase = "compacting"
			}
		}
	case "assistant":
		for _, c := range asList(obj(v["message"])["content"]) {
			if str(obj(c)["type"]) == "tool_use" {
				r.phase = "tool"
			}
		}
	case "user":
		for _, c := range asList(obj(v["message"])["content"]) {
			if str(obj(c)["type"]) == "tool_result" {
				r.phase = "requesting"
			}
		}
	case "result":
		r.phase = ""
	}
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}
