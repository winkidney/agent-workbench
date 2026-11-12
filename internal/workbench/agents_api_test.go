package workbench

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCLIs puts a claude and a codex on PATH that answer --version and say
// whether they are signed in.
func fakeCLIs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script(t, filepath.Join(dir, "claude"), `case "$1 $2" in
  "--version "*) echo "9.9.9 (Claude Code)" ;;
  "auth status") echo '{"loggedIn":true,"authMethod":"claude.ai","email":"me@example.com","subscriptionType":"max"}' ;;
  "--help "*) echo "  --prompt-suggestions [value]" ;;
esac
`)
	script(t, filepath.Join(dir, "codex"), `case "$1 $2" in
  "--version "*) echo "codex-cli 1.2.3" ;;
  "login status") echo "Not logged in"; exit 1 ;;
esac
`)
	t.Setenv("PATH", dir)
	cliVersions.Lock()
	cliVersions.byAgent = nil
	cliVersions.Unlock()
	return dir
}

func setupOf(t *testing.T, body []byte) map[string]agentView {
	t.Helper()
	var v map[string]agentView
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("setup: %v %s", err, body)
	}
	return v
}

func TestAgentsSetupIsCheckedBeforeItIsSaved(t *testing.T) {
	posixOnly(t)
	dir := fakeCLIs(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", t.TempDir())
	s, m := authServer(t)
	get := func() map[string]agentView {
		w := request(t, s, s.Host, "GET", "/api/agents/setup", "", nil)
		if w.Code != 200 {
			t.Fatalf("GET %d %s", w.Code, w.Body.String())
		}
		return setupOf(t, w.Body.Bytes())
	}
	v := get()
	c, x := v["claude"], v["codex"]
	if c.Program.Source != "path" || c.Program.Path != filepath.Join(dir, "claude") || c.Program.Version != "9.9.9 (Claude Code)" {
		t.Fatalf("claude program %+v", c.Program)
	}
	if x.Home.Source != "environment" || !x.Home.Exists || x.Home.SignIn.SignedIn || !x.Home.SignIn.Checked {
		t.Fatalf("codex home %+v", x.Home)
	}
	if c.Home.Source != "default" || c.Home.LoginCommand != "claude auth login" {
		t.Fatalf("claude home %+v", c.Home)
	}

	put := func(body string) (int, string) {
		w := request(t, s, s.Host, "PUT", "/api/agents/setup", body, nil)
		return w.Code, w.Body.String()
	}
	notExec := filepath.Join(dir, "plain")
	os.WriteFile(notExec, []byte("x"), 0o644)
	impostor := script(t, filepath.Join(dir, "impostor"), "echo something else\n")
	for body, want := range map[string]string{
		`{"claude":{"path":"bin/claude"}}`:                           "absolute path",
		`{"claude":{"path":"` + filepath.Join(dir, "none") + `"}}`:   "no such file",
		`{"claude":{"path":"` + notExec + `"}}`:                      "not executable",
		`{"claude":{"path":"` + impostor + `"}}`:                     "does not look like Claude Code",
		`{"codex":{"home":"` + filepath.Join(dir, "missing") + `"}}`: "create it first",
	} {
		if code, text := put(body); code != 400 || !strings.Contains(text, want) {
			t.Fatalf("%s: %d %s", body, code, text)
		}
	}

	// A valid choice is saved and used.
	chosen := script(t, filepath.Join(dir, "claude-pinned"), `case "$1" in --version) echo "9.9.8 (Claude Code)" ;; esac
`)
	home := t.TempDir()
	if code, text := put(`{"claude":{"path":"` + chosen + `/","home":"` + home + `/"}}`); code != 200 {
		t.Fatalf("PUT %d %s", code, text)
	}
	v = get()
	c = v["claude"]
	if c.Program.Source != "setting" || c.Program.Path != chosen || c.Home.Source != "setting" || c.Home.Path != home {
		t.Fatalf("saved %+v %+v", c.Program, c.Home)
	}
	if !strings.HasPrefix(c.Home.LoginCommand, "CLAUDE_CONFIG_DIR="+home+" "+chosen) {
		t.Fatalf("login command %q", c.Home.LoginCommand)
	}
	if m.catalog.ClaudeHome != home {
		t.Fatalf("catalog home %s", m.catalog.ClaudeHome)
	}
	// Check without saving.
	w := request(t, s, s.Host, "POST", "/api/agents/codex/check", `{"path":"`+impostor+`"}`, nil)
	var check agentView
	json.Unmarshal(w.Body.Bytes(), &check)
	if w.Code != 200 || !strings.Contains(check.Program.Error, "does not look like Codex") || m.Agents().Codex.Path != "" {
		t.Fatalf("check %d %+v", w.Code, check)
	}

	// The home of an agent at work does not change.
	m.mu.Lock()
	m.sessions["busy"] = &runtime{session: Session{ID: "busy", Agent: "codex", Status: "running"}, proc: &fakeProcess{}}
	m.mu.Unlock()
	if code, text := put(`{"codex":{"home":"` + t.TempDir() + `"}}`); code != http.StatusConflict || !strings.Contains(text, "at work") {
		t.Fatalf("busy: %d %s", code, text)
	}
	m.mu.Lock()
	delete(m.sessions, "busy")
	m.mu.Unlock()

	// A signed-in device may change them, as any setting; another origin
	// may not.
	setAccess(t, s, `{"remoteAccess":"pairing"}`)
	code := secrets(t, s)["pairingCode"]
	body, _ := json.Marshal(map[string]string{"code": code})
	cookie := fromLAN(t, s, "192.168.1.20:5000", "POST", "/api/login", string(body), nil).Result().Cookies()[0]
	if w := lan(t, s, "PUT", "/api/agents/setup", `{}`, cookie); w.Code != 200 {
		t.Fatalf("signed-in device: %d %s", w.Code, w.Body.String())
	}
	if w := lan(t, s, "PUT", "/api/agents/setup", `{}`, nil); w.Code != 401 {
		t.Fatalf("signed out: %d", w.Code)
	}
}

func TestSigningInFromThePage(t *testing.T) {
	posixOnly(t)
	dir := fakeCLIs(t)
	seen := filepath.Join(dir, "seen")
	// Claude: a sign-in link, then the code its page shows.
	script(t, filepath.Join(dir, "claude"), `case "$1 $2" in
  "--version "*) echo "9.9.9 (Claude Code)"; exit 0 ;;
  "auth login") ;;
  *) exit 0 ;;
esac
echo "browser=$BROWSER display=$DISPLAY" > `+seen+`
echo "Opening browser to sign in…"
echo "If the browser didn't open, visit: https://claude.com/cai/oauth/authorize?code=true&state=s1"
printf "Paste code here if prompted > "
read code
echo "code=$code" >> `+seen+`
if [ "$code" = "good#s1" ]; then echo "Login successful."; exit 0; fi
echo "Login failed: That code wasn't accepted." >&2
exit 1
`)
	// Codex: a device link and a one-time code, in colour, until done.
	script(t, filepath.Join(dir, "codex"), `case "$1 $2" in
  "--version "*) echo "codex-cli 1.2.3"; exit 0 ;;
  "login --device-auth") ;;
  *) exit 0 ;;
esac
printf '1. Open this link\n   \033[94mhttps://auth.openai.com/codex/device\033[0m\n2. Enter this one-time code \033[90m(expires in 15 minutes)\033[0m\n   \033[94mABCD-12345\033[0m\n'
read wait
`)
	t.Setenv("DISPLAY", ":0")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", t.TempDir())
	s, m := authServer(t)
	wait := func(agent, state string) loginState {
		t.Helper()
		for i := 0; i < 300; i++ {
			if st := m.loginState(agent); st != nil && st.State == state {
				return *st
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("%s never %s: %+v", agent, state, m.loginState(agent))
		return loginState{}
	}
	post := func(path, body string) (int, string) {
		w := request(t, s, s.Host, "POST", path, body, nil)
		return w.Code, w.Body.String()
	}

	if code, text := post("/api/agents/claude/login", `{"method":"claudeai"}`); code != 200 {
		t.Fatalf("start %d %s", code, text)
	}
	st := wait("claude", "waiting")
	if !st.NeedsCode || !strings.HasPrefix(st.URL, "https://claude.com/cai/oauth/authorize") {
		t.Fatalf("claude waiting %+v", st)
	}
	// A wrong code: the CLI's own words come back.
	post("/api/agents/claude/login/code", `{"code":"bad"}`)
	st = wait("claude", "failed")
	if !strings.Contains(st.Message, "wasn't accepted") {
		t.Fatalf("failed %+v", st)
	}
	// Again, with the right one.
	post("/api/agents/claude/login", `{}`)
	wait("claude", "waiting")
	code, text := post("/api/agents/claude/login/code", `{"code":" good#s1 "}`)
	if code != 200 || strings.Contains(text, "good#s1") {
		t.Fatalf("code: %d %s", code, text)
	}
	wait("claude", "done")
	b, _ := os.ReadFile(seen)
	if !strings.Contains(string(b), "code=good#s1") || !strings.Contains(string(b), "display=\n") || strings.Contains(string(b), "browser= ") {
		t.Fatalf("the CLI saw %q", b)
	}
	// Nothing to paste into once done.
	if code, _ := post("/api/agents/claude/login/code", `{"code":"x"}`); code != 400 {
		t.Fatalf("late code %d", code)
	}

	// Codex: link and code, and cancelling ends the CLI.
	post("/api/agents/codex/login", `{}`)
	st = wait("codex", "waiting")
	if st.URL != "https://auth.openai.com/codex/device" || st.Code != "ABCD-12345" || st.NeedsCode {
		t.Fatalf("codex waiting %+v", st)
	}
	m.loginsMu.Lock()
	flow := m.logins["codex"]
	m.loginsMu.Unlock()
	if w := request(t, s, s.Host, "DELETE", "/api/agents/codex/login", "", nil); w.Code != 200 {
		t.Fatalf("cancel %d", w.Code)
	}
	if st := m.loginState("codex"); st != nil {
		t.Fatalf("still listed %+v", st)
	}
	if flow.snapshot().State != "cancelled" {
		t.Fatalf("cancelled flow %+v", flow.snapshot())
	}
	select {
	case <-flow.done:
	case <-time.After(5 * time.Second):
		t.Fatal("codex login still running")
	}

	// One left waiting expires.
	old := loginTimeout
	loginTimeout = 200 * time.Millisecond
	defer func() { loginTimeout = old }()
	post("/api/agents/codex/login", `{}`)
	if st := wait("codex", "expired"); !strings.Contains(st.Message, "10 minutes") {
		t.Fatalf("expired %+v", st)
	}
}
