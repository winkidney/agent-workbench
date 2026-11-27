package workbench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateViews = flag.Bool("update", false, "rewrite testdata/claude/*.view.json")

// Each case in testdata/claude is one event as Claude writes it live
// (<case>.live.jsonl, stream-json) and in its transcript
// (<case>.transcript.jsonl). History must read the transcript as
// <case>.view.json says, which the page's tests read too
// (web/src/transcript/records.test.ts), and live must show what history
// shows under the same key (ADR 0025).
func TestClaudeRecordsReadAliveAsInHistory(t *testing.T) {
	files, _ := filepath.Glob("testdata/claude/*.transcript.jsonl")
	if len(files) == 0 {
		t.Fatal("no cases")
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".transcript.jsonl")
		t.Run(name, func(t *testing.T) {
			page, err := readClaudeHistory(f, Session{ID: "claude:x"}, "", 50)
			if err != nil {
				t.Fatal(err)
			}
			views := []map[string]any{}
			for _, e := range page.Events {
				var v map[string]any
				if err := json.Unmarshal(e.Data, &v); err != nil {
					t.Fatal(err)
				}
				views = append(views, v)
			}
			got, _ := json.MarshalIndent(views, "", "  ")
			got = append(got, '\n')
			golden := filepath.Join("testdata", "claude", name+".view.json")
			if *updateViews {
				os.WriteFile(golden, got, 0o644)
			}
			want, err := os.ReadFile(golden)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("history differs from %s (go test -run %s -update):\n%s", golden, t.Name(), got)
			}

			byKey := map[string]map[string]any{}
			reports := 0
			for _, v := range views {
				if k := str(v["key"]); k != "" {
					byKey[k] = v
				}
				if v["type"] == "workbench_peer" && v["handback"] == true {
					reports++
				}
			}
			lf, err := os.Open(filepath.Join("testdata", "claude", name+".live.jsonl"))
			if err != nil {
				return
			}
			defer lf.Close()
			shown := 0
			sc := bufio.NewScanner(lf)
			sc.Buffer(nil, 1<<20)
			for sc.Scan() {
				var v map[string]any
				json.Unmarshal(sc.Bytes(), &v)
				noteClaudeLive(v)
				view := claudeView(v)
				if view == nil {
					continue
				}
				shown++
				// Live shows what history shows, under its key.
				b, _ := json.Marshal(boundJSON(view))
				var live map[string]any
				json.Unmarshal(b, &live)
				h := byKey[str(live["key"])]
				if h == nil {
					t.Fatalf("live %v has no record in history %v", live["key"], keys(byKey))
				}
				for k, x := range live {
					if k == "uuid" || k == "timestamp" {
						continue
					}
					if hb, _ := json.Marshal(h[k]); string(hb) != mustJSON(x) {
						t.Errorf("%s: live %s, history %s", k, mustJSON(x), hb)
					}
				}
			}
			// A subagent's report shows live; other task notifications only
			// from the transcript (claude_records.go liveTaskNotification).
			if shown != reports {
				t.Fatalf("live shows %d records, history %d reports", shown, reports)
			}
		})
	}
}

func TestClaudeRecordsLogUnknownShapesOnce(t *testing.T) {
	claudeShapes.Delete("live system/brand_new")
	noteClaudeLive(map[string]any{"type": "system", "subtype": "brand_new"})
	if _, ok := claudeShapes.Load("live system/brand_new"); !ok {
		t.Fatal("unknown live record not noted")
	}
	noteClaudeLive(map[string]any{"type": "system", "subtype": "hook_started"})
	if _, ok := claudeShapes.Load("live system/hook_started"); ok {
		t.Fatal("known live record noted")
	}
	// A queued message of a kind not read is noted and left out.
	if v := claudeView(map[string]any{"type": "attachment", "attachment": map[string]any{"type": "queued_command", "commandMode": "brand-new", "prompt": "x"}}); v != nil {
		t.Fatalf("unknown queued mode shown: %v", v)
	}
	if _, ok := claudeShapes.Load("queued_command mode brand-new"); !ok {
		t.Fatal("unknown queued mode not noted")
	}
}

func keys(m map[string]map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
