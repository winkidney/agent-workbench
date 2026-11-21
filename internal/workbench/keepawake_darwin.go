//go:build darwin

package workbench

import (
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
)

// macOS: caffeinate holds the assertion while it runs; -w makes it end with
// the workbench's process, so a crash never leaves it behind. -i stops idle
// system sleep; -d stops the display from sleeping, which is what starts the
// screen saver and the lock.
func holdAwake(kind string) (awakeHold, string, error) {
	flag := "-i"
	if kind == awakeLock {
		flag = "-d"
	}
	cmd := exec.Command("caffeinate", flag, "-w", strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	h := &darwinHold{cmd: cmd}
	go func() {
		_ = cmd.Wait()
		h.done.Store(true)
	}()
	return h, "caffeinate", nil
}

type darwinHold struct {
	cmd  *exec.Cmd
	done atomic.Bool
}

func (h *darwinHold) Release()    { _ = h.cmd.Process.Kill() }
func (h *darwinHold) Alive() bool { return !h.done.Load() }
