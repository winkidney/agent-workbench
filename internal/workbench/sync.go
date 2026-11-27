package workbench

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Backing the repository of assistants up to GitHub (ADR 0014). With a
// remote named origin, commits (the workbench's, and the ones assistants make
// themselves) are pushed shortly after; the page shows how that went and can
// make a private GitHub repository for it.

// How often the scheduler looks for commits to push.
const syncEvery = 2 * time.Minute

// SyncState is what the page shows of the backup.
type SyncState struct {
	Remote string `json:"remote,omitempty"`
	// Commits not on the remote yet.
	Ahead    int    `json:"ahead"`
	Pushed   string `json:"pushed,omitempty"`
	Error    string `json:"error,omitempty"`
	checked  time.Time
	inFlight bool
}

type syncer struct {
	mu sync.Mutex
	st SyncState
}

func (m *Manager) syncState() SyncState {
	m.as.sync.mu.Lock()
	defer m.as.sync.mu.Unlock()
	st := m.as.sync.st
	root := m.assistantsRoot()
	if root != "" {
		st.Remote = remoteURL(root)
		if st.Remote != "" {
			st.Ahead = ahead(root)
		}
	}
	return st
}

// remoteURL is origin's address without credentials, or "".
func remoteURL(root string) string {
	out, err := gitRun(root, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return regexp.MustCompile(`://[^/@]+@`).ReplaceAllString(strings.TrimSpace(out), "://")
}

// ahead counts the commits origin does not have; -1 when the branch was
// never pushed.
func ahead(root string) int {
	out, err := gitRun(root, "rev-list", "--count", "@{upstream}..HEAD")
	if err != nil {
		return -1
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n
}

// syncAssistants pushes what origin lacks, at most every syncEvery unless
// now is forced. It runs on the scheduler's goroutine and never blocks it
// for long: the push itself runs on its own.
func (m *Manager) syncAssistants(force bool) {
	root := m.assistantsRoot()
	if root == "" {
		return
	}
	s := &m.as.sync
	s.mu.Lock()
	if s.st.inFlight || (!force && time.Since(s.st.checked) < syncEvery) {
		s.mu.Unlock()
		return
	}
	s.st.checked = time.Now()
	s.mu.Unlock()
	if remoteURL(root) == "" || (!force && ahead(root) == 0) {
		return
	}
	s.mu.Lock()
	s.st.inFlight = true
	s.mu.Unlock()
	go func() {
		err := push(root)
		s.mu.Lock()
		s.st.inFlight = false
		if err != nil {
			s.st.Error = err.Error()
		} else {
			s.st.Error, s.st.Pushed = "", nowStamp()
		}
		s.mu.Unlock()
		m.emit("", "assistants", map[string]any{"sync": true})
	}()
}

func push(root string) error {
	args := []string{"push", "-q", "origin", "HEAD"}
	if ahead(root) < 0 {
		args = []string{"push", "-q", "-u", "origin", "HEAD"}
	}
	_, err := gitRun(root, args...)
	return err
}

// BackUpToGitHub makes a private GitHub repository for the assistants with
// gh (signed in as the user), names it origin and pushes.
func (m *Manager) BackUpToGitHub(name string) (SyncState, error) {
	root := m.assistantsRoot()
	if root == "" {
		return SyncState{}, errors.New("set assistants up first")
	}
	if remoteURL(root) != "" {
		return m.syncState(), errors.New("the repository already has a remote (origin)")
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return SyncState{}, errors.New("install the GitHub CLI (gh) and run `gh auth login` first")
	}
	if name = strings.TrimSpace(name); name == "" {
		name = filepath.Base(root)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`).MatchString(name) {
		return SyncState{}, errors.New("repository name: letters, digits, - . _")
	}
	// What is not committed yet goes too.
	m.commitAll(root, "chore: back up to GitHub")
	cmd := exec.Command(gh, "repo", "create", name, "--private", "--source", root, "--remote", "origin", "--push", "--description", "Assistants for Agent Workbench")
	cmd.Dir = root
	cmd.Env = append(routineEnv(), "GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return SyncState{}, fmt.Errorf("gh repo create: %s", strings.TrimSpace(string(out)))
	}
	m.as.sync.mu.Lock()
	m.as.sync.st.Pushed, m.as.sync.st.Error = nowStamp(), ""
	m.as.sync.mu.Unlock()
	return m.syncState(), nil
}
