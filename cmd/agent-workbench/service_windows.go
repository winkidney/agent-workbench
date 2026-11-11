package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

func defaultDataDir() string {
	return filepath.Join(localAppData(), "agent-workbench")
}

func installDir() string {
	return filepath.Join(localAppData(), "Programs", "agent-workbench")
}

func localAppData() string {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "AppData", "Local")
}

// The per-user login start: a value under the Run key.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// startService: Windows has no user service manager to ask; the caller
// runs serve in the background.
func startService(config, string) (bool, error) { return false, nil }
func stopService(config) (bool, error)          { return false, nil }

// runCommand is the login command line: the windowless build, so no
// console window opens.
func runCommand(c config, exe string) string {
	args := []string{backgroundExe(exe), "serve", "--data-dir", c.dataDir, "--listen", c.listen, "--idle", c.idle.String(), "--env-file", envPath(c)}
	for i, a := range args {
		args[i] = syscall.EscapeArg(a)
	}
	return strings.Join(args, " ")
}

// canAutostart: the Run key is always there.
func canAutostart(string) error { return nil }

func installAutostart(c config, exe string) (<-chan error, error) {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return nil, err
	}
	defer key.Close()
	if err := key.SetStringValue(c.name, runCommand(c, exe)); err != nil {
		return nil, err
	}
	return startDetached(c, exe)
}

func uninstallAutostart(c config) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	if err := key.DeleteValue(c.name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func autostartState(c config) string {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return "not installed"
	}
	defer key.Close()
	if _, _, err := key.GetStringValue(c.name); err != nil {
		return "not installed"
	}
	return fmt.Sprintf(`at login (HKCU\%s\%s)`, runKey, c.name)
}

func doctorPlatform() error {
	exe, err := executable()
	if err != nil {
		return err
	}
	if w := backgroundExe(exe); w == exe {
		fmt.Printf("%s: missing next to %s (background start would open a console window)\n", companions[0], exe)
	}
	return nil
}
