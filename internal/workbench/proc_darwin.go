package workbench

import (
	"bytes"

	"golang.org/x/sys/unix"
)

// UnderSystemd is false: macOS has no systemd.
func UnderSystemd() bool { return false }

// processTable maps each running process to its parent.
func processTable() (map[int]int, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	out := make(map[int]int, len(procs))
	for _, p := range procs {
		out[int(p.Proc.P_pid)] = int(p.Eproc.Ppid)
	}
	return out, nil
}

// processName is the short command name of pid, or "".
func processName(pid int) string {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || p.Proc.P_pid != int32(pid) {
		return ""
	}
	name := p.Proc.P_comm[:]
	if i := bytes.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	return string(name)
}
