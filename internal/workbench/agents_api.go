package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The Agents settings: what each agent runs and where it keeps its data,
// checked before it is saved (agents_setup.go has the rules).

type programView struct {
	// Chosen in Settings; empty: found on PATH.
	Value      string   `json:"value,omitempty"`
	Source     string   `json:"source"`
	Path       string   `json:"path,omitempty"`
	Version    string   `json:"version,omitempty"`
	Error      string   `json:"error,omitempty"`
	Candidates []string `json:"candidates"`
}

type homeView struct {
	// Chosen in Settings; empty: the environment's, or the default.
	Value  string `json:"value,omitempty"`
	Source string `json:"source"`
	Path   string `json:"path"`
	// What the environment names, and the CLI's own default.
	Env          string `json:"env,omitempty"`
	Default      string `json:"default"`
	Exists       bool   `json:"exists"`
	Error        string `json:"error,omitempty"`
	SignIn       signIn `json:"signIn"`
	LoginCommand string `json:"loginCommand"`
}

type agentView struct {
	Program programView `json:"program"`
	Home    homeView    `json:"home"`
	// Sessions at work with this agent: its home cannot change meanwhile.
	Busy  int         `json:"busy"`
	Login *loginState `json:"login,omitempty"`
}

// signIn is what the CLI says of its own login in a home.
type signIn struct {
	Checked  bool   `json:"checked"`
	SignedIn bool   `json:"signedIn"`
	Method   string `json:"method,omitempty"`
	Account  string `json:"account,omitempty"`
	Error    string `json:"error,omitempty"`
}

// conflictError is a change refused for the state things are in (409).
type conflictError struct{ msg string }

func (e conflictError) Error() string { return e.msg }

var agentNames = map[string]string{"claude": "Claude Code", "codex": "Codex"}

// versionLooksRight is whether --version output is the agent's CLI.
func versionLooksRight(agent, version string) bool {
	if agent == "codex" {
		return strings.HasPrefix(version, "codex-cli")
	}
	return strings.Contains(version, "(Claude Code)")
}

// checkProgram validates a chosen program: an executable file that answers
// --version as the agent's CLI.
func checkProgram(agent, path string) (version string, err error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("use an absolute path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", errors.New("no such file")
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a file")
	}
	if !executable(info) {
		return "", errors.New("not executable")
	}
	version, failure := cliVersion(agent+":"+path, path)
	if failure != "" && version == "" {
		return "", fmt.Errorf("did not answer --version: %s", failure)
	}
	if !versionLooksRight(agent, version) {
		if version == "" {
			version = "no output"
		}
		return version, fmt.Errorf("does not look like %s (--version: %s)", agentNames[agent], clip(version, 80))
	}
	return version, nil
}

// checkHome validates a chosen home: an existing folder. It is not created
// here; the CLI's own login fills it.
func checkHome(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("use an absolute path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return errors.New("no such folder; create it first")
	}
	if !info.IsDir() {
		return errors.New("not a folder")
	}
	return nil
}

// candidates are the agent's programs on PATH, and what they link to (a
// pinned version).
func candidates(agent string) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] && len(out) < 8 {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, agent+exeSuffix())
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() || !executable(info) {
			continue
		}
		add(p)
		if real, err := filepath.EvalSymlinks(p); err == nil {
			add(real)
		}
	}
	return out
}

// shellQuote quotes a word for a POSIX shell when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r == '~' || r == ':' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// loginCommand is how to sign in from a terminal on this computer.
func loginCommand(agent string, s AgentSetup) string {
	program := agent
	if s.Path != "" {
		program = shellQuote(s.Path)
	}
	command := program + " auth login"
	if agent == "codex" {
		command = program + " login"
	}
	if s.Home != "" {
		return homeVar[agent] + "=" + shellQuote(s.Home) + " " + command
	}
	return command
}

// signIns caches what the CLIs say of their logins, briefly: asking runs
// them.
type signInCache struct {
	mu   sync.Mutex
	byKy map[string]signInEntry
}

type signInEntry struct {
	at time.Time
	v  signIn
}

func (m *Manager) forgetSignIns() {
	m.signIns.mu.Lock()
	m.signIns.byKy = nil
	m.signIns.mu.Unlock()
}

// signInOf asks the CLI whether it is signed in with this home.
func (m *Manager) signInOf(agent string, s AgentSetup) signIn {
	run, err := runFor(agent, s)
	if err != nil {
		return signIn{Error: "program not found"}
	}
	key := agent + "|" + run.Binary + "|" + s.Home
	m.signIns.mu.Lock()
	if e, ok := m.signIns.byKy[key]; ok && time.Since(e.at) < 20*time.Second {
		m.signIns.mu.Unlock()
		return e.v
	}
	m.signIns.mu.Unlock()
	v := askSignIn(agent, run)
	m.signIns.mu.Lock()
	if m.signIns.byKy == nil {
		m.signIns.byKy = map[string]signInEntry{}
	}
	m.signIns.byKy[key] = signInEntry{time.Now(), v}
	m.signIns.mu.Unlock()
	return v
}

func askSignIn(agent string, run agentRun) signIn {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if agent == "claude" {
		out, _ := run.commandContext(ctx, "auth", "status", "--json").Output()
		var v struct {
			LoggedIn         bool   `json:"loggedIn"`
			AuthMethod       string `json:"authMethod"`
			Email            string `json:"email"`
			OrgName          string `json:"orgName"`
			SubscriptionType string `json:"subscriptionType"`
		}
		if json.Unmarshal(out, &v) != nil {
			return signIn{Error: "could not read the sign-in status"}
		}
		method := v.AuthMethod
		if v.SubscriptionType != "" {
			method += " (" + v.SubscriptionType + ")"
		}
		account := v.Email
		if account == "" {
			account = v.OrgName
		}
		if !v.LoggedIn {
			method, account = "", ""
		}
		return signIn{Checked: true, SignedIn: v.LoggedIn, Method: method, Account: account}
	}
	out, _ := run.commandContext(ctx, "login", "status").CombinedOutput()
	for _, line := range strings.Split(stripANSI(string(out)), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Not logged in"):
			return signIn{Checked: true}
		case strings.HasPrefix(line, "Logged in"):
			method := strings.TrimSpace(strings.TrimPrefix(line, "Logged in using"))
			// An API key is shown masked after " - "; it stays out.
			method, _, _ = strings.Cut(method, " - ")
			return signIn{Checked: true, SignedIn: true, Method: strings.TrimPrefix(method, "an ")}
		}
	}
	return signIn{Error: "could not read the sign-in status"}
}

// view describes one agent as a setup would run it.
func (m *Manager) view(agent string, s AgentSetup, withSignIn bool) agentView {
	v := agentView{}
	p := &v.Program
	p.Value, p.Source, p.Candidates = s.Path, "path", candidates(agent)
	if s.Path != "" {
		p.Source, p.Path = "setting", s.Path
		version, err := checkProgram(agent, s.Path)
		p.Version = version
		if err != nil {
			p.Error = err.Error()
		}
	} else if run, err := runFor(agent, AgentSetup{}); err != nil {
		p.Error = "not found on PATH"
	} else {
		p.Path = run.Binary
		p.Version, p.Error = cliVersion(agent, run.Binary)
	}
	h := &v.Home
	h.Value, h.Env, h.Default = s.Home, envHome(agent), defaultHome(agent)
	switch {
	case s.Home != "":
		h.Source, h.Path = "setting", s.Home
	case h.Env != "":
		h.Source, h.Path = "environment", h.Env
	default:
		h.Source, h.Path = "default", h.Default
	}
	if info, err := os.Stat(h.Path); err == nil && info.IsDir() {
		h.Exists = true
	} else if s.Home != "" {
		h.Error = checkHome(s.Home).Error()
	}
	h.LoginCommand = loginCommand(agent, s)
	if withSignIn && h.Exists && p.Error == "" {
		h.SignIn = m.signInOf(agent, s)
	}
	return v
}

// busyWith counts the sessions at work with agent: a turn running, an
// engine starting, a request waiting for an answer.
func (m *Manager) busyWith(agent string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.sessions {
		if r.session.Agent != agent || r.proc == nil {
			continue
		}
		if r.turn || r.session.Status == "running" || r.session.Status == "starting" || r.session.Status == "waiting" {
			n++
		}
	}
	return n
}

// AgentsView is the Agents settings: each agent as Settings has it.
func (m *Manager) AgentsView() map[string]agentView {
	setup := m.Agents()
	out := map[string]agentView{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, agent := range []string{"claude", "codex"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := m.view(agent, setup.of(agent), true)
			v.Busy = m.busyWith(agent)
			v.Login = m.loginState(agent)
			mu.Lock()
			out[agent] = v
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// CheckAgent describes a setup without saving it.
func (m *Manager) CheckAgent(agent string, s AgentSetup) (agentView, error) {
	clean, err := cleanSetup(s)
	if err != nil {
		return agentView{}, err
	}
	return m.view(agent, clean, true), nil
}

func cleanSetup(s AgentSetup) (AgentSetup, error) {
	path, err := cleanPath(s.Path)
	if err != nil {
		return s, fmt.Errorf("program: %w", err)
	}
	home, err := cleanPath(s.Home)
	if err != nil {
		return s, fmt.Errorf("home: %w", err)
	}
	return AgentSetup{Path: path, Home: home}, nil
}

// SetAgents saves a setup after checking it. A home changes only while
// none of that agent's sessions is at work: the sessions listed change
// with it. Idle engines are put to sleep first; running ones keep their
// program until they sleep.
func (m *Manager) SetAgents(next AgentsSetup) error {
	cur := m.Agents()
	for _, agent := range []string{"claude", "codex"} {
		s, err := cleanSetup(next.of(agent))
		if err != nil {
			return fmt.Errorf("%s %w", agentNames[agent], err)
		}
		next.set(agent, s)
		if s.Path != "" && s.Path != cur.of(agent).Path {
			if _, err := checkProgram(agent, s.Path); err != nil {
				return fmt.Errorf("%s program: %w", agentNames[agent], err)
			}
		}
		if s.Home != "" && s.Home != cur.of(agent).Home {
			if err := checkHome(s.Home); err != nil {
				return fmt.Errorf("%s home: %w", agentNames[agent], err)
			}
		}
	}
	changedHome := map[string]bool{}
	for _, agent := range []string{"claude", "codex"} {
		if next.of(agent).Home == cur.of(agent).Home {
			continue
		}
		changedHome[agent] = true
		if n := m.busyWith(agent); n > 0 {
			return conflictError{fmt.Sprintf("%s has %d session(s) at work; change its home when they finish", agentNames[agent], n)}
		}
	}
	for agent := range changedHome {
		m.sleepEngines(agent)
	}
	for _, agent := range []string{"claude", "codex"} {
		if next.of(agent) != cur.of(agent) {
			m.cancelLogin(agent)
		}
	}
	if err := m.store.setting("agents", next); err != nil {
		return err
	}
	m.agentsMu.Lock()
	m.agents = next
	m.agentsMu.Unlock()
	m.applyHomes()
	m.forgetSignIns()
	m.changes.Broadcast()
	return nil
}

// sleepEngines puts an agent's idle engines to sleep.
func (m *Manager) sleepEngines(agent string) {
	m.mu.Lock()
	ids := []string{}
	for id, r := range m.sessions {
		if r.session.Agent == agent && r.proc != nil {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Stop(id, true)
	}
}

// executable is whether a file's mode lets it run (always on Windows,
// where the extension decides).
func executable(info os.FileInfo) bool {
	if exeSuffix() != "" {
		return true
	}
	return info.Mode()&0o111 != 0
}

func exeSuffix() string {
	if os.PathSeparator == '\\' {
		return ".exe"
	}
	return ""
}
