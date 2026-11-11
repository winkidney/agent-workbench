package workbench

import (
	"testing"
	"time"
)

func TestPruneKeepsRecentEventsPerSession(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := time.Now().Add(-10 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	for i := 0; i < 5; i++ {
		s.db.Exec("INSERT INTO events(session,kind,data,time) VALUES('a','native','{}',?)", old)
	}
	for i := 0; i < 30; i++ {
		s.Append("b", "native", map[string]int{"n": i})
	}
	for i := 0; i < 3; i++ {
		s.Append("c", "native", map[string]int{"n": i})
	}
	n, err := s.Prune(time.Now().Add(-eventRetention), 10)
	if err != nil || n != 5+20 {
		t.Fatalf("pruned %d: %v", n, err)
	}
	count := func(id string) (n int, first int) {
		s.db.QueryRow("SELECT count(*) FROM events WHERE session=?", id).Scan(&n)
		var data string
		s.db.QueryRow("SELECT data FROM events WHERE session=? ORDER BY seq LIMIT 1", id).Scan(&data)
		return n, len(data)
	}
	if a, _ := count("a"); a != 0 {
		t.Fatalf("old events kept: %d", a)
	}
	if b, _ := count("b"); b != 10 {
		t.Fatalf("per-session cap: %d", b)
	}
	var newest string
	s.db.QueryRow("SELECT data FROM events WHERE session='b' ORDER BY seq LIMIT 1").Scan(&newest)
	if newest != `{"n":20}` {
		t.Fatalf("kept the wrong events, oldest kept %s", newest)
	}
	if c, _ := count("c"); c != 3 {
		t.Fatalf("small session touched: %d", c)
	}
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	if s.Size() == 0 {
		t.Fatal("size unknown")
	}
}

func TestProgressOnlyEventsAreNotStored(t *testing.T) {
	for _, v := range []map[string]any{
		{"type": "system", "subtype": "thinking_tokens", "estimated_tokens_delta": 10.0},
		{"type": "system", "subtype": "status", "status": "requesting"},
		{"type": "tool_progress", "heartbeat": true},
		{"method": "thread/status/changed", "params": map[string]any{}},
		{"method": "mcpServer/startupStatus/updated", "params": map[string]any{}},
	} {
		if _, class := classifyLive(v); class != liveDrop {
			t.Fatalf("stored: %v", v)
		}
	}
	for _, v := range []map[string]any{
		{"type": "system", "subtype": "init"},
		{"type": "system", "subtype": "task_notification"},
		{"method": "item/completed", "params": map[string]any{}},
	} {
		if _, class := classifyLive(v); class != liveKeep {
			t.Fatalf("dropped: %v", v)
		}
	}
}
