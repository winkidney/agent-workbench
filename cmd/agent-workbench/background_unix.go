//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

const binaryName = "agent-workbench"

// companions are other files install copies next to the binary.
var companions []string

// backgroundExe is the binary to run without a terminal.
func backgroundExe(exe string) string { return exe }

// startBackground starts cmd in its own session, detached from the
// terminal, so it outlives it.
func startBackground(build func() *exec.Cmd) (*exec.Cmd, error) {
	cmd := build()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd, cmd.Start()
}
