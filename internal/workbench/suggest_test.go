package workbench

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"
)

// claudeTurn runs one turn the CLI started, to its result (m.mu not held).
func claudeTurn(m *Manager, r *runtime) {
	m.receive(r.session.ID, r.generation, map[string]any{"type": "stream_event", "event": map[string]any{"type": "message_start", "message": map[string]any{"id": "m1"}}})
	m.receive(r.session.ID, r.generation, map[string]any{"type": "result", "subtype": "success"})
}

func suggestionNow(m *Manager, id string) string { return m.Telemetry(id).Suggestion }

func pausesSent(p *fakeProcess) []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []map[string]any
	for _, in := range p.in {
		if obj(in["request"])["subtype"] == "set_prompt_suggestions_paused" {
			out = append(out, in)
		}
	}
	return out
}

func TestClaudeSuggestionsReachTheComposer(t *testing.T) {
	m, _, r := claudeReady(t)
	id := r.session.ID
	suggest := func(text string) {
		m.receive(id, r.generation, map[string]any{"type": "prompt_suggestion", "suggestion": text, "uuid": "u", "session_id": "native-1"})
	}

	claudeTurn(m, r)
	suggest("  run the tests ")
	if got := suggestionNow(m, id); got != "run the tests" {
		t.Fatalf("after the turn: %q", got)
	}
	// Kept for the composer only, not in the transcript.
	for _, e := range eventsOf(m, id, "native") {
		if e["type"] == "prompt_suggestion" {
			t.Fatal("stored in the transcript")
		}
	}

	// Hidden while a request waits; back once it is answered.
	m.receive(id, r.generation, map[string]any{"type": "control_request", "request_id": "a1", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{}}})
	if got := suggestionNow(m, id); got != "" {
		t.Fatalf("while a request waits: %q", got)
	}
	m.receive(id, r.generation, map[string]any{"type": "control_cancel_request", "request_id": "a1"})

	// The next turn ends it, and one that arrives during a turn is stale.
	m.receive(id, r.generation, map[string]any{"type": "stream_event", "event": map[string]any{"type": "message_start"}})
	if got := suggestionNow(m, id); got != "" {
		t.Fatalf("in the next turn: %q", got)
	}
	suggest("commit this")
	m.receive(id, r.generation, map[string]any{"type": "result", "subtype": "success"})
	if got := suggestionNow(m, id); got != "" {
		t.Fatalf("stale: %q", got)
	}

	// One line only; and the engine's end clears it.
	suggest("first\nsecond")
	if got := suggestionNow(m, id); got != "" {
		t.Fatalf("two lines: %q", got)
	}
	suggest("push it")
	m.mu.Lock()
	userActed(r)
	m.mu.Unlock()
	if got := suggestionNow(m, id); got != "" {
		t.Fatalf("after the user acted: %q", got)
	}
	suggest("push it")
	m.exited(id, r.generation, nil)
	m.mu.Lock()
	left := r.suggestion
	m.mu.Unlock()
	if left != "" || suggestionNow(m, id) != "" {
		t.Fatalf("after the engine ended: %q", left)
	}
}

func TestSuggestionsCanBeTurnedOff(t *testing.T) {
	m, p, r := claudeReady(t)
	id := r.session.ID
	if !m.Preferences().Suggestions {
		t.Fatal("on by default")
	}
	claudeTurn(m, r)
	m.receive(id, r.generation, map[string]any{"type": "prompt_suggestion", "suggestion": "run it"})

	pause := func(want bool) {
		t.Helper()
		v := sent(p, func(in map[string]any) bool {
			req := obj(in["request"])
			return req["subtype"] == "set_prompt_suggestions_paused" && req["paused"] == want
		})
		if v == nil {
			t.Fatalf("no pause %v sent", want)
		}
		m.receive(id, r.generation, map[string]any{"type": "control_response", "response": map[string]any{"request_id": v["request_id"], "subtype": "success"}})
	}
	if err := m.SetPreferences(Preferences{Suggestions: false}); err != nil {
		t.Fatal(err)
	}
	pause(true)
	if m.Preferences().Suggestions || suggestionNow(m, id) != "" {
		t.Fatalf("off: %v %q", m.Preferences(), suggestionNow(m, id))
	}
	// A CLI that makes one anyway is not listened to.
	m.receive(id, r.generation, map[string]any{"type": "prompt_suggestion", "suggestion": "run it"})
	if got := suggestionNow(m, id); got != "" {
		t.Fatalf("while off: %q", got)
	}

	// An engine that starts while they are off is paused at once.
	before := len(pausesSent(p))
	s, _ := m.Create("claude", t.TempDir(), "later")
	_ = m.Start(s.ID)
	r2 := m.sessions[s.ID]
	m.receive(s.ID, r2.generation, map[string]any{"type": "control_response", "response": map[string]any{"request_id": "init", "subtype": "success"}})
	var v map[string]any
	for deadline := time.Now().Add(2 * time.Second); v == nil && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if all := pausesSent(p); len(all) > before {
			v = all[len(all)-1]
		}
	}
	if v == nil || obj(v["request"])["paused"] != true {
		t.Fatalf("new engine: %v", v)
	}
	m.receive(s.ID, r2.generation, map[string]any{"type": "control_response", "response": map[string]any{"request_id": v["request_id"], "subtype": "success"}})

	if err := m.SetPreferences(Preferences{Suggestions: true}); err != nil {
		t.Fatal(err)
	}
	pause(false)
}

func TestClaudeTakesOnlyTheFlagsItLists(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("a shell script stands in for the CLI")
	}
	dir := t.TempDir()
	cli := filepath.Join(dir, "claude")
	help := "  --include-partial-messages   Include partial message chunks\n  --prompt-suggestions [value]  Enable prompt suggestions\n"
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nprintf '"+help+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := claudeTakes(cli, "--include-hook-events", "--prompt-suggestions")
	if len(got) != 1 || got[0] != "--prompt-suggestions" {
		t.Fatalf("took %v", got)
	}
	if got := claudeTakes(filepath.Join(dir, "missing"), "--prompt-suggestions"); len(got) != 0 {
		t.Fatalf("missing binary: %v", got)
	}
}
