package workbench

import (
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func backgroundNow(m *Manager, r *runtime) (int, []BackgroundTask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return r.session.Background, tasksOf(r)
}

func lastStatusBackground(m *Manager, id string) int {
	events := eventsOf(m, id, "status")
	if len(events) == 0 {
		return -1
	}
	return int(num(events[len(events)-1]["background"]))
}

// The records Claude Code 2.1.322 wrote for a background `sleep 8; echo
// bg-done` (recorded in stream-json mode).
func TestClaudeBackgroundTasks(t *testing.T) {
	m, _, r := claudeReady(t)
	id := r.session.ID
	out := filepath.Join(t.TempDir(), "b5lg2nmqa.output")
	_ = os.WriteFile(out, []byte("line 1\nbg-done\n"), 0o600)
	feed := func(v map[string]any) { m.receive(id, r.generation, v) }

	if err := m.Send(id, "start it in the background"); err != nil {
		t.Fatal(err)
	}
	feed(map[string]any{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{map[string]any{"task_id": "b5lg2nmqa", "task_type": "local_bash", "description": "Background sleep"}}})
	feed(map[string]any{"type": "system", "subtype": "task_started", "task_id": "b5lg2nmqa", "tool_use_id": "toolu_1", "description": "Background sleep", "is_backgrounded": true, "task_type": "local_bash"})
	feed(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": "Command running in background with ID: b5lg2nmqa. Output is being written to: " + out + ". You will be notified when it completes."}}}})
	feed(map[string]any{"type": "result", "subtype": "success"})

	count, tasks := backgroundNow(m, r)
	if count != 1 || len(tasks) != 1 || tasks[0].Kind != "shell" || tasks[0].Status != "running" || !tasks[0].Output || lastStatusBackground(m, id) != 1 {
		t.Fatalf("running: %d %+v (status event %d)", count, tasks, lastStatusBackground(m, id))
	}
	if text, err := m.TaskOutput(id, "b5lg2nmqa"); err != nil || !strings.Contains(text, "bg-done") {
		t.Fatalf("output: %q %v", text, err)
	}
	if got := m.Telemetry(id).Background; len(got) != 1 || got[0].ID != "b5lg2nmqa" {
		t.Fatalf("telemetry: %+v", got)
	}
	if list, _ := m.NativeList(); !func() bool {
		for _, s := range list {
			if s.ID == id {
				return s.Background == 1
			}
		}
		return false
	}() {
		t.Fatal("the session list does not count it")
	}

	// It ends; Claude starts a turn by itself to read the result.
	feed(map[string]any{"type": "system", "subtype": "task_updated", "task_id": "b5lg2nmqa", "patch": map[string]any{"status": "completed"}})
	feed(map[string]any{"type": "system", "subtype": "task_notification", "task_id": "b5lg2nmqa", "status": "completed", "output_file": out, "summary": `Background command "Background sleep" completed (exit code 0)`})
	feed(map[string]any{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{}})
	count, tasks = backgroundNow(m, r)
	if count != 0 || len(tasks) != 1 || tasks[0].Status != "completed" || lastStatusBackground(m, id) != 0 {
		t.Fatalf("ended: %d %+v", count, tasks)
	}
	// Claude's own task-notification record says how it ended.
	if got := notices(t, m, id); len(got) != 0 {
		t.Fatalf("notice: %q", got)
	}
	feed(map[string]any{"type": "system", "subtype": "thinking_tokens", "estimated_tokens": 50.0, "estimated_tokens_delta": 50.0})
	m.mu.Lock()
	running, status := r.turn, r.session.Status
	m.mu.Unlock()
	if !running || status != "running" {
		t.Fatalf("Claude's own turn: %v %s", running, status)
	}
	feed(map[string]any{"type": "result", "subtype": "success"})
	m.mu.Lock()
	running, status = r.turn, r.session.Status
	m.mu.Unlock()
	if running || status != "idle" {
		t.Fatalf("after Claude's own turn: %v %s", running, status)
	}
	// Ended tasks stay listed until the next message.
	if err := m.Send(id, "thanks"); err != nil {
		t.Fatal(err)
	}
	if _, tasks = backgroundNow(m, r); len(tasks) != 0 {
		t.Fatalf("after the next message: %+v", tasks)
	}
}

func TestClaudeBackgroundTaskThatLeavesWithoutResult(t *testing.T) {
	m, _, r := claudeReady(t)
	id := r.session.ID
	m.receive(id, r.generation, map[string]any{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{map[string]any{"task_id": "a1", "task_type": "local_agent", "description": "Explore auth"}}})
	m.receive(id, r.generation, map[string]any{"type": "system", "subtype": "task_progress", "task_id": "a1", "usage": map[string]any{"tool_uses": 12.0}, "last_tool_name": "Grep"})
	if _, tasks := backgroundNow(m, r); len(tasks) != 1 || tasks[0].Kind != "agent" || tasks[0].Progress != "12 tool uses · Grep" {
		t.Fatalf("agent: %+v", tasks)
	}
	m.receive(id, r.generation, map[string]any{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{}})
	if _, tasks := backgroundNow(m, r); tasks[0].Status != "ended" {
		t.Fatalf("left the set: %+v", tasks)
	}
	if got := notices(t, m, id); len(got) != 1 || got[0] != "Background agent ended: Explore auth" {
		t.Fatalf("notice: %q", got)
	}
}

func TestClaudeBackgroundActions(t *testing.T) {
	m, p, r := claudeReady(t)
	id := r.session.ID
	var mu sync.Mutex
	requests := []map[string]any{}
	seen := func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any{}, requests...)
	}
	answerClaude(t, m, p, r, func(req map[string]any) (map[string]any, string) {
		if req["subtype"] != "initialize" {
			mu.Lock()
			requests = append(requests, req)
			mu.Unlock()
		}
		return map[string]any{}, ""
	})
	m.receive(id, r.generation, map[string]any{"type": "system", "subtype": "background_tasks_changed", "tasks": []any{map[string]any{"task_id": "b1", "task_type": "monitor", "description": "watch CI"}}})
	if err := m.StopBackground(id, "b1"); err != nil {
		t.Fatal(err)
	}
	if got := seen(); len(got) != 1 || got[0]["subtype"] != "stop_task" || got[0]["task_id"] != "b1" {
		t.Fatalf("stop: %v", got)
	}
	if err := m.StopBackground(id, "nope"); err == nil {
		t.Fatal("an unknown task")
	}
	// Moving work to the background needs a running turn.
	if err := m.MoveToBackground(id, "toolu_9"); err == nil {
		t.Fatal("moved without a turn")
	}
	if err := m.Send(id, "run the long build"); err != nil {
		t.Fatal(err)
	}
	// Asked before Claude registered the command: sent once it has.
	before := len(seen())
	if err := m.MoveToBackground(id, "toolu_9"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := seen(); len(got) != before {
		t.Fatalf("sent too early: %v", got[before:])
	}
	m.receive(id, r.generation, map[string]any{"type": "system", "subtype": "task_started", "task_id": "f1", "tool_use_id": "toolu_9", "description": "build", "is_backgrounded": false, "task_type": "local_bash"})
	if !waitFor(func() bool { return len(seen()) > before }) {
		t.Fatal("not sent once registered")
	}
	if got := seen(); got[len(got)-1]["subtype"] != "background_tasks" || got[len(got)-1]["tool_use_id"] != "toolu_9" {
		t.Fatalf("to background: %v", got[len(got)-1])
	}
	// Registered already: sent at once.
	if err := m.MoveToBackground(id, "toolu_9"); err != nil {
		t.Fatal(err)
	}
	if got := seen(); got[len(got)-1]["subtype"] != "background_tasks" || len(got) != before+2 {
		t.Fatalf("second move: %v", got[before:])
	}
}

func TestCodexBackgroundTerminals(t *testing.T) {
	m, p, r, _ := codexReady(t)
	id := r.session.ID
	respond := responder(t, m, p, r)
	respond("thread/goal/get", map[string]any{})
	server := map[string]any{"processId": "17", "command": "/usr/bin/zsh -lc 'npm run dev'", "cwd": "/w", "itemId": "call_1", "osPid": 4242.0, "cpuPercent": 1.5, "rssKb": 51200.0}
	respond("thread/backgroundTerminals/list", map[string]any{"data": []any{server}})
	m.pollTerminals(id)
	count, tasks := backgroundNow(m, r)
	if count != 1 || len(tasks) != 1 || tasks[0].Kind != "terminal" || tasks[0].PID != 4242 || tasks[0].RSSKb != 51200 {
		t.Fatalf("listed: %d %+v", count, tasks)
	}
	respond("thread/backgroundTerminals/terminate", map[string]any{})
	if err := m.StopBackground(id, "terminal:17"); err != nil {
		t.Fatal(err)
	}
	if params := obj(sentMethod(p, "thread/backgroundTerminals/terminate")["params"]); params["processId"] != "17" || params["threadId"] != "th" {
		t.Fatalf("terminate: %v", params)
	}
	respond("thread/backgroundTerminals/clean", map[string]any{})
	if err := m.StopBackground(id, ""); err != nil {
		t.Fatal(err)
	}
	if sentMethod(p, "thread/backgroundTerminals/clean") == nil {
		t.Fatal("stop all sends clean")
	}
	respond("thread/backgroundTerminals/list", map[string]any{"data": []any{}})
	m.pollTerminals(id)
	if count, tasks = backgroundNow(m, r); count != 0 || tasks[0].Status != "ended" {
		t.Fatalf("gone: %d %+v", count, tasks)
	}
	if got := notices(t, m, id); len(got) == 0 || !strings.Contains(got[len(got)-1], "Background terminal ended") {
		t.Fatalf("notice: %q", got)
	}
}

// extraProcess is an engine that started other processes.
type extraProcess struct {
	fakeProcess
	pids []int
	root int
}

func (p *extraProcess) ExtraPIDs() []int { return p.pids }

// RootPID is the engine's main process; by default one none of them
// descends from (they outlived what started them).
func (p *extraProcess) RootPID() int {
	if p.root != 0 {
		return p.root
	}
	return 1 << 30
}
func (p *extraProcess) Busy() bool { return len(p.pids) > 0 }

func TestOtherProcessesAndSleepingAnyway(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("command lines come from /proc")
	}
	server := exec.Command("sleep", "30")
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = server.Wait(); close(done) }()
	t.Cleanup(func() { _ = server.Process.Kill() })

	before := settled
	settled = 0
	t.Cleanup(func() { settled = before })
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	p := &extraProcess{pids: []int{server.Process.Pid}}
	m, _ := NewManager(s, func(Session, func(map[string]any), func(error)) (Process, error) { return p, nil }, time.Millisecond)
	created, _ := m.Create("codex", t.TempDir(), "server")
	_ = m.Start(created.ID)
	r := m.sessions[created.ID]
	m.receive(created.ID, r.generation, map[string]any{"id": "init", "result": map[string]any{}})
	m.receive(created.ID, r.generation, map[string]any{"id": "thread", "result": map[string]any{"thread": map[string]any{"id": "th"}}})
	id := r.session.ID

	// What still descends from the engine is its own (a helper, an MCP
	// server, a task it reports): not listed.
	if got := otherProcesses(&extraProcess{pids: p.pids, root: os.Getpid()}); len(got) != 0 {
		t.Fatalf("the engine's own child: %+v", got)
	}
	m.refreshAllOthers(id)
	count, tasks := backgroundNow(m, r)
	if count != 1 || len(tasks) != 1 || tasks[0].Kind != "process" || tasks[0].Description != "sleep 30" {
		t.Fatalf("others: %d %+v", count, tasks)
	}
	// An engine with background work does not just go to sleep.
	if err := m.Stop(id, true); err == nil || !strings.Contains(err.Error(), "background work") {
		t.Fatalf("sleep: %v", err)
	}
	if err := m.StopBackground(id, tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the process is still running")
	}
	if err := m.SleepStoppingBackground(id); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	status, background := r.session.Status, r.session.Background
	m.mu.Unlock()
	if status != "sleeping" || background != 0 {
		t.Fatalf("asleep: %s %d", status, background)
	}
}

func TestCleanCommand(t *testing.T) {
	wrapped := `/usr/bin/zsh -c source /home/u/.claude/shell-snapshots/s.sh 2>/dev/null || true && eval 'python3 -m http.server 8765 --bind '"'"'127.0.0.1'"'"'' < /dev/null && pwd -P >| /tmp/cwd`
	if got := cleanCommand(wrapped); got != "python3 -m http.server 8765 --bind '127.0.0.1'" {
		t.Fatalf("wrapped: %q", got)
	}
	if got := cleanCommand("node server.js"); got != "node server.js" {
		t.Fatalf("plain: %q", got)
	}
}

func waitFor(ok func() bool) bool {
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return true
		}
	}
	return false
}
