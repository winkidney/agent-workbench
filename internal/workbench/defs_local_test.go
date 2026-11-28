package workbench

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The owner's additions reach both agents: Claude through CLAUDE.md,
// Codex through AGENTS.override.md made from the two files.
func TestOwnersAdditionsReachBothAgents(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644)
	}
	read := func(rel string) string {
		b, _ := os.ReadFile(filepath.Join(dir, rel))
		return string(b)
	}
	write("AGENTS.md", "# Mail\n\nSort mail.\n")
	write("CLAUDE.md", claudePlain)
	if err := writeAgentFiles(dir); err != nil {
		t.Fatal(err)
	}
	if read("CLAUDE.md") != claudePlain {
		t.Fatal("CLAUDE.md changed without additions")
	}
	if _, err := os.Stat(filepath.Join(dir, agentsOverride)); err == nil {
		t.Fatal("an override without additions")
	}
	write(agentsLocal, "I am Ana.\n")
	writeAgentFiles(dir)
	if read("CLAUDE.md") != claudeLocal {
		t.Fatalf("CLAUDE.md: %q", read("CLAUDE.md"))
	}
	o := read(agentsOverride)
	if !strings.HasPrefix(o, overrideHeader) || !strings.Contains(o, "Sort mail.") || !strings.HasSuffix(o, "these win.\n\nI am Ana.\n") {
		t.Fatalf("override:\n%s", o)
	}
	// Unchanged: not written again.
	info, _ := os.Stat(filepath.Join(dir, agentsOverride))
	time.Sleep(10 * time.Millisecond)
	writeAgentFiles(dir)
	if again, _ := os.Stat(filepath.Join(dir, agentsOverride)); !again.ModTime().Equal(info.ModTime()) {
		t.Fatal("written again unchanged")
	}
	// The author's procedure changes: the override follows.
	write("AGENTS.md", "# Mail\n\nSort mail twice.\n")
	writeAgentFiles(dir)
	if !strings.Contains(read(agentsOverride), "Sort mail twice.") {
		t.Fatal("the override did not follow AGENTS.md")
	}
	// Additions removed: back as before.
	os.Remove(filepath.Join(dir, agentsLocal))
	writeAgentFiles(dir)
	if read("CLAUDE.md") != claudePlain {
		t.Fatal("CLAUDE.md kept the import")
	}
	if _, err := os.Stat(filepath.Join(dir, agentsOverride)); err == nil {
		t.Fatal("the override stayed")
	}
	// Files written by hand are left alone.
	write("CLAUDE.md", "@AGENTS.md\nMore of mine.\n")
	write(agentsOverride, "mine\n")
	write(agentsLocal, "I am Ana.\n")
	writeAgentFiles(dir)
	if read("CLAUDE.md") != "@AGENTS.md\nMore of mine.\n" || read(agentsOverride) != "mine\n" {
		t.Fatal("a hand-written file was replaced")
	}
}

// Installed: the additions are the owner's data, the override is made and
// never committed, and the definition stays as the catalog has it.
func TestAdditionsInAnInstalledAssistant(t *testing.T) {
	m, root, _, url := installFixture(t)
	if _, err := m.InstallAssistant(url, "mail", "", ""); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "mail")
	os.WriteFile(filepath.Join(dir, agentsLocal), []byte("Only my work mailbox.\n"), 0o644)
	m.agentFilesFor(dir)
	if _, err := os.Stat(filepath.Join(dir, agentsOverride)); err != nil {
		t.Fatal("no override for a session there")
	}
	m.commitPaths(root, "my additions", []string{"mail/" + agentsLocal})
	in := tracked(t, root)
	if !in["mail/"+agentsLocal] || in["mail/"+agentsOverride] || in["mail/AGENTS.md"] || in["mail/CLAUDE.md"] {
		t.Fatalf("tracked: %v", in)
	}
	if out, _ := exec.Command("git", "-C", root, "status", "--porcelain").Output(); len(out) != 0 {
		t.Fatalf("not clean:\n%s", out)
	}
	// Outside the repository: nothing is made.
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "AGENTS.md"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(other, agentsLocal), []byte("y\n"), 0o644)
	m.agentFilesFor(other)
	if _, err := os.Stat(filepath.Join(other, agentsOverride)); err == nil {
		t.Fatal("made outside the repository")
	}
}

// settings.toml changes the keys it sets of an author's routine, adds the
// owner's own, and says what the card says.
func TestOwnerRoutinesAndCardInSettings(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "assistant.toml"), []byte(`name = "Mail"
agent = "claude"
description = "Sorts mail."
examples = ["Sort it"]

[[routines]]
name = "Check mail"
every = "30m"
command = "uv run tools/fetch.py"
message = "Sort it."
enabled = false
`), 0o644)
	settings := `examples = ["Sort my work mail"]

[routines."Check mail"]
enabled = true
daily = "09:30"

[routines."Weekly"]
cron = "0 20 * * 5"
message = "Write the week."

[routines."Gone"]
enabled = false
`
	os.WriteFile(filepath.Join(dir, settingsFile), []byte(settings), 0o644)
	f, err := readAssistant(filepath.Join(dir, "assistant.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Routines) != 2 {
		t.Fatalf("routines %+v", f.Routines)
	}
	c, w := f.Routines[0], f.Routines[1]
	if c.Daily != "09:30" || c.Every != "" || c.Command != "uv run tools/fetch.py" || c.Message != "Sort it." || !*c.Enabled {
		t.Fatalf("the author's routine: %+v", c)
	}
	if w.Name != "Weekly" || w.Cron != "0 20 * * 5" || w.Message != "Write the week." || w.Enabled != nil {
		t.Fatalf("the owner's routine: %+v", w)
	}
	if f.Description != "Sorts mail." || len(f.Examples) != 1 || f.Examples[0] != "Sort my work mail" {
		t.Fatalf("card: %q %v", f.Description, f.Examples)
	}
	// Written back as read.
	s, _ := readSettings(dir)
	if err := writeAssistantSettings(dir, s); err != nil {
		t.Fatal(err)
	}
	if again, err := readSettings(dir); err != nil || len(again.Routines) != 3 || again.Routines["Weekly"].Cron != "0 20 * * 5" || again.Examples[0] != "Sort my work mail" {
		t.Fatalf("round trip %+v %v", again, err)
	}
	for _, bad := range []string{
		"[routines.\"New\"]\nmessage = \"x\"\n",
		"[routines.\"New\"]\nevery = \"1h\"\n",
		"[routines.\"New\"]\nevery = \"1h\"\nwhatever = 1\n",
	} {
		os.WriteFile(filepath.Join(dir, settingsFile), []byte(bad), 0o644)
		if _, err := readAssistant(filepath.Join(dir, "assistant.toml")); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// The page's edits of an installed assistant's card leave its author's
// assistant.toml as the catalog has it.
func TestCardEditsOfAnInstalledAssistantAreTheOwners(t *testing.T) {
	m, root, _, url := installFixture(t)
	if _, err := m.InstallAssistant(url, "mail", "", ""); err != nil {
		t.Fatal(err)
	}
	desc := "My mail, sorted."
	ex := []string{"Anything from Ana?"}
	a, err := m.PatchAssistant("mail", AssistantPatch{Description: &desc, Examples: &ex})
	if err != nil {
		t.Fatal(err)
	}
	if a.Description != desc || len(a.Examples) != 1 || a.Examples[0] != ex[0] {
		t.Fatalf("card %q %v", a.Description, a.Examples)
	}
	if tracked(t, root)["mail/assistant.toml"] {
		t.Fatal("assistant.toml became the owner's")
	}
	s, _ := readSettings(filepath.Join(root, "mail"))
	if s.Description != desc || s.Examples[0] != ex[0] {
		t.Fatalf("settings %+v", s)
	}
}

// An update says what the owner's settings keep over it, and is otherwise
// the catalog's: nothing of the owner's is in the definition.
func TestUpdatesSayWhatSettingsKeep(t *testing.T) {
	m, root, work, url := installFixture(t)
	if _, err := m.InstallAssistant(url, "mail", "", ""); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "mail")
	os.WriteFile(filepath.Join(dir, settingsFile), []byte("[routines.\"Summary\"]\ndaily = \"18:00\"\nmessage = \"Mine.\"\n\n[routines.\"Check mail\"]\ncommand = \"uv run tools/fetch.py --mine\"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, agentsLocal), []byte("Only work mail.\n"), 0o644)
	m.commitPaths(root, "mine", []string{"mail"})
	toml, _ := os.ReadFile(filepath.Join(work, "mail/assistant.toml"))
	next := strings.Replace(string(toml), "uv run tools/fetch.py", "uv run tools/fetch.py --all", 1) + "\n[[routines]]\nname = \"Summary\"\ndaily = \"10:00\"\nmessage = \"Theirs.\"\n"
	os.WriteFile(filepath.Join(work, "mail/assistant.toml"), []byte(next), 0o644)
	os.WriteFile(filepath.Join(work, "mail/AGENTS.md"), []byte("# Mail, two\n"), 0o644)
	commitCatalog(t, work, "two")
	u, err := m.UpdateFromCatalog("mail", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range u.Files {
		if f.Result != "update" {
			t.Fatalf("%s: %s, not a fast-forward", f.Rel, f.Result)
		}
		if f.Rel == "assistant.toml" && (len(f.Notes) != 2 || !strings.Contains(f.Notes[0], "command of \"Check mail\"") || !strings.Contains(f.Notes[1], "adds a routine \"Summary\"")) {
			t.Fatalf("notes %q", f.Notes)
		}
	}
	if _, err := m.UpdateFromCatalog("mail", true, nil, u.Commit); err != nil {
		t.Fatal(err)
	}
	if o, _ := os.ReadFile(filepath.Join(dir, agentsOverride)); !strings.Contains(string(o), "# Mail, two") {
		t.Fatal("the override did not follow the update")
	}
	f, err := readAssistant(filepath.Join(dir, "assistant.toml"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range f.Routines {
		got[r.Name] = r.Message
	}
	if got["Summary"] != "Mine." {
		t.Fatalf("routines %v", got)
	}
}

// Moss's card as an earlier version wrote it becomes the current one; one
// the user changed stays theirs.
func TestMossCardFollowsTheWorkbench(t *testing.T) {
	m, root, _, _ := installFixture(t)
	_ = m
	path := filepath.Join(root, "main", "assistant.toml")
	data, _ := os.ReadFile(path)
	old := olderMossMetas[0]["zh-Hans"]
	text := setOrInsert(string(data), "description", strconv.Quote(old.Description))
	text = setOrInsert(text, "capabilities", tomlStrings(old.Capabilities))
	text = setOrInsert(text, "examples", tomlStrings(old.Examples))
	os.WriteFile(path, []byte(text), 0o644)
	if !upgradeMossCard(root) {
		t.Fatal("not upgraded")
	}
	f, err := readAssistant(path)
	if err != nil || !reflect.DeepEqual(f.Examples, mossMetas["zh-Hans"].Examples) || f.Name == "" {
		t.Fatalf("%v %v", f.Examples, err)
	}
	if upgradeMossCard(root) {
		t.Fatal("the current card upgraded again")
	}
	os.WriteFile(path, []byte(setOrInsert(text, "examples", tomlStrings([]string{"Mine"}))), 0o644)
	if upgradeMossCard(root) {
		t.Fatal("a card the user changed was replaced")
	}
	if !strings.Contains(mustRead(t, filepath.Join(root, "main", "AGENTS.md")), "assistant catalog") {
		t.Fatal("Moss's procedure does not install from catalogs")
	}
	if !strings.Contains(string(mossFile("welcome.md")), "Ready-made assistants") {
		t.Fatal("the welcome does not tell of ready-made assistants")
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
