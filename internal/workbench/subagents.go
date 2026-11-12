package workbench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Subagents in the parent's conversation: the steps a subagent takes show
// as lines under the row that started it, live and in history alike, and
// the row links to the subagent's own transcript.
//
// Claude writes a subagent's records to <session>/subagents/agent-<id>.jsonl
// (with agent-<id>.meta.json naming the Agent call, toolUseId) rather than to
// the session's transcript; live, stream-json forwards the subagent's tool
// calls and results with parent_tool_use_id, and its permission requests
// carry agent_id. Codex runs a subagent as a thread of its own (spawned with
// a subAgentActivity item in the parent) and sends that thread's records,
// and its approval requests, on the parent's connection with the child's
// threadId.
//
// Only what those lines show is kept: the tool's name, a bounded input and
// its status, for the latest subagentTools calls of each subagent. Nothing
// is cached: the files and threads are read when a history page asks.

const (
	// Tool calls kept per subagent, here and in the page (reducer.ts).
	subagentTools = 200
	// Bytes of subagent lines one history page carries at most.
	subagentPageBytes = 1 << 20
	// A string in a child line's input, and a result's text.
	subagentInput  = 1024
	subagentOutput = 300
)

// claudeSubagentDir holds a Claude session's subagent transcripts.
func claudeSubagentDir(mainPath string) string {
	return filepath.Join(strings.TrimSuffix(mainPath, ".jsonl"), "subagents")
}

// claudeSubagentSession is the sidebar's session for a Claude subagent (see
// readClaudeSubagent).
func claudeSubagentSession(parentNative, agentID string) string {
	return nativeKey("claude", parentNative+":agent-"+agentID)
}

type claudeSubagentMeta struct {
	ToolUseID   string `json:"toolUseId"`
	Description string `json:"description"`
	AgentType   string `json:"agentType"`
}

func readClaudeSubagentMeta(dir, agentID string) (claudeSubagentMeta, bool) {
	var meta claudeSubagentMeta
	if agentID == "" || strings.ContainsAny(agentID, `/\`) {
		return meta, false
	}
	b, err := os.ReadFile(filepath.Join(dir, "agent-"+agentID+".meta.json"))
	return meta, err == nil && json.Unmarshal(b, &meta) == nil
}

// claudeSubagentsByTool maps Agent calls to the subagents they started.
func claudeSubagentsByTool(dir string) map[string]string {
	out := map[string]string{}
	files, _ := filepath.Glob(filepath.Join(dir, "agent-*.meta.json"))
	for _, f := range files {
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "agent-"), ".meta.json")
		if meta, ok := readClaudeSubagentMeta(dir, id); ok && meta.ToolUseID != "" {
			out[meta.ToolUseID] = id
		}
	}
	return out
}

// clip shortens s to at most n bytes, on a character boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return safeText(s, n) + "…"
}

// slimValue bounds a value for a child line: short strings, few items, and
// no deep nesting.
func slimValue(v any, depth int) any {
	switch x := v.(type) {
	case string:
		return clip(x, subagentInput)
	case []any:
		if depth > 3 {
			return nil
		}
		n := len(x)
		if n > 20 {
			n = 20
		}
		out := make([]any, 0, n)
		for _, e := range x[:n] {
			out = append(out, slimValue(e, depth+1))
		}
		return out
	case map[string]any:
		if depth > 3 {
			return nil
		}
		out := make(map[string]any, len(x))
		for k, e := range x {
			if len(out) >= 30 {
				break
			}
			out[k] = slimValue(e, depth+1)
		}
		return out
	}
	return v
}

// resultHead is the start of a tool result's text, decoded from its JSON
// (a string, or text blocks) without reading the rest: results can be
// megabytes, of which a child line needs a few hundred bytes.
type resultHead string

func (h *resultHead) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '[' {
		// The first text block's text.
		at := bytes.Index(b, []byte(`"text":`))
		if at < 0 {
			*h = ""
			return nil
		}
		b = bytes.TrimLeft(b[at+len(`"text":`):], " \t\r\n")
	}
	*h = resultHead(unquoteHead(b, subagentOutput))
	return nil
}

// unquoteHead decodes the first n bytes of the JSON string at the start of
// b (empty when b is not a string).
func unquoteHead(b []byte, n int) string {
	if len(b) == 0 || b[0] != '"' {
		return ""
	}
	var out strings.Builder
	for i := 1; i < len(b) && out.Len() < n; i++ {
		c := b[i]
		if c == '"' {
			break
		}
		if c != '\\' || i+1 >= len(b) {
			out.WriteByte(c)
			continue
		}
		i++
		switch b[i] {
		case 'n':
			out.WriteByte('\n')
		case 't':
			out.WriteByte('\t')
		case 'r', 'b', 'f':
			out.WriteByte(' ')
		case 'u':
			if i+4 < len(b) {
				if v, err := strconv.ParseUint(string(b[i+1:i+5]), 16, 32); err == nil {
					r := rune(v)
					if utf16.IsSurrogate(r) {
						r = utf8.RuneError
					}
					out.WriteRune(r)
				}
				i += 4
			}
		default:
			out.WriteByte(b[i])
		}
	}
	return clip(out.String(), n)
}

// The marks of tool calls and results in a record, as Claude writes them
// (compact) and as other JSON writers may.
var (
	callMarks   = [][]byte{[]byte(`"type":"tool_use"`), []byte(`"type": "tool_use"`)}
	resultMarks = [][]byte{[]byte(`"type":"tool_result"`), []byte(`"type": "tool_result"`)}
	// Bytes kept across chunks: one less than the longest mark.
	markCarry = len(`"type": "tool_result"`) - 1
)

// scanLine reads one line through r without holding it (a line can be far
// longer than r's buffer): its length, how many tool calls it has, and
// whether it has a tool result.
func scanLine(r *bufio.Reader) (length, calls int, result bool, err error) {
	var carryBuf, joinBuf [64]byte
	carry := carryBuf[:0]
	for {
		chunk, rerr := r.ReadSlice('\n')
		length += len(chunk)
		for _, m := range callMarks {
			calls += bytes.Count(chunk, m)
		}
		for _, m := range resultMarks {
			result = result || bytes.Contains(chunk, m)
		}
		if len(carry) > 0 && len(chunk) > 0 {
			// Marks across the previous chunk's end.
			joined := append(append(joinBuf[:0], carry...), chunk[:min(len(chunk), markCarry)]...)
			for _, m := range callMarks {
				calls += across(joined, len(carry), m)
			}
			for _, m := range resultMarks {
				result = result || across(joined, len(carry), m) > 0
			}
		}
		if rerr == bufio.ErrBufferFull {
			// The last bytes seen so far, however many chunks they span.
			joined := append(append(joinBuf[:0], carry...), chunk[max(0, len(chunk)-markCarry):]...)
			carry = append(carryBuf[:0], joined[max(0, len(joined)-markCarry):]...)
			continue
		}
		return length, calls, result, rerr
	}
}

// across counts the marks in joined that begin before n and end after it.
func across(joined []byte, n int, mark []byte) int {
	count := 0
	for i := 0; i < n; i++ {
		if i+len(mark) > n && bytes.HasPrefix(joined[i:], mark) {
			count++
		}
	}
	return count
}

type lineSpan struct {
	offset int64
	length int
	calls  int
}

// claudeSubagentTail reads the latest subagentTools tool calls of a Claude
// subagent transcript, and their results, as history events nested under
// the Agent call toolUseID, within budget bytes. It keeps only the offsets
// of the last lines with tool calls or results while it scans, so its
// memory does not grow with the file. earlier counts the calls left out.
func claudeSubagentTail(path, toolUseID string, s Session, budget *int) (events []Event, earlier int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	ring := make([]lineSpan, 2*subagentTools)
	kept, next, total := 0, 0, 0
	var offset int64
	reader := bufio.NewReaderSize(f, 65536)
	for {
		length, calls, result, rerr := scanLine(reader)
		if length > 0 {
			if calls > 0 || result {
				ring[next] = lineSpan{offset, length, calls}
				next = (next + 1) % len(ring)
				if kept < len(ring) {
					kept++
				}
				total += calls
			}
			offset += int64(length)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, 0, rerr
		}
	}
	// Oldest first, from the line where the last subagentTools calls begin.
	spans := make([]lineSpan, 0, kept)
	for i := 0; i < kept; i++ {
		spans = append(spans, ring[(next-kept+i+len(ring))%len(ring)])
	}
	start, calls := len(spans), 0
	for start > 0 && calls+spans[start-1].calls <= subagentTools {
		start--
		calls += spans[start].calls
	}
	earlier = total - calls
	// A result shows only under a call that is shown.
	shown := map[string]bool{}
	var buf []byte
	for _, sp := range spans[start:] {
		if sp.length > 16*1024*1024 {
			continue
		}
		if cap(buf) < sp.length {
			buf = make([]byte, sp.length)
		}
		line := buf[:sp.length]
		if _, err := f.ReadAt(line, sp.offset); err != nil && !errors.Is(err, io.EOF) {
			return events, earlier, err
		}
		var rec struct {
			Type    string `json:"type"`
			UUID    string `json:"uuid"`
			Time    string `json:"timestamp"`
			Message struct {
				Content []struct {
					Type      string     `json:"type"`
					ID        string     `json:"id"`
					Name      string     `json:"name"`
					Input     any        `json:"input"`
					ToolUseID string     `json:"tool_use_id"`
					IsError   bool       `json:"is_error"`
					Content   resultHead `json:"content"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil || (rec.Type != "assistant" && rec.Type != "user") {
			continue
		}
		content := []any{}
		for _, c := range rec.Message.Content {
			switch c.Type {
			case "tool_use":
				content = append(content, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": slimValue(c.Input, 0)})
			case "tool_result":
				if shown[c.ToolUseID] {
					content = append(content, map[string]any{"type": "tool_result", "tool_use_id": c.ToolUseID, "is_error": c.IsError, "content": string(c.Content)})
				}
			}
		}
		if len(content) == 0 {
			continue
		}
		data := map[string]any{"type": rec.Type, "uuid": "sub:" + rec.UUID, "timestamp": rec.Time, "history": true, "parent_tool_use_id": toolUseID, "message": map[string]any{"role": rec.Type, "content": content}}
		b, _ := json.Marshal(data)
		if len(b) > *budget {
			// Out of room: what is left counts as earlier.
			for _, c := range content {
				if obj(c)["type"] == "tool_use" {
					earlier++
				}
			}
			continue
		}
		*budget -= len(b)
		for _, c := range content {
			if m := obj(c); m["type"] == "tool_use" {
				shown[str(m["id"])] = true
			}
		}
		events = append(events, Event{Session: s.ID, Kind: "native", Data: b, Time: rec.Time})
	}
	return events, earlier, nil
}

// codexChildShown: the Codex subagent items a child line shows (its tool
// calls), not its messages, reasoning or plans.
func codexChildShown(item map[string]any) bool {
	switch str(item["type"]) {
	// subAgentActivity: in a subagent's own thread, its exchanges with the
	// thread that started it.
	case "", "agentMessage", "reasoning", "userMessage", "plan", "hookPrompt", "enteredReviewMode", "exitedReviewMode", "contextCompaction", "subAgentActivity":
		return false
	}
	return true
}

// codexChildItem is a Codex subagent's item as a child line shows it, or
// nil for an item it does not show.
func codexChildItem(item map[string]any) map[string]any {
	if !codexChildShown(item) {
		return nil
	}
	out := map[string]any{}
	for k, v := range item {
		switch k {
		case "aggregatedOutput", "output", "result", "error", "contentItems":
			if s, ok := v.(string); ok {
				out[k] = clip(s, subagentOutput)
			} else if v != nil {
				// Structured results: a line shows only whether it worked.
				b, _ := json.Marshal(v)
				out[k] = clip(string(b), subagentOutput)
			}
		case "changes":
			// Paths and kinds, without the diffs.
			changes := []any{}
			for _, c := range asList(v) {
				ch := obj(c)
				changes = append(changes, map[string]any{"path": ch["path"], "kind": ch["kind"]})
				if len(changes) >= 20 {
					break
				}
			}
			out[k] = changes
		default:
			out[k] = slimValue(v, 0)
		}
	}
	return out
}

// codexChildRecord is a Codex subagent's item record for the parent's
// page, or nil for an item its lines do not show.
func codexChildRecord(child, method string, params map[string]any) map[string]any {
	item := codexChildItem(obj(params["item"]))
	if item == nil {
		return nil
	}
	return map[string]any{"method": method, "workbenchChild": child, "params": map[string]any{"threadId": child, "turnId": params["turnId"], "item": item}}
}

// withCodexSubagents adds, after the item that started each Codex subagent
// on a history page (subAgentActivity, as codexTurnEvents lays the items
// out), the subagent thread's latest tool items.
func withCodexSubagents(request codexRequest, s Session, turns []any, events []Event) []Event {
	spawns := map[int]string{}
	seen := map[string]bool{}
	i := 0
	for _, raw := range turns {
		for _, item := range asList(obj(raw)["items"]) {
			it := obj(item)
			if child := str(it["agentThreadId"]); str(it["type"]) == "subAgentActivity" && child != "" && !seen[child] && len(seen) < 10 {
				seen[child] = true
				spawns[i] = child
			}
			i++
		}
		i++ // the turn's summary
	}
	if len(spawns) == 0 {
		return events
	}
	budget := subagentPageBytes
	out := make([]Event, 0, len(events))
	for j, e := range events {
		out = append(out, e)
		if child, ok := spawns[j]; ok {
			out = append(out, codexSubagentTail(request, s, child, &budget)...)
		}
	}
	return out
}

// codexSubagentTail reads a Codex subagent thread's latest subagentTools
// tool items, newest turns first, a few pages at most, as history records
// within budget bytes; a first record says how many earlier ones are left
// out (more: some were not even read).
func codexSubagentTail(request codexRequest, s Session, child string, budget *int) []Event {
	var turns []any
	cursor, tools := "", 0
	for page := 0; page < 5; page++ {
		params := map[string]any{"threadId": child, "limit": 10, "sortDirection": "desc", "itemsView": "full"}
		if cursor != "" {
			params["cursor"] = cursor
		}
		res, err := request("subagent", "thread/turns/list", params)
		if err != nil {
			break
		}
		for _, t := range asList(res["data"]) {
			turns = append(turns, t)
			for _, item := range asList(obj(t)["items"]) {
				if codexChildShown(obj(item)) {
					tools++
				}
			}
		}
		cursor = str(res["nextCursor"])
		if cursor == "" || tools >= subagentTools {
			break
		}
	}
	// Oldest first, then only the latest subagentTools.
	var records []map[string]any
	for k := len(turns) - 1; k >= 0; k-- {
		t := obj(turns[k])
		for _, item := range asList(t["items"]) {
			if !codexChildShown(obj(item)) {
				continue
			}
			records = append(records, map[string]any{"turnId": t["id"], "item": item})
		}
	}
	earlier := 0
	if len(records) > subagentTools {
		earlier = len(records) - subagentTools
		records = records[earlier:]
	}
	var lines []Event
	for _, p := range records {
		data := codexChildRecord(child, "item/completed", p)
		data["history"] = true
		b, _ := json.Marshal(data)
		if len(b) > *budget {
			earlier++
			continue
		}
		*budget -= len(b)
		lines = append(lines, Event{Session: s.ID, Kind: "native", Data: b})
	}
	head, _ := json.Marshal(map[string]any{"method": "workbench/subagent", "history": true, "params": map[string]any{"threadId": child, "earlier": earlier, "more": cursor != ""}})
	return append([]Event{{Session: s.ID, Kind: "native", Data: head}}, lines...)
}

// subagentOf names the subagent a request comes from (m.mu held): a Claude
// request with agent_id, or a Codex request from another thread. Nil for
// the session's own.
func (m *Manager) subagentOf(r *runtime, v any) map[string]any {
	if r.session.Agent == "codex" {
		t := str(obj(obj(v)["params"])["threadId"])
		if t == "" || r.session.NativeID == "" || t == r.session.NativeID {
			return nil
		}
		info := map[string]any{"session": nativeKey("codex", t)}
		if p := r.children[t]; p != "" {
			info["description"] = p[strings.LastIndex(p, "/")+1:]
		}
		return info
	}
	agent := str(obj(obj(v)["request"])["agent_id"])
	if agent == "" || r.session.NativeID == "" {
		return nil
	}
	info := map[string]any{"id": agent, "session": claudeSubagentSession(r.session.NativeID, agent)}
	if t := r.tasks[agent]; t != nil {
		if t.Description != "" {
			info["description"] = t.Description
		}
		if t.toolUseID != "" {
			info["toolUseId"] = t.toolUseID
		}
	}
	for tool, id := range r.foreground {
		if id == agent {
			info["toolUseId"] = tool
		}
	}
	if info["description"] == nil || info["toolUseId"] == nil {
		if path := m.catalog.path(r.session.ID); path != "" {
			if meta, ok := readClaudeSubagentMeta(claudeSubagentDir(path), agent); ok {
				if info["description"] == nil && meta.Description != "" {
					info["description"] = meta.Description
				}
				if info["toolUseId"] == nil && meta.ToolUseID != "" {
					info["toolUseId"] = meta.ToolUseID
				}
				if meta.AgentType != "" {
					info["type"] = meta.AgentType
				}
			}
		}
	}
	return info
}

// ownSubagents is the state of the subagent sessions an engine here runs
// (m.mu held), by session id: true while at work. Their records are locked
// by this engine, which the session list would otherwise take for another
// client.
func ownSubagents(r *runtime) map[string]bool {
	out := map[string]bool{}
	if r.proc == nil || r.session.NativeID == "" {
		return out
	}
	if r.session.Agent == "codex" {
		for thread := range r.children {
			out[nativeKey("codex", thread)] = r.childBusy[thread]
		}
		return out
	}
	for _, t := range r.tasks {
		if t.Source == "claude" && t.Kind == "agent" && t.Status == "running" {
			out[claudeSubagentSession(r.session.NativeID, t.ID)] = true
		}
	}
	return out
}
