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
	for _, p := range []string{"CLAUDE.md", ".gitignore", "shared/wiki/index.md", "main/assistant.toml", "main/AGENTS.md", "main/CLAUDE.md", "main/state/loops.md", "main/state/handoff.md", "main/log.md", "main/.agents/skills/.gitkeep", ".agents/skills/start-session/SKILL.md"} {
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

	a, err := m.NewAssistant("Weekly report", "", "claude", "per-task")
	if err != nil {
		t.Fatal(err)
	}
	if a.Dir != "weekly-report" || a.Sessions != "per-task" || a.Error != "" {
		t.Fatalf("new assistant: %+v", a)
	}
	if _, err := m.NewAssistant("周报", "", "claude", "per-task"); err == nil {
		t.Fatal("a name without a folder name was accepted")
	}
	if _, err := m.NewAssistant("周报", "weekly-report", "claude", "per-task"); err == nil {
		t.Fatal("an existing folder was accepted")
	}
}

func TestAssistantRoutinesFollowTheirFile(t *testing.T) {
	m, _ := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root})
	path := filepath.Join(root, "mail", "assistant.toml")
	writeToml(t, path, "name = \"Mail\"\nagent = \"codex\"\n[[routines]]\nname = \"Fetch\"\nevery = \"30m\"\ncommand = \"uv run tools/fetch.py\"\nmessage = \"Sort these\"\n[[routines]]\nname = \"Digest\"\ndaily = \"18:00\"\nmessage = \"Summarize the day\"")
	m.assistantsChanged()
	m.assistantsChanged()
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
	m.assistantsChanged()
	m.assistantsChanged()
	got = byName()
	if len(got) != 1 || got["Fetch"].ID != fetch.ID || got["Fetch"].Enabled || got["Fetch"].When.Minutes != 15 {
		t.Fatalf("after the edit: %+v", got)
	}
	// A broken file keeps its routines until it is fixed.
	writeToml(t, path, "name = \"Mail\"\nagent = \"nobody\"")
	m.assistantsChanged()
	if list := m.Assistants(); len(list) != 1 || list[0].Error == "" {
		t.Fatalf("broken file: %+v", list)
	}
	if len(byName()) != 1 {
		t.Fatal("routines of a broken file dropped")
	}
	// No root: no file routines.
	_ = m.store.setting("assistants", assistantsSetting{})
	m.assistantsChanged()
	if len(byName()) != 0 {
		t.Fatal("file routines kept without their files")
	}
}

func TestContinuousAssistantStartsNewTopics(t *testing.T) {
	m, _ := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root})
	writeToml(t, filepath.Join(root, "main", "assistant.toml"), "name = \"Assistant\"\nagent = \"claude\"\nsessions = \"continuous\"\nmemory = \"wiki\"")
	m.assistantsChanged()
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
	for _, skill := range []string{"create-assistant", "help"} {
		if _, err := os.Stat(filepath.Join(m.assistantsRoot(), ".agents", "skills", skill, "SKILL.md")); err != nil {
			t.Fatalf("no %s skill", skill)
		}
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

	a, err := m.UpdateAssistant("main", "Mo", "", nil)
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
	if a, err = m.UpdateAssistant("main", "", "codex", nil); err != nil || a.Agent != "codex" || a.Name != "Mo" {
		t.Fatalf("switch: %+v %v", a, err)
	}
	if st := m.topicState("main"); st.Current != "" || st.Previous[len(st.Previous)-1] != "claude:a" {
		t.Fatalf("topic after the switch: %+v", st)
	}
	if v, _ := m.store.routine(fileRoutineID("main", "Daily check")); v.Name != "Daily check" {
		t.Fatalf("routine: %+v", v)
	}
	if v, _ := m.store.routine(fileRoutineID("main", "Repository sync")); v.Command == nil || v.Command.Run != "uv run tools/repo_sync.py" {
		t.Fatalf("repository sync routine: %+v", v)
	}
	for _, bad := range [][2]string{{"", "gpt"}, {"two\nlines", ""}, {strings.Repeat("x", 61), ""}} {
		if _, err := m.UpdateAssistant("main", bad[0], bad[1], nil); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := m.UpdateAssistant("nobody", "x", "", nil); err == nil {
		t.Fatal("an unknown assistant was accepted")
	}
	// A name in single quotes is a name too; the result is checked TOML.
	writeToml(t, filepath.Join(root, "odd", "assistant.toml"), "agent = \"claude\"\nname = 'Odd'\n")
	m.assistantsChanged()
	if a, err := m.UpdateAssistant("odd", "Even", "", nil); err != nil || a.Name != "Even" {
		t.Fatalf("single quotes: %+v %v", a, err)
	}
	// A file without a top-level name line is refused, not broken.
	writeToml(t, filepath.Join(root, "odder", "assistant.toml"), "agent = \"claude\"\n[x]\nname = 'Odd'\n")
	m.assistantsChanged()
	if _, err := m.UpdateAssistant("odder", "Even", "", nil); err == nil {
		t.Fatal("changed a name it could not find")
	}
	if _, err := exec.LookPath("git"); err == nil {
		if out, _ := gitRun(root, "log", "--format=%s", "-3"); out != "odd: rename to Even\nmain: switch to codex\nmain: rename to Mo\n" {
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

func TestAssistantsOrderAndTurningOff(t *testing.T) {
	m, _ := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root})
	for _, d := range []string{"calendar", "main", "zeta", "code-reviewer"} {
		body := "name = \"" + d + "\"\nagent = \"claude\"\n"
		if d == "calendar" {
			body += "\n[[routines]]\nname = \"Agenda\"\nevery = \"1h\"\nmessage = \"x\"\n"
		}
		writeToml(t, filepath.Join(root, d, "assistant.toml"), body)
		m.assistantsChanged()
	}
	dirs := func() string {
		var out []string
		for _, a := range m.Assistants() {
			out = append(out, a.Dir)
		}
		return strings.Join(out, ",")
	}
	// Moss first, then by folder.
	if got := dirs(); got != "main,calendar,code-reviewer,zeta" {
		t.Fatalf("order: %s", got)
	}
	// The dragged order; Moss stays first whatever it says.
	if err := m.SetAssistantOrder([]string{"zeta", "main", "calendar"}); err != nil {
		t.Fatal(err)
	}
	if got := dirs(); got != "main,zeta,calendar,code-reviewer" {
		t.Fatalf("dragged: %s", got)
	}
	if err := m.SetAssistantOrder([]string{"../x"}); err == nil {
		t.Fatal("a bad folder was accepted")
	}

	// Turning off writes enabled = false after the agent line, pauses its
	// routines and refuses messages; Moss cannot be turned off.
	off, on := false, true
	a, err := m.UpdateAssistant("calendar", "", "", &off)
	if err != nil || a.Enabled {
		t.Fatalf("off: %+v %v", a, err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "calendar", "assistant.toml"))
	if !strings.Contains(string(b), "agent = \"claude\"\nenabled = false\n") || !strings.Contains(string(b), "[[routines]]\nname = \"Agenda\"") {
		t.Fatalf("assistant.toml:\n%s", b)
	}
	list, _ := m.Routines()
	if len(list) != 1 || !list[0].AssistantOff || list[0].Next != "" {
		t.Fatalf("routine: %+v", list)
	}
	if _, err := m.RunRoutine(list[0].ID); err == nil {
		t.Fatal("ran a routine of an assistant that is off")
	}
	if _, _, err := m.AssistantMessage("calendar", "hi", nil, ""); err == nil || !strings.Contains(err.Error(), "turned off") {
		t.Fatalf("message: %v", err)
	}
	if _, err := m.UpdateAssistant("main", "", "", &off); err == nil {
		t.Fatal("turned Moss off")
	}
	if a, err := m.UpdateAssistant("calendar", "", "", &on); err != nil || !a.Enabled {
		t.Fatalf("on: %+v %v", a, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "calendar", "assistant.toml")); !strings.Contains(string(b), "enabled = true\n") {
		t.Fatalf("back on:\n%s", b)
	}
	if list, _ := m.Routines(); list[0].AssistantOff {
		t.Fatal("still paused")
	}
	// A file without an agent line gets the key before its first table.
	if got := insertTopLevel("name = \"x\"\n\n[[routines]]\nname = \"r\"\n", "enabled", "false"); got != "name = \"x\"\n\nenabled = false\n\n[[routines]]\nname = \"r\"\n" {
		t.Fatalf("insert: %q", got)
	}
}

func TestAssistantsDescribeThemselves(t *testing.T) {
	m, _ := fixture(t)
	root := filepath.Join(t.TempDir(), "assistants")
	if _, err := m.setUp(root, "claude", "", "zh-Hant"); err != nil {
		t.Fatal(err)
	}
	root = m.assistantsRoot()
	moss, _ := m.assistant("main")
	if !moss.Ready || !strings.Contains(moss.Description, "個人助理") || len(moss.Capabilities) == 0 || len(moss.Examples) == 0 {
		t.Fatalf("Moss: %+v", moss)
	}
	a, err := m.NewAssistant("Report", "", "claude", "per-task")
	if err != nil || a.Ready || a.Description != "" {
		t.Fatalf("new: %+v %v", a, err)
	}
	// The page fills in the home card; lists are TOML arrays, quotes kept.
	desc, caps, ex, ready := `Drafts the "weekly" report`, []string{"Reads git logs", " ", "Asks before sending"}, []string{"Draft this week's report"}, true
	a, err = m.PatchAssistant("report", AssistantPatch{Description: &desc, Capabilities: &caps, Examples: &ex, Ready: &ready, Sessions: "continuous"})
	if err != nil || a.Description != desc || len(a.Capabilities) != 2 || a.Examples[0] != ex[0] || !a.Ready || a.Sessions != "continuous" {
		t.Fatalf("patched: %+v %v", a, err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "report", "assistant.toml"))
	if !strings.Contains(string(b), `capabilities = ["Reads git logs", "Asks before sending"]`) || !strings.Contains(string(b), "# What the page shows about it") {
		t.Fatalf("assistant.toml:\n%s", b)
	}
	// Missing keys are added; bad values are refused and leave the file.
	writeToml(t, filepath.Join(root, "old", "assistant.toml"), "name = \"Old\"\nagent = \"codex\"\n")
	m.assistantsChanged()
	if a, err := m.PatchAssistant("old", AssistantPatch{Description: &desc}); err != nil || a.Description != desc || !a.Ready {
		t.Fatalf("old: %+v %v", a, err)
	}
	long := []string{strings.Repeat("x", 201)}
	if _, err := m.PatchAssistant("report", AssistantPatch{Examples: &long}); err == nil {
		t.Fatal("a 201-character example was accepted")
	}
	two := "a\nb"
	if _, err := m.PatchAssistant("report", AssistantPatch{Description: &two}); err == nil {
		t.Fatal("a two-line description was accepted")
	}
	many := make([]string, 13)
	for i := range many {
		many[i] = "x"
	}
	if _, err := m.PatchAssistant("report", AssistantPatch{Capabilities: &many}); err == nil {
		t.Fatal("13 capabilities were accepted")
	}
	if a, _ := m.assistant("report"); a.Description != desc {
		t.Fatalf("a refused change touched the file: %+v", a)
	}
	if !strings.Contains(setUpText("Report", "en"), "ready = true") {
		t.Fatal("the set-up message does not say to set ready")
	}
}

func TestAssistantsSayWhatTheyNeed(t *testing.T) {
	m, _ := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root})
	writeToml(t, filepath.Join(root, "cr", "assistant.toml"), "name = \"CR\"\nagent = \"claude\"\nrequires = [\"GitHub: gh signed in, or the GitHub MCP server\"]\n")
	m.assistantsChanged()
	a, err := m.assistant("cr")
	if err != nil || len(a.Requires) != 1 {
		t.Fatalf("requires: %+v %v", a, err)
	}
	need := []string{"GitHub: gh signed in", "Linear: the Linear MCP server or an API key"}
	if a, err = m.PatchAssistant("cr", AssistantPatch{Requires: &need}); err != nil || len(a.Requires) != 2 {
		t.Fatalf("patched: %+v %v", a, err)
	}
	for _, f := range []string{"shared/tools/secret.py", "shared/tools/test_secret.py"} {
		if _, ok := sharedFiles[f]; !ok {
			t.Errorf("%s is not shared", f)
		}
	}
}

func TestAssistantFilesAreReadOnReloadOnly(t *testing.T) {
	m, _ := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root})
	p := filepath.Join(root, "a", "assistant.toml")
	writeToml(t, p, "name = \"A\"\nagent = \"claude\"")
	if l := m.Assistants(); len(l) != 1 || l[0].Name != "A" {
		t.Fatalf("first read: %+v", l)
	}
	// Edited behind the workbench's back: not seen until a reload.
	writeToml(t, p, "name = \"B\"\nagent = \"claude\"")
	writeToml(t, filepath.Join(root, "c", "assistant.toml"), "name = \"C\"\nagent = \"codex\"")
	if l := m.Assistants(); len(l) != 1 || l[0].Name != "A" {
		t.Fatalf("read without a reload: %+v", l)
	}
	m.assistantsChanged()
	if l := m.Assistants(); len(l) != 2 || l[0].Name != "B" {
		t.Fatalf("after the reload: %+v", l)
	}
}
