package workbench

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A Codex side turn that runs past the start limit (tools, an approval
// waiting on you) keeps going, finishes, and the side chat takes the next
// question. It used to end in "context deadline exceeded" and refuse
// everything after.
func TestLongSideTurnOutlivesTheStartLimit(t *testing.T) {
	old := sideStartLimit
	sideStartLimit = 50 * time.Millisecond
	t.Cleanup(func() { sideStartLimit = old })
	m, p, parent := sideFixture(t, "codex")
	sc, _ := m.OpenSide(parent)
	var turns atomic.Int32
	p.reply = func(v map[string]any) {
		method := str(v["method"])
		if method == "" {
			return
		}
		result := map[string]any{}
		switch method {
		case "thread/fork":
			result = map[string]any{"thread": map[string]any{"id": "side-native"}}
		case "turn/start":
			turns.Add(1)
			m.receive(parent, 1, map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "side-native", "turn": map[string]any{"id": "side-turn"}}})
		}
		m.receive(parent, 1, map[string]any{"id": v["id"], "result": result})
	}
	if _, err := m.SendSide(sc.ID, "a long question", "one"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(4 * sideStartLimit)
	if s, _ := m.Side(sc.ID); s.Status != "running" || s.Error != "" {
		t.Fatalf("past the start limit: %+v", s)
	}
	m.receive(parent, 1, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "side-native", "turn": map[string]any{"id": "side-turn", "status": "completed"}}})
	awaitSide(t, m, sc.ID, "idle")
	// The answer's goroutine notices the completion within a poll.
	for deadline := time.Now().Add(time.Second); ; time.Sleep(5 * time.Millisecond) {
		_, err := m.SendSide(sc.ID, "and another", "two")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("next question: %v", err)
		}
	}
	for deadline := time.Now().Add(2 * time.Second); turns.Load() < 2; time.Sleep(2 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("second turn never started")
		}
	}
	m.CloseSide(sc.ID, true)
	awaitSide(t, m, sc.ID, "closed")
}

// A parent that never wakes ends the attempt with a readable error, and the
// side chat stays usable.
func TestSideGivesUpWaitingForTheParent(t *testing.T) {
	old := sideStartLimit
	sideStartLimit = 50 * time.Millisecond
	t.Cleanup(func() { sideStartLimit = old })
	m, _, parent := sideFixture(t, "codex")
	m.mu.Lock()
	m.find(parent).ready = false
	m.mu.Unlock()
	sc, _ := m.OpenSide(parent)
	if _, err := m.SendSide(sc.ID, "question", "one"); err != nil {
		t.Fatal(err)
	}
	awaitSide(t, m, sc.ID, "idle")
	s, _ := m.Side(sc.ID)
	if !strings.Contains(s.Error, "did not start answering") || strings.Contains(s.Error, "deadline") {
		t.Fatalf("error: %q", s.Error)
	}
	m.CloseSide(sc.ID, true)
	awaitSide(t, m, sc.ID, "closed")
}
