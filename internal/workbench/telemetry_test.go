package workbench

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTelemetrySnapshotsAndPendingOptions(t *testing.T) {
	m, _ := fixture(t)
	s, err := m.Create("codex", t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	r := m.sessions[s.ID]
	r.began = time.Now().Add(-time.Second)
	v := map[string]any{}
	json.Unmarshal([]byte(`{"method":"thread/tokenUsage/updated","params":{"tokenUsage":{"total":{"inputTokens":120,"cachedInputTokens":40,"outputTokens":30,"reasoningOutputTokens":10,"totalTokens":150},"last":{"inputTokens":100,"outputTokens":20},"modelContextWindow":1000}}}`), &v)
	m.track(r, v)
	m.track(r, v)
	m.track(r, map[string]any{"method": "turn/completed"})
	got := m.Telemetry(s.ID)
	if got.Total["totalTokens"] != float64(150) {
		t.Fatal("cumulative counters summed")
	}
	if got.Context["window"] != float64(1000) {
		t.Fatal(got.Context)
	}
	m.store.setting("options:"+s.ID, Options{Model: "chosen", Mode: "plan"})
	if !m.Telemetry(s.ID).Pending {
		t.Fatal("missing pending state")
	}
	m.mu.Lock()
	delete(m.sessions, s.ID)
	m.mu.Unlock()
	got = m.Telemetry(s.ID)
	if got.Total["totalTokens"] != float64(150) {
		t.Fatal("snapshot not recovered")
	}
}
func TestCodexOptionsMapPlanAndPermissions(t *testing.T) {
	p := map[string]any{"sandbox": "workspace-write"}
	codexOptions(p, Options{Model: "example", Effort: "high", Mode: "plan", Permissions: "workspace", Speed: "fast"})
	if p["sandbox"] != nil || p["permissions"] != "workspace" {
		t.Fatal(p)
	}
	settings := obj(obj(p["collaborationMode"])["settings"])
	if settings["model"] != "example" || settings["reasoning_effort"] != "high" {
		t.Fatal(p)
	}
}
func TestClaudeCumulativeUsageDoesNotCountWindowSize(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("claude", t.TempDir(), "test")
	r := m.sessions[s.ID]
	v := map[string]any{}
	json.Unmarshal([]byte(`{"type":"result","usage":{"input_tokens":10,"output_tokens":5},"modelUsage":{"model":{"inputTokens":100,"outputTokens":20,"contextWindow":200000,"cacheReadInputTokens":40}}}`), &v)
	m.track(r, v)
	m.track(r, v)
	if r.total["inputTokens"] != float64(100) || r.total["contextWindow"] != nil {
		t.Fatal(r.total)
	}
}

func TestClaudeOptionsFollowNativeIdentity(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("claude", t.TempDir(), "test")
	m.store.setting("options:"+s.ID, Options{Model: "sonnet", Mode: "plan"})
	if err := m.Start(s.ID); err != nil {
		t.Fatal(err)
	}
	r := m.sessions[s.ID]
	m.receive(s.ID, r.generation, map[string]any{"type": "system", "subtype": "init", "session_id": "native", "model": "resolved"})
	if got := m.options("claude:native"); got.Model != "sonnet" || got.Mode != "plan" {
		t.Fatal(got)
	}
}
