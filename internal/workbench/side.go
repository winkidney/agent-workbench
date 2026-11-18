package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SideChat records survive the ephemeral engine context. Closed records never
// resume the native thread: opening history is a database-only operation.
type SideChat struct {
	ID      string `json:"id"`
	Parent  string `json:"parent"`
	Agent   string `json:"agent"`
	Status  string `json:"status"`
	Created string `json:"created"`
	Error   string `json:"error,omitempty"`
	// While running: when the question was sent, and what it waits on,
	// "waking" (the parent engine starts) or "answering".
	Since string `json:"since,omitempty"`
	Phase string `json:"phase,omitempty"`
}
type sideRuntime struct {
	SideChat
	rt         *runtime
	native     string
	turns      map[string]string
	threads    map[string]bool
	request    string
	cancel     context.CancelFunc
	done       chan struct{}
	sending    bool
	generation int
	options    Options
	// foreign: multi-agent v2 sub-agents of the side's threads. Codex takes
	// no direct input for them; it runs and releases them through their root.
	foreign map[string]bool
	// stopped: the person stopped the running answer (InterruptSide).
	stopped bool
}

// codexOwnsThread tells whether Codex refused a request because the thread
// is a multi-agent v2 sub-agent, which only its root thread may drive.
func codexOwnsThread(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not allowed for multi-agent v2 sub-agents")
}

func (m *Manager) saveSide(sc *sideRuntime) {
	if sc.Status != "running" {
		sc.Since, sc.Phase = "", ""
	}
	b, _ := json.Marshal(sc.SideChat)
	if _, err := m.store.db.Exec(`INSERT INTO side_chats VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET parent=excluded.parent,data=excluded.data`, sc.ID, sc.Parent, string(b)); err != nil {
		sc.Error = err.Error()
	}
	m.changes.Broadcast()
}
func (m *Manager) sideEmit(id, kind string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	if _, err = m.store.db.Exec(`INSERT INTO side_events(session,kind,data,time) VALUES(?,?,?,?)`, id, kind, string(b), now3339()); err != nil {
		fmt.Println("persist side event:", err)
	}
	m.changes.Broadcast()
}
func (m *Manager) hasSideLocked(parent string) bool {
	for _, sc := range m.sides {
		if sc.Parent == parent && sc.Status != "closed" && sc.Status != "interrupted" {
			return true
		}
	}
	return false
}
func (m *Manager) sideExited(r *runtime) {
	for id, sc := range m.sides {
		if sc.Parent != r.session.ID {
			continue
		}
		if sc.cancel != nil {
			sc.cancel()
		}
		m.flushDelta(sc.rt)
		sc.Status = "interrupted"
		m.saveSide(sc)
		delete(m.sides, id)
	}
}
func (m *Manager) ListSides(parent string) ([]SideChat, error) {
	rows, err := m.store.db.Query(`SELECT data FROM side_chats WHERE parent=? ORDER BY rowid DESC`, parent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SideChat{}
	for rows.Next() {
		var b string
		var sc SideChat
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(b), &sc); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}
func (m *Manager) OpenSide(parent string) (SideChat, error) {
	if err := m.Attach(parent); err != nil {
		return SideChat{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(parent)
	if r.session.ReadOnly || m.closing {
		return SideChat{}, errors.New("session is read-only or shutting down")
	}
	if r.session.NativeID == "" {
		return SideChat{}, errors.New("send the first main-chat message before opening a side chat")
	}
	parent = r.session.ID
	for _, sc := range m.sides {
		if sc.Parent == parent && sc.Status != "closed" && sc.Status != "interrupted" {
			return sc.SideChat, nil
		}
	}
	sc := &sideRuntime{SideChat: SideChat{ID: "side:" + token(), Parent: parent, Agent: r.session.Agent, Status: "idle", Created: now3339()}, turns: map[string]string{}, threads: map[string]bool{}}
	sc.rt = &runtime{session: Session{ID: sc.ID, Agent: sc.Agent}, pending: map[string]any{}}
	if m.sides == nil {
		m.sides = map[string]*sideRuntime{}
	}
	m.sides[sc.ID] = sc
	m.saveSide(sc)
	return sc.SideChat, nil
}
func (m *Manager) Side(id string) (SideChat, error) {
	var b string
	var sc SideChat
	err := m.store.db.QueryRow(`SELECT data FROM side_chats WHERE id=?`, id).Scan(&b)
	if err == nil {
		err = json.Unmarshal([]byte(b), &sc)
	}
	return sc, err
}
func (m *Manager) sideHistory(id string, before int64) ([]Event, error) {
	if before == 0 {
		before = 1 << 62
	}
	rows, err := m.store.db.Query(`SELECT seq,kind,data,time FROM side_events WHERE session=? AND seq<? ORDER BY seq DESC LIMIT 200`, id, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		e := Event{Session: id}
		var b string
		if err = rows.Scan(&e.Seq, &e.Kind, &b, &e.Time); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(b)
		out = append(out, e)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}
func (m *Manager) SendSide(id, text, requestID string) error {
	if err := checkMessage(text, nil); err != nil {
		return err
	}
	if requestID == "" {
		return errors.New("requestId required")
	}
	m.mu.Lock()
	sc := m.sides[id]
	if sc == nil {
		m.mu.Unlock()
		return errors.New("side chat is read-only")
	}
	// Persist an idempotency marker before starting any native request.
	var n int
	m.store.db.QueryRow(`SELECT count(*) FROM side_events WHERE session=? AND kind="user" AND json_extract(data,"$.requestId")=?`, id, requestID).Scan(&n)
	if n > 0 {
		m.mu.Unlock()
		return nil
	}
	if sc.Status != "idle" || sc.sending {
		m.mu.Unlock()
		return errors.New("side chat is busy or closing")
	}
	// No deadline on the whole answer: a Codex side turn may run tools and
	// wait on your approvals for as long as it needs (runSide limits only
	// the start). Closing the side chat cancels it.
	ctx, cancel := context.WithCancel(context.Background())
	sc.cancel = cancel
	sc.done = make(chan struct{})
	sc.Status = "running"
	sc.Since = now3339()
	sc.Phase = "answering"
	if r := m.find(sc.Parent); r == nil || !r.ready || r.proc == nil {
		sc.Phase = "waking"
	}
	sc.sending = true
	sc.request = ""
	sc.Error = ""
	m.saveSide(sc)
	m.sideEmit(id, "user", map[string]any{"text": text, "requestId": requestID})
	m.mu.Unlock()
	go func() {
		defer cancel()
		err := m.runSide(ctx, sc, text)
		m.mu.Lock()
		defer m.mu.Unlock()
		sc.sending = false
		close(sc.done)
		if sc.Status == "closing" || sc.Status == "cleanup_failed" || sc.Status == "interrupted" {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("the agent did not start answering within %v; send again, or close the side chat", sideStartLimit)
		}
		if err != nil {
			sc.Error = err.Error()
			m.sideEmit(id, "error", map[string]any{"message": err.Error()})
		}
		if len(sc.turns) > 0 {
			sc.Status = "cleanup_failed"
		} else {
			sc.Status = "idle"
		}
		sc.request = ""
		m.saveSide(sc)
	}()
	return nil
}

// sideStartLimit bounds what comes before a side answer runs on its own:
// waking the parent session and starting the side turn (for Claude, the
// whole answer, which its CLI gives in one request). A Codex side turn that
// has started ends with turn/completed, an interrupt or a close, never on a
// clock: it may run tools and wait on approvals.
var sideStartLimit = 12 * time.Minute

func (m *Manager) runSide(ctx context.Context, sc *sideRuntime, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	running := ctx
	ctx, stop := context.WithTimeout(ctx, sideStartLimit)
	defer stop()
	if err := m.Start(sc.Parent); err != nil {
		return err
	}
	for {
		m.mu.Lock()
		r := m.find(sc.Parent)
		ready := r != nil && r.ready && r.proc != nil
		if ready {
			if sc.Status == "running" && sc.Phase != "answering" {
				sc.Phase = "answering"
				m.saveSide(sc)
			}
			sc.generation = r.generation
			sc.options = m.options(r.session.ID)
			if sc.options.Model == "" {
				sc.options.Model = r.effective.Model
			}
		}
		m.mu.Unlock()
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	if sc.Agent == "claude" {
		var history []any
		rows, err := m.store.db.Query(`SELECT data FROM side_events WHERE session=? AND kind="exchange" ORDER BY seq DESC LIMIT 20`, sc.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var b string
			rows.Scan(&b)
			var v any
			json.Unmarshal([]byte(b), &v)
			history = append(history, v)
		}
		rows.Close()
		for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
			history[i], history[j] = history[j], history[i]
		}
		result, err := m.rpcContext(ctx, sc.Parent, "side_question", map[string]any{"question": text, "history": history}, func(key string) { sc.request = key })
		if err != nil {
			return err
		}
		if answer := str(result["response"]); answer != "" {
			m.sideEmit(sc.ID, "notice", map[string]any{"text": answer, "markdown": true})
			if result["synthetic"] != true {
				m.sideEmit(sc.ID, "exchange", map[string]any{"question": text, "response": answer})
			}
		} else {
			return errors.New("no side answer available")
		}
		return nil
	}
	m.mu.Lock()
	native := sc.native
	parent := m.find(sc.Parent).session.NativeID
	m.mu.Unlock()
	if native == "" {
		result, err := m.rpcContext(ctx, sc.Parent, "thread/fork", map[string]any{"threadId": parent, "ephemeral": true, "excludeTurns": true, "config": map[string]any{"thread_unload_delay_secs": 0}}, nil)
		if err != nil {
			return err
		}
		native = str(obj(result["thread"])["id"])
		if native == "" {
			return errors.New("fork returned no thread")
		}
		m.mu.Lock()
		sc.native = native
		sc.threads[native] = true
		m.mu.Unlock()
	}
	m.mu.Lock()
	closing := sc.Status != "running"
	m.mu.Unlock()
	if closing {
		return nil
	}
	params := map[string]any{"threadId": native, "input": []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}}}
	codexOptions(params, sc.options)
	_, err := m.rpcContext(ctx, sc.Parent, "turn/start", params, nil)
	if err != nil {
		return err
	}
	// turn/completed owns the transition back to idle. This request remains
	// cancellable (by a close) until then; receiving an RPC acknowledgement
	// is not completion.
	for {
		m.mu.Lock()
		busy := sc.Status == "running"
		m.mu.Unlock()
		if !busy {
			return nil
		}
		select {
		case <-running.Done():
			return running.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Called with m.mu held, before the generic child-thread handler.
func (m *Manager) sideReceive(parent *runtime, v map[string]any) bool {
	p := obj(v["params"])
	thread := str(p["threadId"])
	for _, sc := range m.sides {
		if sc.Parent != parent.session.ID || sc.generation != parent.generation {
			continue
		}
		if str(v["type"]) == "system" && str(v["subtype"]) == "control_request_progress" && str(v["request_id"]) == sc.request {
			return true
		}
		if sc.Agent == "codex" && str(v["method"]) == "thread/started" {
			fork := obj(p["thread"])
			if fork["ephemeral"] == true && str(fork["forkedFromId"]) == parent.session.NativeID && sc.native == "" {
				sc.native = str(fork["id"])
				sc.threads[sc.native] = true
				return true
			}
		}
		if thread == "" || !sc.threads[thread] && !sc.foreign[thread] {
			continue
		}
		method := str(v["method"])
		if v["id"] != nil {
			if method == "currentTime/read" {
				_ = parent.proc.Input(map[string]any{"id": v["id"], "result": map[string]any{"currentTimeAt": time.Now().Unix()}})
				return true
			}
			key := fmt.Sprint(v["id"])
			sc.rt.pending[key] = v
			m.sideEmit(sc.ID, "approval", map[string]any{"id": key, "request": v})
			return true
		}
		if method == "serverRequest/resolved" {
			key := fmt.Sprint(p["requestId"])
			delete(sc.rt.pending, key)
			m.sideEmit(sc.ID, "approval_resolved", map[string]any{"id": key})
		}
		if it := obj(p["item"]); str(it["type"]) == "subAgentActivity" {
			if child := str(it["agentThreadId"]); child != "" && !sc.foreign[child] {
				sc.threads[child] = true
			}
		}
		switch method {
		case "turn/started":
			sc.turns[thread] = str(obj(p["turn"])["id"])
		case "turn/completed":
			delete(sc.turns, thread)
			if thread == sc.native && sc.Status == "running" {
				sc.Status = "idle"
				m.saveSide(sc)
			}
		case "thread/closed":
			sc.threads[thread] = false
		}
		switch d, class := classifyLive(v); class {
		case liveDelta:
			m.bufferDelta(sc.rt, d)
		case liveKeep:
			m.flushDelta(sc.rt)
			m.sideEmit(sc.ID, "native", boundJSON(v))
		}
		return true
	}
	return false
}
func (m *Manager) ReplySide(id, key, decision string, answers map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sc := m.sides[id]
	if sc == nil || sc.Status == "closing" || sc.Status == "cleanup_failed" {
		return errors.New("side chat is not active")
	}
	v := obj(sc.rt.pending[key])
	if v["id"] == nil {
		return errors.New("approval expired")
	}
	r := m.find(sc.Parent)
	if r == nil || r.proc == nil {
		return errors.New("engine exited")
	}
	var result any
	if decision != "allow" && decision != "allowAlways" && decision != "deny" && decision != "cancel" {
		return errors.New("invalid decision")
	}
	allow := decision == "allow" || decision == "allowAlways"
	switch str(v["method"]) {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		d, ok := map[string]string{"allow": "accept", "allowAlways": "acceptForSession", "deny": "decline", "cancel": "cancel"}[decision]
		if !ok {
			return errors.New("invalid decision")
		}
		result = map[string]any{"decision": d}
	case "item/tool/requestUserInput":
		if !allow {
			return errors.New("provide answers or close the side chat")
		}
		result = map[string]any{"answers": answers}
	case "item/permissions/requestApproval":
		result = map[string]any{"permissions": map[string]any{}}
		if allow {
			result = map[string]any{"permissions": obj(pickParams(v)["permissions"]), "scope": "turn"}
		}
	case "mcpServer/elicitation/request":
		result = elicitationResult(decision, answers)
	default:
		return errors.New("unsupported request; close the side chat to cancel")
	}
	if err := r.proc.Input(map[string]any{"id": v["id"], "result": result}); err != nil {
		return err
	}
	delete(sc.rt.pending, key)
	m.sideEmit(id, "approval_resolved", map[string]any{"id": key})
	return nil
}
func pickParams(v map[string]any) map[string]any { return obj(v["params"]) }
func (m *Manager) CloseSide(id string, confirmed bool) error {
	if !confirmed {
		return errors.New("closing a side chat requires confirmation")
	}
	m.mu.Lock()
	sc := m.sides[id]
	if sc == nil {
		m.mu.Unlock()
		v, err := m.Side(id)
		if err == nil && (v.Status == "closed" || v.Status == "interrupted") {
			return nil
		}
		return errors.New("side chat not active")
	}
	if sc.Status == "closing" {
		m.mu.Unlock()
		return nil
	}
	sc.Status = "closing"
	sc.Error = ""
	m.saveSide(sc)
	m.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := m.cleanupSide(ctx, sc)
		m.mu.Lock()
		defer m.mu.Unlock()
		m.flushDelta(sc.rt)
		if err != nil {
			sc.Status = "cleanup_failed"
			sc.Error = err.Error()
			m.saveSide(sc)
			return
		}
		sc.Status = "closed"
		m.saveSide(sc)
		delete(m.sides, id)
	}()
	return nil
}
func (m *Manager) cleanupSide(ctx context.Context, sc *sideRuntime) error {
	m.mu.Lock()
	r := m.find(sc.Parent)
	if r == nil || r.proc == nil || r.generation != sc.generation && sc.generation != 0 {
		if sc.cancel != nil {
			sc.cancel()
		}
		m.mu.Unlock()
		return nil
	}
	if sc.Agent == "claude" && sc.request != "" && sc.sending {
		err := r.proc.Input(map[string]any{"type": "control_cancel_request", "request_id": sc.request})
		done := sc.done
		m.mu.Unlock()
		if err != nil {
			return err
		}
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if sc.Agent == "claude" && sc.cancel != nil {
		sc.cancel()
	}
	done := sc.done
	sending := sc.sending
	m.mu.Unlock()
	// A fork RPC already in flight must settle so we can reclaim its result.
	if sending {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	threads := []string{}
	for t, loaded := range sc.threads {
		if loaded {
			threads = append(threads, t)
		}
	}
	root := sc.native
	m.mu.Unlock()
	// A v2 sub-agent refuses direct requests: it is left to Codex, which
	// stops and releases it with the side's own thread, and not waited on.
	settle := func(thread string, err error) (bool, error) {
		if err == nil {
			return true, nil
		}
		if thread != root && codexOwnsThread(err) {
			m.mu.Lock()
			if sc.foreign == nil {
				sc.foreign = map[string]bool{}
			}
			sc.foreign[thread] = true
			sc.threads[thread] = false
			m.mu.Unlock()
			return false, nil
		}
		return false, err
	}
	kept := threads[:0]
	for _, thread := range threads {
		ok, err := settle(thread, m.cleanupThread(ctx, sc, thread))
		if err != nil {
			return err
		}
		if ok {
			kept = append(kept, thread)
		}
	}
	threads = kept
	m.mu.Lock()
	r = m.find(sc.Parent)
	for key, raw := range sc.rt.pending {
		v := obj(raw)
		if r != nil && r.proc != nil {
			_ = r.proc.Input(map[string]any{"id": v["id"], "error": map[string]any{"code": -32000, "message": "Side chat closed by user"}})
		}
		delete(sc.rt.pending, key)
	}
	m.mu.Unlock()
	kept = threads[:0]
	for _, t := range threads {
		_, err := m.rpcContext(ctx, sc.Parent, "thread/unsubscribe", map[string]any{"threadId": t}, nil)
		ok, err := settle(t, err)
		if err != nil {
			return err
		}
		if ok {
			kept = append(kept, t)
		}
	}
	threads = kept
	for len(threads) > 0 {
		v, err := m.rpcContext(ctx, sc.Parent, "thread/loaded/list", map[string]any{}, nil)
		if err != nil {
			return err
		}
		loaded := map[string]bool{}
		for _, t := range strs(v["data"]) {
			loaded[t] = true
		}
		left := threads[:0]
		for _, t := range threads {
			if loaded[t] {
				left = append(left, t)
			}
		}
		threads = left
		if len(threads) == 0 {
			// Interrupting a turn can reveal a child that was being spawned.
			// Re-read ownership after the loaded-list response, never finish
			// based only on the snapshot taken before the interrupt.
			m.mu.Lock()
			remaining := false
			for thread, owned := range sc.threads {
				if owned && !loaded[thread] {
					sc.threads[thread] = false
				}
				if owned && loaded[thread] {
					remaining = true
				}
			}
			m.mu.Unlock()
			if remaining {
				return m.cleanupSide(ctx, sc)
			}
			break
		}
		select {
		case <-ctx.Done():
			return errors.New("side threads still loaded; retry cleanup")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}

// cleanupThread stops what one side thread may still run.
func (m *Manager) cleanupThread(ctx context.Context, sc *sideRuntime, thread string) error {
	// Clearing the goal stops engine-initiated follow-ups during teardown.
	if _, err := m.rpcContext(ctx, sc.Parent, "thread/goal/clear", map[string]any{"threadId": thread}, nil); err != nil && !strings.Contains(err.Error(), "ephemeral thread does not support goals") {
		return err
	}
	m.mu.Lock()
	turn := sc.turns[thread]
	m.mu.Unlock()
	if turn != "" {
		if _, err := m.rpcContext(ctx, sc.Parent, "turn/interrupt", map[string]any{"threadId": thread, "turnId": turn}, nil); err != nil {
			return err
		}
	}
	_, err := m.rpcContext(ctx, sc.Parent, "thread/backgroundTerminals/clean", map[string]any{"threadId": thread}, nil)
	return err
}
func (s *Server) sideAPI(w http.ResponseWriter, r *http.Request, path string) {
	m := s.Manager
	parts := strings.Split(path, "/")
	var out any
	var err error
	if parts[0] == "sessions" {
		if len(parts) != 3 {
			http.NotFound(w, r)
			return
		}
		if r.Method == "GET" {
			out, err = m.ListSides(parts[1])
		} else if r.Method == "POST" {
			out, err = m.OpenSide(parts[1])
		} else {
			w.WriteHeader(405)
			return
		}
	} else {
		if len(parts) < 2 || len(parts) > 3 {
			http.NotFound(w, r)
			return
		}
		id := parts[1]
		var sc SideChat
		sc, err = m.Side(id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		action := ""
		if len(parts) == 3 {
			action = parts[2]
		}
		if r.Method == "GET" {
			switch action {
			case "":
				out = sc
			case "history":
				before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
				var events []Event
				events, err = m.sideHistory(id, before)
				cursor := int64(0)
				if len(events) == 200 {
					cursor = events[0].Seq
				}
				out = map[string]any{"events": events, "before": cursor, "hasMore": cursor != 0}
			case "live":
				m.mu.Lock()
				pending := []any{}
				if active := m.sides[id]; active != nil {
					for key, v := range active.rt.pending {
						pending = append(pending, map[string]any{"id": key, "request": v})
					}
				}
				m.mu.Unlock()
				out = map[string]any{"side": sc, "pending": pending}
			case "events":
				s.sideStream(w, r, id)
				return
			default:
				http.NotFound(w, r)
				return
			}
		} else if r.Method == "POST" {
			var v struct {
				Text      string
				RequestID string `json:"requestId"`
				Confirmed bool
				ID        string
				Decision  string
				Answers   map[string]any
			}
			if err = decode(w, r, &v); err == nil {
				switch action {
				case "send":
					err = m.SendSide(id, v.Text, v.RequestID)
				case "reply":
					err = m.ReplySide(id, v.ID, v.Decision, v.Answers)
				case "close":
					err = m.CloseSide(id, v.Confirmed)
				default:
					http.NotFound(w, r)
					return
				}
			}
			out = map[string]bool{"ok": err == nil}
		} else {
			w.WriteHeader(405)
			return
		}
	}
	if err != nil {
		bad(w, err)
		return
	}
	writeJSON(w, out)
}
func (s *Server) sideStream(w http.ResponseWriter, r *http.Request, id string) {
	f, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if h := r.Header.Get("Last-Event-ID"); h != "" {
		after, _ = strconv.ParseInt(h, 10, 64)
	}
	// Woken by any change, with a keepalive while nothing happens.
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	sent := ""
	for {
		if !s.authorized(r) {
			return
		}
		changed := s.Manager.changes.C()
		rows, err := s.Manager.store.db.Query(`SELECT seq,kind,data,time FROM side_events WHERE session=? AND seq>? ORDER BY seq LIMIT 200`, id, after)
		if err != nil {
			return
		}
		events := []Event{}
		for rows.Next() {
			e := Event{Session: id}
			var b string
			rows.Scan(&e.Seq, &e.Kind, &b, &e.Time)
			e.Data = json.RawMessage(b)
			events = append(events, e)
		}
		rows.Close()
		for _, e := range events {
			b, _ := json.Marshal(e)
			if _, err = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.Seq, b); err != nil {
				return
			}
			after = e.Seq
		}
		sc, err := s.Manager.Side(id)
		if err != nil {
			return
		}
		if b, _ := json.Marshal(sc); string(b) != sent {
			sent = string(b)
			if _, err = fmt.Fprintf(w, "event: status\ndata: %s\n\n", b); err != nil {
				return
			}
		}
		f.Flush()
		if sc.Status == "closed" || sc.Status == "interrupted" {
			return
		}
		if len(events) == 200 {
			continue
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-tick.C:
			// Keepalive: a comment the page ignores.
			if _, err = fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			f.Flush()
		}
	}
}
