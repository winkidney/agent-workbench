package workbench

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Steering sends a message into the turn that is running, as the CLIs do:
// Codex adds it to the active turn (turn/steer). Claude takes a message the
// CLI holds in at its next step, as when Enter is pressed in its TUI while it
// works; SendNow is the TUI's Ctrl+X Ctrl+S for one still waiting.

// SteerResult says what became of a message sent to steer.
type SteerResult struct {
	// Steered: the running turn takes it in.
	Steered bool `json:"steered,omitempty"`
	// Queued: it waits in the workbench queue for the turn to end.
	Queued bool `json:"queued,omitempty"`
	// Sent: no turn was running, so it started one.
	Sent bool `json:"sent,omitempty"`
	// Reason: why it was queued instead of steered.
	Reason string `json:"reason,omitempty"`
}

// steered is a Claude message sent while a turn ran, until a turn answers it.
type steered struct {
	text  string
	state string // the CLI's command_lifecycle state: queued, started
}

// Steer sends text into the running turn, or starts a turn when none runs.
func (m *Manager) Steer(id, text string) (SteerResult, error) {
	if strings.TrimSpace(text) == "" || len(text) > 1024*1024 {
		return SteerResult{}, errors.New("message must contain 1 to 1048576 bytes")
	}
	if m.findUnlocked(id) == nil {
		if err := m.Attach(id); err != nil {
			return SteerResult{}, err
		}
	}
	m.mu.Lock()
	r := m.find(id)
	if r == nil {
		m.mu.Unlock()
		return SteerResult{}, errors.New("session not found")
	}
	running := r.proc != nil && r.ready && r.turn
	agent, cwd := r.session.Agent, r.session.Cwd
	m.mu.Unlock()
	if !running {
		return m.queueInstead(id, text, "")
	}
	if isCommand(text) {
		return SteerResult{}, errors.New("commands run after the turn; queue this instead")
	}
	prepared, attach, err := m.prepareFor(agent, cwd, text)
	if err != nil {
		return SteerResult{}, err
	}
	if agent == "codex" {
		return m.steerCodex(id, text, prepared, attach)
	}
	if isCommand(prepared) {
		return SteerResult{}, errors.New("skills run after the turn; queue this instead")
	}
	return m.steerClaude(id, text, prepared)
}

func isCommand(text string) bool {
	t := strings.TrimSpace(text)
	return strings.HasPrefix(t, "/") && !strings.HasPrefix(t, "//")
}

// queueInstead hands the message to the workbench queue, which sends it at
// once when no turn runs.
func (m *Manager) queueInstead(id, text, reason string) (SteerResult, error) {
	queued, err := m.Enqueue(id, text)
	if err != nil {
		return SteerResult{}, err
	}
	if !queued {
		reason = ""
	}
	return SteerResult{Queued: queued, Sent: !queued, Reason: reason}, nil
}

// steerable reports whether the session's turn can take a message now, or
// why not ("" when it can).
func (m *Manager) steerable(id string) (r *runtime, reason string) {
	r = m.find(id)
	switch {
	case r == nil || r.proc == nil || !r.turn:
		return r, "the turn had ended"
	case len(r.pending) > 0:
		return r, "answer the pending request first"
	}
	return r, ""
}

func (m *Manager) steerCodex(id, text, prepared string, attach []Skill) (SteerResult, error) {
	// turn/steer names the active turn, known once turn/started arrives.
	deadline := time.Now().Add(2 * time.Second)
	var turnID, native string
	for {
		m.mu.Lock()
		r, reason := m.steerable(id)
		if reason != "" {
			m.mu.Unlock()
			return m.queueInstead(id, text, reason)
		}
		turnID, native = r.turnID, r.session.NativeID
		if turnID != "" {
			// Shown at once, as a sent message is; Codex's own item replaces it.
			m.emit(r.session.ID, "user", map[string]any{"text": prepared, "steered": true})
			m.mu.Unlock()
			break
		}
		m.mu.Unlock()
		if time.Now().After(deadline) {
			return m.queueInstead(id, text, "the turn has not started yet")
		}
		time.Sleep(50 * time.Millisecond)
	}
	input := []any{}
	for _, s := range attach {
		input = append(input, map[string]any{"type": "skill", "name": s.Name, "path": s.Path})
	}
	input = append(input, map[string]any{"type": "text", "text": prepared})
	if _, err := m.rpc(id, "turn/steer", map[string]any{"threadId": native, "input": input, "expectedTurnId": turnID}); err != nil {
		m.retract(id, prepared)
		return m.queueInstead(id, text, "Codex did not take it ("+err.Error()+")")
	}
	// A message into the turn answers (or sets aside) Codex's async questions.
	m.mu.Lock()
	if r := m.find(id); r != nil {
		userActed(r)
		m.syncAttention(r)
	}
	m.mu.Unlock()
	return SteerResult{Steered: true}, nil
}

func (m *Manager) steerClaude(id, text, prepared string) (SteerResult, error) {
	m.mu.Lock()
	r, reason := m.steerable(id)
	if reason != "" {
		m.mu.Unlock()
		return m.queueInstead(id, text, reason)
	}
	u := uuid.NewString()
	if r.steering == nil {
		r.steering = map[string]*steered{}
	}
	r.steering[u] = &steered{text: prepared}
	// Claude does not echo the message on its stream; history shows it later.
	// The uuid lets the page offer send-now while it waits.
	m.emit(r.session.ID, "user", map[string]any{"text": prepared, "steered": true, "uuid": u, "sendNow": r.caps["interrupt_send_now_v1"]})
	err := r.proc.Input(map[string]any{"type": "user", "uuid": u, "message": map[string]any{"role": "user", "content": prepared}, "parent_tool_use_id": nil, "session_id": r.session.NativeID})
	if err != nil {
		delete(r.steering, u)
	}
	m.mu.Unlock()
	if err != nil {
		m.retract(id, prepared)
		return SteerResult{}, err
	}
	return SteerResult{Steered: true}, nil
}

// SendNow asks Claude to take a steered message that is still waiting in
// now, as its TUI's send-now key (Ctrl+X Ctrl+S) does. It is not a stop: the
// CLI moves what the turn waits on to the background, or ends the turn (a
// reply in progress is cut off) and runs the message next. It reports the
// CLI's answer (moved, aborted, nothing_waiting, …).
func (m *Manager) SendNow(id, u string) (string, error) {
	m.mu.Lock()
	r := m.find(id)
	if r == nil || r.proc == nil {
		m.mu.Unlock()
		return "", errors.New("session is not running")
	}
	s, waiting := r.steering[u]
	supported := r.caps["interrupt_send_now_v1"]
	m.mu.Unlock()
	switch {
	case !supported:
		return "", errors.New("this Claude Code version has no send-now")
	case !waiting || s.state == "started":
		return "", errors.New("Claude has already taken this message in")
	}
	res, err := m.rpc(id, "interrupt", map[string]any{"send_now": true, "message_uuid": u})
	if err != nil {
		return "", err
	}
	return str(res["send_now"]), nil
}

// retract takes back a message shown as steered that did not reach the turn.
func (m *Manager) retract(id, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.find(id); r != nil {
		m.emit(r.session.ID, "retract", map[string]any{"text": text})
	}
}

// claudeLifecycle follows a steered message through Claude's
// command_lifecycle frames. A cancelled one goes back to the workbench queue,
// paused, so it is not lost. Runs with m.mu held.
func (m *Manager) claudeLifecycle(r *runtime, v map[string]any) {
	u := str(v["command_uuid"])
	s, ok := r.steering[u]
	if !ok {
		return
	}
	state := str(v["state"])
	switch state {
	case "completed":
		delete(r.steering, u)
	case "cancelled":
		delete(r.steering, u)
		r.queue = append([]queued{{ID: token()[:12], Text: s.text}}, r.queue...)
		r.queuePaused = true
		m.emitQueue(r)
	default:
		s.state = state
	}
	// The page shows a steer as waiting until a turn takes it in.
	m.emit(r.session.ID, "steer", map[string]any{"uuid": u, "state": state})
}

// SteerQueued takes a message out of the workbench queue and steers it; it
// goes back to the front of the queue if that fails.
func (m *Manager) SteerQueued(id, qid string) (SteerResult, error) {
	m.mu.Lock()
	r := m.find(id)
	if r == nil {
		m.mu.Unlock()
		return SteerResult{}, errors.New("session not found")
	}
	var item queued
	found := false
	for i, q := range r.queue {
		if q.ID == qid {
			item, found = q, true
			r.queue = append(r.queue[:i:i], r.queue[i+1:]...)
			break
		}
	}
	if !found {
		m.mu.Unlock()
		return SteerResult{}, errors.New("queued message not found")
	}
	if len(r.queue) == 0 {
		r.queuePaused = false
	}
	m.emitQueue(r)
	m.mu.Unlock()
	res, err := m.Steer(id, item.Text)
	if err != nil {
		m.mu.Lock()
		if r := m.find(id); r != nil {
			r.queue = append([]queued{item}, r.queue...)
			m.emitQueue(r)
		}
		m.mu.Unlock()
	}
	return res, err
}
