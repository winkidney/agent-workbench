package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	return rel == "CLAUDE.md" || rel == ".claude/skills" || strings.HasPrefix(rel, ".claude/skills/") || rel == ".claude/SKILLS.md"
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
	// Routines' switches, by name.
	Routines map[string]RoutineSetting `toml:"routines,omitempty"`
}

type RoutineSetting struct {
	Enabled *bool `toml:"enabled,omitempty"`
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
	return s.Name == "" && s.Agent == "" && s.Sessions == "" && s.Enabled == nil && s.Ready == nil && s.Icon == "" && len(s.Options) == 0 && len(s.Routines) == 0
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
		if r.Enabled == nil {
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
			fmt.Fprintf(&b, "%s = %s\n", k, strconv.Quote(v))
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
		fmt.Fprintf(&b, "\n[routines.%s]\n", strconv.Quote(name))
		boolean("enabled", s.Routines[name].Enabled)
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
	for i, r := range f.Routines {
		if rs, ok := s.Routines[r.Name]; ok && rs.Enabled != nil {
			f.Routines[i].Enabled = rs.Enabled
		}
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
