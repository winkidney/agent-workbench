package workbench

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Background work: what a session runs outside its turns, so the page can say
// that the engine is still at work, and stop it.
//
//   - Claude reports its tasks on stream-json: background_tasks_changed is the
//     set of live background tasks (replace semantics, per process), and the
//     task_started / task_progress / task_updated / task_notification records
//     carry the details. stop_task stops one; background_tasks moves a
//     foreground command or subagent to the background (Ctrl+B).
//   - Codex's background terminals are only listed on request
//     (thread/backgroundTerminals/list), so they are polled after turns and
//     while any runs; terminate and clean stop them.
//   - Anything else the engine left running (a server started with nohup) is
//     an extra process in its unit or process group.

// BackgroundTask is one piece of work, running or recently ended.
type BackgroundTask struct {
	ID          string `json:"id"`
	Source      string `json:"source"` // claude | codex | process
	Kind        string `json:"kind"`   // shell, monitor, agent, mcp, workflow, terminal, process, task
	Description string `json:"description"`
	// running, or how it ended: completed, failed, stopped, ended (gone
	// without a reported result).
	Status   string  `json:"status"`
	Started  string  `json:"started,omitempty"`
	Ended    string  `json:"ended,omitempty"`
	Summary  string  `json:"summary,omitempty"`
	Progress string  `json:"progress,omitempty"`
	Output   bool    `json:"output,omitempty"`
	PID      int     `json:"pid,omitempty"`
	CPU      float64 `json:"cpu,omitempty"`
	RSSKb    float64 `json:"rssKb,omitempty"`

	outputFile string
	toolUseID  string
	order      int
}

// finishedKept is how many ended tasks stay listed until the next message.
const finishedKept = 8

func now3339() string { return time.Now().UTC().Format(time.RFC3339) }

func claudeTaskKind(t string) string {
	switch {
	case t == "local_bash":
		return "shell"
	case strings.HasPrefix(t, "monitor"):
		return "monitor"
	case t == "local_agent" || t == "remote_agent" || t == "in_process_teammate":
		return "agent"
	case t == "mcp_task":
		return "mcp"
	case t == "local_workflow":
		return "workflow"
	}
	return "task"
}

func (r *runtime) task(id string) *BackgroundTask {
	if r.tasks == nil {
		r.tasks = map[string]*BackgroundTask{}
	}
	t := r.tasks[id]
	if t == nil {
		r.taskSeq++
		t = &BackgroundTask{ID: id, Status: "running", Started: now3339(), order: r.taskSeq, outputFile: r.taskOutputs[id]}
		r.tasks[id] = t
	}
	return t
}

var claudeBackgroundResult = regexp.MustCompile(`with ID: (\S+?)\.? Output is being written to: (\S+?\.output)`)

// noteClaudeOutput reads where a task Claude moved to the background writes
// its output, from the tool result that says so (m.mu held).
func noteClaudeOutput(r *runtime, v map[string]any) {
	for _, raw := range asList(obj(v["message"])["content"]) {
		c := obj(raw)
		if str(c["type"]) != "tool_result" {
			continue
		}
		m := claudeBackgroundResult.FindStringSubmatch(nativeText(c["content"]))
		if m == nil {
			continue
		}
		if r.taskOutputs == nil {
			r.taskOutputs = map[string]string{}
		}
		r.taskOutputs[m[1]] = m[2]
		if t := r.tasks[m[1]]; t != nil {
			t.outputFile = m[2]
		}
	}
}

// end records how a task ended. A line in the conversation says so, unless
// Claude's own record of its result already does.
func (m *Manager) endTask(r *runtime, t *BackgroundTask, status, summary string) {
	if t.Status != "running" {
		return
	}
	t.Status, t.Ended = status, now3339()
	if summary != "" {
		t.Summary = summary
	}
	defer m.trimFinished(r)
	if t.Source == "claude" && status != "ended" {
		return
	}
	text := t.Summary
	if text == "" {
		text = fmt.Sprintf("Background %s %s: %s", t.Kind, map[string]string{"completed": "finished", "failed": "failed", "stopped": "was stopped", "ended": "ended"}[status], t.Description)
	}
	data := map[string]any{"text": text}
	if status == "failed" {
		data["tone"] = "warn"
	}
	m.emit(r.session.ID, "notice", data)
}

func (m *Manager) trimFinished(r *runtime) {
	var done []*BackgroundTask
	for _, t := range r.tasks {
		if t.Status != "running" {
			done = append(done, t)
		}
	}
	sort.Slice(done, func(i, j int) bool { return done[i].order < done[j].order })
	for len(done) > finishedKept {
		delete(r.tasks, done[0].ID)
		done = done[1:]
	}
}

// trackClaudeTask follows Claude's task records (m.mu held).
func (m *Manager) trackClaudeTask(r *runtime, v map[string]any) {
	id := str(v["task_id"])
	switch str(v["subtype"]) {
	case "background_tasks_changed":
		live := map[string]bool{}
		for _, raw := range asList(v["tasks"]) {
			e := obj(raw)
			tid := str(e["task_id"])
			if tid == "" {
				continue
			}
			live[tid] = true
			t := r.task(tid)
			t.Source, t.Kind = "claude", claudeTaskKind(str(e["task_type"]))
			if d := str(e["description"]); d != "" {
				t.Description = d
			}
		}
		// A task that left the set without its result ended all the same.
		for _, t := range r.tasks {
			if t.Source == "claude" && t.Status == "running" && !live[t.ID] {
				m.endTask(r, t, "ended", "")
			}
		}
	case "task_started":
		// Foreground tasks are part of the turn; they join when moved to
		// the background, which Claude takes once they are registered.
		if id == "" || v["is_backgrounded"] != true {
			tool := str(v["tool_use_id"])
			if t := r.tasks[id]; t != nil {
				t.toolUseID = tool
			}
			if tool != "" && id != "" {
				if r.foreground == nil {
					r.foreground = map[string]string{}
				}
				r.foreground[tool] = id
				if r.wantBackground[tool] {
					delete(r.wantBackground, tool)
					_ = r.proc.Input(map[string]any{"type": "control_request", "request_id": "background:" + token(), "request": map[string]any{"subtype": "background_tasks", "tool_use_id": tool}})
				}
			}
			return
		}
		t := r.task(id)
		t.Source, t.Kind = "claude", claudeTaskKind(str(v["task_type"]))
		t.Description = str(v["description"])
		t.toolUseID = str(v["tool_use_id"])
	case "task_progress":
		t := r.tasks[id]
		if t == nil {
			return
		}
		if s := str(v["summary"]); s != "" {
			t.Progress = s
		} else if n := num(obj(v["usage"])["tool_uses"]); n > 0 {
			t.Progress = plural(int(n), "tool use")
			if last := str(v["last_tool_name"]); last != "" {
				t.Progress += " · " + last
			}
		}
	case "task_updated":
		t := r.tasks[id]
		patch := obj(v["patch"])
		if t == nil {
			if patch["is_backgrounded"] != true {
				return
			}
			t = r.task(id)
			t.Source, t.Kind = "claude", "task"
		}
		if d := str(patch["description"]); d != "" {
			t.Description = d
		}
		switch str(patch["status"]) {
		case "completed", "failed":
			t.Progress = ""
		case "killed":
			t.Progress = ""
		}
	case "task_notification":
		for tool, task := range r.foreground {
			if task == id {
				delete(r.foreground, tool)
			}
		}
		t := r.tasks[id]
		if t == nil {
			return
		}
		if f := str(v["output_file"]); f != "" {
			t.outputFile = f
		}
		status := str(v["status"])
		if status != "completed" && status != "failed" && status != "stopped" {
			status = "ended"
		}
		m.endTask(r, t, status, str(v["summary"]))
	}
}

// clearTasks forgets the tasks of an engine that stopped: its processes went
// with it (m.mu held).
func clearTasks(r *runtime) {
	r.tasks = nil
	r.others = nil
	if r.terminalPoll != nil {
		r.terminalPoll.Stop()
		r.terminalPoll = nil
	}
}

// clearFinished drops ended tasks once the user writes again (m.mu held).
func clearFinished(r *runtime) {
	for id, t := range r.tasks {
		if t.Status != "running" {
			delete(r.tasks, id)
		}
	}
}

func runningTasks(r *runtime) int {
	n := 0
	for _, t := range r.tasks {
		if t.Status == "running" {
			n++
		}
	}
	return n
}

// backgroundOf counts what runs in the background: the agents' tasks, or
// else the extra processes last seen.
func backgroundOf(r *runtime) int {
	if r.proc == nil {
		return 0
	}
	if n := runningTasks(r); n > 0 {
		return n
	}
	n := 0
	for _, o := range r.others {
		if processAlive(o.PID) {
			n++
		}
	}
	return n
}

// tasksOf lists the session's background work for the page: running first,
// in start order, then what ended (m.mu held).
func tasksOf(r *runtime) []BackgroundTask {
	out := []BackgroundTask{}
	for _, t := range r.tasks {
		c := *t
		c.Output = c.outputFile != ""
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Status == "running", out[j].Status == "running"
		if a != b {
			return a
		}
		return out[i].order < out[j].order
	})
	if runningTasks(r) == 0 {
		others := []BackgroundTask{}
		for _, o := range r.others {
			if processAlive(o.PID) {
				others = append(others, o)
			}
		}
		out = append(others, out...)
	}
	return out
}

// Codex background terminals.

func (m *Manager) scheduleTerminalPoll(r *runtime, after time.Duration) {
	if r.session.Agent != "codex" || r.noTerminals || r.proc == nil {
		return
	}
	if r.terminalPoll != nil {
		r.terminalPoll.Stop()
	}
	id := r.session.ID
	r.terminalPoll = time.AfterFunc(after, func() { m.pollTerminals(id) })
}

func (m *Manager) pollTerminals(id string) {
	m.mu.Lock()
	r := m.find(id)
	if r == nil || r.proc == nil || !r.ready {
		m.mu.Unlock()
		return
	}
	thread := r.session.NativeID
	m.mu.Unlock()
	v, err := m.rpc(id, "thread/backgroundTerminals/list", map[string]any{"threadId": thread, "limit": 50})
	m.mu.Lock()
	defer m.mu.Unlock()
	r = m.find(id)
	if r == nil || r.proc == nil {
		return
	}
	if err != nil {
		// A Codex without background terminals.
		if strings.Contains(err.Error(), "method") || strings.Contains(err.Error(), "unknown") {
			r.noTerminals = true
		}
		return
	}
	m.applyTerminals(r, asList(v["data"]))
	if runningTasks(r) > 0 {
		m.scheduleTerminalPoll(r, 5*time.Second)
	}
	m.syncAttention(r)
}

// applyTerminals replaces the running Codex terminals with a listing (m.mu
// held).
func (m *Manager) applyTerminals(r *runtime, list []any) {
	live := map[string]bool{}
	for _, raw := range list {
		e := obj(raw)
		id := str(e["processId"])
		if id == "" {
			continue
		}
		live[id] = true
		t := r.task("terminal:" + id)
		if t.Status != "running" {
			continue
		}
		t.Source, t.Kind = "codex", "terminal"
		t.Description = str(e["command"])
		t.PID, t.CPU, t.RSSKb = int(num(e["osPid"])), num(e["cpuPercent"]), num(e["rssKb"])
		t.toolUseID = str(e["itemId"])
	}
	for _, t := range r.tasks {
		if t.Source == "codex" && t.Status == "running" && !live[strings.TrimPrefix(t.ID, "terminal:")] {
			m.endTask(r, t, "ended", "")
		}
	}
}

// Extra processes.

// extraLister is an engine that knows the processes it started after it
// became ready, and its own main process.
type extraLister interface {
	ExtraPIDs() []int
	RootPID() int
}

// settled is how long a process must have run to count: the CLIs start short
// helpers of their own (ps, shell snapshots) all the time.
var settled = 10 * time.Second

// descendsFrom is whether pid's ancestry reaches root.
func descendsFrom(pid, root int) bool {
	for i := 0; i < 64 && pid > 1; i++ {
		if pid == root {
			return true
		}
		pid = processParent(pid)
	}
	return false
}

// Claude Code runs a command as `zsh -c source <snapshot> … && eval '<command>'
// …`; the command is what the user should see.
var evalCommand = regexp.MustCompile(`&& eval '((?:[^']|'"'"')*)'`)

func cleanCommand(cmd string) string {
	if m := evalCommand.FindStringSubmatch(cmd); m != nil {
		cmd = strings.ReplaceAll(m[1], `'"'"'`, "'")
	}
	if len(cmd) > 300 {
		cmd = cmd[:299] + "…"
	}
	return cmd
}

// otherProcesses lists what the engine left running that no agent reports
// as a task: processes in its unit that outlived the command that started
// them (`nohup server &`), so they no longer descend from the engine. What
// the CLI runs itself (MCP servers, helpers, its background tasks) descends
// from it and is not listed. Each one whose parent is not one of them, as
// its command line.
func otherProcesses(p Process) []BackgroundTask {
	lister, ok := p.(extraLister)
	if !ok {
		return nil
	}
	root := lister.RootPID()
	if root <= 0 {
		return nil
	}
	pids := []int{}
	for _, pid := range lister.ExtraPIDs() {
		if !descendsFrom(pid, root) {
			pids = append(pids, pid)
		}
	}
	set := map[int]bool{}
	for _, pid := range pids {
		set[pid] = true
	}
	out := []BackgroundTask{}
	for _, pid := range pids {
		if set[processParent(pid)] {
			continue
		}
		if age, ok := processAge(pid); ok && age < settled {
			continue
		}
		cmd := processCommand(pid)
		if cmd == "" {
			cmd = "process " + strconv.Itoa(pid)
		}
		out = append(out, BackgroundTask{ID: "pid:" + strconv.Itoa(pid), Source: "process", Kind: "process", Description: cmd, Status: "running", PID: pid})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// refreshOthers looks at the extra processes of an engine between turns
// (m.mu held; reading them is quick).
func (m *Manager) refreshOthers(r *runtime) {
	if r.proc == nil || r.turn || runningTasks(r) > 0 {
		r.others = nil
	} else {
		r.others = otherProcesses(r.proc)
	}
	m.syncAttention(r)
}

// Actions.

// StopBackground stops one task (taskID), or all of them (taskID "").
func (m *Manager) StopBackground(id, taskID string) error {
	m.mu.Lock()
	r := m.find(id)
	if r == nil || r.proc == nil {
		m.mu.Unlock()
		return errors.New("session is not running")
	}
	targets := []BackgroundTask{}
	for _, t := range tasksOf(r) {
		if t.Status == "running" && (taskID == "" || t.ID == taskID) {
			targets = append(targets, t)
		}
	}
	thread := r.session.NativeID
	m.mu.Unlock()
	if len(targets) == 0 {
		return errors.New("no such background task")
	}
	var errs []error
	cleaned := false
	for _, t := range targets {
		var err error
		switch t.Source {
		case "claude":
			_, err = m.rpc(id, "stop_task", map[string]any{"task_id": t.ID})
		case "codex":
			if taskID == "" {
				if !cleaned {
					_, err = m.rpc(id, "thread/backgroundTerminals/clean", map[string]any{"threadId": thread})
					cleaned = true
				}
			} else {
				_, err = m.rpc(id, "thread/backgroundTerminals/terminate", map[string]any{"threadId": thread, "processId": strings.TrimPrefix(t.ID, "terminal:")})
			}
		case "process":
			err = m.stopOther(id, t.PID)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	m.mu.Lock()
	if r := m.find(id); r != nil {
		if r.session.Agent == "codex" {
			m.scheduleTerminalPoll(r, 300*time.Millisecond)
		}
		m.refreshOthers(r)
	}
	m.mu.Unlock()
	return errors.Join(errs...)
}

// stopOther ends an extra process of the engine and what it started.
func (m *Manager) stopOther(id string, pid int) error {
	m.mu.Lock()
	r := m.find(id)
	var proc Process
	if r != nil {
		proc = r.proc
	}
	m.mu.Unlock()
	lister, ok := proc.(extraLister)
	if !ok {
		return errors.New("cannot list the engine's processes here")
	}
	extra := map[int]bool{}
	for _, p := range lister.ExtraPIDs() {
		extra[p] = true
	}
	if !extra[pid] {
		return errors.New("that process is no longer running")
	}
	// The process and its descendants among the extra ones.
	kill := []int{pid}
	for changed := true; changed; {
		changed = false
		for p := range extra {
			if !containsInt(kill, p) && containsInt(kill, processParent(p)) {
				kill = append(kill, p)
				changed = true
			}
		}
	}
	return terminate(kill)
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// TaskOutput is the end of a Claude task's output file.
func (m *Manager) TaskOutput(id, taskID string) (string, error) {
	m.mu.Lock()
	r := m.find(id)
	path := ""
	if r != nil && r.tasks[taskID] != nil {
		path = r.tasks[taskID].outputFile
	}
	m.mu.Unlock()
	if path == "" {
		return "", errors.New("no output for this task")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("no output for this task")
	}
	const tail = 64 * 1024
	if info.Size() > tail {
		if _, err := f.Seek(-tail, io.SeekEnd); err != nil {
			return "", err
		}
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	s := string(b)
	if info.Size() > tail {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	return s, nil
}

// MoveToBackground asks Claude to move a foreground command or subagent (or
// all of them, toolUseID "") to the background, as Ctrl+B does.
func (m *Manager) MoveToBackground(id, toolUseID string) error {
	m.mu.Lock()
	r := m.find(id)
	if r == nil || r.proc == nil || r.session.Agent != "claude" {
		m.mu.Unlock()
		return errors.New("only a running Claude session can move work to the background")
	}
	if !r.turn {
		m.mu.Unlock()
		return errors.New("nothing is running in the foreground")
	}
	// Claude ignores a move asked for before it registered the task: ask
	// again as it does.
	if toolUseID != "" && r.foreground[toolUseID] == "" {
		if r.wantBackground == nil {
			r.wantBackground = map[string]bool{}
		}
		r.wantBackground[toolUseID] = true
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	params := map[string]any{}
	if toolUseID != "" {
		params["tool_use_id"] = toolUseID
	}
	_, err := m.rpc(id, "background_tasks", params)
	return err
}

// claudeTurnSignal is a record that only a running turn produces.
func claudeTurnSignal(v map[string]any) bool {
	// A subagent's records: its own work (in the background, possibly after
	// the turn ended), not a turn of the session.
	if str(v["parent_tool_use_id"]) != "" {
		return false
	}
	switch str(v["type"]) {
	case "stream_event":
		return true
	case "assistant":
		return str(obj(v["message"])["model"]) != "<synthetic>"
	case "system":
		return str(v["subtype"]) == "thinking_tokens" || (str(v["subtype"]) == "status" && str(v["status"]) == "requesting")
	}
	return false
}

// refreshAllOthers looks at the extra processes of the engines that are
// between turns (only the session id, when given), outside the lock: listing a
// unit's processes runs systemctl.
func (m *Manager) refreshAllOthers(only ...string) {
	m.mu.Lock()
	type job struct {
		id   string
		proc Process
	}
	jobs := []job{}
	for id, r := range m.sessions {
		if len(only) > 0 && id != only[0] && r.session.ID != only[0] {
			continue
		}
		if r.proc != nil && r.ready && !r.turn && runningTasks(r) == 0 {
			jobs = append(jobs, job{id, r.proc})
		}
	}
	m.mu.Unlock()
	for _, j := range jobs {
		others := otherProcesses(j.proc)
		m.mu.Lock()
		if r := m.sessions[j.id]; r != nil && r.proc == j.proc && !r.turn && runningTasks(r) == 0 {
			r.others = others
			m.syncAttention(r)
		}
		m.mu.Unlock()
	}
}
