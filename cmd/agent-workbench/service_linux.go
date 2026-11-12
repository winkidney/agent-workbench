package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/winkidney/agent-workbench/internal/workbench"
)

func defaultDataDir() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "agent-workbench")
}

func installDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin")
}

func unitName(c config) string { return c.name + ".service" }

func unitPath(c config) string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "systemd", "user", unitName(c))
}

func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// userSystemd is whether a systemd user manager runs this user's services.
func userSystemd() bool {
	return exec.Command("systemctl", "--user", "show", "--property=Version").Run() == nil
}

func unitActive(c config) bool {
	out, _ := exec.Command("systemctl", "--user", "is-active", unitName(c)).Output()
	return strings.TrimSpace(string(out)) == "active"
}

// startService starts the installed unit, or a transient unit when nothing
// is installed. Without a user manager the caller runs serve itself.
func startService(c config, exe string) (bool, error) {
	if !userSystemd() {
		return false, nil
	}
	if exists(unitPath(c)) {
		return true, systemctl("start", unitName(c))
	}
	cwd, err := os.Getwd()
	if err != nil {
		return true, err
	}
	args := []string{"--user", "--quiet", "--collect", "--unit=" + unitName(c), "--service-type=exec", "--working-directory=" + cwd, "--property=KillMode=control-group", "--property=TimeoutStopSec=20s"}
	// Copy the launch environment by name; values never appear in process arguments.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name != "INVOCATION_ID" && name != "SYSTEMD_EXEC_PID" {
			args = append(args, "--setenv="+name)
		}
	}
	args = append(args, "--", exe, "serve", "--name", c.name, "--data-dir", c.dataDir, "--listen", c.listen, "--idle", c.idle.String())
	cmd := exec.Command("systemd-run", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return true, cmd.Run()
}

func stopService(c config) (bool, error) {
	if !userSystemd() || !unitActive(c) {
		return false, nil
	}
	return true, systemctl("stop", unitName(c))
}

// unitFile runs serve at login under the user manager. Session units are
// bound to it (BindsTo=<name>.service).
func unitFile(c config, exe string) string {
	args := []string{exe, "serve", "--name", c.name, "--data-dir", c.dataDir, "--listen", c.listen, "--idle", c.idle.String(), "--env-file", envPath(c)}
	for i, a := range args {
		args[i] = unitQuote(a)
	}
	return fmt.Sprintf(`[Unit]
Description=Agent Workbench (Claude Code and Codex)

[Service]
Type=exec
ExecStart=%s
KillMode=control-group
TimeoutStopSec=20s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, strings.Join(args, " "))
}

// unitQuote quotes a word for ExecStart (specifiers and variables
// escaped).
func unitQuote(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$").Replace(s)
	return `"` + s + `"`
}

// canAutostart needs the systemd user manager.
func canAutostart(exe string) error {
	if !userSystemd() {
		return fmt.Errorf("no systemd user manager for this user, so nothing was installed; run %q after logging in instead", exe+" start")
	}
	return nil
}

func installAutostart(c config, exe string) (<-chan error, error) {
	path := unitPath(c)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(unitFile(c, exe)), 0o644); err != nil {
		return nil, err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", unitName(c)}, {"restart", unitName(c)}} {
		if err := systemctl(args...); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func uninstallAutostart(c config) error {
	path := unitPath(c)
	if !exists(path) {
		return nil
	}
	_ = systemctl("disable", "--now", unitName(c))
	if err := os.Remove(path); err != nil {
		return err
	}
	return systemctl("daemon-reload")
}

// autostartFor is whether c's login start runs the workbench of dataDir.
func autostartFor(c config, dataDir string) bool {
	b, err := os.ReadFile(unitPath(c))
	return err == nil && strings.Contains(string(b), unitQuote(dataDir))
}

// otherAutostarts is whether another workbench starts at login.
func otherAutostarts(c config) bool {
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(unitPath(c)), workbench.DefaultInstance+"*.service"))
	for _, f := range files {
		if filepath.Base(f) != unitName(c) {
			return true
		}
	}
	return false
}

func autostartState(c config) string {
	path := unitPath(c)
	if !exists(path) {
		return "not installed"
	}
	out, _ := exec.Command("systemctl", "--user", "is-enabled", unitName(c)).Output()
	return fmt.Sprintf("systemd user unit %s (%s)", path, strings.TrimSpace(string(out)))
}

func doctorPlatform() error {
	for _, name := range []string{"systemd-run", "systemctl"} {
		path, err := exec.LookPath(name)
		if err != nil {
			fmt.Printf("%s: not found (engines run as child processes)\n", name)
			continue
		}
		fmt.Printf("%s: %s\n", name, path)
	}
	if !userSystemd() {
		fmt.Println("systemd user manager: not available (start runs the workbench in the background without it)")
		return nil
	}
	out, _ := exec.Command("systemctl", "--user", "show", "--property=Version", "--value").Output()
	fmt.Printf("systemd user manager: %s\n", strings.TrimSpace(string(out)))
	return nil
}
