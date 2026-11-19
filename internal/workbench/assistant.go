package workbench

import (
	"bytes"
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/BurntSushi/toml"
)

// Assistants (ADR 0010): a folder of assistants, one per folder with an
// assistant.toml, kept in one git repository. Their procedures are the
// CLIs' own instruction files (AGENTS.md, CLAUDE.md, read up the tree), so
// the workbench only reads assistant.toml: where sessions start, how topics
// are kept, and routines to schedule.

//go:embed assistant
var assistantTemplates embed.FS

// AssistantFile is what an assistant.toml holds.
type AssistantFile struct {
	Name     string        `toml:"name"`
	Agent    string        `toml:"agent"`
	Sessions string        `toml:"sessions"`
	Memory   string        `toml:"memory"`
	Routines []FileRoutine `toml:"routines"`
}

// FileRoutine is a routine an assistant.toml lists; it runs in or sends to
// its assistant.
type FileRoutine struct {
	Name           string   `toml:"name"`
	Daily          string   `toml:"daily"`
	Days           []string `toml:"days"`
	Every          string   `toml:"every"`
	At             string   `toml:"at"`
	Message        string   `toml:"message"`
	Command        string   `toml:"command"`
	Timeout        int      `toml:"timeout"`
	NotifyOnOutput bool     `toml:"notify_on_output"`
	// The message goes to a background run, not a session (ADR 0011).
	Background bool  `toml:"background"`
	Enabled    *bool `toml:"enabled"`
}

// Assistant is an assistant as the page sees it.
type Assistant struct {
	// Its folder's name, which identifies it.
	Dir      string `json:"dir"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	Agent    string `json:"agent"`
	Sessions string `json:"sessions"`
	Memory   string `json:"memory"`
	Routines int    `json:"routines"`
	// What is wrong with its assistant.toml; it is then not used.
	Error string `json:"error,omitempty"`
	// continuous: the session of the ongoing topic, and earlier ones.
	Current  string   `json:"current,omitempty"`
	Previous []string `json:"previous,omitempty"`

	file AssistantFile
}

// assistantState is what the workbench keeps of a continuous assistant.
type assistantState struct {
	Current  string   `json:"current,omitempty"`
	Previous []string `json:"previous,omitempty"`
	// Its context passed the limit: a new topic starts once it is idle.
	RotateNext bool `json:"rotateNext,omitempty"`
}

type assistantsSetting struct {
	Root string `json:"root"`
}

const (
	// A continuous assistant starts a new topic with the first use after
	// this hour of the day, and after a turn leaves its context this full.
	topicDayStarts = 4
	topicContext   = 0.6
	topicsKept     = 50
)

type assistants struct {
	mu sync.Mutex
	// The assistant.toml files last read (path, size, time), and what they
	// gave.
	seen string
	list []Assistant
	// Serializes changes to a continuous assistant's topic.
	topic sync.Mutex
	// Finds a session as the list shows it; replaced in tests.
	lookup func(id string) (Session, bool)
	// One background run at a time in the repository (background.go).
	bg sync.Mutex
	// Runs an assistant in the background; replaced in tests.
	background func(a Assistant, prompt string) (string, error)
	// Conversations being ingested now.
	ingesting map[string]bool
}

func (m *Manager) assistantsRoot() string {
	var v assistantsSetting
	m.store.readSetting("assistants", &v)
	return v.Root
}

var dayNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

// parseEvery reads "30m", "2h" or "1d".
func parseEvery(s string) (int, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) < 2 {
		return 0, errors.New("every: like 30m, 2h or 1d")
	}
	var n int
	if _, err := fmt.Sscanf(s[:len(s)-1], "%d", &n); err != nil || n <= 0 {
		return 0, errors.New("every: like 30m, 2h or 1d")
	}
	switch s[len(s)-1] {
	case 'm':
		return n, nil
	case 'h':
		return n * 60, nil
	case 'd':
		return n * 24 * 60, nil
	}
	return 0, errors.New("every: like 30m, 2h or 1d")
}

// when reads a file routine's schedule.
func (f FileRoutine) when() (When, error) {
	set := 0
	for _, s := range []string{f.Daily, f.Every, f.At} {
		if s != "" {
			set++
		}
	}
	if set != 1 {
		return When{}, errors.New("give one of daily, every or at")
	}
	switch {
	case f.Daily != "":
		w := When{Kind: "daily", Time: f.Daily}
		for _, d := range f.Days {
			n, ok := dayNames[strings.ToLower(strings.TrimSpace(d))[:min(3, len(strings.TrimSpace(d)))]]
			if !ok {
				return w, fmt.Errorf("days: %q is not a day (mon … sun)", d)
			}
			w.Days = append(w.Days, n)
		}
		return w, nil
	case f.Every != "":
		n, err := parseEvery(f.Every)
		return When{Kind: "interval", Minutes: n}, err
	default:
		at, err := time.ParseInLocation("2006-01-02 15:04", f.At, time.Local)
		if err != nil {
			return When{}, errors.New(`at: like "2026-12-01 09:00"`)
		}
		return When{Kind: "once", At: at.Format(time.RFC3339)}, nil
	}
}

// fileRoutineID names a file routine by its assistant and name, so it
// keeps its runs and its on/off switch across edits of the file.
func fileRoutineID(dir, name string) string {
	sum := sha1.Sum([]byte(dir + "\x00" + name))
	return "f" + hex.EncodeToString(sum[:])[:15]
}

// readAssistant reads one assistant.toml.
func readAssistant(path string) (AssistantFile, error) {
	var f AssistantFile
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		return f, err
	}
	if extra := md.Undecoded(); len(extra) > 0 {
		return f, fmt.Errorf("unknown setting %q", extra[0].String())
	}
	if strings.TrimSpace(f.Name) == "" {
		return f, errors.New("name is missing")
	}
	if f.Agent != "claude" && f.Agent != "codex" {
		return f, errors.New(`agent: "claude" or "codex"`)
	}
	if f.Sessions == "" {
		f.Sessions = "per-task"
	}
	if f.Sessions != "per-task" && f.Sessions != "continuous" {
		return f, errors.New(`sessions: "per-task" or "continuous"`)
	}
	if f.Memory == "" {
		f.Memory = "state"
	}
	if f.Memory != "state" && f.Memory != "wiki" {
		return f, errors.New(`memory: "state" or "wiki"`)
	}
	names := map[string]bool{}
	for i, r := range f.Routines {
		if strings.TrimSpace(r.Name) == "" {
			return f, fmt.Errorf("routine %d: name is missing", i+1)
		}
		if names[r.Name] {
			return f, fmt.Errorf("routine %q: two routines have this name", r.Name)
		}
		names[r.Name] = true
		if _, err := r.when(); err != nil {
			return f, fmt.Errorf("routine %q: %v", r.Name, err)
		}
		if strings.TrimSpace(r.Message) == "" && strings.TrimSpace(r.Command) == "" {
			return f, fmt.Errorf("routine %q: give a message, a command or both", r.Name)
		}
		if r.Background && strings.TrimSpace(r.Message) == "" {
			return f, fmt.Errorf("routine %q: background needs a message: what to do", r.Name)
		}
	}
	return f, nil
}

// assistantFiles lists the assistant.toml files under root, one folder deep.
func assistantFiles(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(root, e.Name(), "assistant.toml")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// Assistants reads the assistants (again only when a file changed) and
// brings their routines in line with their files.
func (m *Manager) Assistants() []Assistant {
	root := m.assistantsRoot()
	files := []string{}
	if root != "" {
		files = assistantFiles(root)
	}
	var sig strings.Builder
	sig.WriteString(root)
	for _, p := range files {
		if info, err := os.Stat(p); err == nil {
			fmt.Fprintf(&sig, "\x00%s\x00%d\x00%d", p, info.Size(), info.ModTime().UnixNano())
		}
	}
	m.as.mu.Lock()
	if m.as.seen == sig.String() && m.as.list != nil {
		list := m.withTopics(m.as.list)
		m.as.mu.Unlock()
		return list
	}
	m.as.mu.Unlock()

	list := []Assistant{}
	for _, p := range files {
		dir := filepath.Dir(p)
		a := Assistant{Dir: filepath.Base(dir), Path: dir}
		f, err := readAssistant(p)
		if err != nil {
			a.Name, a.Error = a.Dir, err.Error()
		} else {
			a.Name, a.Agent, a.Sessions, a.Memory, a.Routines, a.file = f.Name, f.Agent, f.Sessions, f.Memory, len(f.Routines), f
		}
		list = append(list, a)
	}
	m.syncFileRoutines(list)
	m.as.mu.Lock()
	m.as.seen, m.as.list = sig.String(), list
	out := m.withTopics(list)
	m.as.mu.Unlock()
	return out
}

func (m *Manager) withTopics(list []Assistant) []Assistant {
	out := make([]Assistant, len(list))
	for i, a := range list {
		if a.Sessions == "continuous" {
			st := m.topicState(a.Dir)
			a.Current, a.Previous = st.Current, st.Previous
		}
		out[i] = a
	}
	return out
}

func (m *Manager) assistant(dir string) (Assistant, error) {
	for _, a := range m.Assistants() {
		if a.Dir == dir {
			if a.Error != "" {
				return a, fmt.Errorf("%s/assistant.toml: %s", dir, a.Error)
			}
			return a, nil
		}
	}
	return Assistant{}, errors.New("assistant not found")
}

// syncFileRoutines makes the routines table hold exactly the routines the
// assistants' files list. A routine keeps its id, runs, interval anchor
// and the on/off switch set in the page; a changed `enabled` in the file
// sets the switch again.
func (m *Manager) syncFileRoutines(list []Assistant) {
	stored, err := m.store.routines()
	if err != nil {
		return
	}
	old := map[string]Routine{}
	for _, v := range stored {
		if strings.HasPrefix(v.CreatedBy, "file:") {
			old[v.ID] = v
		}
	}
	now := m.rt.now()
	keep := map[string]bool{}
	for _, a := range list {
		if a.Error != "" {
			// Keep its routines as they were until the file is fixed.
			for id, v := range old {
				if v.CreatedBy == "file:"+a.Dir {
					keep[id] = true
				}
			}
			continue
		}
		for _, f := range a.file.Routines {
			v := fileRoutine(a, f)
			keep[v.ID] = true
			prev, had := old[v.ID]
			if had {
				v.Created = prev.Created
				v.Enabled = prev.Enabled
				if f.Enabled != nil && (prev.FileEnabled == nil || *prev.FileEnabled != *f.Enabled) {
					v.Enabled = *f.Enabled
				}
				v.Ran = prev.Ran
				if prev.When.Kind == v.When.Kind && prev.When.Minutes == v.When.Minutes {
					v.Anchor = prev.Anchor
				}
				if v.When.Kind == "once" && prev.Ran != "" && prev.When.At == v.When.At {
					v.Enabled = false
				}
			} else {
				v.Created = nowStamp()
			}
			if v.Anchor == "" {
				v.Anchor = now.UTC().Format(time.RFC3339)
			}
			if had && sameRoutine(prev, v) {
				continue
			}
			if err := m.store.saveRoutine(v); err != nil {
				continue
			}
			m.scheduleRoutine(v, now)
			m.routineChanged(v.ID, nil)
		}
	}
	for id := range old {
		if !keep[id] {
			_ = m.DeleteRoutine(id)
		}
	}
}

// fileRoutine is the routine a file lists, as the scheduler keeps it.
func fileRoutine(a Assistant, f FileRoutine) Routine {
	when, _ := f.when()
	v := Routine{ID: fileRoutineID(a.Dir, f.Name), Name: f.Name, When: when, CreatedBy: "file:" + a.Dir, Enabled: true, FileEnabled: f.Enabled}
	if f.Enabled != nil {
		v.Enabled = *f.Enabled
	}
	if strings.TrimSpace(f.Command) != "" {
		timeout := f.Timeout
		if timeout <= 0 {
			timeout = routineTimeoutMin
		}
		v.Command = &RoutineCommand{Run: strings.TrimSpace(f.Command), Dir: a.Path, TimeoutMin: min(timeout, routineTimeoutMax), NotifyOnOutput: f.NotifyOnOutput}
	}
	if strings.TrimSpace(f.Message) != "" {
		v.Message = f.Message
		v.Target = &RoutineTarget{Kind: "assistant", Assistant: a.Dir, Background: f.Background}
	}
	return v
}

func sameRoutine(a, b Routine) bool {
	a.Next, a.Last, b.Next, b.Last = "", nil, "", nil
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// --- setting up ---

var folderName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// slug is a folder name for an assistant's name, "" when it has no
// letters or digits a folder name keeps.
func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

type scaffoldData struct {
	Name, Agent, Sessions, Memory string
	Main                          bool
}

// writeNew writes a file unless it exists: setting up never overwrites.
func writeNew(path string, data []byte) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func renderTemplate(name string, data scaffoldData) ([]byte, error) {
	raw, err := assistantTemplates.ReadFile("assistant/" + name)
	if err != nil {
		return nil, err
	}
	t, err := template.New(name).Parse(string(raw))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	err = t.Execute(&out, data)
	return out.Bytes(), err
}

// skillsLink makes .claude/skills point at .agents/skills in dir: Claude
// Code reads the one, Codex the other. Where links cannot be made (Windows
// without the right), a note says what to do instead.
func skillsLink(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, ".agents", "skills"), 0o755); err != nil {
		return err
	}
	keep := filepath.Join(dir, ".agents", "skills", ".gitkeep")
	if err := writeNew(keep, nil); err != nil {
		return err
	}
	link := filepath.Join(dir, ".claude", "skills")
	if _, err := os.Lstat(link); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		return err
	}
	if err := os.Symlink(filepath.Join("..", ".agents", "skills"), link); err != nil {
		return writeNew(filepath.Join(dir, ".claude", "SKILLS.md"), []byte("Skills live in ../.agents/skills. Claude Code reads .claude/skills: link or copy that folder here.\n"))
	}
	return nil
}

// setUpRoot writes the repository's shared files and, the first time, the
// main assistant; it starts the git repository when git is there.
func (m *Manager) SetUpAssistants(root, agent string) error {
	if agent != "claude" && agent != "codex" {
		return errors.New("agent must be claude or codex")
	}
	abs, err := filepath.Abs(expandHome(strings.TrimSpace(root)))
	if err != nil || strings.TrimSpace(root) == "" {
		return errors.New("choose a folder")
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return err
	}
	shared := map[string]string{
		"AGENTS.md":               "root/AGENTS.md",
		"CLAUDE.md":               "root/CLAUDE.md",
		".gitignore":              "root/gitignore",
		".claude/settings.json":   "root/claude-settings.json",
		"shared/wiki/index.md":    "root/wiki/index.md",
		"shared/wiki/overview.md": "root/wiki/overview.md",
	}
	for to, from := range shared {
		data, err := assistantTemplates.ReadFile("assistant/" + from)
		if err != nil {
			return err
		}
		if err := writeNew(filepath.Join(abs, filepath.FromSlash(to)), data); err != nil {
			return err
		}
	}
	if err := skillsLink(abs); err != nil {
		return err
	}
	if len(assistantFiles(abs)) == 0 {
		if err := scaffoldAssistant(abs, "main", scaffoldData{Name: "Assistant", Agent: agent, Sessions: "continuous", Memory: "wiki", Main: true}); err != nil {
			return err
		}
	}
	if err := m.store.setting("assistants", assistantsSetting{Root: abs}); err != nil {
		return err
	}
	commitAll(abs, "Set up assistants")
	m.assistantsChanged()
	return nil
}

func scaffoldAssistant(root, dir string, d scaffoldData) error {
	path := filepath.Join(root, dir)
	files := map[string]string{
		"assistant.toml":   "each/assistant.toml",
		"AGENTS.md":        "each/AGENTS.md",
		"state/loops.md":   "each/loops.md",
		"state/handoff.md": "each/handoff.md",
		"log.md":           "each/log.md",
	}
	for to, from := range files {
		data, err := renderTemplate(from, d)
		if err != nil {
			return err
		}
		if err := writeNew(filepath.Join(path, filepath.FromSlash(to)), data); err != nil {
			return err
		}
	}
	if err := writeNew(filepath.Join(path, "CLAUDE.md"), []byte("@AGENTS.md\n")); err != nil {
		return err
	}
	if err := writeNew(filepath.Join(path, "tools", ".gitkeep"), nil); err != nil {
		return err
	}
	return skillsLink(path)
}

// NewAssistant adds an assistant folder to the repository.
func (m *Manager) NewAssistant(name, dir, agent, sessions string) (Assistant, error) {
	root := m.assistantsRoot()
	if root == "" {
		return Assistant{}, errors.New("set up the assistants folder first")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 60 {
		return Assistant{}, errors.New("name it (at most 60 characters)")
	}
	if dir = strings.TrimSpace(dir); dir == "" {
		dir = slug(name)
	}
	if !folderName.MatchString(dir) || dir == "shared" {
		return Assistant{}, errors.New("folder: lowercase letters, digits, - . _ (not \"shared\")")
	}
	if agent != "claude" && agent != "codex" {
		return Assistant{}, errors.New("agent must be claude or codex")
	}
	if sessions != "per-task" && sessions != "continuous" {
		return Assistant{}, errors.New("sessions: per-task or continuous")
	}
	if _, err := os.Stat(filepath.Join(root, dir)); err == nil {
		return Assistant{}, fmt.Errorf("%s already exists", filepath.Join(root, dir))
	}
	if err := scaffoldAssistant(root, dir, scaffoldData{Name: name, Agent: agent, Sessions: sessions, Memory: "state"}); err != nil {
		return Assistant{}, err
	}
	commitAll(root, "Add assistant "+dir)
	m.assistantsChanged()
	return m.assistant(dir)
}

// commitAll commits everything in root when it is (or can become) a git
// repository; without git the files are simply there.
func commitAll(root, message string) {
	_, _, _ = gitCommit(root, message)
}

func (m *Manager) assistantsChanged() {
	m.as.mu.Lock()
	m.as.seen = ""
	m.as.mu.Unlock()
	m.Assistants()
	m.emit("", "assistants", map[string]any{})
}

// --- topics ---

func (m *Manager) topicState(dir string) assistantState {
	var st assistantState
	m.store.readSetting("assistant:"+dir, &st)
	return st
}

func (m *Manager) saveTopic(dir string, st assistantState) {
	if len(st.Previous) > topicsKept {
		st.Previous = st.Previous[len(st.Previous)-topicsKept:]
	}
	_ = m.store.setting("assistant:"+dir, st)
}

// sessionNow is a session as the list shows it.
func (m *Manager) sessionNow(id string) (Session, bool) {
	if m.as.lookup != nil {
		return m.as.lookup(id)
	}
	list, err := m.NativeList()
	if err != nil {
		return Session{}, false
	}
	for _, s := range list {
		if s.ID == id {
			return s, true
		}
	}
	return Session{}, false
}

// topicDay is when today's topic began: today at topicDayStarts, or
// yesterday's before that hour.
func topicDay(now time.Time) time.Time {
	l := now.In(time.Local)
	start := time.Date(l.Year(), l.Month(), l.Day(), topicDayStarts, 0, 0, 0, time.Local)
	if l.Before(start) {
		start = start.AddDate(0, 0, -1)
	}
	return start
}

func resting(s Session) bool {
	return s.Attention == "" && (s.Status == "idle" || s.Status == "sleeping" || s.Status == "stopped" || s.Status == "")
}

// currentTopic is a continuous assistant's ongoing session, after starting
// a new topic when the day changed or its context filled (and it is not
// busy); "" when the next message starts one.
func (m *Manager) currentTopic(a Assistant, now time.Time) string {
	st := m.topicState(a.Dir)
	if st.Current == "" {
		return ""
	}
	s, ok := m.sessionNow(st.Current)
	if !ok {
		st.Current, st.RotateNext = "", false
		m.saveTopic(a.Dir, st)
		return ""
	}
	updated, _ := time.Parse(time.RFC3339, s.Updated)
	if resting(s) && (st.RotateNext || (!updated.IsZero() && updated.Before(topicDay(now)))) {
		m.newTopic(a, st)
		return ""
	}
	return st.Current
}

// newTopic ends the ongoing topic; its session joins the earlier ones,
// and what it had for the wiki is ingested.
func (m *Manager) newTopic(a Assistant, st assistantState) {
	if st.Current != "" {
		st.Previous = append(st.Previous, st.Current)
		go m.maybeIngest(st.Current)
	}
	st.Current, st.RotateNext = "", false
	m.saveTopic(a.Dir, st)
	m.emit("", "assistants", map[string]any{"assistant": a.Dir})
}

// topicMoved follows a session from its first id (a workbench token) to its
// native one in the topics that name it.
func (m *Manager) topicMoved(oldID, newID string) {
	m.as.topic.Lock()
	defer m.as.topic.Unlock()
	for _, a := range m.Assistants() {
		st := m.topicState(a.Dir)
		changed := false
		if st.Current == oldID {
			st.Current, changed = newID, true
		}
		for i, p := range st.Previous {
			if p == oldID {
				st.Previous[i], changed = newID, true
			}
		}
		if changed {
			m.saveTopic(a.Dir, st)
			m.emit("", "assistants", map[string]any{"assistant": a.Dir})
		}
	}
}

// OpenAssistant says which session to show for an assistant: its ongoing
// topic, or none (the page then shows a new message for it), with the
// session it continues from.
func (m *Manager) OpenAssistant(dir string) (map[string]any, error) {
	a, err := m.assistant(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"assistant": a}
	if a.Sessions == "continuous" {
		m.as.topic.Lock()
		defer m.as.topic.Unlock()
		if cur := m.currentTopic(a, m.rt.now()); cur != "" {
			out["session"] = cur
			return out, nil
		}
		if st := m.topicState(dir); len(st.Previous) > 0 {
			out["previous"] = st.Previous[len(st.Previous)-1]
		}
	}
	out["fresh"] = true
	return out, nil
}

// NewTopic starts a new topic for a continuous assistant now.
func (m *Manager) NewTopic(dir string) error {
	a, err := m.assistant(dir)
	if err != nil {
		return err
	}
	if a.Sessions != "continuous" {
		return errors.New("every request to this assistant is a new topic already")
	}
	m.as.topic.Lock()
	defer m.as.topic.Unlock()
	m.newTopic(a, m.topicState(dir))
	return nil
}

// AssistantMessage gives an assistant a message: to its ongoing topic, or
// as a new session (always, for a per-task assistant). routine is the run
// that sent it, if any. It returns the session and whether the message
// waits in its queue.
func (m *Manager) AssistantMessage(dir, text string, images []string, routine string) (Session, bool, error) {
	a, err := m.assistant(dir)
	if err != nil {
		return Session{}, false, err
	}
	if a.Sessions == "continuous" {
		m.as.topic.Lock()
		defer m.as.topic.Unlock()
		if cur := m.currentTopic(a, m.rt.now()); cur != "" {
			queued, err := m.enqueue(cur, text, images, routine)
			s, _ := m.sessionNow(cur)
			if s.ID == "" {
				s.ID = cur
			}
			return s, queued, err
		}
	}
	named := text
	if label := routineLabelOf(text); label != "" {
		named = label
	}
	s, err := m.createWithMessage(a.Agent, a.Path, "", named, text, images, nil, routine)
	if err != nil {
		return s, false, err
	}
	if a.Sessions == "continuous" {
		st := m.topicState(dir)
		st.Current, st.RotateNext = s.ID, false
		m.saveTopic(dir, st)
		m.emit("", "assistants", map[string]any{"assistant": dir})
	}
	return s, false, nil
}

// routineLabelOf is the routine name a routine's message starts with.
func routineLabelOf(text string) string {
	if !strings.HasPrefix(text, "[Routine: ") {
		return ""
	}
	name, _, ok := strings.Cut(strings.TrimPrefix(text, "[Routine: "), "]")
	if !ok {
		return ""
	}
	return name
}

// assistantTurnEnded marks a continuous assistant's topic for a new one
// once a turn leaves its context past topicContext.
func (m *Manager) assistantTurnEnded(id, cwd string, used, window float64) {
	if window <= 0 || used/window < topicContext {
		return
	}
	for _, a := range m.Assistants() {
		if a.Sessions != "continuous" || a.Path != cwd {
			continue
		}
		m.as.topic.Lock()
		st := m.topicState(a.Dir)
		if st.Current == id && !st.RotateNext {
			st.RotateNext = true
			m.saveTopic(a.Dir, st)
		}
		m.as.topic.Unlock()
	}
}

// assistantRoutineTarget checks a routine's assistant exists.
func (m *Manager) assistantRoutineTarget(dir string) error {
	_, err := m.assistant(dir)
	return err
}

// readable for tests: the templates as files.
var _ fs.FS = assistantTemplates

// assistantsAPI serves /api/assistants[/setup | /{dir}/(open|message|topic)].
func (s *Server) assistantsAPI(w http.ResponseWriter, r *http.Request, rest string) {
	m := s.Manager
	parts := strings.Split(rest, "/")
	switch {
	case rest == "" && r.Method == "GET":
		writeJSON(w, map[string]any{"root": m.assistantsRoot(), "assistants": m.Assistants()})
	case rest == "changes" && r.Method == "GET":
		list, err := m.AssistantChanges()
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, list)
	case len(parts) == 3 && parts[0] == "changes" && r.Method == "POST" && (parts[2] == "undo" || parts[2] == "retry"):
		var v AssistantChange
		var err error
		if parts[2] == "undo" {
			v, err = m.UndoChange(parts[1])
		} else {
			v, err = m.RetryChange(parts[1])
		}
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, v)
	case rest == "setup" && r.Method == "POST":
		var v struct{ Root, Agent string }
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		if err := m.SetUpAssistants(v.Root, v.Agent); err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"root": m.assistantsRoot(), "assistants": m.Assistants()})
	case rest == "" && r.Method == "POST":
		var v struct{ Name, Dir, Agent, Sessions string }
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		a, err := m.NewAssistant(v.Name, v.Dir, v.Agent, v.Sessions)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, a)
	case len(parts) == 2 && parts[1] == "open" && r.Method == "POST":
		out, err := m.OpenAssistant(parts[0])
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, out)
	case len(parts) == 2 && parts[1] == "topic" && r.Method == "POST":
		if err := m.NewTopic(parts[0]); err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case len(parts) == 2 && parts[1] == "message" && r.Method == "POST":
		var v struct {
			Text   string
			Images []string `json:",omitempty"`
			Files  []string `json:",omitempty"`
		}
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		files, err := m.uploads.Files(v.Files)
		if err != nil {
			bad(w, err)
			return
		}
		text := fileLines(v.Text, files)
		if err := checkMessage(text, v.Images); err != nil {
			bad(w, err)
			return
		}
		session, _, err := m.AssistantMessage(parts[0], text, v.Images, "")
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, session)
	default:
		http.NotFound(w, r)
	}
}
