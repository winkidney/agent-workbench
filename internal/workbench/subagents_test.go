package workbench

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func jsonLine(t testing.TB, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// claudeWithSubagent writes a main transcript whose Agent call started
// subagent a1, and the subagent's transcript with calls tool calls whose
// results are result bytes long. agentIDInResult: the Agent result names
// the subagent (otherwise only its meta file does).
func claudeWithSubagent(t testing.TB, calls, result int, agentIDInResult bool) (string, Session) {
	dir := t.TempDir()
	main := filepath.Join(dir, "s1.jsonl")
	tr := map[string]any{"status": "async_launched"}
	if agentIDInResult {
		tr["agentId"] = "a1"
	}
	lines := []map[string]any{
		{"type": "user", "uuid": "u1", "message": map[string]any{"role": "user", "content": "look around"}},
		{"type": "assistant", "uuid": "m1", "parentUuid": "u1", "message": map[string]any{"id": "msg1", "role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "toolu_A", "name": "Agent", "input": map[string]any{"description": "Look around", "prompt": "ls"}}}}},
		{"type": "user", "uuid": "r1", "parentUuid": "m1", "toolUseResult": tr, "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_A", "content": "Async agent launched"}}}},
		{"type": "assistant", "uuid": "m2", "parentUuid": "r1", "message": map[string]any{"id": "msg2", "role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Started it."}}}},
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(jsonLine(t, l))
	}
	if err := os.WriteFile(main, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := claudeSubagentDir(main)
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(sub, "agent-a1.meta.json"), []byte(`{"agentType":"general-purpose","description":"Look around","toolUseId":"toolu_A"}`), 0o600)
	f, err := os.Create(filepath.Join(sub, "agent-a1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	big := strings.Repeat("x", result)
	w := func(v map[string]any) {
		bb, _ := json.Marshal(v)
		f.Write(append(bb, '\n'))
	}
	w(map[string]any{"type": "user", "uuid": "s0", "isSidechain": true, "message": map[string]any{"role": "user", "content": "ls"}})
	for i := 0; i < calls; i++ {
		id := fmt.Sprintf("t%d", i)
		w(map[string]any{"type": "assistant", "uuid": "sa" + id, "isSidechain": true, "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "thinking aloud " + big},
			map[string]any{"type": "tool_use", "id": id, "name": "Bash", "input": map[string]any{"command": "echo " + id, "description": big}},
		}}})
		w(map[string]any{"type": "user", "uuid": "su" + id, "isSidechain": true, "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": id, "content": "out " + big, "is_error": i == 1},
		}}})
	}
	return main, Session{ID: "claude:s1", Agent: "claude", NativeID: "s1"}
}

func pageRecords(t *testing.T, page HistoryPage) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, e := range page.Events {
		var v map[string]any
		if err := json.Unmarshal(e.Data, &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

func TestClaudeHistoryNestsSubagentTools(t *testing.T) {
	for _, named := range []bool{true, false} {
		main, s := claudeWithSubagent(t, 3, 5000, named)
		page, err := readClaudeHistory(main, s, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		recs := pageRecords(t, page)
		// The Agent call, then its subagent's marker and lines, then the rest.
		var kinds []string
		for _, r := range recs {
			k := str(r["type"])
			if r["parent_tool_use_id"] != nil {
				k = "sub:" + k
			}
			kinds = append(kinds, k)
		}
		want := "user assistant workbench_subagent sub:assistant sub:user sub:assistant sub:user sub:assistant sub:user user assistant"
		if got := strings.Join(kinds, " "); got != want {
			t.Fatalf("named=%v order:\n%s\nwant\n%s", named, got, want)
		}
		marker := recs[2]
		if marker["tool_use_id"] != "toolu_A" || marker["agentId"] != "a1" || marker["earlier"] != float64(0) {
			t.Fatalf("marker %v", marker)
		}
		// Tool calls only, bounded; results bounded too.
		call := asList(obj(recs[3]["message"])["content"])
		if len(call) != 1 || obj(call[0])["type"] != "tool_use" {
			t.Fatalf("call %v", call)
		}
		if d := str(obj(obj(call[0])["input"])["description"]); len(d) > subagentInput+4 {
			t.Fatalf("input not bounded: %d", len(d))
		}
		res := obj(asList(obj(recs[4]["message"])["content"])[0])
		if len(str(res["content"])) > subagentOutput+4 || res["tool_use_id"] != "t0" {
			t.Fatalf("result %v", len(str(res["content"])))
		}
		if obj(asList(obj(recs[6]["message"])["content"])[0])["is_error"] != true {
			t.Fatal("a failed call stays failed")
		}
		if named && obj(recs[9]["toolUseResult"])["agentId"] != "a1" {
			t.Fatalf("result names the subagent: %v", recs[9])
		}
	}
}

func TestClaudeSubagentLinesAreBounded(t *testing.T) {
	main, s := claudeWithSubagent(t, subagentTools+50, 64, true)
	page, err := readClaudeHistory(main, s, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	calls, results := 0, 0
	shown := map[string]bool{}
	for _, r := range pageRecords(t, page) {
		if r["type"] == "workbench_subagent" && r["earlier"] != float64(50) {
			t.Fatalf("earlier %v", r["earlier"])
		}
		if r["parent_tool_use_id"] == nil {
			continue
		}
		for _, c := range asList(obj(r["message"])["content"]) {
			switch obj(c)["type"] {
			case "tool_use":
				calls++
				shown[str(obj(c)["id"])] = true
			case "tool_result":
				results++
				if !shown[str(obj(c)["tool_use_id"])] {
					t.Fatal("a result without its call")
				}
			}
		}
	}
	if calls != subagentTools || results != subagentTools {
		t.Fatalf("calls %d results %d", calls, results)
	}
	// The subagent's own transcript is unchanged.
	child := readClaudeSubagent(filepath.Join(claudeSubagentDir(main), "agent-a1.jsonl"), s)
	if child.ID != claudeSubagentSession("s1", "a1") || child.Title != "Look around" {
		t.Fatalf("child %v", child)
	}
	own, err := readClaudeHistory(filepath.Join(claudeSubagentDir(main), "agent-a1.jsonl"), child, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range pageRecords(t, own) {
		if r["type"] == "workbench_subagent" || r["parent_tool_use_id"] != nil {
			t.Fatal("subagent transcript gained nested lines")
		}
	}
}

// A large subagent: what a page carries stays within its budget, and
// reading it again and again leaves no memory behind.
func TestClaudeSubagentMemory(t *testing.T) {
	main, s := claudeWithSubagent(t, 1000, 8*1024, true)
	read := func() int {
		page, err := readClaudeHistory(main, s, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range page.Events {
			n += len(e.Data)
		}
		return n
	}
	if n := read(); n > subagentPageBytes+64*1024 {
		t.Fatalf("page carries %d bytes", n)
	}
	var before, after goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&before)
	for i := 0; i < 10; i++ {
		read()
	}
	goruntime.GC()
	goruntime.ReadMemStats(&after)
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > 4<<20 {
		t.Fatalf("heap grew by %d bytes after 10 reads", grew)
	}
}

func BenchmarkClaudeSubagentTail(b *testing.B) {
	for _, calls := range []int{250, 2500} {
		b.Run(fmt.Sprint(calls), func(b *testing.B) {
			main, s := claudeWithSubagent(b, calls, 8*1024, true)
			path := filepath.Join(claudeSubagentDir(main), "agent-a1.jsonl")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				budget := subagentPageBytes
				if _, _, err := claudeSubagentTail(path, "toolu_A", s, &budget); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestClaudeSubagentRequestsNameTheSubagent(t *testing.T) {
	m, _, r := claudeReady(t)
	id := r.session.ID
	// A background subagent Claude registered.
	m.receive(id, r.generation, map[string]any{"type": "system", "subtype": "task_started", "task_id": "a9", "tool_use_id": "toolu_B", "task_type": "local_agent", "is_backgrounded": true, "description": "Check the build"})
	m.receive(id, r.generation, map[string]any{"type": "control_request", "request_id": "p1", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "agent_id": "a9", "input": map[string]any{"command": "make"}}})
	approvals := eventsOf(m, id, "approval")
	sub := obj(approvals[len(approvals)-1]["subagent"])
	if sub["description"] != "Check the build" || sub["toolUseId"] != "toolu_B" || sub["session"] != claudeSubagentSession("native-1", "a9") {
		t.Fatalf("subagent %v", sub)
	}
	// The same after a reload.
	live := m.Live(id)["pending"].([]map[string]any)
	if obj(live[0]["subagent"])["description"] != "Check the build" {
		t.Fatalf("live %v", live)
	}
	// The session's own requests name none.
	m.receive(id, r.generation, map[string]any{"type": "control_request", "request_id": "p2", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": "ls"}}})
	approvals = eventsOf(m, id, "approval")
	if approvals[len(approvals)-1]["subagent"] != nil {
		t.Fatal("own request named a subagent")
	}
	// A subagent's records are its work, not a turn of the session.
	if claudeTurnSignal(map[string]any{"type": "assistant", "parent_tool_use_id": "toolu_B", "message": map[string]any{"model": "claude-haiku-5-5"}}) {
		t.Fatal("subagent record started a turn")
	}
}

func TestCodexSubagentThreadIsNotTheSessionsTurn(t *testing.T) {
	m, p, r, _ := codexReady(t)
	id := r.session.ID
	respond := responder(t, m, p, r)
	respond("thread/goal/get", map[string]any{})
	recv := func(v map[string]any) { m.receive(id, r.generation, v) }
	recv(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "th", "turn": map[string]any{"id": "t1"}}})
	recv(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "th", "turnId": "t1", "item": map[string]any{"type": "subAgentActivity", "id": "sa1", "kind": "started", "agentThreadId": "kid", "agentPath": "/root/inspect_files"}}})
	// The subagent's thread: a turn, a command with long output, a message,
	// token use, and the end of its turn.
	recv(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "kid", "turn": map[string]any{"id": "k1"}}})
	recv(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "kid", "turnId": "k1", "item": map[string]any{"type": "commandExecution", "id": "c1", "command": "ls -la", "status": "completed", "aggregatedOutput": strings.Repeat("y", 10000)}}})
	recv(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "kid", "itemId": "msg", "delta": "found it"}})
	recv(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "kid", "item": map[string]any{"type": "agentMessage", "id": "msg", "text": "found it"}}})
	recv(map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "kid", "tokenUsage": map[string]any{"total": map[string]any{"totalTokens": 99999}}}})
	recv(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "kid", "turn": map[string]any{"id": "k1", "status": "completed"}}})
	m.mu.Lock()
	turn, turnID, status := r.turn, r.turnID, r.session.Status
	m.mu.Unlock()
	if !turn || turnID != "t1" || status != "running" {
		t.Fatalf("the session's turn: %v %q %s", turn, turnID, status)
	}
	var child []map[string]any
	for _, e := range eventsOf(m, id, "native") {
		if e["workbenchChild"] != nil {
			child = append(child, e)
		}
		if strings.Contains(fmt.Sprint(e), "found it") {
			t.Fatal("the subagent's message reached the session's transcript")
		}
	}
	if len(child) != 1 || child[0]["workbenchChild"] != "kid" {
		t.Fatalf("child records %v", child)
	}
	if out := str(obj(obj(child[0]["params"])["item"])["aggregatedOutput"]); len(out) > subagentOutput+4 {
		t.Fatalf("output not bounded: %d", len(out))
	}
	// Its request is answered here and names it.
	recv(map[string]any{"id": 77, "method": "item/commandExecution/requestApproval", "params": map[string]any{"threadId": "kid", "turnId": "k2", "itemId": "c2", "command": "curl -sI https://example.com"}})
	approvals := eventsOf(m, id, "approval")
	sub := obj(approvals[len(approvals)-1]["subagent"])
	if sub["session"] != "codex:kid" || sub["description"] != "inspect_files" {
		t.Fatalf("subagent %v", sub)
	}
	if attentionNow(m, r) != "approval" {
		t.Fatalf("attention %q", attentionNow(m, r))
	}
	recv(map[string]any{"method": "serverRequest/resolved", "params": map[string]any{"threadId": "kid", "requestId": 77}})
	m.mu.Lock()
	left := len(r.pending)
	m.mu.Unlock()
	if left != 0 || attentionNow(m, r) != "" {
		t.Fatalf("resolved: %d %q", left, attentionNow(m, r))
	}
	// The session's own turn still ends it.
	recv(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "th", "turn": map[string]any{"id": "t1", "status": "completed"}}})
	m.mu.Lock()
	turn = r.turn
	m.mu.Unlock()
	if turn {
		t.Fatal("the session's turn did not end")
	}
}

func TestCodexHistoryNestsSubagentTools(t *testing.T) {
	s := Session{ID: "codex:th", Agent: "codex", NativeID: "th"}
	turns := []any{map[string]any{"id": "t1", "items": []any{
		map[string]any{"type": "userMessage", "id": "u1"},
		map[string]any{"type": "subAgentActivity", "id": "sa1", "kind": "started", "agentThreadId": "kid", "agentPath": "/root/x"},
		map[string]any{"type": "subAgentActivity", "id": "sa2", "kind": "completed", "agentThreadId": "kid", "agentPath": "/root/x"},
		map[string]any{"type": "agentMessage", "id": "a1", "text": "done"},
	}}}
	// The subagent ran subagentTools+30 commands over two pages of turns.
	var kidTurns []any
	n := 0
	for k := 0; k < 23; k++ {
		items := []any{map[string]any{"type": "agentMessage", "id": fmt.Sprint("m", k)}, map[string]any{"type": "subAgentActivity", "id": fmt.Sprint("x", k), "kind": "interacted", "agentPath": "/root"}}
		for j := 0; j < 10; j++ {
			items = append(items, map[string]any{"type": "commandExecution", "id": fmt.Sprint("c", n), "command": "echo", "aggregatedOutput": strings.Repeat("z", 4000)})
			n++
		}
		kidTurns = append([]any{map[string]any{"id": fmt.Sprint("k", k), "items": items}}, kidTurns...)
	}
	asked := 0
	request := func(_, method string, params any) (map[string]any, error) {
		p := params.(map[string]any)
		if method != "thread/turns/list" || p["threadId"] != "kid" {
			return nil, fmt.Errorf("unexpected %s %v", method, p)
		}
		asked++
		if p["cursor"] == nil {
			return map[string]any{"data": kidTurns[:10], "nextCursor": "c2"}, nil
		}
		if p["cursor"] == "c2" {
			return map[string]any{"data": kidTurns[10:20], "nextCursor": "c3"}, nil
		}
		return map[string]any{"data": kidTurns[20:]}, nil
	}
	events := withCodexSubagents(request, s, turns, codexTurnEvents(s, turns))
	var head map[string]any
	lines, at := 0, -1
	for i, e := range events {
		var v map[string]any
		_ = json.Unmarshal(e.Data, &v)
		if v["method"] == "workbench/subagent" {
			head, at = v, i
		}
		if v["workbenchChild"] == "kid" {
			lines++
			if it := obj(obj(v["params"])["item"]); str(it["type"]) != "commandExecution" || len(str(it["aggregatedOutput"])) > subagentOutput+4 {
				t.Fatalf("line %v", it["type"])
			}
		}
	}
	// After the item that started it; 200 lines; the rest counted.
	if at != 2 || lines != subagentTools {
		t.Fatalf("head at %d, %d lines", at, lines)
	}
	p := obj(head["params"])
	if p["threadId"] != "kid" || p["earlier"] != float64(0) || p["more"] != true || asked != 2 {
		t.Fatalf("head %v after %d requests", p, asked)
	}
}

func TestScanLineFindsMarksAcrossChunks(t *testing.T) {
	// Lines far longer than the reader's buffer, with marks at every offset
	// relative to the chunk boundaries.
	for shift := 0; shift < 40; shift++ {
		pad := strings.Repeat("a", 16+shift)
		line := pad + `{"type":"tool_use"}` + strings.Repeat("b", 40) + `{"type": "tool_use"}{"type": "tool_result"}` + "\n"
		r := bufio.NewReaderSize(strings.NewReader(line+"next\n"), 16)
		n, calls, result, err := scanLine(r)
		if err != nil || n != len(line) || calls != 2 || !result {
			t.Fatalf("shift %d: %d %d %v %v", shift, n, calls, result, err)
		}
		if n, calls, result, _ := scanLine(r); n != 5 || calls != 0 || result {
			t.Fatalf("shift %d: next line %d %d %v", shift, n, calls, result)
		}
	}
}

func TestResultHead(t *testing.T) {
	var h resultHead
	_ = json.Unmarshal([]byte(`"line one\nsay \"hi\" \u00e9"`), &h)
	if h != "line one\nsay \"hi\" é" {
		t.Fatalf("%q", h)
	}
	_ = json.Unmarshal([]byte(`[{"type":"text","text":"from blocks"}]`), &h)
	if h != "from blocks" {
		t.Fatalf("%q", h)
	}
	long, _ := json.Marshal(strings.Repeat("é", 5000))
	_ = json.Unmarshal(long, &h)
	if len(h) > subagentOutput+4 || !utf8.ValidString(string(h)) {
		t.Fatalf("long: %d", len(h))
	}
}

func TestCodexSubagentThreadsAreNamedByTheirPath(t *testing.T) {
	root := t.TempDir()
	codex := filepath.Join(root, "codex")
	os.MkdirAll(codex, 0o700)
	db, err := sql.Open("sqlite", filepath.Join(codex, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	spawn := func(path, nick string) string {
		b, _ := json.Marshal(map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": "main", "agent_path": path, "agent_nickname": nick}}})
		return string(b)
	}
	_, err = db.Exec(`CREATE TABLE threads(id TEXT,cwd TEXT,name TEXT,title TEXT,updated_at INTEGER,source TEXT,rollout_path TEXT,archived INTEGER);INSERT INTO threads VALUES('main','/w','Main','',10,'cli','',0),('kid','/w','','',11,?,'',0),('kid2','/w','','',12,?,'',0),('kid3','/w','Named','',13,?,'',0)`,
		spawn("/root/inspect_directory", "Planck"), spawn("", "Noether"), spawn("/root/x", "Y"))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	c := NewNativeCatalog()
	c.CodexHome = codex
	c.ClaudeHome = filepath.Join(root, "claude")
	for id, want := range map[string]string{"codex:kid": "inspect_directory", "codex:kid2": "Noether", "codex:kid3": "Named"} {
		s, ok := c.Find(id)
		if !ok || s.Title != want || s.ParentID != "codex:main" {
			t.Fatalf("%s: %q %q", id, s.Title, s.ParentID)
		}
	}
}

func TestOwnSubagentsFollowTheEngine(t *testing.T) {
	posixOnly(t)
	m, p, r, _ := codexReady(t)
	responder(t, m, p, r)("thread/goal/get", map[string]any{})
	root := t.TempDir()
	codex := filepath.Join(root, "codex")
	os.MkdirAll(filepath.Join(codex, "thread-writer-locks"), 0o700)
	db, err := sql.Open("sqlite", filepath.Join(codex, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	spawn, _ := json.Marshal(map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": "th", "agent_path": "/root/inspect"}}})
	if _, err := db.Exec(`CREATE TABLE threads(id TEXT,cwd TEXT,name TEXT,title TEXT,updated_at INTEGER,source TEXT,rollout_path TEXT,archived INTEGER);INSERT INTO threads VALUES('th','/w','Main','',10,'cli','',0),('kid','/w','','',11,?,'',0)`, string(spawn)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	m.catalog.CodexHome = codex
	m.catalog.ClaudeHome = filepath.Join(root, "claude")
	// The engine holds the subagent thread's writer lock, as Codex does.
	f, _ := os.Create(filepath.Join(codex, "thread-writer-locks", "kid.lock"))
	defer f.Close()
	release := holdLock(f)
	defer release()
	status := func() (string, bool) {
		t.Helper()
		m.catalog.mu.Lock()
		m.catalog.updated = time.Time{}
		m.catalog.mu.Unlock()
		list, err := m.NativeList()
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range list {
			if s.ID == "codex:kid" {
				return s.Status, s.ReadOnly
			}
		}
		t.Fatal("subagent not listed")
		return "", false
	}
	// Unknown to the engine: by its lock, another client.
	if st, _ := status(); st != "external" && st != "external_running" {
		t.Fatalf("before: %s", st)
	}
	recv := func(v map[string]any) { m.receive(r.session.ID, r.generation, v) }
	recv(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "kid", "turn": map[string]any{"id": "k1"}}})
	if st, ro := status(); st != "running" || !ro {
		t.Fatalf("its turn: %s %v", st, ro)
	}
	recv(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "kid", "turn": map[string]any{"id": "k1", "status": "completed"}}})
	if st, ro := status(); st != "idle" || !ro {
		t.Fatalf("after its turn: %s %v", st, ro)
	}
	// Viewing the subagent's page (no engine of its own) changes nothing.
	if err := m.Attach("codex:kid"); err != nil {
		t.Fatal(err)
	}
	if st, _ := status(); st != "idle" {
		t.Fatalf("while viewed: %s", st)
	}
	// The engine gone, the lock decides again.
	m.exited(r.session.ID, r.generation, nil)
	if st, _ := status(); st != "external" && st != "external_running" {
		t.Fatalf("after the engine: %s", st)
	}
}

func TestOwnClaudeSubagentRunsWithItsTask(t *testing.T) {
	m, _, r := claudeReady(t)
	root := t.TempDir()
	project := filepath.Join(root, "claude", "projects", "enc")
	os.MkdirAll(filepath.Join(project, "native-1", "subagents"), 0o700)
	os.WriteFile(filepath.Join(project, "native-1.jsonl"), []byte(`{"type":"user","uuid":"u","sessionId":"native-1","cwd":"/w","message":{"content":"hello"}}`+"\n"), 0o600)
	os.WriteFile(filepath.Join(project, "native-1", "subagents", "agent-a9.jsonl"), []byte(`{"type":"user","uuid":"s","isSidechain":true,"message":{"content":"work"}}`+"\n"), 0o600)
	m.catalog.ClaudeHome = filepath.Join(root, "claude")
	m.catalog.CodexHome = filepath.Join(root, "codex")
	status := func() string {
		m.catalog.mu.Lock()
		m.catalog.updated = time.Time{}
		m.catalog.mu.Unlock()
		list, _ := m.NativeList()
		for _, s := range list {
			if s.ID == claudeSubagentSession("native-1", "a9") {
				return s.Status
			}
		}
		return "(not listed)"
	}
	if st := status(); st != "sleeping" {
		t.Fatalf("before: %s", st)
	}
	m.receive(r.session.ID, r.generation, map[string]any{"type": "system", "subtype": "task_started", "task_id": "a9", "tool_use_id": "toolu_B", "task_type": "local_agent", "is_backgrounded": true, "description": "Check"})
	if st := status(); st != "running" {
		t.Fatalf("while its task runs: %s", st)
	}
	m.receive(r.session.ID, r.generation, map[string]any{"type": "system", "subtype": "task_notification", "task_id": "a9", "status": "completed", "summary": "done"})
	if st := status(); st != "sleeping" {
		t.Fatalf("after its task: %s", st)
	}
}
