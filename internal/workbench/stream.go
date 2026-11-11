package workbench

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// notifier wakes every waiting reader when an event is persisted. Readers take
// the channel before querying, so an event appended during the query is never missed.
type notifier struct {
	mu sync.Mutex
	ch chan struct{}
}

func newNotifier() *notifier { return &notifier{ch: make(chan struct{})} }

func (n *notifier) C() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ch
}

func (n *notifier) Broadcast() {
	n.mu.Lock()
	close(n.ch)
	n.ch = make(chan struct{})
	n.mu.Unlock()
}

// delta is coalesced streaming text. Item identifies the native block so the
// browser can replace it with the completed native item.
type delta struct {
	Item, Channel, Parent, Text string
}

const (
	liveKeep = iota
	liveDrop
	liveDelta
)

// High-frequency or UI-irrelevant notifications. Telemetry reads them before
// this filter, so they are only kept out of the persisted event stream.
var codexDropped = map[string]bool{
	"item/reasoning/summaryPartAdded":           true,
	"item/plan/delta":                           true,
	"item/commandExecution/outputDelta":         true,
	"item/commandExecution/terminalInteraction": true,
	"item/fileChange/outputDelta":               true,
	"item/fileChange/patchUpdated":              true,
	"command/exec/outputDelta":                  true,
	"process/outputDelta":                       true,
	"turn/moderationMetadata":                   true,
	"thread/tokenUsage/updated":                 true,
	"account/rateLimits/updated":                true,
	"account/updated":                           true,
	"mcpServer/startupStatus/updated":           true,
	// Progress only: the status line reads it from memory (trackProgress).
	"thread/status/changed":         true,
	"thread/readState/changed":      true,
	"remoteControl/status/changed":  true,
	"model/safetyBuffering/updated": true,
}

// classifyLive decides whether a native message is persisted, dropped, or
// coalesced as streaming text. Unknown messages are kept so new CLI features
// still reach the browser's generic renderer.
func classifyLive(v map[string]any) (delta, int) {
	if method := str(v["method"]); method != "" {
		p := obj(v["params"])
		switch method {
		case "item/agentMessage/delta":
			return delta{Item: str(p["itemId"]), Channel: "text", Text: str(p["delta"])}, liveDelta
		case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
			return delta{Item: str(p["itemId"]), Channel: "thinking", Text: str(p["delta"])}, liveDelta
		}
		if codexDropped[method] {
			return delta{}, liveDrop
		}
		return delta{}, liveKeep
	}
	switch str(v["type"]) {
	case "stream_event":
		e := obj(v["event"])
		d := obj(e["delta"])
		item := str(v["api_message_id"])
		if item == "" {
			item = str(obj(e["message"])["id"])
		}
		item = fmt.Sprintf("%s:%v", item, e["index"])
		parent := str(v["parent_tool_use_id"])
		switch str(d["type"]) {
		case "text_delta":
			return delta{Item: item, Channel: "text", Parent: parent, Text: str(d["text"])}, liveDelta
		case "thinking_delta":
			return delta{Item: item, Channel: "thinking", Parent: parent, Text: str(d["thinking"])}, liveDelta
		}
		return delta{}, liveDrop
	case "rate_limit_event", "stderr", "tool_progress":
		return delta{}, liveDrop
	case "system":
		// Progress only: thinking token estimates and request status feed the
		// status line from memory (trackProgress) and are not worth storing.
		switch str(v["subtype"]) {
		case "thinking_tokens", "status":
			return delta{}, liveDrop
		}
	}
	return delta{}, liveKeep
}

func (m *Manager) bufferDelta(r *runtime, d delta) {
	if d.Text == "" {
		return
	}
	if r.buf.Text != "" && (r.buf.Item != d.Item || r.buf.Channel != d.Channel || r.buf.Parent != d.Parent) {
		m.flushDelta(r)
	}
	if r.buf.Text == "" {
		r.buf = d
	} else {
		r.buf.Text += d.Text
	}
	if r.bufTimer == nil {
		r.bufTimer = time.AfterFunc(40*time.Millisecond, func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.flushDelta(r)
		})
	}
}

func (m *Manager) flushDelta(r *runtime) {
	if r.bufTimer != nil {
		r.bufTimer.Stop()
		r.bufTimer = nil
	}
	if r.buf.Text == "" {
		return
	}
	d := r.buf
	r.buf = delta{}
	data := map[string]any{"text": d.Text, "channel": d.Channel}
	if d.Item != "" {
		data["item"] = d.Item
	}
	if d.Parent != "" {
		data["parent"] = d.Parent
	}
	m.emit(r.session.ID, "delta", data)
}

const (
	historyStringLimit = 32 * 1024
	historyImageLimit  = 512 * 1024
)

// boundJSON keeps native structure while capping long strings and inline images,
// so history pages stay small without a per-tool schema.
func boundJSON(v any) any {
	switch x := v.(type) {
	case string:
		if len(x) > historyStringLimit {
			return safeText(x, historyStringLimit) + fmt.Sprintf("\n[… %d bytes truncated. Full content remains in native history.]", len(x)-historyStringLimit)
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = boundJSON(e)
		}
		return out
	case map[string]any:
		if str(x["type"]) == "image" {
			source := obj(x["source"])
			if data := str(source["data"]); len(data) > historyImageLimit {
				return map[string]any{"type": "image", "omitted": true, "bytes": len(data) * 3 / 4, "media_type": source["media_type"]}
			}
			return x
		}
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = boundJSON(e)
		}
		return out
	}
	return v
}

// dropLiveTurnSummary removes the newest turn summary while that turn is still
// running here: the separate history reader reports it as interrupted.
func dropLiveTurnSummary(events []Event) []Event {
	for i := len(events) - 1; i >= 0; i-- {
		var v map[string]any
		if json.Unmarshal(events[i].Data, &v) != nil || v["method"] != "turn/completed" {
			continue
		}
		if str(obj(obj(v["params"])["turn"])["status"]) != "completed" {
			return append(events[:i:i], events[i+1:]...)
		}
		return events
	}
	return events
}
