package workbench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Model       string `json:"model,omitempty"`
	Effort      string `json:"effort,omitempty"`
	Speed       string `json:"speed,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Permissions string `json:"permissions,omitempty"`
}
type Telemetry struct {
	Options   Options        `json:"options"`
	Effective Options        `json:"effective"`
	Pending   bool           `json:"pending"`
	Total     map[string]any `json:"total,omitempty"`
	Last      map[string]any `json:"last,omitempty"`
	Context   map[string]any `json:"context,omitempty"`
	Speed     float64        `json:"speed"`
	// While a turn runs: estimated output tokens so far and what the agent
	// is doing (thinking, responding, tool, requesting, compacting).
	TurnTokens float64 `json:"turnTokens,omitempty"`
	Phase      string  `json:"phase,omitempty"`
	// What the turn waits on, in words (an API retry, a hook, a check).
	Detail string `json:"detail,omitempty"`
	// While a message waits for MCP servers (phase "mcp"): how many are up.
	McpDone   int     `json:"mcpDone,omitempty"`
	McpTotal  int     `json:"mcpTotal,omitempty"`
	Average   float64 `json:"average,omitempty"`
	Estimated bool    `json:"estimated"`
	Updated   string  `json:"updated"`
	Scope     string  `json:"scope"`
	// The usage is being read from a compressed record; it follows.
	Reading bool `json:"reading,omitempty"`
	// The session's /goal, if any (goal.go).
	Goal *Goal `json:"goal,omitempty"`
	// What runs outside the turns, and recently ended (background.go).
	Background []BackgroundTask `json:"background,omitempty"`
	// What the user would likely type next (suggest.go).
	Suggestion string `json:"suggestion,omitempty"`
}
type metadata struct {
	Models      []any          `json:"models"`
	Permissions []any          `json:"permissions"`
	Modes       []any          `json:"modes"`
	Usage       map[string]any `json:"usage"`
	UsageError  string         `json:"usageError,omitempty"`
	Defaults    Options        `json:"defaults"` // used where the user set no override
	// Commands the CLI runs itself (Claude), and skills for "$" (both).
	Commands []any `json:"commands,omitempty"`
	Skills   []any `json:"skills,omitempty"`
	// Claude commands that only work in its terminal UI.
	TerminalCommands []string `json:"terminalCommands,omitempty"`
	Updated          string   `json:"updated"`
	Error            string   `json:"error,omitempty"`
	at               time.Time
}

func (s *Store) setting(key string, v any) error {
	b, _ := json.Marshal(v)
	_, err := s.db.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, string(b))
	return err
}
func (s *Store) readSetting(key string, v any) {
	var raw string
	if s.db.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&raw) == nil {
		_ = json.Unmarshal([]byte(raw), v)
	}
}
func (m *Manager) options(id string) Options {
	var o Options
	m.store.readSetting("options:"+id, &o)
	return o
}
func (m *Manager) SetOptions(id string, o Options) error {
	if err := m.Attach(id); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(id)
	if r.session.ReadOnly || m.catalog.Busy(r.session) && r.proc == nil {
		return errors.New("session is active in another client; read-only")
	}
	return m.store.setting("options:"+r.session.ID, o)
}

// Read-only metadata probes do not create threads, send prompts or start MCP servers.
func probe(run agentRun, agent, cwd string) (*exec.Cmd, func(string, any) (map[string]any, error), func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	args := []string{"app-server"}
	if agent == "claude" {
		args = []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`}
	}
	cmd := run.command(args...)
	cmd.Dir = cwd
	in, e := cmd.StdinPipe()
	if e != nil {
		cancel()
		return nil, nil, nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		cancel()
		return nil, nil, nil, e
	}
	group, e := startGroup(cmd)
	if e != nil {
		cancel()
		return nil, nil, nil, e
	}
	unwatch := group.killOn(ctx)
	close := func() {
		in.Close()
		unwatch()
		group.Kill(group.PIDs())
		_ = cmd.Wait()
		group.Release()
		cancel()
	}
	enc := json.NewEncoder(in)
	scan := newLineReader(out)
	call := func(method string, params any) (map[string]any, error) {
		id := token()
		var req any = map[string]any{"id": id, "method": method, "params": params}
		if agent == "claude" {
			p := obj(params)
			p["subtype"] = method
			req = map[string]any{"type": "control_request", "request_id": id, "request": p}
		}
		if e := enc.Encode(req); e != nil {
			return nil, e
		}
		for scan.Scan() {
			var v map[string]any
			if json.Unmarshal(scan.Bytes(), &v) != nil {
				continue
			}
			if agent == "claude" {
				r := obj(v["response"])
				if str(r["request_id"]) != id {
					continue
				}
				if r["subtype"] == "error" {
					return nil, fmt.Errorf("CLI control: %v", r["error"])
				}
				return obj(r["response"]), nil
			}
			if str(v["id"]) == id {
				if v["error"] != nil {
					return nil, fmt.Errorf("CLI RPC: %v", v["error"])
				}
				return obj(v["result"]), nil
			}
		}
		return nil, errors.New("metadata query timed out or CLI exited")
	}
	return cmd, call, close, nil
}
func (m *Manager) Metadata(agent, cwd string) metadata {
	return m.readMetadata(agent, cwd, false, false)
}

func (m *Manager) readMetadata(agent, cwd string, refresh, usageOnly bool) metadata {
	m.metaMu.Lock()
	defer m.metaMu.Unlock()
	key := agent + ":" + cwd
	if k := m.cacheKey(agent); k != "" {
		// Models, skills and permissions come from the program and home.
		key = agent + ":" + k + ":" + cwd
	}
	if usageOnly {
		key += ":usage"
	}
	if m.metadata == nil {
		m.metadata = map[string]metadata{}
	}
	if v, ok := m.metadata[key]; ok && !refresh && time.Since(v.at) < time.Minute {
		return v
	}
	result := metadata{Models: []any{}, Permissions: []any{}, Modes: []any{}, at: time.Now(), Updated: time.Now().UTC().Format(time.RFC3339)}
	run, e := m.cli(agent)
	if e != nil {
		result.Error = e.Error()
		return result
	}
	_, call, close, e := probe(run, agent, cwd)
	if e != nil {
		result.Error = e.Error()
		return result
	}
	defer close()
	if agent == "codex" {
		_, e = call("initialize", map[string]any{"clientInfo": map[string]any{"name": "workbench_metadata", "version": "0.1.0"}, "capabilities": map[string]any{"experimentalApi": true}})
		if e == nil {
			if !usageOnly {
				v, err := call("model/list", map[string]any{"limit": 100})
				if err != nil {
					e = err
				} else {
					result.Models, _ = v["data"].([]any)
				}
				v, _ = call("permissionProfile/list", map[string]any{"cwd": cwd, "limit": 100})
				result.Permissions = withAutoReview(asList(v["data"]))
				v, _ = call("collaborationMode/list", map[string]any{})
				result.Modes, _ = v["data"].([]any)
			}
			var usageErr error
			result.Usage, usageErr = call("account/rateLimits/read", map[string]any{})
			if usageErr != nil {
				result.UsageError = usageErr.Error()
			}
			if !usageOnly {
				config, _ := call("config/read", map[string]any{"cwd": cwd})
				result.Defaults = codexDefaults(obj(config["config"]), result.Models)
				listed, _ := call("skills/list", map[string]any{"cwds": []any{cwd}})
				result.Skills = codexSkills(listed)
			}
		}
	} else {
		v, err := call("initialize", map[string]any{})
		e = err
		result.Models, _ = v["models"].([]any)
		result.Commands, _ = v["commands"].([]any)
		if !usageOnly {
			init := m.claudeInit(cwd)
			for _, name := range init.Skills {
				result.Skills = append(result.Skills, map[string]any{"name": name})
			}
			result.TerminalCommands = init.Terminal
		}
		for _, p := range []string{"manual", "acceptEdits", "auto", "dontAsk", "bypassPermissions"} {
			result.Permissions = append(result.Permissions, map[string]any{"id": p, "name": p})
		}
		result.Modes = []any{map[string]any{"mode": "default", "name": "Execute"}, map[string]any{"mode": "plan", "name": "Plan"}}
		result.Defaults = claudeSettingsDefaults(m.catalog.ClaudeHome, cwd)
		if e == nil {
			var usageErr error
			result.Usage, usageErr = call("get_usage", map[string]any{})
			if usageErr != nil {
				result.UsageError = usageErr.Error()
			}
			m.mu.Lock()
			liveID := ""
			for _, r := range m.sessions {
				if r.session.Agent == agent && r.proc != nil && r.ready {
					liveID = r.session.ID
					break
				}
			}
			m.mu.Unlock()
			if liveID != "" {
				if usage, err := m.rpc(liveID, "get_usage", map[string]any{}); err == nil {
					result.Usage = usage
					result.UsageError = ""
				}
			}
		}
	}
	if e != nil {
		result.Error = e.Error()
	}
	m.metadata[key] = result
	return result
}
func (m *Manager) rpc(id, method string, params map[string]any) (map[string]any, error) {
	m.mu.Lock()
	r := m.find(id)
	if r == nil || r.proc == nil {
		m.mu.Unlock()
		return nil, errors.New("engine is sleeping")
	}
	key := "control:" + token()
	ch := make(chan map[string]any, 1)
	if r.rpc == nil {
		r.rpc = map[string]chan map[string]any{}
	}
	r.rpc[key] = ch
	var v any = map[string]any{"id": key, "method": method, "params": params}
	if r.session.Agent == "claude" {
		params["subtype"] = method
		v = map[string]any{"type": "control_request", "request_id": key, "request": params}
	}
	err := r.proc.Input(v)
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(r.rpc, key); m.mu.Unlock() }()
	if err != nil {
		return nil, err
	}
	select {
	case v := <-ch:
		if v["error"] != nil {
			return nil, fmt.Errorf("CLI control: %v", v["error"])
		}
		return obj(v["result"]), nil
	case <-time.After(20 * time.Second):
		return nil, errors.New("CLI control timed out")
	}
}
func (m *Manager) applyOptions(id string) error {
	m.mu.Lock()
	r := m.find(id)
	o := m.options(r.session.ID)
	agent := r.session.Agent
	previous := r.applied
	m.mu.Unlock()
	if agent == "codex" {
		return nil
	}
	if agent == "claude" {
		if o.Model == "" && previous.Model != "" {
			if _, e := m.rpc(id, "set_model", map[string]any{"model": "default"}); e != nil {
				return e
			}
		}
		if o.Model != "" {
			if _, e := m.rpc(id, "set_model", map[string]any{"model": o.Model}); e != nil {
				return e
			}
		}
		settings := map[string]any{}
		if previous.Effort != "" && o.Effort == "" {
			settings["effortLevel"] = nil
		}
		if previous.Speed != "" && o.Speed == "" {
			settings["fastMode"] = nil
		}
		if o.Effort != "" {
			settings["effortLevel"] = o.Effort
		}
		if o.Speed != "" {
			settings["fastMode"] = o.Speed == "fast"
		}
		if len(settings) > 0 {
			if _, e := m.rpc(id, "apply_flag_settings", map[string]any{"settings": settings}); e != nil {
				return e
			}
		}
		if p := claudePermissionMode(o); p != "" {
			m.mu.Lock()
			r.requestedMode = p
			m.mu.Unlock()
			if _, e := m.rpc(id, "set_permission_mode", map[string]any{"mode": p}); e != nil {
				if p != "auto" {
					return e
				}
				// Auto mode depends on the model, the plan, fast mode and
				// settings; without it, approvals ask.
				m.mu.Lock()
				r.requestedMode = "default"
				m.mu.Unlock()
				if _, e2 := m.rpc(id, "set_permission_mode", map[string]any{"mode": "default"}); e2 != nil {
					return e
				}
				o.Permissions = "manual"
				m.mu.Lock()
				_ = m.store.setting("options:"+r.session.ID, o)
				m.mu.Unlock()
				m.notice(id, "Auto mode isn't available here ("+strings.TrimPrefix(e.Error(), "CLI control: ")+"); approvals will ask you.")
			}
		}
	}
	m.mu.Lock()
	r.applied = o
	r.effective = claudeApplied(r.effective, previous, o)
	m.mu.Unlock()
	return nil
}
func (m *Manager) Telemetry(id string) Telemetry {
	t := m.telemetry(id)
	t.Goal = nil
	m.store.readSetting("goal:"+id, &t.Goal)
	m.mu.Lock()
	if r := m.find(id); r != nil && r.proc != nil {
		t.Background = tasksOf(r)
		t.Suggestion = suggestionOf(r)
	}
	m.mu.Unlock()
	return t
}
func (m *Manager) telemetry(id string) Telemetry {
	o := m.options(id)
	t := Telemetry{Options: o, Estimated: true, Scope: "Native CLI session totals; subagent inclusion follows the provider"}
	m.store.readSetting("telemetry:"+id, &t)
	m.mu.Lock()
	r := m.find(id)
	live := r != nil && r.proc != nil
	m.mu.Unlock()
	if !live {
		native := m.nativeUsage(id)
		if native.Reading {
			t.Reading = true
		} else if native.Total != nil && native.Updated > t.Updated {
			t = native
		}
	}
	t.Options = o
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.find(id); r != nil {
		if r.proc == nil && t.Updated > r.last.UTC().Format(time.RFC3339) {
			t.Pending = o != r.applied
			return t
		}
		if r.effective != (Options{}) {
			t.Effective = r.effective
		}
		t.Pending = o != r.applied
		if r.total != nil {
			t.Total = r.total
		}
		if r.usage != nil {
			t.Last = r.usage
		}
		if r.context != nil {
			t.Context = r.context
		}
		t.Updated = r.last.UTC().Format(time.RFC3339)
		t.Average = r.average
		if r.turn {
			t.TurnTokens, t.Phase, t.Detail = r.turnOut, r.phase, r.detail
		} else if r.phase == "mcp" {
			t.Phase = r.phase
			t.McpDone, t.McpTotal = mcpProgress(r)
		}
		now := time.Now()
		for _, sample := range r.samples {
			if now.Sub(sample.at) <= 5*time.Second {
				t.Speed += sample.tokens / 5
			}
		}
	}
	return t
}

type sample struct {
	at     time.Time
	tokens float64
}

func estimate(text string) float64 {
	n := 0.0
	for _, r := range text {
		if r > 127 {
			n++
		} else {
			n += .25
		}
	}
	return n
}
func (m *Manager) track(r *runtime, v map[string]any) {
	p := obj(v["params"])
	if v["method"] == "model/rerouted" {
		r.effective.Model = str(p["toModel"])
	}
	if str(v["method"]) == "thread/tokenUsage/updated" {
		u := obj(p["tokenUsage"])
		r.total = obj(u["total"])
		r.usage = obj(u["last"])
		r.context = map[string]any{"used": r.usage["inputTokens"], "window": u["modelContextWindow"], "estimated": false}
	}
	trackProgress(r, v)
	trackDetail(r, v)
	if v["type"] == "system" && v["subtype"] == "status" && str(v["permissionMode"]) != "" {
		m.claudeModeReported(r, str(v["permissionMode"]))
	}
	if v["type"] == "system" && v["subtype"] == "init" {
		r.effective = claudeInitOptions(r.effective, v)
		// Skills and terminal-only commands for the "$" and "/" menus.
		m.store.cacheSet(m.claudeInitKey(r.session.Cwd), claudeInitInfo{Skills: strs(v["skills"]), Terminal: strs(v["terminal_slash_commands"])})
	}
	if v["type"] == "assistant" {
		model := str(obj(v["message"])["model"])
		if model != "" {
			r.effective.Model = model
		}
	}
	if v["type"] == "result" {
		r.usage = obj(v["usage"])
		models := obj(v["modelUsage"])
		if len(models) > 0 {
			total := map[string]any{}
			for _, value := range models {
				for k, n := range obj(value) {
					if x, ok := n.(float64); ok && (k == "inputTokens" || k == "outputTokens" || k == "cacheReadInputTokens" || k == "cacheCreationInputTokens") {
						old, _ := total[k].(float64)
						total[k] = old + x
					}
				}
			}
			r.total = total
		}
	}
	for len(r.samples) > 4096 || len(r.samples) > 0 && time.Since(r.samples[0].at) > 5*time.Second {
		r.samples = r.samples[1:]
	}
	if v["type"] == "result" || v["method"] == "turn/completed" {
		out, _ := r.usage["outputTokens"].(float64)
		if v["method"] == "turn/completed" {
			if total, ok := r.total["outputTokens"].(float64); ok {
				out = total - r.baseline
			}
		}
		if out == 0 {
			out, _ = r.usage["output_tokens"].(float64)
		}
		if !r.began.IsZero() {
			r.average = out / time.Since(r.began).Seconds()
		}
		m.store.setting("telemetry:"+r.session.ID, Telemetry{Total: r.total, Last: r.usage, Context: r.context, Effective: r.effective, Average: r.average, Updated: time.Now().UTC().Format(time.RFC3339), Scope: "Native CLI session totals"})
	}
}
func codexOptions(params map[string]any, o Options) {
	if o.Model != "" {
		params["model"] = o.Model
	}
	if o.Effort != "" {
		params["effort"] = o.Effort
	}
	if o.Speed != "" {
		params["serviceTier"] = o.Speed
	}
	if o.Permissions != "" {
		delete(params, "sandbox")
		// Codex keeps a turn's reviewer for later turns, so every choice names
		// its reviewer.
		params["permissions"], params["approvalsReviewer"] = o.Permissions, "user"
		if o.Permissions == codexAutoReview {
			params["permissions"], params["approvalsReviewer"] = ":workspace", "auto_review"
		}
	}
	if o.Mode != "" {
		params["collaborationMode"] = map[string]any{"mode": o.Mode, "settings": map[string]any{"model": o.Model, "reasoning_effort": nil, "developer_instructions": nil}}
		settings := obj(obj(params["collaborationMode"])["settings"])
		if o.Effort != "" {
			settings["reasoning_effort"] = o.Effort
		}
	}
}

// withAutoReview adds the auto-review choice after the workspace profile, as
// the Codex TUI offers "Approve for me" next to its default permissions.
func withAutoReview(profiles []any) []any {
	out := []any{}
	for _, raw := range profiles {
		out = append(out, raw)
		if p := obj(raw); str(p["id"]) == ":workspace" {
			out = append(out, map[string]any{"id": codexAutoReview, "allowed": p["allowed"] != false, "description": "Workspace, with approval requests reviewed by Codex's auto-review instead of you"})
		}
	}
	return out
}

func (m *Manager) ContextUsage(id string) {
	m.mu.Lock()
	r := m.find(id)
	live := r != nil && r.proc != nil && r.ready && !r.turn
	agent := ""
	if r != nil {
		agent = r.session.Agent
	}
	m.mu.Unlock()
	if live && agent == "claude" {
		if v, e := m.rpc(id, "get_context_usage", map[string]any{"detail": "summary"}); e == nil {
			m.mu.Lock()
			r.context = map[string]any{"used": v["totalTokens"], "window": v["maxTokens"], "estimated": true, "categories": v["categories"], "autoCompactThreshold": v["autoCompactThreshold"]}
			m.mu.Unlock()
		}
	}
}

// usageScan accumulates usage from a native record as it grows.
type usageScan struct {
	mu     sync.Mutex
	offset int64
	t      Telemetry
	// Claude: each reply's input, output, cache read and cache write
	// tokens, by message id (a reply is recorded more than once).
	seen map[string][4]float64
	// Last read; the least recently read scan goes first.
	used time.Time
	// The file stamp last read and its result.
	stamp string
	last  Telemetry
}

// scan reads complete records after the last offset. Only lines that can
// carry usage are decoded, so a large rollout is mostly skipped byte-wise.
func (u *usageScan) scan(f *os.File, agent string) error {
	if _, err := f.Seek(u.offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			// A trailing line without a newline may still be being written.
			return nil
		}
		if err != nil {
			return err
		}
		u.offset += int64(len(line))
		u.add(line, agent)
	}
}

var usageKeys = [4]string{"input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"}

// usageLine is whether a record line can carry usage.
func usageLine(line []byte, agent string) bool {
	if agent == "codex" {
		return bytes.Contains(line, []byte(`"token_count"`)) || bytes.Contains(line, []byte(`"turn_context"`))
	}
	return bytes.Contains(line, []byte(`"assistant"`))
}

func (u *usageScan) add(line []byte, agent string) {
	if !usageLine(line, agent) {
		return
	}
	var v map[string]any
	if json.Unmarshal(line, &v) != nil {
		return
	}
	t := &u.t
	p := obj(v["payload"])
	if v["type"] == "turn_context" {
		// The settings Codex used for the latest turn.
		t.Effective = codexTurnOptions(p)
	}
	if p["type"] == "token_count" {
		info := obj(p["info"])
		t.Total = obj(info["total_token_usage"])
		t.Last = obj(info["last_token_usage"])
		t.Context = map[string]any{"used": t.Last["input_tokens"], "window": info["model_context_window"], "estimated": false}
	}
	if agent == "claude" && v["type"] == "assistant" && v["isSidechain"] != true {
		msg := obj(v["message"])
		if key := str(msg["id"]); key != "" {
			usage := obj(msg["usage"])
			var counts [4]float64
			for i, k := range usageKeys {
				counts[i], _ = usage[k].(float64)
			}
			u.seen[key] = counts
			t.Effective.Model = str(msg["model"])
			t.Last = obj(msg["usage"])
		}
	}
}

func (u *usageScan) result() Telemetry {
	t := u.t
	t.Scope = "Main conversation (native history)"
	if len(u.seen) > 0 {
		var sum [4]float64
		for _, counts := range u.seen {
			for i := range sum {
				sum[i] += counts[i]
			}
		}
		t.Total = map[string]any{}
		for i, k := range usageKeys {
			t.Total[k] = sum[i]
		}
	}
	return t
}

// nativeUsage reads usage from the native record. Results are cached by file
// size and time; a record that grows (another client is writing it) is read
// incrementally.
func (m *Manager) nativeUsage(id string) Telemetry {
	t := Telemetry{Scope: "Main conversation (native history)", Estimated: false}
	s, ok := m.catalog.Find(id)
	if !ok {
		return t
	}
	file, ok := m.catalog.record(id)
	if !ok {
		return t
	}
	path := file.Path
	info, e := os.Stat(path)
	if e != nil {
		return t
	}
	key := "history_usage:" + id
	// The version prefix invalidates summaries cached by older builds.
	stamp := fmt.Sprintf("v2:%d:%d", info.Size(), info.ModTime().UnixNano())
	if file.Zstd {
		stamp = "z" + stamp
	}
	m.usageMu.Lock()
	u := m.usage[id]
	m.usageMu.Unlock()
	if u == nil {
		var cached struct {
			Stamp string
			Data  Telemetry
		}
		m.store.cacheGet(key, &cached)
		if cached.Stamp == stamp {
			return cached.Data
		}
		if file.Zstd {
			return m.compressedUsage(id, s.Agent, file, info.ModTime(), stamp, key)
		}
		m.usageMu.Lock()
		if m.usage == nil {
			m.usage = map[string]*usageScan{}
		}
		if u = m.usage[id]; u == nil {
			// A few sessions are followed at once: the least recently
			// read gives way.
			if len(m.usage) >= maxUsageScans {
				oldest := ""
				for k, v := range m.usage {
					if oldest == "" || v.used.Before(m.usage[oldest].used) {
						oldest = k
					}
				}
				delete(m.usage, oldest)
			}
			u = &usageScan{seen: map[string][4]float64{}}
			m.usage[id] = u
		}
		m.usageMu.Unlock()
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.used = time.Now()
	if u.stamp == stamp {
		return u.last
	}
	if info.Size() < u.offset {
		// Rewritten or truncated: start over.
		u.offset, u.t, u.seen = 0, Telemetry{}, map[string][4]float64{}
	}
	f, e := os.Open(path)
	if e != nil {
		return t
	}
	defer f.Close()
	if u.scan(f, s.Agent) != nil {
		return t
	}
	t = u.result()
	t.Updated = info.ModTime().UTC().Format(time.RFC3339)
	u.stamp, u.last = stamp, t
	m.store.cacheSet(key, struct {
		Stamp string
		Data  Telemetry
	}{stamp, t})
	return t
}

// One compressed record is read at a time: the largest decompress to
// gigabytes.
var compressedReads = make(chan struct{}, 1)

// Sessions whose usage is read from their records as they grow.
const maxUsageScans = 8

// compressedUsage reads usage from a compressed record in the background,
// once: it does not change, so the summary is kept until it does. Until
// then the telemetry says it is being read.
func (m *Manager) compressedUsage(id, agent string, file recordFile, mod time.Time, stamp, key string) Telemetry {
	m.usageMu.Lock()
	if m.usageReading == nil {
		m.usageReading = map[string]time.Time{}
	}
	busy := !m.usageReading[id].IsZero()
	// When the page last asked: a read nobody waits for any more is
	// skipped when its turn comes (the next look asks again).
	m.usageReading[id] = time.Now()
	m.usageMu.Unlock()
	if !busy {
		go func() {
			defer func() {
				m.usageMu.Lock()
				delete(m.usageReading, id)
				m.usageMu.Unlock()
			}()
			compressedReads <- struct{}{}
			defer func() { <-compressedReads }()
			m.usageMu.Lock()
			asked := m.usageReading[id]
			m.usageMu.Unlock()
			if time.Since(asked) > 30*time.Second {
				return
			}
			r, err := file.open()
			if err != nil {
				return
			}
			defer r.Close()
			u := &usageScan{seen: map[string][4]float64{}}
			if _, err := eachRecordLine(r, true, func(line []byte) bool { return usageLine(line, agent) }, func(line []byte) { u.add(line, agent) }); err != nil {
				return
			}
			t := u.result()
			t.Updated = mod.UTC().Format(time.RFC3339)
			m.store.cacheSet(key, struct {
				Stamp string
				Data  Telemetry
			}{stamp, t})
		}()
	}
	return Telemetry{Scope: "Main conversation (native history)", Reading: true}
}

// claudeInitInfo is what Claude's system/init lists for one directory.
type claudeInitInfo struct {
	Skills   []string `json:"skills"`
	Terminal []string `json:"terminal"`
}

// claudeInit returns the skills Claude lists for cwd: from the last managed
// turn there, or else from a probe that runs the local /context command
// without saving a session (no model call: verified cost 0, no transcript).
func (m *Manager) claudeInit(cwd string) claudeInitInfo {
	var info claudeInitInfo
	key := m.claudeInitKey(cwd)
	m.store.cacheGet(key, &info)
	if info.Skills != nil {
		return info
	}
	run, err := m.cli("claude")
	if err != nil {
		return info
	}
	if probed, err := probeClaudeInit(run, cwd); err == nil {
		info = probed
		m.store.cacheSet(key, info)
	}
	return info
}

// claudeInitKey keeps what Claude lists per directory, and per program and
// home when Settings chose them: skills and commands come from the home.
func (m *Manager) claudeInitKey(cwd string) string {
	if k := m.cacheKey("claude"); k != "" {
		return "claude-init:" + k + ":" + cwd
	}
	return "claude-init:" + cwd
}

func probeClaudeInit(run agentRun, cwd string) (claudeInitInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := run.command("-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--no-session-persistence", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`)
	cmd.Dir = cwd
	in, err := cmd.StdinPipe()
	if err != nil {
		return claudeInitInfo{}, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return claudeInitInfo{}, err
	}
	group, err := startGroup(cmd)
	if err != nil {
		return claudeInitInfo{}, err
	}
	unwatch := group.killOn(ctx)
	defer func() { in.Close(); unwatch(); group.Kill(group.PIDs()); _ = cmd.Wait(); group.Release() }()
	enc := json.NewEncoder(in)
	_ = enc.Encode(map[string]any{"type": "control_request", "request_id": "init", "request": map[string]any{"subtype": "initialize"}})
	_ = enc.Encode(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "/context"}, "parent_tool_use_id": nil, "session_id": ""})
	scan := newLineReader(out)
	for scan.Scan() {
		var v map[string]any
		if json.Unmarshal(scan.Bytes(), &v) == nil && v["type"] == "system" && v["subtype"] == "init" {
			return claudeInitInfo{Skills: strs(v["skills"]), Terminal: strs(v["terminal_slash_commands"])}, nil
		}
	}
	return claudeInitInfo{}, errors.New("claude did not report its skills")
}

// codexSkills keeps the enabled skills from a skills/list result.
func codexSkills(listed map[string]any) []any {
	out := []any{}
	for _, entry := range asList(listed["data"]) {
		for _, raw := range asList(obj(entry)["skills"]) {
			sk := obj(raw)
			if sk["enabled"] == false || str(sk["name"]) == "" {
				continue
			}
			out = append(out, map[string]any{"name": sk["name"], "description": sk["description"], "path": sk["path"]})
		}
	}
	return out
}

func strs(v any) []string {
	out := []string{}
	for _, x := range asList(v) {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// claudeModeReported follows a permission mode Claude Code reports. When it
// leaves auto mode on its own (the model, plan, fast mode or a failing safety
// check no longer allow it), the session's setting follows, so the next turn
// does not ask for auto mode again (m.mu held).
func (m *Manager) claudeModeReported(r *runtime, mode string) {
	r.effective.Mode, r.effective.Permissions = claudePermissionOptions(mode)
	if mode == r.requestedMode || mode == "auto" || mode == "plan" || r.applied.Permissions != "auto" || r.applied.Mode == "plan" {
		return
	}
	_, permissions := claudePermissionOptions(mode)
	o := m.options(r.session.ID)
	o.Permissions = permissions
	_ = m.store.setting("options:"+r.session.ID, o)
	r.applied.Permissions = permissions
	m.emit(r.session.ID, "notice", map[string]any{"text": "Claude Code left auto mode; approvals will ask you."})
}
