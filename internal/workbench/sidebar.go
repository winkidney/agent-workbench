package workbench

import (
	"errors"
	"strings"
	"time"
)

// SidebarPrefs is workbench-only organisation of native sessions. It never
// changes native records; aliases only affect how titles are displayed.
type SidebarPrefs struct {
	PinnedSessions   []string          `json:"pinnedSessions"`
	PinnedWorkspaces []string          `json:"pinnedWorkspaces"`
	Hidden           []string          `json:"hidden"`
	Aliases          map[string]string `json:"aliases"`
}

const prefsLimit = 200

func (m *Manager) Prefs() SidebarPrefs {
	var p SidebarPrefs
	m.store.readSetting("sidebar", &p)
	if p.PinnedSessions == nil {
		p.PinnedSessions = []string{}
	}
	if p.PinnedWorkspaces == nil {
		p.PinnedWorkspaces = []string{}
	}
	if p.Hidden == nil {
		p.Hidden = []string{}
	}
	if p.Aliases == nil {
		p.Aliases = map[string]string{}
	}
	return p
}

func addUnique(list []string, id string) []string {
	for _, v := range list {
		if v == id {
			return list
		}
	}
	list = append(list, id)
	if len(list) > prefsLimit {
		list = list[len(list)-prefsLimit:]
	}
	return list
}

func without(list []string, id string) []string {
	out := list[:0:0]
	for _, v := range list {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}

func (m *Manager) savePrefs(p SidebarPrefs) error {
	if err := m.store.setting("sidebar", p); err != nil {
		return err
	}
	m.emit("", "prefs", p)
	return nil
}

// UpdatePrefs applies one pin / hide operation atomically.
func (m *Manager) UpdatePrefs(op, id string) (SidebarPrefs, error) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 4096 {
		return SidebarPrefs{}, errors.New("id is required")
	}
	m.prefsMu.Lock()
	defer m.prefsMu.Unlock()
	p := m.Prefs()
	switch op {
	case "pinSession":
		p.PinnedSessions = addUnique(p.PinnedSessions, id)
		p.Hidden = without(p.Hidden, id)
	case "unpinSession":
		p.PinnedSessions = without(p.PinnedSessions, id)
	case "pinWorkspace":
		p.PinnedWorkspaces = addUnique(p.PinnedWorkspaces, id)
	case "unpinWorkspace":
		p.PinnedWorkspaces = without(p.PinnedWorkspaces, id)
	case "hide":
		p.Hidden = addUnique(p.Hidden, id)
		p.PinnedSessions = without(p.PinnedSessions, id)
	case "unhide":
		p.Hidden = without(p.Hidden, id)
	default:
		return p, errors.New("unknown sidebar operation")
	}
	return p, m.savePrefs(p)
}

// Rename changes the native Codex thread name when possible; otherwise (and
// always for Claude, whose transcript the workbench never writes) it stores a
// workbench display alias. An empty title clears the alias.
func (m *Manager) Rename(id, title string) (bool, error) {
	title = strings.Join(strings.Fields(title), " ")
	if len(title) > 200 {
		return false, errors.New("title must be at most 200 bytes")
	}
	if err := m.Attach(id); err != nil {
		return false, err
	}
	m.mu.Lock()
	r := m.find(id)
	s := r.session
	live := r.proc != nil && r.ready
	m.mu.Unlock()
	native := false
	if s.Agent == "codex" && s.NativeID != "" && title != "" {
		params := map[string]any{"threadId": s.NativeID, "name": title}
		var err error
		if live {
			_, err = m.rpc(s.ID, "thread/name/set", params)
		} else {
			err = m.renameCodex(params)
		}
		if err == nil {
			native = true
			m.mu.Lock()
			if r := m.find(id); r != nil {
				r.session.Title = title
			}
			m.mu.Unlock()
			m.catalog.mu.Lock()
			m.catalog.updated = time.Time{}
			m.catalog.mu.Unlock()
		}
	}
	m.prefsMu.Lock()
	defer m.prefsMu.Unlock()
	p := m.Prefs()
	if native || title == "" {
		delete(p.Aliases, s.ID)
	} else {
		p.Aliases[s.ID] = title
	}
	return native, m.savePrefs(p)
}

func codexRename(params map[string]any) error {
	return withCodexClient("workbench_rename", func(request codexRequest) error {
		_, err := request("rename", "thread/name/set", params)
		return err
	})
}
