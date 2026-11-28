package workbench

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/robfig/cron/v3"
)

// Routines are messages and commands the workbench runs on a schedule
// (ADR 0009). A routine sends a message to a session, runs a command, or
// runs a command and hands what it printed to a session. They are not the
// agents' own timers (schedule.go), which live inside an engine process.

// When is a routine's schedule, in the workbench computer's local time.
type When struct {
	// once | daily | interval | cron
	Kind string `json:"kind"`
	// cron: a standard 5-field expression ("0 9 1 * *") or a descriptor
	// ("@weekly"), in this computer's local time.
	Cron string `json:"cron,omitempty"`
	// once: the time (RFC 3339).
	At string `json:"at,omitempty"`
	// daily: "HH:MM", on Days (0 = Sunday … 6 = Saturday; none = every day).
	Time string `json:"time,omitempty"`
	Days []int  `json:"days,omitempty"`
	// interval: every so many minutes, counted from the routine's Anchor.
	Minutes int `json:"minutes,omitempty"`
}

// RoutineTarget is where a routine's message goes: an existing session, or
// a new session started in a folder.
type RoutineTarget struct {
	// session | new | assistant
	Kind    string `json:"kind"`
	Session string `json:"session,omitempty"`
	// assistant: its folder name (assistant.go).
	Assistant string `json:"assistant,omitempty"`
	// assistant: hand it to a background run (ADR 0011), not a session.
	Background bool     `json:"background,omitempty"`
	Agent      string   `json:"agent,omitempty"`
	Cwd        string   `json:"cwd,omitempty"`
	Options    *Options `json:"options,omitempty"`
}

// RoutineCommand is a command line a routine runs in a folder.
type RoutineCommand struct {
	Run            string `json:"run"`
	Dir            string `json:"dir"`
	TimeoutMin     int    `json:"timeoutMin,omitempty"`
	NotifyOnOutput bool   `json:"notifyOnOutput,omitempty"`
}

type Routine struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Message string          `json:"message,omitempty"`
	Enabled bool            `json:"enabled"`
	When    When            `json:"when"`
	Target  *RoutineTarget  `json:"target,omitempty"`
	Command *RoutineCommand `json:"command,omitempty"`
	// page, or agent:<session> (a routine an agent made).
	CreatedBy string `json:"createdBy"`
	Created   string `json:"created"`
	// Where interval counts start: when the schedule was last set.
	Anchor string `json:"anchor,omitempty"`
	// A once routine: when it ran (it is then off, and removed a week later).
	Ran string `json:"ran,omitempty"`
	// A routine from an assistant.toml: the `enabled` its file last said.
	FileEnabled *bool `json:"fileEnabled,omitempty"`
	// What its command printed waits for the user's approval before the
	// message takes it to an agent (ADR 0021).
	Approve bool `json:"approve,omitempty"`
	// A file the page offers to open while a run waits: what the command
	// wrote for the user to look at first, shown without an agent.
	Preview string `json:"preview,omitempty"`

	// Computed for the page.
	Next string `json:"next,omitempty"`
	// Its assistant is turned off: it does not run (computed, not kept).
	AssistantOff bool        `json:"assistantOff,omitempty"`
	Last         *RoutineRun `json:"last,omitempty"`
	// Its latest runs, newest first, for the page's heartbeat: each output cut
	// to its first line (routineRecentPreview), the whole of it in the runs.
	Recent []RoutineRun `json:"recent,omitempty"`
	// Its runs that wait for the user's approval, oldest first (outputs cut
	// as in Recent).
	Waiting []RoutineRun `json:"waiting,omitempty"`
	// Its runs that wait for their assistant's lane (lane.go), oldest first,
	// each with what it waits for.
	Held []RoutineRun `json:"held,omitempty"`
	// Its newest run whose report is for the user and not yet seen.
	Report *RoutineRun `json:"report,omitempty"`
}

// RoutineRun is one time a routine was due.
type RoutineRun struct {
	ID       string `json:"id"`
	Routine  string `json:"routine"`
	Name     string `json:"name"`
	Due      string `json:"due"`
	Started  string `json:"started"`
	Finished string `json:"finished,omitempty"`
	// running (its command) | waiting (its output waits for the user's
	// approval) | held (it waits for its assistant's lane, lane.go) | sent |
	// queued (the message waits for the session) | done | failed | skipped |
	// merged (its output went out with a later hand-off, MergedInto)
	Status string `json:"status"`
	// The assistant whose lane the run went through, and when it was let
	// through (lane.go).
	Assistant string `json:"assistant,omitempty"`
	Sent      string `json:"sent,omitempty"`
	// merged: the run that carried its output; on that run, how many runs
	// were merged into it.
	MergedInto string `json:"mergedInto,omitempty"`
	Merged     int    `json:"merged,omitempty"`
	// held: what holds the lane now (computed for the page).
	Behind        string `json:"behind,omitempty"`
	BehindSession string `json:"behindSession,omitempty"`
	// The session the message went to.
	Session string `json:"session,omitempty"`
	Exit    *int   `json:"exit,omitempty"`
	// The command's output (stdout and stderr), its last routineOutputMax bytes.
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
	// Whether the page should notify about this run's end.
	Notify bool `json:"notify,omitempty"`
	// A message run: the agent's last reply in the turn it started, up to
	// routineReportMax bytes, without the quiet mark. ForYou: the reply did
	// not say it has nothing for the user, so the page shows it to them
	// until Seen (when they opened its session or dismissed it).
	Report string `json:"report,omitempty"`
	ForYou bool   `json:"forYou,omitempty"`
	Seen   string `json:"seen,omitempty"`
}

const (
	routineOutputMax  = 16 << 10
	routineHandOffMax = 4 << 10
	routineReportMax  = 8 << 10
	routineTimeoutMin = 10
	routineTimeoutMax = 60
	routineMinMinutes = 5
	// A routine due longer ago than this (the workbench was not running, or
	// the computer slept) is skipped, not run late.
	routineLate = 5 * time.Minute
	// Kept runs: a month, and at most this many per routine.
	routineRunsKept   = 50
	routineRunsMaxAge = 30 * 24 * time.Hour
	// The runs the list shows with each routine, and how much of each output.
	routineRecentShown   = 24
	routineRecentPreview = 120
	// A sent message whose turn never reported its end.
	routineRunStale = 24 * time.Hour
	// A once routine that ran is removed after this.
	routineOnceKept = 7 * 24 * time.Hour
)

// routines is the scheduler's state; the definitions and runs are in the
// store.
type routines struct {
	mu sync.Mutex
	// When each enabled routine is next due.
	due map[string]time.Time
	// Routines whose command is running now.
	running map[string]bool
	// Wakes the scheduler after a change.
	wake chan struct{}
	// The clock; replaced in tests.
	now func() time.Time
}

func newRoutines() *routines {
	return &routines{due: map[string]time.Time{}, running: map[string]bool{}, wake: make(chan struct{}, 1), now: time.Now}
}

func (rs *routines) poke() {
	select {
	case rs.wake <- struct{}{}:
	default:
	}
}

// next is when w is next due strictly after after; zero when never.
func (w When) next(after, anchor time.Time) time.Time {
	switch w.Kind {
	case "once":
		at, err := time.Parse(time.RFC3339, w.At)
		if err != nil || !at.After(after) {
			return time.Time{}
		}
		return at
	case "daily":
		hour, minute, ok := clockTime(w.Time)
		if !ok {
			return time.Time{}
		}
		local := after.In(time.Local)
		for i := 0; i <= 7; i++ {
			day := local.AddDate(0, 0, i)
			at := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, time.Local)
			if at.After(after) && onDay(w.Days, at.Weekday()) {
				return at
			}
		}
		return time.Time{}
	case "interval":
		if w.Minutes <= 0 {
			return time.Time{}
		}
		step := time.Duration(w.Minutes) * time.Minute
		if anchor.IsZero() || anchor.After(after) {
			if anchor.After(after) {
				return anchor
			}
			anchor = after
		}
		n := after.Sub(anchor)/step + 1
		return anchor.Add(n * step)
	case "cron":
		s, err := cron.ParseStandard(w.Cron)
		if err != nil {
			return time.Time{}
		}
		return s.Next(after.In(time.Local))
	}
	return time.Time{}
}

// cronGap is the shortest time between two runs of a cron expression over
// the next week, to hold it to the same minimum as an interval.
func cronGap(expr string, now time.Time) (time.Duration, error) {
	s, err := cron.ParseStandard(expr)
	if err != nil {
		return 0, fmt.Errorf("cron: %v (like \"0 9 1 * *\" or \"@weekly\")", err)
	}
	t := s.Next(now.In(time.Local))
	if t.IsZero() {
		return 0, errors.New("cron: it never runs")
	}
	gap := time.Duration(1<<62 - 1)
	for i := 0; i < 200 && t.Before(now.Add(8*24*time.Hour)); i++ {
		n := s.Next(t)
		if n.IsZero() {
			break
		}
		if d := n.Sub(t); d < gap {
			gap = d
		}
		t = n
	}
	return gap, nil
}

func clockTime(s string) (hour, minute int, ok bool) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, false
	}
	return t.Hour(), t.Minute(), true
}

func onDay(days []int, d time.Weekday) bool {
	if len(days) == 0 {
		return true
	}
	for _, x := range days {
		if x == int(d) {
			return true
		}
	}
	return false
}

func (w When) check(now time.Time) error {
	switch w.Kind {
	case "once":
		at, err := time.Parse(time.RFC3339, w.At)
		if err != nil {
			return errors.New("once: a time is needed")
		}
		if !at.After(now.Add(-time.Minute)) {
			return errors.New("once: the time has passed")
		}
	case "daily":
		if _, _, ok := clockTime(w.Time); !ok {
			return errors.New("daily: a time like 09:30 is needed")
		}
		for _, d := range w.Days {
			if d < 0 || d > 6 {
				return errors.New("daily: days are 0 (Sunday) to 6")
			}
		}
	case "interval":
		if w.Minutes < routineMinMinutes || w.Minutes > 7*24*60 {
			return fmt.Errorf("interval: every %d minutes to 7 days", routineMinMinutes)
		}
	case "cron":
		gap, err := cronGap(w.Cron, now)
		if err != nil {
			return err
		}
		if gap < routineMinMinutes*time.Minute {
			return fmt.Errorf("cron: at most every %d minutes", routineMinMinutes)
		}
	default:
		return errors.New("when: once, daily, interval or cron")
	}
	return nil
}

// checkRoutine validates what the page sent and fills in defaults.
func (m *Manager) checkRoutine(v *Routine, now time.Time) error {
	if err := v.When.check(now); err != nil {
		return err
	}
	if v.Target == nil && v.Command == nil {
		return errors.New("a routine sends a message, runs a command, or both")
	}
	if len(v.Message) > 64<<10 {
		return errors.New("the message is too long")
	}
	if t := v.Target; t != nil {
		switch t.Kind {
		case "session":
			if strings.TrimSpace(t.Session) == "" {
				return errors.New("choose a session")
			}
			t.Agent, t.Cwd, t.Options = "", "", nil
		case "assistant":
			if err := m.assistantRoutineTarget(t.Assistant); err != nil {
				return err
			}
			t.Session, t.Agent, t.Cwd, t.Options = "", "", "", nil
		case "new":
			if t.Agent != "codex" && t.Agent != "claude" {
				return errors.New("agent must be codex or claude")
			}
			dir, err := existingDir(t.Cwd)
			if err != nil {
				return err
			}
			t.Cwd, t.Session = dir, ""
		default:
			return errors.New("target: session, assistant or new")
		}
		if v.Command == nil && strings.TrimSpace(v.Message) == "" {
			return errors.New("write the message to send")
		}
	} else {
		v.Message = ""
	}
	if c := v.Command; c != nil {
		c.Run = strings.TrimSpace(c.Run)
		if c.Run == "" || len(c.Run) > 4096 {
			return errors.New("write the command to run (at most 4096 bytes)")
		}
		dir, err := existingDir(c.Dir)
		if err != nil {
			return err
		}
		c.Dir = dir
		if c.TimeoutMin == 0 {
			c.TimeoutMin = routineTimeoutMin
		}
		if c.TimeoutMin < 1 || c.TimeoutMin > routineTimeoutMax {
			return fmt.Errorf("timeout: 1 to %d minutes", routineTimeoutMax)
		}
	}
	if v.Approve && (v.Command == nil || v.Target == nil) {
		return errors.New("approval needs a command and a message to send what it printed")
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		v.Name = routineName(v)
	}
	if len([]rune(v.Name)) > 100 {
		v.Name = string([]rune(v.Name)[:100])
	}
	return nil
}

func existingDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("choose a folder")
	}
	abs, err := canonicalDir(path)
	if err != nil {
		return "", errors.New("the folder does not exist")
	}
	return abs, nil
}

func canonicalDir(path string) (string, error) {
	abs, err := filepath.Abs(expandHome(path))
	if err != nil {
		return "", err
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", errors.New("not a folder")
	}
	return abs, nil
}

// routineName names a routine by its message or command.
func routineName(v *Routine) string {
	if t := sessionTitle(v.Message); t != "" && v.Target != nil {
		return t
	}
	if v.Command != nil {
		return sessionTitle(v.Command.Run)
	}
	return "Routine"
}

// --- store ---

func (s *Store) routines() ([]Routine, error) {
	rows, err := s.db.Query(`SELECT data FROM routines`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Routine{}
	for rows.Next() {
		var data string
		if rows.Scan(&data) != nil {
			continue
		}
		var v Routine
		if json.Unmarshal([]byte(data), &v) == nil {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

func (s *Store) routine(id string) (Routine, error) {
	var data string
	var v Routine
	if err := s.db.QueryRow(`SELECT data FROM routines WHERE id=?`, id).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, errors.New("routine not found")
		}
		return v, err
	}
	err := json.Unmarshal([]byte(data), &v)
	return v, err
}

func (s *Store) saveRoutine(v Routine) error {
	v.Next, v.Last, v.Recent, v.Waiting, v.Report = "", nil, nil, nil, nil
	b, _ := json.Marshal(v)
	_, err := s.db.Exec(`INSERT INTO routines(id,data) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, v.ID, string(b))
	return err
}

func (s *Store) deleteRoutine(id string) error {
	if _, err := s.db.Exec(`DELETE FROM routine_runs WHERE routine=?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM routines WHERE id=?`, id)
	return err
}

func (s *Store) saveRun(v RoutineRun) error {
	b, _ := json.Marshal(v)
	_, err := s.db.Exec(`INSERT INTO routine_runs(id,routine,started,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, v.ID, v.Routine, v.Started, string(b))
	return err
}

func (s *Store) run(id string) (RoutineRun, error) {
	var data string
	var v RoutineRun
	if err := s.db.QueryRow(`SELECT data FROM routine_runs WHERE id=?`, id).Scan(&data); err != nil {
		return v, err
	}
	err := json.Unmarshal([]byte(data), &v)
	return v, err
}

// runs lists a routine's runs, newest first; limit 0 lists all of them.
func (s *Store) runs(routine string, limit int) ([]RoutineRun, error) {
	q := `SELECT data FROM routine_runs WHERE routine=? ORDER BY started DESC`
	args := []any{routine}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
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

// waitingRuns lists a routine's runs that wait for approval, oldest first.
func (s *Store) waitingRuns(routine string) ([]RoutineRun, error) {
	rows, err := s.db.Query(`SELECT data FROM routine_runs WHERE routine=? AND json_extract(data,'$.status')='waiting' ORDER BY started`, routine)
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

// unseenReports lists a routine's runs whose report is for the user and not
// yet seen, newest first.
func (s *Store) unseenReports(routine string) ([]RoutineRun, error) {
	rows, err := s.db.Query(`SELECT data FROM routine_runs WHERE routine=? AND json_extract(data,'$.forYou')=1 AND json_extract(data,'$.seen') IS NULL ORDER BY started DESC`, routine)
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

// openRuns lists the runs not yet ended.
func (s *Store) openRuns() ([]RoutineRun, error) {
	rows, err := s.db.Query(`SELECT data FROM routine_runs WHERE json_extract(data,'$.status') IN ('running','sent','queued')`)
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

// pruneRuns keeps a month of runs, at most routineRunsKept per routine.
func (s *Store) pruneRuns(now time.Time) error {
	cut := now.Add(-routineRunsMaxAge).UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Exec(`DELETE FROM routine_runs WHERE started < ?`, cut); err != nil {
		return err
	}
	// A run that waits for approval or for its assistant's lane is kept
	// until it is a month old: a routine that runs every half hour would
	// otherwise push it out in a day.
	_, err := s.db.Exec(`DELETE FROM routine_runs WHERE id IN (SELECT id FROM (SELECT id, row_number() OVER (PARTITION BY routine ORDER BY started DESC) AS n FROM routine_runs) WHERE n > ?) AND json_extract(data,'$.status') NOT IN ('waiting','held')`, routineRunsKept)
	return err
}

// --- the page's view ---

// Routines lists the routines with when each is next due and its last run.
func (m *Manager) Routines() ([]Routine, error) {
	list, err := m.store.routines()
	if err != nil {
		return nil, err
	}
	m.rt.mu.Lock()
	for i := range list {
		if due, ok := m.rt.due[list[i].ID]; ok && list[i].Enabled {
			list[i].Next = due.Format(time.RFC3339)
		}
	}
	m.rt.mu.Unlock()
	for i := range list {
		if _, off := m.assistantOff(list[i]); off {
			list[i].AssistantOff, list[i].Next = true, ""
		}
	}
	for i := range list {
		runs, _ := m.store.runs(list[i].ID, routineRecentShown)
		for j := range runs {
			runs[j].Output = outputPreview(runs[j].Output)
			runs[j].Report = outputPreview(runs[j].Report)
		}
		if reports, _ := m.store.unseenReports(list[i].ID); len(reports) > 0 {
			list[i].Report = &reports[0]
		}
		if len(runs) > 0 {
			last := runs[0]
			last.Output, last.Report = "", ""
			list[i].Last, list[i].Recent = &last, runs
		}
		if list[i].Approve {
			waiting, _ := m.store.waitingRuns(list[i].ID)
			for j := range waiting {
				waiting[j].Output = outputPreview(waiting[j].Output)
			}
			if len(waiting) > 0 {
				list[i].Waiting = waiting
			}
		}
		if held, _ := m.store.heldRuns(list[i].ID); len(held) > 0 {
			for j := range held {
				held[j].Output = outputPreview(held[j].Output)
				m.ln.mu.Lock()
				if h, busy := m.laneHolder(held[j].Assistant, held[j].ID); busy {
					held[j].Behind, held[j].BehindSession = h.Name, h.Session
				}
				m.ln.mu.Unlock()
			}
			list[i].Held = held
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Created < list[j].Created })
	return list, nil
}

// outputPreview is the first line of a run's output that has text, cut to
// routineRecentPreview characters.
func outputPreview(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if r := []rune(line); len(r) > routineRecentPreview {
				return string(r[:routineRecentPreview]) + "…"
			}
			return line
		}
	}
	return ""
}

// SaveRoutine creates (no ID) or replaces a routine.
func (m *Manager) SaveRoutine(v Routine) (Routine, error) {
	now := m.rt.now()
	if err := m.checkRoutine(&v, now); err != nil {
		return v, err
	}
	if v.ID == "" {
		v.ID = token()[:16]
		v.Created = nowStamp()
		if v.CreatedBy == "" {
			v.CreatedBy = "page"
		}
	} else {
		old, err := m.store.routine(v.ID)
		if err != nil {
			return v, err
		}
		if strings.HasPrefix(old.CreatedBy, "file:") {
			return v, errFileRoutine(old)
		}
		v.Created, v.CreatedBy = old.Created, old.CreatedBy
		if old.When.Kind == v.When.Kind && old.When.Minutes == v.When.Minutes {
			v.Anchor = old.Anchor
		}
	}
	if v.Anchor == "" || v.When.Kind != "interval" {
		v.Anchor = now.UTC().Format(time.RFC3339)
	}
	v.Ran = ""
	if err := m.store.saveRoutine(v); err != nil {
		return v, err
	}
	m.scheduleRoutine(v, now)
	m.routineChanged(v.ID, nil)
	return v, nil
}

// SetRoutineEnabled turns a routine on or off.
func (m *Manager) SetRoutineEnabled(id string, on bool) (Routine, error) {
	v, err := m.store.routine(id)
	if err != nil {
		return v, err
	}
	now := m.rt.now()
	if on && v.When.Kind == "once" && v.When.next(now, time.Time{}).IsZero() {
		return v, errors.New("its time has passed; choose a new one")
	}
	v.Enabled = on
	if err := m.store.saveRoutine(v); err != nil {
		return v, err
	}
	m.scheduleRoutine(v, now)
	m.routineChanged(v.ID, nil)
	return v, nil
}

func (m *Manager) DeleteRoutine(id string) error {
	if err := m.store.deleteRoutine(id); err != nil {
		return err
	}
	m.rt.mu.Lock()
	delete(m.rt.due, id)
	m.rt.mu.Unlock()
	m.routineChanged(id, nil)
	return nil
}

// RoutineRuns lists a routine's recent runs with their output.
func (m *Manager) RoutineRuns(id string) ([]RoutineRun, error) {
	if _, err := m.store.routine(id); err != nil {
		return nil, err
	}
	return m.store.runs(id, routineRunsKept)
}

// RunRoutine runs a routine now, whatever its schedule.
func (m *Manager) RunRoutine(id string) (RoutineRun, error) {
	v, err := m.store.routine(id)
	if err != nil {
		return RoutineRun{}, err
	}
	if name, off := m.assistantOff(v); off {
		return RoutineRun{}, fmt.Errorf("%s is turned off", name)
	}
	now := m.rt.now()
	return m.startRun(v, now, false), nil
}

// assistantOff names the turned-off assistant a routine belongs to or sends
// to; its runs wait until it is on again.
func (m *Manager) assistantOff(v Routine) (string, bool) {
	dir := strings.TrimPrefix(v.CreatedBy, "file:")
	if dir == v.CreatedBy {
		dir = ""
	}
	if dir == "" && v.Target != nil && v.Target.Kind == "assistant" {
		dir = v.Target.Assistant
	}
	if dir == "" {
		return "", false
	}
	a, err := m.assistant(dir)
	if err != nil || a.Enabled {
		return "", false
	}
	return a.Name, true
}

// routineChanged tells every page a routine or one of its runs changed.
func (m *Manager) routineChanged(id string, run *RoutineRun) {
	data := map[string]any{"routine": id}
	if run != nil {
		r := *run
		// The report's first lines are enough for a notification; the page
		// reads the whole of it with the routines.
		r.Output, r.Report = "", outputPreview(r.Report)
		data["run"] = r
	}
	m.emit("", "routine", data)
}

// --- the scheduler ---

func (m *Manager) scheduleRoutine(v Routine, now time.Time) {
	m.rt.mu.Lock()
	defer m.rt.mu.Unlock()
	delete(m.rt.due, v.ID)
	if v.Enabled {
		anchor, _ := time.Parse(time.RFC3339, v.Anchor)
		if next := v.When.next(now, anchor); !next.IsZero() {
			m.rt.due[v.ID] = next
		}
	}
	m.rt.poke()
}

// loadRoutines schedules the stored routines from now on (what fell due
// while the workbench was not running is skipped) and ends the runs a
// previous process left open: their messages and commands ended with it.
func (m *Manager) loadRoutines() {
	now := m.rt.now()
	list, err := m.store.routines()
	if err != nil {
		fmt.Fprintln(os.Stderr, "load routines:", err)
		return
	}
	for _, v := range list {
		m.scheduleRoutine(v, now)
	}
	open, _ := m.store.openRuns()
	for _, run := range open {
		run.Status, run.Error, run.Finished = "failed", "the workbench restarted before it ended", nowStamp()
		_ = m.store.saveRun(run)
	}
}

// errFileRoutine: a routine an assistant.toml lists changes in that file.
func errFileRoutine(v Routine) error {
	return fmt.Errorf("%s/assistant.toml defines this routine; change it there", strings.TrimPrefix(v.CreatedBy, "file:"))
}

// RunRoutines starts what falls due until ctx ends.
func (m *Manager) RunRoutines(ctx context.Context) {
	m.loadRoutines()
	m.endOpenChanges()
	m.ensureShared()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-m.rt.wake:
		}
		// Routines from assistant.toml files follow their edits.
		m.Assistants()
		// Commits go to the repository's remote shortly after (sync.go).
		m.syncAssistants(false)
		wait := m.dueRoutines(m.rt.now())
		m.tickLanes(m.rt.now())
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)
	}
}

// dueRoutines starts the routines due by now and says how long until the
// next one (at most a minute, so a changed clock is noticed).
func (m *Manager) dueRoutines(now time.Time) time.Duration {
	type due struct {
		id string
		at time.Time
	}
	m.rt.mu.Lock()
	ready := []due{}
	wait := time.Minute
	for id, at := range m.rt.due {
		if !at.After(now) {
			ready = append(ready, due{id, at})
		} else if d := at.Sub(now); d < wait {
			wait = d
		}
	}
	m.rt.mu.Unlock()
	for _, d := range ready {
		v, err := m.store.routine(d.id)
		if err != nil {
			m.rt.mu.Lock()
			delete(m.rt.due, d.id)
			m.rt.mu.Unlock()
			continue
		}
		if name, off := m.assistantOff(v); off {
			// Paused with its assistant: no run. A once routine says why it
			// did not run; the others simply wait for their next time.
			if v.When.Kind == "once" {
				m.recordRun(RoutineRun{ID: token()[:16], Routine: v.ID, Name: v.Name, Due: d.at.UTC().Format(time.RFC3339), Started: nowStamp(), Finished: nowStamp(), Status: "skipped", Error: name + " is turned off"})
			}
		} else if now.Sub(d.at) > routineLate {
			m.recordRun(RoutineRun{ID: token()[:16], Routine: v.ID, Name: v.Name, Due: d.at.UTC().Format(time.RFC3339), Started: nowStamp(), Finished: nowStamp(), Status: "skipped", Error: "missed: the workbench was not running or the computer was asleep"})
		} else {
			m.startRun(v, d.at, true)
		}
		if v.When.Kind == "once" {
			v.Enabled, v.Ran = false, nowStamp()
			_ = m.store.saveRoutine(v)
		}
		m.scheduleRoutine(v, now)
	}
	if len(ready) > 0 {
		return 0
	}
	return wait
}

func (m *Manager) recordRun(run RoutineRun) {
	if err := m.store.saveRun(run); err != nil {
		fmt.Fprintln(os.Stderr, "save routine run:", err)
	}
	m.routineChanged(run.Routine, &run)
}

// startRun records a run of v and carries it out in the background.
func (m *Manager) startRun(v Routine, due time.Time, scheduled bool) RoutineRun {
	run := RoutineRun{ID: token()[:16], Routine: v.ID, Name: v.Name, Due: due.UTC().Format(time.RFC3339), Started: nowStamp(), Status: "running"}
	if v.Command != nil {
		m.rt.mu.Lock()
		busy := m.rt.running[v.ID]
		if !busy {
			m.rt.running[v.ID] = true
		}
		m.rt.mu.Unlock()
		if busy {
			run.Status, run.Finished, run.Error = "skipped", nowStamp(), "the previous run is still running"
			m.recordRun(run)
			return run
		}
	}
	m.recordRun(run)
	go m.carryOut(v, run)
	return run
}

func (m *Manager) carryOut(v Routine, run RoutineRun) {
	message := v.Message
	if c := v.Command; c != nil {
		out, exit, err := m.routineCommand(v)
		m.rt.mu.Lock()
		delete(m.rt.running, v.ID)
		m.rt.mu.Unlock()
		run.Output, run.Exit = out, exit
		switch {
		case err != nil:
			m.endRun(run, "failed", err.Error(), true)
			return
		case *exit != 0:
			m.endRun(run, "failed", fmt.Sprintf("exited with %d", *exit), true)
			return
		case v.Target == nil:
			m.endRun(run, "done", "", c.NotifyOnOutput && strings.TrimSpace(out) != "")
			return
		case strings.TrimSpace(out) == "":
			m.endRun(run, "skipped", "nothing printed, so nothing was sent", false)
			return
		case v.Approve:
			// Nothing reaches an agent until the user approves (ApproveRun).
			run.Status, run.Notify = "waiting", true
			m.recordRun(run)
			return
		}
		message = handOff(message, out)
	}
	m.sendRun(v, run, message)
}

// routineCommand runs a routine's command. An assistant's command waits
// while a background run changes the repository, and a background run
// waits for it: what a background run changed, and the workbench undid
// (ADR 0021), is never run in between.
func (m *Manager) routineCommand(v Routine) (string, *int, error) {
	if strings.HasPrefix(v.CreatedBy, "file:") {
		m.as.bg.Lock()
		defer m.as.bg.Unlock()
	}
	return runRoutineCommand(v.Command)
}

// ApproveRun hands a waiting run's output to its routine's message
// (approve), or ends the run without sending anything.
func (m *Manager) ApproveRun(routine, id string, approve bool) (RoutineRun, error) {
	v, err := m.store.routine(routine)
	if err != nil {
		return RoutineRun{}, err
	}
	if approve {
		if name, off := m.assistantOff(v); off {
			return RoutineRun{}, fmt.Errorf("%s is turned off", name)
		}
	}
	// One answer per run: a second click finds it no longer waiting.
	m.rt.mu.Lock()
	run, err := m.store.run(id)
	if err != nil || run.Routine != routine {
		m.rt.mu.Unlock()
		return RoutineRun{}, errors.New("run not found")
	}
	if run.Status == "held" && !approve {
		m.rt.mu.Unlock()
		return m.dismissHeld(run)
	}
	if run.Status != "waiting" {
		m.rt.mu.Unlock()
		return run, errors.New("this run no longer waits for approval")
	}
	if approve {
		run.Status, run.Notify = "sent", false
	} else {
		run.Status, run.Error, run.Finished, run.Notify = "skipped", "dismissed: nothing was sent", nowStamp(), false
	}
	err = m.store.saveRun(run)
	m.rt.mu.Unlock()
	if err != nil {
		return run, err
	}
	m.routineChanged(run.Routine, &run)
	if approve {
		if v.Target == nil {
			m.endRun(run, "failed", "the routine no longer sends a message", true)
			return run, nil
		}
		go m.sendRun(v, run, handOff(v.Message, run.Output))
	}
	return run, nil
}

// routinePreviewMax caps the preview file the page reads (ADR 0023).
const routinePreviewMax = 1 << 20

// RoutinePreview is the text of the file a routine's preview names, read by
// the workbench so the user sees it on the assistant's page before deciding,
// with or without a session (ADR 0023). Only a file inside the assistant's
// folder, after following links, and only up to routinePreviewMax.
func (m *Manager) RoutinePreview(routine string) (string, error) {
	full, err := m.routinePreviewPath(routine)
	if err != nil {
		return "", err
	}
	return readPreviewText(full)
}

// RoutinePreviewFile is the text of a Markdown file the preview links to,
// by its path relative to the preview's folder: a mail's file next to the
// mail preview (ADR 0026). Only a .md file inside that folder, after
// following links, so a link cannot reach the assistant's scripts or
// settings, and only up to routinePreviewMax.
func (m *Manager) RoutinePreviewFile(routine, file string) (string, error) {
	preview, err := m.routinePreviewPath(routine)
	if err != nil {
		return "", err
	}
	rel := filepath.FromSlash(file)
	if !filepath.IsLocal(rel) || !strings.EqualFold(filepath.Ext(rel), ".md") {
		return "", errors.New("a preview opens only .md files in its own folder")
	}
	base := filepath.Dir(preview)
	full, err := filepath.EvalSymlinks(filepath.Join(base, rel))
	if err != nil {
		return "", err
	}
	if inside, err := filepath.Rel(base, full); err != nil || inside == "." || !filepath.IsLocal(inside) ||
		!strings.EqualFold(filepath.Ext(full), ".md") {
		return "", errors.New("a preview opens only .md files in its own folder")
	}
	return readPreviewText(full)
}

// routinePreviewPath is the file a routine's preview names, links followed,
// once it is known to be inside the assistant's folder.
func (m *Manager) routinePreviewPath(routine string) (string, error) {
	v, err := m.store.routine(routine)
	if err != nil {
		return "", err
	}
	if v.Preview == "" || v.Command == nil {
		return "", errors.New("this routine has no preview")
	}
	dir, err := filepath.EvalSymlinks(v.Command.Dir)
	if err != nil {
		return "", err
	}
	full, err := filepath.EvalSymlinks(v.Preview)
	if err != nil {
		return "", err
	}
	if rel, err := filepath.Rel(dir, full); err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", errors.New("the preview is outside the assistant's folder")
	}
	return full, nil
}

// readPreviewText reads a regular file for the page, up to routinePreviewMax.
func readPreviewText(full string) (string, error) {
	f, err := os.Open(full)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return "", errors.New("the preview is not a file")
	}
	b := make([]byte, routinePreviewMax+1)
	n, err := io.ReadFull(f, b)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	if n > routinePreviewMax {
		return string(b[:routinePreviewMax]) + "\n\n… (cut at 1 MB)\n", nil
	}
	return string(b[:n]), nil
}

// handOff adds the end of what a command printed to the routine's message.
func handOff(message, out string) string {
	out = strings.TrimSpace(out)
	if len(out) > routineHandOffMax {
		out = "…" + validUTF8Tail(out, routineHandOffMax)
	}
	fence := "```"
	for strings.Contains(out, fence) {
		fence += "`"
	}
	if strings.TrimSpace(message) == "" {
		return fence + "\n" + out + "\n" + fence
	}
	return strings.TrimRight(message, "\n") + "\n\n" + fence + "\n" + out + "\n" + fence
}

// routineText is the message as the agent and the transcript see it: its
// first line names the routine, so a reader (and the agent) knows nobody
// typed it just now.
func routineText(name, message string) string {
	return "[Routine: " + strings.ReplaceAll(name, "]", ")") + "]\n\n" + message
}

func (m *Manager) sendRun(v Routine, run RoutineRun, message string) {
	text := routineText(v.Name, message)
	t := v.Target
	if !t.Background {
		text += reportNote
	}
	switch t.Kind {
	case "session":
		run.Session = t.Session
		queued, err := m.enqueue(t.Session, text, nil, run.ID)
		if err != nil {
			m.endRun(run, "failed", err.Error(), true)
			return
		}
		run.Status = "sent"
		if queued {
			run.Status = "queued"
		}
	case "new":
		s, err := m.createWithMessage(t.Agent, t.Cwd, "", v.Name, text, nil, t.Options, run.ID)
		if err != nil {
			m.endRun(run, "failed", err.Error(), true)
			return
		}
		run.Session, run.Status = s.ID, "sent"
	case "assistant":
		// One piece of routine work at a time per assistant (lane.go); a run
		// the lane already let through has Sent.
		if run.Sent == "" {
			run.Assistant = t.Assistant
			if !m.takeLane(&run) {
				return
			}
		}
		if t.Background {
			go m.backgroundRoutine(v, run, text)
			return
		}
		send := m.AssistantMessage
		if m.as.foreground != nil {
			send = m.as.foreground
		}
		s, queued, err := send(t.Assistant, text, nil, run.ID)
		run.Session = s.ID
		if err != nil {
			m.endRun(run, "failed", err.Error(), true)
			return
		}
		run.Status = "sent"
		if queued {
			run.Status = "queued"
		}
	}
	// The run may have ended already (a fast turn); keep what it recorded.
	if saved, err := m.store.run(run.ID); err == nil && (saved.Status == "done" || saved.Status == "failed") {
		return
	}
	m.recordRun(run)
}

func (m *Manager) endRun(run RoutineRun, status, reason string, notify bool) {
	run.Status, run.Error, run.Finished, run.Notify = status, reason, nowStamp(), notify
	m.recordRun(run)
	// What waited for its assistant goes next (lane.go).
	m.laneFreed(run.Assistant)
}

// What a routine's message asks of the agent's reply: it is the run's
// report, shown to the user unless it ends with quietMark. Routine work
// often finds nothing (no mail, nothing due), and the user is told only
// when there is something to look at or do.
const (
	quietMark  = "[Nothing for you]"
	reportNote = "\n\n(Workbench: your last reply is shown to the user as this routine's report. If nothing in it needs them to look or act, end it with the line " + quietMark + " and they are not told.)"
)

// reportOf is a turn's last reply as a run's report, without the quiet
// mark, and whether it is for the user: not when the reply is empty or
// ends with the mark (in bold, code or with a period, as agents write it).
func reportOf(reply string) (string, bool) {
	text := strings.TrimSpace(reply)
	lines := strings.Split(text, "\n")
	last := strings.Trim(strings.TrimSpace(lines[len(lines)-1]), "*_`.。 ")
	quiet := strings.EqualFold(last, quietMark)
	if quiet {
		text = strings.TrimSpace(strings.Join(lines[:len(lines)-1], "\n"))
	}
	if len(text) > routineReportMax {
		cut := routineReportMax
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "…"
	}
	return text, text != "" && !quiet
}

// SeeReports records that the user saw a routine's reports: they opened the
// session or dismissed the report.
func (m *Manager) SeeReports(id string) error {
	runs, err := m.store.unseenReports(id)
	if err != nil {
		return err
	}
	for _, run := range runs {
		run.Seen, run.Notify = nowStamp(), false
		m.recordRun(run)
	}
	return nil
}

// routineTurnEnded ends the run whose message started the turn that ended,
// keeping the last reply as its report.
func (m *Manager) routineTurnEnded(id, session string, failed bool, reason, reply string) {
	run, err := m.store.run(id)
	if err != nil || (run.Status != "sent" && run.Status != "queued") {
		return
	}
	if session != "" {
		run.Session = session
	}
	if failed {
		m.endRun(run, "failed", reason, true)
		return
	}
	// An older report of the routine the user has not seen gives way to
	// this one: the page shows the newest.
	if run.Report, run.ForYou = reportOf(reply); run.ForYou {
		_ = m.SeeReports(run.Routine)
	}
	m.endRun(run, "done", "", run.ForYou)
}

// tidyRoutines ends runs that never reported back, drops old runs and the
// once routines that ran a week ago.
func (m *Manager) tidyRoutines(now time.Time) {
	open, _ := m.store.openRuns()
	for _, run := range open {
		started, _ := time.Parse(time.RFC3339Nano, run.Started)
		if run.Status != "running" && now.Sub(started) > routineRunStale {
			m.endRun(run, "failed", "no result after a day", false)
		}
	}
	if err := m.store.pruneRuns(now); err != nil {
		fmt.Fprintln(os.Stderr, "prune routine runs:", err)
	}
	if err := m.store.pruneChanges(now); err != nil {
		fmt.Fprintln(os.Stderr, "prune assistant changes:", err)
	}
	list, _ := m.store.routines()
	for _, v := range list {
		ran, err := time.Parse(time.RFC3339Nano, v.Ran)
		if v.When.Kind == "once" && !v.Enabled && err == nil && now.Sub(ran) > routineOnceKept {
			_ = m.DeleteRoutine(v.ID)
		}
	}
}

// --- commands ---

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu   sync.Mutex
	max  int
	buf  []byte
	more bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
		t.more = true
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := validUTF8Tail(string(t.buf), t.max)
	if t.more {
		s = "…" + s
	}
	return s
}

// validUTF8Tail is the last max bytes of s, starting at a whole character.
func validUTF8Tail(s string, max int) string {
	if len(s) > max {
		s = s[len(s)-max:]
	}
	for i := 0; i < len(s) && i < 4; i++ {
		if s[i]&0xC0 != 0x80 {
			return s[i:]
		}
	}
	return strings.ToValidUTF8(s, "")
}

// runRoutineCommand runs c with the user's shell in its folder, without
// input, until it exits or its time is up; it returns the end of its output
// and its exit code.
func runRoutineCommand(c *RoutineCommand) (string, *int, error) {
	timeout := time.Duration(c.TimeoutMin) * time.Minute
	if timeout <= 0 {
		timeout = routineTimeoutMin * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd, err := shellCommand(c.Run)
	if err != nil {
		return "", nil, err
	}
	cmd.Dir = c.Dir
	cmd.Env = routineEnv()
	out := &tailBuffer{max: routineOutputMax}
	cmd.Stdout, cmd.Stderr, cmd.Stdin = out, out, bytes.NewReader(nil)
	group, err := startGroup(cmd)
	if err != nil {
		return "", nil, err
	}
	unwatch := group.killOn(ctx)
	err = cmd.Wait()
	unwatch()
	// What it left running in its group ends with it.
	group.Kill(group.PIDs())
	group.Release()
	if ctx.Err() == context.DeadlineExceeded {
		return out.String(), nil, fmt.Errorf("stopped after %d minutes", int(timeout/time.Minute))
	}
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return out.String(), nil, err
		}
		code = exit.ExitCode()
	}
	return out.String(), &code, nil
}

// routinesAPI serves /api/routines[/{id}[/runs|/run|/preview[?file=]|/approve|/dismiss|/runs/{run}/approve|dismiss|start]].
func (s *Server) routinesAPI(w http.ResponseWriter, r *http.Request, rest string) {
	m := s.Manager
	parts := strings.Split(rest, "/")
	id := parts[0]
	switch {
	case id == "" && r.Method == "GET":
		list, err := m.Routines()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, list)
	case id == "" && r.Method == "POST":
		var v Routine
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		// A preview is a file an assistant's routine names (assistant.toml).
		v.ID, v.CreatedBy, v.Preview = "", "page", ""
		saved, err := m.SaveRoutine(v)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, saved)
	case len(parts) == 1 && r.Method == "PUT":
		var v Routine
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		v.ID, v.Preview = id, ""
		saved, err := m.SaveRoutine(v)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, saved)
	case len(parts) == 1 && r.Method == "PATCH":
		var v struct {
			Enabled *bool `json:"enabled"`
		}
		if err := decode(w, r, &v); err != nil || v.Enabled == nil {
			bad(w, errors.New("send {enabled}"))
			return
		}
		saved, err := m.SetRoutineEnabled(id, *v.Enabled)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, saved)
	case len(parts) == 1 && r.Method == "DELETE":
		if old, err := m.store.routine(id); err == nil && strings.HasPrefix(old.CreatedBy, "file:") {
			bad(w, errFileRoutine(old))
			return
		}
		if err := m.DeleteRoutine(id); err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case len(parts) == 2 && parts[1] == "runs" && r.Method == "GET":
		runs, err := m.RoutineRuns(id)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, runs)
	case len(parts) == 2 && parts[1] == "preview" && r.Method == "GET":
		var text string
		var err error
		if file := r.URL.Query().Get("file"); file != "" {
			text, err = m.RoutinePreviewFile(id, file)
		} else {
			text, err = m.RoutinePreview(id)
		}
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "the file is not there (any more)", http.StatusNotFound)
			return
		} else if err != nil {
			bad(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]string{"text": text})
	case len(parts) == 4 && parts[1] == "runs" && (parts[3] == "approve" || parts[3] == "dismiss") && r.Method == "POST":
		run, err := m.ApproveRun(id, parts[2], parts[3] == "approve")
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, run)
	case len(parts) == 4 && parts[1] == "runs" && parts[3] == "start" && r.Method == "POST":
		run, err := m.StartHeld(id, parts[2])
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, run)
	case len(parts) == 2 && (parts[1] == "approve" || parts[1] == "dismiss") && r.Method == "POST":
		// Every waiting run at once (lane.go).
		run, err := m.ApproveWaiting(id, parts[1] == "approve")
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, run)
	case len(parts) == 2 && parts[1] == "seen" && r.Method == "POST":
		// The user saw its report: opened its session or dismissed it.
		if err := m.SeeReports(id); err != nil {
			bad(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 2 && parts[1] == "run" && r.Method == "POST":
		run, err := m.RunRoutine(id)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, run)
	default:
		http.NotFound(w, r)
	}
}

// routineEnv is a terminal's environment (the workbench's own service
// variables left out) for a command without a terminal: plain output.
func routineEnv() []string {
	out := []string{}
	for _, kv := range terminalEnv() {
		name, _, _ := strings.Cut(kv, "=")
		if name != "TERM" && name != "COLORTERM" {
			out = append(out, kv)
		}
	}
	return append(out, "TERM=dumb", "NO_COLOR=1")
}
