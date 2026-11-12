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
)

type transcriptNode struct {
	UUID, Parent, Kind, Time string
	Offset                   int64
	Length                   int
	Hidden                   bool
	ToolResult               bool
}

// HistoryPage is a chronological slice of native records ending before a cursor.
type HistoryPage struct {
	Events  []Event `json:"events"`
	Before  string  `json:"before,omitempty"`
	HasMore bool    `json:"hasMore"`
	// Running: a turn is in flight here or in another client.
	Running bool `json:"running"`
	// The native record is gone; only what the CLI's index keeps remains.
	RecordMissing *recordRemains `json:"recordMissing,omitempty"`
	// Claude Agent calls on the page and the subagents their results name,
	// for the subagents' lines (subagents.go).
	agentCalls []agentCall
	agentIDs   map[string]string
}

type agentCall struct {
	index     int
	toolUseID string
}

// pageBounds returns the [start,end) window of n records that ends at the
// numeric cursor before (or at the end when empty).
func pageBounds(n int, before string, limit int) (int, int) {
	end := n
	if v, err := strconv.Atoi(before); err == nil && v >= 0 && v < n {
		end = v
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	return start, end
}

func readClaudeHistory(path string, s Session, before string, limit int) (HistoryPage, error) {
	f, err := os.Open(path)
	if err != nil {
		return HistoryPage{}, err
	}
	defer f.Close()
	x := newClaudeIndex(s)
	if err := x.scan(f, true); err != nil {
		return HistoryPage{}, err
	}
	page, err := x.page(f, s, before, limit)
	if err == nil && !x.subagent && len(page.agentCalls) > 0 {
		page.Events = withClaudeSubagents(path, s, page)
	}
	return page, err
}

// withClaudeSubagents adds, after each Agent call on the page, a record
// naming its subagent and the subagent's latest tool calls, as the live
// stream nests them; all of them within subagentPageBytes.
func withClaudeSubagents(mainPath string, s Session, page HistoryPage) []Event {
	dir := claudeSubagentDir(mainPath)
	calls := map[int][]string{}
	for _, c := range page.agentCalls {
		calls[c.index] = append(calls[c.index], c.toolUseID)
	}
	var byMeta map[string]string
	budget := subagentPageBytes
	out := make([]Event, 0, len(page.Events))
	for i, e := range page.Events {
		out = append(out, e)
		for _, tool := range calls[i] {
			id := page.agentIDs[tool]
			if id == "" {
				if byMeta == nil {
					byMeta = claudeSubagentsByTool(dir)
				}
				id = byMeta[tool]
			}
			if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
				continue
			}
			lines, earlier, _ := claudeSubagentTail(filepath.Join(dir, "agent-"+id+".jsonl"), tool, s, &budget)
			b, _ := json.Marshal(map[string]any{"type": "workbench_subagent", "history": true, "tool_use_id": tool, "agentId": id, "earlier": earlier})
			out = append(out, Event{Session: s.ID, Kind: "native", Data: b, Time: e.Time})
			out = append(out, lines...)
		}
	}
	return out
}

// claudeIndex is the record layout of a Claude transcript. It extends as the
// file grows, so following a transcript another client writes reads only the
// new bytes.
type claudeIndex struct {
	subagent bool
	offset   int64
	nodes    map[string]transcriptNode
	// continued marks messages another message follows, possibly through
	// hidden records such as attachments.
	continued map[string]bool
	order     []transcriptNode
	head      string
	linked    bool
}

func newClaudeIndex(s Session) *claudeIndex {
	return &claudeIndex{subagent: s.Source == "subagent", nodes: map[string]transcriptNode{}, continued: map[string]bool{}}
}

func (n transcriptNode) message() bool {
	return !n.Hidden && (n.Kind == "user" || n.Kind == "assistant" || n.Kind == "local_command" || n.Kind == "steer")
}

// scan indexes the records after the last scanned offset. Unless final, a
// trailing line without a newline is left for the next scan: the writer may
// still be appending it.
func (x *claudeIndex) scan(f *os.File, final bool) error {
	if _, err := f.Seek(x.offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(f, 65536)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 16*1024*1024 {
			return errors.New("native message exceeds 16 MiB")
		}
		if err == io.EOF && !final {
			return nil
		}
		var v struct {
			UUID          string `json:"uuid"`
			Parent        string `json:"parentUuid"`
			LogicalParent string `json:"logicalParentUuid"`
			Type          string `json:"type"`
			Subtype       string `json:"subtype"`
			Time          string `json:"timestamp"`
			Meta          bool   `json:"isMeta"`
			Sidechain     bool   `json:"isSidechain"`
			Attachment    struct {
				Type        string `json:"type"`
				CommandMode string `json:"commandMode"`
			} `json:"attachment"`
		}
		if json.Unmarshal(line, &v) == nil && v.UUID != "" {
			// The output of a local command (/context, /usage) is shown too.
			if v.Type == "system" && v.Subtype == "local_command" {
				v.Type = "local_command"
			}
			// A message sent while a turn ran, taken in mid-turn (steering).
			// Task notifications use the same attachment with another mode.
			if v.Type == "attachment" && v.Attachment.Type == "queued_command" && (v.Attachment.CommandMode == "" || v.Attachment.CommandMode == "prompt") {
				v.Type = "steer"
			}
			parent := v.Parent
			if parent == "" {
				parent = v.LogicalParent
			}
			// A subagent transcript is entirely sidechain records; main transcripts hide them.
			hidden := v.Meta || (v.Sidechain && !x.subagent)
			node := transcriptNode{v.UUID, parent, v.Type, v.Time, x.offset, len(line), hidden, v.Type == "user" && bytes.Contains(line, []byte(`"tool_result"`))}
			x.nodes[v.UUID] = node
			if node.message() {
				for q, steps := parent, 0; q != "" && steps < 64; steps++ {
					p, ok := x.nodes[q]
					if !ok {
						break
					}
					if p.message() {
						x.continued[q] = true
						break
					}
					q = p.Parent
				}
				x.order = append(x.order, node)
				x.head = v.UUID
				if parent != "" {
					x.linked = true
				}
			}
		}
		x.offset += int64(len(line))
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// page returns the records on the newest branch, oldest first, ending before
// the cursor.
func (x *claudeIndex) page(f *os.File, s Session, before string, limit int) (HistoryPage, error) {
	order := x.order
	if x.linked {
		chain := []transcriptNode{}
		seen := map[string]bool{}
		head := x.head
		for head != "" && !seen[head] {
			seen[head] = true
			n, ok := x.nodes[head]
			if !ok {
				break
			}
			if n.message() {
				chain = append(chain, n)
			}
			head = n.Parent
		}
		// Parallel tool calls write each result as a leaf off its own tool_use
		// record, so only the last result lies on the chain. Keep the others;
		// a result the conversation continued from belongs to another branch.
		// Hidden records (file attachments) after a leaf do not continue it.
		inChain := make(map[string]bool, len(chain))
		for _, n := range chain {
			inChain[n.UUID] = true
		}
		order = nil
		for _, n := range x.order {
			if inChain[n.UUID] || (n.ToolResult && inChain[n.Parent] && !x.continued[n.UUID]) {
				order = append(order, n)
			}
		}
	}
	start, end := pageBounds(len(order), before, limit)
	page := HistoryPage{Events: []Event{}, HasMore: start > 0}
	if start > 0 {
		page.Before = strconv.Itoa(start)
	}
	for _, n := range order[start:end] {
		line := make([]byte, n.Length)
		_, err := f.ReadAt(line, n.Offset)
		if err != nil && err != io.EOF {
			return HistoryPage{}, err
		}
		var v map[string]any
		if json.Unmarshal(line, &v) != nil {
			continue
		}
		if n.Kind == "steer" {
			attachment := obj(v["attachment"])
			data := map[string]any{"type": "user", "uuid": n.UUID, "timestamp": n.Time, "history": true, "steered": true, "message": map[string]any{"role": "user", "content": boundJSON(attachment["prompt"])}}
			b, _ := json.Marshal(data)
			page.Events = append(page.Events, Event{Session: s.ID, Kind: "native", Data: b, Time: n.Time})
			continue
		}
		if n.Kind == "local_command" {
			data := map[string]any{"type": "system", "subtype": "local_command", "uuid": n.UUID, "timestamp": n.Time, "history": true, "content": boundJSON(v["content"])}
			b, _ := json.Marshal(data)
			page.Events = append(page.Events, Event{Session: s.ID, Kind: "native", Data: b, Time: n.Time})
			continue
		}
		msg := obj(v["message"])
		// Agent calls, and the subagents their results name.
		for _, raw := range asList(msg["content"]) {
			c := obj(raw)
			if n.Kind == "assistant" && str(c["type"]) == "tool_use" && (str(c["name"]) == "Agent" || str(c["name"]) == "Task") {
				page.agentCalls = append(page.agentCalls, agentCall{len(page.Events), str(c["id"])})
			}
		}
		if agent := str(obj(v["toolUseResult"])["agentId"]); n.Kind == "user" && agent != "" {
			for _, raw := range asList(msg["content"]) {
				if c := obj(raw); str(c["type"]) == "tool_result" {
					if page.agentIDs == nil {
						page.agentIDs = map[string]string{}
					}
					page.agentIDs[str(c["tool_use_id"])] = agent
				}
			}
		}
		data := map[string]any{
			"type":      n.Kind,
			"uuid":      n.UUID,
			"timestamp": n.Time,
			"history":   true,
			"message":   map[string]any{"id": msg["id"], "role": msg["role"], "content": boundJSON(msg["content"])},
		}
		if v["isCompactSummary"] == true {
			data["isCompactSummary"] = true
		}
		// Where the agent was: paths in the record are relative to it.
		if cwd := str(v["cwd"]); cwd != "" {
			data["cwd"] = cwd
		}
		if agent := str(obj(v["toolUseResult"])["agentId"]); agent != "" {
			data["toolUseResult"] = map[string]any{"agentId": agent}
		}
		// Who produced a user record: typed, SDK, or harness events such as task notifications.
		for _, k := range []string{"origin", "promptSource"} {
			if v[k] != nil {
				data[k] = v[k]
			}
		}
		b, _ := json.Marshal(data)
		page.Events = append(page.Events, Event{Session: s.ID, Kind: "native", Data: b, Time: n.Time})
	}
	return page, nil
}
