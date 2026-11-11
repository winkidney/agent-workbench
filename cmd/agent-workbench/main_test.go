package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestServiceEnvKeepsPathsAndProxiesButNotSecrets(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.local:3128")
	t.Setenv("ANTHROPIC_API_KEY", "sk-secret")
	t.Setenv("WORKBENCH_EXTRA", "named")
	path := filepath.Join(t.TempDir(), "service-env.json")
	if err := writeEnv(path, []string{"WORKBENCH_EXTRA"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	env := map[string]string{}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	if env["PATH"] == "" || env["HTTPS_PROXY"] != "http://proxy.local:3128" || env["WORKBENCH_EXTRA"] != "named" {
		t.Fatalf("env: %v", env)
	}
	if _, ok := env["ANTHROPIC_API_KEY"]; ok {
		t.Fatal("a secret was captured")
	}
	if st, _ := os.Stat(path); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	t.Setenv("HTTPS_PROXY", "")
	if err := loadEnv(path); err != nil || os.Getenv("HTTPS_PROXY") != "http://proxy.local:3128" {
		t.Fatalf("load: %v %q", err, os.Getenv("HTTPS_PROXY"))
	}
}

// cli builds the command once per test and runs it.
func cli(t *testing.T) func(args ...string) (string, error) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), binaryName)
	if out, err := exec.Command("go", "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return func(args ...string) (string, error) {
		cmd := exec.Command(exe, args...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
}

func freePort(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

// The background start, status and stop, on whatever this machine offers
// (a transient systemd unit, or a detached process).
func TestStartStatusStop(t *testing.T) {
	run := cli(t)
	port := freePort(t)
	flags := []string{"--name", fmt.Sprintf("aw-cli-test-%d", os.Getpid()), "--data-dir", t.TempDir(), "--listen", "127.0.0.1:" + port}
	t.Cleanup(func() { run(append([]string{"stop"}, flags...)...) })
	out, err := run(append([]string{"start"}, flags...)...)
	if err != nil || !strings.Contains(out, "Open http://agent.localhost:"+port) {
		t.Fatalf("start: %v\n%s", err, out)
	}
	if out, _ := run(append([]string{"start"}, flags...)...); !strings.Contains(out, "Already running") {
		t.Fatalf("second start: %s", out)
	}
	if out, _ := run(append([]string{"status"}, flags...)...); !strings.Contains(out, "Running") || !strings.Contains(out, "Autostart not installed") {
		t.Fatalf("status: %s", out)
	}
	res, err := http.Get("http://127.0.0.1:" + port + "/api/auth/status")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("auth status: %v %v", res, err)
	}
	res.Body.Close()
	if out, err := run(append([]string{"stop"}, flags...)...); err != nil {
		t.Fatalf("stop: %v\n%s", err, out)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := http.Get("http://127.0.0.1:" + port + "/api/auth/status")
		if err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("still answering after stop")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if out, _ := run(append([]string{"status"}, flags...)...); !strings.Contains(out, "Not running") {
		t.Fatalf("status after stop: %s", out)
	}
}

// install and uninstall change the login start of the account running the
// tests, so they run only where asked (CI).
func TestInstallUninstall(t *testing.T) {
	if os.Getenv("AGENT_WORKBENCH_INSTALL_TEST") != "1" {
		t.Skip("set AGENT_WORKBENCH_INSTALL_TEST=1 to register and remove a real login start")
	}
	if exe, _ := executable(); canAutostart(exe) != nil {
		t.Skip("no login start on this machine:", canAutostart(exe))
	}
	run := cli(t)
	port := freePort(t)
	name := fmt.Sprintf("aw-install-test-%d", os.Getpid())
	flags := []string{"--name", name, "--data-dir", t.TempDir(), "--listen", "127.0.0.1:" + port}
	t.Cleanup(func() { run(append([]string{"uninstall"}, flags...)...) })
	out, err := run(append([]string{"install"}, flags...)...)
	if err != nil || !strings.Contains(out, "Installed") || strings.Contains(out, "Autostart not installed") {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(installDir(), binaryName)); err != nil && !packaged(installDir()) {
		t.Fatalf("binary not installed: %v", err)
	}
	res, err := http.Get("http://127.0.0.1:" + port + "/api/auth/status")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("after install: %v %v", res, err)
	}
	res.Body.Close()
	if out, err := run(append([]string{"uninstall"}, flags...)...); err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if out, _ := run(append([]string{"status"}, flags...)...); !strings.Contains(out, "Not running") || !strings.Contains(out, "Autostart not installed") {
		t.Fatalf("status after uninstall: %s", out)
	}
	if _, err := http.Get("http://127.0.0.1:" + port + "/api/auth/status"); err == nil {
		t.Fatal("still answering after uninstall")
	}
}
