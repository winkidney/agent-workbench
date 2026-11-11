package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeSettings(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeSettingsDefaultsLayerUserProjectLocal(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	// Unrelated keys (including credentials-like ones) are never decoded.
	writeSettings(t, filepath.Join(home, "settings.json"), `{"model":"opus","effortLevel":"high","env":{"TOKEN":"x"},"permissions":{"defaultMode":"acceptEdits","allow":["Bash"]}}`)
	writeSettings(t, filepath.Join(cwd, ".claude", "settings.json"), `{"effortLevel":"low","fastMode":true}`)
	writeSettings(t, filepath.Join(cwd, ".claude", "settings.local.json"), `{"permissions":{"defaultMode":"plan"}}`)
	got := claudeSettingsDefaults(home, cwd)
	want := Options{Model: "opus", Effort: "low", Speed: "fast", Mode: "plan", Permissions: "manual"}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}

	// A broken layer is skipped, not fatal; nothing set falls back to the CLI's own.
	writeSettings(t, filepath.Join(cwd, ".claude", "settings.local.json"), `{broken`)
	if got := claudeSettingsDefaults(home, cwd); got.Mode != "default" || got.Permissions != "acceptEdits" {
		t.Fatalf("got %+v", got)
	}
	if got := claudeSettingsDefaults(t.TempDir(), t.TempDir()); got != (Options{Model: "default", Speed: "standard", Mode: "default", Permissions: "manual"}) {
		t.Fatalf("empty settings: %+v", got)
	}
}

func TestCodexDefaultsFromConfigAndModelList(t *testing.T) {
	models := []any{
		map[string]any{"model": "small", "defaultReasoningEffort": "medium"},
		map[string]any{"model": "big", "isDefault": true, "defaultReasoningEffort": "low"},
	}
	got := codexDefaults(map[string]any{}, models)
	if got != (Options{Model: "big", Effort: "low", Speed: "default", Mode: "default", Permissions: ":workspace"}) {
		t.Fatalf("model list fallback: %+v", got)
	}
	got = codexDefaults(map[string]any{"model": "small", "service_tier": "priority"}, models)
	if got.Model != "small" || got.Effort != "medium" || got.Speed != "priority" {
		t.Fatalf("config model: %+v", got)
	}
	got = codexDefaults(map[string]any{"model": "small", "model_reasoning_effort": "xhigh"}, models)
	if got.Effort != "xhigh" {
		t.Fatalf("config effort wins: %+v", got)
	}
}

func TestCodexThreadResultFillsEffective(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("codex", t.TempDir(), "x")
	_ = m.Start(s.ID)
	r := m.sessions[s.ID]
	v := map[string]any{}
	json.Unmarshal([]byte(`{"id":"thread","result":{"thread":{"id":"n"},"model":"gpt-x","reasoningEffort":"high","serviceTier":null,"sandbox":{"type":"readOnly"},"collaborationMode":{"mode":"plan","settings":{}}}}`), &v)
	m.receive(s.ID, r.generation, map[string]any{"id": "init", "result": map[string]any{}})
	m.receive(s.ID, r.generation, v)
	want := Options{Model: "gpt-x", Effort: "high", Speed: "default", Mode: "plan", Permissions: ":read-only"}
	if got := m.Telemetry(r.session.ID).Effective; got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}

	// A turn acknowledgement overlays overrides without clearing the rest.
	m.store.setting("options:"+r.session.ID, Options{Effort: "low"})
	m.receive(r.session.ID, r.generation, map[string]any{"id": "turn:1", "result": map[string]any{}})
	want.Effort = "low"
	if got := m.Telemetry(r.session.ID).Effective; got != want {
		t.Fatalf("after turn: got %+v want %+v", got, want)
	}
}

func TestCodexRolloutTurnContext(t *testing.T) {
	v := map[string]any{}
	json.Unmarshal([]byte(`{"model":"gpt-x","effort":"high","sandbox_policy":{"type":"workspace-write"},"collaboration_mode":{"mode":"default"}}`), &v)
	if got := codexTurnOptions(v); got != (Options{Model: "gpt-x", Effort: "high", Mode: "default", Permissions: ":workspace"}) {
		t.Fatalf("%+v", got)
	}
}

func TestClaudeInitAndAppliedOptions(t *testing.T) {
	init := map[string]any{"type": "system", "subtype": "init", "model": "claude-x", "permissionMode": "acceptEdits", "fast_mode_state": "cooldown"}
	got := claudeInitOptions(Options{Effort: "high"}, init)
	if got != (Options{Model: "claude-x", Effort: "high", Speed: "fast", Mode: "default", Permissions: "acceptEdits"}) {
		t.Fatalf("init: %+v", got)
	}
	init["permissionMode"], init["fast_mode_state"] = "plan", "off"
	if got := claudeInitOptions(Options{}, init); got.Mode != "plan" || got.Permissions != "manual" || got.Speed != "standard" {
		t.Fatalf("plan init: %+v", got)
	}

	live := Options{Model: "claude-x", Effort: "max", Speed: "fast", Mode: "default", Permissions: "acceptEdits"}
	// Clearing effort and speed hands them back to the CLI; Mode default sends manual.
	got = claudeApplied(live, Options{Effort: "max", Speed: "fast"}, Options{Mode: "default"})
	if got != (Options{Model: "claude-x", Mode: "default", Permissions: "manual"}) {
		t.Fatalf("cleared: %+v", got)
	}
	// Nothing sent for permissions keeps what the CLI reported.
	got = claudeApplied(live, Options{}, Options{Model: "sonnet"})
	if got.Model != "sonnet" || got.Permissions != "acceptEdits" || got.Effort != "max" {
		t.Fatalf("overlay: %+v", got)
	}
	if got := mergeOptions(live, Options{}); got != live {
		t.Fatalf("empty overrides changed values: %+v", got)
	}
}
