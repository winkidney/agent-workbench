package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

const binaryName = "agent-workbench.exe"

// companions are other files install copies next to the binary: the same
// program built for the GUI subsystem, which runs without a console window.
var companions = []string{"agent-workbenchw.exe"}

// backgroundExe is the windowless twin next to exe, when there is one.
func backgroundExe(exe string) string {
	w := filepath.Join(filepath.Dir(exe), companions[0])
	if exists(w) {
		return w
	}
	return exe
}

// startBackground starts cmd without a console and outside the terminal's
// job, so closing the terminal does not end it. Some terminals forbid
// leaving their job; then it stays in it.
func startBackground(build func() *exec.Cmd) (*exec.Cmd, error) {
	flags := uint32(windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP)
	cmd := build()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags | windows.CREATE_BREAKAWAY_FROM_JOB}
	err := cmd.Start()
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		cmd = build()
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		err = cmd.Start()
	}
	return cmd, err
}
