package workbench

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// "/" commands and "$" skills in the composer.
//
// Claude publishes its own command catalog (initialize) and runs "/name
// args" itself, so its commands pass through as text. Codex's slash commands
// are implemented by its TUI, so the workbench maps the meaningful ones to
// app-server calls. Commands that change a session setting are handled here
// for both agents, so the option pills stay the single source of truth.

type Command struct {
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases,omitempty"`
	ArgumentHint string   `json:"argumentHint,omitempty"`
	Description  string   `json:"description,omitempty"`
	Kind         string   `json:"kind"`   // command | skill
	Source       string   `json:"source"` // native | workbench
	// Action is done by the page itself: new, status, skills.
	Action string `json:"action,omitempty"`
}

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
}

type Catalog struct {
	Commands []Command `json:"commands"`
	Skills   []Skill   `json:"skills"`
}

type workbenchCommand struct {
	Command
	agents string // "", "claude" or "codex"
}

var workbenchCommands = []workbenchCommand{
	{Command: Command{Name: "plan", ArgumentHint: "[task]", Description: "Switch to plan mode, and send the task if given"}},
	{Command: Command{Name: "default", ArgumentHint: "[task]", Description: "Leave plan mode, and send the task if given"}},
	{Command: Command{Name: "model", ArgumentHint: "<model>", Description: "Set the model for this session"}},
	{Command: Command{Name: "effort", ArgumentHint: "<level>", Description: "Set the reasoning effort"}},
	{Command: Command{Name: "permissions", ArgumentHint: "<profile>", Description: "Set what the agent may do"}},
	{Command: Command{Name: "fast", ArgumentHint: "[on|off]", Description: "Toggle fast mode"}},
	{agents: "codex", Command: Command{Name: "compact", Description: "Summarize the conversation to free context"}},
	{agents: "codex", Command: Command{Name: "review", ArgumentHint: "[instructions]", Description: "Review the current changes for issues"}},
	{agents: "codex", Command: Command{Name: "goal", ArgumentHint: "[objective|clear]", Description: "Set, show or clear the goal of a long task"}},
	{agents: "codex", Command: Command{Name: "rename", ArgumentHint: "<name>", Description: "Rename the current thread"}},
	{agents: "codex", Command: Command{Name: "fork", Description: "Fork the current chat into a new session"}},
	{agents: "codex", Command: Command{Name: "mcp", Description: "List MCP servers and their tools"}},
	{agents: "codex", Command: Command{Name: "diff", Description: "Show git diff, including untracked files"}},
	{agents: "codex", Command: Command{Name: "new", Description: "Start a new chat in this project", Action: "new"}},
	{agents: "codex", Command: Command{Name: "status", Description: "Show session configuration and token usage", Action: "status"}},
	{agents: "codex", Command: Command{Name: "usage", Description: "Show account usage and limits", Action: "status"}},
	{agents: "codex", Command: Command{Name: "skills", Description: "Use skills for specific tasks", Action: "skills"}},
	{agents: "codex", Command: Command{Name: "quit", Aliases: []string{"exit"}, Description: "Put the engine to sleep"}},
}

func workbenchFor(agent string) []Command {
	out := []Command{}
	for _, c := range workbenchCommands {
		if c.agents == "" || c.agents == agent {
			cmd := c.Command
			cmd.Kind, cmd.Source = "command", "workbench"
			out = append(out, cmd)
		}
	}
	return out
}

// buildCatalog merges the workbench commands with what the CLI reports.
func buildCatalog(agent string, meta metadata) Catalog {
	cat := Catalog{Commands: workbenchFor(agent), Skills: []Skill{}}
	taken := map[string]bool{}
	for _, c := range cat.Commands {
		taken[c.Name] = true
		for _, a := range c.Aliases {
			taken[a] = true
		}
	}
	for _, raw := range meta.Skills {
		s := obj(raw)
		cat.Skills = append(cat.Skills, Skill{Name: str(s["name"]), Description: str(s["description"]), Path: str(s["path"])})
	}
	if agent != "claude" {
		return cat
	}
	// Skills by name, and plugin skills ("plugin:name") by their bare name,
	// which is how the command list shows them (-1 when two share it).
	skill, bare := map[string]int{}, map[string]int{}
	for i, s := range cat.Skills {
		skill[s.Name] = i
		if j := strings.LastIndex(s.Name, ":"); j >= 0 {
			if _, dup := bare[s.Name[j+1:]]; dup {
				bare[s.Name[j+1:]] = -1
			} else {
				bare[s.Name[j+1:]] = i
			}
		}
	}
	terminal := map[string]bool{}
	for _, t := range meta.TerminalCommands {
		terminal[t] = true
	}
	for _, raw := range meta.Commands {
		c := obj(raw)
		name := str(c["name"])
		if name == "" || terminal[name] || strings.HasPrefix(name, "__") {
			continue
		}
		kind := "command"
		i, ok := skill[name]
		if !ok {
			i, ok = bare[name]
			ok = ok && i >= 0
		}
		if ok {
			kind = "skill"
			// "$docx" is sent as "/docx", the name the CLI runs it by.
			cat.Skills[i].Name = name
			// Claude lists skills by name only; the description is here.
			if cat.Skills[i].Description == "" {
				cat.Skills[i].Description = str(c["description"])
			}
		}
		if taken[name] {
			continue
		}
		cat.Commands = append(cat.Commands, Command{Name: name, Aliases: strs(c["aliases"]), ArgumentHint: str(c["argumentHint"]), Description: str(c["description"]), Kind: kind, Source: "native"})
	}
	return cat
}

// Catalog lists the commands and skills for a session.
func (m *Manager) Catalog(id string) (Catalog, error) {
	if err := m.Attach(id); err != nil {
		return Catalog{}, err
	}
	m.mu.Lock()
	r := m.find(id)
	agent, cwd := r.session.Agent, r.session.Cwd
	m.mu.Unlock()
	return buildCatalog(agent, m.Metadata(agent, cwd)), nil
}

// lookupCommand finds a workbench command by name or alias for an agent.
func lookupCommand(agent, name string) (Command, bool) {
	for _, c := range workbenchFor(agent) {
		if c.Name == name {
			return c, true
		}
		for _, a := range c.Aliases {
			if a == name {
				return c, true
			}
		}
	}
	return Command{}, false
}

// CommandResult tells the page what to do after a command.
type CommandResult struct {
	Session string   `json:"session,omitempty"` // open this session (fork)
	Options *Options `json:"options,omitempty"` // settings now in force
	Sent    bool     `json:"sent,omitempty"`    // the task was sent as a turn
}

// RunCommand runs a workbench command (see workbenchCommands).
func (m *Manager) RunCommand(id, name, args string) (CommandResult, error) {
	var res CommandResult
	if err := m.Attach(id); err != nil {
		return res, err
	}
	m.mu.Lock()
	r := m.find(id)
	agent := r.session.Agent
	readOnly := r.session.ReadOnly
	m.mu.Unlock()
	if readOnly {
		return res, errors.New("session is read-only")
	}
	cmd, ok := lookupCommand(agent, name)
	if !ok {
		return res, fmt.Errorf("unknown command /%s", name)
	}
	args = strings.TrimSpace(args)
	setOption := func(change func(*Options)) error {
		o := m.options(id)
		change(&o)
		if err := m.SetOptions(id, o); err != nil {
			return err
		}
		res.Options = &o
		return nil
	}
	need := func() error {
		if args == "" {
			return fmt.Errorf("usage: /%s %s", cmd.Name, cmd.ArgumentHint)
		}
		return nil
	}
	switch cmd.Name {
	case "plan", "default":
		if err := setOption(func(o *Options) { o.Mode = cmd.Name }); err != nil {
			return res, err
		}
		if args != "" {
			res.Sent = true
			return res, m.Send(id, args)
		}
		return res, nil
	case "model":
		if err := need(); err != nil {
			return res, err
		}
		return res, setOption(func(o *Options) { o.Model, o.Effort, o.Speed = args, "", "" })
	case "effort":
		if err := need(); err != nil {
			return res, err
		}
		return res, setOption(func(o *Options) { o.Effort = args })
	case "permissions":
		if err := need(); err != nil {
			return res, err
		}
		return res, setOption(func(o *Options) { o.Permissions = args })
	case "fast":
		on := args == "" || args == "on"
		if args != "" && args != "on" && args != "off" {
			return res, errors.New("usage: /fast [on|off]")
		}
		speed := map[bool]map[bool]string{true: {true: "priority", false: "default"}, false: {true: "fast", false: "standard"}}[agent == "codex"][on]
		return res, setOption(func(o *Options) { o.Speed = speed })
	case "quit":
		return res, m.Stop(id, true)
	case "rename":
		if err := need(); err != nil {
			return res, err
		}
		_, err := m.Rename(id, args)
		return res, err
	}
	// The rest drive the Codex app-server.
	thread, err := m.readyCodex(id)
	if err != nil {
		return res, err
	}
	switch cmd.Name {
	case "compact":
		return res, m.codexTurn(id, "thread/compact/start", map[string]any{"threadId": thread})
	case "review":
		target := map[string]any{"type": "uncommittedChanges"}
		if args != "" {
			target = map[string]any{"type": "custom", "instructions": args}
		}
		return res, m.codexTurn(id, "review/start", map[string]any{"threadId": thread, "target": target, "delivery": "inline"})
	case "diff":
		_, err := m.rpc(id, "thread/shellCommand", map[string]any{"threadId": thread, "command": "git status --short && git diff"})
		return res, err
	case "goal":
		return res, m.codexGoal(id, thread, args)
	case "mcp":
		v, err := m.rpc(id, "mcpServerStatus/list", map[string]any{})
		if err != nil {
			return res, err
		}
		m.notice(id, mcpSummary(v))
		return res, nil
	case "fork":
		// Only the new thread's id is used, as with thread/resume.
		v, err := m.rpc(id, "thread/fork", map[string]any{"threadId": thread, "excludeTurns": true})
		if err != nil {
			return res, err
		}
		forked := str(obj(v["thread"])["id"])
		if forked == "" {
			return res, errors.New("fork returned no thread")
		}
		// Name the fork after its source. The fork stays loaded (and
		// write-locked) in this engine, which thread/unsubscribe does not
		// release, so this engine goes to sleep: the session is idle (a fork
		// needs no running turn), and the new session can run in its own.
		m.mu.Lock()
		title := r.session.Title
		m.mu.Unlock()
		if title != "" {
			named := make(chan struct{})
			go func() {
				defer close(named)
				_, _ = m.rpc(id, "thread/name/set", map[string]any{"threadId": forked, "name": title + " (fork)"})
			}()
			select {
			case <-named:
			case <-time.After(3 * time.Second):
			}
		}
		// Right after a cold start the MCP servers still count as work until
		// they settle, so retry for a while; previews or a real background
		// process keep the engine (and the fork read-only) until it sleeps.
		slept := false
		for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(500 * time.Millisecond) {
			if m.Stop(id, true) == nil {
				slept = true
				break
			}
			if time.Now().After(deadline) {
				break
			}
		}
		if !slept {
			m.notice(id, "Forked. The fork opens read-only until this session's engine goes to sleep (it has previews or background work).")
		}
		m.catalog.mu.Lock()
		m.catalog.updated = time.Time{}
		m.catalog.mu.Unlock()
		res.Session = nativeKey("codex", forked)
		m.notice(id, "Forked into a new session.")
		return res, nil
	}
	return res, fmt.Errorf("/%s is handled by the page", cmd.Name)
}

// readyCodex starts the session's engine if needed and waits for its thread.
func (m *Manager) readyCodex(id string) (string, error) {
	if err := m.Start(id); err != nil {
		return "", err
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		m.mu.Lock()
		r := m.find(id)
		if r == nil || r.proc == nil {
			m.mu.Unlock()
			return "", errors.New("agent exited during startup")
		}
		if r.ready {
			thread := r.session.NativeID
			m.mu.Unlock()
			return thread, nil
		}
		m.mu.Unlock()
		if time.Now().After(deadline) {
			return "", errors.New("agent initialization timed out; inspect session errors")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// codexTurn starts a turn through an app-server call (compaction, review),
// with the same turn bookkeeping as a message.
func (m *Manager) codexTurn(id, method string, params map[string]any) error {
	m.mu.Lock()
	r := m.find(id)
	if r.turn || len(r.pending) > 0 {
		m.mu.Unlock()
		return errors.New("session is busy or awaiting approval")
	}
	if value, ok := r.total["outputTokens"].(float64); ok {
		r.baseline = value
	}
	r.began = time.Now()
	r.turnOut, r.phase = 0, "requesting"
	r.turn = true
	r.interrupted = false
	m.status(r, "running")
	m.mu.Unlock()
	if _, err := m.rpc(id, method, params); err != nil {
		m.mu.Lock()
		if r.turn {
			r.turn, r.phase = false, ""
			m.status(r, "idle")
		}
		m.mu.Unlock()
		return err
	}
	return nil
}

func (m *Manager) codexGoal(id, thread, args string) error {
	switch args {
	case "":
		v, err := m.rpc(id, "thread/goal/get", map[string]any{"threadId": thread})
		if err != nil {
			return err
		}
		goal := obj(v["goal"])
		if str(goal["objective"]) == "" {
			m.notice(id, "No goal is set.")
		} else {
			m.notice(id, fmt.Sprintf("Goal: %s (%s)", str(goal["objective"]), str(goal["status"])))
		}
		return nil
	case "clear":
		if _, err := m.rpc(id, "thread/goal/clear", map[string]any{"threadId": thread}); err != nil {
			return err
		}
		m.notice(id, "Goal cleared.")
		return nil
	}
	if _, err := m.rpc(id, "thread/goal/set", map[string]any{"threadId": thread, "objective": args, "origin": "user"}); err != nil {
		return err
	}
	m.notice(id, "Goal set: "+args)
	return nil
}

func mcpSummary(v map[string]any) string {
	lines := []string{}
	for _, raw := range asList(v["data"]) {
		s := obj(raw)
		status := str(s["runtimeStatus"])
		if status == "" {
			status = str(obj(s["runtimeStatus"])["type"])
		}
		tools := len(obj(s["tools"]))
		if tools == 0 {
			tools = len(asList(s["tools"]))
		}
		line := str(s["name"])
		if status != "" {
			line += " · " + status
		}
		line += fmt.Sprintf(" · %d tools", tools)
		if e := str(s["toolsError"]); e != "" {
			line += " · " + e
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "No MCP servers are configured."
	}
	return "MCP servers:\n" + strings.Join(lines, "\n")
}

// notice posts a workbench message into the session's timeline.
func (m *Manager) notice(id, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.find(id); r != nil {
		m.emit(r.session.ID, "notice", map[string]any{"text": text})
	}
}

var skillMention = regexp.MustCompile(`(?:^|\s)\$([\w][\w:.-]*)`)

// prepareText applies "$" skills and "/" rules before a message is sent.
// Claude runs a leading "$skill" as "/skill"; Codex attaches every "$skill"
// mention as a skill input, refuses unknown "/" commands (its slash
// commands belong to its TUI) and sends "//text" as "/text".
func prepareText(agent, text string, skills []Skill) (string, []Skill, error) {
	known := map[string]Skill{}
	for _, s := range skills {
		known[s.Name] = s
	}
	trimmed := strings.TrimLeft(text, " \t\n")
	if agent == "claude" {
		if strings.HasPrefix(trimmed, "$") {
			name, _, _ := strings.Cut(trimmed[1:], " ")
			if _, ok := known[strings.TrimSpace(name)]; ok {
				return "/" + trimmed[1:], nil, nil
			}
		}
		return text, nil, nil
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed[1:], nil, nil
	}
	if strings.HasPrefix(trimmed, "/") {
		name, _, _ := strings.Cut(trimmed[1:], " ")
		return "", nil, fmt.Errorf("unknown command /%s (start with // to send it as text)", strings.TrimSpace(name))
	}
	var attach []Skill
	seen := map[string]bool{}
	for _, m := range skillMention.FindAllStringSubmatch(text, -1) {
		if s, ok := known[m[1]]; ok && !seen[s.Name] {
			seen[s.Name] = true
			attach = append(attach, s)
		}
	}
	return text, attach, nil
}
