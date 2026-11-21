//go:build !linux && !darwin && !windows

package workbench

import "errors"

func holdAwake(kind string) (awakeHold, string, error) {
	return nil, "", errors.New("not supported on this system")
}
