package workbench

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if err := m.SetUpAssistants(root, "codex"); err != nil {
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
	if err := m.SetUpAssistants(root, "claude"); err != nil {
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
	session := Session{ID: "claude:a", Status: "sleeping"}
	m.as.lookup = func(id string) (Session, bool) { return session, id == session.ID }
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.Local)
	m.rt.now = func() time.Time { return now }
	m.saveTopic("main", assistantState{Current: "claude:a"})

	// Used today: it goes on.
	session.Updated = now.Add(-time.Hour).UTC().Format(time.RFC3339)
	if out, _ := m.OpenAssistant("main"); out["session"] != "claude:a" {
		t.Fatalf("today: %+v", out)
	}
	// Busy past its context limit: still the same topic until it rests.
	m.assistantTurnEnded("claude:a", filepath.Join(root, "main"), 130000, 200000)
	session.Status = "running"
	if out, _ := m.OpenAssistant("main"); out["session"] != "claude:a" {
		t.Fatalf("busy: %+v", out)
	}
	session.Status = "idle"
	if out, _ := m.OpenAssistant("main"); out["fresh"] != true || out["previous"] != "claude:a" {
		t.Fatalf("context full: %+v", out)
	}
	// Last used before 04:00 today: a new topic.
	m.saveTopic("main", assistantState{Current: "claude:a"})
	session.Updated = time.Date(2026, 11, 19, 3, 0, 0, 0, time.Local).UTC().Format(time.RFC3339)
	if out, _ := m.OpenAssistant("main"); out["fresh"] != true {
		t.Fatalf("new day: %+v", out)
	}
	// New topic by hand.
	m.saveTopic("main", assistantState{Current: "claude:a"})
	session.Updated = now.UTC().Format(time.RFC3339)
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
