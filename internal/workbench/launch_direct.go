package workbench

import (
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"
)

// directProcess is an engine run as a child of the workbench (macOS,
// Windows, Linux without a systemd user manager). Everything it spawns stays
// in one group, a process group or a Job Object, which plays the part of the
// systemd unit: Stop and the engine's exit end the whole group.
type directProcess struct {
	mu       sync.Mutex
	stdin    io.WriteCloser
	group    *procGroup
	done     chan struct{}
	baseline map[int]bool
}

func DirectLaunch(s Session, onMessage func(map[string]any), onExit func(error)) (Process, error) {
	binary, args, err := agentCommand(s)
	if err != nil {
		return nil, err
	}
	return startDirect(exec.Command(binary, args...), s.Cwd, onMessage, onExit)
}

func startDirect(cmd *exec.Cmd, cwd string, onMessage func(map[string]any), onExit func(error)) (*directProcess, error) {
	cmd.Dir = cwd
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	group, err := startGroup(cmd)
	if err != nil {
		return nil, err
	}
	p := &directProcess{stdin: in, group: group, done: make(chan struct{})}
	abort := func() { group.Kill(group.PIDs()) }
	exited := func() {
		// Like a unit stopping when its main process exits: what the
		// engine left running goes with it.
		group.Kill(nil)
		group.Release()
		close(p.done)
	}
	pump(cmd, out, stderr, onMessage, onExit, abort, exited)
	return p, nil
}

func (p *directProcess) Input(v any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return json.NewEncoder(p.stdin).Encode(v)
}

// Stop asks the engine and its children to exit, and kills them after ten
// seconds, as systemd does for a unit.
func (p *directProcess) Stop() error {
	if closed(p.done) {
		return nil
	}
	pids := p.group.PIDs()
	p.mu.Lock()
	_ = p.stdin.Close()
	p.mu.Unlock()
	p.group.Terminate(pids)
	if waitClosed(p.done, 10*time.Second) {
		return nil
	}
	p.group.Kill(pids)
	if waitClosed(p.done, 5*time.Second) {
		return nil
	}
	return errors.New("engine did not exit")
}

func (p *directProcess) MarkReady() { p.baseline = p.group.PIDs() }

// Busy is whether the engine has started processes since it became ready
// (a tool, a shell, a dev server), as for a unit's cgroup.
func (p *directProcess) Busy() bool {
	now := p.group.PIDs()
	if now == nil || p.baseline == nil {
		return true
	}
	for pid := range now {
		if !p.baseline[pid] {
			return true
		}
	}
	return false
}

func (p *directProcess) Owns(pid int) bool { return p.group.PIDs()[pid] }

func closed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

func waitClosed(c chan struct{}, d time.Duration) bool {
	select {
	case <-c:
		return true
	case <-time.After(d):
		return false
	}
}
