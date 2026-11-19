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
	"strconv"
	"strings"
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
}

const (
	backgroundTimeout = 15 * time.Minute
	changesMaxAge     = 90 * 24 * time.Hour
	changesListed     = 50
)

// What every background run is told first: nobody is there to answer.
const backgroundPreamble = `You are running in the background: no one reads this conversation or can answer a question. Do not ask; do what you can and leave the rest in state/loops.md for the user. Only change files in this repository. The workbench commits your changes when you finish. End your answer with one line, starting "Summary:", that says what you changed.

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

// inBackground runs prompt as a then commits what changed. It waits for
// the run before it: one at a time in the repository.
func (m *Manager) inBackground(a Assistant, v AssistantChange, prompt string) AssistantChange {
	root := m.assistantsRoot()
	v.Status = "running"
	m.recordChange(v)
	m.as.bg.Lock()
	defer m.as.bg.Unlock()
	// What changed before the run is not the run's doing: commit it on its
	// own, so undoing the run leaves it.
	if _, _, err := gitCommit(root, "chore: keep changes made before "+v.Title); err != nil {
		v.Status, v.Error, v.Finished = "failed", err.Error(), nowStamp()
		m.recordChange(v)
		return v
	}
	run := m.as.background
	if run == nil {
		run = m.backgroundAgent
	}
	out, err := run(a, backgroundPreamble+prompt)
	v.Summary = summaryOf(out)
	if err != nil {
		v.Status, v.Error, v.Finished = "failed", err.Error(), nowStamp()
		// Keep what it did get done, so it can be seen and undone.
		v.Commit, v.Files, _ = gitCommit(root, v.Kind+": "+v.Title+" (stopped)")
		m.recordChange(v)
		return v
	}
	commit, files, err := gitCommit(root, v.Kind+": "+v.Title)
	v.Commit, v.Files, v.Finished = commit, files, nowStamp()
	if err != nil {
		v.Status, v.Error = "failed", "commit: "+err.Error()
	} else {
		v.Status = "done"
	}
	m.recordChange(v)
	return v
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
func (m *Manager) backgroundAgent(a Assistant, prompt string) (string, error) {
	root := m.assistantsRoot()
	run, err := m.cli(a.Agent)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), backgroundTimeout)
	defer cancel()
	var cmd *exec.Cmd
	last := ""
	if a.Agent == "codex" {
		f, err := os.CreateTemp("", "aw-background-*.txt")
		if err != nil {
			return "", err
		}
		last = f.Name()
		f.Close()
		defer os.Remove(last)
		cmd = run.command(append([]string{"exec", "--ephemeral", "--sandbox", "workspace-write", "--skip-git-repo-check", "--color", "never", "-C", a.Path, "--add-dir", root, "-o", last}, append(backgroundModel(a.Agent, m.assistantOptions(a)), "-")...)...)
	} else {
		cmd = run.command(append([]string{"-p", "--no-session-persistence", "--output-format", "text", "--permission-mode", "acceptEdits", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--add-dir", root, "--tools", backgroundTools, "--allowedTools", backgroundTools}, backgroundModel(a.Agent, m.assistantOptions(a))...)...)
	}
	cmd.Dir = a.Path
	if cmd.Env == nil {
		cmd.Env = routineEnv()
	}
	out := &tailBuffer{max: routineOutputMax}
	cmd.Stdout, cmd.Stderr, cmd.Stdin = out, out, strings.NewReader(prompt)
	group, err := startGroup(cmd)
	if err != nil {
		return "", err
	}
	unwatch := group.killOn(ctx)
	err = cmd.Wait()
	unwatch()
	group.Kill(group.PIDs())
	group.Release()
	text := out.String()
	if last != "" {
		if b, e := os.ReadFile(last); e == nil && len(bytes.TrimSpace(b)) > 0 {
			text = string(b)
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return text, fmt.Errorf("stopped after %d minutes", int(backgroundTimeout/time.Minute))
	}
	if err != nil {
		return text, fmt.Errorf("%s: %w: %s", a.Agent, err, clip(strings.TrimSpace(text), 500))
	}
	return text, nil
}

// --- git ---

func gitRun(root string, args ...string) (string, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return "", errors.New("git is not installed")
	}
	cmd := exec.Command(git, args...)
	cmd.Dir = root
	cmd.Env = append(routineEnv(), "GIT_TERMINAL_PROMPT=0")
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
	if _, _, err := gitCommit(root, "chore: keep changes made before undoing "+v.Title); err != nil {
		return v, err
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
