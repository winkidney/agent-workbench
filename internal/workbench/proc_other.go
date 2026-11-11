//go:build !linux

package workbench

import (
	"path/filepath"
	"strings"
)

// processMatches is whether pid is a live Claude process. Claude's recorded
// start time is only comparable on Linux, so elsewhere the name guards
// against PID reuse: the native binary is "claude", an npm install "node".
func processMatches(pid int, _ string) bool {
	if !processAlive(pid) {
		return false
	}
	name := strings.ToLower(filepath.Base(processName(pid)))
	for _, n := range claudeProcessNames {
		if strings.Contains(name, n) {
			return true
		}
	}
	return false
}
