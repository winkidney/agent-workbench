package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// UnderSystemd is whether this process is the workbench's own systemd
// service, so engines and previews can be units bound to it.
func UnderSystemd() bool {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasSuffix(line, "/"+Service) {
			return true
		}
	}
	return false
}

// stat reads the fields after the command name in /proc/<pid>/stat
// (fields[0] is the state, fields[1] the parent, fields[19] the start time).
func stat(pid int) []string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return nil
	}
	s := string(data)
	i := strings.LastIndex(s, ")")
	if i < 0 || i+2 > len(s) {
		return nil
	}
	return strings.Fields(s[i+2:])
}

// processTable maps each running process to its parent.
func processTable() (map[int]int, error) {
	dirs, err := filepath.Glob("/proc/[0-9]*")
	if err != nil {
		return nil, err
	}
	out := map[int]int{}
	for _, dir := range dirs {
		pid, err := strconv.Atoi(filepath.Base(dir))
		if err != nil {
			continue
		}
		fields := stat(pid)
		if len(fields) < 2 || fields[0] == "Z" {
			continue
		}
		if ppid, err := strconv.Atoi(fields[1]); err == nil {
			out[pid] = ppid
		}
	}
	return out, nil
}

// processMatches is whether pid is still the process that registered with
// start time procStart (as Claude records it), guarding against PID reuse.
func processMatches(pid int, procStart string) bool {
	fields := stat(pid)
	if len(fields) <= 19 {
		return false
	}
	if _, err := strconv.ParseUint(procStart, 10, 64); err != nil {
		return true
	}
	return procStart == fields[19]
}
