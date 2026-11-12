package workbench

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// writeZstd writes data as Codex compresses a record: one zstd frame.
func writeZstd(t *testing.T, path string, data []byte) {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, enc.EncodeAll(data, nil), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCodexRecordFindsCompressedAndMovedRecords(t *testing.T) {
	home := t.TempDir()
	plain := filepath.Join(home, "sessions", "2026", "10", "01", "rollout-a.jsonl")
	writeFile(t, plain, []byte("{}\n"))
	compressed := filepath.Join(home, "sessions", "2026", "10", "01", "rollout-b.jsonl")
	writeZstd(t, compressed+".zst", []byte("{}\n"))
	// Recorded under another home; here it is compressed.
	moved := filepath.Join(home, "archived_sessions", "rollout-c.jsonl")
	writeZstd(t, moved+".zst", []byte("{}\n"))
	elsewhere := "/elsewhere/user/.codex/archived_sessions/rollout-c.jsonl"

	cases := []struct {
		recorded string
		want     recordFile
		ok       bool
	}{
		{plain, recordFile{Path: plain}, true},
		{compressed, recordFile{Path: compressed + ".zst", Zstd: true}, true},
		{elsewhere, recordFile{Path: moved + ".zst", Zstd: true}, true},
		// Relative paths are inside the home.
		{filepath.Join("sessions", "2026", "10", "01", "rollout-a.jsonl"), recordFile{Path: plain}, true},
		{filepath.Join(home, "sessions", "gone.jsonl"), recordFile{}, false},
		{"", recordFile{}, false},
	}
	for _, c := range cases {
		got, ok := codexRecord(home, c.recorded)
		if ok != c.ok || got != c.want {
			t.Errorf("%q: got %+v %v, want %+v %v", c.recorded, got, ok, c.want, c.ok)
		}
	}
}

func TestEachRecordLineKeepsWhatTheFilterWants(t *testing.T) {
	marker := []byte(`"keep"`)
	keep := func(line []byte) bool { return bytes.Contains(line, marker) }
	short := `{"type":"keep","n":1}` + "\n"
	longKept := `{"type":"keep","pad":"` + strings.Repeat("x", 2<<20) + `"}` + "\n"
	longSkipped := `{"type":"other","pad":"` + strings.Repeat("y", 3<<20) + `"}` + "\n"
	tooLong := `{"type":"keep","pad":"` + strings.Repeat("z", 9<<20) + `"}` + "\n"
	partial := `{"type":"keep","n":2}`
	data := short + longKept + longSkipped + tooLong + short + partial

	var lines []string
	read, err := eachRecordLine(strings.NewReader(data), false, keep, func(line []byte) {
		lines = append(lines, string(line[:min(len(line), 24)]))
	})
	if err != nil {
		t.Fatal(err)
	}
	// The partial last line is left for later; a line over the limit is
	// skipped; the others arrive whole.
	if read != int64(len(data)-len(partial)) {
		t.Fatalf("read %d of %d", read, len(data)-len(partial))
	}
	if len(lines) != 3 || !strings.HasPrefix(lines[1], `{"type":"keep","pad":"xx`) {
		t.Fatalf("lines %q", lines)
	}
	// A complete (compressed) record ends with its last line, newline or not.
	lines = nil
	read, _ = eachRecordLine(strings.NewReader(data), true, keep, func(line []byte) {
		lines = append(lines, string(line))
	})
	if read != int64(len(data)) || len(lines) != 4 || lines[3] != partial {
		t.Fatalf("final: read %d, last %q", read, lines[len(lines)-1])
	}
	// A long line arrives whole.
	var whole int
	eachRecordLine(strings.NewReader(longKept), true, keep, func(line []byte) { whole = len(line) })
	if whole != len(longKept) {
		t.Fatalf("long line %d of %d", whole, len(longKept))
	}
}

// codexIndex writes a Codex thread index as 0.167 keeps it.
func codexIndex(t *testing.T, home string, rows ...[]any) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(home, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE threads(id TEXT, rollout_path TEXT, created_at INTEGER, updated_at INTEGER, source TEXT, cwd TEXT, title TEXT, name TEXT, archived INTEGER, first_user_message TEXT, tokens_used INTEGER, history_mode TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO threads VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, r...); err != nil {
			t.Fatal(err)
		}
	}
}

func codexUsageRecord() []byte {
	line := func(v map[string]any) string { b, _ := json.Marshal(v); return string(b) + "\n" }
	var b strings.Builder
	b.WriteString(line(map[string]any{"type": "session_meta", "payload": map[string]any{"cwd": "/work"}}))
	b.WriteString(line(map[string]any{"type": "turn_context", "payload": map[string]any{"model": "gpt-test", "effort": "low"}}))
	for i := 1; i <= 3; i++ {
		b.WriteString(line(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "token_count", "info": map[string]any{
			"total_token_usage":    map[string]any{"total_tokens": 1000 * i, "input_tokens": 900 * i},
			"last_token_usage":     map[string]any{"input_tokens": 300 * i},
			"model_context_window": 200000,
		}}}))
	}
	return []byte(b.String())
}

func TestCompressedRecordsMissingRecordsAndNullColumns(t *testing.T) {
	posixOnly(t)
	// No codex on PATH: history goes through app-server, which is absent.
	t.Setenv("PATH", t.TempDir())
	m, _ := fixture(t)
	home := t.TempDir()
	record := filepath.Join(home, "sessions", "2026", "10", "01", "rollout-z.jsonl")
	writeZstd(t, record+".zst", codexUsageRecord())
	codexIndex(t, home,
		[]any{"z", "/elsewhere/.codex/sessions/2026/10/01/rollout-z.jsonl", 1, 30, "cli", "/work", "Compressed", "", 0, "hello", 3000, "paginated"},
		[]any{"gone", filepath.Join(home, "sessions", "2026", "07", "01", "rollout-gone.jsonl"), 1784000000, 20, "cli", "/work", "Gone", "", 0, "an old question", 12345, "legacy"},
		// Columns Codex has not filled in yet.
		[]any{"null", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
	)
	m.catalog.CodexHome = home
	m.catalog.ClaudeHome = t.TempDir()
	list, err := m.catalog.List()
	if err != nil || len(list) != 3 {
		t.Fatalf("listed %d %v", len(list), err)
	}

	// Usage from the compressed record, read once in the background; the
	// page is told meanwhile.
	first := m.Telemetry("codex:z")
	if !first.Reading || first.Total != nil {
		t.Fatalf("first read %+v", first)
	}
	var got Telemetry
	for i := 0; i < 200; i++ {
		if got = m.nativeUsage("codex:z"); !got.Reading {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.Total["total_tokens"] != float64(3000) || got.Context["window"] != float64(200000) || got.Effective.Model != "gpt-test" {
		t.Fatalf("usage %+v", got)
	}
	// Kept: a new manager over the same store does not read it again.
	m.usage = nil
	if again := m.Telemetry("codex:z"); again.Reading || again.Total["total_tokens"] != float64(3000) {
		t.Fatalf("cached %+v", again)
	}

	// A record that is gone: its remains, not an error.
	gone, _ := m.catalog.Find("codex:gone")
	page, err := m.catalog.History(gone, "", 50)
	if err != nil || page.RecordMissing == nil {
		t.Fatalf("missing record: %v %+v", err, page)
	}
	if r := page.RecordMissing; r.FirstMessage != "an old question" || r.TokensUsed != 12345 || r.Created == "" {
		t.Fatalf("remains %+v", r)
	}
	// One whose record exists but cannot be read still fails.
	z, _ := m.catalog.Find("codex:z")
	if _, err := m.catalog.History(z, "", 50); err == nil {
		t.Fatal("history without app-server succeeded")
	}
}

func TestActivityIndexReadsCompressedRecords(t *testing.T) {
	posixOnly(t)
	m, _ := fixture(t)
	root := t.TempDir()
	for _, repo := range []string{"api", "web"} {
		os.MkdirAll(filepath.Join(root, repo, ".git"), 0700)
	}
	call := func(workdir string) string {
		args, _ := json.Marshal(map[string]any{"cmd": "make", "workdir": workdir})
		b, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{"type": "function_call", "name": "exec_command", "arguments": string(args)}})
		return string(b) + "\n"
	}
	var head, tail strings.Builder
	for i := 0; i < 10; i++ {
		head.WriteString(call(filepath.Join(root, "api")))
		tail.WriteString(call(filepath.Join(root, "web")))
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	writeFile(t, path, []byte(head.String()))
	s := Session{ID: "codex:a", Agent: "codex", Workspace: root, Origin: "project"}
	m.activity.Scan([]Session{s}, map[string]recordFile{s.ID: {Path: path}}, 1<<30)
	if got := strings.Join(m.activity.Related(s.ID), ","); got != "api" {
		t.Fatalf("plain %q", got)
	}
	// The record grew, then Codex compressed it: the rest is counted once.
	os.Remove(path)
	writeZstd(t, path+".zst", []byte(head.String()+tail.String()))
	compressed := recordFile{Path: path + ".zst", Zstd: true}
	m.activity.Scan([]Session{s}, map[string]recordFile{s.ID: compressed}, 1<<30)
	if got := strings.Join(m.activity.Related(s.ID), ","); got != "api,web" {
		t.Fatalf("compressed %q", got)
	}
	m.activity.Scan([]Session{s}, map[string]recordFile{s.ID: compressed}, 1<<30)
	var counts string
	m.store.db.QueryRow(`SELECT counts FROM session_activity WHERE id=?`, s.ID).Scan(&counts)
	if counts != `{"api":10,"web":10}` {
		t.Fatalf("counted twice: %s", counts)
	}
	// A compressed record larger than what is left of a pass waits, unless
	// it is the first of the pass.
	other := Session{ID: "codex:b", Agent: "codex", Workspace: root, Origin: "project"}
	big := filepath.Join(dir, "big.jsonl.zst")
	writeZstd(t, big, []byte(tail.String()))
	files := map[string]recordFile{other.ID: {Path: big, Zstd: true}}
	m.activity.Scan([]Session{other}, files, 10)
	if got := m.activity.Related(other.ID); len(got) != 1 {
		t.Fatalf("first of a pass not read: %v", got)
	}
}
