package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// An assistant's definition and its owner's data (ADR 0031). Its author
// keeps the definition: assistant.toml, the procedure, its scripts and
// skills, which a catalog installs and updates. Everything else in its
// folder is the owner's: state, log, records, config.toml and
// settings.toml, the owner's choices that take precedence over
// assistant.toml.

// definitionFile is whether a path in an assistant's folder (slash
// separated, relative to it) is part of its definition.
func definitionFile(rel string) bool {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	// Caches a script leaves behind are no one's definition.
	if strings.Contains("/"+rel, "/__pycache__/") || strings.HasSuffix(rel, ".pyc") || strings.HasSuffix(rel, "/.DS_Store") {
		return false
	}
	switch rel {
	case "assistant.toml", "AGENTS.md", "config.example.toml", "icon.png", ".gitignore", ".claude/settings.json":
		return true
	}
	for _, dir := range []string{"tools/", ".agents/", "examples/"} {
		if strings.HasPrefix(rel, dir) {
			return true
		}
	}
	return false
}

// generatedFile: made where an assistant is installed, for one agent or the
// other to read the same definition (CLAUDE.md imports AGENTS.md;
// .claude/skills links to .agents/skills), so never distributed.
func generatedFile(rel string) bool {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	return rel == "CLAUDE.md" || rel == agentsOverride || rel == ".claude/skills" || strings.HasPrefix(rel, ".claude/skills/") || rel == ".claude/SKILLS.md"
}

const settingsFile = "settings.toml"

// AssistantSettings are the owner's choices for an assistant
// (settings.toml); a key that is set overrides assistant.toml.
type AssistantSettings struct {
	Name     string                  `toml:"name,omitempty"`
	Agent    string                  `toml:"agent,omitempty"`
	Sessions string                  `toml:"sessions,omitempty"`
	Enabled  *bool                   `toml:"enabled,omitempty"`
	Ready    *bool                   `toml:"ready,omitempty"`
	Icon     string                  `toml:"icon,omitempty"`
	Options  map[string]AgentOptions `toml:"options,omitempty"`
	// What the assistant's card says, over the author's.
	Description  string   `toml:"description,omitempty"`
	Capabilities []string `toml:"capabilities,omitempty"`
	Examples     []string `toml:"examples,omitempty"`
	Requires     []string `toml:"requires,omitempty"`
	// Routines by name: one the author has takes the keys set here; any
	// other name is the owner's own routine.
	Routines map[string]RoutineSetting `toml:"routines,omitempty"`
}

// RoutineSetting is a routine of settings.toml: what it changes of the
// author's routine of that name, or all of the owner's own.
type RoutineSetting struct {
	Enabled        *bool    `toml:"enabled,omitempty"`
	Daily          string   `toml:"daily,omitempty"`
	Days           []string `toml:"days,omitempty"`
	Every          string   `toml:"every,omitempty"`
	Cron           string   `toml:"cron,omitempty"`
	At             string   `toml:"at,omitempty"`
	Message        string   `toml:"message,omitempty"`
	Command        string   `toml:"command,omitempty"`
	Timeout        int      `toml:"timeout,omitempty"`
	NotifyOnOutput *bool    `toml:"notify_on_output,omitempty"`
	Background     *bool    `toml:"background,omitempty"`
	Approve        *bool    `toml:"approve,omitempty"`
	Preview        string   `toml:"preview,omitempty"`
}

func (r RoutineSetting) when() bool {
	return r.Daily != "" || r.Every != "" || r.Cron != "" || r.At != ""
}

// own: complete enough to be a routine of the owner's own.
func (r RoutineSetting) own() bool { return r.when() && (r.Message != "" || r.Command != "") }

func (r RoutineSetting) onlySwitch() bool {
	r.Enabled = nil
	return reflect.DeepEqual(r, RoutineSetting{})
}

// lay puts what r sets over the routine f.
func (r RoutineSetting) lay(f *FileRoutine) {
	if r.when() {
		// Another time replaces the author's, not adds to it.
		f.Daily, f.Days, f.Every, f.Cron, f.At = r.Daily, r.Days, r.Every, r.Cron, r.At
	} else if r.Days != nil {
		f.Days = r.Days
	}
	for _, p := range []struct {
		to *string
		v  string
	}{{&f.Message, r.Message}, {&f.Command, r.Command}, {&f.Preview, r.Preview}} {
		if p.v != "" {
			*p.to = p.v
		}
	}
	if r.Timeout != 0 {
		f.Timeout = r.Timeout
	}
	for _, p := range []struct {
		to *bool
		v  *bool
	}{{&f.NotifyOnOutput, r.NotifyOnOutput}, {&f.Background, r.Background}, {&f.Approve, r.Approve}} {
		if p.v != nil {
			*p.to = *p.v
		}
	}
	// No approval: nothing to preview either.
	if r.Approve != nil && !*r.Approve && r.Preview == "" {
		f.Preview = ""
	}
	if r.Enabled != nil {
		f.Enabled = r.Enabled
	}
}

// checkOwnRoutines: an owner's own routine (a name the author does not
// have) says when it runs and what it does.
func (s AssistantSettings) checkOwnRoutines(f AssistantFile) error {
	for name, r := range s.Routines {
		// Changes to a routine the author has since removed (no time, or
		// nothing to do, of their own) are left unused: an update must not
		// break the assistant. The update says so (settingsNotes).
		if inRoutines(f.Routines, name) || !r.own() {
			continue
		}
	}
	return nil
}

func inRoutines(rs []FileRoutine, name string) bool {
	for _, r := range rs {
		if r.Name == name {
			return true
		}
	}
	return false
}

func readSettings(dir string) (AssistantSettings, error) {
	var s AssistantSettings
	md, err := toml.DecodeFile(filepath.Join(dir, settingsFile), &s)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("%s: %w", settingsFile, err)
	}
	if extra := md.Undecoded(); len(extra) > 0 {
		return s, fmt.Errorf("%s: unknown setting %q", settingsFile, extra[0].String())
	}
	return s, nil
}

const settingsHeader = `# Your choices for this assistant, which Agent Workbench and the assistant
# write: they take precedence over assistant.toml, which its author keeps.
# This file is yours, and is never published or replaced by an update.

`

func (s AssistantSettings) empty() bool {
	return s.Name == "" && s.Agent == "" && s.Sessions == "" && s.Enabled == nil && s.Ready == nil && s.Icon == "" && len(s.Options) == 0 && len(s.Routines) == 0 && s.Description == "" && s.Capabilities == nil && s.Examples == nil && s.Requires == nil
}

// writeAssistantSettings writes settings.toml, or removes it when nothing
// is set. Written by hand: only the keys set, in a fixed order.
func writeAssistantSettings(dir string, s AssistantSettings) error {
	path := filepath.Join(dir, settingsFile)
	for agent, o := range s.Options {
		if o == (AgentOptions{}) {
			delete(s.Options, agent)
		}
	}
	for name, r := range s.Routines {
		if reflect.DeepEqual(r, RoutineSetting{}) {
			delete(s.Routines, name)
		}
	}
	if s.empty() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	var b strings.Builder
	b.WriteString(settingsHeader)
	str := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s = %s\n", k, tomlQuote(v))
		}
	}
	boolean := func(k string, v *bool) {
		if v != nil {
			fmt.Fprintf(&b, "%s = %t\n", k, *v)
		}
	}
	str("name", s.Name)
	str("agent", s.Agent)
	str("sessions", s.Sessions)
	boolean("enabled", s.Enabled)
	boolean("ready", s.Ready)
	str("icon", s.Icon)
	str("description", s.Description)
	list := func(k string, v []string) {
		if v != nil {
			fmt.Fprintf(&b, "%s = %s\n", k, tomlStrings(v))
		}
	}
	list("capabilities", s.Capabilities)
	list("examples", s.Examples)
	list("requires", s.Requires)
	for _, agent := range []string{"claude", "codex"} {
		o, ok := s.Options[agent]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "\n[options.%s]\n", agent)
		str("model", o.Model)
		str("effort", o.Effort)
		str("mode", o.Mode)
		str("permissions", o.Permissions)
	}
	names := make([]string, 0, len(s.Routines))
	for name := range s.Routines {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		r := s.Routines[name]
		fmt.Fprintf(&b, "\n[routines.%s]\n", strconv.Quote(name))
		boolean("enabled", r.Enabled)
		str("daily", r.Daily)
		list("days", r.Days)
		str("every", r.Every)
		str("cron", r.Cron)
		str("at", r.At)
		str("message", r.Message)
		str("command", r.Command)
		if r.Timeout != 0 {
			fmt.Fprintf(&b, "timeout = %d\n", r.Timeout)
		}
		boolean("notify_on_output", r.NotifyOnOutput)
		boolean("background", r.Background)
		boolean("approve", r.Approve)
		str("preview", r.Preview)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// apply lays the owner's settings over the author's file.
func (s AssistantSettings) apply(f *AssistantFile) {
	if s.Name != "" {
		f.Name = s.Name
	}
	if s.Agent != "" {
		f.Agent = s.Agent
	}
	if s.Sessions != "" {
		f.Sessions = s.Sessions
	}
	if s.Enabled != nil {
		f.Enabled = s.Enabled
	}
	if s.Ready != nil {
		f.Ready = s.Ready
	}
	if s.Icon != "" {
		f.Icon = s.Icon
	}
	if len(s.Options) > 0 {
		if f.Options == nil {
			f.Options = map[string]AgentOptions{}
		}
		// An agent's table is the owner's whole choice (the settings form
		// sends all of it), so it replaces the author's.
		for agent, o := range s.Options {
			f.Options[agent] = o
		}
	}
	if s.Description != "" {
		f.Description = s.Description
	}
	for _, p := range []struct {
		to *[]string
		v  []string
	}{{&f.Capabilities, s.Capabilities}, {&f.Examples, s.Examples}, {&f.Requires, s.Requires}} {
		if p.v != nil {
			*p.to = p.v
		}
	}
	for i, r := range f.Routines {
		if rs, ok := s.Routines[r.Name]; ok {
			rs.lay(&f.Routines[i])
		}
	}
	// The owner's own routines, after the author's, by name.
	var own []string
	for name := range s.Routines {
		if !inRoutines(f.Routines, name) && s.Routines[name].own() {
			own = append(own, name)
		}
	}
	sort.Strings(own)
	for _, name := range own {
		r := FileRoutine{Name: name}
		s.Routines[name].lay(&r)
		f.Routines = append(f.Routines, r)
	}
}

// definitionFiles lists an assistant folder's definition files (slash
// separated, sorted); symbolic links are not files of it.
func definitionFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (generatedFile(rel) || rel == ".git") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || generatedFile(rel) || !definitionFile(rel) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out, err
}

// The owner's additions to a procedure (AGENTS.local.md): data of theirs
// beside the author's AGENTS.md, which both agents read after it. Claude
// imports it from CLAUDE.md; Codex reads AGENTS.override.md in place of
// AGENTS.md, made here from the two.
const (
	agentsLocal    = "AGENTS.local.md"
	agentsOverride = "AGENTS.override.md"
	overrideHeader = "<!-- Made by Agent Workbench from AGENTS.md and AGENTS.local.md: edit those, not this. -->\n\n"
	claudePlain    = "@AGENTS.md\n"
	claudeLocal    = "@AGENTS.md\n@AGENTS.local.md\n"
)

// writeAgentFiles brings CLAUDE.md and AGENTS.override.md in dir in step
// with its AGENTS.md and AGENTS.local.md. AGENTS.override.md is always
// the workbench's (one written by anyone else would replace AGENTS.md for
// Codex); a CLAUDE.md someone wrote by hand is left alone.
func writeAgentFiles(dir string) error {
	override := filepath.Join(dir, agentsOverride)
	old, oerr := os.ReadFile(override)
	agents, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		if oerr == nil {
			return os.Remove(override)
		}
		return nil
	}
	local, lerr := os.ReadFile(filepath.Join(dir, agentsLocal))
	hasLocal := lerr == nil && len(bytes.TrimSpace(local)) > 0
	claude := filepath.Join(dir, "CLAUDE.md")
	want := claudePlain
	if hasLocal {
		want = claudeLocal
	}
	if info, err := os.Lstat(claude); err == nil && info.Mode().IsRegular() {
		cur, _ := os.ReadFile(claude)
		// A checkout with Windows line ends has the same lines.
		crlf := bytes.Contains(cur, []byte("\r\n"))
		plain := strings.ReplaceAll(string(cur), "\r\n", "\n")
		if (plain == claudePlain || plain == claudeLocal) && plain != want {
			out := want
			if crlf {
				out = strings.ReplaceAll(want, "\n", "\r\n")
			}
			if err := writeAtomic(claude, []byte(out)); err != nil {
				return err
			}
		}
	}
	if !hasLocal {
		if oerr == nil {
			return os.Remove(override)
		}
		return nil
	}
	var b bytes.Buffer
	b.WriteString(overrideHeader)
	b.Write(bytes.TrimRight(agents, "\r\n"))
	b.WriteString("\n\n## The owner's additions (AGENTS.local.md)\n\nWhere these differ from the above, these win.\n\n")
	b.Write(bytes.TrimRight(local, "\r\n"))
	b.WriteString("\n")
	if oerr == nil && bytes.Equal(old, b.Bytes()) {
		return nil
	}
	return writeAtomic(override, b.Bytes())
}

// writeAtomic replaces a file whole, so an agent starting meanwhile never
// reads half of it.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".aw-tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// agentFiles are the files writeAgentFiles makes, which git may not show
// (excluded): a background run's changes to them are put back.
func snapshotAgentFiles(root string) map[string][]byte {
	out := map[string][]byte{}
	dirs := []string{"."}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, e.Name())
		}
	}
	for _, d := range dirs {
		for _, f := range []string{"CLAUDE.md", agentsOverride} {
			rel := filepath.ToSlash(filepath.Join(d, f))
			b, err := os.ReadFile(filepath.Join(root, d, f))
			if err != nil {
				b = nil
			} else if b == nil {
				b = []byte{}
			}
			out[rel] = b
		}
	}
	return out
}

// putBackAgentFiles undoes changes to them since the snapshot (a new one
// removed); it returns what it put back.
func putBackAgentFiles(root string, before map[string][]byte) []string {
	var back []string
	now := snapshotAgentFiles(root)
	for rel, cur := range now {
		was, known := before[rel]
		if !known {
			was = nil
		}
		if (cur == nil) == (was == nil) && bytes.Equal(cur, was) {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		if was == nil {
			if os.Remove(p) == nil {
				back = append(back, rel)
			}
		} else if writeAtomic(p, was) == nil {
			back = append(back, rel)
		}
	}
	sort.Strings(back)
	return back
}

// writeAllAgentFiles does it for the repository's top and every folder in
// it.
func writeAllAgentFiles(root string) {
	if root == "" {
		return
	}
	writeAgentFiles(root)
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			writeAgentFiles(filepath.Join(root, e.Name()))
		}
	}
}

// agentFilesFor brings the agent files in step for a session in path: its
// assistant's folder and the repository's top, when path is inside it.
func (m *Manager) agentFilesFor(path string) {
	root := m.assistantsRoot()
	if root == "" || path == "" {
		return
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return
	}
	// Not while a background run works: it would take the change for the
	// run's and undo it. The files are as of its start anyway.
	if !m.as.bg.TryLock() {
		return
	}
	defer m.as.bg.Unlock()
	writeAgentFiles(root)
	if first := strings.Split(filepath.ToSlash(rel), "/")[0]; first != "." && first != "" {
		writeAgentFiles(filepath.Join(root, first))
	}
}

// tomlQuote writes s as a TOML basic string (Go's quoting writes \x..,
// which TOML does not read).
func tomlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
