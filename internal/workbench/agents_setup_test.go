package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// script writes an executable shell script.
func script(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAgentProgramsAndHomesComeFromSettingsThenEnvironment(t *testing.T) {
	posixOnly(t)
	dir := t.TempDir()
	onPath := script(t, filepath.Join(dir, "claude"), "exit 0\n")
	script(t, filepath.Join(dir, "codex"), "exit 0\n")
	t.Setenv("PATH", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "/env/codex")
	m, _ := fixture(t)

	// Nothing chosen: PATH, and no home variable added for the CLI.
	run, err := m.cli("claude")
	if err != nil || run.Binary != onPath || run.Env != nil {
		t.Fatalf("automatic: %+v %v", run, err)
	}
	if home, from := m.agentHome("claude"); from != "default" || !strings.HasSuffix(home, ".claude") {
		t.Fatalf("claude home %s %s", home, from)
	}
	if home, from := m.agentHome("codex"); home != "/env/codex" || from != "environment" {
		t.Fatalf("codex home %s %s", home, from)
	}

	// Chosen in Settings: that program, and the home named for it alone.
	chosen := script(t, filepath.Join(dir, "claude-2"), "exit 0\n")
	m.agents.Claude = AgentSetup{Path: chosen, Home: "/x/claude"}
	m.agents.Codex = AgentSetup{Home: "/x/codex"}
	m.applyHomes()
	run, _ = m.cli("claude")
	if run.Binary != chosen || !contains(run.Env, "CLAUDE_CONFIG_DIR=/x/claude") || anyPrefix(run.Env, "CODEX_HOME=/x") {
		t.Fatalf("chosen: %+v", run)
	}
	codex, err := m.cli("codex")
	if err != nil || !contains(codex.Env, "CODEX_HOME=/x/codex") || anyPrefix(codex.Env, "CODEX_HOME=/env") {
		t.Fatalf("codex env replaces the inherited home: %v", codex.Env)
	}
	if m.catalog.ClaudeHome != "/x/claude" || m.catalog.CodexHome != "/x/codex" {
		t.Fatalf("catalog homes %s %s", m.catalog.ClaudeHome, m.catalog.CodexHome)
	}
	// Results that depend on them are kept apart.
	if m.claudeInitKey("/w") == "claude-init:/w" || m.cacheKey("codex") == "" {
		t.Fatal("cache keys ignore the chosen program and home")
	}
	m.agents = AgentsSetup{}
	if m.claudeInitKey("/w") != "claude-init:/w" {
		t.Fatal("default cache key changed")
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func anyPrefix(list []string, prefix string) bool {
	for _, x := range list {
		if strings.HasPrefix(x, prefix) {
			return true
		}
	}
	return false
}

func TestEnginesRunTheChosenProgramWithItsHome(t *testing.T) {
	posixOnly(t)
	dir := t.TempDir()
	seen := filepath.Join(dir, "seen")
	fake := script(t, filepath.Join(dir, "my-claude"), `case "$1" in --help) exit 0;; esac
echo "$0 home=$CLAUDE_CONFIG_DIR" > `+seen+"\n")
	s := Session{ID: "x", Agent: "claude", Cwd: dir, run: agentRun{Binary: fake, Env: withEnv(os.Environ(), "CLAUDE_CONFIG_DIR", "/chosen/home")}}
	exited := make(chan error, 1)
	if _, err := DirectLaunch(s, func(map[string]any) {}, func(err error) { exited <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("fake engine did not exit")
	}
	b, _ := os.ReadFile(seen)
	if got := strings.TrimSpace(string(b)); got != fake+" home=/chosen/home" {
		t.Fatalf("engine ran as %q", got)
	}
}

func TestDoctorReadsTheChosenProgramWithoutWriting(t *testing.T) {
	posixOnly(t)
	dir := t.TempDir()
	chosen := script(t, filepath.Join(dir, "codex-9"), "exit 0\n")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	m, _ := fixture(t)
	if err := m.store.setting("agents", AgentsSetup{Codex: AgentSetup{Path: chosen, Home: "/h/codex"}}); err != nil {
		t.Fatal(err)
	}
	bin, home, from, err := AgentCLI(m.store.dir, "codex")
	if err != nil || bin != chosen || home != "/h/codex" || from != "setting" {
		t.Fatalf("doctor: %s %s %s %v", bin, home, from, err)
	}
	if _, _, _, err := AgentCLI(m.store.dir, "claude"); err == nil {
		t.Fatal("claude found on an empty PATH")
	}
}

// A Codex app-server that answers initialize and lists one turn.
const fakeAppServer = `while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  [ -z "$id" ] && continue
  case "$line" in
    *thread/turns/list*) printf '{"id":"%s","result":{"data":[{"id":"t1","status":"completed","items":[{"type":"agentMessage","id":"i1","text":"hi"}]}]}}\n' "$id" ;;
    *) printf '{"id":"%s","result":{}}\n' "$id" ;;
  esac
done
`

func TestFollowingACompressedRecordAsksAppServer(t *testing.T) {
	posixOnly(t)
	m, _ := fixture(t)
	home := t.TempDir()
	record := filepath.Join(home, "sessions", "2026", "10", "01", "rollout-z.jsonl")
	writeZstd(t, record+".zst", codexUsageRecord())
	codexIndex(t, home, []any{"z", record, 1, 30, "cli", "/work", "Resumed elsewhere", "", 0, "hi", 1, "paginated"})
	m.catalog.CodexHome = home
	m.catalog.ClaudeHome = t.TempDir()
	server := script(t, filepath.Join(t.TempDir(), "codex"), fakeAppServer)
	m.catalog.startCodex = func(name string) (*codexClient, error) {
		return startCodexClientWith(agentRun{Binary: server}, name)
	}
	// Another client holds the thread while Codex restores its record.
	os.MkdirAll(filepath.Join(home, "thread-writer-locks"), 0o700)
	lock, _ := os.Create(filepath.Join(home, "thread-writer-locks", "z.lock"))
	defer lock.Close()
	release := holdLock(lock)
	defer release()

	tail := m.followExternal("codex:z")
	defer tail.close()
	first := tail.poll()
	if first == nil || !first.External || first.Running {
		t.Fatalf("first %+v", first)
	}
	tail.polled = time.Time{}
	u := tail.poll()
	if u == nil || len(u.Events) == 0 {
		t.Fatalf("no records while compressed: %+v", u)
	}
	// Asked again only after two seconds.
	tail.polled = time.Time{}
	if again := tail.poll(); again != nil {
		t.Fatalf("asked again at once: %+v", again)
	}
}
