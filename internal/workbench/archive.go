package workbench

import (
	"errors"
	"time"
)

// Archive, Unarchive and Delete use the Codex app-server's own thread
// requests, so the Codex app and CLI see the same change. Claude Code has no
// such operations for local sessions; there the sidebar offers Hide.

func (m *Manager) Archive(id string) error   { return m.threadOp(id, "thread/archive") }
func (m *Manager) Unarchive(id string) error { return m.threadOp(id, "thread/unarchive") }

// Delete removes the Codex thread for good, then everything the workbench
// kept about it.
func (m *Manager) Delete(id string) error {
	if err := m.threadOp(id, "thread/delete"); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
	if err := m.store.Forget(id); err != nil {
		return err
	}
	m.prefsMu.Lock()
	p := m.Prefs()
	p.PinnedSessions = without(p.PinnedSessions, id)
	p.Hidden = without(p.Hidden, id)
	delete(p.Aliases, id)
	err := m.savePrefs(p)
	m.prefsMu.Unlock()
	m.emit(id, "deleted", map[string]any{"id": id})
	return err
}

func (m *Manager) threadOp(id, method string) error {
	if err := m.Attach(id); err != nil {
		return err
	}
	m.mu.Lock()
	r := m.find(id)
	if r == nil {
		m.mu.Unlock()
		return errors.New("session not found")
	}
	s := r.session
	busy := r.turn || len(r.pending) > 0 || len(r.queue) > 0
	live := r.proc != nil
	m.mu.Unlock()
	if s.Agent != "codex" || s.NativeID == "" {
		return errors.New("Claude Code has no native archive or delete; hide the session instead")
	}
	if busy {
		return errors.New("finish or interrupt the current turn first")
	}
	if m.isExternal(s) {
		return errors.New("session is active in another client")
	}
	// The thread must not stay loaded in our engine while it is archived or
	// deleted.
	if live {
		if err := m.Stop(id, false); err != nil {
			return err
		}
	}
	if err := m.codexThread(method, map[string]any{"threadId": s.NativeID}); err != nil {
		return err
	}
	m.catalog.mu.Lock()
	m.catalog.updated = time.Time{}
	m.catalog.mu.Unlock()
	if method == "thread/delete" {
		return nil
	}
	m.mu.Lock()
	if r := m.find(id); r != nil {
		r.session.Archived = method == "thread/archive"
		m.status(r, r.session.Status)
	}
	m.mu.Unlock()
	return nil
}

func codexThreadRequest(method string, params map[string]any) error {
	return withCodexClient("workbench_threads", func(request codexRequest) error {
		_, err := request("thread", method, params)
		return err
	})
}
