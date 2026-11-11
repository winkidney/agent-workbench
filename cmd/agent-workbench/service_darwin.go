package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func defaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "agent-workbench")
}

func installDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin")
}

func label(c config) string { return "local." + c.name }

func plistPath(c config) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", label(c)+".plist")
}

// domain is the login session's launchd domain, or the user's background
// domain on a Mac without one (signed in over SSH only).
func domain() string {
	gui := "gui/" + strconv.Itoa(os.Getuid())
	if exec.Command("launchctl", "print", gui).Run() == nil {
		return gui
	}
	return "user/" + strconv.Itoa(os.Getuid())
}

func launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %v: %s: %w", args, bytes.TrimSpace(out), err)
	}
	return nil
}

func loaded(c config) bool {
	return exec.Command("launchctl", "print", domain()+"/"+label(c)).Run() == nil
}

// startService starts the installed launch agent; without one the caller
// runs serve itself.
func startService(c config, exe string) (bool, error) {
	path := plistPath(c)
	if !exists(path) {
		return false, nil
	}
	if !loaded(c) {
		return true, launchctl("bootstrap", domain(), path)
	}
	return true, launchctl("kickstart", domain()+"/"+label(c))
}

// stopService leaves stopping to the shutdown route: the agent restarts
// only after a failure, and a requested stop exits cleanly.
func stopService(config) (bool, error) { return false, nil }

// plist runs serve at login, restarts it after a failure, and logs to the
// data directory.
func plist(c config, exe string) []byte {
	var b bytes.Buffer
	str := func(s string) string {
		var e bytes.Buffer
		_ = xml.EscapeText(&e, []byte(s))
		return "<string>" + e.String() + "</string>"
	}
	home, _ := os.UserHomeDir()
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>` + str(label(c)) + `
	<key>ProgramArguments</key>
	<array>
`)
	for _, a := range []string{exe, "serve", "--data-dir", c.dataDir, "--listen", c.listen, "--idle", c.idle.String(), "--env-file", envPath(c)} {
		b.WriteString("\t\t" + str(a) + "\n")
	}
	b.WriteString(`	</array>
	<key>WorkingDirectory</key>` + str(home) + `
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key><false/>
	</dict>
	<key>StandardOutPath</key>` + str(logPath(c.dataDir)) + `
	<key>StandardErrorPath</key>` + str(logPath(c.dataDir)) + `
</dict>
</plist>
`)
	return b.Bytes()
}

// canAutostart needs launchctl.
func canAutostart(string) error {
	if _, err := exec.LookPath("launchctl"); err != nil {
		return fmt.Errorf("launchctl not found, so nothing was installed: %w", err)
	}
	return nil
}

func installAutostart(c config, exe string) (<-chan error, error) {
	path := plistPath(c)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if loaded(c) {
		_ = launchctl("bootout", domain()+"/"+label(c))
	}
	if err := os.WriteFile(path, plist(c, exe), 0o644); err != nil {
		return nil, err
	}
	// RunAtLoad starts it now.
	return nil, launchctl("bootstrap", domain(), path)
}

func uninstallAutostart(c config) error {
	path := plistPath(c)
	if loaded(c) {
		_ = launchctl("bootout", domain()+"/"+label(c))
	}
	if exists(path) {
		return os.Remove(path)
	}
	return nil
}

func autostartState(c config) string {
	path := plistPath(c)
	if !exists(path) {
		return "not installed"
	}
	state := "not loaded"
	if loaded(c) {
		state = "loaded"
	}
	return fmt.Sprintf("launch agent %s (%s)", path, state)
}

func doctorPlatform() error {
	path, err := exec.LookPath("launchctl")
	if err != nil {
		fmt.Println("launchctl: not found (install cannot set up autostart)")
		return nil
	}
	fmt.Printf("launchctl: %s\n", path)
	return nil
}
