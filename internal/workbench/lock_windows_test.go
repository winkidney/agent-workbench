package workbench

import (
	"os"

	"golang.org/x/sys/windows"
)

// holdLock locks f as Codex does and returns the release.
func holdLock(f *os.File) func() {
	var ol windows.Overlapped
	h := windows.Handle(f.Fd())
	windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, ^uint32(0), ^uint32(0), &ol)
	return func() { windows.UnlockFileEx(h, 0, ^uint32(0), ^uint32(0), &ol) }
}
