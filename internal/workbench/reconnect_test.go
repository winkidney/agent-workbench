package workbench

import (
	"testing"
	"time"
)

// Codex lost the model's stream and will retry; its next attempt can wait
// minutes. The status line says what it waits on and since when, so the
// page can offer to try now; the model answering again clears it.
func TestCodexReconnectShowsAndClears(t *testing.T) {
	m, _ := fixture(t)
	s, _ := m.Create("codex", t.TempDir(), "reconnect")
	_ = m.Start(s.ID)
	r := m.sessions[s.ID]
	gen := r.generation
	m.receive(s.ID, gen, map[string]any{"id": "init", "result": map[string]any{}})
	m.receive(s.ID, gen, map[string]any{"id": "thread", "result": map[string]any{"thread": map[string]any{"id": "main"}}})
	m.receive(s.ID, gen, map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "main", "turn": map[string]any{"id": "t1"}}})
	// As recorded on 2026-11-18 (Codex 0.169).
	m.receive(s.ID, gen, map[string]any{"method": "error", "params": map[string]any{"threadId": "main", "turnId": "t1", "willRetry": true, "error": map[string]any{
		"message":           "Reconnecting... 1/5",
		"additionalDetails": "Selected model is at capacity. Please try a different model.",
		"codexErrorInfo":    map[string]any{"responseStreamDisconnected": map[string]any{"httpStatusCode": nil}},
	}}})
	tel := m.Telemetry(s.ID)
	if tel.Detail != "Reconnecting to the model (1/5) · Selected model is at capacity" {
		t.Fatalf("detail: %q", tel.Detail)
	}
	since, err := time.Parse(time.RFC3339, tel.RetrySince)
	if err != nil || time.Since(since) > time.Minute {
		t.Fatalf("since: %q", tel.RetrySince)
	}
	// The next attempt keeps the first wait's start.
	m.receive(s.ID, gen, map[string]any{"method": "error", "params": map[string]any{"threadId": "main", "willRetry": true, "error": map[string]any{"message": "Reconnecting... 2/5"}}})
	if tel2 := m.Telemetry(s.ID); tel2.Detail != "Reconnecting to the model (2/5)" || tel2.RetrySince != tel.RetrySince {
		t.Fatalf("second attempt: %q %q", tel2.Detail, tel2.RetrySince)
	}
	// The model answers again.
	m.receive(s.ID, gen, map[string]any{"method": "item/started", "params": map[string]any{"threadId": "main", "item": map[string]any{"type": "agentMessage", "id": "a1"}}})
	if tel3 := m.Telemetry(s.ID); tel3.Detail != "" || tel3.RetrySince != "" {
		t.Fatalf("after the model answered: %q %q", tel3.Detail, tel3.RetrySince)
	}
	// A failure Codex will not retry is not a reconnect.
	m.receive(s.ID, gen, map[string]any{"method": "error", "params": map[string]any{"threadId": "main", "willRetry": false, "error": map[string]any{"message": "stream disconnected"}}})
	if tel4 := m.Telemetry(s.ID); tel4.RetrySince != "" {
		t.Fatalf("final error shown as a reconnect: %q", tel4.Detail)
	}
}
