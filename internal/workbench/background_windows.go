package workbench

import (
	"errors"
	"os"
)

// terminate ends processes.
func terminate(pids []int) error {
	var errs []error
	for _, pid := range pids {
		if p, err := os.FindProcess(pid); err == nil {
			if err := p.Kill(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
