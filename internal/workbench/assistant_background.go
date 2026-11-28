package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Background runs (ADR 0011): one-off, restricted agent runs in an
// assistant's folder that no one watches. They take what a conversation
// left (ingest) or what a routine fetched (background routines) and change
// files in the repository of assistants; the workbench then commits what
// changed, so every such change can be seen and undone.

// AssistantChange is one background run and the commit it made.
type AssistantChange struct {
	ID        string `json:"id"`
	Assistant string `json:"assistant"`
	// ingest: from a conversation; routine: from a routine's run.
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// ingest: the conversation, the turns it covered and the record of them.
	Session string `json:"session,omitempty"`
	From    int    `json:"from,omitempty"`
	To      int    `json:"to,omitempty"`
	Source  string `json:"source,omitempty"`
	// routine: the run.
	Run      string `json:"run,omitempty"`
	Started  string `json:"started"`
	Finished string `json:"finished,omitempty"`
	// running | done | failed | undone
	Status  string   `json:"status"`
	Commit  string   `json:"commit,omitempty"`
	Files   []string `json:"files,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Error   string   `json:"error,omitempty"`
	// The commit that undid it.
	Reverted string `json:"reverted,omitempty"`
	// Files it changed that a background run may not change: undone, not
	// committed (ADR 0021).
	Blocked []string `json:"blocked,omitempty"`
}

const (
	backgroundTimeout = 15 * time.Minute
	changesMaxAge     = 90 * 24 * time.Hour
	changesListed     = 50
)

// What every background run is told first: nobody is there to answer.
const backgroundPreamble = `You are running in the background: no one reads this conversation or can answer a question. Do not ask; do what you can and leave the rest in state/loops.md for the user. Only change your own state/, drafts/ and log.md (and the shared wiki, if you keep it): the workbench undoes changes to any other file. It commits your changes when you finish. End your answer with one line, starting "Summary:", that says what you changed.

`

// --- store ---

func (s *Store) saveChange(v AssistantChange) error {
	b, _ := json.Marshal(v)
	_, err := s.db.Exec(`INSERT INTO assistant_changes(id,started,data) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, v.ID, v.Started, string(b))
	return err
}

func (s *Store) change(id string) (AssistantChange, error) {
	var data string
	var v AssistantChange
	if err := s.db.QueryRow(`SELECT data FROM assistant_changes WHERE id=?`, id).Scan(&data); err != nil {
		return v, err
	}
	err := json.Unmarshal([]byte(data), &v)
	return v, err
}

// changes lists the latest changes, newest first.
func (s *Store) changes(limit int) ([]AssistantChange, error) {
	rows, err := s.db.Query(`SELECT data FROM assistant_changes ORDER BY started DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssistantChange{}
	for rows.Next() {
		var data string
		if rows.Scan(&data) != nil {
			continue
		}
		var v AssistantChange
		if json.Unmarshal([]byte(data), &v) == nil {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

func (s *Store) pruneChanges(now time.Time) error {
	cut := now.Add(-changesMaxAge).UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`DELETE FROM assistant_changes WHERE started < ?`, cut)
	return err
}

func (m *Manager) recordChange(v AssistantChange) {
	if err := m.store.saveChange(v); err != nil {
		fmt.Fprintln(os.Stderr, "save assistant change:", err)
	}
	m.emit("", "assistant", v)
}

// endOpenChanges fails the runs a stop of the workbench cut short.
func (m *Manager) endOpenChanges() {
	list, _ := m.store.changes(changesListed)
	for _, v := range list {
		if v.Status == "running" {
			v.Status, v.Error, v.Finished = "failed", "the workbench stopped during the run", nowStamp()
			_ = m.store.saveChange(v)
		}
	}
}

// AssistantChanges lists the latest background runs.
func (m *Manager) AssistantChanges() ([]AssistantChange, error) {
	return m.store.changes(changesListed)
}

// --- running ---

// backgroundOut is what a background run answered and the files it wrote.
type backgroundOut struct {
	Text string
	// Claude: the files its Edit and Write calls named (absolute, or
	// relative to the repository), the only way it can change a file.
	// Codex: not known (its sandbox can write with any command).
	Wrote []string
	Known bool
}

// inBackground runs prompt as a then commits what it changed. It waits for
// the run before it: one at a time in the repository.
//
// A run's commit and undo cover only the files the run wrote (ADR 0027):
// sessions and other assistants may change the repository at the same
// time, and their work is neither committed in its name nor undone. Codex
// does not report what it writes, so for Codex every change made during
// the run counts as its own, as before.
func (m *Manager) inBackground(a Assistant, v AssistantChange, prompt string) AssistantChange {
	root := m.assistantsRoot()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	v.Status = "running"
	m.recordChange(v)
	m.as.bg.Lock()
	defer m.as.bg.Unlock()
	allowed := backgroundWrites(a)
	// What the run's commit includes besides what it wrote: the record an
	// ingest reads, written for it just before.
	extra := []string{}
	if v.Source != "" {
		extra = append(extra, filepath.ToSlash(filepath.Join(a.Dir, filepath.FromSlash(v.Source))))
	}
	known := a.Agent != "codex"
	// What changed before the run is not the run's doing: commit it on its
	// own, so undoing the run leaves it. With known writes, only where the
	// run may write; edits elsewhere stay as they are.
	before := append(append([]string{}, allowed...), extra...)
	if !known {
		before = nil
	}
	if _, _, err := gitCommitPaths(root, "chore: keep changes made before "+v.Title, before); err != nil {
		v.Status, v.Error, v.Finished = "failed", err.Error(), nowStamp()
		m.recordChange(v)
		return v
	}
	// Edits outside those paths that are not committed: if the run writes
	// over one, undoing the run puts the edit back, not the last commit.
	saved := dirtyFiles(root)
	run := m.as.background
	if run == nil {
		run = m.backgroundAgent
	}
	out, err := run(a, backgroundPreamble+prompt)
	v.Summary = summaryOf(out.Text)
	var blocked, own []string
	var undoErr error
	if known && out.Known {
		var outside []string
		own, outside = splitWrites(root, out.Wrote, allowed)
		blocked, undoErr = restoreFiles(root, outside, saved)
		own = append(own, extra...)
	} else {
		blocked, undoErr = undoOutside(root, allowed)
	}
	v.Blocked = blocked
	if undoErr != nil && err == nil {
		err = fmt.Errorf("checking what it changed: %w", undoErr)
	}
	if err == nil && len(blocked) > 0 {
		err = fmt.Errorf("it changed files a background run may not change, which were undone: %s", strings.Join(blocked, ", "))
	}
	commit := func(message string) (string, []string, error) {
		if known && out.Known {
			if len(own) == 0 {
				return "", nil, nil
			}
			return gitCommitPaths(root, message, own)
		}
		return gitCommit(root, message)
	}
	if err != nil {
		v.Status, v.Error, v.Finished = "failed", err.Error(), nowStamp()
		// Keep what it did get done, so it can be seen and undone.
		v.Commit, v.Files, _ = commit(v.Kind + ": " + v.Title + " (stopped)")
		m.recordChange(v)
		return v
	}
	c, files, err := commit(v.Kind + ": " + v.Title)
	v.Commit, v.Files, v.Finished = c, files, nowStamp()
	if err != nil {
		v.Status, v.Error = "failed", "commit: "+err.Error()
	} else {
		v.Status = "done"
	}
	m.recordChange(v)
	return v
}

// splitWrites turns the files a run wrote into paths relative to root, and
// splits them into those it may change and the others. Files outside the
// repository are left out: the run's tools cannot reach them anyway.
func splitWrites(root string, wrote, allowed []string) (own, outside []string) {
	seen := map[string]bool{}
	for _, p := range wrote {
		rel := repoPath(root, p)
		if rel == "" || seen[rel] {
			continue
		}
		seen[rel] = true
		if writeAllowed(rel, allowed) {
			own = append(own, rel)
		} else {
			outside = append(outside, rel)
		}
	}
	sort.Strings(own)
	sort.Strings(outside)
	return own, outside
}

// repoPath is p relative to root (links resolved), with forward slashes;
// "" when it is not inside root.
func repoPath(root, p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, filepath.FromSlash(p))
	}
	// The file may be new: resolve its folder, which exists.
	dir, base := filepath.Split(filepath.Clean(p))
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	} else if d, err := filepath.EvalSymlinks(dir); err == nil {
		p = filepath.Join(d, base)
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || !filepath.IsLocal(rel) || rel == ".git" || strings.HasPrefix(filepath.ToSlash(rel), ".git/") {
		return ""
	}
	return filepath.ToSlash(rel)
}

// savedFile is an uncommitted file as it was before a run.
type savedFile struct {
	data   []byte
	exists bool
}

// dirtySaveMax bounds what dirtyFiles keeps in memory, per file and in all.
const (
	dirtyFileMax  = 1 << 20
	dirtyTotalMax = 16 << 20
)

// dirtyFiles keeps the uncommitted files of root as they are now (within
// dirtyFileMax and dirtyTotalMax): a file a run then writes over without
// being allowed to is put back as it was, with the user's edit in it.
func dirtyFiles(root string) map[string]savedFile {
	out := map[string]savedFile{}
	status, err := gitRun(root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return out
	}
	total := 0
	entries := strings.Split(status, "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		if e[0] == 'R' || e[0] == 'C' {
			i++
		}
		path := e[3:]
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		switch {
		case errors.Is(err, os.ErrNotExist):
			out[path] = savedFile{}
		case err == nil && len(b) <= dirtyFileMax && total+len(b) <= dirtyTotalMax:
			out[path] = savedFile{data: b, exists: true}
			total += len(b)
		}
	}
	return out
}

// restoreFiles undoes a run's writes to paths: each goes back to how it was
// before the run (an uncommitted edit kept in saved, or the last commit), a
// file the run made is removed. It returns the paths.
func restoreFiles(root string, paths []string, saved map[string]savedFile) ([]string, error) {
	var firstErr error
	fail := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(p))
		if was, ok := saved[p]; ok {
			if !was.exists {
				if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
					fail(err)
				}
				continue
			}
			fail(os.WriteFile(full, was.data, 0o644))
			continue
		}
		if _, err := gitRun(root, "cat-file", "-e", "HEAD:"+p); err == nil {
			_, err := gitRun(root, "checkout", "HEAD", "--", p)
			fail(err)
			continue
		}
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			fail(err)
		}
	}
	return paths, firstErr
}

// backgroundWrites is what a background run of a may change, relative to
// the repository: its own state/, drafts/ and log.md, and the shared wiki
// for an assistant that keeps it. Its procedure, settings, skills and
// scripts are not among them: text the run read (mail, a web page) must not
// change what runs later or what the next session is told (ADR 0021).
func backgroundWrites(a Assistant) []string {
	dir := filepath.ToSlash(a.Dir)
	allowed := []string{dir + "/state/", dir + "/drafts/", dir + "/log.md"}
	if a.Memory == "wiki" {
		allowed = append(allowed, "shared/wiki/")
	}
	return allowed
}

func writeAllowed(path string, allowed []string) bool {
	for _, a := range allowed {
		if path == a || strings.HasSuffix(a, "/") && strings.HasPrefix(path, a) {
			return true
		}
	}
	return false
}

// undoOutside puts back every change in root outside allowed: a changed or
// deleted file is restored from the last commit, a new one removed. Run
// after a background run, whose repository was committed just before it,
// so every change is the run's. It returns the files it undid. Files git
// ignores are not seen.
func undoOutside(root string, allowed []string) ([]string, error) {
	out, err := gitRun(root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	var restore, remove, blocked []string
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		code, path := e[:2], e[3:]
		if code[0] == 'R' || code[0] == 'C' {
			// A staged rename or copy names its source next; a background
			// run stages nothing, but restore both if one appears.
			i++
			if i < len(entries) && !writeAllowed(entries[i], allowed) {
				restore = append(restore, entries[i])
				blocked = append(blocked, entries[i])
			}
		}
		if writeAllowed(path, allowed) {
			continue
		}
		blocked = append(blocked, path)
		if code == "??" {
			remove = append(remove, path)
		} else if code[0] == 'A' {
			restore = append(restore, path)
			remove = append(remove, path)
		} else {
			restore = append(restore, path)
		}
	}
	var firstErr error
	if len(restore) > 0 {
		args := append([]string{"checkout", "HEAD", "--"}, restore...)
		if _, err := gitRun(root, args...); err != nil {
			// A file added and staged has no HEAD version: drop it from the
			// index, then remove it below.
			_, _ = gitRun(root, append([]string{"rm", "-q", "--cached", "--ignore-unmatch", "--"}, restore...)...)
			for _, p := range restore {
				_, _ = gitRun(root, "checkout", "HEAD", "--", p)
			}
		}
	}
	for _, p := range remove {
		full := filepath.Join(root, filepath.FromSlash(p))
		if _, err := gitRun(root, "cat-file", "-e", "HEAD:"+p); err == nil {
			continue
		}
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	if left, err := gitRun(root, "status", "--porcelain=v1", "-z", "--untracked-files=all"); err == nil {
		for _, e := range strings.Split(left, "\x00") {
			if len(e) >= 4 && !writeAllowed(e[3:], allowed) && firstErr == nil {
				firstErr = fmt.Errorf("could not undo %s", e[3:])
			}
		}
	}
	return blocked, firstErr
}

// summaryOf is the "Summary:" line an answer ends with, or its last line.
func summaryOf(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if s, ok := strings.CutPrefix(l, "Summary:"); ok {
			return clip(strings.TrimSpace(s), 300)
		}
	}
	return clip(strings.TrimSpace(lines[len(lines)-1]), 300)
}

// The only tools a background Claude has. --tools takes the others away:
// allowing these alone is not enough, as acceptEdits and the user's own
// allow rules still let Bash run simple commands (checked with 2.1.328).
const backgroundTools = "Read,Edit,Write,Glob,Grep"

// backgroundModel passes the assistant's model and effort to a background
// run; its mode and permissions never apply there (the limits above stay).
func backgroundModel(agent string, o Options) []string {
	var args []string
	if agent == "codex" {
		if o.Model != "" {
			args = append(args, "-m", o.Model)
		}
		if o.Effort != "" {
			args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(o.Effort))
		}
		return args
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.Effort != "" {
		args = append(args, "--effort", o.Effort)
	}
	return args
}

// backgroundAgent runs the assistant's agent once, without a session the
// sidebar shows, limited to reading and editing files in the repository:
// Claude with only the file tools and no MCP servers; Codex in its
// workspace-write sandbox, which has no network.
func (m *Manager) backgroundAgent(a Assistant, prompt string) (backgroundOut, error) {
	root := m.assistantsRoot()
	run, err := m.cli(a.Agent)
	if err != nil {
		return backgroundOut{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), backgroundTimeout)
	defer cancel()
	var cmd *exec.Cmd
	last := ""
	codex := a.Agent == "codex"
	if codex {
		f, err := os.CreateTemp("", "aw-background-*.txt")
		if err != nil {
			return backgroundOut{}, err
		}
		last = f.Name()
		f.Close()
		defer os.Remove(last)
		cmd = run.command(append([]string{"exec", "--ephemeral", "--sandbox", "workspace-write", "--skip-git-repo-check", "--color", "never", "-C", a.Path, "--add-dir", root, "-o", last}, append(backgroundModel(a.Agent, m.assistantOptions(a)), "-")...)...)
	} else {
		// stream-json names every file its Edit and Write calls touch: the
		// run's commit and undo cover those alone (inBackground).
		cmd = run.command(append([]string{"-p", "--no-session-persistence", "--output-format", "stream-json", "--verbose", "--permission-mode", "acceptEdits", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--add-dir", root, "--tools", backgroundTools, "--allowedTools", backgroundTools}, backgroundModel(a.Agent, m.assistantOptions(a))...)...)
	}
	cmd.Dir = a.Path
	if cmd.Env == nil {
		cmd.Env = routineEnv()
	}
	errs := &tailBuffer{max: routineOutputMax}
	stream := &claudeStream{}
	if codex {
		cmd.Stdout = errs
	} else {
		cmd.Stdout = stream
	}
	cmd.Stderr, cmd.Stdin = errs, strings.NewReader(prompt)
	group, err := startGroup(cmd)
	if err != nil {
		return backgroundOut{}, err
	}
	unwatch := group.killOn(ctx)
	err = cmd.Wait()
	unwatch()
	group.Kill(group.PIDs())
	group.Release()
	out := backgroundOut{Text: errs.String()}
	if codex {
		if b, e := os.ReadFile(last); e == nil && len(bytes.TrimSpace(b)) > 0 {
			out.Text = string(b)
		}
	} else {
		out.Text, out.Wrote, out.Known = stream.result(errs.String())
	}
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("stopped after %d minutes", int(backgroundTimeout/time.Minute))
	}
	if err != nil {
		return out, fmt.Errorf("%s: %w: %s", a.Agent, err, clip(strings.TrimSpace(out.Text), 500))
	}
	return out, nil
}

// claudeStream reads a background Claude's stream-json output as it comes:
// the files its tools wrote, and its answer.
type claudeStream struct {
	mu      sync.Mutex
	partial []byte
	wrote   []string
	answer  string
	last    string
	done    bool
}

func (c *claudeStream) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partial = append(c.partial, p...)
	for {
		i := bytes.IndexByte(c.partial, '\n')
		if i < 0 {
			break
		}
		c.line(c.partial[:i])
		c.partial = c.partial[i+1:]
	}
	// A line longer than this is no record we use (a large file read).
	if len(c.partial) > 8<<20 {
		c.partial = nil
	}
	return len(p), nil
}

// line takes one record: the file_path of an Edit or Write call, the text
// of the assistant's messages, and the result's answer.
func (c *claudeStream) line(b []byte) {
	var rec struct {
		Type    string `json:"type"`
		Result  string `json:"result"`
		Message struct {
			Content []struct {
				Type  string `json:"type"`
				Name  string `json:"name"`
				Text  string `json:"text"`
				Input struct {
					FilePath     string `json:"file_path"`
					NotebookPath string `json:"notebook_path"`
				} `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(bytes.TrimSpace(b), &rec) != nil {
		return
	}
	switch rec.Type {
	case "assistant":
		for _, item := range rec.Message.Content {
			switch {
			case item.Type == "text" && strings.TrimSpace(item.Text) != "":
				c.last = item.Text
			case item.Type == "tool_use" && writeTools[item.Name]:
				if f := item.Input.FilePath + item.Input.NotebookPath; f != "" {
					c.wrote = append(c.wrote, f)
				}
			}
		}
	case "result":
		c.answer, c.done = rec.Result, true
	}
}

// The tools that change files; a background Claude has only Edit and Write
// of these (backgroundTools), the others are named in case that changes.
var writeTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

// result is the run's answer (the result record's, else its last message,
// else what it printed on stderr), the files it wrote, and whether the
// stream was read at all (no record: nothing is known of its writes).
func (c *claudeStream) result(stderr string) (string, []string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(bytes.TrimSpace(c.partial)) > 0 {
		c.line(c.partial)
		c.partial = nil
	}
	text := c.answer
	if !c.done {
		text = c.last
	}
	if strings.TrimSpace(text) == "" {
		text = stderr
	}
	return text, append([]string{}, c.wrote...), true
}

// --- git ---

func gitRun(root string, args ...string) (string, error) {
	return gitRunEnv(root, nil, args...)
}

// gitRunEnv is gitRun with more environment (a temporary index).
func gitRunEnv(root string, env []string, args ...string) (string, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return "", errors.New("git is not installed")
	}
	cmd := exec.Command(git, args...)
	cmd.Dir = root
	cmd.Env = append(append(routineEnv(), "GIT_TERMINAL_PROMPT=0"), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

// gitWithName runs a git command that makes a commit; a fresh account may
// have no name to make it with, and then the workbench's name is used. (A
// failed attempt may leave a revert half done, so this is asked first.)
func gitWithName(root string, args ...string) (string, error) {
	if out, err := gitRun(root, "config", "user.email"); err != nil || strings.TrimSpace(out) == "" {
		args = append([]string{"-c", "user.name=Agent Workbench", "-c", "user.email=workbench@localhost"}, args...)
	}
	return gitRun(root, args...)
}

// gitCommitOnly commits the changes under paths, and drop taken out of
// the index, and nothing else: through a temporary index from HEAD, so what
// else is changed or staged stays as it is (the assistants' work in
// progress). The real index then follows HEAD for those paths.
func gitCommitOnly(root, message string, paths, drop []string) (string, error) {
	if len(paths)+len(drop) == 0 {
		return "", nil
	}
	f, err := os.CreateTemp("", "aw-index-*")
	if err != nil {
		return "", err
	}
	index := f.Name()
	f.Close()
	os.Remove(index)
	defer os.Remove(index)
	env := []string{"GIT_INDEX_FILE=" + index}
	if _, err := gitRun(root, "rev-parse", "-q", "--verify", "HEAD"); err == nil {
		if _, err := gitRunEnv(root, env, "read-tree", "HEAD"); err != nil {
			return "", err
		}
	} else if _, err := gitRunEnv(root, env, "read-tree", "--empty"); err != nil {
		return "", err
	}
	// A path that is neither here nor in HEAD would stop add.
	var add []string
	if len(paths) > 0 {
		known, _ := gitRunEnv(root, env, append([]string{"ls-files", "-z", "--"}, paths...)...)
		inHead := map[string]bool{}
		for _, k := range strings.Split(known, "\x00") {
			inHead[k] = true
		}
		for _, p := range paths {
			p = strings.TrimSuffix(p, "/")
			_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(p)))
			if err == nil || inHead[p] || strings.Contains(known, p+"/") {
				add = append(add, p)
			}
		}
	}
	// A path git ignores and does not track (a definition file as its
	// catalog has it) is not the commit's: naming it would stop add.
	if len(add) > 0 {
		ignored, _ := gitRunEnv(root, env, append([]string{"-c", "core.quotePath=false", "check-ignore", "--"}, add...)...)
		skip := map[string]bool{}
		for _, p := range strings.Split(ignored, "\n") {
			skip[p] = true
		}
		kept := add[:0]
		for _, p := range add {
			if !skip[p] {
				kept = append(kept, p)
			}
		}
		add = kept
	}
	if len(add) > 0 {
		if _, err := gitRunEnv(root, env, append([]string{"add", "-A", "--"}, add...)...); err != nil {
			return "", err
		}
	}
	if len(drop) > 0 {
		if _, err := gitRunEnv(root, env, append([]string{"rm", "-q", "--cached", "--ignore-unmatch", "--"}, drop...)...); err != nil {
			return "", err
		}
	}
	if _, err := gitRunEnv(root, env, "diff", "--cached", "--quiet"); err == nil {
		return "", nil
	}
	args := []string{"commit", "-q", "-m", message}
	if out, err := gitRun(root, "config", "user.email"); err != nil || strings.TrimSpace(out) == "" {
		args = append([]string{"-c", "user.name=Agent Workbench", "-c", "user.email=workbench@localhost"}, args...)
	}
	if _, err := gitRunEnv(root, env, args...); err != nil {
		return "", err
	}
	sync := append(append([]string{"reset", "-q", "--"}, add...), drop...)
	gitRun(root, sync...)
	head, err := gitRun(root, "rev-parse", "HEAD")
	return strings.TrimSpace(head), err
}

// gitCommitPaths commits the changes under paths only (gitCommit when
// paths is nil), whatever else is changed or staged in root, and returns
// the commit and its files; none when nothing under them changed.
func gitCommitPaths(root, message string, paths []string) (string, []string, error) {
	if paths == nil {
		return gitCommit(root, message)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		if _, err := gitRun(root, "init", "-q"); err != nil {
			return "", nil, err
		}
	}
	// git refuses a path that matches nothing: keep those that exist now or
	// are in the last commit (deleted since).
	var present []string
	for _, p := range paths {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(p))); err == nil {
			present = append(present, p)
		} else if out, _ := gitRun(root, "ls-files", "--", p); strings.TrimSpace(out) != "" {
			present = append(present, p)
		}
	}
	if len(present) == 0 {
		return "", nil, nil
	}
	if _, err := gitRun(root, append([]string{"add", "-A", "--"}, present...)...); err != nil {
		return "", nil, err
	}
	if _, err := gitRun(root, append([]string{"diff", "--cached", "--quiet", "--"}, present...)...); err == nil {
		return "", nil, nil
	}
	// --only (implied by paths): what else is staged stays out.
	if _, err := gitWithName(root, append([]string{"commit", "-q", "-m", message, "--"}, present...)...); err != nil {
		return "", nil, err
	}
	head, err := gitRun(root, "rev-parse", "HEAD")
	if err != nil {
		return "", nil, err
	}
	names, _ := gitRun(root, "show", "--name-only", "--format=", "HEAD")
	files := []string{}
	for _, f := range strings.Split(strings.TrimSpace(names), "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	return strings.TrimSpace(head), files, nil
}

// gitCommit commits everything in root (making it a repository first) and
// returns the commit and the files it changed; none when nothing changed.
func gitCommit(root, message string) (string, []string, error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		if _, err := gitRun(root, "init", "-q"); err != nil {
			return "", nil, err
		}
	}
	if _, err := gitRun(root, "add", "-A"); err != nil {
		return "", nil, err
	}
	if _, err := gitRun(root, "diff", "--cached", "--quiet"); err == nil {
		return "", nil, nil
	}
	if _, err := gitWithName(root, "commit", "-q", "-m", message); err != nil {
		return "", nil, err
	}
	head, err := gitRun(root, "rev-parse", "HEAD")
	if err != nil {
		return "", nil, err
	}
	names, _ := gitRun(root, "show", "--name-only", "--format=", "HEAD")
	files := []string{}
	for _, f := range strings.Split(strings.TrimSpace(names), "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	return strings.TrimSpace(head), files, nil
}

// UndoChange reverts a change's commit with a new commit. A later change
// to the same lines makes that fail; the repository is then left as it was.
func (m *Manager) UndoChange(id string) (AssistantChange, error) {
	v, err := m.store.change(id)
	if err != nil {
		return v, errors.New("no such change")
	}
	if v.Status != "done" && v.Status != "failed" || v.Commit == "" {
		return v, errors.New("this change made no commit to undo")
	}
	root := m.assistantsRoot()
	m.as.bg.Lock()
	defer m.as.bg.Unlock()
	// Only its own files are reverted; other work in the repository stays
	// as it is, uncommitted. An uncommitted edit to one of its files would
	// be lost or block the revert: say so instead.
	if len(v.Files) > 0 {
		if out, _ := gitRun(root, append([]string{"status", "--porcelain", "--"}, v.Files...)...); strings.TrimSpace(out) != "" {
			return v, fmt.Errorf("could not undo it: %s changed since and is not committed", strings.Fields(strings.TrimSpace(out))[1])
		}
	}
	if _, err := gitWithName(root, "revert", "--no-edit", v.Commit); err != nil {
		_, _ = gitRun(root, "revert", "--abort")
		return v, fmt.Errorf("could not undo it: later changes touch the same lines (%v)", err)
	}
	head, _ := gitRun(root, "rev-parse", "HEAD")
	v.Status, v.Reverted = "undone", strings.TrimSpace(head)
	m.recordChange(v)
	return v, nil
}

// --- background routines ---

// backgroundRoutine hands a routine's message to its assistant as a
// background run, and ends the run with what it did.
func (m *Manager) backgroundRoutine(v Routine, run RoutineRun, text string) {
	a, err := m.assistant(v.Target.Assistant)
	if err != nil {
		m.endRun(run, "failed", err.Error(), true)
		return
	}
	c := m.inBackground(a, AssistantChange{ID: token()[:16], Assistant: a.Dir, Kind: "routine", Title: v.Name, Run: run.ID, Started: nowStamp()}, text)
	run.Output = c.Summary
	if c.Status != "done" {
		m.endRun(run, "failed", c.Error, true)
		return
	}
	// A run that changed nothing has nothing to tell.
	m.endRun(run, "done", "", len(c.Files) > 0)
}
