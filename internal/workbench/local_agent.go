package workbench

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// The agents' own commands (ADR 0012): an agent in a session runs
// `agent-workbench routine add …` or `assistant send …`, which call
// /api/local/* here. Only this computer may call them, with the token the
// workbench writes to <data dir>/agent-token at every start; the engines
// learn where to find it from the variables below.

const (
	agentTokenFile = "agent-token"
	agentHeader    = "X-Workbench-Agent"
	// What an engine's environment carries for the command.
	EnvBin     = "AGENT_WORKBENCH_BIN"
	EnvDataDir = "AGENT_WORKBENCH_DATA_DIR"
	EnvSession = "AGENT_WORKBENCH_SESSION"
	// Limits on what agents schedule.
	agentRoutinesMax   = 20
	agentMinMinutes    = 15
	agentMessageMax    = 8 << 10
	agentMessagePrefix = "[From another session: "
)

// engineEnv adds what the agent-workbench command needs to an engine's
// environment.
func (m *Manager) engineEnv(env []string, session string) []string {
	if env == nil {
		env = os.Environ()
	}
	if bin, err := os.Executable(); err == nil {
		env = withEnv(env, EnvBin, bin)
	}
	env = withEnv(env, EnvDataDir, m.store.dir)
	return withEnv(env, EnvSession, session)
}

// AgentRoutineRequest is a routine an agent asks for.
type AgentRoutineRequest struct {
	// The session asking (AGENT_WORKBENCH_SESSION); empty from a terminal.
	Caller  string `json:"caller"`
	Name    string `json:"name"`
	Message string `json:"message"`
	// this | an assistant's folder | a session id
	To    string   `json:"to"`
	Daily string   `json:"daily,omitempty"`
	Days  []string `json:"days,omitempty"`
	Every string   `json:"every,omitempty"`
	Cron  string   `json:"cron,omitempty"`
	At    string   `json:"at,omitempty"`
}

// callerSession is the session an id names now: the id it started with is
// replaced by the native one.
func (m *Manager) callerSession(id string) (Session, bool) {
	if id == "" {
		return Session{}, false
	}
	m.mu.Lock()
	r := m.find(id)
	var s Session
	if r != nil {
		s = r.session
	}
	m.mu.Unlock()
	if r != nil {
		return s, true
	}
	return m.sessionNow(id)
}

// agentTarget is where "--to" sends: this session (or, in a continuous
// assistant's folder, that assistant, whose topic changes), an assistant,
// or a session.
func (m *Manager) agentTarget(to, caller string) (*RoutineTarget, error) {
	to = strings.TrimSpace(to)
	if to == "" || to == "this" {
		s, ok := m.callerSession(caller)
		if !ok {
			return nil, errors.New(`--to this works only inside a workbench session; name an assistant or a session id`)
		}
		for _, a := range m.Assistants() {
			if a.Path == s.Cwd && a.Sessions == "continuous" && a.Error == "" {
				return &RoutineTarget{Kind: "assistant", Assistant: a.Dir}, nil
			}
		}
		return &RoutineTarget{Kind: "session", Session: s.ID}, nil
	}
	if a, err := m.assistant(to); err == nil {
		return &RoutineTarget{Kind: "assistant", Assistant: a.Dir}, nil
	}
	if s, ok := m.callerSession(to); ok {
		return &RoutineTarget{Kind: "session", Session: s.ID}, nil
	}
	return nil, fmt.Errorf("--to %q: not an assistant or a session", to)
}

// AgentRoutine makes a message routine an agent asked for. It never runs a
// command, runs at most every agentMinMinutes, and an agent has at most
// agentRoutinesMax switched on.
func (m *Manager) AgentRoutine(req AgentRoutineRequest) (Routine, error) {
	when, err := FileRoutine{Daily: req.Daily, Days: req.Days, Every: req.Every, At: req.At, Cron: req.Cron}.when()
	if err != nil {
		return Routine{}, err
	}
	if when.Kind == "interval" && when.Minutes < agentMinMinutes {
		return Routine{}, fmt.Errorf("--every: at least %d minutes", agentMinMinutes)
	}
	if when.Kind == "cron" {
		if gap, err := cronGap(when.Cron, time.Now()); err != nil || gap < agentMinMinutes*time.Minute {
			return Routine{}, fmt.Errorf("--cron: at most every %d minutes", agentMinMinutes)
		}
	}
	if strings.TrimSpace(req.Message) == "" {
		return Routine{}, errors.New("--message is missing")
	}
	if len(req.Message) > agentMessageMax {
		return Routine{}, fmt.Errorf("--message: at most %d bytes", agentMessageMax)
	}
	target, err := m.agentTarget(req.To, req.Caller)
	if err != nil {
		return Routine{}, err
	}
	by := "agent:cli"
	if s, ok := m.callerSession(req.Caller); ok {
		by = "agent:" + s.ID
	}
	list, err := m.store.routines()
	if err != nil {
		return Routine{}, err
	}
	on := 0
	for _, v := range list {
		if v.CreatedBy == by && v.Enabled {
			on++
		}
	}
	if on >= agentRoutinesMax {
		return Routine{}, fmt.Errorf("this session has %d routines switched on already; remove one first (routine rm)", on)
	}
	return m.SaveRoutine(Routine{Name: strings.TrimSpace(req.Name), Message: req.Message, Enabled: true, When: when, Target: target, CreatedBy: by})
}

// RemoveAgentRoutine deletes a routine an agent made; the others are the
// user's, changed in the page or in their file.
func (m *Manager) RemoveAgentRoutine(id string) error {
	v, err := m.store.routine(id)
	if err != nil {
		return errors.New("no such routine")
	}
	if !strings.HasPrefix(v.CreatedBy, "agent:") {
		return errors.New("only routines an agent made can be removed here; the user changes the others in Settings → Routines")
	}
	return m.DeleteRoutine(id)
}

// AgentSend gives an assistant a message from a session, labelled so the
// receiving agent knows nobody typed it.
func (m *Manager) AgentSend(caller, dir, text string) (Session, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Session{}, errors.New("the message is empty")
	}
	if len(text) > agentMessageMax {
		return Session{}, fmt.Errorf("the message: at most %d bytes", agentMessageMax)
	}
	s, _, err := m.AssistantMessage(dir, m.fromLabel(caller)+text, nil, "")
	return s, err
}

// fromLabel starts a message one session hands another: who sent it, so
// the receiving agent knows nobody typed it.
func (m *Manager) fromLabel(caller string) string {
	from := "a terminal"
	if s, ok := m.callerSession(caller); ok {
		from = s.Title
		for _, a := range m.Assistants() {
			if a.Path == s.Cwd {
				from = a.Name + ": " + s.Title
			}
		}
	}
	return agentMessagePrefix + strings.ReplaceAll(strings.ReplaceAll(from, "]", ")"), "\n", " ") + "]\n\n"
}

// AgentNewSession starts a session the user asked an assistant for (the
// start-session skill, ADR 0019): an ordinary session in dir with the
// user's new-session settings and the assistant's message as its first.
// Only an assistant's session may ask, not a terminal or another session.
func (m *Manager) AgentNewSession(caller, dir, agent, title, text string) (Session, error) {
	from, ok := m.callerSession(caller)
	assistant := false
	if ok {
		for _, a := range m.Assistants() {
			if a.Path == from.Cwd {
				assistant = true
			}
		}
	}
	if !assistant {
		return Session{}, errors.New("only an assistant starts sessions this way, when the user asks it to; start one from the page instead")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Session{}, errors.New("the first message is empty")
	}
	if len(text) > agentMessageMax {
		return Session{}, fmt.Errorf("the first message: at most %d bytes", agentMessageMax)
	}
	if strings.TrimSpace(dir) == "" {
		return Session{}, errors.New("--folder is missing")
	}
	if agent == "" {
		agent = from.Agent
	}
	named := strings.TrimSpace(title)
	if named == "" {
		named = text
	}
	return m.createWithMessage(agent, dir, strings.TrimSpace(title), named, m.fromLabel(caller)+text, nil, nil, "")
}

// AgentNewAssistant makes an assistant an agent proposed and the user
// approved (the create-assistant skill): the agent setup would choose and a
// session per request unless given. It never replaces a folder; the agent
// writes its procedure next.
func (m *Manager) AgentNewAssistant(name, dir, agent, sessions string) (Assistant, error) {
	if agent == "" {
		if agent = m.defaultAgent(); agent == "" {
			return Assistant{}, errors.New("no agent is installed")
		}
	}
	if sessions == "" {
		sessions = "per-task"
	}
	return m.NewAssistant(name, dir, agent, sessions)
}

// localAgentAPI serves /api/local/(routines[/{id}] | assistants/{dir}/send |
// sessions)
// to the command on this computer.
func (s *Server) localAgentAPI(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	// A tunnel or proxy on this computer forwards others' requests from
	// loopback; they carry these headers, the command never does.
	forwarded := r.Header.Get("Cf-Connecting-Ip") != "" || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "" || r.Header.Get("Origin") != ""
	got := r.Header.Get(agentHeader)
	if !localAddress(net.ParseIP(host)) || forwarded || s.agentToken == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.agentToken)) != 1 {
		http.Error(w, "forbidden", 403)
		return
	}
	m := s.Manager
	rest := strings.TrimPrefix(r.URL.Path, "/api/local/")
	parts := strings.Split(rest, "/")
	switch {
	case rest == "routines" && r.Method == "GET":
		list, err := m.Routines()
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, list)
	case rest == "routines" && r.Method == "POST":
		var req AgentRoutineRequest
		if err := decode(w, r, &req); err != nil {
			bad(w, err)
			return
		}
		v, err := m.AgentRoutine(req)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, v)
	case len(parts) == 2 && parts[0] == "routines" && r.Method == "DELETE":
		if err := m.RemoveAgentRoutine(parts[1]); err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case rest == "assistants" && r.Method == "POST":
		var v struct{ Name, Dir, Agent, Sessions string }
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		a, err := m.AgentNewAssistant(v.Name, v.Dir, v.Agent, v.Sessions)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"assistant": a})
	case rest == "assistants/memory" && r.Method == "POST":
		// The user asked to update memory: from this conversation when it
		// keeps the wiki, else Moss's ongoing one.
		var v struct{ Caller string }
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		dir, session := "main", ""
		if s, ok := m.callerSession(v.Caller); ok {
			for _, a := range m.Assistants() {
				if a.Path == s.Cwd && a.Memory == "wiki" {
					dir, session = a.Dir, s.ID
				}
			}
		}
		started, err := m.IngestNow(dir, session)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"started": started})
	case rest == "sessions" && r.Method == "POST":
		var v struct{ Caller, Dir, Agent, Title, Text string }
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		session, err := m.AgentNewSession(v.Caller, v.Dir, v.Agent, v.Title, v.Text)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, session)
	case rest == "assistants/reload" && r.Method == "POST":
		// An agent changed an assistant.toml: apply it now (ADR 0014).
		m.assistantsChanged()
		m.rt.poke()
		writeJSON(w, map[string]any{"assistants": len(m.Assistants())})
	case len(parts) == 3 && parts[0] == "assistants" && parts[2] == "send" && r.Method == "POST":
		var v struct{ Caller, Text string }
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		session, err := m.AgentSend(v.Caller, parts[1], v.Text)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, session)
	default:
		http.NotFound(w, r)
	}
}

// writeAgentToken makes a new token for the command; an older one stops
// working.
func (s *Server) writeAgentToken() error {
	s.agentToken = token()
	return writePrivateFile(s.Manager.store.dir, agentTokenFile, []byte(s.agentToken))
}
