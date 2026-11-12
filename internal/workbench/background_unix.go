//go:build !windows

package workbench

import (
	"errors"
	"syscall"
)

// terminate asks processes to exit.
func terminate(pids []int) error {
	var errs []error
	for _, pid := range pids {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
