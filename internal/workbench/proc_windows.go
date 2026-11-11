package workbench

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// UnderSystemd is false: Windows has no systemd.
func UnderSystemd() bool { return false }

// hideWindow keeps a console program from opening a window, so a
// workbench started without one (autostart) does not flash windows. Every
// child the workbench starts on Windows goes through it.
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
	cmd.SysProcAttr.HideWindow = true
}

// rawArgs passes args to the program as written, for programs such as
// explorer.exe that do not follow the argv quoting rules.
func rawArgs(cmd *exec.Cmd, args []string) {
	hideWindow(cmd)
	cmd.SysProcAttr.CmdLine = strings.Join(append([]string{syscall.EscapeArg(cmd.Path)}, args...), " ")
}

// shellCommand runs a user command line with cmd.exe. The line is passed
// as is (cmd.exe does not follow the argv quoting rules).
func shellCommand(command string) (*exec.Cmd, error) {
	shell := os.Getenv("ComSpec")
	if shell == "" {
		shell = "cmd.exe"
	}
	cmd := exec.Command(shell)
	hideWindow(cmd)
	cmd.SysProcAttr.CmdLine = `/d /s /c "` + command + `"`
	return cmd, nil
}

// procGroup is a started child and everything it spawns, held in a Job
// Object. The job kills its processes when its last handle closes, so they
// also end if the workbench itself dies.
type procGroup struct {
	cmd *exec.Cmd
	job windows.Handle
}

func startGroup(cmd *exec.Cmd) (*procGroup, error) {
	hideWindow(cmd)
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, h)
		windows.CloseHandle(h)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		windows.CloseHandle(job)
		return nil, err
	}
	return &procGroup{cmd: cmd, job: job}, nil
}

// jobProcessList is JOBOBJECT_BASIC_PROCESS_ID_LIST with room for 1024.
type jobProcessList struct {
	Assigned, Listed uint32
	IDs              [1024]uintptr
}

// PIDs lists every process in the job, or nil when it cannot be read.
func (g *procGroup) PIDs() map[int]bool {
	var list jobProcessList
	if err := windows.QueryInformationJobObject(g.job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil); err != nil {
		return nil
	}
	out := map[int]bool{}
	for _, id := range list.IDs[:list.Listed] {
		out[int(id)] = true
	}
	return out
}

// Terminate ends the job: Windows has no polite stop for console programs
// without a console.
func (g *procGroup) Terminate(map[int]bool) { _ = windows.TerminateJobObject(g.job, 1) }
func (g *procGroup) Kill(map[int]bool)      { _ = windows.TerminateJobObject(g.job, 1) }

// Release closes the job handle once the leader has been waited for (which
// also ends anything still in it).
func (g *procGroup) Release() { _ = windows.CloseHandle(g.job) }

// fileLocked is whether another process holds a lock on path (Codex locks
// its thread files with LockFileEx here).
func fileLocked(path string) bool {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	defer f.Close()
	var ol windows.Overlapped
	h := windows.Handle(f.Fd())
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, ^uint32(0), ^uint32(0), &ol); err != nil {
		return true
	}
	_ = windows.UnlockFileEx(h, 0, ^uint32(0), ^uint32(0), &ol)
	return false
}

const stillActive = 259

// processAlive is whether pid names a running process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	var code uint32
	return windows.GetExitCodeProcess(h, &code) == nil && code == stillActive
}

// processName is the executable path of pid, or "".
func processName(pid int) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}
