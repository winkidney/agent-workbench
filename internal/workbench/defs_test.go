package workbench

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefinitionAndDataAreTold(t *testing.T) {
	for _, def := range []string{"assistant.toml", "AGENTS.md", "config.example.toml", "icon.png", ".gitignore", ".claude/settings.json", "tools/fetch.py", "tools/test_fetch.py", ".agents/skills/x/SKILL.md", "examples/draft.toml", "./AGENTS.md"} {
		if !definitionFile(def) {
			t.Errorf("%s: not definition", def)
		}
	}
	for _, data := range []string{"settings.toml", "config.toml", "state/loops.md", "log.md", "raw/mail/a.md", "data/x.json", "drafts/a.md", "reports/r.md", "reviews/r.md", "notes.md", "CLAUDE.md", ".claude/skills", "tools/__pycache__/a.cpython-312.pyc", "tools/a.pyc"} {
		if definitionFile(data) {
			t.Errorf("%s: definition", data)
		}
	}
	for _, gen := range []string{"CLAUDE.md", ".claude/skills", ".claude/skills/x/SKILL.md", ".claude/SKILLS.md"} {
		if !generatedFile(gen) {
			t.Errorf("%s: not generated", gen)
		}
	}
	dir := t.TempDir()
	for _, f := range []string{"assistant.toml", "AGENTS.md", "CLAUDE.md", "config.toml", "settings.toml", "tools/a.py", "tools/__pycache__/a.pyc", ".agents/skills/s/SKILL.md", "state/loops.md", "log.md"} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755)
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}
	os.MkdirAll(filepath.Join(dir, ".claude"), 0o755)
	os.Symlink("../.agents/skills", filepath.Join(dir, ".claude", "skills"))
	got, err := definitionFiles(dir)
	want := []string{".agents/skills/s/SKILL.md", "AGENTS.md", "assistant.toml", "tools/a.py"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("definition files %v %v", got, err)
	}
}

// The owner's settings lie over the author's file: a key set wins, a key
// not set leaves the author's, routines by name, unknown keys refused.
func TestOwnerSettingsLieOverTheAuthors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "assistant.toml")
	writeToml(t, path, `name = "Mail"
agent = "claude"
agents = ["claude", "codex"]
ready = false
icon = "mail"

[options.claude]
model = "opus"

[[routines]]
name = "Check mail"
every = "30m"
message = "m"
enabled = false

[[routines]]
name = "Summary"
daily = "10:00"
message = "s"
`)
	on, yes := true, true
	if err := writeAssistantSettings(dir, AssistantSettings{Agent: "codex", Ready: &yes, Options: map[string]AgentOptions{"codex": {Effort: "low"}, "claude": {}}, Routines: map[string]RoutineSetting{"Check mail": {Enabled: &on}, "Gone": {Enabled: &on}}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "settings.toml"))
	for _, line := range []string{`agent = "codex"`, "ready = true", "[options.codex]\neffort = \"low\"", "[routines.\"Check mail\"]\nenabled = true"} {
		if !strings.Contains(string(b), line) {
			t.Fatalf("settings.toml lacks %q:\n%s", line, b)
		}
	}
	if strings.Contains(string(b), "[options.claude]") {
		t.Fatalf("an empty table was written:\n%s", b)
	}
	f, err := readAssistant(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Agent != "codex" || f.Ready == nil || !*f.Ready || f.Icon != "mail" || f.Options["claude"].Model != "opus" || f.Options["codex"].Effort != "low" {
		t.Fatalf("merged %+v", f)
	}
	if !*f.Routines[0].Enabled || f.Routines[1].Enabled != nil {
		t.Fatalf("routines %+v %+v", f.Routines[0], f.Routines[1])
	}
	// Nothing set: no file.
	if err := writeAssistantSettings(dir, AssistantSettings{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.toml")); err == nil {
		t.Fatal("an empty settings.toml stayed")
	}
	writeToml(t, filepath.Join(dir, "settings.toml"), "agnet = \"codex\"\n")
	if _, err := readAssistant(path); err == nil || !strings.Contains(err.Error(), "settings.toml") {
		t.Fatalf("a typo passed: %v", err)
	}
	writeToml(t, filepath.Join(dir, "settings.toml"), "")
	writeToml(t, path, "name = \"x\"\nagent = \"claude\"\nagents = [\"gemini\"]\n")
	if _, err := readAssistant(path); err == nil {
		t.Fatal("an unknown agent in agents passed")
	}
}
