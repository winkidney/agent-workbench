package workbench

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeToml(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAssistantFileIsChecked(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"no name":       `agent = "claude"`,
		"agent":         "name = \"x\"\nagent = \"gpt\"",
		"sessions":      "name = \"x\"\nagent = \"codex\"\nsessions = \"forever\"",
		"unknown key":   "name = \"x\"\nagent = \"codex\"\nagnet = \"codex\"",
		"two schedules": "name = \"x\"\nagent = \"codex\"\n[[routines]]\nname = \"r\"\ndaily = \"09:00\"\nevery = \"1h\"\nmessage = \"m\"",
		"bad day":       "name = \"x\"\nagent = \"codex\"\n[[routines]]\nname = \"r\"\ndaily = \"09:00\"\ndays = [\"someday\"]\nmessage = \"m\"",
		"nothing to do": "name = \"x\"\nagent = \"codex\"\n[[routines]]\nname = \"r\"\nevery = \"1h\"",
		"same name":     "name = \"x\"\nagent = \"codex\"\n[[routines]]\nname = \"r\"\nevery = \"1h\"\nmessage = \"m\"\n[[routines]]\nname = \"r\"\nevery = \"2h\"\nmessage = \"m\"",
	}
	for name, body := range cases {
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".toml")
		writeToml(t, p, body)
		if _, err := readAssistant(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	p := filepath.Join(dir, "ok.toml")
	writeToml(t, p, "name = \"Report\"\nagent = \"claude\"\n[[routines]]\nname = \"Friday\"\ndaily = \"17:00\"\ndays = [\"Fri\", \"monday\"]\nmessage = \"Draft it\"\n[[routines]]\nname = \"Poll\"\nevery = \"2h\"\ncommand = \"uv run tools/poll.py\"")
	f, err := readAssistant(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.Sessions != "per-task" || f.Memory != "state" {
		t.Fatalf("defaults: %+v", f)
	}
	w, _ := f.Routines[0].when()
	if w.Kind != "daily" || w.Time != "17:00" || len(w.Days) != 2 || w.Days[0] != 5 || w.Days[1] != 1 {
		t.Fatalf("daily: %+v", w)
	}
	if w, _ := f.Routines[1].when(); w.Kind != "interval" || w.Minutes != 120 {
		t.Fatalf("every: %+v", w)
	}
}

func TestSettingUpAssistantsWritesOnlyWhatIsMissing(t *testing.T) {
	m, _ := fixture(t)
	root := filepath.Join(t.TempDir(), "assistants")
	// A file of the user's own is kept as it is.
	writeToml(t, filepath.Join(root, "AGENTS.md"), "my rules\n")
	if err := m.SetUpAssistants(root, "codex", ""); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")); string(b) != "my rules\n" {
		t.Fatalf("overwrote AGENTS.md: %q", b)
	}
	for _, p := range []string{"CLAUDE.md", ".gitignore", "shared/wiki/index.md", "main/assistant.toml", "main/AGENTS.md", "main/CLAUDE.md", "main/state/loops.md", "main/state/handoff.md", "main/log.md", "main/.agents/skills/.gitkeep"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	if target, err := os.Readlink(filepath.Join(root, "main", ".claude", "skills")); err == nil && target != filepath.Join("..", ".agents", "skills") {
		t.Errorf("skills link points to %q", target)
	}
	list := m.Assistants()
	if len(list) != 1 || list[0].Dir != "main" || list[0].Agent != "codex" || list[0].Sessions != "continuous" || list[0].Memory != "wiki" || list[0].Error != "" {
		t.Fatalf("assistants: %+v", list)
	}
	if _, err := exec.LookPath("git"); err == nil {
		if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
			t.Error("no git repository")
		}
	}
	// Set up again: nothing changes, main is not made twice.
	if err := m.SetUpAssistants(root, "claude", ""); err != nil {
		t.Fatal(err)
	}
	if list := m.Assistants(); len(list) != 1 || list[0].Agent != "codex" {
		t.Fatalf("after setting up again: %+v", list)
	}

	a, err := m.NewAssistant("Weekly report", "", "claude", "per-task", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Dir != "weekly-report" || a.Sessions != "per-task" || a.Error != "" {
		t.Fatalf("new assistant: %+v", a)
	}
	if _, err := m.NewAssistant("周报", "", "claude", "per-task", ""); err == nil {
		t.Fatal("a name without a folder name was accepted")
	}
	if _, err := m.NewAssistant("周报", "weekly-report", "claude", "per-task", ""); err == nil {
		t.Fatal("an existing folder was accepted")
	}
}

func TestAssistantRoutinesFollowTheirFile(t *testing.T) {
	m, _ := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root})
	path := filepath.Join(root, "mail", "assistant.toml")
	writeToml(t, path, "name = \"Mail\"\nagent = \"codex\"\n[[routines]]\nname = \"Fetch\"\nevery = \"30m\"\ncommand = \"uv run tools/fetch.py\"\nmessage = \"Sort these\"\n[[routines]]\nname = \"Digest\"\ndaily = \"18:00\"\nmessage = \"Summarize the day\"")
	m.Assistants()
	byName := func() map[string]Routine {
		list, _ := m.store.routines()
		out := map[string]Routine{}
		for _, v := range list {
			out[v.Name] = v
		}
		return out
	}
	got := byName()
	fetch := got["Fetch"]
	if len(got) != 2 || fetch.CreatedBy != "file:mail" || !fetch.Enabled || fetch.Command.Dir != filepath.Join(root, "mail") || fetch.Target.Kind != "assistant" || fetch.Target.Assistant != "mail" {
		t.Fatalf("routines: %+v", got)
	}
	if got["Digest"].Command != nil || got["Digest"].Message != "Summarize the day" {
		t.Fatalf("digest: %+v", got["Digest"])
	}
	// The page turns one off; the page cannot change or delete it.
	if _, err := m.SetRoutineEnabled(fetch.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveRoutine(fetch); err == nil || !strings.Contains(err.Error(), "mail/assistant.toml") {
		t.Fatalf("saved a file routine: %v", err)
	}
	// An edit keeps the switch and the id; a routine left out goes.
	time.Sleep(10 * time.Millisecond)
	writeToml(t, path, "name = \"Mail\"\nagent = \"codex\"\n[[routines]]\nname = \"Fetch\"\nevery = \"15m\"\ncommand = \"uv run tools/fetch.py --all\"\nmessage = \"Sort these\"")
	m.Assistants()
	got = byName()
	if len(got) != 1 || got["Fetch"].ID != fetch.ID || got["Fetch"].Enabled || got["Fetch"].When.Minutes != 15 {
		t.Fatalf("after the edit: %+v", got)
	}
	// A broken file keeps its routines until it is fixed.
	writeToml(t, path, "name = \"Mail\"\nagent = \"nobody\"")
	if list := m.Assistants(); len(list) != 1 || list[0].Error == "" {
		t.Fatalf("broken file: %+v", list)
	}
	if len(byName()) != 1 {
		t.Fatal("routines of a broken file dropped")
	}
	// No root: no file routines.
	_ = m.store.setting("assistants", assistantsSetting{})
	m.Assistants()
	if len(byName()) != 0 {
		t.Fatal("file routines kept without their files")
	}
}

func TestContinuousAssistantStartsNewTopics(t *testing.T) {
	m, _ := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root})
	writeToml(t, filepath.Join(root, "main", "assistant.toml"), "name = \"Assistant\"\nagent = \"claude\"\nsessions = \"continuous\"\nmemory = \"wiki\"")
	// Starting a topic ingests the last one in the background, which reads
	// the session too.
	var mu sync.Mutex
	session := Session{ID: "claude:a", Status: "sleeping"}
	set := func(f func(s *Session)) { mu.Lock(); f(&session); mu.Unlock() }
	m.as.lookup = func(id string) (Session, bool) { mu.Lock(); defer mu.Unlock(); return session, id == session.ID }
	m.as.background = func(Assistant, string) (string, error) { return "", nil }
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.Local)
	m.rt.now = func() time.Time { return now }
	m.saveTopic("main", assistantState{Current: "claude:a"})

	// Used today: it goes on.
	set(func(s *Session) { s.Updated = now.Add(-time.Hour).UTC().Format(time.RFC3339) })
	if out, _ := m.OpenAssistant("main"); out["session"] != "claude:a" {
		t.Fatalf("today: %+v", out)
	}
	// Busy past its context limit: still the same topic until it rests.
	m.assistantTurnEnded("claude:a", filepath.Join(root, "main"), 130000, 200000)
	set(func(s *Session) { s.Status = "running" })
	if out, _ := m.OpenAssistant("main"); out["session"] != "claude:a" {
		t.Fatalf("busy: %+v", out)
	}
	set(func(s *Session) { s.Status = "idle" })
	if out, _ := m.OpenAssistant("main"); out["fresh"] != true || out["previous"] != "claude:a" {
		t.Fatalf("context full: %+v", out)
	}
	// Last used before 04:00 today: a new topic.
	m.saveTopic("main", assistantState{Current: "claude:a"})
	set(func(s *Session) {
		s.Updated = time.Date(2026, 11, 19, 3, 0, 0, 0, time.Local).UTC().Format(time.RFC3339)
	})
	if out, _ := m.OpenAssistant("main"); out["fresh"] != true {
		t.Fatalf("new day: %+v", out)
	}
	// New topic by hand.
	m.saveTopic("main", assistantState{Current: "claude:a"})
	set(func(s *Session) { s.Updated = now.UTC().Format(time.RFC3339) })
	if err := m.NewTopic("main"); err != nil {
		t.Fatal(err)
	}
	if st := m.topicState("main"); st.Current != "" || st.Previous[len(st.Previous)-1] != "claude:a" {
		t.Fatalf("by hand: %+v", st)
	}
	if got := topicDay(time.Date(2026, 11, 19, 2, 0, 0, 0, time.Local)); got.Day() != 18 || got.Hour() != topicDayStarts {
		t.Fatalf("before 04:00 the day began yesterday: %v", got)
	}
}

func TestMailAssistantStartsFromItsTemplate(t *testing.T) {
	m, _ := fixture(t)
	root := filepath.Join(t.TempDir(), "assistants")
	if err := m.SetUpAssistants(root, "claude", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.NewAssistant("X", "", "claude", "per-task", "nope"); err == nil {
		t.Fatal("an unknown template was accepted")
	}
	a, err := m.NewAssistant("Mail", "", "codex", "continuous", "mail")
	if err != nil {
		t.Fatal(err)
	}
	if a.Error != "" || a.Agent != "codex" || a.Sessions != "per-task" || a.Routines != 1 {
		t.Fatalf("mail: %+v", a)
	}
	for _, p := range []string{"AGENTS.md", "CLAUDE.md", "config.toml", "tools/fetch.py", "tools/test_fetch.py", ".agents/skills/check-mail/SKILL.md", "state/inbox.md", "state/loops.md", "log.md"} {
		if _, err := os.Stat(filepath.Join(a.Path, filepath.FromSlash(p))); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(a.Path, "AGENTS.md")); !strings.HasPrefix(string(b), "# Mail\n") || strings.Contains(string(b), "{{") {
		t.Fatalf("AGENTS.md:\n%s", b)
	}
	// Its routine is a background one, off until it is set up.
	v, err := m.store.routine(fileRoutineID("mail", "Check mail"))
	if err != nil {
		t.Fatal(err)
	}
	if v.Enabled || v.Command == nil || v.Command.Run != "uv run tools/fetch.py" || v.Target == nil || !v.Target.Background || v.When.Minutes != 30 {
		t.Fatalf("routine: %+v %+v", v, v.Target)
	}
	// Its script's own tests pass.
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	if out, err := exec.Command(python, "-c", "import sys; sys.exit(sys.version_info < (3, 11))").CombinedOutput(); err != nil {
		t.Skipf("python3 is older than 3.11: %s", out)
	}
	cmd := exec.Command(python, filepath.Join("tools", "test_fetch.py"))
	cmd.Dir = a.Path
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("test_fetch.py: %v\n%s", err, out)
	}
}

// fakeAgents puts stand-in CLIs on PATH, and nothing else.
func fakeAgents(t *testing.T, names ...string) {
	t.Helper()
	posixOnly(t)
	dir := t.TempDir()
	for _, n := range names {
		writeToml(t, filepath.Join(dir, n), "#!/bin/sh\nexit 0\n")
		os.Chmod(filepath.Join(dir, n), 0o755)
	}
	t.Setenv("PATH", dir)
}

func TestSettingUpTakesDefaults(t *testing.T) {
	git, gitErr := exec.LookPath("git")
	m, _ := fixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	fakeAgents(t)
	if err := m.SetUpAssistants("", "", ""); err == nil || !strings.Contains(err.Error(), "install") {
		t.Fatalf("no agent: %v", err)
	}
	fakeAgents(t, "codex")
	if gitErr == nil {
		// git stays reachable for the commit.
		os.Symlink(git, filepath.Join(os.Getenv("PATH"), "git"))
	}
	if got := m.defaultAgent(); got != "codex" {
		t.Fatalf("default agent %q", got)
	}
	if err := m.SetUpAssistants("", "", "  "); err != nil {
		t.Fatal(err)
	}
	list := m.Assistants()
	if len(list) != 1 || list[0].Name != "Moss" || list[0].Agent != "codex" {
		t.Fatalf("assistants: %+v", list)
	}
	if want, _ := filepath.EvalSymlinks(filepath.Join(home, "assistants")); m.assistantsRoot() != want {
		t.Fatalf("root %q", m.assistantsRoot())
	}
	if _, err := os.Stat(filepath.Join(m.assistantsRoot(), ".agents", "skills", "create-assistant", "SKILL.md")); err != nil {
		t.Fatal("no create-assistant skill")
	}
	if b, _ := os.ReadFile(filepath.Join(list[0].Path, "AGENTS.md")); !strings.Contains(string(b), "(Moss unless the user changed it)") {
		t.Fatalf("main's AGENTS.md:\n%s", b)
	}
	if err := m.SetUpAssistants("", "", "two\nlines"); err == nil {
		t.Fatal("a name over two lines was accepted")
	}
}

func TestAssistantsAreRenamedAndSwitchAgents(t *testing.T) {
	m, _ := fixture(t)
	root := filepath.Join(t.TempDir(), "assistants")
	if err := m.SetUpAssistants(root, "claude", ""); err != nil {
		t.Fatal(err)
	}
	root = m.assistantsRoot()
	path := filepath.Join(root, "main", "assistant.toml")
	before, _ := os.ReadFile(path)
	m.saveTopic("main", assistantState{Current: "claude:a"})

	a, err := m.UpdateAssistant("main", "Mo", "")
	if err != nil || a.Name != "Mo" || a.Agent != "claude" {
		t.Fatalf("rename: %+v %v", a, err)
	}
	// Only the top-level name changed: the routines keep theirs.
	after, _ := os.ReadFile(path)
	if strings.Replace(string(before), `name = "Moss"`, `name = "Mo"`, 1) != string(after) {
		t.Fatalf("assistant.toml:\n%s", after)
	}
	if st := m.topicState("main"); st.Current != "claude:a" {
		t.Fatalf("a rename started a new topic: %+v", st)
	}
	// Switching agent starts a new topic: the ongoing one is the old agent's.
	if a, err = m.UpdateAssistant("main", "", "codex"); err != nil || a.Agent != "codex" || a.Name != "Mo" {
		t.Fatalf("switch: %+v %v", a, err)
	}
	if st := m.topicState("main"); st.Current != "" || st.Previous[len(st.Previous)-1] != "claude:a" {
		t.Fatalf("topic after the switch: %+v", st)
	}
	if v, _ := m.store.routine(fileRoutineID("main", "Daily check")); v.Name != "Daily check" {
		t.Fatalf("routine: %+v", v)
	}
	for _, bad := range [][2]string{{"", "gpt"}, {"two\nlines", ""}, {strings.Repeat("x", 61), ""}} {
		if _, err := m.UpdateAssistant("main", bad[0], bad[1]); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := m.UpdateAssistant("nobody", "x", ""); err == nil {
		t.Fatal("an unknown assistant was accepted")
	}
	// A file without a top-level name line is refused, not broken.
	writeToml(t, filepath.Join(root, "odd", "assistant.toml"), "agent = \"claude\"\nname = 'Odd'\n")
	if _, err := m.UpdateAssistant("odd", "Even", ""); err == nil {
		t.Fatal("changed a name it could not find")
	}
	if _, err := exec.LookPath("git"); err == nil {
		if out, _ := gitRun(root, "log", "--format=%s", "-2"); out != "main: switch to codex\nmain: rename to Mo\n" {
			t.Fatalf("log:\n%s", out)
		}
	}
	if got, ok := setTopLevel("name = \"A\"\r\nagent = \"claude\"\r\n", "name", "B"); !ok || got != "name = \"B\"\r\nagent = \"claude\"\r\n" {
		t.Fatalf("CRLF: %q", got)
	}
}

func TestOlderRepositoriesGetNewSharedFiles(t *testing.T) {
	m, _ := fixture(t)
	root := filepath.Join(t.TempDir(), "assistants")
	if err := m.SetUpAssistants(root, "claude", ""); err != nil {
		t.Fatal(err)
	}
	root = m.assistantsRoot()
	skill := filepath.Join(root, ".agents", "skills", "create-assistant", "SKILL.md")
	// As set up by v0.4.0: no skill; the user's rules are their own.
	os.RemoveAll(filepath.Dir(skill))
	writeToml(t, filepath.Join(root, "AGENTS.md"), "my rules\n")
	commitAll(root, "older")
	m.ensureShared()
	if _, err := os.Stat(skill); err != nil {
		t.Fatal("the skill was not added")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")); string(b) != "my rules\n" {
		t.Fatal("changed the user's rules")
	}
	if _, err := exec.LookPath("git"); err == nil {
		log1, _ := gitRun(root, "log", "--format=%s")
		m.ensureShared()
		log2, _ := gitRun(root, "log", "--format=%s")
		if !strings.HasPrefix(log1, "chore: add shared files from Agent Workbench\n") || log1 != log2 {
			t.Fatalf("log:\n%s\nthen:\n%s", log1, log2)
		}
	}
}
