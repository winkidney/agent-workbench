package workbench

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// An assistant's routine work takes turns (ADR 0027). A routine run that
// hands work to an assistant (a session or a background run) holds that
// assistant's lane from when it is sent until it ends; a memory update
// holds it while it runs. Work that arrives meanwhile is held, and when the
// lane frees the oldest held run goes next, with every other held run of
// the same routine merged into it, so three checks that piled up become one
// conversation. Sessions the user starts take no part: they neither wait
// nor make routines wait.
//
// Who holds a lane is read from the runs in the store, so held runs outlast
// a restart; memory updates, which are not runs, are counted here.
type lanes struct {
	mu sync.Mutex
	// Signalled whenever a lane may have freed (dispatchLane).
	cond *sync.Cond
	// Memory updates running now, by assistant folder.
	ingests map[string]int
}

func newLanes() *lanes {
	l := &lanes{ingests: map[string]int{}}
	l.cond = sync.NewCond(&l.mu)
	return l
}

// laneQuiet is how long a sent run may go without its session knowing it
// before the lane stops waiting for it: a session that was deleted, or an
// engine that exited without reporting the turn's end.
const laneQuiet = 2 * time.Minute

// --- store ---

// laneRuns lists the runs of an assistant's lane in these statuses, oldest
// first.
func (s *Store) laneRuns(assistant string, statuses ...string) ([]RoutineRun, error) {
	q := `SELECT data FROM routine_runs WHERE json_extract(data,'$.assistant')=? AND json_extract(data,'$.status') IN (` +
		strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") + `) ORDER BY started`
	args := []any{assistant}
	for _, st := range statuses {
		args = append(args, st)
	}
	return s.queryRuns(q, args...)
}

// heldRuns lists a routine's held runs, oldest first.
func (s *Store) heldRuns(routine string) ([]RoutineRun, error) {
	return s.queryRuns(`SELECT data FROM routine_runs WHERE routine=? AND json_extract(data,'$.status')='held' ORDER BY started`, routine)
}

// heldLanes lists the assistants with held runs.
func (s *Store) heldLanes() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT json_extract(data,'$.assistant') FROM routine_runs WHERE json_extract(data,'$.status')='held'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a *string
		if rows.Scan(&a) == nil && a != nil && *a != "" {
			out = append(out, *a)
		}
	}
	return out, rows.Err()
}

func (s *Store) queryRuns(q string, args ...any) ([]RoutineRun, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoutineRun{}
	for rows.Next() {
		var data string
		if rows.Scan(&data) != nil {
			continue
		}
		var v RoutineRun
		if json.Unmarshal([]byte(data), &v) == nil {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

// --- holding and freeing ---

// laneHolder is what holds the assistant's lane, other than the run except:
// a run sent and not ended, or a memory update (m.ln.mu held).
func (m *Manager) laneHolder(assistant, except string) (RoutineRun, bool) {
	open, _ := m.store.laneRuns(assistant, "sent", "queued")
	for _, r := range open {
		if r.ID != except {
			return r, true
		}
	}
	if m.ln.ingests[assistant] > 0 {
		return RoutineRun{Name: "Memory update"}, true
	}
	return RoutineRun{}, false
}

// takeLane lets run (whose Assistant is set) through when its assistant's
// lane is free and no older run waits for it, recording it as sent;
// otherwise it records the run as held and reports false.
func (m *Manager) takeLane(run *RoutineRun) bool {
	m.ln.mu.Lock()
	defer m.ln.mu.Unlock()
	_, busy := m.laneHolder(run.Assistant, run.ID)
	if !busy {
		held, _ := m.store.laneRuns(run.Assistant, "held")
		for _, h := range held {
			if h.ID != run.ID && h.Started < run.Started {
				busy = true
				break
			}
		}
	}
	if busy {
		run.Status, run.Notify = "held", false
		m.recordRun(*run)
		return false
	}
	run.Status, run.Sent = "sent", nowStamp()
	m.recordRun(*run)
	return true
}

// dispatchLane sends the next held run of the assistant when its lane is
// free, with the other held runs of its routine merged into it, and wakes
// what waits for a lane.
func (m *Manager) dispatchLane(assistant string) {
	m.ln.mu.Lock()
	defer m.ln.mu.Unlock()
	defer m.ln.cond.Broadcast()
	if _, busy := m.laneHolder(assistant, ""); busy {
		return
	}
	held, _ := m.store.laneRuns(assistant, "held")
	if len(held) == 0 {
		return
	}
	first := held[0]
	var same []RoutineRun
	for _, h := range held[1:] {
		if h.Routine == first.Routine {
			same = append(same, h)
		}
	}
	first = m.mergeRuns(first, same)
	first.Status, first.Sent = "sent", nowStamp()
	m.recordRun(first)
	go m.deliver(first)
}

// laneFreed is called when a run of the assistant's lane ended.
func (m *Manager) laneFreed(assistant string) {
	if assistant != "" {
		go m.dispatchLane(assistant)
	}
}

// holdLane waits until the assistant's lane is free and holds it for a
// memory update until the returned function is called. Ingests are not
// runs: one waiting here is lost if the workbench restarts, and the next
// topic's ingest takes up what it would have read.
func (m *Manager) holdLane(assistant string) func() {
	m.ln.mu.Lock()
	for {
		if _, busy := m.laneHolder(assistant, ""); !busy {
			break
		}
		m.ln.cond.Wait()
	}
	m.ln.ingests[assistant]++
	m.ln.mu.Unlock()
	return func() {
		m.ln.mu.Lock()
		m.ln.ingests[assistant]--
		m.ln.mu.Unlock()
		m.dispatchLane(assistant)
	}
}

// deliver sends a run the lane let through, as carryOut would have.
func (m *Manager) deliver(run RoutineRun) {
	v, err := m.store.routine(run.Routine)
	if err != nil {
		m.endRun(run, "failed", "the routine was removed", true)
		return
	}
	if name, off := m.assistantOff(v); off {
		m.endRun(run, "skipped", name+" is turned off", false)
		return
	}
	if v.Target == nil {
		m.endRun(run, "failed", "the routine no longer sends a message", true)
		return
	}
	message := v.Message
	if v.Command != nil && strings.TrimSpace(run.Output) != "" {
		message = handOff(v.Message, run.Output)
	}
	m.sendRun(v, run, message)
}

// mergeRuns makes first carry the outputs of the others, which end as
// merged into it (recorded here). Each run's output keeps its own share of
// the hand-off, so the oldest is not cut away by the newest.
func (m *Manager) mergeRuns(first RoutineRun, others []RoutineRun) RoutineRun {
	if len(others) == 0 {
		return first
	}
	all := append([]RoutineRun{first}, others...)
	first.Output = joinOutputs(all)
	for _, o := range others {
		first.Merged += 1 + o.Merged
		o.Status, o.MergedInto, o.Finished, o.Notify = "merged", first.ID, nowStamp(), false
		o.Output = ""
		m.recordRun(o)
	}
	return first
}

// joinOutputs puts the outputs of runs of one routine together, oldest
// first, each under the time it ran and cut to its share of the hand-off.
func joinOutputs(runs []RoutineRun) string {
	count := 0
	for _, r := range runs {
		count += 1 + r.Merged
	}
	// Room for each part's heading, the "…" of a cut and the blank line.
	share := routineHandOffMax/count - 48
	var parts []string
	for _, r := range runs {
		out := strings.TrimSpace(r.Output)
		max := share * (1 + r.Merged)
		if len(out) > max {
			out = "…" + validUTF8Tail(out, max)
		}
		if r.Merged > 0 {
			// Already a merge: its parts carry their own times.
			parts = append(parts, out)
			continue
		}
		when := r.Started
		if t, err := time.Parse(time.RFC3339Nano, r.Started); err == nil {
			when = t.Local().Format("2006-01-02 15:04")
		}
		parts = append(parts, "--- run of "+when+" ---\n"+out)
	}
	return strings.Join(parts, "\n\n")
}

// tickLanes sends what waits for a free lane, and stops waiting for a sent
// run whose session no longer knows it (deleted, or its engine exited
// without reporting the turn's end): run by the scheduler, so held runs go
// out after a restart too.
func (m *Manager) tickLanes(now time.Time) {
	open, _ := m.store.openRuns()
	for _, run := range open {
		if run.Assistant == "" || run.Session == "" || run.Sent == "" {
			continue
		}
		sent, err := time.Parse(time.RFC3339Nano, run.Sent)
		if err != nil || now.Sub(sent) < laneQuiet || m.sessionHasRun(run.Session, run.ID) {
			continue
		}
		m.endRun(run, "done", "", false)
	}
	lanes, _ := m.store.heldLanes()
	for _, a := range lanes {
		m.dispatchLane(a)
	}
}

// sessionHasRun reports whether the session still works on the run: its
// running turn came from it, or its message waits in the session's queue
// (also a queue paused after a failed turn, which the user resumes).
func (m *Manager) sessionHasRun(session, run string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(session)
	if r == nil {
		return false
	}
	if r.routineRun == run {
		return true
	}
	for _, q := range r.queue {
		if q.Routine == run {
			return true
		}
	}
	return false
}

// --- the page's actions ---

// ApproveWaiting answers every run of the routine that waits for approval
// at once: approved, they become one hand-off (the oldest run, with the
// others merged into it), so piled-up checks reach the agent together;
// dismissed, nothing is sent.
func (m *Manager) ApproveWaiting(routine string, approve bool) (RoutineRun, error) {
	v, err := m.store.routine(routine)
	if err != nil {
		return RoutineRun{}, err
	}
	if approve {
		if name, off := m.assistantOff(v); off {
			return RoutineRun{}, errors.New(name + " is turned off")
		}
	}
	m.rt.mu.Lock()
	waiting, err := m.store.waitingRuns(routine)
	if err != nil || len(waiting) == 0 {
		m.rt.mu.Unlock()
		return RoutineRun{}, errors.New("no run waits for approval")
	}
	var changed []RoutineRun
	first := waiting[0]
	if !approve {
		for _, w := range waiting {
			w.Status, w.Error, w.Finished, w.Notify = "skipped", "dismissed: nothing was sent", nowStamp(), false
			changed = append(changed, w)
		}
	} else {
		all := waiting
		if len(all) > 1 {
			first.Output = joinOutputs(all)
			for _, o := range all[1:] {
				first.Merged += 1 + o.Merged
				o.Status, o.MergedInto, o.Finished, o.Notify, o.Output = "merged", first.ID, nowStamp(), false, ""
				changed = append(changed, o)
			}
		}
		first.Status, first.Notify = "sent", false
		changed = append(changed, first)
	}
	for _, c := range changed {
		if err := m.store.saveRun(c); err != nil {
			m.rt.mu.Unlock()
			return first, err
		}
	}
	m.rt.mu.Unlock()
	for i := range changed {
		m.routineChanged(routine, &changed[i])
	}
	if !approve {
		return changed[0], nil
	}
	if v.Target == nil {
		m.endRun(first, "failed", "the routine no longer sends a message", true)
		return first, nil
	}
	go m.sendRun(v, first, handOff(v.Message, first.Output))
	return first, nil
}

// StartHeld sends a held run now, without waiting for its assistant's lane:
// the user chose to let it run beside what holds it.
func (m *Manager) StartHeld(routine, id string) (RoutineRun, error) {
	m.ln.mu.Lock()
	run, err := m.store.run(id)
	if err != nil || run.Routine != routine {
		m.ln.mu.Unlock()
		return RoutineRun{}, errors.New("run not found")
	}
	if run.Status != "held" {
		m.ln.mu.Unlock()
		return run, errors.New("this run is not waiting for its turn")
	}
	run.Status, run.Sent = "sent", nowStamp()
	m.recordRun(run)
	m.ln.mu.Unlock()
	go m.deliver(run)
	return run, nil
}

// dismissHeld ends a held run without sending it.
func (m *Manager) dismissHeld(run RoutineRun) (RoutineRun, error) {
	m.ln.mu.Lock()
	cur, err := m.store.run(run.ID)
	if err != nil || cur.Status != "held" {
		m.ln.mu.Unlock()
		return cur, errors.New("this run is not waiting for its turn")
	}
	cur.Status, cur.Error, cur.Finished, cur.Notify = "skipped", "dismissed: nothing was sent", nowStamp(), false
	m.recordRun(cur)
	m.ln.mu.Unlock()
	m.laneFreed(cur.Assistant)
	return cur, nil
}
