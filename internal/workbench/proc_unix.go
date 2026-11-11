//go:build !windows

package workbench

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// rawArgs passes args to the program as written (Windows only).
func rawArgs(*exec.Cmd, []string) {}

// shellCommand runs a user command line in a login shell.
func shellCommand(command string) (*exec.Cmd, error) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		return nil, err
	}
	return exec.Command(sh, "-lc", command), nil
}

// procGroup is a started child and everything it spawns: its own process
// group, plus descendants that moved to another group.
type procGroup struct{ cmd *exec.Cmd }

// startGroup starts cmd as the leader of a new process group.
func startGroup(cmd *exec.Cmd) (*procGroup, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &procGroup{cmd: cmd}, nil
}

// PIDs lists the leader and its living descendants, or nil when the
// process table cannot be read.
func (g *procGroup) PIDs() map[int]bool {
	return descendants(g.cmd.Process.Pid)
}

func (g *procGroup) signal(sig syscall.Signal, pids map[int]bool) {
	_ = syscall.Kill(-g.cmd.Process.Pid, sig)
	for pid := range pids {
		if pid != g.cmd.Process.Pid {
			_ = syscall.Kill(pid, sig)
		}
	}
}

// Terminate asks the group to exit; pids is a snapshot taken while the tree
// was intact (orphans lose their parent link).
func (g *procGroup) Terminate(pids map[int]bool) { g.signal(syscall.SIGTERM, pids) }

// Kill ends the group and the snapshot at once.
func (g *procGroup) Kill(pids map[int]bool) { g.signal(syscall.SIGKILL, pids) }

// Release frees what the group holds once the leader has been waited for.
func (g *procGroup) Release() {}

// descendants walks the process table from root.
func descendants(root int) map[int]bool {
	parents, err := processTable()
	if err != nil {
		return nil
	}
	children := map[int][]int{}
	for pid, ppid := range parents {
		children[ppid] = append(children[ppid], pid)
	}
	out := map[int]bool{}
	if _, ok := parents[root]; !ok {
		return out
	}
	queue := []int{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if out[pid] {
			continue
		}
		out[pid] = true
		queue = append(queue, children[pid]...)
	}
	return out
}

// fileLocked is whether another process holds an exclusive flock on path.
func fileLocked(path string) bool {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// processAlive is whether pid names a running process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
