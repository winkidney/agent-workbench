package workbench

import (
	"testing"
	"time"
)

// sweptAwake reports whether the session's engine still runs after a sweep, with
// the engine last heard from quiet ago.
func sweptAwake(m *Manager, r *runtime, quiet time.Duration) bool {
	m.mu.Lock()
	r.last = time.Now().Add(-quiet)
	m.mu.Unlock()
	m.Sweep()
	m.mu.Lock()
	defer m.mu.Unlock()
	return r.proc != nil
}

// A background subagent runs inside Claude's own process: no extra process
// shows it. While Claude reports it running, the idle engine stays awake,
// for an automatic sleep and for the Sleep button alike; once it ends, the
// engine sleeps as usual.
func TestBackgroundSubagentKeepsClaudeAwake(t *testing.T) {
	m, _, r := claudeReady(t)
	id := r.session.ID
	m.receive(id, r.generation, map[string]any{"type": "system", "subtype": "task_started", "task_id": "a9", "tool_use_id": "toolu_B", "task_type": "local_agent", "is_backgrounded": true, "description": "Check the build"})
	if !sweptAwake(m, r, 10*time.Minute) {
		t.Fatal("slept while a background subagent ran")
	}
	if err := m.Stop(id, true); err == nil {
		t.Fatal("the Sleep button slept while a background subagent ran")
	}
	m.receive(id, r.generation, map[string]any{"type": "system", "subtype": "task_notification", "task_id": "a9", "status": "completed", "summary": "done"})
	if sweptAwake(m, r, 10*time.Minute) {
		t.Fatal("stayed awake after the subagent ended")
	}
}

// A Codex subagent's thread in a turn keeps its idle engine awake too.
func TestBusyCodexSubagentKeepsEngineAwake(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("codex", t.TempDir(), "children")
	_ = m.Start(s.ID)
	r := m.sessions[s.ID]
	m.receive(s.ID, r.generation, map[string]any{"id": "init", "result": map[string]any{}})
	m.receive(s.ID, r.generation, map[string]any{"id": "thread", "result": map[string]any{"thread": map[string]any{"id": "main"}}})
	m.mu.Lock()
	r.childBusy = map[string]bool{"child-1": true}
	m.mu.Unlock()
	if !sweptAwake(m, r, 10*time.Minute) {
		t.Fatal("slept while a subagent thread ran a turn")
	}
	m.mu.Lock()
	r.childBusy["child-1"] = false
	m.mu.Unlock()
	if sweptAwake(m, r, 10*time.Minute) {
		t.Fatal("stayed awake after the subagent's turn")
	}
}

// An end that was never reported does not keep an engine awake for good:
// after an hour without a word from it, the engine sleeps.
func TestUnreportedEndStopsKeepingTheEngineAwake(t *testing.T) {
	m, _, r := claudeReady(t)
	m.receive(r.session.ID, r.generation, map[string]any{"type": "system", "subtype": "task_started", "task_id": "a9", "tool_use_id": "toolu_B", "task_type": "local_agent", "is_backgrounded": true, "description": "Lost"})
	if !sweptAwake(m, r, 59*time.Minute) {
		t.Fatal("slept within the hour")
	}
	if sweptAwake(m, r, 61*time.Minute) {
		t.Fatal("a task never reported ended kept the engine awake past an hour of silence")
	}
}
