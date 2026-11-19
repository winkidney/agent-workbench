package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/winkidney/agent-workbench/internal/workbench"
)

// A workbench that answers the agents' commands, and a data directory that
// points at it.
func agentServer(t *testing.T, handle func(r *http.Request, body map[string]any) any) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Workbench-Agent") != "tok" {
			http.Error(w, "forbidden", 403)
			return
		}
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		out := handle(r, body)
		if err, ok := out.(error); ok {
			http.Error(w, err.Error(), 400)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	info, _ := json.Marshal(workbench.ServerInfo{PID: os.Getpid(), Listen: strings.TrimPrefix(srv.URL, "http://")})
	os.WriteFile(filepath.Join(dir, "server.json"), info, 0o600)
	os.WriteFile(filepath.Join(dir, "agent-token"), []byte("tok\n"), 0o600)
	return dir
}

func TestRoutineAddSendsWhatItWasGiven(t *testing.T) {
	var got map[string]any
	var path string
	dir := agentServer(t, func(r *http.Request, body map[string]any) any {
		got, path = body, r.Method+" "+r.URL.Path
		return workbench.Routine{ID: "r1", Name: "Follow up", When: workbench.When{Kind: "daily", Time: "09:00", Days: []int{1, 5}}, Target: &workbench.RoutineTarget{Kind: "assistant", Assistant: "main"}}
	})
	t.Setenv(workbench.EnvDataDir, dir)
	t.Setenv(workbench.EnvSession, "claude:s")
	out := captureStdout(t, func() error {
		return agentCommand("routine", []string{"add", "--daily", "09:00", "--days", "mon,fri", "--message", "Check the report", "--name", "Follow up"})
	})
	if path != "POST /api/local/routines" || got["caller"] != "claude:s" || got["daily"] != "09:00" || got["to"] != "this" || got["message"] != "Check the report" {
		t.Fatalf("%s %v", path, got)
	}
	if days, _ := got["days"].([]any); len(days) != 2 || days[1] != "fri" {
		t.Fatalf("days: %v", got["days"])
	}
	if !strings.Contains(out, "Added routine r1: Follow up, Mon, Fri at 09:00 to the assistant main") {
		t.Fatalf("out: %q", out)
	}

	// --in is a once routine at that time from now.
	before := time.Now()
	captureStdout(t, func() error {
		return agentCommand("routine", []string{"add", "--in", "2h", "--message", "Again"})
	})
	at, err := time.ParseInLocation("2006-01-02 15:04", got["at"].(string), time.Local)
	if err != nil || at.Sub(before) < 119*time.Minute || at.Sub(before) > 121*time.Minute {
		t.Fatalf("at: %v %v", got["at"], err)
	}
	if err := agentCommand("routine", []string{"add", "--in", "soon", "--message", "x"}); err == nil {
		t.Fatal("--in soon was accepted")
	}
	if err := agentCommand("routine", []string{"add", "--every", "1h", "--message", "x", "stray"}); err == nil {
		t.Fatal("a stray argument was accepted")
	}
}

func TestAssistantSendAndRoutineRm(t *testing.T) {
	var calls []string
	var text any
	dir := agentServer(t, func(r *http.Request, body map[string]any) any {
		calls = append(calls, r.Method+" "+r.URL.Path)
		text = body["text"]
		return workbench.Session{ID: "claude:new"}
	})
	t.Setenv(workbench.EnvDataDir, dir)
	out := captureStdout(t, func() error {
		// Flags may come after the arguments.
		return agentCommand("assistant", []string{"send", "report", "Draft", "it", "--data-dir", dir})
	})
	if text != "Draft it" || !strings.Contains(out, "Sent to report (session claude:new)") {
		t.Fatalf("%v %q", text, out)
	}
	captureStdout(t, func() error { return agentCommand("routine", []string{"rm", "r1"}) })
	if strings.Join(calls, ",") != "POST /api/local/assistants/report/send,DELETE /api/local/routines/r1" {
		t.Fatalf("calls: %v", calls)
	}
	// Not running: says so.
	t.Setenv(workbench.EnvDataDir, t.TempDir())
	if err := agentCommand("routine", []string{"list"}); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("not running: %v", err)
	}
}

func captureStdout(t *testing.T, f func() error) string {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	err := f()
	os.Stdout = old
	w.Close()
	b, _ := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
