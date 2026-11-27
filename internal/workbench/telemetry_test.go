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

// The context follows each reply of the main conversation, with the window
// a result names, or the last one known before that.
func TestClaudeContextFollowsReplies(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("claude", t.TempDir(), "test")
	m.store.setting("telemetry:"+s.ID, Telemetry{Context: map[string]any{"used": 10.0, "window": 500000.0}})
	r := m.sessions[s.ID]
	r.proc = &sideProcess{}
	feed := func(line string) {
		v := map[string]any{}
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatal(err)
		}
		m.track(r, v)
	}
	feed(`{"type":"assistant","parent_tool_use_id":null,"message":{"model":"opus","usage":{"input_tokens":2,"cache_read_input_tokens":900,"cache_creation_input_tokens":98,"output_tokens":7}}}`)
	if c := m.Telemetry(s.ID).Context; c["used"] != 1000.0 || c["window"] != 500000.0 || c["estimated"] != false {
		t.Fatalf("before a result: %v", c)
	}
	// A reply the CLI writes itself names no context.
	feed(`{"type":"assistant","parent_tool_use_id":null,"message":{"model":"<synthetic>","usage":{"input_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`)
	if c := m.Telemetry(s.ID).Context; c["used"] != 1000.0 {
		t.Fatalf("after a synthetic reply: %v", c)
	}
	// A subagent's reply is not this conversation's context.
	feed(`{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"usage":{"input_tokens":5,"cache_read_input_tokens":50000}}}`)
	feed(`{"type":"result","usage":{"input_tokens":2},"modelUsage":{"haiku":{"contextWindow":200000},"opus":{"contextWindow":1000000}}}`)
	if c := m.Telemetry(s.ID).Context; c["used"] != 1000.0 || c["window"] != 1000000.0 {
		t.Fatalf("after the result: %v", c)
	}
	var saved Telemetry
	m.store.readSetting("telemetry:"+s.ID, &saved)
	if saved.Context["used"] != 1000.0 || saved.Context["window"] != 1000000.0 {
		t.Fatalf("saved: %v", saved.Context)
	}
}

func TestClaudeResultKeepsSavedContext(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("claude", t.TempDir(), "test")
	m.store.setting("telemetry:"+s.ID, Telemetry{Context: map[string]any{"used": 10.0, "window": 500000.0}})
	m.track(m.sessions[s.ID], map[string]any{"type": "result"})
	var saved Telemetry
	m.store.readSetting("telemetry:"+s.ID, &saved)
	if saved.Context["window"] != 500000.0 {
		t.Fatalf("saved: %v", saved.Context)
	}
}

// A session whose window is not known (another client runs it) takes the
// last window a result named for its model.
func TestClaudeContextTakesTheModelsLastWindow(t *testing.T) {
	m, _ := fixture(t)
	a, _ := m.Create("claude", t.TempDir(), "a")
	r := m.sessions[a.ID]
	r.effective.Model = "claude-opus-5-5"
	m.track(r, map[string]any{"type": "result", "modelUsage": map[string]any{"claude-opus-5-5": map[string]any{"contextWindow": 1000000.0}}})
	b, _ := m.Create("claude", t.TempDir(), "b")
	m.store.setting("telemetry:"+b.ID, Telemetry{Effective: Options{Model: "claude-opus-5-5"}, Context: map[string]any{"used": 1000.0}})
	if c := m.Telemetry(b.ID).Context; c["window"] != 1000000.0 {
		t.Fatalf("context: %v", c)
	}
	m.store.setting("telemetry:"+b.ID, Telemetry{Effective: Options{Model: "other"}, Context: map[string]any{"used": 1000.0}})
	if c := m.Telemetry(b.ID).Context; c["window"] != nil {
		t.Fatalf("another model's window: %v", c)
	}
}

// A window saved for another model is not this one's.
func TestClaudeContextKeepsOnlyTheSameModelsWindow(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("claude", t.TempDir(), "test")
	m.store.setting("telemetry:"+s.ID, Telemetry{Effective: Options{Model: "big"}, Context: map[string]any{"used": 10.0, "window": 1000000.0}})
	r := m.sessions[s.ID]
	r.proc = &sideProcess{}
	m.track(r, map[string]any{"type": "assistant", "message": map[string]any{"model": "small", "usage": map[string]any{"input_tokens": 50.0}}})
	if c := m.Telemetry(s.ID).Context; c["used"] != 50.0 || c["window"] != nil {
		t.Fatalf("another model's window kept: %v", c)
	}
	m.track(r, map[string]any{"type": "assistant", "message": map[string]any{"model": "big", "usage": map[string]any{"input_tokens": 60.0}}})
	if c := m.Telemetry(s.ID).Context; c["window"] != 1000000.0 {
		t.Fatalf("the same model's window lost: %v", c)
	}
}
