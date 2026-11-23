package workbench

import (
	"errors"
	"path/filepath"
	"strings"
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
	return m.forgetDeleted(id)
}

// forgetDeleted drops everything the workbench kept about a session whose
// Codex thread is gone.
func (m *Manager) forgetDeleted(id string) error {
	m.endTerminals(id, "session deleted")
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

func (m *Manager) codexThreadRequest(method string, params map[string]any) error {
	return m.catalog.withCodexClient("workbench_threads", func(request codexRequest) error {
		_, err := request("thread", method, params)
		return err
	})
}

// codexThreadRequests sends method for each thread over one app-server
// connection: starting one per thread would take minutes for a folder.
func (m *Manager) codexThreadRequests(method string, threads []string) map[string]error {
	failed := map[string]error{}
	err := m.catalog.withCodexClient("workbench_threads", func(request codexRequest) error {
		for _, t := range threads {
			if _, err := request("thread", method, map[string]any{"threadId": t}); err != nil {
				failed[t] = err
			}
		}
		return nil
	})
	if err != nil {
		for _, t := range threads {
			failed[t] = err
		}
	}
	return failed
}

// FolderResult is what archiving or deleting a folder's sessions did, or
// would do: Codex sessions archived or deleted, Claude sessions hidden.
type FolderResult struct {
	Codex   int          `json:"codex"`
	Claude  int          `json:"claude"`
	Skipped []FolderSkip `json:"skipped"`
	Failed  []FolderSkip `json:"failed"`
}

type FolderSkip struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// FolderSessions archives or deletes the Codex sessions started in a
// project folder or any folder under it, and hides its Claude sessions
// (Claude Code has neither). It takes the sessions the sidebar lists under
// the folder in Projects: not related ones started elsewhere, not
// assistants' or Other's. Sessions at work or open in another client are
// skipped. dryRun only counts.
func (m *Manager) FolderSessions(folder, op string, dryRun bool) (FolderResult, error) {
	res := FolderResult{Skipped: []FolderSkip{}, Failed: []FolderSkip{}}
	method := map[string]string{"archive": "thread/archive", "delete": "thread/delete"}[op]
	if method == "" {
		return res, errors.New("op must be archive or delete")
	}
	folder = strings.TrimRight(strings.TrimSpace(folder), `/\`)
	if folder == "" || !filepath.IsAbs(folder) {
		return res, errors.New("a project folder is required")
	}
	list, err := m.NativeList()
	if err != nil {
		return res, err
	}
	under := func(path, dir string) bool {
		return path == dir || strings.HasPrefix(path, dir+"/") || strings.HasPrefix(path, dir+string(filepath.Separator))
	}
	assistants := strings.TrimRight(m.assistantsRoot(), `/\`)
	hidden := map[string]bool{}
	for _, id := range m.Prefs().Hidden {
		hidden[id] = true
	}
	var codex []Session
	var claude []string
	for _, s := range list {
		ns := s.Workspace
		if ns == "" {
			ns = s.Cwd
		}
		if !under(ns, folder) || (s.Origin != "" && s.Origin != "project") || (assistants != "" && under(s.Cwd, assistants)) {
			continue
		}
		if s.Agent != "codex" {
			// A subagent goes with its parent.
			if s.ParentID == "" && !hidden[s.ID] {
				claude = append(claude, s.ID)
			}
			continue
		}
		if s.NativeID == "" || (op == "archive" && s.Archived) {
			continue
		}
		m.mu.Lock()
		r := m.find(s.ID)
		busy := r != nil && (r.turn || len(r.pending) > 0 || len(r.queue) > 0)
		m.mu.Unlock()
		switch {
		case busy:
			res.Skipped = append(res.Skipped, FolderSkip{s.ID, s.Title, "at work"})
		case m.isExternal(s):
			res.Skipped = append(res.Skipped, FolderSkip{s.ID, s.Title, "open in another client"})
		default:
			codex = append(codex, s)
		}
	}
	res.Codex, res.Claude = len(codex), len(claude)
	if dryRun {
		return res, nil
	}
	// A thread must not stay loaded in our engine while it is archived or
	// deleted.
	var threads []string
	byThread := map[string]Session{}
	for _, s := range codex {
		m.mu.Lock()
		r := m.find(s.ID)
		live := r != nil && r.proc != nil
		m.mu.Unlock()
		if live {
			if err := m.Stop(s.ID, false); err != nil {
				res.Failed = append(res.Failed, FolderSkip{s.ID, s.Title, err.Error()})
				continue
			}
		}
		threads = append(threads, s.NativeID)
		byThread[s.NativeID] = s
	}
	failed := map[string]error{}
	if len(threads) > 0 {
		failed = m.codexThreads(method, threads)
	}
	for _, t := range threads {
		s := byThread[t]
		if err := failed[t]; err != nil {
			res.Failed = append(res.Failed, FolderSkip{s.ID, s.Title, err.Error()})
			continue
		}
		if op == "delete" {
			if err := m.forgetDeleted(s.ID); err != nil {
				res.Failed = append(res.Failed, FolderSkip{s.ID, s.Title, err.Error()})
			}
			continue
		}
		m.mu.Lock()
		if r := m.find(s.ID); r != nil {
			r.session.Archived = true
			m.status(r, r.session.Status)
		}
		m.mu.Unlock()
	}
	res.Codex -= len(res.Failed)
	if len(claude) > 0 {
		m.prefsMu.Lock()
		p := m.Prefs()
		for _, id := range claude {
			p.Hidden = addUniqueMax(p.Hidden, id, hiddenLimit)
			p.PinnedSessions = without(p.PinnedSessions, id)
		}
		err := m.savePrefs(p)
		m.prefsMu.Unlock()
		if err != nil {
			return res, err
		}
	}
	m.catalog.mu.Lock()
	m.catalog.updated = time.Time{}
	m.catalog.mu.Unlock()
	return res, nil
}
