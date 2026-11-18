package workbench

import (
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Preview tabs: what a session's preview panel had open, so it can be found
// and opened again from any device. Only the targets are kept, the six most
// recently opened; a tab's content loads when it is shown and is gone when
// it is not (the page holds no state of its own). Terminal tabs are kept
// besides those six: a terminal lives until it is closed (terminal.go).

const (
	maxPreviewTabs  = 6
	maxTerminalTabs = 2 * termPerSession
)

// PreviewTab is one opened target.
type PreviewTab struct {
	// file: Path, relative to the session's directory. service: Target (a
	// loopback URL), or Command and Port for one the workbench starts.
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Target  string `json:"target,omitempty"`
	Command string `json:"command,omitempty"`
	Port    int    `json:"port,omitempty"`
	// The preview serving a service, while it runs.
	Preview string `json:"preview,omitempty"`
	// terminal: the terminal shown, and its name ("Terminal 2").
	Terminal string `json:"terminal,omitempty"`
	Title    string `json:"title,omitempty"`
	// Read only: a file's size and time (to tell it changed), a file gone,
	// a service's preview still running.
	Stamp   string `json:"stamp,omitempty"`
	Missing bool   `json:"missing,omitempty"`
	Running bool   `json:"running,omitempty"`
	// Read only: an ended terminal's exit status.
	Code *int `json:"code,omitempty"`
}

// PreviewTabs are a session's tabs, most recently opened first, and the one
// shown (-1: the file list).
type PreviewTabs struct {
	Tabs   []PreviewTab `json:"tabs"`
	Active int          `json:"active"`
}

// sessionDir is a session's launch directory, loaded or not.
func (m *Manager) sessionDir(id string) string {
	m.mu.Lock()
	r := m.find(id)
	m.mu.Unlock()
	if r != nil {
		return r.session.Cwd
	}
	if s, ok := m.catalog.Find(id); ok {
		return s.Cwd
	}
	return ""
}

// PreviewTabsOf reads a session's tabs, with what each now finds.
func (m *Manager) PreviewTabsOf(id string) PreviewTabs {
	v, ok := m.store.previewTabs(id)
	if !ok {
		v = PreviewTabs{Active: -1}
	}
	if v.Tabs == nil {
		v.Tabs = []PreviewTab{}
	}
	if v.Active < -1 || v.Active >= len(v.Tabs) {
		v.Active = -1
	}
	root := m.sessionDir(id)
	for i := range v.Tabs {
		t := &v.Tabs[i]
		switch t.Kind {
		case "file":
			full, err := confined(root, t.Path)
			info, statErr := os.Stat(full)
			if err != nil || statErr != nil || !info.Mode().IsRegular() {
				t.Missing = true
				continue
			}
			t.Stamp = fileStamp(info)
		case "service":
			if t.Preview != "" {
				if p := m.FindPreview(t.Preview); p != nil && p.Session == id {
					t.Running = true
				}
			}
		case "terminal":
			t.Running, t.Code = m.terminalState(id, t.Terminal)
			t.Missing = !t.Running && t.Code == nil
		}
	}
	return v
}

// SetPreviewTabs saves a session's tabs after checking each target.
func (m *Manager) SetPreviewTabs(id string, v PreviewTabs) (PreviewTabs, error) {
	clean := make([]PreviewTab, 0, len(v.Tabs))
	others, terms := 0, 0
	for _, t := range v.Tabs {
		t.Stamp, t.Missing, t.Running, t.Code = "", false, false, nil
		// Six files and services, most recent first; terminals besides.
		if t.Kind == "terminal" {
			if terms++; terms > maxTerminalTabs {
				continue
			}
		} else if others++; others > maxPreviewTabs {
			continue
		}
		switch t.Kind {
		case "terminal":
			if len(t.Terminal) != 48 || strings.Trim(t.Terminal, "0123456789abcdef") != "" {
				return v, errors.New("a terminal tab names a terminal")
			}
			title := t.Title
			if len(title) > 64 {
				title = title[:64]
			}
			t = PreviewTab{Kind: "terminal", Terminal: t.Terminal, Title: title}
		case "file":
			// As a preview names it ("/report.html"), relative to the session.
			p := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(t.Path, "/")))
			if p == "." || p == ".." || strings.HasPrefix(p, "../") || filepath.IsAbs(p) || len(p) > 4096 {
				return v, errors.New("a file tab names a path inside the session's directory")
			}
			t = PreviewTab{Kind: "file", Path: p}
		case "service":
			if t.Command != "" {
				if len(t.Command) > 4096 || t.Port < 1024 || t.Port > 65535 {
					return v, errors.New("a started service needs its command and a port between 1024 and 65535")
				}
				t = PreviewTab{Kind: "service", Command: t.Command, Port: t.Port, Preview: t.Preview}
			} else {
				u, err := url.Parse(t.Target)
				if err != nil || u.Scheme != "http" || u.User != nil || len(t.Target) > 2048 {
					return v, errors.New("a service tab names an http loopback URL")
				}
				if ip := net.ParseIP(u.Hostname()); ip == nil || !ip.IsLoopback() {
					return v, errors.New("a service tab names an http loopback URL")
				}
				t = PreviewTab{Kind: "service", Target: t.Target, Preview: t.Preview}
			}
			if len(t.Preview) > 64 {
				t.Preview = ""
			}
		default:
			return v, errors.New("a tab is a file, a service or a terminal")
		}
		clean = append(clean, t)
	}
	v.Tabs = clean
	if v.Active < -1 || v.Active >= len(v.Tabs) {
		v.Active = -1
	}
	if err := m.store.setPreviewTabs(id, v); err != nil {
		return v, err
	}
	return m.PreviewTabsOf(id), nil
}
