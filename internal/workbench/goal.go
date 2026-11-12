package workbench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A session's /goal, as the page shows it.
//
// Codex keeps the goal on the thread (thread/goal/*) and starts the turns
// that pursue it itself. Claude runs the goal as a session Stop hook that
// keeps the turn going until an evaluator finds the condition met; it
// records each check as a goal_status attachment in the session's JSONL,
// which is where the workbench reads it (stdout carries no goal events).
type Goal struct {
	Objective string `json:"objective"`
	// Codex: active, paused, blocked, usageLimited, budgetLimited, complete.
	// Claude: active, met, failed.
	Status      string  `json:"status"`
	TokensUsed  float64 `json:"tokensUsed,omitempty"`
	TokenBudget float64 `json:"tokenBudget,omitempty"`
	Seconds     float64 `json:"seconds,omitempty"`
	// Claude: goal checks so far, and what the latest one said.
	Checks     int    `json:"checks,omitempty"`
	LastReason string `json:"lastReason,omitempty"`
	Updated    string `json:"updated"`
}

func num(v any) float64 { f, _ := v.(float64); return f }

func codexGoal(v map[string]any) *Goal {
	if str(v["objective"]) == "" {
		return nil
	}
	g := &Goal{Objective: str(v["objective"]), Status: str(v["status"]), TokensUsed: num(v["tokensUsed"]), TokenBudget: num(v["tokenBudget"]), Seconds: num(v["timeUsedSeconds"]), Updated: time.Now().UTC().Format(time.RFC3339)}
	if at := num(v["updatedAt"]); at > 0 {
		g.Updated = time.Unix(int64(at), 0).UTC().Format(time.RFC3339)
	}
	return g
}

// claudeGoalStep applies one goal_status attachment, with the CLI's own rule
// for the active goal: the latest attachment decides, and a met or failed
// one ends it.
func claudeGoalStep(g *Goal, a map[string]any, at string) *Goal {
	condition := str(a["condition"])
	met, _ := a["met"].(bool)
	failed, _ := a["failed"].(bool)
	sentinel, _ := a["sentinel"].(bool)
	switch {
	case sentinel && met: // cleared
		return nil
	case sentinel: // set
		return &Goal{Objective: condition, Status: "active", Updated: at}
	case met || failed:
		next := &Goal{Objective: condition, Status: "met", Checks: int(num(a["iterations"])), TokensUsed: num(a["tokens"]), Seconds: num(a["durationMs"]) / 1000, Updated: at}
		if failed {
			next.Status, next.LastReason = "failed", str(a["reason"])
		}
		if next.Checks == 0 && g != nil && g.Objective == condition {
			next.Checks = g.Checks + 1
		}
		return next
	}
	next := &Goal{Objective: condition, Status: "active", Checks: 1, LastReason: str(a["reason"]), Updated: at}
	if g != nil && g.Objective == condition && g.Status == "active" {
		next.Checks = g.Checks + 1
	}
	return next
}

// claudeGoalScan follows goal_status attachments through a Claude JSONL.
type claudeGoalScan struct {
	mu     sync.Mutex
	path   string
	offset int64
	goal   *Goal
}

// read continues from the last complete line it read, and reports whether
// the goal changed.
func (s *claudeGoalScan) read() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.goal
	err := s.readLocked()
	return s.goal != before, err
}

func (s *claudeGoalScan) readLocked() error {
	f, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() < s.offset {
		s.offset, s.goal = 0, nil
	}
	if _, err := f.Seek(s.offset, io.SeekStart); err != nil {
		return err
	}
	rd := bufio.NewReaderSize(f, 64*1024)
	marker := []byte(`"goal_status"`)
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			// A line still being written is read again next time.
			return nil
		}
		s.offset += int64(len(line))
		if !bytes.Contains(line, marker) {
			continue
		}
		var v struct {
			Type       string         `json:"type"`
			Timestamp  string         `json:"timestamp"`
			Attachment map[string]any `json:"attachment"`
		}
		if json.Unmarshal(line, &v) == nil && v.Type == "attachment" && str(v.Attachment["type"]) == "goal_status" {
			s.goal = claudeGoalStep(s.goal, v.Attachment, v.Timestamp)
		}
	}
}

// claudeTranscript is the JSONL of a Claude session.
func (c *NativeCatalog) claudeTranscript(s Session) string {
	c.mu.Lock()
	path := c.paths[s.ID]
	c.mu.Unlock()
	if path != "" || s.NativeID == "" {
		return path
	}
	files, _ := filepath.Glob(filepath.Join(c.ClaudeHome, "projects", "*", s.NativeID+".jsonl"))
	if len(files) == 1 {
		return files[0]
	}
	return ""
}

// refreshClaudeGoal reads new goal records shortly after the CLI wrote them
// (a /goal command, a goal check, the end of a turn).
func (m *Manager) refreshClaudeGoal(r *runtime) {
	if r.goalTimer != nil {
		r.goalTimer.Stop()
	}
	id := r.session.ID
	r.goalTimer = time.AfterFunc(300*time.Millisecond, func() { m.scanClaudeGoal(id) })
}

func (m *Manager) scanClaudeGoal(id string) {
	m.mu.Lock()
	r := m.find(id)
	if r == nil || r.proc == nil {
		m.mu.Unlock()
		return
	}
	scan, session := r.goalScan, r.session
	m.mu.Unlock()
	first := scan == nil
	if first {
		path := m.catalog.claudeTranscript(session)
		if path == "" {
			return
		}
		scan = &claudeGoalScan{path: path}
	}
	changed, err := scan.read()
	if err != nil {
		return
	}
	scan.mu.Lock()
	goal := scan.goal
	scan.mu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.find(id); r != nil && r.proc != nil {
		if r.goalScan == nil {
			r.goalScan = scan
		}
		// The first read also replaces what was recorded while no engine ran.
		if changed || first {
			m.setGoal(r, goal)
		}
	}
}

// setGoal records the goal for the page (m.mu held), with a notice when it
// reaches an end the agent decided: achieved, met, stalled, limited.
func (m *Manager) setGoal(r *runtime, g *Goal) {
	var prev *Goal
	m.store.readSetting("goal:"+r.session.ID, &prev)
	r.goal = g
	var err error
	if g == nil {
		err = m.store.deleteSetting("goal:" + r.session.ID)
	} else {
		err = m.store.setting("goal:"+r.session.ID, g)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "save goal:", err)
	}
	if text := goalNotice(prev, g); text != "" {
		m.emit(r.session.ID, "notice", map[string]any{"text": text})
	} else {
		m.changes.Broadcast()
	}
}

func goalNotice(prev, g *Goal) string {
	if prev == nil || g == nil || prev.Objective != g.Objective || prev.Status == g.Status {
		return ""
	}
	switch g.Status {
	case "complete":
		return joinStats("Goal achieved", g)
	case "met":
		return joinStats("Goal met", g)
	case "failed":
		return "Goal can't be met: " + g.LastReason
	case "blocked":
		return "Goal stalled · /goal resume to continue"
	case "usageLimited":
		return "Goal hit usage limits · /goal resume to continue"
	case "budgetLimited":
		return joinStats("Goal reached its token budget", g)
	}
	return ""
}

func joinStats(head string, g *Goal) string {
	parts := []string{head}
	if g.Checks > 0 {
		parts = append(parts, plural(g.Checks, "check"))
	}
	if g.Seconds >= 1 {
		parts = append(parts, shortDuration(g.Seconds))
	}
	if g.TokensUsed > 0 {
		parts = append(parts, compactTokens(g.TokensUsed)+" tokens")
	}
	return strings.Join(parts, " · ")
}

// codexGoalPanel is what /goal shows, after the Codex TUI's goal summary.
func codexGoalPanel(g *Goal) string {
	status := map[string]string{"blocked": "stalled", "usageLimited": "usage limited", "budgetLimited": "limited by budget"}[g.Status]
	if status == "" {
		status = g.Status
	}
	lines := []string{"Goal: " + g.Objective, "Status: " + status}
	if g.Seconds >= 1 {
		lines = append(lines, "Time used: "+shortDuration(g.Seconds))
	}
	tokens := compactTokens(g.TokensUsed)
	if g.TokenBudget > 0 {
		tokens += " of " + compactTokens(g.TokenBudget)
	}
	lines = append(lines, "Tokens used: "+tokens)
	toggle := "/goal pause"
	if g.Status != "active" {
		toggle = "/goal resume"
	}
	lines = append(lines, "Commands: /goal edit, "+toggle+", /goal clear")
	return strings.Join(lines, "\n")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func shortDuration(seconds float64) string {
	s := int(math.Round(seconds))
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		if s%60 == 0 {
			return fmt.Sprintf("%dm", s/60)
		}
		return fmt.Sprintf("%dm %ds", s/60, s%60)
	}
	if s%3600/60 == 0 {
		return fmt.Sprintf("%dh", s/3600)
	}
	return fmt.Sprintf("%dh %dm", s/3600, s%3600/60)
}

func compactTokens(n float64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", int(math.Round(n)))
	case n < 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", n/1000), ".0") + "k"
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", n/1_000_000), ".0") + "M"
}
