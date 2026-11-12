package workbench

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Which program runs each agent and where it keeps its data. Settings may
// choose both; otherwise the workbench's own environment decides (PATH,
// CLAUDE_CONFIG_DIR, CODEX_HOME), as a shell would.
//
// A home is passed to the CLI only when Settings chose one. Leaving the
// variable unset matters: Claude Code keys its macOS login to the exact
// CLAUDE_CONFIG_DIR string, and with the variable set (even to ~/.claude)
// it keeps .claude.json there rather than in the home directory.

// AgentSetup is one agent's program and home; empty is automatic.
type AgentSetup struct {
	Path string `json:"path,omitempty"`
	Home string `json:"home,omitempty"`
}

// AgentsSetup is what Settings chose, per agent.
type AgentsSetup struct {
	Claude AgentSetup `json:"claude"`
	Codex  AgentSetup `json:"codex"`
}

func (a AgentsSetup) of(agent string) AgentSetup {
	if agent == "codex" {
		return a.Codex
	}
	return a.Claude
}

func (a *AgentsSetup) set(agent string, s AgentSetup) {
	if agent == "codex" {
		a.Codex = s
	} else {
		a.Claude = s
	}
}

// homeVar names the variable each CLI takes its home from.
var homeVar = map[string]string{"claude": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME"}

// defaultHome is where a CLI keeps its data with no variable set.
func defaultHome(agent string) string {
	home, _ := os.UserHomeDir()
	if agent == "codex" {
		return filepath.Join(home, ".codex")
	}
	return filepath.Join(home, ".claude")
}

// agentRun is how to start an agent's CLI: its program, and the environment
// when a home was chosen (nil inherits the workbench's).
type agentRun struct {
	Binary string
	Env    []string
}

func (r agentRun) command(args ...string) *exec.Cmd {
	cmd := exec.Command(r.Binary, args...)
	if r.Env != nil {
		cmd.Env = r.Env
	}
	return cmd
}

func (r agentRun) commandContext(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.Binary, args...)
	if r.Env != nil {
		cmd.Env = r.Env
	}
	return cmd
}

// withEnv is env with name set to value.
func withEnv(env []string, name, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if k, _, _ := strings.Cut(e, "="); k != name {
			out = append(out, e)
		}
	}
	return append(out, name+"="+value)
}

// cleanPath is the one spelling of a chosen path the CLI is given every
// time: absolute, without a trailing separator.
func cleanPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		return "", errors.New("use an absolute path")
	}
	return filepath.Clean(p), nil
}

// loadAgents reads what Settings chose.
func (m *Manager) loadAgents() {
	var a AgentsSetup
	m.store.readSetting("agents", &a)
	m.agentsMu.Lock()
	m.agents = a
	m.agentsMu.Unlock()
	m.applyHomes()
}

// Agents is what Settings chose.
func (m *Manager) Agents() AgentsSetup {
	m.agentsMu.Lock()
	defer m.agentsMu.Unlock()
	return m.agents
}

// envHome is the home the workbench's environment names, if any.
func envHome(agent string) string { return os.Getenv(homeVar[agent]) }

// agentHome is the home an agent's CLI uses, and where that comes from:
// "setting", "environment" or "default".
func (m *Manager) agentHome(agent string) (string, string) {
	if h := m.Agents().of(agent).Home; h != "" {
		return h, "setting"
	}
	if h := envHome(agent); h != "" {
		return h, "environment"
	}
	return defaultHome(agent), "default"
}

// cli is how to start an agent's CLI now.
func (m *Manager) cli(agent string) (agentRun, error) {
	return runFor(agent, m.Agents().of(agent))
}

func runFor(agent string, s AgentSetup) (agentRun, error) {
	run := agentRun{Binary: s.Path}
	if run.Binary == "" {
		path, err := exec.LookPath(agent)
		if err != nil {
			return agentRun{}, err
		}
		run.Binary = path
	}
	if s.Home != "" {
		run.Env = withEnv(os.Environ(), homeVar[agent], s.Home)
	}
	return run, nil
}

// cacheKey tells apart results that depend on the program and home.
func (m *Manager) cacheKey(agent string) string {
	s := m.Agents().of(agent)
	if s.Path == "" && s.Home == "" {
		return ""
	}
	return s.Path + "|" + s.Home
}

// applyHomes points the catalog at the homes in use.
func (m *Manager) applyHomes() {
	claude, _ := m.agentHome("claude")
	codex, _ := m.agentHome("codex")
	m.catalog.SetHomes(claude, codex)
	m.metaMu.Lock()
	m.metadata = nil
	m.metaMu.Unlock()
}

// SetHomes changes where the catalog reads native records.
func (c *NativeCatalog) SetHomes(claude, codex string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ClaudeHome == claude && c.CodexHome == codex {
		return
	}
	c.ClaudeHome, c.CodexHome = claude, codex
	c.updated = time.Time{}
}

// AgentCLI is the program and home an agent runs with in the workbench
// whose data is in dataDir, read without opening it for writing: for the
// doctor command, which may run beside it.
func AgentCLI(dataDir, agent string) (binary, home, homeSource string, err error) {
	var setup AgentsSetup
	if db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dataDir, "workbench.db"))+"?mode=ro"); e == nil {
		var raw string
		if db.QueryRow(`SELECT value FROM settings WHERE key='agents'`).Scan(&raw) == nil {
			_ = json.Unmarshal([]byte(raw), &setup)
		}
		db.Close()
	}
	s := setup.of(agent)
	switch {
	case s.Home != "":
		home, homeSource = s.Home, "setting"
	case envHome(agent) != "":
		home, homeSource = envHome(agent), "environment"
	default:
		home, homeSource = defaultHome(agent), "default"
	}
	run, err := runFor(agent, s)
	return run.Binary, home, homeSource, err
}
