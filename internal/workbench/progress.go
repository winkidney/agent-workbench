package workbench

import (
	"fmt"
	"regexp"
	"time"
)

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

// trackDetail follows what a turn waits on that its phase does not say: an
// API retry, an automatic permission check, a hook, an auto-review.
func trackDetail(r *runtime, v map[string]any) {
	set := func(kind, text string) { r.detailKind, r.detail = kind, text }
	clear := func(kinds ...string) {
		for _, k := range kinds {
			if r.detailKind == k {
				r.detailKind, r.detail = "", ""
			}
		}
	}
	p := obj(v["params"])
	switch str(v["method"]) {
	case "item/autoApprovalReview/started":
		set("review", "Auto-review is checking "+reviewTarget(obj(p["action"])))
	case "item/autoApprovalReview/completed":
		clear("review")
	case "hook/started":
		set("hook", "Running the "+str(obj(p["run"])["eventName"])+" hook")
	case "hook/completed":
		clear("hook")
	case "turn/completed":
		clear("review", "hook", "retry", "check")
	}
	switch str(v["type"]) {
	case "system":
		switch str(v["subtype"]) {
		case "api_retry":
			what := "Connection error"
			if s, ok := v["error_status"].(float64); ok {
				what = fmt.Sprintf("API error %d", int(s))
			}
			set("retry", fmt.Sprintf("%s · retrying (attempt %d of %d)", what, int(num(v["attempt"])), int(num(v["max_retries"]))))
		case "permission_check_status":
			if str(v["status"]) == "checking" {
				set("check", "Auto mode is checking this step")
			} else {
				clear("check")
			}
		case "hook_started":
			text := "Running the " + str(v["hook_event"]) + " hook"
			if name := str(v["hook_name"]); name != "" && name != str(v["hook_event"]) {
				text += ": " + name
			}
			set("hook", text)
		case "hook_response":
			clear("hook")
		}
	case "stream_event", "assistant":
		clear("retry")
	case "result":
		clear("review", "hook", "retry", "check")
	}
}

var shellWrapped = regexp.MustCompile(`^(?:/\S*/)?(?:ba|z)?sh\s+-l?c\s+(['"])([\s\S]*)['"]$`)

// reviewTarget names what an auto-review looks at.
func reviewTarget(action map[string]any) string {
	switch str(action["type"]) {
	case "command":
		cmd := str(action["command"])
		if m := shellWrapped.FindStringSubmatch(cmd); m != nil {
			cmd = m[2]
		}
		if len(cmd) > 80 {
			cmd = cmd[:79] + "…"
		}
		return cmd
	case "networkAccess":
		return "network access to " + str(action["host"])
	case "applyPatch":
		return "a file change"
	case "mcpToolCall":
		return str(action["server"]) + " · " + str(action["toolName"])
	}
	return "a request"
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}
