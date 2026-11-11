package workbench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"hash/fnv"
	"io"
	"os"
	"sync"
	"time"
)

// Sessions another client (a terminal CLI, the desktop app) is running are
// followed read-only through their native records. Nothing here writes a
// native file, resumes a thread or calls a model.

// activityState is whether a turn is running, decided by the newest record
// that says so.
type activityState struct {
	running bool
	since   string
	// Codex: a turn marker was seen. Claude: a prompt or a finished reply.
	decided bool
}

func (st *activityState) apply(agent string, line []byte) {
	if agent == "codex" {
		if !bytes.Contains(line, []byte(`"event_msg"`)) || !(bytes.Contains(line, []byte(`"task_started"`)) || bytes.Contains(line, []byte(`"task_complete"`)) || bytes.Contains(line, []byte(`"turn_aborted"`))) {
			return
		}
		var v struct {
			Type    string `json:"type"`
			Payload struct {
				Type      string  `json:"type"`
				StartedAt float64 `json:"started_at"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &v) != nil || v.Type != "event_msg" {
			return
		}
		switch v.Payload.Type {
		case "task_started":
			st.running, st.decided, st.since = true, true, ""
			if v.Payload.StartedAt > 0 {
				st.since = time.Unix(int64(v.Payload.StartedAt), 0).UTC().Format(time.RFC3339)
			}
		case "task_complete", "turn_aborted":
			st.running, st.decided, st.since = false, true, ""
		}
		return
	}
	if !bytes.Contains(line, []byte(`"type":"user"`)) && !bytes.Contains(line, []byte(`"type":"assistant"`)) {
		return
	}
	var v struct {
		Type      string `json:"type"`
		Sidechain bool   `json:"isSidechain"`
		Meta      bool   `json:"isMeta"`
		Time      string `json:"timestamp"`
		Message   struct {
			StopReason *string         `json:"stop_reason"`
			Content    json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &v) != nil || v.Sidechain || v.Meta {
		return
	}
	switch v.Type {
	case "assistant":
		stop := ""
		if v.Message.StopReason != nil {
			stop = *v.Message.StopReason
		}
		// A reply that stops for a tool (or pauses) continues the turn.
		st.running = stop == "" || stop == "tool_use" || stop == "pause_turn"
		if !st.running {
			st.since, st.decided = "", true
		}
	case "user":
		switch {
		case bytes.Contains(v.Message.Content, []byte(`"tool_result"`)):
			st.running = true
		case bytes.Contains(v.Message.Content, []byte("[Request interrupted")):
			st.running, st.since, st.decided = false, "", true
		default:
			st.running, st.since, st.decided = true, v.Time, true
		}
	}
}

// scanLines calls fn for each complete line in [from, to) and returns the
// offset after the last one. With skipFirst the first (partial) line is
// dropped: from is not a line start.
func scanLines(f *os.File, from, to int64, skipFirst bool, fn func([]byte)) (int64, error) {
	reader := bufio.NewReaderSize(io.NewSectionReader(f, from, to-from), 1<<20)
	offset := from
	if skipFirst {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return from, nil
		}
		offset += int64(len(line))
	}
	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			return offset, nil
		}
		if err != nil {
			return offset, err
		}
		offset += int64(len(line))
		fn(line)
	}
}

// externalActivity keeps a session's activityState current. The first read
// looks back from the end in growing windows until a deciding record turns
// up; later reads consume only appended lines.
type externalActivity struct {
	mu     sync.Mutex
	path   string
	primed bool
	offset int64
	state  activityState
}

func (a *externalActivity) read(agent string) error {
	f, err := os.Open(a.path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	if size < a.offset {
		a.primed, a.offset, a.state = false, 0, activityState{}
	}
	if a.primed {
		a.offset, err = scanLines(f, a.offset, size, false, func(line []byte) { a.state.apply(agent, line) })
		return err
	}
	for window := int64(1 << 20); ; window *= 4 {
		start := max(size-window, 0)
		st := activityState{}
		end, err := scanLines(f, start, size, start > 0, func(line []byte) { st.apply(agent, line) })
		if err != nil {
			return err
		}
		if st.decided || start == 0 {
			a.primed, a.offset, a.state = true, end, st
			return nil
		}
	}
}

// externalStatus reports whether another client is running a turn in the
// session and when that turn started.
func (m *Manager) externalStatus(id, agent, path string) (bool, string) {
	m.extMu.Lock()
	if m.external == nil {
		m.external = map[string]*externalActivity{}
	}
	a := m.external[id]
	if a == nil || a.path != path {
		a = &externalActivity{path: path}
		m.external[id] = a
	}
	m.extMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.read(agent) != nil {
		return false, ""
	}
	return a.state.running, a.state.since
}

// externalRunning is whether another client is running a turn in the
// session. A Claude process reports busy or idle itself, which wins over
// reading the record (a compaction summary, for one, looks like a prompt).
func (m *Manager) externalRunning(s Session, path string) (bool, string) {
	running, since := m.externalStatus(s.ID, s.Agent, path)
	if s.Agent == "claude" {
		switch m.catalog.ClaudeStatus(s) {
		case "idle":
			return false, ""
		case "busy":
			return true, since
		}
	}
	return running, since
}

// isExternal is whether another client holds the session.
func (m *Manager) isExternal(s Session) bool {
	// Mirrors NativeList: a Claude engine this workbench runs can still be
	// shared with another Claude process.
	m.mu.Lock()
	r := m.find(s.ID)
	live := r != nil && r.proc != nil
	var o processOwner
	if live {
		o, _ = r.proc.(processOwner)
	}
	m.mu.Unlock()
	if live {
		return o != nil && s.Agent == "claude" && m.catalog.ClaudeBusyExcept(s, o.Owns)
	}
	return m.catalog.Busy(s)
}

func externalStatusName(running bool) string {
	if running {
		return "external_running"
	}
	return "external"
}

// externalUpdate is pushed to a page viewing a session another client runs.
type externalUpdate struct {
	Session   string `json:"session"`
	External  bool   `json:"external"`
	Running   bool   `json:"running"`
	StartedAt string `json:"startedAt,omitempty"`
	Status    string `json:"status"`
	// Records new or changed since the last update, oldest first.
	Events []Event `json:"events,omitempty"`
	// The record no longer continues what was sent (a rewound branch, or
	// more new records than a tail page holds): reload the history.
	Reset bool `json:"reset,omitempty"`
}

// externalTail follows one session for one event stream.
type externalTail struct {
	m        *Manager
	id       string
	primed   bool
	external bool
	running  bool
	size     int64
	mod      time.Time
	polled   time.Time
	sent     map[string]uint64
	lastKey  string
	claude   *claudeIndex
	codex    *codexClient
}

func (m *Manager) followExternal(id string) *externalTail {
	return &externalTail{m: m, id: id, sent: map[string]uint64{}}
}

func (t *externalTail) close() {
	if t.codex != nil {
		t.codex.Close()
		t.codex = nil
	}
}

// poll returns an update when the session is held by another client and
// its record or run state changed, and a last one when that client lets go.
func (t *externalTail) poll() *externalUpdate {
	if time.Since(t.polled) < 900*time.Millisecond {
		return nil
	}
	t.polled = time.Now()
	m := t.m
	s, ok := m.catalog.Find(t.id)
	if !ok {
		return nil
	}
	m.catalog.mu.Lock()
	path := m.catalog.paths[t.id]
	m.catalog.mu.Unlock()
	info, err := os.Stat(path)
	if path == "" || err != nil {
		return nil
	}
	external := m.isExternal(s)
	changed := info.Size() != t.size || !info.ModTime().Equal(t.mod)
	t.size, t.mod = info.Size(), info.ModTime()
	first := !t.primed
	t.primed = true
	if !external && !t.external {
		t.close()
		return nil
	}
	u := &externalUpdate{Session: t.id, External: external, Status: s.Status}
	if external {
		u.Running, u.StartedAt = m.externalRunning(s, path)
		u.Status = externalStatusName(u.Running)
	}
	stateChanged := external != t.external || u.Running != t.running
	t.external, t.running = external, u.Running
	if first {
		// The page has just loaded the history; send the run state only.
		return u
	}
	if !changed && !stateChanged {
		return nil
	}
	if events, err := t.tail(s, path, u.Running); err == nil {
		u.Events, u.Reset = t.fresh(events)
	}
	if !external {
		t.close()
	}
	if len(u.Events) == 0 && !u.Reset && !stateChanged {
		// Only records the timeline does not show changed.
		return nil
	}
	return u
}

// tail reads the newest records: the last two Codex turns through a
// read-only app-server, or the newest Claude branch records from an index
// that only reads appended bytes.
func (t *externalTail) tail(s Session, path string, running bool) ([]Event, error) {
	if s.Agent == "codex" {
		if t.codex == nil {
			c, err := startCodexClient("workbench_follow")
			if err != nil {
				return nil, err
			}
			t.codex = c
		}
		page, err := t.codex.request("tail", "thread/turns/list", map[string]any{"threadId": s.NativeID, "limit": 2, "sortDirection": "desc", "itemsView": "full"})
		if err != nil {
			t.close()
			return nil, err
		}
		turns, _ := page["data"].([]any)
		for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
			turns[i], turns[j] = turns[j], turns[i]
		}
		events := codexTurnEvents(s, turns)
		if running {
			// The in-flight turn reads back as interrupted.
			events = dropLiveTurnSummary(events)
		}
		return events, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if t.claude == nil {
		t.claude = newClaudeIndex(s)
	}
	if err := t.claude.scan(f, false); err != nil {
		t.claude = nil
		return nil, err
	}
	page, err := t.claude.page(f, s, "", 80)
	return page.Events, err
}

// fresh keeps the events not yet sent, or changed since. When the page no
// longer holds the last record sent, it does not continue the stream.
func (t *externalTail) fresh(events []Event) ([]Event, bool) {
	keys := make([]string, len(events))
	found := t.lastKey == ""
	for i, e := range events {
		keys[i] = eventKey(e)
		found = found || keys[i] == t.lastKey
	}
	if len(events) > 0 {
		t.lastKey = keys[len(keys)-1]
	}
	if !found {
		clear(t.sent)
		return nil, true
	}
	out := []Event{}
	for i, e := range events {
		h := fnv.New64a()
		h.Write(e.Data)
		sum := h.Sum64()
		if keys[i] != "" && t.sent[keys[i]] == sum {
			continue
		}
		if keys[i] != "" {
			t.sent[keys[i]] = sum
		}
		out = append(out, e)
	}
	return out, false
}

// eventKey identifies a native record: a Claude uuid, a Codex item id or a
// Codex turn summary.
func eventKey(e Event) string {
	var v struct {
		UUID   string `json:"uuid"`
		Params struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		} `json:"params"`
	}
	if json.Unmarshal(e.Data, &v) != nil {
		return ""
	}
	switch {
	case v.UUID != "":
		return v.UUID
	case v.Params.Item.ID != "":
		return "item:" + v.Params.Item.ID
	case v.Params.Turn.ID != "":
		return "turn:" + v.Params.Turn.ID
	}
	return ""
}
