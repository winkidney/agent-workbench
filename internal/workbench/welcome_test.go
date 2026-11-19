package workbench

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLanguageIsSetAndNamedInTheRules(t *testing.T) {
	m, _ := fixture(t)
	root := filepath.Join(t.TempDir(), "assistants")
	if _, err := m.setUp(root, "claude", "", "xx"); err == nil {
		t.Fatal("an unknown language was accepted")
	}
	created, err := m.setUp(root, "claude", "", "zh-Hant")
	if err != nil || !created {
		t.Fatalf("set up: %v %v", created, err)
	}
	root = m.assistantsRoot()
	rules := func() string { b, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")); return string(b) }
	if m.language() != "zh-Hant" || !strings.Contains(rules(), languageLine("zh-Hant")) || strings.Contains(rules(), "{{") {
		t.Fatalf("language %q, rules:\n%s", m.language(), rules())
	}
	if err := m.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	if m.language() != "en" || !strings.Contains(rules(), languageLine("en")) || strings.Contains(rules(), "Traditional") {
		t.Fatalf("after switching: %q\n%s", m.language(), rules())
	}
	if err := m.SetLanguage("fr"); err == nil {
		t.Fatal("fr was accepted")
	}
	// Rules from before there was a language line get one.
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Assistants\n\n## Rules for every assistant\n\n- **Read before you start.**\n"), 0o644)
	if err := m.SetLanguage("zh-Hans"); err != nil || !strings.Contains(rules(), "## Rules for every assistant\n\n"+languageLine("zh-Hans")+"\n- **Read") {
		t.Fatalf("added line: %v\n%s", err, rules())
	}
	// Setting up again does not make another main.
	if created, _ := m.setUp(root, "claude", "", ""); created {
		t.Fatal("made main twice")
	}
}

func TestWelcomeAndSetUpMessages(t *testing.T) {
	text := welcomeText("zh-Hans", []Assistant{{Name: "Code Reviewer", Dir: "code-reviewer"}})
	for _, want := range []string{"[Workbench: Welcome]\n\n", "Simplified Chinese", "Code Reviewer (code-reviewer/)", "Quick starts", "clock button", "New assistants"} {
		if !strings.Contains(text, want) {
			t.Errorf("welcome lacks %q", want)
		}
	}
	if strings.Contains(text, "Built-in") {
		t.Error("the welcome offers built-in assistants")
	}
	set := setUpText("Report", "en")
	if !strings.HasPrefix(set, "[Workbench: Set up]\n\n") || !strings.Contains(set, "create-assistant skill") || !strings.Contains(set, "Never ask for a password") {
		t.Fatalf("set up: %s", set)
	}
	if routineLabelOf(text) != "Welcome" || routineLabelOf("[Routine: Daily]\n\nx") != "Daily" || routineLabelOf("hello") != "" {
		t.Fatal("labels")
	}
}

func TestWelcomeGoesToMossTopic(t *testing.T) {
	m, p := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root, Language: "en"})
	writeToml(t, filepath.Join(root, "main", "assistant.toml"), "name = \"Moss\"\nagent = \"codex\"\nsessions = \"continuous\"\nmemory = \"wiki\"")
	topic, _ := m.Create("codex", filepath.Join(root, "main"), "t")
	_ = m.Start(topic.ID)
	gen := m.sessions[topic.ID].generation
	m.receive(topic.ID, gen, map[string]any{"id": "init", "result": map[string]any{}})
	m.receive(topic.ID, gen, map[string]any{"id": "thread", "result": map[string]any{"thread": map[string]any{"id": "w"}}})
	m.saveTopic("main", assistantState{Current: "codex:w"})
	m.as.lookup = func(id string) (Session, bool) {
		return Session{ID: id, Status: "idle", Updated: time.Now().UTC().Format(time.RFC3339)}, id == "codex:w"
	}
	if _, err := m.Welcome(); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var text string
	for _, in := range p.in {
		if in["method"] == "turn/start" {
			text = str(obj(obj(in["params"])["input"].([]any)[0])["text"])
		}
	}
	if !strings.HasPrefix(text, "[Workbench: Welcome]") || !strings.Contains(text, "in English") {
		t.Fatalf("sent %q", text)
	}
}

func TestCommitsArePushedToTheRemote(t *testing.T) {
	m, root := assistantsWithGit(t)
	if st := m.syncState(); st.Remote != "" {
		t.Fatalf("a remote before there is one: %+v", st)
	}
	bare := filepath.Join(t.TempDir(), "backup.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := gitRun(root, "remote", "add", "origin", bare); err != nil {
		t.Fatal(err)
	}
	if st := m.syncState(); st.Remote != bare || st.Ahead != -1 {
		t.Fatalf("before the first push: %+v", st)
	}
	m.syncAssistants(false)
	waitUntil(t, "the first push", func() bool { st := m.syncState(); return st.Ahead == 0 && st.Pushed != "" })
	// A new commit is pushed at the next look; not before syncEvery unless forced.
	writeToml(t, filepath.Join(root, "main", "state", "loops.md"), "- call Ana\n")
	commitAll(root, "loops")
	if st := m.syncState(); st.Ahead != 1 {
		t.Fatalf("ahead: %+v", st)
	}
	m.syncAssistants(false)
	time.Sleep(50 * time.Millisecond)
	if st := m.syncState(); st.Ahead != 1 {
		t.Fatalf("pushed before its time: %+v", st)
	}
	m.syncAssistants(true)
	waitUntil(t, "the second push", func() bool { return m.syncState().Ahead == 0 })
	if out, _ := exec.Command("git", "--git-dir", bare, "log", "--format=%s", "-1").Output(); string(out) != "loops\n" {
		t.Fatalf("remote has %q", out)
	}
	// A remote that fails says so.
	gitRun(root, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	commitAll(root, "more")
	m.syncAssistants(true)
	waitUntil(t, "the error", func() bool { return m.syncState().Error != "" })
	if _, err := m.BackUpToGitHub(""); err == nil || !strings.Contains(err.Error(), "already has a remote") {
		t.Fatalf("back up with a remote: %v", err)
	}
}

func TestCredentialsAreRefusedAtCommit(t *testing.T) {
	m, root := assistantsWithGit(t)
	_ = m
	hook, err := os.ReadFile(filepath.Join(root, ".git", "hooks", "pre-commit"))
	if err != nil || !strings.Contains(string(hook), hookMarker) {
		t.Fatalf("hook: %v", err)
	}
	ignore, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	for _, want := range []string{".env", "*.pem", "*token*.json", "secrets/"} {
		if !strings.Contains(string(ignore), "\n"+want+"\n") {
			t.Errorf(".gitignore lacks %s", want)
		}
	}
	commit := func(env ...string) error {
		cmd := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "x")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil && !strings.Contains(string(out), "Commit refused") {
			t.Fatalf("unexpected failure: %s", out)
		}
		if strings.Contains(string(out), "ghp_") {
			t.Fatal("the hook printed the token")
		}
		return err
	}
	token := "ghp_" + strings.Repeat("a1B2", 9)
	writeToml(t, filepath.Join(root, "main", "config.toml"), "token = \""+token+"\"\n")
	gitRun(root, "add", "-A")
	if commit() == nil {
		t.Fatal("a GitHub token was committed")
	}
	// The workbench's own commits are checked too.
	if c, _, err := gitCommit(root, "chore: x"); err == nil || c != "" {
		t.Fatalf("gitCommit with a token: %q %v", c, err)
	}
	// Only on the user's explicit request.
	if err := commit("ASSISTANTS_ALLOW_SECRETS=1"); err != nil {
		t.Fatal("refused with the user's permission")
	}
	// A credential file by its name, and a URL with a password.
	writeToml(t, filepath.Join(root, "main", "google-token.json"), "{}\n")
	gitRun(root, "add", "-f", "main/google-token.json")
	if commit() == nil {
		t.Fatal("a token file was committed")
	}
	gitRun(root, "reset", "-q")
	os.Remove(filepath.Join(root, "main", "google-token.json"))
	writeToml(t, filepath.Join(root, "main", "notes.md"), "see https://me:hunter22@example.com/cal\n")
	gitRun(root, "add", "-A")
	if commit() == nil {
		t.Fatal("a URL with a password was committed")
	}
	// Ordinary text passes.
	writeToml(t, filepath.Join(root, "main", "notes.md"), "the password is in ~/.config/assistant/secrets/mail-password\n")
	gitRun(root, "add", "-A")
	if err := commit(); err != nil {
		t.Fatal("refused ordinary text")
	}
	// A hook of the user's own is kept.
	os.WriteFile(filepath.Join(root, ".git", "hooks", "pre-commit"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	if wrote, _ := installHook(root); wrote {
		t.Fatal("replaced the user's hook")
	}
}
