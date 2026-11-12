package workbench

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	goruntime "runtime"
	"strings"
	"sync"
	"time"
)

// Signing a CLI in from the page, for a home the user cannot reach with a
// terminal (a phone, through a tunnel). The CLI's own login runs here and
// only what needs the user goes to the page: Codex's device link and
// one-time code, or Claude's sign-in link and a box for the code its page
// shows. Credentials are written by the CLI into its home; the workbench
// never reads, keeps or logs them, and passes a pasted code straight to the
// CLI's input.

// loginState is a login as the page sees it.
type loginState struct {
	// starting, waiting (for the user), verifying (a code was given),
	// done, failed, cancelled or expired.
	State string `json:"state"`
	URL   string `json:"url,omitempty"`
	// Codex: the one-time code to enter at URL.
	Code string `json:"code,omitempty"`
	// Claude: the page shows a code to paste back here.
	NeedsCode bool   `json:"needsCode,omitempty"`
	Message   string `json:"message,omitempty"`
	Started   string `json:"started"`
}

type loginFlow struct {
	mu     sync.Mutex
	state  loginState
	cmd    *exec.Cmd
	group  *procGroup
	stdin  io.WriteCloser
	cancel context.CancelFunc
	// The output so far, for finding the link and the code.
	seen strings.Builder
	// The last line the CLI printed, for a failure.
	last string
	// Closed once the CLI has exited.
	done chan struct{}
}

// loginTimeout ends a sign-in left waiting; shortened in tests.
var loginTimeout = 10 * time.Minute

var (
	ansi       = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	firstURL   = regexp.MustCompile(`https://[^\s"'<>]+`)
	deviceCode = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4,6}\b`)
)

func stripANSI(s string) string { return ansi.ReplaceAllString(s, "") }

// loginArgs are the CLI's own login command. Claude: claudeai (a Claude
// subscription, the default), console (API billing) or sso.
func loginArgs(agent, method string) ([]string, error) {
	if agent == "codex" {
		return []string{"login", "--device-auth"}, nil
	}
	switch method {
	case "", "claudeai":
		return []string{"auth", "login", "--claudeai"}, nil
	case "console", "sso":
		return []string{"auth", "login", "--" + method}, nil
	}
	return nil, errors.New("unknown sign-in method")
}

// loginEnv keeps the CLI from opening a browser on this computer: the page
// shows the link wherever the user is.
func loginEnv(run agentRun) []string {
	env := run.Env
	if env == nil {
		env = os.Environ()
	}
	browser := "true"
	if path, err := exec.LookPath("true"); err == nil {
		browser = path
	}
	env = withEnv(env, "BROWSER", browser)
	if goruntime.GOOS == "linux" {
		out := env[:0:0]
		for _, e := range env {
			if k, _, _ := strings.Cut(e, "="); k != "DISPLAY" && k != "WAYLAND_DISPLAY" {
				out = append(out, e)
			}
		}
		env = out
	}
	return env
}

// StartLogin starts a login for agent with the program and home in use,
// ending one already under way.
func (m *Manager) StartLogin(agent, method string) (loginState, error) {
	args, err := loginArgs(agent, method)
	if err != nil {
		return loginState{}, err
	}
	run, err := m.cli(agent)
	if err != nil {
		return loginState{}, err
	}
	home, _ := m.agentHome(agent)
	if err := checkHome(home); err != nil {
		return loginState{}, err
	}
	m.cancelLogin(agent)
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	cmd := run.command(args...)
	cmd.Env = loginEnv(run)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return loginState{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return loginState{}, err
	}
	cmd.Stderr = cmd.Stdout
	group, err := startGroup(cmd)
	if err != nil {
		cancel()
		return loginState{}, err
	}
	f := &loginFlow{cmd: cmd, group: group, stdin: stdin, cancel: cancel, done: make(chan struct{}), state: loginState{State: "starting", Started: time.Now().UTC().Format(time.RFC3339)}}
	m.loginsMu.Lock()
	if m.logins == nil {
		m.logins = map[string]*loginFlow{}
	}
	m.logins[agent] = f
	m.loginsMu.Unlock()
	unwatch := group.killOn(ctx)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				f.read(agent, string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
		err := cmd.Wait()
		unwatch()
		group.Release()
		f.finish(err, ctx.Err())
		close(f.done)
		cancel()
		m.forgetSignIns()
		m.changes.Broadcast()
	}()
	return f.snapshot(), nil
}

// read takes in what the CLI printed.
func (f *loginFlow) read(agent, chunk string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen.Len() < 64<<10 {
		f.seen.WriteString(stripANSI(chunk))
	}
	text := f.seen.String()
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			f.last = line
		}
	}
	if f.state.State != "starting" {
		return
	}
	if agent == "codex" {
		if u := firstURL.FindString(text); u != "" {
			f.state.URL = u
		}
		if i := strings.Index(text, "one-time code"); i >= 0 {
			f.state.Code = deviceCode.FindString(text[i:])
		}
		if f.state.URL != "" && f.state.Code != "" {
			f.state.State = "waiting"
		}
		return
	}
	// Claude names its sign-in page, then asks for the code that page
	// shows.
	for _, u := range firstURL.FindAllString(text, -1) {
		if strings.Contains(u, "oauth/authorize") {
			f.state.URL = u
		}
	}
	if f.state.URL != "" && strings.Contains(text, "Paste code here") {
		f.state.State, f.state.NeedsCode = "waiting", true
	}
}

func (f *loginFlow) finish(err, timeout error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case f.state.State == "cancelled":
	case errors.Is(timeout, context.DeadlineExceeded):
		f.state.State, f.state.Message = "expired", "The sign-in took longer than 10 minutes."
	case err == nil:
		f.state.State, f.state.Message = "done", ""
	default:
		f.state.State, f.state.Message = "failed", clip(f.last, 300)
		if f.state.Message == "" {
			f.state.Message = err.Error()
		}
	}
	f.state.NeedsCode = false
	f.stdin.Close()
}

func (f *loginFlow) snapshot() loginState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (m *Manager) loginState(agent string) *loginState {
	m.loginsMu.Lock()
	f := m.logins[agent]
	m.loginsMu.Unlock()
	if f == nil {
		return nil
	}
	s := f.snapshot()
	return &s
}

// LoginCode gives the CLI the code its sign-in page showed.
func (m *Manager) LoginCode(agent, code string) (loginState, error) {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 4096 || strings.ContainsAny(code, "\r\n") {
		return loginState{}, errors.New("paste the code the sign-in page shows")
	}
	m.loginsMu.Lock()
	f := m.logins[agent]
	m.loginsMu.Unlock()
	if f == nil {
		return loginState{}, errors.New("no sign-in under way")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state.State != "waiting" || !f.state.NeedsCode {
		return f.state, errors.New("the sign-in is not waiting for a code")
	}
	if _, err := io.WriteString(f.stdin, code+"\n"); err != nil {
		return f.state, err
	}
	f.state.State, f.state.NeedsCode = "verifying", false
	return f.state, nil
}

// cancelLogin ends agent's login, if one is under way.
func (m *Manager) cancelLogin(agent string) {
	m.loginsMu.Lock()
	f := m.logins[agent]
	delete(m.logins, agent)
	m.loginsMu.Unlock()
	if f == nil {
		return
	}
	f.mu.Lock()
	running := f.state.State == "starting" || f.state.State == "waiting" || f.state.State == "verifying"
	if running {
		f.state.State = "cancelled"
	}
	f.mu.Unlock()
	if running {
		f.group.Kill(nil)
		f.cancel()
	}
}

// CancelLogin ends agent's login and forgets it.
func (m *Manager) CancelLogin(agent string) { m.cancelLogin(agent) }
