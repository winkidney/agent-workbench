package workbench

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAssistantOptions(t *testing.T) {
	m, _ := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root})
	path := filepath.Join(root, "cr", "assistant.toml")
	writeToml(t, path, "name = \"CR\"\nagent = \"claude\"\n\n# Its routines.\n[[routines]]\nname = \"Check\"\nevery = \"1h\"\nmessage = \"Check.\"\n")
	m.assistantsChanged()
	a, _ := m.assistant("cr")
	// Without a table: Settings → Agents, as for any new session.
	_ = m.store.setting("defaults:claude", Options{Model: "opus", Effort: "high"})
	if o := m.assistantOptions(a); o.Model != "opus" || o.Effort != "high" || o.Permissions != "auto" {
		t.Fatalf("defaults: %+v", o)
	}
	a, err := m.PatchAssistant("cr", AssistantPatch{Options: map[string]AgentOptions{"claude": {Model: "sonnet"}, "codex": {Effort: "low"}}})
	if err != nil || a.Options["claude"].Model != "sonnet" || a.Options["codex"].Effort != "low" {
		t.Fatalf("patched: %+v %v", a, err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "[options.claude]\nmodel = \"sonnet\"\n\n") || strings.Index(string(b), "[options.") > strings.Index(string(b), "# Its routines.") || !strings.Contains(string(b), "[[routines]]\nname = \"Check\"") {
		t.Fatalf("assistant.toml:\n%s", b)
	}
	if strings.Index(string(b), "[options.claude]") > strings.Index(string(b), "[options.codex]") {
		t.Fatalf("the tables are apart or out of order:\n%s", b)
	}
	// Its table first, the saved defaults for what it leaves out.
	if o := m.assistantOptions(a); o.Model != "sonnet" || o.Effort != "high" {
		t.Fatalf("resolved: %+v", o)
	}
	// The whole table is replaced; an empty one goes.
	if a, err = m.PatchAssistant("cr", AssistantPatch{Options: map[string]AgentOptions{"claude": {Model: "opus", Effort: "max"}, "codex": {}}}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Count(string(b), "[options.claude]") != 1 || strings.Contains(string(b), "[options.codex]") || !strings.Contains(string(b), "effort = \"max\"") {
		t.Fatalf("replaced:\n%s", b)
	}
	if o := m.assistantOptions(a); o.Model != "opus" || o.Effort != "max" {
		t.Fatalf("after replace: %+v", o)
	}
	// Bad values are refused and leave the file.
	if _, err := m.PatchAssistant("cr", AssistantPatch{Options: map[string]AgentOptions{"claude": {Model: "a\"b"}}}); err == nil {
		t.Fatal("a quote in a model was accepted")
	}
	for _, bad := range []string{"[options.gemini]\nmodel = \"x\"\n", "[options.claude]\nspeed = \"fast\"\n"} {
		writeToml(t, filepath.Join(root, "bad", "assistant.toml"), "name = \"Bad\"\nagent = \"claude\"\n"+bad)
		if _, err := readAssistant(filepath.Join(root, "bad", "assistant.toml")); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	// A file with no tables gets it at the end.
	if got := setOptionsTable("name = \"x\"\n", "codex", AgentOptions{Model: "m"}); got != "name = \"x\"\n\n[options.codex]\nmodel = \"m\"\n" {
		t.Fatalf("no tables: %q", got)
	}
	// Background runs take the model and effort only.
	if got := backgroundModel("claude", Options{Model: "opus", Effort: "high", Permissions: "bypassPermissions"}); !slices.Equal(got, []string{"--model", "opus", "--effort", "high"}) {
		t.Fatalf("claude: %q", got)
	}
	if got := backgroundModel("codex", Options{Model: "gpt", Effort: "low", Mode: "plan"}); !slices.Equal(got, []string{"-m", "gpt", "-c", `model_reasoning_effort="low"`}) {
		t.Fatalf("codex: %q", got)
	}
}
