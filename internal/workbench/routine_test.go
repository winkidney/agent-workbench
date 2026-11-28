package workbench

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRoutineWhenNext(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// 2026-11-19 is a Thursday.
	now := at("2026-11-19 10:00")
	cases := []struct {
		name   string
		when   When
		anchor time.Time
		want   string
	}{
		{"daily later today", When{Kind: "daily", Time: "17:30"}, time.Time{}, "2026-11-19 17:30"},
		{"daily passed today", When{Kind: "daily", Time: "09:00"}, time.Time{}, "2026-11-20 09:00"},
		{"daily on Mondays", When{Kind: "daily", Time: "09:00", Days: []int{1}}, time.Time{}, "2026-11-23 09:00"},
		{"daily at this minute is next week's", When{Kind: "daily", Time: "10:00", Days: []int{4}}, time.Time{}, "2026-11-26 10:00"},
		{"interval from its anchor", When{Kind: "interval", Minutes: 30}, at("2026-11-19 09:10"), "2026-11-19 10:10"},
		{"interval right on a step", When{Kind: "interval", Minutes: 30}, at("2026-11-19 09:30"), "2026-11-19 10:30"},
		{"once ahead", When{Kind: "once", At: at("2026-11-20 08:00").Format(time.RFC3339)}, time.Time{}, "2026-11-20 08:00"},
		{"once passed", When{Kind: "once", At: at("2026-11-18 08:00").Format(time.RFC3339)}, time.Time{}, ""},
	}
	for _, c := range cases {
		got := c.when.next(now, c.anchor)
		text := ""
		if !got.IsZero() {
			text = got.In(time.Local).Format("2006-01-02 15:04")
		}
		if text != c.want {
			t.Errorf("%s: got %q, want %q", c.name, text, c.want)
		}
	}
}

func TestRoutineChecksWhatThePageSends(t *testing.T) {
	m, _ := fixture(t)
	dir := t.TempDir()
	now := time.Now()
	bad := []Routine{
		{When: When{Kind: "daily", Time: "9am"}, Target: &RoutineTarget{Kind: "session", Session: "x"}, Message: "hi"},
		{When: When{Kind: "interval", Minutes: 1}, Target: &RoutineTarget{Kind: "session", Session: "x"}, Message: "hi"},
		{When: When{Kind: "daily", Time: "09:00"}},
		{When: When{Kind: "daily", Time: "09:00"}, Target: &RoutineTarget{Kind: "session", Session: "x"}},
		{When: When{Kind: "daily", Time: "09:00"}, Command: &RoutineCommand{Run: "true", Dir: dir + "/missing"}},
		{When: When{Kind: "daily", Time: "09:00"}, Command: &RoutineCommand{Run: "true", Dir: dir, TimeoutMin: 90}},
		{When: When{Kind: "once", At: now.Add(-time.Hour).Format(time.RFC3339)}, Command: &RoutineCommand{Run: "true", Dir: dir}},
		{When: When{Kind: "daily", Time: "09:00"}, Target: &RoutineTarget{Kind: "new", Agent: "other", Cwd: dir}, Message: "hi"},
	}
	for i, v := range bad {
		if err := m.checkRoutine(&v, now); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	ok := Routine{When: When{Kind: "daily", Time: "09:00"}, Command: &RoutineCommand{Run: " ./check.sh ", Dir: dir}}
	if err := m.checkRoutine(&ok, now); err != nil {
		t.Fatal(err)
	}
	if ok.Command.Run != "./check.sh" || ok.Command.TimeoutMin != routineTimeoutMin || ok.Name != "./check.sh" {
		t.Fatalf("defaults not filled in: %+v %+v", ok, ok.Command)
	}
}

// waitRun waits until the run is in one of the statuses.
func waitRun(t *testing.T, m *Manager, id string, statuses ...string) RoutineRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, err := m.store.run(id)
		if err == nil {
			for _, s := range statuses {
				if run.Status == s {
					return run
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s is %q (%s), want %v", id, run.Status, run.Error, statuses)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRoutineCommands(t *testing.T) {
	posixOnly(t)
	m, _ := fixture(t)
	dir := t.TempDir()
	save := func(v Routine) Routine {
		v.When = When{Kind: "daily", Time: "09:00"}
		v.Enabled = true
		saved, err := m.SaveRoutine(v)
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}

	// A command alone: its output and exit code are kept.
	only := save(Routine{Command: &RoutineCommand{Run: "echo checked; echo warn >&2", Dir: dir, NotifyOnOutput: true}})
	run := waitRun(t, m, m.startRun(only, time.Now(), true).ID, "done", "failed")
	if run.Status != "done" || run.Exit == nil || *run.Exit != 0 || run.Output != "checked\nwarn\n" || !run.Notify {
		t.Fatalf("command run: %+v", run)
	}

	// A failing command fails the run and sends nothing.
	failing := save(Routine{Command: &RoutineCommand{Run: "echo oops; exit 3", Dir: dir}, Target: &RoutineTarget{Kind: "session", Session: "nowhere"}, Message: "look"})
	run = waitRun(t, m, m.startRun(failing, time.Now(), true).ID, "done", "failed", "skipped")
	if run.Status != "failed" || run.Exit == nil || *run.Exit != 3 || run.Session != "" {
		t.Fatalf("failing command: %+v", run)
	}

	// A command that prints nothing hands nothing to the agent.
	quiet := save(Routine{Command: &RoutineCommand{Run: "true", Dir: dir}, Target: &RoutineTarget{Kind: "session", Session: "nowhere"}, Message: "look"})
	run = waitRun(t, m, m.startRun(quiet, time.Now(), true).ID, "done", "failed", "skipped")
	if run.Status != "skipped" || run.Session != "" {
		t.Fatalf("quiet command: %+v", run)
	}

	// A run while the last one runs is skipped.
	m.rt.mu.Lock()
	m.rt.running[only.ID] = true
	m.rt.mu.Unlock()
	if run := m.startRun(only, time.Now(), true); run.Status != "skipped" {
		t.Fatalf("overlapping run: %+v", run)
	}
}

func TestRoutineMessageEndsWithItsTurn(t *testing.T) {
	m, p := fixture(t)
	s, _ := m.Create("codex", t.TempDir(), "r")
	_ = m.Start(s.ID)
	r := m.sessions[s.ID]
	gen := r.generation
	m.receive(s.ID, gen, map[string]any{"id": "init", "result": map[string]any{}})
	m.receive(s.ID, gen, map[string]any{"id": "thread", "result": map[string]any{"thread": map[string]any{"id": "n"}}})
	v, err := m.SaveRoutine(Routine{Name: "Morning", Enabled: true, When: When{Kind: "daily", Time: "09:00"}, Target: &RoutineTarget{Kind: "session", Session: s.ID}, Message: "Check the inbox"})
	if err != nil {
		t.Fatal(err)
	}
	first := waitRun(t, m, m.startRun(v, time.Now(), true).ID, "sent")
	// While that turn runs, the next run waits in the queue.
	second := waitRun(t, m, m.startRun(v, time.Now(), true).ID, "queued")
	p.mu.Lock()
	var text string
	for _, in := range p.in {
		if in["method"] == "turn/start" {
			text = str(obj(obj(in["params"])["input"].([]any)[0])["text"])
		}
	}
	p.mu.Unlock()
	if !strings.HasPrefix(text, "[Routine: Morning]\n\nCheck the inbox") {
		t.Fatalf("sent %q", text)
	}
	m.receive(s.ID, gen, map[string]any{"method": "item/completed", "params": map[string]any{"item": map[string]any{"type": "agentMessage", "text": "Two mails need you."}}})
	m.receive(s.ID, gen, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})
	// The session took its native id with the first turn; the turn's last
	// message is the run's report.
	if run := waitRun(t, m, first.ID, "done", "failed"); run.Status != "done" || run.Session != "codex:n" || !run.Notify || !run.ForYou || run.Report != "Two mails need you." {
		t.Fatalf("first run: %+v", run)
	}
	// The queued message went out when the turn ended; its own turn fails.
	waitRun(t, m, second.ID, "sent", "queued")
	deadline := time.Now().Add(2 * time.Second)
	for {
		m.mu.Lock()
		busy := r.turn
		m.mu.Unlock()
		if busy || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.receive(s.ID, gen, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "failed"}}})
	if run := waitRun(t, m, second.ID, "done", "failed"); run.Status != "failed" {
		t.Fatalf("second run: %+v", run)
	}
}

func TestRoutineSchedulerSkipsWhatItMissed(t *testing.T) {
	m, _ := fixture(t)
	dir := t.TempDir()
	v, err := m.SaveRoutine(Routine{Enabled: true, When: When{Kind: "daily", Time: "09:00"}, Command: &RoutineCommand{Run: "true", Dir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m.rt.mu.Lock()
	m.rt.due[v.ID] = now.Add(-time.Hour)
	m.rt.mu.Unlock()
	m.dueRoutines(now)
	runs, _ := m.store.runs(v.ID, 0)
	if len(runs) != 1 || runs[0].Status != "skipped" || !strings.HasPrefix(runs[0].Error, "missed") {
		t.Fatalf("runs: %+v", runs)
	}
	m.rt.mu.Lock()
	next := m.rt.due[v.ID]
	m.rt.mu.Unlock()
	if !next.After(now) {
		t.Fatalf("not scheduled again: %v", next)
	}

	// Runs a previous process left open end as failed.
	open := RoutineRun{ID: "open", Routine: v.ID, Started: nowStamp(), Status: "sent"}
	_ = m.store.saveRun(open)
	m.loadRoutines()
	if run, _ := m.store.run("open"); run.Status != "failed" {
		t.Fatalf("open run after restart: %+v", run)
	}
}

func TestRoutineOnceTurnsItselfOff(t *testing.T) {
	posixOnly(t)
	m, _ := fixture(t)
	now := time.Now()
	v, err := m.SaveRoutine(Routine{Enabled: true, When: When{Kind: "once", At: now.Add(time.Hour).Format(time.RFC3339)}, Command: &RoutineCommand{Run: "true", Dir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	m.dueRoutines(now.Add(time.Hour + time.Second))
	saved, _ := m.store.routine(v.ID)
	if saved.Enabled || saved.Ran == "" {
		t.Fatalf("once routine after its run: %+v", saved)
	}
	m.rt.mu.Lock()
	_, scheduled := m.rt.due[v.ID]
	m.rt.mu.Unlock()
	if scheduled {
		t.Fatal("once routine scheduled again")
	}
}

func TestRoutineOutputKeepsItsEnd(t *testing.T) {
	b := &tailBuffer{max: 8}
	_, _ = b.Write([]byte("0123456789"))
	if got := b.String(); got != "…23456789" {
		t.Fatalf("got %q", got)
	}
	if got := handOff("Summarize", "a ``` b"); !strings.Contains(got, "````\na ``` b\n````") || !strings.HasPrefix(got, "Summarize\n\n") {
		t.Fatalf("hand-off %q", got)
	}
}

func TestCronRoutines(t *testing.T) {
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)
	monthly := When{Kind: "cron", Cron: "0 9 1 * *"}
	if err := monthly.check(now); err != nil {
		t.Fatal(err)
	}
	if got := monthly.next(now, time.Time{}); !got.Equal(time.Date(2026, 12, 1, 9, 0, 0, 0, time.Local)) {
		t.Fatalf("monthly next: %v", got)
	}
	if got := (When{Kind: "cron", Cron: "@weekly"}).next(now, time.Time{}); got.Weekday() != time.Sunday || got.Hour() != 0 {
		t.Fatalf("@weekly: %v", got)
	}
	for _, bad := range []string{"* * * * *", "*/3 * * * *", "61 * * * *", "nonsense", ""} {
		if err := (When{Kind: "cron", Cron: bad}).check(now); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if err := (When{Kind: "cron", Cron: "*/5 9-18 * * 1-5"}).check(now); err != nil {
		t.Fatalf("every 5 minutes in working hours: %v", err)
	}
	// In assistant.toml, and from an agent (at least every 15 minutes).
	if w, err := (FileRoutine{Cron: "0 9 1 * *"}).when(); err != nil || w.Kind != "cron" {
		t.Fatalf("file: %+v %v", w, err)
	}
	if _, err := (FileRoutine{Cron: "0 9 1 * *", Daily: "09:00"}).when(); err == nil {
		t.Fatal("two schedules accepted")
	}
	m, _ := fixture(t)
	m.as.lookup = func(id string) (Session, bool) { return Session{ID: id, Cwd: "/w"}, id == "claude:w" }
	if _, err := m.AgentRoutine(AgentRoutineRequest{Caller: "claude:w", Cron: "*/5 * * * *", Message: "x"}); err == nil {
		t.Fatal("an agent's 5-minute cron was accepted")
	}
	if v, err := m.AgentRoutine(AgentRoutineRequest{Caller: "claude:w", Cron: "0 9 1 * *", Message: "x"}); err != nil || v.When.Cron != "0 9 1 * *" {
		t.Fatalf("agent monthly: %+v %v", v, err)
	}
}

// The list carries each routine's latest runs for the heartbeat, outputs cut
// to a first-line preview; the runs themselves keep the whole output.
func TestRoutinesListTheirRecentRuns(t *testing.T) {
	m, _ := fixture(t)
	v, err := m.SaveRoutine(Routine{Enabled: true, When: When{Kind: "daily", Time: "09:00"}, Command: &RoutineCommand{Run: "true", Dir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	long := "\n  " + strings.Repeat("x", 200) + "\nsecond line"
	for i := 0; i < routineRecentShown+6; i++ {
		run := RoutineRun{ID: fmt.Sprintf("r%02d", i), Routine: v.ID, Started: base.Add(time.Duration(i) * time.Minute).UTC().Format(time.RFC3339Nano), Status: "done"}
		if i == routineRecentShown+5 {
			run.Output = long
		}
		_ = m.store.saveRun(run)
	}
	list, _ := m.Routines()
	got := list[0]
	if len(got.Recent) != routineRecentShown || got.Recent[0].ID != "r29" || got.Recent[len(got.Recent)-1].ID != "r06" {
		t.Fatalf("recent: %d runs, %+v", len(got.Recent), got.Recent[0])
	}
	if want := strings.Repeat("x", routineRecentPreview) + "…"; got.Recent[0].Output != want {
		t.Fatalf("preview: %q", got.Recent[0].Output)
	}
	if got.Last == nil || got.Last.ID != "r29" || got.Last.Output != "" {
		t.Fatalf("last: %+v", got.Last)
	}
	if runs, _ := m.RoutineRuns(v.ID); runs[0].Output != long {
		t.Fatalf("the runs lost the output: %q", runs[0].Output)
	}
	// Not kept with the routine.
	saved, _ := m.store.routine(v.ID)
	if saved.Recent != nil || saved.Last != nil {
		t.Fatalf("saved computed fields: %+v", saved)
	}
}
