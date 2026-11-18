//go:build !windows

package workbench

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// defaultShell is the user's login shell.
func defaultShell() []string {
	if sh := os.Getenv("SHELL"); sh != "" {
		if path, err := exec.LookPath(sh); err == nil {
			return []string{path, "-l"}
		}
	}
	for _, sh := range []string{"/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(sh); err == nil {
			return []string{sh, "-l"}
		}
	}
	return []string{"sh", "-l"}
}

// unixShell is a shell on a pseudo-terminal (creack/pty): its own session,
// with the terminal as its controlling one.
type unixShell struct {
	cmd  *exec.Cmd
	tty  *os.File
	done chan struct{}
	code int
}

func startShell(argv []string, dir string, env []string, cols, rows int) (shellPty, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	s := &unixShell{cmd: cmd, tty: tty, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		s.code = 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			s.code = exit.ExitCode()
		} else if err != nil {
			s.code = -1
		}
		close(s.done)
	}()
	return s, nil
}

func (s *unixShell) Read(p []byte) (int, error)  { return s.tty.Read(p) }
func (s *unixShell) Write(p []byte) (int, error) { return s.tty.Write(p) }
func (s *unixShell) Done() <-chan struct{}       { return s.done }
func (s *unixShell) Code() int                   { return s.code }
func (s *unixShell) Close() error                { return s.tty.Close() }

func (s *unixShell) Resize(cols, rows int) error {
	return pty.Setsize(s.tty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Kill hangs up on everything in the terminal's session, as closing a
// terminal window does, then kills what is still there: programs that ignore
// the hangup (nohup) and jobs the shell left behind. The session outlives the
// shell and holds every job group; a program that left it on purpose
// (setsid, a daemon) is left alone, as a real terminal would.
func (s *unixShell) Kill() {
	sid := s.cmd.Process.Pid
	signal := func(sig syscall.Signal) int {
		members := sessionMembers(sid)
		_ = syscall.Kill(-sid, sig)
		for _, p := range members {
			_ = syscall.Kill(p, sig)
		}
		return len(members)
	}
	if signal(syscall.SIGHUP) == 0 {
		select {
		case <-s.done:
			return
		default:
		}
	}
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
	}
	// A moment for those that do exit on a hangup.
	for i := 0; i < 10 && len(sessionMembers(sid)) > 0; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	signal(syscall.SIGKILL)
	<-s.done
}

// sessionMembers lists the processes in session sid.
func sessionMembers(sid int) []int {
	table, err := processTable()
	if err != nil {
		return nil
	}
	out := []int{}
	for p := range table {
		if got, err := unix.Getsid(p); err == nil && got == sid {
			out = append(out, p)
		}
	}
	return out
}
