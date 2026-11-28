package workbench

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// laneAssistant writes a per-task assistant with two routines whose
// messages go to sessions: a command one (its output handed on) and a
// message one, as the mail assistant's Check mail and Daily summary.
func laneAssistant(t *testing.T, m *Manager, root, extra string) {
	t.Helper()
	writeToml(t, filepath.Join(root, "mail", "assistant.toml"), `name = "Mail"
agent = "claude"
sessions = "per-task"
[[routines]]
name = "Check mail"
every = "30m"
command = "echo checked"
message = "Sort the new mail."
`+extra+`
[[routines]]
name = "Daily summary"
daily = "10:00"
message = "Summarize yesterday."
`)
	m.assistantsChanged()
	for _, a := range m.Assistants() {
		if a.Dir == "mail" && a.Error != "" {
			t.Fatalf("mail: %s", a.Error)
		}
	}
}

// sessions fakes the sessions routines start: each message gets a session
// of its own, recorded with the run that sent it.
type sessions struct {
	mu   sync.Mutex
	sent []struct{ text, run, session string }
}

func (s *sessions) send(dir, text string, _ []string, run string) (Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("s%d", len(s.sent)+1)
	s.sent = append(s.sent, struct{ text, run, session string }{text, run, id})
	return Session{ID: id}, false, nil
}

func (s *sessions) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

func (s *sessions) waitFor(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.count() < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d sessions started, want %d", s.count(), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runNow(t *testing.T, m *Manager, id string, statuses ...string) RoutineRun {
	t.Helper()
	run, err := m.RunRoutine(id)
	if err != nil {
		t.Fatal(err)
	}
	return waitRun(t, m, run.ID, statuses...)
}

func TestApprovingWaitingRunsSendsThemTogether(t *testing.T) {
	m, root := assistantsWithGit(t)
	laneAssistant(t, m, root, "approve = true\n")
	fake := &sessions{}
	m.as.foreground = fake.send
	id := fileRoutineID("mail", "Check mail")
	var runs []RoutineRun
	for i := 0; i < 3; i++ {
		runs = append(runs, runNow(t, m, id, "waiting"))
	}
	first, err := m.ApproveWaiting(id, true)
	if err != nil || first.ID != runs[0].ID {
		t.Fatalf("approve: %+v %v", first, err)
	}
	fake.waitFor(t, 1)
	sent := waitRun(t, m, first.ID, "sent")
	if sent.Merged != 2 || sent.Session != "s1" || sent.Assistant != "mail" {
		t.Fatalf("sent: %+v", sent)
	}
	// One conversation, with every check's output in it.
	text := fake.sent[0].text
	if strings.Count(text, "--- run of ") != 3 || strings.Count(text, "checked") != 3 || !strings.Contains(text, "Sort the new mail.") {
		t.Fatalf("hand-off: %q", text)
	}
	for _, r := range runs[1:] {
		if v, _ := m.store.run(r.ID); v.Status != "merged" || v.MergedInto != first.ID || v.Notify || v.Finished == "" {
			t.Fatalf("merged run: %+v", v)
		}
	}
	if _, err := m.ApproveWaiting(id, true); err == nil {
		t.Fatal("approved again with nothing waiting")
	}
	time.Sleep(50 * time.Millisecond)
	if fake.count() != 1 {
		t.Fatalf("%d sessions", fake.count())
	}

	// Dismissed together: nothing is sent.
	m.routineTurnEnded(first.ID, "s1", false, "", "")
	a, b := runNow(t, m, id, "waiting"), runNow(t, m, id, "waiting")
	if _, err := m.ApproveWaiting(id, false); err != nil {
		t.Fatal(err)
	}
	for _, r := range []RoutineRun{a, b} {
		if v, _ := m.store.run(r.ID); v.Status != "skipped" {
			t.Fatalf("dismissed run: %+v", v)
		}
	}
	if fake.count() != 1 {
		t.Fatalf("%d sessions", fake.count())
	}
}

func TestAnAssistantsRoutineWorkTakesTurns(t *testing.T) {
	m, root := assistantsWithGit(t)
	laneAssistant(t, m, root, "")
	fake := &sessions{}
	m.as.foreground = fake.send
	check, daily := fileRoutineID("mail", "Check mail"), fileRoutineID("mail", "Daily summary")

	first := runNow(t, m, check, "sent")
	fake.waitFor(t, 1)
	// While its session works, the next routine work of the assistant waits.
	summary := runNow(t, m, daily, "held", "sent")
	second := runNow(t, m, check, "held", "sent")
	third := runNow(t, m, check, "held", "sent")
	if summary.Status != "held" || second.Status != "held" || third.Status != "held" {
		t.Fatalf("not held: %s %s %s", summary.Status, second.Status, third.Status)
	}
	if fake.count() != 1 {
		t.Fatalf("%d sessions", fake.count())
	}
	list, _ := m.Routines()
	for _, r := range list {
		if r.ID == check && (len(r.Held) != 2 || r.Held[0].Behind != "Check mail" || r.Held[0].BehindSession != "s1") {
			t.Fatalf("held on the page: %+v", r.Held)
		}
	}
	// Another assistant's routine does not wait for this one.
	writeToml(t, filepath.Join(root, "notes", "assistant.toml"), "name = \"Notes\"\nagent = \"claude\"\n[[routines]]\nname = \"Tidy\"\ndaily = \"09:00\"\nmessage = \"Tidy up.\"\n")
	m.assistantsChanged()
	m.Assistants()
	runNow(t, m, fileRoutineID("notes", "Tidy"), "sent")
	fake.waitFor(t, 2)

	// The turn ends: the oldest held run goes next, alone (another routine).
	m.routineTurnEnded(first.ID, "s1", false, "", "")
	fake.waitFor(t, 3)
	if fake.sent[2].run != summary.ID || !strings.Contains(fake.sent[2].text, "Summarize yesterday.") {
		t.Fatalf("next: %+v", fake.sent[2])
	}
	time.Sleep(50 * time.Millisecond)
	if fake.count() != 3 {
		t.Fatalf("%d sessions", fake.count())
	}
	// Then the two checks, as one.
	m.routineTurnEnded(summary.ID, fake.sent[2].session, false, "", "")
	fake.waitFor(t, 4)
	if fake.sent[3].run != second.ID || strings.Count(fake.sent[3].text, "--- run of ") != 2 {
		t.Fatalf("merged checks: %q", fake.sent[3].text)
	}
	if v, _ := m.store.run(third.ID); v.Status != "merged" || v.MergedInto != second.ID {
		t.Fatalf("third: %+v", v)
	}
}

func TestHeldRunsOutlastARestartAndAQuietSession(t *testing.T) {
	m, root := assistantsWithGit(t)
	laneAssistant(t, m, root, "")
	fake := &sessions{}
	m.as.foreground = fake.send
	check, daily := fileRoutineID("mail", "Check mail"), fileRoutineID("mail", "Daily summary")
	first := runNow(t, m, check, "sent")
	fake.waitFor(t, 1)
	held := runNow(t, m, daily, "held")

	// A run that waits for its turn is not pruned like other runs.
	for i := 0; i < routineRunsKept+5; i++ {
		started := time.Now().Add(time.Duration(i+1) * time.Second).UTC().Format(time.RFC3339Nano)
		_ = m.store.saveRun(RoutineRun{ID: token()[:16], Routine: daily, Started: started, Status: "skipped"})
	}
	if err := m.store.pruneRuns(time.Now()); err != nil {
		t.Fatal(err)
	}

	// While the session still works on the run (its message waits in the
	// queue, even a paused one), the lane stays held.
	m.mu.Lock()
	m.sessions["s1"] = &runtime{session: Session{ID: "s1"}, queue: []queued{{ID: "q", Routine: first.ID}}, queuePaused: true}
	m.mu.Unlock()
	m.tickLanes(time.Now().Add(laneQuiet + time.Minute))
	if v, _ := m.store.run(held.ID); v.Status != "held" {
		t.Fatalf("let through beside a paused queue: %+v", v)
	}

	// A restart ends the open run; the held one is still there and goes on
	// the scheduler's next tick.
	m.mu.Lock()
	delete(m.sessions, "s1")
	m.mu.Unlock()
	m.loadRoutines()
	if v, _ := m.store.run(held.ID); v.Status != "held" {
		t.Fatalf("after a restart: %+v", v)
	}
	m.tickLanes(time.Now())
	fake.waitFor(t, 2)
	next := waitRun(t, m, held.ID, "sent")

	// Its session goes away without reporting the turn's end: after a
	// while the lane stops waiting for it.
	another := runNow(t, m, check, "held")
	m.tickLanes(time.Now())
	if v, _ := m.store.run(another.ID); v.Status != "held" {
		t.Fatalf("let through too early: %+v", v)
	}
	m.tickLanes(time.Now().Add(laneQuiet + time.Minute))
	if v := waitRun(t, m, next.ID, "done"); v.Notify {
		t.Fatalf("quiet run: %+v", v)
	}
	fake.waitFor(t, 3)
	waitRun(t, m, another.ID, "sent")
}

func TestHeldRunsCanStartNowOrBeDismissed(t *testing.T) {
	m, root := assistantsWithGit(t)
	laneAssistant(t, m, root, "")
	fake := &sessions{}
	m.as.foreground = fake.send
	check, daily := fileRoutineID("mail", "Check mail"), fileRoutineID("mail", "Daily summary")
	runNow(t, m, check, "sent")
	fake.waitFor(t, 1)
	held := runNow(t, m, daily, "held")
	// Start now: it runs beside what holds the lane.
	if _, err := m.StartHeld(daily, held.ID); err != nil {
		t.Fatal(err)
	}
	fake.waitFor(t, 2)
	if _, err := m.StartHeld(daily, held.ID); err == nil {
		t.Fatal("started twice")
	}
	// Dismissed: nothing is sent.
	other := runNow(t, m, check, "held")
	if v, err := m.ApproveRun(check, other.ID, false); err != nil || v.Status != "skipped" {
		t.Fatalf("dismiss: %+v %v", v, err)
	}
	if _, err := m.ApproveRun(check, other.ID, true); err == nil {
		t.Fatal("approved a dismissed run")
	}
	time.Sleep(50 * time.Millisecond)
	if fake.count() != 2 {
		t.Fatalf("%d sessions", fake.count())
	}
}

func TestAMemoryUpdateWaitsForTheLane(t *testing.T) {
	m, root := assistantsWithGit(t)
	laneAssistant(t, m, root, "")
	fake := &sessions{}
	m.as.foreground = fake.send
	first := runNow(t, m, fileRoutineID("mail", "Check mail"), "sent")
	held := make(chan func())
	go func() { held <- m.holdLane("mail") }()
	select {
	case <-held:
		t.Fatal("a memory update ran beside a routine's session")
	case <-time.After(100 * time.Millisecond):
	}
	m.routineTurnEnded(first.ID, "s1", false, "", "")
	var release func()
	select {
	case release = <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("the memory update never ran")
	}
	// While it runs, routine work waits; after it, the work goes.
	waiting := runNow(t, m, fileRoutineID("mail", "Daily summary"), "held")
	release()
	waitRun(t, m, waiting.ID, "sent")
}

func TestJoinOutputsKeepsEachRunsShare(t *testing.T) {
	big := strings.Repeat("x", routineHandOffMax)
	runs := []RoutineRun{
		{Started: "2026-11-27T01:00:00Z", Output: "first " + big},
		{Started: "2026-11-27T01:30:00Z", Output: "second"},
		{Started: "2026-11-27T02:00:00Z", Output: big + " third"},
	}
	out := joinOutputs(runs)
	if len(out) > routineHandOffMax || !strings.Contains(out, "second") || !strings.HasSuffix(out, " third") || strings.Count(out, "--- run of ") != 3 {
		t.Fatalf("joined (%d bytes): %.200q…", len(out), out)
	}
	// Merging a merge keeps its parts as they were.
	again := joinOutputs([]RoutineRun{{Output: out, Merged: 2}, {Started: "2026-11-27T03:00:00Z", Output: "fourth"}})
	if len(again) > routineHandOffMax || !strings.HasSuffix(again, "--- run of 2026-11-27 "+time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC).Local().Format("15:04")+" ---\nfourth") {
		t.Fatalf("joined again: %q", again)
	}
}
