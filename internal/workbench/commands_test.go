package workbench

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCatalogMergesNativeAndWorkbenchCommands(t *testing.T) {
	meta := metadata{
		Commands: []any{
			map[string]any{"name": "compact", "argumentHint": "<instructions>", "description": "Free up context"},
			map[string]any{"name": "model", "description": "native model picker"},
			map[string]any{"name": "doctor", "description": "terminal only"},
			map[string]any{"name": "__remote-workflow"},
			map[string]any{"name": "dataviz", "description": "Charts skill"},
			map[string]any{"name": "clear", "aliases": []any{"reset", "new"}},
			// Plugin skills are listed by their bare name.
			map[string]any{"name": "docx", "description": "Word files (claude.ai sync)"},
			map[string]any{"name": "lint", "description": "two plugins have it"},
		},
		Skills: []any{
			map[string]any{"name": "dataviz"},
			map[string]any{"name": "anthropic-skills:docx"},
			map[string]any{"name": "a:lint"},
			map[string]any{"name": "b:lint"},
			map[string]any{"name": "x:unlisted"},
		},
		TerminalCommands: []string{"doctor"},
	}
	cat := buildCatalog("claude", meta)
	names := map[string]Command{}
	for _, c := range cat.Commands {
		if _, dup := names[c.Name]; dup {
			t.Fatalf("duplicate %s", c.Name)
		}
		names[c.Name] = c
	}
	if names["model"].Source != "workbench" || names["compact"].Source != "native" || names["plan"].Source != "workbench" {
		t.Fatalf("sources: %+v", names)
	}
	if _, ok := names["doctor"]; ok {
		t.Fatal("terminal-only command offered")
	}
	if _, ok := names["__remote-workflow"]; ok {
		t.Fatal("internal command offered")
	}
	if names["dataviz"].Kind != "skill" || cat.Skills[0].Description != "Charts skill" {
		t.Fatalf("skills: %+v %+v", names["dataviz"], cat.Skills)
	}
	// A plugin skill takes the name the CLI runs it by; a bare name two
	// plugins share stays a command.
	if names["docx"].Kind != "skill" || names["lint"].Kind != "command" {
		t.Fatalf("plugin skills: %+v %+v", names["docx"], names["lint"])
	}
	var skills []string
	for _, s := range cat.Skills {
		skills = append(skills, s.Name+"="+s.Description)
	}
	if got := strings.Join(skills, ","); got != "dataviz=Charts skill,docx=Word files (claude.ai sync),a:lint=,b:lint=,x:unlisted=" {
		t.Fatalf("skill list: %s", got)
	}
	if strings.Join(names["clear"].Aliases, ",") != "reset,new" {
		t.Fatalf("aliases: %+v", names["clear"])
	}
	if _, ok := names["review"]; ok {
		t.Fatal("Codex-only command offered to Claude")
	}

	codex := buildCatalog("codex", metadata{Skills: []any{map[string]any{"name": "pdf", "description": "PDFs", "path": "/s/pdf/SKILL.md"}}})
	got := map[string]bool{}
	for _, c := range codex.Commands {
		got[c.Name] = true
	}
	for _, want := range []string{"plan", "default", "compact", "review", "goal", "fork", "mcp", "diff", "new", "status", "skills", "quit"} {
		if !got[want] {
			t.Fatalf("codex catalog lacks /%s", want)
		}
	}
	if len(codex.Skills) != 1 || codex.Skills[0].Path != "/s/pdf/SKILL.md" {
		t.Fatalf("codex skills: %+v", codex.Skills)
	}
}

func TestPrepareTextFollowsEachCLI(t *testing.T) {
	posixOnly(t)
	skills := []Skill{{Name: "dataviz", Path: "/a"}, {Name: "pdf", Path: "/b"}}
	for _, c := range []struct {
		agent, in, out string
		attach         []string
		err            string
	}{
		{"claude", "$dataviz chart it", "/dataviz chart it", nil, ""},
		{"claude", "$nope x", "$nope x", nil, ""},
		{"claude", "/compact keep tests", "/compact keep tests", nil, ""},
		{"claude", "use $dataviz later", "use $dataviz later", nil, ""},
		{"codex", "use $dataviz and $pdf, then $dataviz again", "use $dataviz and $pdf, then $dataviz again", []string{"dataviz", "pdf"}, ""},
		{"codex", "price is $5", "price is $5", nil, ""},
		{"codex", "/theme dark", "", nil, "unknown command /theme"},
		{"codex", "//etc/hosts is a path", "/etc/hosts is a path", nil, ""},
	} {
		out, attach, err := prepareText(c.agent, c.in, skills)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Fatalf("%q: %v", c.in, err)
			}
			continue
		}
		var names []string
		for _, s := range attach {
			names = append(names, s.Name)
		}
		if err != nil || out != c.out || strings.Join(names, ",") != strings.Join(c.attach, ",") {
			t.Fatalf("%s %q: %q %v %v", c.agent, c.in, out, names, err)
		}
	}
}

// codexReady starts a Codex session on the fake process and answers RPCs.
func codexReady(t *testing.T) (*Manager, *fakeProcess, *runtime, func(method string, result map[string]any)) {
	t.Helper()
	m, p := fixture(t)
	s, _ := m.Create("codex", t.TempDir(), "cmd")
	_ = m.Start(s.ID)
	r := m.sessions[s.ID]
	m.receive(s.ID, r.generation, map[string]any{"id": "init", "result": map[string]any{}})
	m.receive(s.ID, r.generation, map[string]any{"id": "thread", "result": map[string]any{"thread": map[string]any{"id": "th"}}})
	answer := func(method string, result map[string]any) {
		go func() {
			for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
				p.mu.Lock()
				var id any
				for _, in := range p.in {
					if in["method"] == method {
						id = in["id"]
					}
				}
				p.mu.Unlock()
				if id != nil {
					m.receive(r.session.ID, r.generation, map[string]any{"id": id, "result": result})
					return
				}
			}
		}()
	}
	return m, p, r, answer
}

func sentMethod(p *fakeProcess, method string) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.in) - 1; i >= 0; i-- {
		if p.in[i]["method"] == method {
			return p.in[i]
		}
	}
	return nil
}

func notices(t *testing.T, m *Manager, id string) []string {
	t.Helper()
	events, _ := m.store.Events(id, 0, 1000)
	var out []string
	for _, e := range events {
		if e.Kind == "notice" {
			var v map[string]any
			json.Unmarshal(e.Data, &v)
			out = append(out, str(v["text"]))
		}
	}
	return out
}

func TestCodexCommandsDriveTheAppServer(t *testing.T) {
	m, p, r, answer := codexReady(t)
	id := r.session.ID

	if res, err := m.RunCommand(id, "plan", ""); err != nil || res.Options.Mode != "plan" || m.options(id).Mode != "plan" {
		t.Fatalf("/plan: %+v %v", res, err)
	}
	if _, err := m.RunCommand(id, "fast", "on"); err != nil || m.options(id).Speed != "priority" {
		t.Fatalf("/fast: %v %+v", err, m.options(id))
	}

	answer("thread/compact/start", map[string]any{})
	if err := runCmd(m, id, "compact", ""); err != nil {
		t.Fatal(err)
	}
	if sentMethod(p, "thread/compact/start") == nil || !r.turn || r.session.Status != "running" {
		t.Fatalf("compact turn: %v %s", r.turn, r.session.Status)
	}
	m.receive(id, r.generation, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})

	answer("review/start", map[string]any{"turn": map[string]any{"id": "t2"}})
	if err := runCmd(m, id, "review", "check the error paths"); err != nil {
		t.Fatal(err)
	}
	target := obj(obj(sentMethod(p, "review/start")["params"])["target"])
	if target["type"] != "custom" || target["instructions"] != "check the error paths" {
		t.Fatalf("review target: %v", target)
	}
	m.receive(id, r.generation, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})

	answer("thread/goal/set", map[string]any{})
	if err := runCmd(m, id, "goal", "ship it"); err != nil {
		t.Fatal(err)
	}
	if params := obj(sentMethod(p, "thread/goal/set")["params"]); params["objective"] != "ship it" || params["origin"] != "user" {
		t.Fatalf("goal: %v", params)
	}
	answer("mcpServerStatus/list", map[string]any{"data": []any{map[string]any{"name": "docs", "runtimeStatus": "ready", "tools": map[string]any{"a": 1, "b": 2}}}})
	if err := runCmd(m, id, "mcp", ""); err != nil {
		t.Fatal(err)
	}
	got := notices(t, m, id)
	if len(got) != 2 || got[0] != "Goal set: ship it" || got[1] != "MCP servers:\ndocs · ready · 2 tools" {
		t.Fatalf("notices: %q", got)
	}
	// "/plan task" switches the mode and sends the task as a turn.
	if res, err := m.RunCommand(id, "default", "go on"); err != nil || !res.Sent || m.options(id).Mode != "default" {
		t.Fatalf("/default task: %+v %v", res, err)
	}
	turn := sentMethod(p, "turn/start")
	if turn == nil || str(obj(asList(obj(turn["params"])["input"])[0])["text"]) != "go on" {
		t.Fatalf("task not sent: %v", turn)
	}
	m.receive(id, r.generation, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})

	answer("thread/fork", map[string]any{"thread": map[string]any{"id": "forked"}})
	if res, err := m.RunCommand(id, "fork", ""); err != nil || res.Session != "codex:forked" {
		t.Fatalf("fork: %+v %v", res, err)
	}
	if fork := obj(sentMethod(p, "thread/fork")["params"]); fork["excludeTurns"] != true {
		t.Fatalf("fork asked for the full history: %v", fork)
	}
	// The fork stays loaded in this engine, so the engine goes to sleep.
	m.mu.Lock()
	asleep := r.proc == nil
	m.mu.Unlock()
	if !asleep {
		t.Fatal("source engine still holds the fork")
	}
	if _, err := m.RunCommand(id, "theme", "dark"); err == nil {
		t.Fatal("unknown command accepted")
	}
}

func TestCodexSendAttachesSkills(t *testing.T) {
	m, p, r, _ := codexReady(t)
	// A warm capability cache, as after the page listed skills.
	m.metaMu.Lock()
	m.metadata = map[string]metadata{"codex:" + r.session.Cwd: {Skills: []any{map[string]any{"name": "pdf", "path": "/s/pdf/SKILL.md"}}, at: time.Now()}}
	m.metaMu.Unlock()
	if err := m.Send(r.session.ID, "fill the form with $pdf please"); err != nil {
		t.Fatal(err)
	}
	input := asList(obj(sentMethod(p, "turn/start")["params"])["input"])
	if len(input) != 2 || obj(input[0])["type"] != "skill" || obj(input[0])["path"] != "/s/pdf/SKILL.md" || obj(input[1])["text"] != "fill the form with $pdf please" {
		t.Fatalf("input: %v", input)
	}
	m.receive(r.session.ID, r.generation, map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}})
	if err := m.Send(r.session.ID, "/theme dark"); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unknown slash sent: %v", err)
	}
}

func TestCommandsRefuseReadOnlySessions(t *testing.T) {
	m, _, r, _ := codexReady(t)
	m.mu.Lock()
	r.session.ReadOnly = true
	m.mu.Unlock()
	if _, err := m.RunCommand(r.session.ID, "plan", ""); err == nil {
		t.Fatal("read-only session changed")
	}
}

// runCmd runs a command whose result the test does not need.
func runCmd(m *Manager, id, name, args string) error {
	_, err := m.RunCommand(id, name, args)
	return err
}

func TestClaudeHistoryShowsLocalCommandOutput(t *testing.T) {
	path := t.TempDir() + "/s.jsonl"
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"message":{"role":"user","content":"<command-name>/context</command-name>"}}`,
		`{"type":"system","subtype":"local_command","uuid":"c1","parentUuid":"u1","content":"<local-command-stdout>## Context Usage</local-command-stdout>"}`,
		`{"type":"system","subtype":"api_error","uuid":"e1","parentUuid":"c1","content":"hidden"}`,
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	page, err := readClaudeHistory(path, Session{ID: "claude:x"}, "", 10)
	if err != nil || len(page.Events) != 2 {
		t.Fatalf("%d events: %v", len(page.Events), err)
	}
	var v map[string]any
	json.Unmarshal(page.Events[1].Data, &v)
	if v["subtype"] != "local_command" || !strings.Contains(str(v["content"]), "Context Usage") {
		t.Fatalf("local command record: %v", v)
	}
}
