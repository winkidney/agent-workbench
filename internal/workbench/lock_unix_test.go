//go:build !windows

package workbench

import (
	"os"
	"syscall"
)

// holdLock locks f as Codex does and returns the release.
func holdLock(f *os.File) func() {
	syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
}
