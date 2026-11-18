package workbench

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// awaitSideTurn waits until the side's Codex turn has an id.
func awaitSideTurn(t *testing.T, m *Manager, id string) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(2 * time.Millisecond) {
		m.mu.Lock()
		sc := m.sides[id]
		turn := sc.turns[sc.native]
		m.mu.Unlock()
		if turn != "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no side turn")
		}
	}
}

// sideNotices are the notice texts in a side chat's record.
func sideNotices(t *testing.T, m *Manager, id string) []string {
	t.Helper()
	history, err := m.sideHistory(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range history {
		if e.Kind == "notice" {
			out = append(out, string(e.Data))
		}
	}
	return out
}

func TestCodexSideSteersAndStops(t *testing.T) {
	m, p, parent := sideFixture(t, "codex")
	sc, _ := m.OpenSide(parent)
	var mu sync.Mutex
	var steer, interrupt map[string]any
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
			m.receive(parent, 1, map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "side-native", "turn": map[string]any{"id": "side-turn"}}})
		case "turn/steer":
			mu.Lock()
			steer = obj(v["params"])
			mu.Unlock()
		case "turn/interrupt":
			mu.Lock()
			interrupt = obj(v["params"])
			mu.Unlock()
			m.receive(parent, 1, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "side-native", "turn": map[string]any{"id": "side-turn", "status": "interrupted"}}})
		}
		m.receive(parent, 1, map[string]any{"id": v["id"], "result": result})
	}
	if _, err := m.SendSide(sc.ID, "question", "one"); err != nil {
		t.Fatal(err)
	}
	awaitSideTurn(t, m, sc.ID)
	res, err := m.SendSide(sc.ID, "also look at this", "two")
	if err != nil || !res.Steered {
		t.Fatalf("steer: %+v %v", res, err)
	}
	mu.Lock()
	if steer["threadId"] != "side-native" || steer["expectedTurnId"] != "side-turn" {
		t.Fatalf("steered %+v", steer)
	}
	mu.Unlock()
	if s, _ := m.Side(sc.ID); s.Status != "running" {
		t.Fatalf("steer ended the answer: %+v", s)
	}
	// The steered message is in the record, marked as such.
	history, _ := m.sideHistory(sc.ID, 0)
	steered := false
	for _, e := range history {
		steered = steered || e.Kind == "user" && strings.Contains(string(e.Data), `"steered":true`)
	}
	if !steered {
		t.Fatal("steered message not recorded")
	}
	if err := m.InterruptSide(sc.ID); err != nil {
		t.Fatal(err)
	}
	awaitSide(t, m, sc.ID, "idle")
	mu.Lock()
	if interrupt["threadId"] != "side-native" || interrupt["turnId"] != "side-turn" {
		t.Fatalf("interrupted %+v", interrupt)
	}
	mu.Unlock()
	// The next question may follow once the answer's goroutine settles.
	for deadline := time.Now().Add(time.Second); ; time.Sleep(5 * time.Millisecond) {
		_, err := m.SendSide(sc.ID, "a new question", "three")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after stop: %v", err)
		}
	}
	if s, _ := m.Side(sc.ID); s.Error != "" {
		t.Fatalf("a stop reported as an error: %+v", s)
	}
	if notices := sideNotices(t, m, sc.ID); len(notices) != 1 || !strings.Contains(notices[0], "Stopped") {
		t.Fatalf("notices %v", notices)
	}
}

func TestClaudeSideRefusesSteerAndStops(t *testing.T) {
	m, p, parent := sideFixture(t, "claude")
	sc, _ := m.OpenSide(parent)
	asked := make(chan string, 1)
	p.reply = func(v map[string]any) {
		if str(obj(v["request"])["subtype"]) == "side_question" {
			asked <- str(v["request_id"])
		}
		if v["type"] == "control_cancel_request" {
			m.receive(parent, 1, map[string]any{"type": "control_response", "response": map[string]any{"request_id": v["request_id"], "subtype": "error", "error": "Side question cancelled"}})
		}
	}
	if _, err := m.SendSide(sc.ID, "question", "one"); err != nil {
		t.Fatal(err)
	}
	<-asked
	if _, err := m.SendSide(sc.ID, "more", "two"); err == nil || !strings.Contains(err.Error(), "stop it") {
		t.Fatalf("claude steer: %v", err)
	}
	if err := m.InterruptSide(sc.ID); err != nil {
		t.Fatal(err)
	}
	awaitSide(t, m, sc.ID, "idle")
	if s, _ := m.Side(sc.ID); s.Error != "" {
		t.Fatalf("a stop reported as an error: %+v", s)
	}
	if notices := sideNotices(t, m, sc.ID); len(notices) != 1 || !strings.Contains(notices[0], "Stopped") {
		t.Fatalf("notices %v", notices)
	}
}

func TestSideStopWhileWaking(t *testing.T) {
	m, p, parent := sideFixture(t, "codex")
	sc, _ := m.OpenSide(parent)
	m.mu.Lock()
	m.find(parent).ready = false
	m.mu.Unlock()
	if _, err := m.SendSide(sc.ID, "question", "one"); err != nil {
		t.Fatal(err)
	}
	if s, _ := m.Side(sc.ID); s.Phase != "waking" {
		t.Fatalf("not waking: %+v", s)
	}
	if err := m.InterruptSide(sc.ID); err != nil {
		t.Fatal(err)
	}
	awaitSide(t, m, sc.ID, "idle")
	if s, _ := m.Side(sc.ID); s.Error != "" {
		t.Fatalf("a stop reported as an error: %+v", s)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, v := range p.in {
		if str(v["method"]) == "thread/fork" {
			t.Fatal("forked after the stop")
		}
	}
}
