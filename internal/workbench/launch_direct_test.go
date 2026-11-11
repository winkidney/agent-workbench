package workbench

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"
)

// fakeAgent builds testdata/fakeagent as claude in a directory put first
// on PATH, so DirectLaunch finds it as the real agent.
func fakeAgent(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "claude"
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", filepath.Join(dir, name), "./testdata/fakeagent").CombinedOutput()
	if err != nil {
		t.Fatalf("build fake agent: %v\n%s", err, out)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// launched runs the fake agent with DirectLaunch and collects its lines.
func launched(t *testing.T) (*directProcess, chan map[string]any, chan error) {
	t.Helper()
	dir := fakeAgent(t)
	lines := make(chan map[string]any, 16)
	exits := make(chan error, 1)
	proc, err := DirectLaunch(Session{ID: "s1", Agent: "claude", Cwd: dir, NativeID: "native-1"}, func(v map[string]any) {
		if v["type"] != "stderr" {
			lines <- v
		}
	}, func(err error) { exits <- err })
	if err != nil {
		t.Fatal(err)
	}
	p := proc.(*directProcess)
	t.Cleanup(func() { _ = p.Stop() })
	return p, lines, exits
}

func next(t *testing.T, lines chan map[string]any) map[string]any {
	t.Helper()
	select {
	case v := <-lines:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("no output from the engine")
		return nil
	}
}

func gone(pid int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func TestDirectLaunchRunsAndStopsTheWholeTree(t *testing.T) {
	p, lines, exits := launched(t)
	if err := p.Input(map[string]any{"hello": 1.0}); err != nil {
		t.Fatal(err)
	}
	echo := next(t, lines)
	args, _ := echo["args"].([]any)
	if echo["type"] != "echo" || len(args) == 0 || args[0] != "-p" || args[len(args)-1] != "native-1" {
		t.Fatalf("echo: %v", echo)
	}
	p.MarkReady()
	if p.Busy() {
		t.Fatal("busy before any work")
	}
	_ = p.Input(map[string]any{"spawn": true})
	spawned := next(t, lines)
	pid := int(spawned["pid"].(float64))
	if !p.Busy() {
		t.Fatal("a new child is work")
	}
	if !p.Owns(pid) || p.Owns(os.Getpid()) {
		t.Fatalf("owns: child %v, test %v", p.Owns(pid), p.Owns(os.Getpid()))
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exits:
	case <-time.After(5 * time.Second):
		t.Fatal("no exit after Stop")
	}
	if !gone(pid) {
		t.Fatal("the engine's child outlived Stop")
	}
}

func TestDirectEngineExitEndsWhatItLeft(t *testing.T) {
	p, lines, exits := launched(t)
	_ = p.Input(map[string]any{"spawn": true})
	pid := int(next(t, lines)["pid"].(float64))
	_ = p.Input(map[string]any{"exit": true})
	select {
	case <-exits:
	case <-time.After(10 * time.Second):
		t.Fatal("no exit")
	}
	if !gone(pid) {
		t.Fatal("a child outlived its engine")
	}
	if err := p.Stop(); err != nil {
		t.Fatal("Stop after exit:", err)
	}
}

func TestLocalShutdownNeedsTheTokenFromThisMachine(t *testing.T) {
	m, _ := fixture(t)
	s := NewServer(m, nil, "agent.localhost:17880", "")
	stopped := 0
	s.stopToken, s.stop = "secret", func() { stopped++ }
	try := func(method, remote, tok string) int {
		r := httptest.NewRequest(method, "http://127.0.0.1:17880/api/local/shutdown", nil)
		r.RemoteAddr = remote
		if tok != "" {
			r.Header.Set(stopHeader, tok)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	for _, c := range []struct {
		method, remote, tok string
		want                int
	}{
		{"POST", "203.0.113.9:4000", "secret", 403},
		{"POST", "127.0.0.1:4000", "wrong", 403},
		{"POST", "127.0.0.1:4000", "", 403},
		{"GET", "127.0.0.1:4000", "secret", 405},
		{"POST", "127.0.0.1:4000", "secret", 202},
	} {
		if got := try(c.method, c.remote, c.tok); got != c.want {
			t.Fatalf("%+v: %d", c, got)
		}
	}
	if stopped != 1 {
		t.Fatalf("stopped %d times", stopped)
	}
}

func TestRunPublishesItselfAndStopsOnRequest(t *testing.T) {
	m, _ := fixture(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), m, nil, addr, "") }()
	var info ServerInfo
	deadline := time.Now().Add(5 * time.Second)
	for {
		if info, err = ReadServerInfo(m.store.dir); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no server.json")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if info.PID != os.Getpid() || info.Listen != addr || info.Token == "" {
		t.Fatalf("info: %+v", info)
	}
	if st, _ := os.Stat(filepath.Join(m.store.dir, serverInfoFile)); goruntime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("server.json mode %v", st.Mode())
	}
	req, _ := http.NewRequest("POST", "http://"+addr+"/api/local/shutdown", nil)
	req.Header.Set(stopHeader, info.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 202 {
		t.Fatalf("shutdown: %v %v", res, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
	if _, err := os.Stat(filepath.Join(m.store.dir, serverInfoFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("server.json left behind:", err)
	}
}
