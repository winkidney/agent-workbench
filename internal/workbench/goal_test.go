package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// responder answers every app-server request of a method with the latest
// result given for it, each request once.
func responder(t *testing.T, m *Manager, p *fakeProcess, r *runtime) func(method string, result map[string]any) {
	t.Helper()
	var mu sync.Mutex
	results := map[string]map[string]any{}
	answered := map[any]bool{}
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(2 * time.Millisecond):
			}
			p.mu.Lock()
			var todo []map[string]any
			for _, in := range p.in {
				mu.Lock()
				_, known := results[str(in["method"])]
				mu.Unlock()
				if in["id"] != nil && known && !answered[in["id"]] {
					answered[in["id"]] = true
					todo = append(todo, in)
				}
			}
			p.mu.Unlock()
			for _, in := range todo {
				mu.Lock()
				result := results[str(in["method"])]
				mu.Unlock()
				m.receive(r.session.ID, r.generation, map[string]any{"id": in["id"], "result": result})
			}
		}
	}()
	return func(method string, result map[string]any) {
		mu.Lock()
		results[method] = result
		mu.Unlock()
	}
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func threadGoal(objective, status string) map[string]any {
	return map[string]any{"threadId": "th", "objective": objective, "status": status, "tokensUsed": 1200.0, "timeUsedSeconds": 75.0, "createdAt": 1.0, "updatedAt": 2.0}
}

func TestCodexGoalSubcommandsAndUpdates(t *testing.T) {
	m, p, r, _ := codexReady(t)
	id := r.session.ID
	respond := responder(t, m, p, r)

	// The goal is read when the engine starts.
	respond("thread/goal/get", map[string]any{"goal": threadGoal("ship it", "active")})
	waitUntil(t, "goal on start", func() bool { g := m.Telemetry(id).Goal; return g != nil && g.Status == "active" })
	if got := len(notices(t, m, id)); got != 0 {
		t.Fatalf("reading the goal on start is silent: %d notices", got)
	}

	setParams := func() map[string]any { return obj(sentMethod(p, "thread/goal/set")["params"]) }
	respond("thread/goal/set", map[string]any{"goal": threadGoal("ship it", "paused")})
	if err := runCmd(m, id, "goal", "pause"); err != nil {
		t.Fatal(err)
	}
	if params := setParams(); params["status"] != "paused" || params["objective"] != nil || params["origin"] != "user" {
		t.Fatalf("/goal pause: %v", params)
	}
	if g := m.Telemetry(id).Goal; g == nil || g.Status != "paused" {
		t.Fatalf("paused goal: %+v", g)
	}

	respond("thread/goal/set", map[string]any{"goal": threadGoal("ship it", "active")})
	if err := runCmd(m, id, "goal", "Resume"); err != nil {
		t.Fatal(err)
	}
	if params := setParams(); params["status"] != "active" {
		t.Fatalf("/goal resume: %v", params)
	}
	if err := runCmd(m, id, "goal", "edit ship it today"); err != nil {
		t.Fatal(err)
	}
	if params := setParams(); params["objective"] != "ship it today" || params["status"] != nil {
		t.Fatalf("/goal edit: %v", params)
	}
	if err := runCmd(m, id, "goal", "edit"); err == nil {
		t.Fatal("/goal edit without text should be refused")
	}
	// A subcommand word followed by text is an objective. A new objective
	// replaces the goal: cleared, then set active.
	respond("thread/goal/clear", map[string]any{})
	if err := runCmd(m, id, "goal", "pause the deploy until Friday"); err != nil {
		t.Fatal(err)
	}
	if params := setParams(); params["objective"] != "pause the deploy until Friday" || params["status"] != "active" {
		t.Fatalf("objective: %v", params)
	}
	p.mu.Lock()
	order := []string{}
	for _, in := range p.in {
		if m := str(in["method"]); strings.HasPrefix(m, "thread/goal/") && m != "thread/goal/get" {
			order = append(order, m)
		}
	}
	p.mu.Unlock()
	if strings.Join(order[len(order)-2:], " ") != "thread/goal/clear thread/goal/set" {
		t.Fatalf("replace order: %v", order)
	}
	if err := runCmd(m, id, "goal", ""); err != nil {
		t.Fatal(err)
	}
	got := notices(t, m, id)
	panel := got[len(got)-1]
	for _, want := range []string{"Goal: ship it", "Status: active", "Time used: 1m 15s", "Tokens used: 1.2k", "Commands: /goal edit, /goal pause, /goal clear"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("panel %q lacks %q", panel, want)
		}
	}

	// Codex reports the end it reached; the page gets a notice.
	m.receive(id, r.generation, map[string]any{"method": "thread/goal/updated", "params": map[string]any{"threadId": "th", "goal": threadGoal("ship it", "complete")}})
	got = notices(t, m, id)
	if last := got[len(got)-1]; last != "Goal achieved · 1m 15s · 1.2k tokens" {
		t.Fatalf("achieved notice: %q", last)
	}
	m.receive(id, r.generation, map[string]any{"method": "thread/goal/cleared", "params": map[string]any{"threadId": "th"}})
	if g := m.Telemetry(id).Goal; g != nil {
		t.Fatalf("cleared goal: %+v", g)
	}
}

func TestCodexTurnsStartedByCodexRun(t *testing.T) {
	m, p, r, _ := codexReady(t)
	id := r.session.ID
	respond := responder(t, m, p, r)
	respond("thread/goal/get", map[string]any{"goal": threadGoal("ship it", "active")})
	waitUntil(t, "goal", func() bool { return m.Telemetry(id).Goal != nil })

	// No turn/start from the page: Codex pursues the goal on its own.
	m.receive(id, r.generation, map[string]any{"method": "turn/started", "params": map[string]any{"turn": map[string]any{"id": "g1"}}})
	m.mu.Lock()
	running, status, turnID := r.turn, r.session.Status, r.turnID
	m.mu.Unlock()
	if !running || status != "running" || turnID != "g1" {
		t.Fatalf("goal turn: %v %s %s", running, status, turnID)
	}
	// A message sent now joins the running turn.
	respond("turn/steer", map[string]any{"turnId": "g1"})
	if _, err := m.Steer(id, "also update the docs"); err != nil {
		t.Fatal(err)
	}
	if steer := obj(sentMethod(p, "turn/steer")["params"]); steer["expectedTurnId"] != "g1" {
		t.Fatalf("steer: %v", steer)
	}
	m.receive(id, r.generation, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": "g1", "status": "completed"}}})
	m.mu.Lock()
	running, status = r.turn, r.session.Status
	// Between its turns an active goal keeps the engine awake.
	r.last = time.Now().Add(-time.Hour)
	m.mu.Unlock()
	if running || status != "idle" {
		t.Fatalf("after the goal turn: %v %s", running, status)
	}
	m.Sweep()
	m.mu.Lock()
	awake := r.proc != nil
	m.setGoal(r, &Goal{Objective: "ship it", Status: "paused"})
	r.last = time.Now().Add(-time.Hour)
	m.mu.Unlock()
	if !awake {
		t.Fatal("an engine pursuing a goal was put to sleep")
	}
	m.Sweep()
	m.mu.Lock()
	awake = r.proc != nil
	m.mu.Unlock()
	if awake {
		t.Fatal("a paused goal should not keep the engine awake")
	}
}

func TestClaudeGoalFromJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	write := func(lines ...string) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, l := range lines {
			_, _ = f.WriteString(l + "\n")
		}
	}
	scan := &claudeGoalScan{path: path}
	read := func() *Goal {
		t.Helper()
		if _, err := scan.read(); err != nil {
			t.Fatal(err)
		}
		return scan.goal
	}
	write(
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
		`{"type":"attachment","timestamp":"t1","attachment":{"type":"goal_status","met":false,"sentinel":true,"condition":"tests pass"}}`,
	)
	if g := read(); g == nil || g.Objective != "tests pass" || g.Status != "active" || g.Checks != 0 {
		t.Fatalf("set: %+v", g)
	}
	write(
		`{"type":"attachment","timestamp":"t2","attachment":{"type":"goal_status","met":false,"condition":"tests pass","reason":"2 failing"}}`,
		`{"type":"attachment","timestamp":"t3","attachment":{"type":"goal_status","met":false,"condition":"tests pass","reason":"1 failing"}}`,
	)
	if g := read(); g.Status != "active" || g.Checks != 2 || g.LastReason != "1 failing" {
		t.Fatalf("not met: %+v", g)
	}
	// A line still being written waits for its end.
	met := `{"type":"attachment","timestamp":"t4","attachment":{"type":"goal_status","met":true,"condition":"tests pass","iterations":3,"durationMs":125000,"tokens":42000}}`
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(met[:40])
	_ = f.Close()
	if g := read(); g.Status != "active" {
		t.Fatalf("partial line: %+v", g)
	}
	write(met[40:])
	if g := read(); g.Status != "met" || g.Checks != 3 || g.Seconds != 125 || g.TokensUsed != 42000 {
		t.Fatalf("met: %+v", g)
	}
	if got := goalNotice(&Goal{Objective: "tests pass", Status: "active"}, scan.goal); got != "Goal met · 3 checks · 2m 5s · 42k tokens" {
		t.Fatalf("met notice: %q", got)
	}
	write(`{"type":"attachment","timestamp":"t5","attachment":{"type":"goal_status","met":false,"sentinel":true,"condition":"ship"}}`,
		`{"type":"attachment","timestamp":"t6","attachment":{"type":"goal_status","met":true,"sentinel":true,"condition":"ship"}}`)
	if g := read(); g != nil {
		t.Fatalf("cleared: %+v", g)
	}
	write(`{"type":"attachment","timestamp":"t7","attachment":{"type":"goal_status","met":false,"failed":true,"condition":"ship","reason":"no access"}}`)
	if g := read(); g.Status != "failed" || g.LastReason != "no access" {
		t.Fatalf("failed: %+v", g)
	}
	// Read from the start again after the file was replaced.
	_ = os.WriteFile(path, []byte(`{"type":"attachment","timestamp":"t1","attachment":{"type":"goal_status","met":false,"sentinel":true,"condition":"x"}}`+"\n"), 0o600)
	if g := read(); g == nil || g.Objective != "x" {
		t.Fatalf("rescan: %+v", g)
	}
}

func TestClaudeGoalCommandHint(t *testing.T) {
	cat := buildCatalog("claude", metadata{Commands: []any{map[string]any{"name": "goal", "description": "Set a goal"}}})
	for _, c := range cat.Commands {
		if c.Name == "goal" && c.ArgumentHint == "[<condition> | clear]" {
			return
		}
	}
	t.Fatalf("goal hint: %+v", cat.Commands)
}

func TestGoalFormatting(t *testing.T) {
	for in, want := range map[float64]string{0: "0s", 59: "59s", 60: "1m", 75: "1m 15s", 3600: "1h", 3725: "1h 2m"} {
		if got := shortDuration(in); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{12: "12", 1000: "1k", 1260: "1.3k", 2_500_000: "2.5M"} {
		if got := compactTokens(in); got != want {
			t.Errorf("compactTokens(%v) = %q, want %q", in, got, want)
		}
	}
}
