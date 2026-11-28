//go:build !linux

package main

import "os/exec"

// detachUpdate starts the update in a session of its own (macOS: launchd
// ends the service's process group, not this one) or, on Windows, out of
// the service's job, without a console.
func detachUpdate(c config, program string, args []string) error {
	_, err := startBackground(func() *exec.Cmd { return exec.Command(backgroundExe(program), args...) })
	return err
}
