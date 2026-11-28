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
	"strconv"
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
	Name string `toml:"name"`
	// false: turned off (hidden under Show more, routines paused).
	Enabled *bool `toml:"enabled"`
	// What the page shows about it (ADR 0015).
	Description  string   `toml:"description"`
	Capabilities []string `toml:"capabilities"`
	Examples     []string `toml:"examples"`
	// What it needs from the user: a signed-in CLI, an MCP server, an API
	// token, an account.
	Requires []string `toml:"requires"`
	// false until set up; missing (older files) means set up.
	Ready    *bool  `toml:"ready"`
	Agent    string `toml:"agent"`
	Sessions string `toml:"sessions"`
	Memory   string `toml:"memory"`
	// Its icon: a built-in drawing's name, or a .png in its folder (ADR 0030).
	Icon string `toml:"icon"`
	// The agents its author made it for; none means both (ADR 0031).
	Agents []string `toml:"agents"`
	// Its sessions' model, effort, mode and permissions, by agent (ADR 0017).
	Options  map[string]AgentOptions `toml:"options"`
	Routines []FileRoutine           `toml:"routines"`
}

// FileRoutine is a routine an assistant.toml lists; it runs in or sends to
// its assistant.
type FileRoutine struct {
	Name           string   `toml:"name"`
	Daily          string   `toml:"daily"`
	Days           []string `toml:"days"`
	Every          string   `toml:"every"`
	Cron           string   `toml:"cron"`
	At             string   `toml:"at"`
	Message        string   `toml:"message"`
	Command        string   `toml:"command"`
	Timeout        int      `toml:"timeout"`
	NotifyOnOutput bool     `toml:"notify_on_output"`
	// The message goes to a background run, not a session (ADR 0011).
	Background bool `toml:"background"`
	// What the command printed waits for the user's approval, with Preview
	// (a file in the assistant's folder) to look at first (ADR 0021).
	Approve bool   `toml:"approve"`
	Preview string `toml:"preview"`
	Enabled *bool  `toml:"enabled"`
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
	// Turned on (the personal assistant always is).
	Enabled bool `json:"enabled"`
	// What it does, can do, and requests to try; set up yet (ADR 0015).
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Examples     []string `json:"examples,omitempty"`
	Requires     []string `json:"requires,omitempty"`
	Ready        bool     `json:"ready"`
	// Its own session settings, by agent (ADR 0017).
	Options map[string]AgentOptions `json:"options,omitempty"`
	// What is wrong with its assistant.toml; it is then not used.
	Error string `json:"error,omitempty"`
	// continuous: the session of the ongoing topic, and earlier ones.
	Current  string   `json:"current,omitempty"`
	Previous []string `json:"previous,omitempty"`
	// Its icon (ADR 0030): a built-in drawing's name, or "file" for the
	// image in its folder, at IconVersion (its modification time, ms).
	Icon        string `json:"icon,omitempty"`
	IconVersion int64  `json:"iconVersion,omitempty"`
	// The agents its author made it for; none means both.
	Agents []string `json:"agents,omitempty"`
	// The catalog it was installed from, and its definition files the user
	// changed (catalog.go).
	Catalog *AssistantCatalog `json:"catalog,omitempty"`

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
	// The language assistants write to the user in (language.go).
	Language string `json:"language,omitempty"`
	// The order the user dragged the assistants into (folders); the
	// personal assistant is first whatever it says.
	Order []string `json:"order,omitempty"`
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
	// The workbench's clones of catalogs (catalog.go).
	catalog sync.Mutex
	// assistants.lock.toml is read, changed and written under it.
	lockMu sync.Mutex
	// What a catalog could update, by assistant (catalog_update.go).
	updates map[string]CatalogUpdate
	// Runs an assistant in the background; replaced in tests.
	background func(a Assistant, prompt string) (backgroundOut, error)
	// Hands a routine's message to an assistant's session (AssistantMessage);
	// replaced in tests.
	foreground func(dir, text string, images []string, routine string) (Session, bool, error)
	// Conversations being ingested now.
	ingesting map[string]bool
	// The backup to GitHub (sync.go).
	sync syncer
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
	for _, s := range []string{f.Daily, f.Every, f.At, f.Cron} {
		if s != "" {
			set++
		}
	}
	if set != 1 {
		return When{}, errors.New("give one of daily, every, at or cron")
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
	case f.Cron != "":
		w := When{Kind: "cron", Cron: strings.TrimSpace(f.Cron)}
		if _, err := cronGap(w.Cron, time.Now()); err != nil {
			return w, err
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
// Limits on what the home card shows (ADR 0015).
const (
	metaItems = 12
	metaChars = 200
)

func oneLine(key, s string) error {
	if strings.ContainsAny(s, "\n\r") || len([]rune(s)) > metaChars {
		return fmt.Errorf("%s: one line, at most %d characters", key, metaChars)
	}
	return nil
}

// tomlStrings is a TOML array of strings.
func tomlStrings(list []string) string {
	q := make([]string, len(list))
	for i, s := range list {
		q[i] = strconv.Quote(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

func readAssistant(path string) (AssistantFile, error) {
	var f AssistantFile
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		return f, err
	}
	if extra := md.Undecoded(); len(extra) > 0 {
		return f, fmt.Errorf("unknown setting %q", extra[0].String())
	}
	// The owner's settings beside it take precedence (defs.go).
	settings, err := readSettings(filepath.Dir(path))
	if err != nil {
		return f, err
	}
	if err := settings.checkOwnRoutines(f); err != nil {
		return f, err
	}
	settings.apply(&f)
	for _, a := range f.Agents {
		if a != "claude" && a != "codex" {
			return f, errors.New(`agents: "claude", "codex" or both`)
		}
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
	if err := oneLine("description", f.Description); err != nil {
		return f, err
	}
	if err := checkIcon(f.Icon); err != nil {
		return f, err
	}
	for key, list := range map[string][]string{"capabilities": f.Capabilities, "examples": f.Examples, "requires": f.Requires} {
		if len(list) > metaItems {
			return f, fmt.Errorf("%s: at most %d", key, metaItems)
		}
		for _, item := range list {
			if err := oneLine(key, item); err != nil {
				return f, err
			}
		}
	}
	if err := checkAgentOptions(f.Options); err != nil {
		return f, err
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
		if r.Approve && (strings.TrimSpace(r.Message) == "" || strings.TrimSpace(r.Command) == "") {
			return f, fmt.Errorf("routine %q: approve needs a command and a message", r.Name)
		}
		if r.Preview != "" {
			if !r.Approve {
				return f, fmt.Errorf("routine %q: preview goes with approve", r.Name)
			}
			if _, ok := insideFolder(r.Preview); !ok {
				return f, fmt.Errorf("routine %q: preview is a file in the assistant's folder, such as raw/preview.md", r.Name)
			}
		}
	}
	return f, nil
}

// insideFolder cleans a path relative to an assistant's folder; false when
// it is absolute or leaves the folder.
func insideFolder(rel string) (string, bool) {
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
		return "", false
	}
	c := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if c == "." || c == ".." || strings.HasPrefix(c, "../") || filepath.VolumeName(c) != "" {
		return "", false
	}
	return c, true
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
// sortAssistants puts the personal assistant first, then the order the user
// dragged them into, then the rest by folder.
func (m *Manager) sortAssistants(list []Assistant) {
	var v assistantsSetting
	m.store.readSetting("assistants", &v)
	rank := map[string]int{}
	for i, d := range v.Order {
		rank[d] = i + 1
	}
	key := func(a Assistant) (int, int, string) {
		if a.Dir == "main" {
			return 0, 0, ""
		}
		r, ok := rank[a.Dir]
		if !ok {
			r = len(v.Order) + 1
		}
		return 1, r, a.Dir
	}
	sort.SliceStable(list, func(i, j int) bool {
		a1, b1, c1 := key(list[i])
		a2, b2, c2 := key(list[j])
		if a1 != a2 {
			return a1 < a2
		}
		if b1 != b2 {
			return b1 < b2
		}
		return c1 < c2
	})
}

// SetAssistantOrder keeps the order the user dragged the assistants into.
func (m *Manager) SetAssistantOrder(order []string) error {
	if len(order) > 500 {
		return errors.New("too many assistants")
	}
	for _, d := range order {
		if !folderName.MatchString(d) {
			return fmt.Errorf("order: %q is not an assistant's folder", d)
		}
	}
	var v assistantsSetting
	m.store.readSetting("assistants", &v)
	v.Order = order
	if err := m.store.setting("assistants", v); err != nil {
		return err
	}
	m.assistantsChanged()
	return nil
}

func (m *Manager) Assistants() []Assistant {
	root := m.assistantsRoot()
	// The files are read once, and again only when something says they
	// changed (assistantsChanged: the page, `assistant reload`, Reload
	// assistants): no polling the disk (ADR 0015).
	sig := "loaded:" + root
	m.as.mu.Lock()
	if m.as.seen == sig && m.as.list != nil {
		list := m.withTopics(m.as.list)
		m.as.mu.Unlock()
		return list
	}
	m.as.mu.Unlock()
	files := []string{}
	if root != "" {
		files = assistantFiles(root)
	}

	list := []Assistant{}
	for _, p := range files {
		dir := filepath.Dir(p)
		a := Assistant{Dir: filepath.Base(dir), Path: dir}
		f, err := readAssistant(p)
		a.Enabled = true
		if err != nil {
			a.Name, a.Error = a.Dir, err.Error()
		} else {
			a.Name, a.Agent, a.Sessions, a.Memory, a.Routines, a.file = f.Name, f.Agent, f.Sessions, f.Memory, len(f.Routines), f
			a.Enabled = f.Enabled == nil || *f.Enabled || a.Dir == "main"
			a.Description, a.Capabilities, a.Examples, a.Requires = f.Description, f.Capabilities, f.Examples, f.Requires
			a.Ready = f.Ready == nil || *f.Ready
			a.Options = f.Options
			a.Icon, a.IconVersion = iconShown(dir, f.Icon)
			a.Agents = f.Agents
		}
		list = append(list, a)
	}
	m.sortAssistants(list)
	if root != "" {
		withCatalogs(root, list)
		m.as.mu.Lock()
		updates := m.as.updates
		m.as.mu.Unlock()
		for i := range list {
			if u, ok := updates[list[i].Dir]; ok && list[i].Catalog != nil {
				list[i].Catalog.Update = &u
			}
		}
	}
	m.syncFileRoutines(list)
	m.as.mu.Lock()
	m.as.seen, m.as.list = sig, list
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
		v.Approve = f.Approve && v.Command != nil
		if rel, ok := insideFolder(f.Preview); ok && v.Approve {
			v.Preview = filepath.Join(a.Path, filepath.FromSlash(rel))
		}
	}
	return v
}

func sameRoutine(a, b Routine) bool {
	a.Next, a.Last, a.Recent, b.Next, b.Last, b.Recent = "", nil, nil, "", nil, nil
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
	// The personal assistant's home card, in the setup language.
	Description                    string
	CapabilitiesTOML, ExamplesTOML string
	// The rule naming the language, for the root AGENTS.md.
	LanguageLine string
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

// The name the personal assistant starts with; the user can change it.
const mainName = "Moss"

// sharedFiles are the files at the top of the repository of assistants.
var sharedFiles = map[string]string{
	"AGENTS.md":               "root/AGENTS.md",
	"CLAUDE.md":               "root/CLAUDE.md",
	".gitignore":              "root/gitignore",
	".claude/settings.json":   "root/claude-settings.json",
	"shared/wiki/index.md":    "root/wiki/index.md",
	"shared/wiki/overview.md": "root/wiki/overview.md",
	".agents/skills/create-assistant/SKILL.md": "root/skills/create-assistant/SKILL.md",
	"shared/tools/secret.py":                   "root/tools/secret.py",
	"shared/tools/test_secret.py":              "root/tools/test_secret.py",
	".agents/skills/help/SKILL.md":             "root/skills/help/SKILL.md",
	".agents/skills/start-session/SKILL.md":    "root/skills/start-session/SKILL.md",
}

// writeShared writes the shared files and templates root lacks and links
// its skills; it reports whether it wrote anything. The root AGENTS.md
// names lang.
func writeShared(root, lang string) (bool, error) {
	wrote := false
	for to, from := range sharedFiles {
		path := filepath.Join(root, filepath.FromSlash(to))
		if _, err := os.Lstat(path); err == nil {
			continue
		}
		var data []byte
		var err error
		if to == "AGENTS.md" {
			data, err = renderTemplate(from, scaffoldData{LanguageLine: languageLine(lang)})
		} else {
			data, err = assistantTemplates.ReadFile("assistant/" + from)
		}
		if err != nil {
			return wrote, err
		}
		if err := writeNew(path, data); err != nil {
			return wrote, err
		}
		wrote = true
	}
	if _, err := os.Lstat(filepath.Join(root, ".claude", "skills")); err != nil {
		wrote = true
	}
	ignored, err := ensureIgnored(root)
	if err != nil {
		return wrote, err
	}
	// The hook is not part of the repository: no commit for it.
	if _, err := installHook(root); err != nil {
		return wrote, err
	}
	return wrote || ignored, skillsLink(root)
}

// ensureShared brings a repository set up by an earlier version up to the
// shared files this one has (a new shared skill), never changing one that
// is there.
func (m *Manager) ensureShared() {
	root := m.assistantsRoot()
	if root == "" {
		return
	}
	if _, err := os.Stat(root); err != nil {
		return
	}
	wrote, err := writeShared(root, m.language())
	if err != nil {
		fmt.Fprintln(os.Stderr, "assistants: shared files:", err)
	}
	// Moss's definition follows this version where the user kept it as
	// shipped (moss.go).
	if updated, err := m.updateMoss(root); err != nil {
		fmt.Fprintln(os.Stderr, "assistants: Moss:", err)
	} else if updated {
		wrote = true
	}
	// Installed assistants' definitions missing here (a clone on another
	// computer) come back from their catalogs (catalog.go).
	for _, err := range m.restoreDefinitions(root) {
		fmt.Fprintln(os.Stderr, "assistants: catalog:", err)
	}
	writeAllAgentFiles(root)
	if upgradeMossCard(root) {
		wrote = true
	}
	drop, err := refreshManaged(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "assistants: catalog files:", err)
	}
	if wrote || len(drop) > 0 {
		// What refreshManaged just took out of the index leaves with it.
		m.commitPaths(root, "chore: add shared files from Agent Workbench", append(sharedPaths(), "main/assistant.toml"), drop...)
	}
}

// installedAgents says which agents' programs can be found.
func (m *Manager) installedAgents() map[string]bool {
	out := map[string]bool{}
	for _, agent := range []string{"claude", "codex"} {
		_, err := m.cli(agent)
		out[agent] = err == nil
	}
	return out
}

// defaultAgent is the agent setting up chooses: Claude Code if it is
// installed, else Codex; "" with neither.
func (m *Manager) defaultAgent() string {
	for _, agent := range []string{"claude", "codex"} {
		if _, err := m.cli(agent); err == nil {
			return agent
		}
	}
	return ""
}

// assistantName checks a name an assistant is given.
func assistantName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 60 || strings.ContainsAny(name, "\n\r") {
		return name, errors.New("name it: one line, at most 60 characters")
	}
	return name, nil
}

// SetUpAssistants makes the repository of assistants with its shared files
// and, the first time, the personal assistant; it starts the git repository
// when git is there. Empty values take the defaults: ~/assistants, the
// installed agent, and the name Moss.
func (m *Manager) SetUpAssistants(root, agent, name string) error {
	_, err := m.setUp(root, agent, name, "")
	return err
}

// setUp sets assistants up and reports whether it made the personal
// assistant (which then gets a welcome, welcome.go). lang "" is zh-Hans.
func (m *Manager) setUp(root, agent, name, lang string) (bool, error) {
	if lang == "" {
		lang = defaultLanguage
	}
	if !validLanguage(lang) {
		return false, errors.New("language: en, zh-Hans or zh-Hant")
	}
	if strings.TrimSpace(root) == "" {
		root = "~/assistants"
	}
	if agent == "" {
		if agent = m.defaultAgent(); agent == "" {
			return false, errors.New("install Claude Code or Codex first (Settings → Agents)")
		}
	}
	if agent != "claude" && agent != "codex" {
		return false, errors.New("agent must be claude or codex")
	}
	if strings.TrimSpace(name) == "" {
		name = mainName
	}
	name, err := assistantName(name)
	if err != nil {
		return false, err
	}
	abs, err := filepath.Abs(expandHome(strings.TrimSpace(root)))
	if err != nil {
		return false, errors.New("choose a folder")
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return false, err
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return false, err
	}
	// A repository first, so the secret check guards the first commit.
	_, noHead := gitRun(abs, "rev-parse", "-q", "--verify", "HEAD")
	if _, err := os.Stat(filepath.Join(abs, ".git")); err != nil {
		_, _ = gitRun(abs, "init", "-q")
	}
	if _, err := writeShared(abs, lang); err != nil {
		return false, err
	}
	created := false
	if len(assistantFiles(abs)) == 0 {
		meta := mossMeta(lang)
		if err := scaffoldAssistant(abs, "main", scaffoldData{Name: name, Agent: agent, Sessions: "continuous", Memory: "wiki", Main: true, Description: meta.Description, CapabilitiesTOML: tomlStrings(meta.Capabilities), ExamplesTOML: tomlStrings(meta.Examples)}); err != nil {
			return false, err
		}
		created = true
	}
	if err := m.store.setting("assistants", assistantsSetting{Root: abs, Language: lang}); err != nil {
		return false, err
	}
	// Remember what was shipped, so later versions can update it.
	if _, err := m.updateMoss(abs); err != nil {
		return false, err
	}
	// A clone of the user's repository from another computer: its
	// installed assistants' definitions come back from their catalogs.
	defer writeAllAgentFiles(abs)
	for _, err := range m.restoreDefinitions(abs) {
		fmt.Fprintln(os.Stderr, "assistants: catalog:", err)
	}
	// A new repository's first commit has everything; an existing one (a
	// clone from another computer) only what setting up wrote.
	if noHead != nil {
		m.commitAll(abs, "Set up assistants")
	} else {
		paths := sharedPaths()
		if created {
			paths = append(paths, "main")
		}
		m.commitPaths(abs, "Set up assistants", paths)
	}
	m.assistantsChanged()
	return created, nil
}

// scaffoldAssistant writes an assistant's folder from the one template
// (assistant/each): its settings, procedure, state and log; every other
// assistant also keeps its fetched data (raw/) out of git.
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
		// The personal assistant's procedure is Moss's own file (moss.go).
		if d.Main && to == "AGENTS.md" {
			if err := writeNew(filepath.Join(path, to), mossFile("moss/AGENTS.md")); err != nil {
				return err
			}
			continue
		}
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
	if !d.Main {
		if err := writeNew(filepath.Join(path, ".gitignore"), []byte("# Fetched data and scripts' bookkeeping: fetched again, never committed.\nraw/\n")); err != nil {
			return err
		}
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
	m.commitPaths(root, "Add assistant "+dir, []string{dir})
	m.assistantsChanged()
	return m.assistant(dir)
}

// commitAll commits everything in root when it is (or can become) a git
// repository; without git the files are simply there.
// commitAll commits everything in root, after the background run in
// progress: it would otherwise commit the run's half-written files, or meet
// its git in the index.
// commitAll commits the whole repository: setting it up, a backup.
func (m *Manager) commitAll(root, message string) {
	m.commitPaths(root, message, nil)
}

// commitPaths commits what the workbench changed, paths, and nothing of
// the assistants' work in progress elsewhere; nil paths commit everything.
// Definition files as their catalog has them leave the index on the way
// (catalog.go).
func (m *Manager) commitPaths(root, message string, paths []string, dropped ...string) {
	m.as.bg.Lock()
	defer m.as.bg.Unlock()
	drop, err := refreshManaged(root)
	drop = append(drop, dropped...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "assistants: catalog files:", err)
	}
	if paths == nil {
		_, _, _ = gitCommit(root, message)
		return
	}
	if _, err := gitCommitOnly(root, message, paths, drop); err != nil {
		fmt.Fprintln(os.Stderr, "assistants: commit:", err)
	}
}

// sharedPaths are the files the workbench keeps at the top of the
// repository and in Moss's folder.
func sharedPaths() []string {
	var out []string
	for p := range sharedFiles {
		out = append(out, p)
	}
	for p := range mossFiles {
		out = append(out, p)
	}
	out = append(out, ".agents/skills/.gitkeep", ".claude/skills")
	sort.Strings(out)
	return out
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

// setTopLevel replaces the first `key = "…"` line before assistant.toml's
// first table ([[routines]] has name lines of its own); false when there
// is none.
func setTopLevel(text, key, value string) (string, bool) {
	return setTopLevelRaw(text, key, strconv.Quote(value))
}

// setTopLevelRaw replaces the first top-level `key = …` line with raw as
// its value; false when there is none.
func setTopLevelRaw(text, key, raw string) (string, bool) {
	line := regexp.MustCompile(`(?m)^` + key + `[ \t]*=[^\n]*$`)
	head, tail := text, ""
	if loc := regexp.MustCompile(`(?m)^[ \t]*\[`).FindStringIndex(text); loc != nil {
		head, tail = text[:loc[0]], text[loc[0]:]
	}
	loc := line.FindStringIndex(head)
	if loc == nil {
		return text, false
	}
	end := loc[1]
	if strings.HasSuffix(head[loc[0]:end], "\r") {
		end--
	}
	return head[:loc[0]] + key + " = " + raw + head[end:] + tail, true
}

// setOrInsert sets a top-level key, adding it when it is missing.
func setOrInsert(text, key, raw string) string {
	if t, ok := setTopLevelRaw(text, key, raw); ok {
		return t
	}
	return insertTopLevel(text, key, raw)
}

// insertTopLevel adds `key = raw` after the agent line (else at the end of
// the top-level keys, before the first table).
func insertTopLevel(text, key, raw string) string {
	add := key + " = " + raw + "\n"
	if loc := regexp.MustCompile(`(?m)^agent[ \t]*=[^\n]*\n`).FindStringIndex(text); loc != nil {
		if first := regexp.MustCompile(`(?m)^[ \t]*\[`).FindStringIndex(text); first == nil || loc[0] < first[0] {
			return text[:loc[1]] + add + text[loc[1]:]
		}
	}
	if first := regexp.MustCompile(`(?m)^[ \t]*\[`).FindStringIndex(text); first != nil {
		return text[:first[0]] + add + "\n" + text[first[0]:]
	}
	return strings.TrimRight(text, "\n") + "\n" + add
}

// UpdateAssistant renames an assistant or switches its agent, changing only
// those lines of its assistant.toml (comments and routines stay). A
// continuous assistant that changes agent starts a new topic: the ongoing
// session belongs to the old one.
func (m *Manager) UpdateAssistant(dir, name, agent string, enabled *bool) (Assistant, error) {
	return m.PatchAssistant(dir, AssistantPatch{Name: name, Agent: agent, Enabled: enabled})
}

// AssistantPatch is what the page changes in an assistant.toml; nil and ""
// leave a field as it is.
type AssistantPatch struct {
	Name, Agent, Sessions string
	Description           *string
	Capabilities          *[]string
	Examples              *[]string
	Requires              *[]string
	Enabled, Ready        *bool
	// A built-in icon's name, or a .png in its folder (ADR 0030).
	Icon *string
	// Its session settings for an agent: the whole table, "" leaves a key
	// to Settings → Agents.
	Options map[string]AgentOptions
}

// PatchAssistant changes only the given top-level lines of an
// assistant.toml (comments and routines stay), checks the result before it
// replaces the file, and commits. A continuous assistant that switches agent
// or topic mode starts a new topic.
func (m *Manager) PatchAssistant(dir string, p AssistantPatch) (Assistant, error) {
	name, agent, enabled := p.Name, p.Agent, p.Enabled
	a, err := m.assistant(dir)
	if err != nil {
		return a, err
	}
	path := filepath.Join(a.Path, "assistant.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return a, err
	}
	// The owner's choices go to settings.toml, the author's text to
	// assistant.toml (defs.go, ADR 0031).
	settings, err := readSettings(a.Path)
	if err != nil {
		return a, err
	}
	text := string(data)
	var did []string
	if name != "" && strings.TrimSpace(name) != a.Name {
		if name, err = assistantName(name); err != nil {
			return a, err
		}
		settings.Name = name
		did = append(did, "rename to "+name)
	}
	switched := false
	if agent != "" && agent != a.Agent {
		if agent != "claude" && agent != "codex" {
			return a, errors.New("agent must be claude or codex")
		}
		settings.Agent = agent
		did, switched = append(did, "switch to "+agent), true
	}
	if enabled != nil && *enabled != a.Enabled {
		if dir == "main" {
			return a, errors.New("the personal assistant cannot be turned off")
		}
		v := *enabled
		settings.Enabled = &v
		if v {
			did = append(did, "turn on")
		} else {
			did = append(did, "turn off")
		}
	}
	if p.Sessions != "" && p.Sessions != a.Sessions {
		if p.Sessions != "per-task" && p.Sessions != "continuous" {
			return a, errors.New("sessions: per-task or continuous")
		}
		settings.Sessions = p.Sessions
		did, switched = append(did, "topics "+p.Sessions), true
	}
	// An assistant from a catalog keeps its author's assistant.toml: what
	// its card says becomes the owner's, in settings.toml (defs.go).
	lock, _ := readLock(m.assistantsRoot())
	_, fromCatalog := lock.Assistants[dir]
	if p.Description != nil && *p.Description != a.Description {
		if fromCatalog {
			settings.Description = strings.TrimSpace(*p.Description)
		} else {
			text = setOrInsert(text, "description", strconv.Quote(strings.TrimSpace(*p.Description)))
		}
		did = append(did, "description")
	}
	for _, f := range []struct {
		key  string
		list *[]string
	}{{"capabilities", p.Capabilities}, {"examples", p.Examples}, {"requires", p.Requires}} {
		if f.list == nil {
			continue
		}
		clean := []string{}
		for _, item := range *f.list {
			if item = strings.TrimSpace(item); item != "" {
				clean = append(clean, item)
			}
		}
		if fromCatalog {
			*map[string]*[]string{"capabilities": &settings.Capabilities, "examples": &settings.Examples, "requires": &settings.Requires}[f.key] = clean
		} else {
			text = setOrInsert(text, f.key, tomlStrings(clean))
		}
		did = append(did, f.key)
	}
	if p.Icon != nil && *p.Icon != a.file.Icon {
		if err := checkIcon(*p.Icon); err != nil {
			return a, err
		}
		settings.Icon = *p.Icon
		if settings.Icon == "" {
			// The default is the camera; an empty setting would leave the
			// author's icon showing.
			settings.Icon = "camera"
		}
		did = append(did, "icon "+settings.Icon)
	}
	if p.Ready != nil && *p.Ready != a.Ready {
		v := *p.Ready
		settings.Ready = &v
		did = append(did, map[bool]string{true: "ready", false: "not set up"}[v])
	}
	for _, agent := range []string{"claude", "codex"} {
		o, ok := p.Options[agent]
		if !ok || o == a.file.Options[agent] {
			continue
		}
		if err := checkAgentOptions(map[string]AgentOptions{agent: o}); err != nil {
			return a, err
		}
		if settings.Options == nil {
			settings.Options = map[string]AgentOptions{}
		}
		settings.Options[agent] = o
		did = append(did, agent+" settings")
	}
	if len(did) == 0 {
		return a, nil
	}
	// Checked before it replaces the files: a change never breaks them.
	oldSettings, _ := os.ReadFile(filepath.Join(a.Path, settingsFile))
	restore := func() {
		if oldSettings == nil {
			os.Remove(filepath.Join(a.Path, settingsFile))
		} else {
			os.WriteFile(filepath.Join(a.Path, settingsFile), oldSettings, 0o644)
		}
	}
	if err := writeAssistantSettings(a.Path, settings); err != nil {
		return a, err
	}
	if text != string(data) {
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
			restore()
			return a, err
		}
		if _, err := readAssistant(tmp); err != nil {
			os.Remove(tmp)
			restore()
			return a, err
		}
		if err := os.Rename(tmp, path); err != nil {
			restore()
			return a, err
		}
	} else if _, err := readAssistant(path); err != nil {
		restore()
		return a, err
	}
	if switched && a.Sessions == "continuous" {
		m.as.topic.Lock()
		m.newTopic(a, m.topicState(a.Dir))
		m.as.topic.Unlock()
	}
	paths := []string{a.Dir + "/assistant.toml", a.Dir + "/" + settingsFile}
	// An icon image, put in place or removed with the choice.
	if p.Icon != nil {
		for _, icon := range []string{a.file.Icon, *p.Icon} {
			if icon != "" && !builtinIcon(icon) {
				paths = append(paths, a.Dir+"/"+icon)
			}
		}
	}
	m.commitPaths(m.assistantsRoot(), a.Dir+": "+strings.Join(did, ", "), paths)
	m.assistantsChanged()
	return m.assistant(dir)
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
	return m.AssistantMessageWith(dir, text, images, routine, nil)
}

// AssistantMessageWith is AssistantMessage with the settings chosen for a
// session it starts, over the assistant's own; a message that joins the
// ongoing topic keeps that session's.
func (m *Manager) AssistantMessageWith(dir, text string, images []string, routine string, chosen *Options) (Session, bool, error) {
	a, err := m.assistant(dir)
	if err != nil {
		return Session{}, false, err
	}
	if !a.Enabled {
		return Session{}, false, fmt.Errorf("%s is turned off: turn it on from its menu in the sidebar", a.Name)
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
	options := m.assistantOptions(a)
	if chosen != nil {
		options = mergeOptions(options, *chosen)
	}
	s, err := m.createWithMessage(a.Agent, a.Path, "", named, text, images, &options, routine)
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

// routineLabelOf is the name a routine's (or the workbench's) message
// starts with, which titles the session it starts.
func routineLabelOf(text string) string {
	for _, prefix := range []string{"[Routine: ", "[Workbench: "} {
		if strings.HasPrefix(text, prefix) {
			name, _, ok := strings.Cut(strings.TrimPrefix(text, prefix), "]")
			if ok {
				return name
			}
		}
	}
	return ""
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

// afterNew starts a new assistant's first conversation, in which it asks
// what it is for and writes its procedure, and says where it is.
func (m *Manager) afterNew(a Assistant) map[string]any {
	out := map[string]any{"assistant": a}
	if s, err := m.SetUpMessage(a.Dir); err != nil {
		out["setupError"] = err.Error()
	} else {
		out["session"] = s.ID
	}
	return out
}

// readable for tests: the templates as files.
var _ fs.FS = assistantTemplates

// assistantsAPI serves /api/assistants[/setup | /{dir}/(open|message|topic)].
func (s *Server) assistantsAPI(w http.ResponseWriter, r *http.Request, rest string) {
	m := s.Manager
	parts := strings.Split(rest, "/")
	switch {
	case rest == "" && r.Method == "GET":
		writeJSON(w, map[string]any{"root": m.assistantsRoot(), "assistants": m.Assistants(), "defaultAgent": m.defaultAgent(), "language": m.language(), "sync": m.syncState(), "installed": m.installedAgents(), "mossUpdates": m.mossState().Pending})
	case rest == "moss" && r.Method == "POST":
		// The newer Moss for the files the user changed: take it or keep theirs.
		var v struct{ Take bool }
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		if err := m.ResolveMoss(v.Take); err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case rest == "github" && r.Method == "POST":
		var v struct{ Name string }
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		st, err := m.BackUpToGitHub(v.Name)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, st)
	case rest == "sync" && r.Method == "POST":
		m.syncAssistants(true)
		writeJSON(w, m.syncState())
	case rest == "settings" && r.Method == "PATCH":
		var v struct {
			Language string
			Order    *[]string
		}
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		if v.Language != "" {
			if err := m.SetLanguage(v.Language); err != nil {
				bad(w, err)
				return
			}
		}
		if v.Order != nil {
			if err := m.SetAssistantOrder(*v.Order); err != nil {
				bad(w, err)
				return
			}
		}
		m.assistantsChanged()
		writeJSON(w, map[string]any{"language": m.language()})
	case rest == "main/welcome" && r.Method == "POST":
		s, err := m.Welcome()
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"session": s.ID})
	case rest == "catalogs" && r.Method == "GET":
		writeJSON(w, m.catalogSources())
	case rest == "catalogs" && r.Method == "PUT":
		var v []CatalogSource
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		if err := m.SetCatalogSources(v); err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, m.catalogSources())
	case rest == "catalog" && r.Method == "GET":
		url := r.URL.Query().Get("url")
		if url == "" {
			url = m.catalogSources()[0].URL
		}
		list, err := m.CatalogEntries(url)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{"catalog": url, "assistants": list})
	case rest == "install" && r.Method == "POST":
		var v struct{ Catalog, Path, Dir, Agent string }
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		a, err := m.InstallAssistant(v.Catalog, v.Path, v.Dir, v.Agent)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, a)
	case len(parts) == 2 && parts[1] == "update" && r.Method == "POST":
		var v struct {
			Apply   bool
			Choices map[string]string
			Commit  string
		}
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		u, err := m.UpdateFromCatalog(parts[0], v.Apply, v.Choices, v.Commit)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, u)
	case rest == "migrate" && r.Method == "POST":
		var v struct {
			Catalog    string
			Apply      bool
			UseCatalog []string
		}
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		if v.Catalog == "" {
			v.Catalog = m.catalogSources()[0].URL
		}
		items, err := m.MigrateAssistants(v.Catalog, v.Apply, v.UseCatalog)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"catalog": v.Catalog, "applied": v.Apply, "assistants": items})
	case rest == "reload" && r.Method == "POST":
		// Read the assistant.toml files again (they are not watched).
		m.assistantsChanged()
		m.rt.poke()
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
		var v struct{ Root, Agent, Name, Language string }
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		created, err := m.setUp(v.Root, v.Agent, v.Name, v.Language)
		if err != nil {
			bad(w, err)
			return
		}
		out := map[string]any{"root": m.assistantsRoot(), "assistants": m.Assistants()}
		// A new personal assistant greets the user first.
		if created {
			if s, err := m.Welcome(); err != nil {
				out["welcomeError"] = err.Error()
			} else {
				out["session"] = s.ID
			}
		}
		writeJSON(w, out)
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
		// Made in the page: it asks the user what it is for.
		writeJSON(w, m.afterNew(a))
	case len(parts) == 1 && parts[0] != "" && r.Method == "PATCH":
		var v AssistantPatch
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		a, err := m.PatchAssistant(parts[0], v)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, a)
	case len(parts) == 2 && parts[1] == "icon":
		m.assistantIconAPI(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "open" && r.Method == "POST":
		out, err := m.OpenAssistant(parts[0])
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, out)
	case len(parts) == 2 && parts[1] == "setup" && r.Method == "POST":
		// Set it up now: its set-up conversation (welcome.go).
		s, err := m.SetUpMessage(parts[0])
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"session": s.ID})
	case len(parts) == 2 && parts[1] == "memory" && r.Method == "POST":
		started, err := m.IngestNow(parts[0], "")
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"started": started})
	case len(parts) == 2 && parts[1] == "topic" && r.Method == "POST":
		if err := m.NewTopic(parts[0]); err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case len(parts) == 2 && parts[1] == "message" && r.Method == "POST":
		var v struct {
			Text    string
			Images  []string `json:",omitempty"`
			Files   []string `json:",omitempty"`
			Options *Options `json:",omitempty"`
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
		session, _, err := m.AssistantMessageWith(parts[0], text, v.Images, "", v.Options)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, session)
	case len(parts) == 2 && parts[1] == "options" && r.Method == "GET":
		// What a session it starts now would use (the page's choices start here).
		a, err := m.assistant(parts[0])
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, m.assistantOptions(a))
	default:
		http.NotFound(w, r)
	}
}
