package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"sync"
	"time"
)

// About: what the settings page shows of this workbench and the agents it
// runs, as `agent-workbench doctor` prints it.

// Autostart describes whether the workbench starts at login; the command,
// which installs it, sets it.
var Autostart func() string

// claudeFeatures are the optional Claude flags the workbench uses when the
// CLI has them (agentCommand).
var claudeFeatures = []string{"--include-hook-events", "--prompt-suggestions"}

type aboutAgent struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
	// Claude: the optional flags it takes, and those it lacks.
	Features []string `json:"features,omitempty"`
	Missing  []string `json:"missing,omitempty"`
	// Where the program comes from ("setting" or "path"), the home it uses
	// and where that comes from ("setting", "environment" or "default").
	Source     string `json:"source,omitempty"`
	Home       string `json:"home,omitempty"`
	HomeSource string `json:"homeSource,omitempty"`
}

// cliVersions keeps one --version answer per agent, for its binary's path,
// size and modification time: asked again only after an update.
var cliVersions struct {
	sync.Mutex
	byAgent map[string][3]string // key, version, error
}

func cliVersion(name, path string) (version, failure string) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err.Error()
	}
	key := fmt.Sprint(path, info.Size(), info.ModTime().UnixNano())
	cliVersions.Lock()
	cached, ok := cliVersions.byAgent[name]
	cliVersions.Unlock()
	if ok && cached[0] == key {
		return cached[1], cached[2]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	version = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if err != nil {
		failure = err.Error()
	}
	cliVersions.Lock()
	if cliVersions.byAgent == nil {
		cliVersions.byAgent = map[string][3]string{}
	}
	cliVersions.byAgent[name] = [3]string{key, version, failure}
	cliVersions.Unlock()
	return version, failure
}

func (m *Manager) aboutAgent(name string) aboutAgent {
	a := aboutAgent{Name: name, Source: "path"}
	a.Home, a.HomeSource = m.agentHome(name)
	if m.Agents().of(name).Path != "" {
		a.Source = "setting"
	}
	run, err := m.cli(name)
	if err != nil {
		a.Error = "not found on PATH"
		return a
	}
	path := run.Binary
	a.Path = path
	a.Version, a.Error = cliVersion(name, path)
	if name == "claude" {
		took := map[string]bool{}
		for _, f := range claudeTakes(path, claudeFeatures...) {
			took[f] = true
			a.Features = append(a.Features, f)
		}
		for _, f := range claudeFeatures {
			if !took[f] {
				a.Missing = append(a.Missing, f)
			}
		}
	}
	return a
}

// about is the settings page's About tab.
func (s *Server) about() map[string]any {
	agents := make([]aboutAgent, 2)
	var wg sync.WaitGroup
	for i, name := range []string{"claude", "codex"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			agents[i] = s.Manager.aboutAgent(name)
		}()
	}
	wg.Wait()
	counts := map[string]int{}
	if list, err := s.Manager.catalog.List(); err == nil {
		for _, ss := range list {
			if ss.ParentID == "" {
				counts[ss.Agent]++
			} else {
				counts["subagents"]++
			}
		}
	}
	workbench := map[string]any{
		"version":  Version,
		"build":    s.build,
		"go":       goruntime.Version(),
		"os":       goruntime.GOOS,
		"arch":     goruntime.GOARCH,
		"dataDir":  s.Manager.store.dir,
		"instance": instance,
		"listen":   s.listen,
		"engines":  s.mode,
		"repo":     "https://github.com/winkidney/agent-workbench",
		"license":  "MIT",
		"pid":      os.Getpid(),
		"platform": goruntime.GOOS + "/" + goruntime.GOARCH,
	}
	if !s.started.IsZero() {
		workbench["started"] = s.started.UTC().Format(time.RFC3339)
	}
	if Autostart != nil {
		workbench["autostart"] = Autostart()
	}
	return map[string]any{
		"workbench": workbench,
		"agents":    agents,
		"data": map[string]any{
			"storageBytes":  s.Manager.store.Size(),
			"retentionDays": int(eventRetention / (24 * time.Hour)),
			"sessions":      counts,
		},
	}
}

// signedInLocked counts the pairing sign-ins still valid (s.mu held).
func (s *Server) signedInLocked() int {
	n, now := 0, time.Now()
	for _, expires := range s.auth {
		if now.Before(expires) {
			n++
		}
	}
	return n
}

// signOutOthersLocked ends every pairing sign-in except the one r makes
// (s.mu held); file previews granted to them end with them (files.go).
func (s *Server) signOutOthersLocked(r *http.Request) int {
	keep := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		if expires, ok := s.auth[c.Value]; ok && time.Now().Before(expires) {
			keep = c.Value
		}
	}
	ended := 0
	for key := range s.auth {
		if key != keep {
			delete(s.auth, key)
			ended++
		}
	}
	return ended
}

// releasesURL is where "Check for updates" looks; a variable for tests.
var releasesURL = "https://api.github.com/repos/winkidney/agent-workbench/releases/latest"

// latestRelease remembers the last answer for ten minutes: the check runs
// only when asked, and GitHub limits unauthenticated requests.
var latestRelease struct {
	sync.Mutex
	at        time.Time
	tag, link string
}

// LatestRelease is the newest published release (its tag and page). The
// request carries nothing about this machine or its user.
func LatestRelease() (tag, link string, err error) {
	latestRelease.Lock()
	defer latestRelease.Unlock()
	if latestRelease.tag != "" && time.Since(latestRelease.at) < 10*time.Minute {
		return latestRelease.tag, latestRelease.link, nil
	}
	req, _ := http.NewRequest("GET", releasesURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "agent-workbench")
	client := http.Client{Timeout: 8 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", "", fmt.Errorf("GitHub answered %s", res.Status)
	}
	var v struct {
		Tag  string `json:"tag_name"`
		Link string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&v); err != nil || v.Tag == "" {
		return "", "", errors.New("unreadable release information")
	}
	latestRelease.at, latestRelease.tag, latestRelease.link = time.Now(), v.Tag, v.Link
	return v.Tag, v.Link, nil
}
