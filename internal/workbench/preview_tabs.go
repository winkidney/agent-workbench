package workbench

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Preview tabs: what a session's preview panel had open, so it can be found
// and opened again from any device. Only the targets are kept, the six most
// recently opened; a tab's content loads when it is shown and is gone when
// it is not (the page holds no state of its own).

const maxPreviewTabs = 6

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
	// Read only: a file's size and time (to tell it changed), a file gone,
	// a service's preview still running.
	Stamp   string `json:"stamp,omitempty"`
	Missing bool   `json:"missing,omitempty"`
	Running bool   `json:"running,omitempty"`
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
			t.Stamp = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
		case "service":
			if t.Preview != "" {
				if p := m.FindPreview(t.Preview); p != nil && p.Session == id {
					t.Running = true
				}
			}
		}
	}
	return v
}

// SetPreviewTabs saves a session's tabs after checking each target.
func (m *Manager) SetPreviewTabs(id string, v PreviewTabs) (PreviewTabs, error) {
	if len(v.Tabs) > maxPreviewTabs {
		v.Tabs = v.Tabs[:maxPreviewTabs]
	}
	clean := make([]PreviewTab, 0, len(v.Tabs))
	for _, t := range v.Tabs {
		t.Stamp, t.Missing, t.Running = "", false, false
		switch t.Kind {
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
			return v, errors.New("a tab is a file or a service")
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
