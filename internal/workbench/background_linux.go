package workbench

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// processParent is the parent of pid, 0 when unknown.
func processParent(pid int) int {
	fields := stat(pid)
	if len(fields) < 2 {
		return 0
	}
	ppid, _ := strconv.Atoi(fields[1])
	return ppid
}

// processCommand is pid's command line, shortened.
func processCommand(pid int) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return ""
	}
	return cleanCommand(strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " ")))
}

// processAge is how long pid has been running (Linux counts start times in
// clock ticks since boot, 100 a second).
func processAge(pid int) (time.Duration, bool) {
	fields := stat(pid)
	if len(fields) <= 19 {
		return 0, false
	}
	start, err := strconv.ParseFloat(fields[19], 64)
	if err != nil {
		return 0, false
	}
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, false
	}
	up, err := strconv.ParseFloat(strings.Fields(string(b))[0], 64)
	if err != nil {
		return 0, false
	}
	return time.Duration((up - start/100) * float64(time.Second)), true
}
