package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexAutoReviewChoice(t *testing.T) {
	params := map[string]any{"sandbox": "workspace-write"}
	codexOptions(params, Options{Permissions: codexAutoReview})
	if params["permissions"] != ":workspace" || params["approvalsReviewer"] != "auto_review" || params["sandbox"] != nil {
		t.Fatalf("auto-review: %v", params)
	}
	// Codex keeps a turn's reviewer, so other choices name theirs.
	params = map[string]any{}
	codexOptions(params, Options{Permissions: ":read-only"})
	if params["permissions"] != ":read-only" || params["approvalsReviewer"] != "user" {
		t.Fatalf("read-only: %v", params)
	}
	params = map[string]any{}
	codexOptions(params, Options{})
	if _, ok := params["approvalsReviewer"]; ok {
		t.Fatalf("no choice sends no reviewer: %v", params)
	}

	workspace := map[string]any{"type": "workspaceWrite"}
	if got := codexThreadOptions(map[string]any{"sandbox": workspace, "approvalsReviewer": "auto_review"}).Permissions; got != codexAutoReview {
		t.Fatalf("thread auto-review: %q", got)
	}
	if got := codexThreadOptions(map[string]any{"sandbox": workspace, "approvalsReviewer": "user"}).Permissions; got != ":workspace" {
		t.Fatalf("thread workspace: %q", got)
	}
	if got := codexThreadOptions(map[string]any{"sandbox": map[string]any{"type": "readOnly"}, "approvalsReviewer": "auto_review"}).Permissions; got != ":read-only" {
		t.Fatalf("read-only stays read-only: %q", got)
	}
	if got := codexTurnOptions(map[string]any{"sandbox_policy": map[string]any{"type": "workspace-write"}, "approvals_reviewer": "auto_review"}).Permissions; got != codexAutoReview {
		t.Fatalf("rollout auto-review: %q", got)
	}

	list := withAutoReview([]any{
		map[string]any{"id": ":read-only", "allowed": true},
		map[string]any{"id": ":workspace", "allowed": true},
		map[string]any{"id": ":danger-full-access", "allowed": false},
	})
	ids := []string{}
	for _, p := range list {
		ids = append(ids, str(obj(p)["id"]))
	}
	if strings.Join(ids, " ") != ":read-only :workspace auto-review :danger-full-access" || obj(list[2])["allowed"] != true {
		t.Fatalf("choices: %v", list)
	}
}

func TestNewSessionsStartWithAutomaticApprovals(t *testing.T) {
	m, p := fixture(t)
	if got := m.newSessionDefaults("claude"); got.Permissions != "auto" {
		t.Fatalf("claude: %+v", got)
	}
	if got := m.newSessionDefaults("codex"); got.Permissions != codexAutoReview {
		t.Fatalf("codex: %+v", got)
	}
	// Saved defaults win over the built-in ones, field by field.
	_ = m.store.setting("defaults:claude", Options{Model: "opus", Permissions: "acceptEdits"})
	_ = m.store.setting("defaults:codex", Options{Effort: "low"})
	if got := m.newSessionDefaults("claude"); got.Permissions != "acceptEdits" || got.Model != "opus" {
		t.Fatalf("saved claude: %+v", got)
	}
	if got := m.newSessionDefaults("codex"); got.Permissions != codexAutoReview || got.Effort != "low" {
		t.Fatalf("saved codex: %+v", got)
	}

	// A Codex thread for a session in auto-review starts with that reviewer.
	s, _ := m.Create("codex", t.TempDir(), "auto")
	_ = m.store.setting("options:"+s.ID, Options{Permissions: codexAutoReview})
	_ = m.Start(s.ID)
	r := m.sessions[s.ID]
	m.receive(s.ID, r.generation, map[string]any{"id": "init", "result": map[string]any{}})
	if params := obj(sentMethod(p, "thread/start")["params"]); params["approvalsReviewer"] != "auto_review" || params["sandbox"] != "workspace-write" {
		t.Fatalf("thread/start: %v", params)
	}
}

func TestCodexFallsBackWhenAutoReviewIsRefused(t *testing.T) {
	m, p, r, _ := codexReady(t)
	id := r.session.ID
	_ = m.store.setting("options:"+id, Options{Permissions: codexAutoReview})
	if err := m.Send(id, "run the tests"); err != nil {
		t.Fatal(err)
	}
	first := sentMethod(p, "turn/start")
	if obj(first["params"])["approvalsReviewer"] != "auto_review" {
		t.Fatalf("first turn: %v", first)
	}
	m.receive(id, r.generation, map[string]any{"id": first["id"], "error": map[string]any{"code": -32600, "message": "approvals reviewer auto_review is not allowed by requirements"}})
	retry := sentMethod(p, "turn/start")
	params := obj(retry["params"])
	if retry["id"] == first["id"] || params["approvalsReviewer"] != "user" || params["permissions"] != ":workspace" {
		t.Fatalf("retry: %v", retry)
	}
	m.mu.Lock()
	status := r.session.Status
	m.mu.Unlock()
	if status == "error" || m.options(id).Permissions != ":workspace" {
		t.Fatalf("after retry: %s %+v", status, m.options(id))
	}
	got := notices(t, m, id)
	if len(got) == 0 || !strings.Contains(got[len(got)-1], "couldn't use auto-review") {
		t.Fatalf("notice: %q", got)
	}
	// Other errors stay errors.
	m.receive(id, r.generation, map[string]any{"id": retry["id"], "error": map[string]any{"message": "model overloaded"}})
	m.mu.Lock()
	status = r.session.Status
	m.mu.Unlock()
	if status != "error" {
		t.Fatalf("other error: %s", status)
	}
}

// answerClaude replies to Claude control requests: reply(subtype, request)
// returns the response, or an error text.
func answerClaude(t *testing.T, m *Manager, p *fakeProcess, r *runtime, reply func(request map[string]any) (map[string]any, string)) {
	t.Helper()
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	answered := map[string]bool{}
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(2 * time.Millisecond):
			}
			p.mu.Lock()
			var todo []map[string]any
			for _, in := range p.in {
				if key := str(in["request_id"]); in["type"] == "control_request" && key != "" && !answered[key] {
					answered[key] = true
					todo = append(todo, in)
				}
			}
			p.mu.Unlock()
			for _, in := range todo {
				result, failure := reply(obj(in["request"]))
				response := map[string]any{"request_id": in["request_id"], "subtype": "success", "response": result}
				if failure != "" {
					response = map[string]any{"request_id": in["request_id"], "subtype": "error", "error": failure}
				}
				m.receive(r.session.ID, r.generation, map[string]any{"type": "control_response", "response": response})
			}
		}
	}()
}

func TestClaudeAskWhenAutoModeIsUnavailable(t *testing.T) {
	m, p, r := claudeReady(t)
	id := r.session.ID
	_ = m.store.setting("options:"+id, Options{Permissions: "auto"})
	modes := []string{}
	answerClaude(t, m, p, r, func(req map[string]any) (map[string]any, string) {
		if req["subtype"] == "set_permission_mode" {
			modes = append(modes, str(req["mode"]))
			if req["mode"] == "auto" {
				return nil, "auto mode is unavailable for your plan"
			}
		}
		return map[string]any{}, ""
	})
	if err := m.applyOptions(id); err != nil {
		t.Fatal(err)
	}
	if strings.Join(modes, " ") != "auto default" || m.options(id).Permissions != "manual" {
		t.Fatalf("modes %v, options %+v", modes, m.options(id))
	}
	got := notices(t, m, id)
	if len(got) != 1 || !strings.Contains(got[0], "auto mode is unavailable for your plan") {
		t.Fatalf("notice: %q", got)
	}
}

func TestClaudeLeavingAutoModeOnItsOwn(t *testing.T) {
	m, _, r := claudeReady(t)
	id := r.session.ID
	_ = m.store.setting("options:"+id, Options{Permissions: "auto"})
	m.mu.Lock()
	r.applied = Options{Mode: "default", Permissions: "auto"}
	r.requestedMode = "auto"
	m.mu.Unlock()
	// Its own confirmation of auto mode changes nothing.
	m.receive(id, r.generation, map[string]any{"type": "system", "subtype": "status", "permissionMode": "auto"})
	if len(notices(t, m, id)) != 0 || m.options(id).Permissions != "auto" {
		t.Fatal("auto confirmed")
	}
	m.receive(id, r.generation, map[string]any{"type": "system", "subtype": "status", "permissionMode": "default"})
	m.mu.Lock()
	effective := r.effective.Permissions
	m.mu.Unlock()
	if m.options(id).Permissions != "manual" || effective != "manual" {
		t.Fatalf("left auto: %+v %s", m.options(id), effective)
	}
	if got := notices(t, m, id); len(got) != 1 || !strings.Contains(got[0], "left auto mode") {
		t.Fatalf("notice: %q", got)
	}
}

func TestClaudePlanReturnsToAutoMode(t *testing.T) {
	m, p, r := claudeReady(t)
	id := r.session.ID
	_ = m.store.setting("options:"+id, Options{Mode: "plan", Permissions: "auto"})
	m.receive(id, r.generation, map[string]any{"type": "control_request", "request_id": "plan-1", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "ExitPlanMode", "input": map[string]any{"plan": "1. Do it"}}})
	approvals := eventsOf(m, id, "approval")
	if len(approvals) != 1 || approvals[0]["planResume"] != "auto" {
		t.Fatalf("approval: %v", approvals)
	}
	if err := m.ReplyDecision(id, "plan-1", "allow", "", nil, "auto"); err != nil {
		t.Fatal(err)
	}
	reply := sent(p, func(v map[string]any) bool { return v["type"] == "control_response" })
	update := obj(asList(obj(obj(reply["response"])["response"])["updatedPermissions"])[0])
	if update["mode"] != "auto" {
		t.Fatalf("reply: %v", reply)
	}
	if o := m.options(id); o.Mode != "default" || o.Permissions != "auto" {
		t.Fatalf("after the plan: %+v", o)
	}
}

// A plan offers auto mode when the session ran in it before planning, even
// with no permission chosen here: Claude Code then resumed the mode its
// record holds (ADR 0002).
func TestClaudePlanOffersTheAutoModeItRanIn(t *testing.T) {
	for _, c := range []struct {
		name   string
		stored Options
		record []string
		want   bool
	}{
		{"auto in the record", Options{Mode: "plan"}, []string{"auto"}, true},
		{"edits in the record", Options{Mode: "plan"}, []string{"acceptEdits"}, false},
		{"a choice made here wins", Options{Mode: "plan", Permissions: "manual"}, []string{"auto"}, false},
		{"no choice, no record: the default", Options{Mode: "plan"}, nil, true},
		{"plan in the record is passed over", Options{Mode: "plan"}, []string{"auto", "plan"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, _, r := claudeReady(t)
			id := r.session.ID
			_ = m.store.setting("options:"+id, c.stored)
			if c.record != nil {
				var b strings.Builder
				for _, mode := range c.record {
					b.WriteString(`{"type":"user","message":{"role":"user","content":"x"}}` + "\n")
					fmt.Fprintf(&b, `{"type":"permission-mode","permissionMode":%q}`+"\n", mode)
				}
				path := filepath.Join(t.TempDir(), "s.jsonl")
				if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
					t.Fatal(err)
				}
				m.catalog.mu.Lock()
				m.catalog.paths[id] = path
				m.catalog.mu.Unlock()
			}
			m.receive(id, r.generation, map[string]any{"type": "control_request", "request_id": "plan-1", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "ExitPlanMode", "input": map[string]any{"plan": "1. Do it"}}})
			approvals := eventsOf(m, id, "approval")
			if got := len(approvals) == 1 && approvals[0]["planResume"] == "auto"; got != c.want {
				t.Fatalf("auto offered %v, want %v (%v)", got, c.want, approvals)
			}
		})
	}
}

func TestClaudeRecordedMode(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if got := claudeRecordedMode(write("a.jsonl", `{"type":"permission-mode","permissionMode":"auto"}`+"\n"+`not json "permission-mode"`+"\n")); got != "auto" {
		t.Fatalf("plain: %q", got)
	}
	// Only the end of a large record is read: a mode written long before
	// is not found.
	old := `{"type":"permission-mode","permissionMode":"auto"}` + "\n" + strings.Repeat(`{"type":"user"}`+"\n", 300<<10/16)
	if got := claudeRecordedMode(write("b.jsonl", old)); got != "" {
		t.Fatalf("beyond the tail: %q", got)
	}
	if got := claudeRecordedMode(write("c.jsonl", old+`{"type":"permission-mode","permissionMode":"acceptEdits"}`+"\n")); got != "acceptEdits" {
		t.Fatalf("in the tail: %q", got)
	}
	if got := claudeRecordedMode(filepath.Join(dir, "missing.jsonl")); got != "" {
		t.Fatalf("missing: %q", got)
	}
}
