package workbench

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Service = "agent-workbench.service"

type Process interface {
	Input(any) error
	Stop() error
}

// processOwner is a running engine that knows which processes are its own,
// so a Claude engine it runs is not mistaken for another client.
type processOwner interface{ Owns(pid int) bool }
type Launch func(Session, func(map[string]any), func(error)) (Process, error)
type runtime struct {
	rpc                   map[string]chan map[string]any
	effective             Options
	applied               Options
	baseline              float64
	configuring           bool
	total, usage, context map[string]any
	samples               []sample
	turnOut               float64 // estimated output tokens this turn
	phase                 string  // what the running turn is doing
	began                 time.Time
	average               float64
	session               Session
	proc                  Process
	pending               map[string]any
	ready                 bool
	turn                  bool
	turnID                string
	generation            int
	mcp                   map[string]string
	last                  time.Time
	previews              map[string]*Preview
	buf                   delta
	bufTimer              *time.Timer
	queue                 []queued
	queuePaused           bool
	interrupted           bool
	// Claude: what the CLI advertised on system/init (send-now steering).
	caps map[string]bool
	// Claude: steered messages, by uuid, that no turn has answered yet.
	steering map[string]*steered
	// Codex: the last turn/start sent, to send again without auto-review if
	// Codex refuses it.
	turnRequest map[string]any
	// Claude: the permission mode last sent, to tell the CLI's own mode
	// changes from ours.
	requestedMode string
	// The session's /goal (also kept in the store for the page); Claude's
	// is read from its JSONL (goal.go).
	goal      *Goal
	goalScan  *claudeGoalScan
	goalTimer *time.Timer
	// Codex: an async question awaits an answer; a plan-mode turn ended with
	// a plan that awaits a decision (planSeen: this turn produced one).
	asking, planReady, planSeen bool
}
type Manager struct {
	// closing: the workbench is shutting down (Shutdown); engines it stops
	// then are asleep, not failed.
	closing  bool
	metaMu   sync.Mutex
	metadata map[string]metadata
	mu       sync.Mutex
	store    *Store
	sessions map[string]*runtime
	launch   Launch
	idle     time.Duration
	catalog  *NativeCatalog
	changes  *notifier
	prefsMu  sync.Mutex
	activity *ActivityIndex
	// renameCodex renames an unloaded Codex thread; replaced in tests.
	renameCodex func(map[string]any) error
	// codexThread sends one thread request (archive, unarchive, delete)
	// through a short-lived app-server; replaced in tests.
	codexThread func(method string, params map[string]any) error

	// Incremental readers of native records other clients are writing.
	usageMu  sync.Mutex
	usage    map[string]*usageScan
	extMu    sync.Mutex
	external map[string]*externalActivity
}

func NewManager(store *Store, launch Launch, idle time.Duration) (*Manager, error) {
	m := &Manager{store: store, launch: launch, idle: idle, sessions: map[string]*runtime{}, changes: newNotifier(), renameCodex: codexRename, codexThread: codexThreadRequest, activity: NewActivityIndex(store)}
	m.catalog = NewNativeCatalog()
	return m, nil
}
func (m *Manager) emit(id, kind string, data any) {
	if _, err := m.store.Append(id, kind, data); err != nil {
		fmt.Fprintln(os.Stderr, "persist event:", err)
	}
	m.changes.Broadcast()
}
func (m *Manager) status(r *runtime, state string) {
	r.session.Status = state
	r.session.Attention = attentionOf(r)
	r.last = time.Now()
	if err := m.store.Save(r.session); err != nil {
		fmt.Fprintln(os.Stderr, "save session:", err)
	}
	m.emit(r.session.ID, "status", r.session)
}
func (m *Manager) Create(agent, cwd, title string) (Session, error) {
	return m.CreateSession(agent, cwd, title, "")
}

// CreateSession registers a new session. A title the user typed becomes the
// native name; otherwise the display title comes from the first message and
// the CLI keeps its own automatic title.
func (m *Manager) CreateSession(agent, cwd, title, first string) (Session, error) {
	if agent != "codex" && agent != "claude" {
		return Session{}, errors.New("agent must be codex or claude")
	}
	abs, err := filepath.Abs(expandHome(cwd))
	if err != nil {
		return Session{}, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return Session{}, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return Session{}, errors.New("working directory does not exist")
	}
	explicit := strings.TrimSpace(title) != ""
	if !explicit {
		title = sessionTitle(first)
	}
	if title == "" {
		title = filepath.Base(abs)
	}
	s := Session{ID: token()[:24], Agent: agent, Cwd: abs, Workspace: workspaceNamespace(abs), Title: title, Status: "sleeping", ExplicitName: explicit}
	s.Origin = pathOrigin(s.Workspace)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.store.Save(s); err != nil {
		return s, err
	}
	m.sessions[s.ID] = &runtime{session: s, pending: map[string]any{}, previews: map[string]*Preview{}, last: time.Now()}
	m.emit(s.ID, "status", s)
	return s, nil
}
func (m *Manager) Start(id string) error {
	if m.findUnlocked(id) == nil {
		if err := m.Attach(id); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(id)
	if r == nil {
		return errors.New("session not found")
	}
	if r.proc != nil {
		return nil
	}
	if r.session.Status == "stopping" {
		return errors.New("session is stopping")
	}
	if r.session.Source == "subagent" {
		return errors.New("subagent transcripts are read-only; continue in the parent session")
	}
	if r.session.Archived {
		return errors.New("session is archived; unarchive it to continue")
	}
	if m.catalog.Busy(r.session) {
		return errors.New("native session is active in another client; read-only")
	}
	var saved Telemetry
	m.store.readSetting("telemetry:"+r.session.ID, &saved)
	r.total = saved.Total
	r.usage = saved.Last
	r.context = saved.Context
	r.average = saved.Average
	r.mcp = map[string]string{}
	r.generation++
	generation := r.generation
	r.ready = false
	r.turn = false
	r.pending = map[string]any{}
	r.rpc = map[string]chan map[string]any{}
	r.caps, r.steering = nil, nil
	proc, err := m.launch(r.session, func(v map[string]any) { m.receive(id, generation, v) }, func(err error) { m.exited(id, generation, err) })
	if err != nil {
		m.emit(r.session.ID, "error", map[string]any{"message": err.Error()})
		m.status(r, "error")
		return err
	}
	r.proc = proc
	m.status(r, "starting")
	if r.session.Agent == "codex" {
		return proc.Input(map[string]any{"id": "init", "method": "initialize", "params": map[string]any{"clientInfo": map[string]any{"name": "agent_workbench", "title": "Agent Workbench", "version": "0.1.0"}, "capabilities": map[string]any{"experimentalApi": true}}})
	}
	return proc.Input(map[string]any{"type": "control_request", "request_id": "init", "request": map[string]any{"subtype": "initialize"}})
}
func (m *Manager) Send(id, text string) error {
	if strings.TrimSpace(text) == "" || len(text) > 1024*1024 {
		return errors.New("message must contain 1 to 1048576 bytes")
	}
	// "$" skills and "/" rules (see prepareText).
	if m.findUnlocked(id) == nil {
		if err := m.Attach(id); err != nil {
			return err
		}
	}
	m.mu.Lock()
	agent, cwd := m.find(id).session.Agent, m.find(id).session.Cwd
	m.mu.Unlock()
	text, attach, err := m.prepareFor(agent, cwd, text)
	if err != nil {
		return err
	}

	if err := m.Start(id); err != nil {
		return err
	}
	// Initializing is asynchronous. Do not hold the manager lock while waiting.
	deadline := time.Now().Add(30 * time.Second)
	var mcpSince time.Time
	for {
		m.mu.Lock()
		r := m.find(id)
		if r.proc == nil {
			m.mu.Unlock()
			return errors.New("agent exited during startup")
		}
		settled := mcpSettled(r)
		if r.ready && !settled {
			// Codex starts its MCP servers after the thread is ready. Show
			// that wait, and never let one slow server fail the message:
			// past mcpWait the turn starts without it.
			if mcpSince.IsZero() {
				mcpSince = time.Now()
				r.phase = "mcp"
				m.status(r, "starting")
			}
			settled = time.Since(mcpSince) > mcpWait
		}
		if r.ready && settled {
			// Back to idle if the message is not sent after all.
			revert := func() {
				if r.phase == "mcp" {
					r.phase = ""
					if r.proc != nil && r.session.Status == "starting" {
						m.status(r, "idle")
					}
				}
			}
			if r.session.Status == "error" {
				m.mu.Unlock()
				return errors.New("agent reported an error; end the session before retrying")
			}
			if o, ok := r.proc.(processOwner); ok && r.session.Agent == "claude" && m.catalog.ClaudeBusyExcept(r.session, o.Owns) {
				revert()
				m.mu.Unlock()
				return errors.New("native session is active in another client; read-only")
			}
			if r.turn || r.configuring || len(r.pending) > 0 {
				revert()
				m.mu.Unlock()
				return errors.New("session is busy or awaiting approval")
			}
			r.configuring = true
			m.mu.Unlock()
			err := m.applyOptions(id)
			m.mu.Lock()
			r.configuring = false
			if err != nil {
				revert()
				m.mu.Unlock()
				return err
			}
			if r.proc == nil || !r.ready {
				m.mu.Unlock()
				return errors.New("agent exited while applying settings")
			}
			if r.turn || len(r.pending) > 0 {
				revert()
				m.mu.Unlock()
				return errors.New("session is busy")
			}
			userActed(r)
			m.beginTurn(r)
			m.emit(r.session.ID, "user", map[string]any{"text": text})
			var v any
			if r.session.Agent == "codex" {
				// Skill mentions go first, as the Codex TUI sends them.
				input := []any{}
				for _, s := range attach {
					input = append(input, map[string]any{"type": "skill", "name": s.Name, "path": s.Path})
				}
				input = append(input, map[string]any{"type": "text", "text": text})
				params := map[string]any{"threadId": r.session.NativeID, "input": input}
				chosen := m.options(r.session.ID)
				if chosen.Mode != "" && chosen.Model == "" {
					chosen.Model = r.effective.Model
				}
				codexOptions(params, chosen)
				request := map[string]any{"id": "turn:" + token(), "method": "turn/start", "params": params}
				r.turnRequest = request
				v = request
			} else {
				v = map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}, "parent_tool_use_id": nil, "session_id": r.session.NativeID}
			}
			err = r.proc.Input(v)
			m.mu.Unlock()
			return err
		}
		m.mu.Unlock()
		if mcpSince.IsZero() && time.Now().After(deadline) {
			return errors.New("agent initialization timed out; inspect session errors")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// beginTurn marks a turn as running (m.mu held): one the page sent, or one
// Codex started itself to pursue a goal.
func (m *Manager) beginTurn(r *runtime) {
	if value, ok := r.total["outputTokens"].(float64); ok {
		r.baseline = value
	}
	r.began = time.Now()
	r.turnOut, r.phase = 0, "requesting"
	r.turn = true
	r.interrupted = false
	m.status(r, "running")
}

// prepareFor applies the "$" skill and "/" rules to a message (see
// prepareText), loading the skill list only when the text could name one.
func (m *Manager) prepareFor(agent, cwd, text string) (string, []Skill, error) {
	var skills []Skill
	if trimmed := strings.TrimLeft(text, " \t\n"); strings.Contains(text, "$") && (agent == "codex" || strings.HasPrefix(trimmed, "$")) {
		skills = buildCatalog(agent, m.Metadata(agent, cwd)).Skills
	}
	return prepareText(agent, text, skills)
}

// mcpWait is how long a message waits for MCP servers still starting.
var mcpWait = 60 * time.Second

// mcpProgress counts MCP servers that finished starting (ready or failed).
func mcpProgress(r *runtime) (done, total int) {
	for _, status := range r.mcp {
		total++
		if status != "starting" {
			done++
		}
	}
	return done, total
}
func str(v any) string { s, _ := v.(string); return s }
func obj(v any) map[string]any {
	x, _ := v.(map[string]any)
	if x == nil {
		return map[string]any{}
	}
	return x
}
func (m *Manager) receive(id string, generation int, v map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(id)
	if r == nil || r.proc == nil || r.generation != generation {
		return
	}
	defer m.syncAttention(r)
	key := str(v["id"])
	response := obj(v["response"])
	if v["type"] == "control_response" {
		key = str(response["request_id"])
	}
	if ch := r.rpc[key]; ch != nil {
		reply := v
		if v["type"] == "control_response" {
			reply = map[string]any{"result": response["response"]}
			if response["subtype"] == "error" {
				reply["error"] = response["error"]
			}
		}
		select {
		case ch <- reply:
		default:
		}
		return
	}
	// The goal read when a Codex engine starts (a CLI without goals answers
	// with an error, which is not the session's).
	if r.session.Agent == "codex" && str(v["id"]) == "goal" && v["method"] == nil {
		if v["error"] == nil {
			m.setGoal(r, codexGoal(obj(obj(v["result"])["goal"])))
		}
		return
	}
	if strings.HasPrefix(str(v["id"]), "turn:") && v["error"] == nil {
		chosen := m.options(r.session.ID)
		r.applied = chosen
		// Codex keeps turn overrides for later turns, so a cleared override
		// leaves the previous value in effect.
		r.effective = mergeOptions(r.effective, chosen)
	}
	m.track(r, v)
	r.last = time.Now()
	switch d, class := classifyLive(v); class {
	case liveDelta:
		m.bufferDelta(r, d)
	case liveKeep:
		m.flushDelta(r)
		m.emit(r.session.ID, "native", boundJSON(v))
	}
	if v["type"] == "stderr" {
		m.emit(r.session.ID, "diagnostic", v)
	}
	if r.session.Agent == "codex" {
		if v["error"] != nil && m.retryWithoutAutoReview(r, v) {
			return
		}
		if v["error"] != nil {
			m.emit(r.session.ID, "error", v["error"])
			r.turn = false
			m.status(r, "error")
			return
		}
		switch str(v["id"]) {
		case "init":
			_ = r.proc.Input(map[string]any{"method": "initialized"})
			method := "thread/start"
			params := map[string]any{"cwd": r.session.Cwd, "approvalPolicy": "on-request", "sandbox": "workspace-write"}
			// The reviewer from the start, so turns Codex starts itself (a goal)
			// already have it.
			if p := m.options(r.session.ID).Permissions; p == codexAutoReview {
				params["approvalsReviewer"] = "auto_review"
			} else if p != "" {
				params["approvalsReviewer"] = "user"
			}
			if r.session.NativeID != "" {
				method = "thread/resume"
				params["threadId"] = r.session.NativeID
				// Only the thread's settings are read from the result; the
				// history comes from thread/turns/list. Codex 0.167 sends a
				// deprecation notice on every resume that returns all turns.
				params["excludeTurns"] = true
			}
			_ = r.proc.Input(map[string]any{"id": "thread", "method": method, "params": params})
		case "thread":
			r.effective = codexThreadOptions(obj(v["result"]))
			newThread := r.session.NativeID == ""
			r.session.NativeID = str(obj(obj(v["result"])["thread"])["id"])
			if r.session.NativeID == "" {
				m.emit(r.session.ID, "error", map[string]any{"message": "missing native thread ID"})
				return
			}
			if r.session.NativeID != "" {
				oldID := r.session.ID
				r.session.ID = nativeKey(r.session.Agent, r.session.NativeID)
				if oldID != r.session.ID {
					m.store.setting("options:"+r.session.ID, m.options(oldID))
				}
				if newThread && r.session.ExplicitName && r.session.Title != "" {
					_ = r.proc.Input(map[string]any{"id": "name", "method": "thread/name/set", "params": map[string]any{"threadId": r.session.NativeID, "name": r.session.Title}})
				}
			}
			r.ready = true
			if p, ok := r.proc.(interface{ MarkReady() }); ok {
				p.MarkReady()
			}
			m.status(r, "idle")
			// The thread's goal; later changes arrive as notifications.
			_ = r.proc.Input(map[string]any{"id": "goal", "method": "thread/goal/get", "params": map[string]any{"threadId": r.session.NativeID}})
		}
		method := str(v["method"])
		params := obj(v["params"])
		trackAttention(r, method, params)
		if method == "mcpServer/startupStatus/updated" {
			r.mcp[str(params["name"])] = str(params["status"])
			if mcpSettled(r) && !r.turn {
				if p, ok := r.proc.(interface{ MarkReady() }); ok {
					p.MarkReady()
				}
			}
		}

		if method == "turn/started" {
			r.turnID = str(obj(params["turn"])["id"])
			// Codex starts turns itself to pursue a goal (after /goal, after
			// each turn while the goal is active, on resume).
			if !r.turn {
				m.beginTurn(r)
			}
		}
		switch method {
		case "thread/goal/updated":
			m.setGoal(r, codexGoal(obj(params["goal"])))
		case "thread/goal/cleared":
			m.setGoal(r, nil)
		}
		if v["id"] != nil && method != "" {
			key := fmt.Sprint(v["id"])
			r.pending[key] = v
			m.emit(r.session.ID, "approval", m.approvalData(r, key, v))
			m.status(r, "waiting")
		}
		if method == "turn/started" {
			m.emit(r.session.ID, "turn", map[string]any{"id": r.turnID})
		}
		if method == "serverRequest/resolved" {
			key := fmt.Sprint(params["requestId"])
			if _, ok := r.pending[key]; ok {
				delete(r.pending, key)
				m.emit(r.session.ID, "approval_resolved", map[string]any{"id": key})
				if len(r.pending) == 0 && r.turn {
					m.status(r, "running")
				}
			}
		}
		if method == "turn/completed" {
			r.turn = false
			r.pending = map[string]any{}
			m.status(r, "idle")
			turnStatus := str(obj(params["turn"])["status"])
			m.turnEnded(r, turnStatus == "failed" || turnStatus == "interrupted")
		}
	} else {
		if str(v["type"]) == "control_response" && str(obj(v["response"])["request_id"]) == "init" {
			if str(obj(v["response"])["subtype"]) == "error" {
				m.status(r, "error")
				return
			}
			if r.session.NativeID != "" {
				oldID := r.session.ID
				r.session.ID = nativeKey(r.session.Agent, r.session.NativeID)
				if oldID != r.session.ID {
					m.store.setting("options:"+r.session.ID, m.options(oldID))
				}
			}
			r.ready = true
			if p, ok := r.proc.(interface{ MarkReady() }); ok {
				p.MarkReady()
			}
			m.status(r, "idle")
			if r.session.NativeID != "" {
				m.refreshClaudeGoal(r)
			}
		}
		if sid := str(v["session_id"]); sid != "" && sid != r.session.NativeID {
			oldID := r.session.ID
			r.session.NativeID = sid
			r.session.ID = nativeKey(r.session.Agent, sid)
			if oldID != r.session.ID {
				m.store.setting("options:"+r.session.ID, m.options(oldID))
			}
			_ = m.store.Save(r.session)
		}
		switch str(v["type"]) {
		case "control_request":
			key := str(v["request_id"])
			r.pending[key] = v
			m.emit(r.session.ID, "approval", m.approvalData(r, key, v))
			m.status(r, "waiting")
		case "control_cancel_request":
			delete(r.pending, str(v["request_id"]))
			m.emit(r.session.ID, "approval_cancelled", map[string]any{"id": v["request_id"]})
		case "assistant": // Full message is retained in native history, not duplicated into text deltas.
			// Local command output (/goal set, cleared, shown).
			if str(obj(v["message"])["model"]) == "<synthetic>" {
				m.refreshClaudeGoal(r)
			}
		case "user":
			// A goal check that found the condition not yet met.
			if strings.HasPrefix(nativeText(obj(v["message"])["content"]), "Stop hook feedback:") {
				m.refreshClaudeGoal(r)
			}
		case "system":
			if str(v["subtype"]) == "init" {
				r.caps = map[string]bool{}
				for _, c := range strs(v["capabilities"]) {
					r.caps[c] = true
				}
			}
		case "command_lifecycle":
			m.claudeLifecycle(r, v)
		case "result":
			// A goal met (or given up) is only in the JSONL.
			m.refreshClaudeGoal(r)
			for _, u := range strs(v["user_message_uuids"]) {
				delete(r.steering, u)
			}
			// A steered message this turn did not take in (send-now ended the
			// turn to make room) runs next on its own: the session keeps
			// working rather than going idle in between.
			if len(r.steering) > 0 && !r.interrupted {
				r.pending = map[string]any{}
				r.began = time.Now()
				r.turnOut, r.phase = 0, "requesting"
				return
			}
			r.turn = false
			r.pending = map[string]any{}
			// A user interrupt ends the turn with an error result; the transcript already says so.
			if v["is_error"] == true && !r.interrupted {
				m.emit(r.session.ID, "error", v)
			}
			m.status(r, "idle")
			m.turnEnded(r, v["is_error"] == true)
		}
	}
}
func (m *Manager) Reply(id, key string, allow bool, answers map[string]any) error {
	decision := "deny"
	if allow {
		decision = "allow"
	}
	return m.ReplyDecision(id, key, decision, "", answers, "")
}

// ReplyDecision answers a pending native request. decision is allow,
// allowAlways (session-scoped), deny (the turn continues, message is shown to
// the agent) or cancel (deny and interrupt the turn). mode is the permission
// mode an approved Claude plan continues in, as Claude Code's own choices set
// it: auto, acceptEdits, or default (edits approved one by one; also when
// empty).
func (m *Manager) ReplyDecision(id, key, decision, message string, answers map[string]any, mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(id)
	if r == nil || r.proc == nil {
		return errors.New("session is not active")
	}
	v, ok := r.pending[key]
	if !ok {
		return errors.New("approval expired")
	}
	switch decision {
	case "allow", "allowAlways", "deny", "cancel":
	default:
		return errors.New("decision must be allow, allowAlways, deny or cancel")
	}
	allow := decision == "allow" || decision == "allowAlways"
	request := obj(v)
	var reply any
	// An approved Claude plan leaves plan mode.
	leavesPlan := false
	if r.session.Agent == "codex" {
		method := str(request["method"])
		var result any
		switch {
		case strings.Contains(method, "requestUserInput"):
			if !allow {
				return errors.New("provide answers or interrupt the turn")
			}
			result = map[string]any{"answers": answers}
		case method == "item/commandExecution/requestApproval" || method == "item/fileChange/requestApproval":
			result = map[string]any{"decision": map[string]string{"allow": "accept", "allowAlways": "acceptForSession", "deny": "decline", "cancel": "cancel"}[decision]}
		default:
			return errors.New("unsupported agent request; interrupt the turn instead")
		}
		reply = map[string]any{"id": request["id"], "result": result}
	} else {
		req := obj(request["request"])
		if str(req["subtype"]) != "can_use_tool" {
			return errors.New("unsupported control request")
		}
		if message == "" {
			message = "Denied by user"
		}
		response := map[string]any{"behavior": "deny", "message": message}
		if decision == "cancel" {
			response["interrupt"] = true
		}
		if allow {
			input := obj(req["input"])
			if str(req["tool_name"]) == "AskUserQuestion" {
				if len(answers) == 0 {
					return errors.New("provide answers")
				}
				input["answers"] = answers
			}
			response = map[string]any{"behavior": "allow", "updatedInput": input}
			if suggestions, ok := req["permission_suggestions"].([]any); decision == "allowAlways" && ok && len(suggestions) > 0 {
				response["updatedPermissions"] = suggestions
			}
			if str(req["tool_name"]) == "ExitPlanMode" {
				switch mode {
				case "", "default", "manual":
					mode = "default"
				case "acceptEdits", "auto":
				default:
					return errors.New("an approved plan continues in auto, acceptEdits or default mode")
				}
				response["updatedPermissions"] = []any{map[string]any{"type": "setMode", "destination": "session", "mode": mode}}
				leavesPlan = true
			}
		}
		reply = map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": key, "response": response}}
	}
	if err := r.proc.Input(reply); err != nil {
		return err
	}
	delete(r.pending, key)
	r.session.Attention = attentionOf(r)
	if leavesPlan {
		m.leavePlan(r, mode)
	}
	m.emit(r.session.ID, "approval_resolved", map[string]any{"id": key})
	m.status(r, "running")
	return nil
}

// approvalData is an approval as the page gets it. A Claude plan in a session
// that was in auto mode before planning says so: Claude Code then offers to
// go back to auto mode (m.mu held).
func (m *Manager) approvalData(r *runtime, key string, v any) map[string]any {
	data := map[string]any{"id": key, "request": v}
	req := obj(obj(v)["request"])
	if r.session.Agent == "claude" && str(req["tool_name"]) == "ExitPlanMode" && m.options(r.session.ID).Permissions == "auto" {
		data["planResume"] = "auto"
	}
	return data
}

// leavePlan records that an approved plan took a Claude session out of plan
// mode into mode, so the settings shown, and those set before the next turn,
// are no longer plan mode.
func (m *Manager) leavePlan(r *runtime, mode string) {
	permissions := "manual"
	if mode == "acceptEdits" || mode == "auto" {
		permissions = mode
	}
	o := m.options(r.session.ID)
	o.Mode, o.Permissions = "default", permissions
	_ = m.store.setting("options:"+r.session.ID, o)
	r.applied.Mode, r.applied.Permissions = "default", permissions
	r.effective.Mode, r.effective.Permissions = "default", permissions
}

// Live returns approvals still awaiting a reply and the running turn's start,
// for a browser that subscribed after they were emitted.
func (m *Manager) Live(id string) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	pending := []map[string]any{}
	out := map[string]any{"pending": pending}
	r := m.find(id)
	if r == nil {
		return out
	}
	keys := make([]string, 0, len(r.pending))
	for k := range r.pending {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		pending = append(pending, m.approvalData(r, k, r.pending[k]))
	}
	out["pending"] = pending
	out["queue"] = append([]queued{}, r.queue...)
	out["queuePaused"] = r.queuePaused
	if r.turn && !r.began.IsZero() {
		out["turnStartedAt"] = r.began.UTC().Format(time.RFC3339Nano)
	}
	return out
}
func (m *Manager) Interrupt(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(id)
	if r == nil || r.proc == nil {
		return errors.New("session is not running")
	}
	var v any
	if r.session.Agent == "codex" {
		if r.turnID == "" {
			return errors.New("no active turn")
		}
		v = map[string]any{"id": token(), "method": "turn/interrupt", "params": map[string]any{"threadId": r.session.NativeID, "turnId": r.turnID}}
	} else {
		request := map[string]any{"subtype": "interrupt"}
		// A stop also drops steered messages still waiting at the CLI; they
		// come back to the workbench queue (claudeLifecycle).
		if len(r.steering) > 0 && r.caps["interrupt_cancel_queued_v1"] {
			request["cancel_queued"] = true
		}
		v = map[string]any{"type": "control_request", "request_id": token(), "request": request}
	}
	if err := r.proc.Input(v); err != nil {
		return err
	}
	r.interrupted = true
	userActed(r)
	m.syncAttention(r)
	return nil
}
func (m *Manager) Stop(id string, sleep bool) error {
	m.mu.Lock()
	r := m.find(id)
	if r == nil {
		m.mu.Unlock()
		return errors.New("session not found")
	}
	if sleep && (r.turn || len(r.pending) > 0 || hasServicePreviews(r) || processBusy(r.proc)) {
		m.mu.Unlock()
		return errors.New("session has active work or previews")
	}
	m.flushDelta(r)
	if len(r.queue) > 0 && !r.queuePaused {
		r.queuePaused = true
		m.emitQueue(r)
	}
	proc := r.proc
	r.proc = nil
	r.ready = false
	r.turn = false
	m.releasePending(r)
	previews := r.previews
	if sleep {
		previews = map[string]*Preview{}
	} else {
		r.previews = map[string]*Preview{}
	}
	m.status(r, "stopping")
	m.mu.Unlock()
	var errs []error
	if proc != nil {
		if err := proc.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, p := range previews {
		if p.stop != nil {
			if err := p.stop(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := "stopped"
	if sleep || m.closing {
		// Put away with the workbench, the session resumes on its next message.
		state = "sleeping"
	}
	if len(errs) > 0 && !m.closing {
		state = "error"
	}
	m.status(r, state)
	return errors.Join(errs...)
}
func (m *Manager) exited(id string, generation int, err error) {
	requested := stoppedOnRequest(err)
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(id)
	if r == nil || r.proc == nil || r.generation != generation {
		return
	}
	m.flushDelta(r)
	if len(r.queue) > 0 && !r.queuePaused {
		r.queuePaused = true
		m.emitQueue(r)
	}
	r.proc = nil
	r.ready = false
	r.turn = false
	m.releasePending(r)
	state := "sleeping"
	if err != nil && !requested && !m.closing {
		state = "error"
		m.emit(r.session.ID, "error", map[string]any{"message": err.Error()})
	}
	m.status(r, state)
}

// stoppedOnRequest reports engine exits that are stops, not crashes: killed
// by SIGTERM or SIGINT, or exiting with 143 or 130 after catching them. When
// the workbench's unit stops, systemd stops the engines bound to it first,
// and Claude Code exits 143 there (Codex exits 0); the systemd-run clients in
// the workbench's own cgroup may be terminated as well.
func stoppedOnRequest(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	switch exit.ExitCode() {
	case 143, 130:
		return true
	case -1:
		s := exit.String()
		return strings.Contains(s, "signal: terminated") || strings.Contains(s, "signal: interrupt")
	}
	return false
}

// releasePending drops the agent's pending requests (approvals, questions)
// when its engine is gone, and tells open pages so their cards go away.
func (m *Manager) releasePending(r *runtime) {
	keys := make([]string, 0, len(r.pending))
	for k := range r.pending {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		m.emit(r.session.ID, "approval_resolved", map[string]any{"id": k})
	}
	r.pending = map[string]any{}
}
func (m *Manager) Sweep() {
	m.mu.Lock()
	ids := []string{}
	for id, r := range m.sessions {
		// A Codex goal that is still active keeps its engine, as the TUI
		// would: Codex starts the next turn itself.
		pursuing := r.session.Agent == "codex" && r.goal != nil && r.goal.Status == "active"
		if r.proc != nil && r.ready && !r.turn && !pursuing && len(r.pending) == 0 && (len(r.queue) == 0 || r.queuePaused) && !hasServicePreviews(r) && !processBusy(r.proc) && time.Since(r.last) >= m.idle {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Stop(id, true)
	}
}

// Shutdown stops the engines and previews when the workbench exits. Under
// systemd the engines' units are bound to the workbench's, so systemd may be
// stopping them already; either way the sessions are asleep afterwards.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.closing = true
	ids := []string{}
	for id, r := range m.sessions {
		if r.proc != nil || len(r.previews) > 0 {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Stop(id, false)
	}
}

// Each engine is its own systemd service; --pipe transports stdio without a SDK host.
type systemProcess struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	unit     string
	mu       sync.Mutex
	baseline map[string]bool
}

func (p *systemProcess) Input(v any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return json.NewEncoder(p.stdin).Encode(v)
}
func (p *systemProcess) Stop() error { return stopUnit(p.unit) }

// systemctl runs a user systemctl command; replaced in tests.
var systemctl = func(args ...string) (string, error) {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// stopUnit stops an engine's unit. A failed stop still counts when the unit
// ends up stopped: when the workbench itself stops, systemd stops the units
// bound to it in the same transaction, and a second stop request can fail.
func stopUnit(unit string) error {
	_, err := systemctl("stop", unit)
	if err == nil {
		return nil
	}
	for range 30 {
		state, _ := systemctl("is-active", unit)
		switch state {
		case "active", "activating", "deactivating", "reloading", "refreshing":
			time.Sleep(100 * time.Millisecond)
		default:
			// inactive, failed, or no longer known: stopped.
			return nil
		}
	}
	return err
}

// unitArgs runs an engine or preview as a transient unit bound to the
// workbench. SuccessExitStatus=143 makes a stop that the program answers by
// exiting 143 (Claude Code catches SIGTERM) a successful stop, as systemd
// already counts a death by SIGTERM: the journal records no failure and
// systemd-run returns 0.
func unitArgs(unit, cwd string) []string {
	return []string{"--user", "--quiet", "--pipe", "--wait", "--collect", "--service-type=exec", "--unit=" + unit, "--working-directory=" + cwd, "--property=KillMode=control-group", "--property=TimeoutStopSec=10s", "--property=SuccessExitStatus=143", "--property=BindsTo=" + Service, "--property=After=" + Service, "--setenv=PATH=" + os.Getenv("PATH")}
}

// agentCommand is the agent binary and arguments for a session's engine.
func agentCommand(s Session) (string, []string, error) {
	binary, err := exec.LookPath(s.Agent)
	if err != nil {
		return "", nil, err
	}
	if s.Agent == "codex" {
		return binary, []string{"app-server"}, nil
	}
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--permission-prompt-tool", "stdio"}
	if s.NativeID != "" {
		args = append(args, "--resume", s.NativeID)
	} else if s.ExplicitName && s.Title != "" {
		args = append(args, "--name", s.Title)
	}
	return binary, args, nil
}

// pump delivers an engine's JSON lines and stderr until it exits. abort ends
// the engine when its output cannot be read; done runs after the exit.
func pump(cmd *exec.Cmd, out, stderr io.Reader, onMessage func(map[string]any), onExit func(error), abort, done func()) {
	go func() {
		scan := bufio.NewScanner(stderr)
		scan.Buffer(make([]byte, 4096), 1024*1024)
		for scan.Scan() {
			onMessage(map[string]any{"type": "stderr", "text": scan.Text()})
		}
	}()
	go func() {
		scan := bufio.NewScanner(out)
		scan.Buffer(make([]byte, 65536), 4*1024*1024)
		for scan.Scan() {
			var v map[string]any
			if err := json.Unmarshal(scan.Bytes(), &v); err == nil {
				onMessage(v)
			}
		}
		scanErr := scan.Err()
		if scanErr != nil {
			abort()
		}
		err := cmd.Wait()
		if scanErr != nil {
			err = scanErr
		}
		done()
		onExit(err)
	}()
}

// DefaultLaunch runs engines as systemd units when the workbench is its own
// systemd service, and as its child processes everywhere else.
func DefaultLaunch() Launch {
	if UnderSystemd() {
		return SystemLaunch
	}
	return DirectLaunch
}

func SystemLaunch(s Session, onMessage func(map[string]any), onExit func(error)) (Process, error) {
	binary, agentArgs, err := agentCommand(s)
	if err != nil {
		return nil, err
	}
	unit := "agent-workbench-session-" + s.ID + ".service"
	args := unitArgs(unit, s.Cwd)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name != "INVOCATION_ID" && name != "SYSTEMD_EXEC_PID" {
			args = append(args, "--setenv="+name)
		}
	}
	args = append(append(args, "--", binary), agentArgs...)
	cmd := exec.Command("systemd-run", args...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	p := &systemProcess{cmd: cmd, stdin: in, unit: unit}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	pump(cmd, out, stderr, onMessage, onExit, func() { _ = stopUnit(unit) }, func() {})
	return p, nil
}

func (p *systemProcess) processes() map[string]bool {
	out := map[string]bool{}
	b, err := exec.Command("systemctl", "--user", "show", p.unit, "--property=ControlGroup", "--value").Output()
	if err != nil {
		return nil
	}
	group := strings.TrimSpace(string(b))
	if group == "" || group == "/" {
		return nil
	}
	root := filepath.Join("/sys/fs/cgroup", group)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "cgroup.procs" {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, id := range strings.Fields(string(data)) {
				out[id] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return out
}
func (p *systemProcess) MarkReady() { p.baseline = p.processes() }
func (p *systemProcess) Busy() bool {
	now := p.processes()
	if now == nil || p.baseline == nil {
		return true
	}
	for id := range now {
		if !p.baseline[id] {
			return true
		}
	}
	return false
}
func processBusy(p Process) bool {
	if v, ok := p.(interface{ Busy() bool }); ok {
		return v.Busy()
	}
	return false
}

func mcpSettled(r *runtime) bool {
	for _, status := range r.mcp {
		if status == "starting" {
			return false
		}
	}
	return true
}

func hasServicePreviews(r *runtime) bool {
	for _, p := range r.previews {
		if p.target != nil {
			return true
		}
	}
	return false
}

func (m *Manager) findUnlocked(id string) *runtime {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.find(id)
}

// Owns is whether pid is one of the unit's processes.
func (p *systemProcess) Owns(pid int) bool {
	return p.processes()[strconv.Itoa(pid)]
}

// retryWithoutAutoReview sends a turn Codex refused because of auto-review
// again with approvals asked of the user, and keeps that choice for the
// session (m.mu held).
func (m *Manager) retryWithoutAutoReview(r *runtime, v map[string]any) bool {
	sent := r.turnRequest
	if sent == nil || str(sent["id"]) != str(v["id"]) {
		return false
	}
	params := obj(sent["params"])
	reason := fmt.Sprint(obj(v["error"])["message"])
	if params["approvalsReviewer"] != "auto_review" || !strings.Contains(strings.ToLower(reason), "review") {
		return false
	}
	params["approvalsReviewer"], params["permissions"] = "user", ":workspace"
	retry := map[string]any{"id": "turn:" + token(), "method": "turn/start", "params": params}
	r.turnRequest = retry
	o := m.options(r.session.ID)
	o.Permissions = ":workspace"
	_ = m.store.setting("options:"+r.session.ID, o)
	m.emit(r.session.ID, "notice", map[string]any{"text": "Codex couldn't use auto-review (" + reason + "); approvals will ask you."})
	return r.proc.Input(retry) == nil
}
