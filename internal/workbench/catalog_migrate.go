package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Migrating a repository of assistants made before catalogs (ADR 0031):
// each assistant the catalog also has is bound to it. Its definition files
// that match the catalog leave the repository (the catalog has them);
// those that differ stay as the user's changes. Its assistant.toml becomes
// the catalog's when what differs is only the user's settings, which move
// to settings.toml.

// MigrationItem is what migrating does, or did, to one assistant.
type MigrationItem struct {
	Dir     string `json:"dir"`
	Catalog string `json:"catalog,omitempty"`
	// Definition files that match the catalog: no longer committed.
	FromCatalog []string `json:"fromCatalog,omitempty"`
	// Definition files that differ: kept as the user's changes.
	Changed []string `json:"changed,omitempty"`
	// Definition files the catalog has and the folder lacks: written.
	Added []string `json:"added,omitempty"`
	// Settings moved out of assistant.toml into settings.toml.
	Settings []string `json:"settings,omitempty"`
	// Why it is left as it is.
	Skipped string `json:"skipped,omitempty"`
}

// MigrateAssistants binds the assistants the catalog at url also has; with
// apply false it only says what it would do. A file in useCatalog
// ("dir/rel") takes the catalog's version over the user's.
func (m *Manager) MigrateAssistants(url string, apply bool, useCatalog []string) ([]MigrationItem, error) {
	root := m.assistantsRoot()
	if root == "" {
		return nil, errors.New("set up assistants first")
	}
	if apply {
		m.as.lockMu.Lock()
		defer m.as.lockMu.Unlock()
		if out, err := gitRun(root, "status", "--porcelain"); err != nil || strings.TrimSpace(out) != "" {
			return nil, fmt.Errorf("commit or put aside the changes in %s first:\n%s", root, out)
		}
	}
	clone, commit, err := m.fetchCatalog(url)
	if err != nil {
		return nil, err
	}
	take := map[string]bool{}
	for _, f := range useCatalog {
		take[f] = true
	}
	lock, err := readLock(root)
	if err != nil {
		return nil, err
	}
	var items []MigrationItem
	var bound []string
	for _, a := range m.Assistants() {
		item := MigrationItem{Dir: a.Dir}
		switch {
		case a.Dir == "main":
			item.Skipped = "Moss comes with Agent Workbench"
		case lock.Assistants[a.Dir].Catalog != "":
			item.Skipped = "already from " + lock.Assistants[a.Dir].Catalog
		case a.Error != "":
			item.Skipped = "its assistant.toml has an error: " + a.Error
		}
		if item.Skipped != "" {
			items = append(items, item)
			continue
		}
		files, err := catalogFiles(clone, commit, a.Dir)
		if err != nil {
			item.Skipped = "the catalog has no " + a.Dir
			items = append(items, item)
			continue
		}
		item.Catalog = url
		entry := LockEntry{Catalog: url, Path: a.Dir, Commit: commit, Files: map[string]string{}}
		var write []catalogFile
		var settings AssistantSettings
		for _, f := range files {
			entry.Files[f.Rel] = digest(f.Data)
			path := filepath.Join(a.Path, filepath.FromSlash(f.Rel))
			mine, err := os.ReadFile(path)
			switch {
			case err != nil:
				item.Added = append(item.Added, f.Rel)
				write = append(write, f)
			case bytes.Equal(mine, f.Data):
				item.FromCatalog = append(item.FromCatalog, f.Rel)
			case take[a.Dir+"/"+f.Rel]:
				item.FromCatalog = append(item.FromCatalog, f.Rel)
				write = append(write, f)
			case f.Rel == "assistant.toml":
				if s, moved, ok := settingsApart(mine, f.Data); ok {
					settings, item.Settings = s, moved
					item.FromCatalog = append(item.FromCatalog, f.Rel)
					write = append(write, f)
				} else {
					item.Changed = append(item.Changed, f.Rel)
				}
			default:
				item.Changed = append(item.Changed, f.Rel)
			}
		}
		items = append(items, item)
		if !apply {
			continue
		}
		if len(item.Settings) > 0 {
			old, err := readSettings(a.Path)
			if err != nil {
				return items, err
			}
			if !old.empty() {
				return items, fmt.Errorf("%s already has a settings.toml", a.Dir)
			}
			if err := writeAssistantSettings(a.Path, settings); err != nil {
				return items, err
			}
		}
		if err := writeDefinition(a.Path, write); err != nil {
			return items, err
		}
		lock.Assistants[a.Dir] = entry
		bound = append(bound, a.Dir)
	}
	if !apply {
		return items, nil
	}
	if err := writeLock(root, lock); err != nil {
		return items, err
	}
	m.commitPaths(root, "Bind the assistants to their catalog "+url+"@"+short(commit), append([]string{lockFile}, bound...))
	m.assistantsChanged()
	return items, nil
}

// settingsApart: whether a user's assistant.toml is the catalog's with the
// user's settings, and those settings if so (what moves, by name).
func settingsApart(mine, theirs []byte) (AssistantSettings, []string, bool) {
	var u, c AssistantFile
	if _, err := toml.Decode(string(mine), &u); err != nil {
		return AssistantSettings{}, nil, false
	}
	if _, err := toml.Decode(string(theirs), &c); err != nil {
		return AssistantSettings{}, nil, false
	}
	var s AssistantSettings
	var moved []string
	if u.Name != c.Name {
		s.Name, moved = u.Name, append(moved, "name")
	}
	if u.Agent != c.Agent {
		s.Agent, moved = u.Agent, append(moved, "agent")
	}
	if u.Sessions != c.Sessions && u.Sessions != "" {
		s.Sessions, moved = u.Sessions, append(moved, "sessions")
	}
	if !sameBool(u.Enabled, c.Enabled, true) {
		s.Enabled, moved = boolOr(u.Enabled, true), append(moved, "enabled")
	}
	if !sameBool(u.Ready, c.Ready, true) {
		s.Ready, moved = boolOr(u.Ready, true), append(moved, "ready")
	}
	if u.Icon != c.Icon {
		s.Icon, moved = u.Icon, append(moved, "icon")
	}
	for _, agent := range []string{"claude", "codex"} {
		if u.Options[agent] != c.Options[agent] {
			if s.Options == nil {
				s.Options = map[string]AgentOptions{}
			}
			s.Options[agent] = u.Options[agent]
			moved = append(moved, "options."+agent)
		}
	}
	theirsByName := map[string]FileRoutine{}
	for _, r := range c.Routines {
		theirsByName[r.Name] = r
	}
	for _, r := range u.Routines {
		cr := theirsByName[r.Name]
		if !sameBool(r.Enabled, cr.Enabled, true) {
			if s.Routines == nil {
				s.Routines = map[string]RoutineSetting{}
			}
			s.Routines[r.Name] = RoutineSetting{Enabled: boolOr(r.Enabled, true)}
			moved = append(moved, "routine "+r.Name)
		}
	}
	// What is left must be the catalog's file exactly.
	s.apply(&c)
	normalize := func(f *AssistantFile) {
		for i := range f.Routines {
			if f.Routines[i].Enabled == nil {
				f.Routines[i].Enabled = boolOr(nil, true)
			}
		}
		f.Enabled, f.Ready = boolOr(f.Enabled, true), boolOr(f.Ready, true)
		if len(f.Options) == 0 {
			f.Options = nil
		}
		for k, o := range f.Options {
			if o == (AgentOptions{}) {
				delete(f.Options, k)
			}
		}
	}
	normalize(&u)
	normalize(&c)
	if !reflect.DeepEqual(u, c) {
		return AssistantSettings{}, nil, false
	}
	sort.Strings(moved)
	return s, moved, true
}

func sameBool(a, b *bool, missing bool) bool {
	return *boolOr(a, missing) == *boolOr(b, missing)
}

func boolOr(v *bool, missing bool) *bool {
	if v != nil {
		x := *v
		return &x
	}
	return &missing
}
