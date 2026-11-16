package workbench

import (
	"net/url"
	"testing"
	"testing/fstest"
)

// An answer to Claude's question can carry a note: the reply passes it on
// as AskUserQuestion's annotations, next to the answers. Without notes there
// are no annotations.
func TestClaudeAnswerNotesBecomeAnnotations(t *testing.T) {
	for _, c := range []struct {
		name, body string
		notes      bool
	}{
		{"with a note", `{"id":"q1","decision":"allow","answers":{"Which DB?":"Postgres"},"annotations":{"Which DB?":{"notes":"we already run it"}}}`, true},
		{"without", `{"id":"q1","decision":"allow","answers":{"Which DB?":"Postgres"}}`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, p := fixture(t)
			sess, _ := m.Create("claude", t.TempDir(), "q")
			_ = m.Start(sess.ID)
			r := m.sessions[sess.ID]
			m.receive(sess.ID, r.generation, map[string]any{"type": "control_request", "request_id": "q1", "request": map[string]any{"subtype": "can_use_tool", "tool_name": "AskUserQuestion", "input": map[string]any{"questions": []any{map[string]any{"question": "Which DB?", "options": []any{map[string]any{"label": "Postgres"}, map[string]any{"label": "SQLite"}}}}}}})
			s := NewServer(m, fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}, "agent.localhost:17880", "")
			w := request(t, s, s.Host, "POST", "/api/sessions/"+url.PathEscape(r.session.ID)+"/reply", c.body, nil)
			if w.Code != 200 {
				t.Fatalf("reply: %d %s", w.Code, w.Body.String())
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			last := p.in[len(p.in)-1]
			input := obj(obj(obj(last["response"])["response"])["updatedInput"])
			if obj(input["answers"])["Which DB?"] != "Postgres" {
				t.Fatalf("answers: %v", input)
			}
			notes := obj(obj(input["annotations"])["Which DB?"])["notes"]
			if c.notes && notes != "we already run it" {
				t.Fatalf("annotations: %v", input["annotations"])
			}
			if !c.notes && input["annotations"] != nil {
				t.Fatalf("unexpected annotations: %v", input["annotations"])
			}
		})
	}
}
