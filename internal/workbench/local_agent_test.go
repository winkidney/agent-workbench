package workbench

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

func TestAgentAPINeedsTheTokenFromThisMachine(t *testing.T) {
	m, _ := fixture(t)
	s := NewServer(m, nil, "agent.localhost:17880", "")
	if err := s.writeAgentToken(); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(m.store.dir, agentTokenFile))
	if err != nil || string(saved) != s.agentToken {
		t.Fatalf("token file: %q %v", saved, err)
	}
	if info, _ := os.Stat(filepath.Join(m.store.dir, agentTokenFile)); info.Mode().Perm()&0o077 != 0 && goruntime.GOOS != "windows" {
		t.Fatalf("token file mode %v", info.Mode())
	}
	try := func(remote, tok string, header ...string) int {
		r := httptest.NewRequest("GET", "http://127.0.0.1:17880/api/local/routines", nil)
		r.RemoteAddr = remote
		if tok != "" {
			r.Header.Set(agentHeader, tok)
		}
		for i := 0; i+1 < len(header); i += 2 {
			r.Header.Set(header[i], header[i+1])
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	for name, code := range map[string]int{
		"another computer": try("203.0.113.9:4000", s.agentToken),
		"no token":         try("127.0.0.1:4000", ""),
		"wrong token":      try("127.0.0.1:4000", "wrong"),
		"the stop token":   try("127.0.0.1:4000", s.stopToken),
		"through a tunnel": try("127.0.0.1:4000", s.agentToken, "Cf-Connecting-Ip", "203.0.113.9"),
		"through a proxy":  try("127.0.0.1:4000", s.agentToken, "X-Forwarded-For", "203.0.113.9"),
		"from a page":      try("127.0.0.1:4000", s.agentToken, "Origin", "http://127.0.0.1:17880"),
	} {
		if code != 403 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if code := try("127.0.0.1:4000", s.agentToken); code != 200 {
		t.Fatalf("the command: %d", code)
	}
}

func TestAgentRoutinesAreLimited(t *testing.T) {
	m, _ := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root})
	writeToml(t, filepath.Join(root, "main", "assistant.toml"), "name = \"Main\"\nagent = \"claude\"\nsessions = \"continuous\"")
	writeToml(t, filepath.Join(root, "report", "assistant.toml"), "name = \"Report\"\nagent = \"claude\"")
	work := Session{ID: "claude:w", Title: "Work", Cwd: "/work"}
	topic := Session{ID: "claude:t", Title: "Today", Cwd: filepath.Join(root, "main")}
	task := Session{ID: "claude:r", Title: "Draft", Cwd: filepath.Join(root, "report")}
	m.as.lookup = func(id string) (Session, bool) {
		for _, s := range []Session{work, topic, task} {
			if s.ID == id {
				return s, true
			}
		}
		return Session{}, false
	}
	add := func(req AgentRoutineRequest) (Routine, error) {
		if req.Message == "" {
			req.Message = "check"
		}
		return m.AgentRoutine(req)
	}
	if _, err := add(AgentRoutineRequest{Caller: "claude:w", Every: "10m"}); err == nil {
		t.Fatal("every 10 minutes was accepted")
	}
	if _, err := add(AgentRoutineRequest{Caller: "claude:w", Every: "1h", Message: " "}); err == nil {
		t.Fatal("an empty message was accepted")
	}
	if _, err := add(AgentRoutineRequest{Every: "1h", To: "this"}); err == nil {
		t.Fatal("this, outside a session, was accepted")
	}
	if _, err := add(AgentRoutineRequest{Caller: "claude:w", Every: "1h", To: "nobody"}); err == nil {
		t.Fatal("an unknown target was accepted")
	}
	// this: the session; in a continuous assistant's folder, the assistant;
	// in a per-task one, the task's session.
	for caller, want := range map[string]RoutineTarget{
		"claude:w": {Kind: "session", Session: "claude:w"},
		"claude:t": {Kind: "assistant", Assistant: "main"},
		"claude:r": {Kind: "session", Session: "claude:r"},
	} {
		v, err := add(AgentRoutineRequest{Caller: caller, Daily: "09:00", Days: []string{"mon"}, To: "this", Name: "Follow up"})
		if err != nil {
			t.Fatal(err)
		}
		if *v.Target != want || v.CreatedBy != "agent:"+caller || v.Command != nil || v.When.Kind != "daily" {
			t.Fatalf("%s: %+v %+v", caller, v, v.Target)
		}
	}
	if v, err := add(AgentRoutineRequest{Caller: "claude:w", At: "2030-01-02 09:00", To: "report"}); err != nil || v.Target.Kind != "assistant" || v.Target.Assistant != "report" {
		t.Fatalf("to an assistant: %+v %v", v, err)
	}
	// At most agentRoutinesMax switched on per session.
	for i := 0; i < agentRoutinesMax-2; i++ {
		if _, err := add(AgentRoutineRequest{Caller: "claude:w", Every: "1h"}); err != nil {
			t.Fatal(i, err)
		}
	}
	extra, err := add(AgentRoutineRequest{Caller: "claude:w", Every: "1h"})
	if err == nil || !strings.Contains(err.Error(), "remove one") {
		t.Fatalf("over the limit: %+v %v", extra, err)
	}
	// Another session has its own.
	if _, err := add(AgentRoutineRequest{Caller: "claude:r", Every: "1h"}); err != nil {
		t.Fatal(err)
	}

	// rm removes an agent's routine, never the user's.
	mine, _ := m.SaveRoutine(Routine{Name: "Mine", Message: "x", Enabled: true, When: When{Kind: "interval", Minutes: 60}, Target: &RoutineTarget{Kind: "session", Session: "claude:w"}})
	if err := m.RemoveAgentRoutine(mine.ID); err == nil {
		t.Fatal("removed the user's routine")
	}
	list, _ := m.store.routines()
	for _, v := range list {
		if strings.HasPrefix(v.CreatedBy, "agent:") {
			if err := m.RemoveAgentRoutine(v.ID); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if err := m.RemoveAgentRoutine("nope"); err == nil {
		t.Fatal("removed nothing without saying so")
	}
}

func TestAgentSendLabelsTheMessage(t *testing.T) {
	m, p := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root})
	writeToml(t, filepath.Join(root, "report", "assistant.toml"), "name = \"Report\"\nagent = \"codex\"\nsessions = \"continuous\"")
	writeToml(t, filepath.Join(root, "main", "assistant.toml"), "name = \"Main\"\nagent = \"claude\"\nsessions = \"continuous\"")
	// The report assistant's ongoing topic, its engine started.
	topic, _ := m.Create("codex", filepath.Join(root, "report"), "r")
	_ = m.Start(topic.ID)
	gen := m.sessions[topic.ID].generation
	m.receive(topic.ID, gen, map[string]any{"id": "init", "result": map[string]any{}})
	m.receive(topic.ID, gen, map[string]any{"id": "thread", "result": map[string]any{"thread": map[string]any{"id": "n"}}})
	m.saveTopic("report", assistantState{Current: "codex:n"})
	from := Session{ID: "claude:t", Title: "Today", Cwd: filepath.Join(root, "main")}
	m.as.lookup = func(id string) (Session, bool) {
		if id == "codex:n" {
			return Session{ID: id, Status: "idle", Updated: time.Now().UTC().Format(time.RFC3339)}, true
		}
		return from, id == from.ID
	}
	if _, err := m.AgentSend("claude:t", "report", " "); err == nil {
		t.Fatal("an empty message was accepted")
	}
	if _, err := m.AgentSend("claude:t", "nobody", "hi"); err == nil {
		t.Fatal("an unknown assistant was accepted")
	}
	if _, err := m.AgentSend("claude:t", "report", "Draft the weekly report"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	var text string
	for _, in := range p.in {
		if in["method"] == "turn/start" {
			text = str(obj(obj(in["params"])["input"].([]any)[0])["text"])
		}
	}
	p.mu.Unlock()
	if text != "[From another session: Main: Today]\n\nDraft the weekly report" {
		t.Fatalf("sent %q", text)
	}
}

func TestEnginesLearnWhereTheWorkbenchIs(t *testing.T) {
	m, _ := fixture(t)
	env := m.engineEnv([]string{"PATH=/bin", EnvSession + "=old"}, "claude:x")
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	if got[EnvSession] != "claude:x" || got[EnvDataDir] != m.store.dir || got[EnvBin] == "" || got["PATH"] != "/bin" {
		t.Fatalf("env: %v", got)
	}
}

func TestAgentsMakeAssistants(t *testing.T) {
	m, _ := fixture(t)
	root := filepath.Join(t.TempDir(), "assistants")
	if err := m.SetUpAssistants(root, "claude", ""); err != nil {
		t.Fatal(err)
	}
	a, err := m.AgentNewAssistant("Weekly report", "", "codex", "")
	if err != nil || a.Dir != "weekly-report" || a.Agent != "codex" || a.Sessions != "per-task" {
		t.Fatalf("new: %+v %v", a, err)
	}
	if _, err := m.AgentNewAssistant("Again", "weekly-report", "codex", ""); err == nil {
		t.Fatal("replaced a folder")
	}
	// Its fetched data stays out of git; the personal assistant's records do not.
	if b, err := os.ReadFile(filepath.Join(a.Path, ".gitignore")); err != nil || !strings.Contains(string(b), "raw/") {
		t.Fatalf(".gitignore: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(m.assistantsRoot(), "main", ".gitignore")); err == nil {
		t.Fatal("main ignores its raw/sessions records")
	}
}

func TestAssistantsStartSessionsTheUserAskedFor(t *testing.T) {
	m, p := fixture(t)
	root := t.TempDir()
	_ = m.store.setting("assistants", assistantsSetting{Root: root})
	writeToml(t, filepath.Join(root, "main", "assistant.toml"), "name = \"Moss\"\nagent = \"codex\"\nsessions = \"continuous\"")
	project := t.TempDir()
	moss := Session{ID: "codex:moss", Agent: "codex", Title: "Today", Cwd: filepath.Join(root, "main")}
	other := Session{ID: "codex:other", Agent: "codex", Title: "Work", Cwd: project}
	m.as.lookup = func(id string) (Session, bool) {
		switch id {
		case moss.ID:
			return moss, true
		case other.ID:
			return other, true
		}
		return Session{}, false
	}
	// Only an assistant's session starts one: not a terminal, not a session
	// elsewhere.
	for _, caller := range []string{"", other.ID} {
		if _, err := m.AgentNewSession(caller, project, "", "", "Fix the login bug"); err == nil {
			t.Fatalf("started a session for %q", caller)
		}
	}
	if _, err := m.AgentNewSession(moss.ID, project, "", "", " "); err == nil {
		t.Fatal("an empty message was accepted")
	}
	if _, err := m.AgentNewSession(moss.ID, filepath.Join(project, "missing"), "", "", "hi"); err == nil {
		t.Fatal("a missing folder was accepted")
	}
	// The engine answers as the session starts, so the native id arrives.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			time.Sleep(5 * time.Millisecond)
			m.mu.Lock()
			var r *runtime
			for _, x := range m.sessions {
				if x.session.Cwd != "" && x.proc != nil && x.session.NativeID == "" {
					r = x
				}
			}
			m.mu.Unlock()
			if r != nil {
				m.receive(r.session.ID, r.generation, map[string]any{"id": "init", "result": map[string]any{}})
				m.receive(r.session.ID, r.generation, map[string]any{"id": "thread", "result": map[string]any{"thread": map[string]any{"id": "new"}}})
				return
			}
		}
	}()
	s, err := m.AgentNewSession(moss.ID, project, "", "Login bug", "Fix the login bug")
	<-done
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(project)
	if s.Cwd != real || s.Agent != "codex" || s.Title != "Login bug" || s.ParentID != "" {
		t.Fatalf("session: %+v", s)
	}
	p.mu.Lock()
	var text string
	for _, in := range p.in {
		if in["method"] == "turn/start" {
			text = str(obj(obj(in["params"])["input"].([]any)[0])["text"])
		}
	}
	p.mu.Unlock()
	if text != "[From another session: Moss: Today]\n\nFix the login bug" {
		t.Fatalf("sent %q", text)
	}
}
