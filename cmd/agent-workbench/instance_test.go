package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstanceNames(t *testing.T) {
	other := t.TempDir()
	cases := []struct {
		dir, listen string
		given       map[string]bool
		action      string
		want        string
		fails       bool
	}{
		// The default data directory keeps the name, whatever the port.
		{defaultDataDir(), "0.0.0.0:18000", map[string]bool{"listen": true}, "start", "agent-workbench", false},
		// Another is named after its port.
		{other, "127.0.0.1:17881", map[string]bool{"data-dir": true, "listen": true}, "start", "agent-workbench-17881", false},
		// And needs one of its own to start.
		{other, "0.0.0.0:17880", map[string]bool{"data-dir": true}, "start", "", true},
		{other, "0.0.0.0:17880", map[string]bool{"data-dir": true}, "install", "", true},
		// A name given is kept.
		{other, "127.0.0.1:17882", map[string]bool{"name": true, "listen": true}, "start", "chosen", false},
	}
	for _, c := range cases {
		cfg := config{name: "agent-workbench", dataDir: c.dir, listen: c.listen}
		if c.given["name"] {
			cfg.name = "chosen"
		}
		err := resolveInstance(&cfg, c.given, c.action)
		if (err != nil) != c.fails || (!c.fails && cfg.name != c.want) {
			t.Errorf("%s %s %s: %q %v", c.dir, c.listen, c.action, cfg.name, err)
		}
	}
	// Recorded: found again from --data-dir alone.
	writeInstance(config{name: "agent-workbench-17881", dataDir: other, listen: "127.0.0.1:17881"})
	cfg := config{name: "agent-workbench", dataDir: other, listen: "0.0.0.0:17880"}
	if err := resolveInstance(&cfg, map[string]bool{"data-dir": true}, "stop"); err != nil || cfg.name != "agent-workbench-17881" || cfg.listen != "127.0.0.1:17881" {
		t.Fatalf("recorded: %+v %v", cfg, err)
	}
}

// Two workbenches, told apart by --data-dir and --listen alone.
func TestSecondInstanceByDataDirAndListen(t *testing.T) {
	run := cli(t)
	port := freePort(t)
	dir := t.TempDir()
	start := []string{"--data-dir", dir, "--listen", "127.0.0.1:" + port}
	byDir := []string{"--data-dir", dir}
	t.Cleanup(func() { run(append([]string{"stop"}, byDir...)...) })
	if out, err := run(append([]string{"start"}, byDir...)...); err == nil || !strings.Contains(out, "needs its own --listen") {
		t.Fatalf("start without --listen: %v\n%s", err, out)
	}
	if out, err := run(append([]string{"start"}, start...)...); err != nil || !strings.Contains(out, "Open http://agent.localhost:"+port) {
		t.Fatalf("start: %v\n%s", err, out)
	}
	out, _ := run(append([]string{"status"}, byDir...)...)
	if !strings.Contains(out, "Running") || !strings.Contains(out, "Instance  agent-workbench-"+port) {
		t.Fatalf("status: %s", out)
	}
	if out, err := run(append([]string{"stop"}, byDir...)...); err != nil {
		t.Fatalf("stop: %v\n%s", err, out)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := http.Get("http://127.0.0.1:" + port + "/api/auth/status"); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("still answering after stop")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "instance.json")); err != nil || !strings.Contains(string(b), "agent-workbench-"+port) {
		t.Fatalf("instance.json: %s %v", b, err)
	}
}
