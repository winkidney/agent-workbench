package workbench

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type NativeCatalog struct {
	mu                    sync.Mutex
	cached                []Session
	paths                 map[string]string
	updated               time.Time
	CodexHome, ClaudeHome string
	// Where Codex keeps its databases, when not in its home
	// (CODEX_SQLITE_HOME).
	CodexSQLiteHome string
	// startCodex starts a read-only app-server (the manager's chosen
	// program and home); replaced in tests.
	startCodex func(name string) (*codexClient, error)
	// What the last refreshes read: Codex's threads for its index's stamp,
	// Claude's transcripts by path for their size and time, and the list's
	// positions by id.
	codexRows   []Session
	codexPaths  map[string]string
	codexSeen   string
	claudeFiles map[string]claudeFile
	claudeOrder []string
	byID        map[string]int
}

func NewNativeCatalog() *NativeCatalog {
	home, _ := os.UserHomeDir()
	a, b := os.Getenv("CODEX_HOME"), os.Getenv("CLAUDE_CONFIG_DIR")
	if a == "" {
		a = filepath.Join(home, ".codex")
	}
	if b == "" {
		b = filepath.Join(home, ".claude")
	}
	return &NativeCatalog{CodexHome: a, ClaudeHome: b, CodexSQLiteHome: os.Getenv("CODEX_SQLITE_HOME"), paths: map[string]string{}, startCodex: startCodexClient}
}

// codexStatePath is Codex's thread index.
func (c *NativeCatalog) codexStatePath() string {
	dir := c.CodexSQLiteHome
	if dir == "" {
		dir = c.CodexHome
	}
	return filepath.Join(dir, "state_5.sqlite")
}

// codexState opens Codex's thread index read-only.
func (c *NativeCatalog) codexState() (*sql.DB, error) {
	c.mu.Lock()
	path := c.codexStatePath()
	c.mu.Unlock()
	return openCodexState(path)
}

func openCodexState(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}
func nativeKey(agent, id string) string { return agent + ":" + id }
func (c *NativeCatalog) List() ([]Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.refreshLocked(); err != nil {
		return nil, err
	}
	return append([]Session{}, c.cached...), nil
}

// refreshLocked reads the native records again, at most every 5 s, and
// only what changed: Codex's index when its database did, a Claude
// transcript when its size or time did (c.mu held).
func (c *NativeCatalog) refreshLocked() error {
	if time.Since(c.updated) < 5*time.Second && c.cached != nil {
		return nil
	}
	codexChanged, err := c.codexRowsLocked()
	if err != nil {
		return err
	}
	claudeChanged := c.claudeRowsLocked()
	c.updated = time.Now()
	if !codexChanged && !claudeChanged && c.cached != nil {
		return nil
	}
	out := make([]Session, 0, len(c.codexRows)+len(c.claudeOrder))
	out = append(out, c.codexRows...)
	paths := make(map[string]string, cap(out))
	for id, p := range c.codexPaths {
		paths[id] = p
	}
	for _, f := range c.claudeOrder {
		e := c.claudeFiles[f]
		if !e.ok {
			continue
		}
		out = append(out, e.s)
		paths[e.s.ID] = f
	}
	namespaces := map[string]string{}
	for i := range out {
		cwd := out[i].Cwd
		key, ok := namespaces[cwd]
		if !ok {
			key = workspaceNamespace(cwd)
			namespaces[cwd] = key
		}
		out[i].Workspace = key
		out[i].Origin = sessionOrigin(out[i])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	byID := make(map[string]int, len(out))
	for i, s := range out {
		byID[s.ID] = i
	}
	c.cached, c.paths, c.byID = out, paths, byID
	return nil
}

// codexStamp tells whether Codex's index changed: its database and log.
func codexStamp(path string) string {
	stamp := ""
	for _, p := range []string{path, path + "-wal"} {
		if info, err := os.Stat(p); err == nil {
			stamp += fmt.Sprintf("%d:%d;", info.Size(), info.ModTime().UnixNano())
		}
	}
	return stamp
}

// codexRowsLocked reads Codex's threads when its index changed.
func (c *NativeCatalog) codexRowsLocked() (bool, error) {
	path := c.codexStatePath()
	stamp := path + "|" + codexStamp(path)
	if stamp == c.codexSeen && c.codexRows != nil {
		return false, nil
	}
	rows, paths := []Session{}, map[string]string{}
	if db, err := openCodexState(path); err == nil {
		defer db.Close()
		// Archived threads too: the sidebar shows them on request.
		list, err := db.Query(`SELECT id,cwd,coalesce(nullif(name,''),title,''),updated_at,source,rollout_path,archived FROM threads ORDER BY updated_at DESC`)
		if err != nil {
			return false, fmt.Errorf("unsupported Codex metadata: %w", err)
		}
		defer list.Close()
		for list.Next() {
			var s Session
			// Any column may be NULL in a row Codex is still filling in.
			var cwd, title, source, rollout sql.NullString
			var timestamp, archived sql.NullInt64
			if err := list.Scan(&s.NativeID, &cwd, &title, &timestamp, &source, &rollout, &archived); err != nil || s.NativeID == "" {
				continue
			}
			s.Cwd, s.Title = cwd.String, title.String
			s.Archived = archived.Int64 != 0
			s.Agent = "codex"
			s.ID = nativeKey(s.Agent, s.NativeID)
			s.Updated = time.Unix(timestamp.Int64, 0).UTC().Format(time.RFC3339)
			s.Status = "sleeping"
			s.Source = source.String
			if strings.Contains(s.Source, "subagent") {
				var value map[string]any
				_ = json.Unmarshal([]byte(s.Source), &value)
				spawn := obj(obj(obj(value["subagent"])["thread_spawn"]))
				s.ParentID = str(spawn["parent_thread_id"])
				if s.ParentID == "" {
					continue
				}
				// Untitled: named as the parent's row names it (its path).
				if s.Title == "" {
					if p := str(spawn["agent_path"]); p != "" {
						s.Title = p[strings.LastIndex(p, "/")+1:]
					} else {
						s.Title = str(spawn["agent_nickname"])
					}
				}
				s.ParentID = nativeKey("codex", s.ParentID)
			}
			paths[s.ID] = rollout.String
			rows = append(rows, s)
		}
	}
	c.codexRows, c.codexPaths, c.codexSeen = rows, paths, stamp
	return true, nil
}

// claudeFile is what a Claude transcript said, for its size and time.
type claudeFile struct {
	size, mod int64
	// A subagent's: its parent, which names it.
	parent, parentCwd string
	s                 Session
	ok                bool
}

// claudeRowsLocked reads the Claude transcripts that are new or changed;
// the others are as last read. Transcripts gone are forgotten.
func (c *NativeCatalog) claudeRowsLocked() bool {
	seen := make(map[string]claudeFile, len(c.claudeFiles))
	order := make([]string, 0, len(c.claudeOrder))
	changed := false
	look := func(path string, read func(os.FileInfo) claudeFile) (claudeFile, bool) {
		info, err := os.Stat(path)
		if err != nil {
			return claudeFile{}, false
		}
		e, ok := c.claudeFiles[path]
		if !ok || e.size != info.Size() || e.mod != info.ModTime().UnixNano() {
			e = read(info)
			e.size, e.mod = info.Size(), info.ModTime().UnixNano()
			changed = true
		}
		seen[path] = e
		order = append(order, path)
		return e, true
	}
	directories, _ := os.ReadDir(filepath.Join(c.ClaudeHome, "projects"))
	for _, dir := range directories {
		if !dir.IsDir() {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(c.ClaudeHome, "projects", dir.Name(), "*.jsonl"))
		for _, path := range files {
			e, ok := look(path, func(os.FileInfo) claudeFile {
				s, ok := readClaudeMetadata(path)
				return claudeFile{s: s, ok: ok}
			})
			if !ok || !e.ok {
				continue
			}
			parent := e.s
			agents, _ := filepath.Glob(filepath.Join(c.ClaudeHome, "projects", dir.Name(), parent.NativeID, "subagents", "agent-*.jsonl"))
			for _, agent := range agents {
				if old, ok := c.claudeFiles[agent]; ok && (old.parent != parent.ID || old.parentCwd != parent.Cwd) {
					delete(c.claudeFiles, agent)
				}
				look(agent, func(os.FileInfo) claudeFile {
					return claudeFile{s: readClaudeSubagent(agent, parent), ok: true, parent: parent.ID, parentCwd: parent.Cwd}
				})
			}
		}
	}
	if len(seen) != len(c.claudeFiles) {
		changed = true
	}
	c.claudeFiles, c.claudeOrder = seen, order
	return changed
}

// readClaudeSubagent lists a Task / subagent transcript as a read-only child
// of the session that launched it.
func readClaudeSubagent(path string, parent Session) Session {
	agent := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	child := Session{
		Agent:    "claude",
		NativeID: parent.NativeID + ":" + agent,
		ParentID: parent.ID,
		Cwd:      parent.Cwd,
		Status:   "sleeping",
		Source:   "subagent",
		ReadOnly: true,
		Title:    agent,
	}
	child.ID = nativeKey("claude", child.NativeID)
	if info, err := os.Stat(path); err == nil {
		child.Updated = info.ModTime().UTC().Format(time.RFC3339)
	}
	var meta struct{ Description, AgentType string }
	if b, err := os.ReadFile(strings.TrimSuffix(path, ".jsonl") + ".meta.json"); err == nil && json.Unmarshal(b, &meta) == nil {
		if meta.Description != "" {
			child.Title = meta.Description
		} else if meta.AgentType != "" {
			child.Title = meta.AgentType
		}
	}
	return child
}

var temporaryRoots = []string{os.TempDir(), "/tmp", "/var/tmp"}

// pathOrigin classifies a launch directory: temporary directories and the
// home / Codex app chat folders are not projects.
func pathOrigin(ns string) string {
	within := func(root string) bool {
		if root == "" {
			return false
		}
		root = filepath.Clean(root)
		return ns == root || strings.HasPrefix(ns, root+string(filepath.Separator))
	}
	for _, tmp := range temporaryRoots {
		if within(tmp) || within(workspaceNamespace(tmp)) {
			return "temporary"
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		home = workspaceNamespace(home)
		if ns == home || within(filepath.Join(home, "Documents", "Codex")) {
			return "chat"
		}
	}
	return "project"
}

func sessionOrigin(s Session) string {
	if s.Agent == "codex" && s.Source == "exec" {
		return "automation"
	}
	return pathOrigin(s.Workspace)
}

func readClaudeMetadata(path string) (Session, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Session{}, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Session{}, false
	}
	id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if strings.HasPrefix(id, "agent-") {
		return Session{}, false
	}
	s := Session{Agent: "claude", NativeID: id, ID: nativeKey("claude", id), Status: "sleeping", Updated: info.ModTime().UTC().Format(time.RFC3339), Source: "cli"}
	var reader io.Reader = file
	if info.Size() > 262144 {
		head := make([]byte, 131072)
		n, _ := file.ReadAt(head, 0)
		tail := make([]byte, 131072)
		k, _ := file.ReadAt(tail, info.Size()-131072)
		reader = io.MultiReader(strings.NewReader(string(head[:n])+"\n"), strings.NewReader(string(tail[:k])))
	}
	scan := bufio.NewScanner(reader)
	scan.Buffer(make([]byte, 65536), 4*1024*1024)
	for scan.Scan() {
		var v map[string]any
		if json.Unmarshal(scan.Bytes(), &v) != nil {
			continue
		}
		// The first recorded cwd is the launch directory; later records follow `cd`s.
		if cwd := str(v["cwd"]); cwd != "" && s.Cwd == "" {
			s.Cwd = cwd
		}
		if str(v["type"]) == "custom-title" {
			s.Title = str(v["customTitle"])
		}
		if s.Title == "" && v["type"] == "user" && v["isMeta"] != true {
			content := obj(v["message"])["content"]
			s.Title = sessionTitle(nativeText(content))
		}
	}
	if s.Title == "" {
		s.Title = id
	}
	if s.Cwd == "" {
		s.Cwd = decodeClaudeProject(filepath.Base(filepath.Dir(path)))
	}
	return s, s.Cwd != ""
}

var claudeUnsafe = regexp.MustCompile(`[^A-Za-z0-9]`)

// claudeProjectName is how Claude Code names a project folder after its launch directory.
func claudeProjectName(dir string) string { return claudeUnsafe.ReplaceAllString(dir, "-") }

// decodeClaudeProject resolves a project folder name back to an existing
// directory. The encoding is lossy, so the result must encode back exactly.
func decodeClaudeProject(name string) string {
	var walk func(dir, rest string) string
	walk = func(dir, rest string) string {
		if rest == "" {
			return dir
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return ""
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			enc := claudeProjectName(e.Name())
			if rest == enc {
				return filepath.Join(dir, e.Name())
			}
			if strings.HasPrefix(rest, enc+"-") {
				if found := walk(filepath.Join(dir, e.Name()), rest[len(enc)+1:]); found != "" {
					return found
				}
			}
		}
		return ""
	}
	if !strings.HasPrefix(name, "-") {
		return ""
	}
	found := walk("/", name[1:])
	if found == "" || claudeProjectName(found) != name {
		return ""
	}
	return found
}

// Files returns the native record of each listed session that has one.
func (c *NativeCatalog) Files() map[string]recordFile {
	if _, err := c.List(); err != nil {
		return nil
	}
	c.mu.Lock()
	paths := make(map[string]string, len(c.paths))
	for id, p := range c.paths {
		paths[id] = p
	}
	home := c.CodexHome
	c.mu.Unlock()
	out := make(map[string]recordFile, len(paths))
	for id, p := range paths {
		if f, ok := recordOf(home, id, p); ok {
			out[id] = f
		}
	}
	return out
}

// record is a listed session's native record as it is on disk now.
func (c *NativeCatalog) record(id string) (recordFile, bool) {
	c.mu.Lock()
	p, home := c.paths[id], c.CodexHome
	c.mu.Unlock()
	return recordOf(home, id, p)
}

func recordOf(codexHome, id, recorded string) (recordFile, bool) {
	if strings.HasPrefix(id, "codex:") {
		return codexRecord(codexHome, recorded)
	}
	if recorded == "" || !regularFile(recorded) {
		return recordFile{}, false
	}
	return recordFile{Path: recorded}, true
}

// IndexActivity runs one bounded pass of the related-project index.
func (m *Manager) IndexActivity(budget int64) {
	list, err := m.catalog.List()
	if err != nil {
		return
	}
	m.activity.Scan(list, m.catalog.Files(), budget)
}

// path is a listed session's native record (a Claude transcript).
func (c *NativeCatalog) path(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paths[id]
}

// Find looks a session up by id, without copying the list.
func (c *NativeCatalog) Find(id string) (Session, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refreshLocked() != nil {
		return Session{}, false
	}
	if i, ok := c.byID[id]; ok {
		return c.cached[i], true
	}
	return Session{}, false
}
func (c *NativeCatalog) Busy(s Session) bool {
	if s.Agent == "codex" {
		return fileLocked(filepath.Join(c.CodexHome, "thread-writer-locks", s.NativeID+".lock"))
	}
	return c.ClaudeBusyExcept(s, nil)
}

// ClaudeBusyExcept is whether a live Claude process other than ours (the
// processes of the engine this workbench runs) holds the session.
func (c *NativeCatalog) ClaudeBusyExcept(s Session, ours func(pid int) bool) bool {
	_, ok := c.claudeOwner(s, ours)
	return ok
}

// ClaudeStatus is what the Claude process holding the session reports about
// itself ("busy" while a turn runs, "idle" while it waits), or "" when no
// live process holds it or it does not say.
func (c *NativeCatalog) ClaudeStatus(s Session) string {
	status, _ := c.claudeOwner(s, nil)
	return status
}

// claudeProcessNames are what a Claude process is called: the native binary
// or node. Off Linux, where Claude's recorded start time cannot be compared,
// the name guards against a reused PID.
var claudeProcessNames = []string{"claude", "node"}

// claudeOwner finds the live Claude process registered for the session.
func (c *NativeCatalog) claudeOwner(s Session, ours func(pid int) bool) (string, bool) {
	files, _ := filepath.Glob(filepath.Join(c.ClaudeHome, "sessions", "*.json"))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var v struct {
			PID       int             `json:"pid"`
			ProcStart json.RawMessage `json:"procStart"`
			SessionID string          `json:"sessionId"`
			Status    string          `json:"status"`
		}
		if json.Unmarshal(data, &v) != nil || v.SessionID != s.NativeID || v.PID <= 0 || (ours != nil && ours(v.PID)) {
			continue
		}
		if processMatches(v.PID, strings.Trim(string(v.ProcStart), "\"")) {
			return v.Status, true
		}
	}
	return "", false
}
func (c *NativeCatalog) History(s Session, before string, limit int) (HistoryPage, error) {
	_, err := c.List()
	if err != nil {
		return HistoryPage{}, err
	}
	if s.Agent == "codex" {
		page, err := c.codexHistory(s, before, limit)
		if err != nil {
			// Codex lists threads whose records are gone (deleted, or from
			// a home that no longer has them): say so instead of failing.
			file, ok := c.record(s.ID)
			if !ok {
				if gone, ok := c.codexRemains(s.NativeID); ok {
					return HistoryPage{Events: []Event{}, RecordMissing: &gone}, nil
				}
			}
			// Its index names the record under another home: Codex itself
			// cannot open it there.
			c.mu.Lock()
			recorded := c.paths[s.ID]
			c.mu.Unlock()
			if ok && recorded != "" && filepath.IsAbs(recorded) && !strings.HasPrefix(file.Path, recorded) {
				return page, fmt.Errorf("Codex cannot open this conversation: its index names the record as %s, which is not where it is now (%s). %w", recorded, file.Path, err)
			}
		}
		return page, err
	}
	c.mu.Lock()
	path := c.paths[s.ID]
	c.mu.Unlock()
	if path == "" {
		return HistoryPage{}, errors.New("native transcript not found")
	}
	return readClaudeHistory(path, s, before, limit)
}

func nativeText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	parts, _ := v.([]any)
	text := []string{}
	for _, part := range parts {
		p := obj(part)
		switch str(p["type"]) {
		case "text", "input_text", "output_text":
			text = append(text, str(p["text"]))
		case "tool_use":
			b, _ := json.Marshal(p["input"])
			text = append(text, "["+str(p["name"])+"] "+string(b))
		case "tool_result":
			text = append(text, nativeText(p["content"]))
		}
	}
	return strings.Join(text, "\n")
}

type codexRequest func(id, method string, params any) (map[string]any, error)

// codexClient is a read-only app-server connection. It never resumes a
// thread or calls a model; Close kills its process group.
type codexClient struct {
	cmd   *exec.Cmd
	group *procGroup
	in    io.WriteCloser
	enc   *json.Encoder
	lines chan map[string]any
	// readErr is why reading its output stopped, set before lines closes.
	readErr error
	mu      sync.Mutex
}

// startCodexClient starts the codex on PATH, as the workbench's own
// environment has it.
func startCodexClient(name string) (*codexClient, error) {
	run, err := runFor("codex", AgentSetup{})
	if err != nil {
		return nil, err
	}
	return startCodexClientWith(run, name)
}

func startCodexClientWith(run agentRun, name string) (*codexClient, error) {
	cmd := run.command("app-server")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	group, err := startGroup(cmd)
	if err != nil {
		return nil, err
	}
	c := &codexClient{cmd: cmd, group: group, in: in, enc: json.NewEncoder(in), lines: make(chan map[string]any, 16)}
	go func() {
		scan := newLineReader(out)
		for scan.Scan() {
			var v map[string]any
			// Only responses are read; notifications are dropped.
			if json.Unmarshal(scan.Bytes(), &v) == nil && v["id"] != nil {
				c.lines <- v
			}
		}
		// Read by request only after lines is closed.
		c.readErr = scan.Err()
		close(c.lines)
	}()
	if _, err := c.request("init", "initialize", map[string]any{"clientInfo": map[string]any{"name": name, "version": "0.1.0"}}); err != nil {
		c.Close()
		return nil, err
	}
	_ = c.enc.Encode(map[string]any{"method": "initialized"})
	return c, nil
}

func (c *codexClient) request(id, method string, params any) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enc.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	timeout := time.After(20 * time.Second)
	for {
		select {
		case v, ok := <-c.lines:
			if !ok {
				if c.readErr != nil {
					return nil, fmt.Errorf("reading codex app-server: %w", c.readErr)
				}
				return nil, errors.New("codex app-server exited")
			}
			if str(v["id"]) != id {
				continue
			}
			if v["error"] != nil {
				return nil, fmt.Errorf("codex %s: %v", method, v["error"])
			}
			return obj(v["result"]), nil
		case <-timeout:
			return nil, fmt.Errorf("codex %s timed out", method)
		}
	}
}

func (c *codexClient) Close() {
	c.in.Close()
	c.group.Kill(c.group.PIDs())
	_ = c.cmd.Wait()
	c.group.Release()
	// Unblock the reader if it is waiting to deliver a late response.
	for range c.lines {
	}
}

// withCodexClient runs fn against a short-lived app-server.
func (c0 *NativeCatalog) withCodexClient(name string, fn func(codexRequest) error) error {
	c, err := c0.startCodex(name)
	if err != nil {
		return err
	}
	defer c.Close()
	return fn(c.request)
}

// A short-lived read-only protocol client: never resumes a thread or calls a model.
func (c *NativeCatalog) codexHistory(s Session, before string, limit int) (HistoryPage, error) {
	var page HistoryPage
	err := c.withCodexClient("workbench_history", func(request codexRequest) error {
		var e error
		page, e = codexHistoryPage(request, s, before, limit)
		return e
	})
	return page, err
}

func codexHistoryPage(request codexRequest, s Session, before string, limit int) (HistoryPage, error) {
	// Turns are listed newest first; the opaque Codex cursor pages toward older turns.
	var turns []any
	cursor := strings.TrimPrefix(before, "codex:")
	legacy := strings.HasPrefix(before, "legacy:")
	next := ""
	if !legacy {
		count := 0
		for {
			params := map[string]any{"threadId": s.NativeID, "limit": 10, "sortDirection": "desc", "itemsView": "full"}
			if cursor != "" {
				params["cursor"] = cursor
			}
			page, e := request("turns", "thread/turns/list", params)
			if e != nil {
				if cursor != "" {
					return HistoryPage{}, e
				}
				legacy = true
				break
			}
			data, _ := page["data"].([]any)
			for _, turn := range data {
				turns = append(turns, turn)
				items, _ := obj(turn)["items"].([]any)
				count += len(items) + 1
			}
			next = str(page["nextCursor"])
			cursor = next
			if next == "" || count >= limit {
				break
			}
		}
		for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
			turns[i], turns[j] = turns[j], turns[i]
		}
	}
	if legacy {
		read, e := request("legacy", "thread/read", map[string]any{"threadId": s.NativeID, "includeTurns": true})
		if e != nil {
			return HistoryPage{}, e
		}
		turns, _ = obj(read["thread"])["turns"].([]any)
	}
	events := codexTurnEvents(s, turns)
	if !legacy {
		events = withCodexSubagents(request, s, turns, events)
	}
	if legacy {
		start, end := pageBounds(len(events), strings.TrimPrefix(before, "legacy:"), limit)
		page := HistoryPage{Events: events[start:end], HasMore: start > 0}
		if start > 0 {
			page.Before = "legacy:" + strconv.Itoa(start)
		}
		return page, nil
	}
	page := HistoryPage{Events: events, HasMore: next != ""}
	if next != "" {
		page.Before = "codex:" + next
	}
	return page, nil
}

// codexTurnEvents renders chronological Codex turns as history events: each
// item, then the turn summary.
func codexTurnEvents(s Session, turns []any) []Event {
	events := []Event{}
	for _, raw := range turns {
		turn := obj(raw)
		items, _ := turn["items"].([]any)
		prompted := false
		for _, item := range items {
			bounded := boundJSON(item)
			// A user message after the turn's first was steered into it.
			if v, ok := bounded.(map[string]any); ok && v["type"] == "userMessage" {
				if prompted {
					v["workbenchSteered"] = true
				}
				prompted = true
			}
			b, _ := json.Marshal(map[string]any{"method": "item/completed", "history": true, "params": map[string]any{"turnId": turn["id"], "item": bounded}})
			events = append(events, Event{Session: s.ID, Kind: "native", Data: b})
		}
		summary := map[string]any{"id": turn["id"], "status": turn["status"], "error": turn["error"], "durationMs": turn["durationMs"]}
		b, _ := json.Marshal(map[string]any{"method": "turn/completed", "history": true, "params": map[string]any{"turn": summary}})
		events = append(events, Event{Session: s.ID, Kind: "native", Data: b})
	}
	return events
}

func (m *Manager) find(id string) *runtime {
	if r := m.sessions[id]; r != nil {
		return r
	}
	for _, r := range m.sessions {
		if r.session.ID == id {
			return r
		}
	}
	return nil
}
func (m *Manager) Attach(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.find(id) != nil {
		return nil
	}
	s, ok := m.catalog.Find(id)
	if !ok {
		return errors.New("native session not found")
	}
	m.sessions[id] = &runtime{session: s, pending: map[string]any{}, previews: map[string]*Preview{}, last: time.Now()}
	return nil
}
func (m *Manager) NativeList() ([]Session, error) {
	list, err := m.catalog.List()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	for i, s := range list {
		if related := m.activity.Related(s.ID); len(related) > 0 {
			list[i].Related = related
		}
		if r := m.find(s.ID); r != nil {
			// Async questions wait for an answer while the engine sleeps.
			list[i].Attention = r.session.Attention
			list[i].Background = r.session.Background
		}
		if r := m.find(s.ID); r != nil && r.proc != nil {
			list[i].Status = r.session.Status
			if o, ok := r.proc.(processOwner); ok && s.Agent == "claude" && m.catalog.ClaudeBusyExcept(s, o.Owns) {
				list[i].Status = "external"
				list[i].ReadOnly = true
			}
		} else if m.catalog.Busy(s) {
			list[i].Status = "external"
			list[i].ReadOnly = true
		}
	}
	// Subagents of an engine here: running while at work, else idle; read
	// only, as their parent session takes the messages.
	index := make(map[string]int, len(list))
	for i, s := range list {
		index[s.ID] = i
	}
	for _, r := range m.sessions {
		for id, busy := range ownSubagents(r) {
			// Unless it runs an engine of its own (only viewed: none).
			if own := m.find(id); own != nil && own.proc != nil {
				continue
			}
			if i, ok := index[id]; ok {
				list[i].Status = "idle"
				if busy {
					list[i].Status = "running"
				}
				list[i].ReadOnly = true
			}
		}
	}
	for _, r := range m.sessions {
		if r.proc == nil || r.session.NativeID == "" {
			continue
		}
		found := false
		for _, s := range list {
			if s.ID == r.session.ID {
				found = true
				break
			}
		}
		if !found {
			list = append([]Session{r.session}, list...)
		}
	}
	m.mu.Unlock()
	// What other clients are doing, read from the tails of their records.
	for i, s := range list {
		if s.Status != "external" {
			continue
		}
		if running, _ := m.externalRunning(s); running {
			list[i].Status = "external_running"
		}
	}
	return list, nil
}

func (c *NativeCatalog) Workspaces() ([]string, error) {
	list, err := c.List()
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, s := range list {
		set[s.Workspace] = true
	}
	c.mu.Lock()
	statePath := c.codexStatePath()
	c.mu.Unlock()
	db, err := openCodexState(statePath)
	if err == nil {
		defer db.Close()
		rows, e := db.Query("SELECT path FROM project_roots")
		if e == nil {
			defer rows.Close()
			for rows.Next() {
				var path string
				if rows.Scan(&path) == nil {
					set[workspaceNamespace(path)] = true
				}
			}
		}
	}
	out := []string{}
	for path := range set {
		// Temporary and chat directories never become empty project folders.
		if path != "" && pathOrigin(path) == "project" {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Namespace identity resolves path aliases without rewriting the native launch cwd.
// Resolve existing ancestors too, so deleted/moved workspaces still have stable keys.
func workspaceNamespace(cwd string) string {
	if cwd == "" {
		return ""
	}
	abs, err := filepath.Abs(filepath.Clean(cwd))
	if err != nil {
		return filepath.Clean(cwd)
	}
	probe := abs
	tail := []string{}
	for {
		if resolved, e := filepath.EvalSymlinks(probe); e == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return abs
		}
		tail = append(tail, filepath.Base(probe))
		probe = parent
	}
}
