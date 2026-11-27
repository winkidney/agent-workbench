package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Claude Code writes the same event two ways: as stream-json on a running
// CLI's stdout, and as a line of its transcript (JSONL). A subagent's report
// is a task_notification live, and a queued_command attachment or a user
// record in the transcript. claudeView reads both into one record for the
// page (ADR 0025), so the live view and history show the same thing under
// the same key, and the turn's record can replace the live view
// (transcript/reducer.ts reconcile).
//
// What it makes:
//   - workbench_peer: a message from another session, or a subagent's report
//     (key, from, name, handback, body as Markdown).
//   - workbench_task: a background task the model was told had ended (key,
//     taskId, status, summary, toolUses, tokens, durationMs, detail); only
//     from the transcript.
//   - a mid-turn message the user sent: a steered user record.
//
// Every other record is returned as it is.

var (
	agentMessageRE = regexp.MustCompile(`(?s)<agent-message([^>]*)>(.*?)</agent-message>`)
	agentFromRE    = regexp.MustCompile(`from="([^"]+)"`)
	handbackRE     = regexp.MustCompile(`(?s)^\s*\[Subagent hand-back\].*?The report follows:\n`)
	// A message from another session without origin metadata (older CLIs).
	peerTextRE = regexp.MustCompile(`^\s*(?:Another [^\n]* sent a message[^\n:]*:\s*)?<agent-message[\s>]`)
	taskTextRE = regexp.MustCompile(`^\s*<task-notification>`)
)

// claudeView is the record the page shows for one Claude record, live or
// from the transcript; nil when it shows nothing.
func claudeView(v map[string]any) map[string]any {
	switch str(v["type"]) {
	case "system":
		if str(v["subtype"]) == "task_notification" {
			return liveTaskNotification(v)
		}
	case "attachment":
		a := obj(v["attachment"])
		if str(a["type"]) == "queued_command" {
			return queuedCommand(v, a)
		}
	case "user":
		origin := obj(v["origin"])
		raw := userText(obj(v["message"])["content"])
		switch kind := str(origin["kind"]); {
		case kind == "peer" || (kind == "" && peerTextRE.MatchString(raw)):
			return peerView(v, origin, raw)
		case kind == "task-notification" || (kind == "" && taskTextRE.MatchString(raw)):
			return taskView(v, raw)
		}
	}
	return v
}

// liveTaskNotification: a task ended. The model hears of only some of them,
// and the transcript records only those, so live shows a subagent's report
// alone, which always reaches the transcript as a message from it. The other
// notifications show from the transcript once the turn's record replaces
// the live view.
func liveTaskNotification(v map[string]any) map[string]any {
	report := str(obj(v["handback_report"])["text"])
	id := str(v["task_id"])
	if report == "" || id == "" {
		return nil
	}
	return viewOf(v, map[string]any{
		"type":     "workbench_peer",
		"key":      reportKey(id, report),
		"from":     id,
		"handback": true,
		"body":     strings.TrimSpace(report),
	})
}

// queuedCommand: a message taken in mid-turn. The user's own is a steered
// user record; another session's or a subagent's, and a task notification,
// are what they are.
func queuedCommand(v, a map[string]any) map[string]any {
	prompt := str(a["prompt"])
	origin := obj(a["origin"])
	switch mode := str(a["commandMode"]); {
	case mode == "task-notification" || str(origin["kind"]) == "task-notification":
		return taskView(v, prompt)
	case mode != "" && mode != "prompt":
		noteClaudeShape("queued_command mode " + mode)
		return nil
	case str(origin["kind"]) == "peer" || (str(origin["kind"]) == "" && peerTextRE.MatchString(prompt)):
		return peerView(v, origin, prompt)
	}
	if k := str(origin["kind"]); k != "" && k != "human" {
		noteClaudeShape("queued_command origin " + k)
	}
	return viewOf(v, map[string]any{
		"type":    "user",
		"steered": true,
		"message": map[string]any{"role": "user", "content": boundJSON(a["prompt"])},
	})
}

// peerView: a message from another session or a subagent's report. A
// subagent's report keeps its own copy in handbackReportText; otherwise it
// is taken out of the <agent-message>, without the harness's hand-back
// preamble and the indent it adds.
func peerView(v, origin map[string]any, raw string) map[string]any {
	m := agentMessageRE.FindStringSubmatch(raw)
	from := str(origin["from"])
	if from == "" && m != nil {
		if f := agentFromRE.FindStringSubmatch(m[1]); f != nil {
			from = f[1]
		}
	}
	body := str(origin["handbackReportText"])
	handback := origin["handback"] == true || body != ""
	if body == "" && !handback {
		body = str(origin["body"])
	}
	if body == "" && m != nil {
		inner := m[2]
		if pre := handbackRE.FindString(inner); pre != "" {
			handback = true
			lines := strings.Split(inner[len(pre):], "\n")
			for i, l := range lines {
				lines[i] = strings.TrimPrefix(l, "  ")
			}
			inner = strings.Join(lines, "\n")
		}
		body = inner
	}
	// A subagent's report has the same key live and in the transcript.
	body = strings.TrimSpace(body)
	key := "peer:" + str(v["uuid"])
	if id := str(origin["senderTaskId"]); handback && id != "" {
		key = reportKey(id, body)
	} else if handback && from != "" {
		key = reportKey(from, body)
	}
	out := map[string]any{
		"type":     "workbench_peer",
		"key":      key,
		"from":     from,
		"handback": handback,
		"body":     body,
	}
	if name := str(origin["name"]); name != "" {
		out["name"] = name
	}
	return viewOf(v, out)
}

// reportKey names a subagent's report the same live and in the transcript:
// its task, and its text, since a subagent that is resumed reports again.
func reportKey(task, report string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(report)))
	return "peer:" + task + ":" + hex.EncodeToString(sum[:4])
}

// taskView: the harness telling the model a background task ended.
func taskView(v map[string]any, raw string) map[string]any {
	num := func(names ...string) int64 {
		for _, n := range names {
			if x, err := strconv.ParseInt(strings.TrimSpace(xmlTag(raw, n)), 10, 64); err == nil {
				return x
			}
		}
		return 0
	}
	id := strings.TrimSpace(xmlTag(raw, "task-id"))
	status := strings.TrimSpace(xmlTag(raw, "status"))
	summary := strings.TrimSpace(xmlTag(raw, "summary"))
	if summary == "" && id == "" {
		// A plain line (a plugin's notice) instead of the task's fields.
		summary = clip(strings.Join(strings.Fields(xmlTag(raw, "task-notification")), " "), 200)
	}
	out := map[string]any{
		"type": "workbench_task",
		// Only the transcript shows it (liveTaskNotification), and a task
		// that is resumed notifies again: its record names it.
		"key":     "task:" + str(v["uuid"]),
		"taskId":  id,
		"status":  status,
		"summary": summary,
		"detail":  raw,
	}
	if n := num("tool_uses"); n > 0 {
		out["toolUses"] = n
	}
	if n := num("subagent_tokens", "total_tokens"); n > 0 {
		out["tokens"] = n
	}
	if n := num("duration_ms"); n > 0 {
		out["durationMs"] = n
	}
	return viewOf(v, out)
}

// viewOf adds what every record carries: its time and whether it is history.
func viewOf(v, out map[string]any) map[string]any {
	for _, k := range []string{"uuid", "timestamp", "history"} {
		if v[k] != nil {
			out[k] = v[k]
		}
	}
	return out
}

func xmlTag(s, name string) string {
	open, end := "<"+name+">", "</"+name+">"
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, end)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// userText is a user message's text, for telling what the harness sent.
func userText(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	parts := []string{}
	for _, c := range asList(content) {
		if b := obj(c); str(b["type"]) == "text" {
			parts = append(parts, str(b["text"]))
		}
	}
	return strings.Join(parts, "\n")
}

// claudeLiveKnown: the live record types the page shows or deliberately
// leaves out. Another type is logged once, so a new record shape from the
// CLI is noticed rather than lost (ADR 0025).
var claudeLiveKnown = map[string]bool{
	"assistant": true, "user": true, "result": true,
	"control_request": true, "control_response": true, "control_cancel_request": true,
	"command_lifecycle": true, "conversation_reset": true,
	"stream_event": true, "rate_limit_event": true, "tool_progress": true, "prompt_suggestion": true, "stderr": true,
	"system/init": true, "system/hook_started": true, "system/hook_response": true,
	"system/task_started": true, "system/task_progress": true, "system/task_updated": true, "system/task_notification": true,
	"system/background_tasks_changed": true, "system/commands_changed": true, "system/permission_check_status": true,
	"system/permission_denied": true, "system/session_title_changed": true, "system/ui_log": true,
	"system/vcs_state_changed": true, "system/informational": true, "system/notification": true,
	"system/model_fallback": true, "system/model_refusal_fallback": true, "system/model_consent_fallback": true,
	"system/instruction_size_warning": true, "system/compact_boundary": true, "system/local_command": true,
	"system/thinking_tokens": true, "system/status": true, "system/api_retry": true,
}

// noteClaudeLive logs a live record type claudeLiveKnown does not list.
func noteClaudeLive(v map[string]any) {
	t := str(v["type"])
	if t == "" {
		return
	}
	if t == "system" {
		t += "/" + str(v["subtype"])
	}
	if !claudeLiveKnown[t] {
		noteClaudeShape("live " + t)
	}
}

var claudeShapes sync.Map

// noteClaudeShape logs a Claude record shape the workbench does not read,
// once per process: its type only, never its content.
func noteClaudeShape(shape string) {
	if _, seen := claudeShapes.LoadOrStore(shape, true); !seen {
		log.Printf("claude: a record the page does not read yet: %s", shape)
	}
}
