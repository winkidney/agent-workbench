//go:build windows

package workbench

import (
	"errors"
	goruntime "runtime"

	"golang.org/x/sys/windows"
)

// Windows: SetThreadExecutionState belongs to the thread that calls it, so
// each hold keeps a goroutine locked to its own OS thread. When the hold ends
// the goroutine returns still locked, and Go ends that thread, with its state.
const (
	esContinuous      = 0x80000000
	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002
)

var setThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

func holdAwake(kind string) (awakeHold, string, error) {
	flags := uintptr(esContinuous | esSystemRequired)
	if kind == awakeLock {
		flags |= esDisplayRequired
	}
	h := &windowsHold{stop: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		goruntime.LockOSThread()
		if r, _, err := setThreadExecutionState.Call(flags); r == 0 {
			started <- errors.New("SetThreadExecutionState: " + err.Error())
			return
		}
		started <- nil
		<-h.stop
		_, _, _ = setThreadExecutionState.Call(esContinuous)
	}()
	if err := <-started; err != nil {
		return nil, "", err
	}
	return h, "Windows power requests", nil
}

type windowsHold struct{ stop chan struct{} }

func (h *windowsHold) Release()    { close(h.stop) }
func (h *windowsHold) Alive() bool { return true }
