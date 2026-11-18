//go:build windows

package workbench

import (
	"context"
	"os"
	"os/exec"
	"unsafe"

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

// defaultShell is PowerShell when there is one, else the command processor.
func defaultShell() []string {
	for _, sh := range []string{"pwsh.exe", "powershell.exe"} {
		if path, err := exec.LookPath(sh); err == nil {
			return []string{path, "-NoLogo"}
		}
	}
	if sh := os.Getenv("ComSpec"); sh != "" {
		return []string{sh}
	}
	return []string{"cmd.exe"}
}

// winShell is a shell on a Windows pseudo console (ConPTY), held in a Job
// Object so a close takes everything it started with it, and so it ends if
// the workbench itself dies.
type winShell struct {
	cpty *conpty.ConPty
	job  windows.Handle
	done chan struct{}
	code int
}

func startShell(argv []string, dir string, env []string, cols, rows int) (shellPty, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, conpty.ErrConPtyUnsupported
	}
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
	cpty, err := conpty.Start(windows.ComposeCommandLine(argv), conpty.ConPtyDimensions(cols, rows), conpty.ConPtyWorkDir(dir), conpty.ConPtyEnv(env))
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cpty.Pid()))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, h)
		windows.CloseHandle(h)
	}
	if err != nil {
		cpty.Close()
		windows.CloseHandle(job)
		return nil, err
	}
	s := &winShell{cpty: cpty, job: job, done: make(chan struct{})}
	go func() {
		code, err := cpty.Wait(context.Background())
		s.code = int(code)
		if err != nil {
			s.code = -1
		}
		close(s.done)
	}()
	return s, nil
}

func (s *winShell) Read(p []byte) (int, error)  { return s.cpty.Read(p) }
func (s *winShell) Write(p []byte) (int, error) { return s.cpty.Write(p) }
func (s *winShell) Done() <-chan struct{}       { return s.done }
func (s *winShell) Code() int                   { return s.code }
func (s *winShell) Resize(cols, rows int) error { return s.cpty.Resize(cols, rows) }

// Kill ends the job: everything the shell started.
func (s *winShell) Kill() {
	_ = windows.TerminateJobObject(s.job, 1)
	<-s.done
}

func (s *winShell) Close() error {
	err := s.cpty.Close()
	windows.CloseHandle(s.job)
	return err
}
