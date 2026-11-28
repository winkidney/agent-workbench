package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// detachUpdate starts the update outside the service. Stopping a systemd
// unit ends every process in it, a new session or not, so under systemd
// the update runs as a unit of its own.
func detachUpdate(c config, program string, args []string) error {
	if os.Getenv("INVOCATION_ID") != "" {
		if run, err := exec.LookPath("systemd-run"); err == nil {
			unit := fmt.Sprintf("%s-update-%d", c.name, time.Now().Unix())
			cmd := exec.Command(run, append([]string{"--user", "--collect", "--quiet", "--unit", unit,
				"--setenv=PATH=" + os.Getenv("PATH"), "--setenv=HOME=" + os.Getenv("HOME"), program}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("systemd-run: %v: %s", err, out)
			}
			return nil
		}
	}
	_, err := startBackground(func() *exec.Cmd { return exec.Command(program, args...) })
	return err
}
