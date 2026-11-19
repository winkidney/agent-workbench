package workbench

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
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
	when, err := FileRoutine{Daily: req.Daily, Days: req.Days, Every: req.Every, At: req.At}.when()
	if err != nil {
		return Routine{}, err
	}
	if when.Kind == "interval" && when.Minutes < agentMinMinutes {
		return Routine{}, fmt.Errorf("--every: at least %d minutes", agentMinMinutes)
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
	from := "a terminal"
	if s, ok := m.callerSession(caller); ok {
		from = s.Title
		for _, a := range m.Assistants() {
			if a.Path == s.Cwd {
				from = a.Name + ": " + s.Title
			}
		}
	}
	label := agentMessagePrefix + strings.ReplaceAll(strings.ReplaceAll(from, "]", ")"), "\n", " ") + "]\n\n"
	s, _, err := m.AssistantMessage(dir, label+text, nil, "")
	return s, err
}

// localAgentAPI serves /api/local/(routines[/{id}] | assistants/{dir}/send)
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
