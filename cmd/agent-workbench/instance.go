package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/winkidney/agent-workbench/internal/workbench"
)

// Several workbenches can run for one user, each with its own --data-dir
// and --listen. The one in the default data directory keeps the name every
// install had (agent-workbench); another is named after its port
// (agent-workbench-17881). The name and address are recorded in the data
// directory, so stop, status and uninstall find the instance from
// --data-dir alone.

type instanceInfo struct {
	Name   string `json:"name"`
	Listen string `json:"listen"`
}

func instancePath(dataDir string) string { return filepath.Join(dataDir, "instance.json") }

func readInstance(dataDir string) instanceInfo {
	var v instanceInfo
	if b, err := os.ReadFile(instancePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &v)
	}
	return v
}

// writeInstance records the instance; a failure only loses the shortcut.
func writeInstance(c config) {
	b, _ := json.Marshal(instanceInfo{Name: c.name, Listen: c.listen})
	_ = os.WriteFile(instancePath(c.dataDir), b, 0o600)
}

func sameDir(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	return filepath.Clean(a) == filepath.Clean(b)
}

// resolveInstance settles the service name and, when not given, the
// address an instance was last run with.
func resolveInstance(c *config, given map[string]bool, action string) error {
	saved := readInstance(c.dataDir)
	if !given["listen"] {
		if saved.Listen != "" {
			c.listen = saved.Listen
		} else if info, err := workbench.ReadServerInfo(c.dataDir); err == nil && info.Listen != "" {
			c.listen = info.Listen
		}
	}
	if given["name"] {
		return nil
	}
	if sameDir(c.dataDir, defaultDataDir()) {
		c.name = workbench.DefaultInstance
		return nil
	}
	if !given["listen"] && saved.Listen == "" {
		switch action {
		case "serve", "start", "install":
			// Its own data, then its own address: the default one is the
			// default workbench's.
			if _, err := workbench.ReadServerInfo(c.dataDir); err != nil {
				return errors.New("a workbench with its own --data-dir needs its own --listen, for example --listen 127.0.0.1:17881")
			}
		}
	}
	if saved.Name != "" && !given["listen"] {
		c.name = saved.Name
		return nil
	}
	_, port, err := net.SplitHostPort(c.listen)
	if err != nil || port == "" {
		return fmt.Errorf("--listen %q: use HOST:PORT", c.listen)
	}
	c.name = workbench.DefaultInstance + "-" + port
	return nil
}

// migrateLegacyAutostart removes a login start an older install made for
// this data directory under the default name, before one is made under
// the instance's own.
func migrateLegacyAutostart(c config) error {
	if c.name == workbench.DefaultInstance {
		return nil
	}
	legacy := c
	legacy.name = workbench.DefaultInstance
	if !autostartFor(legacy, c.dataDir) {
		return nil
	}
	fmt.Printf("Moving the login start of %s from %s to %s\n", c.dataDir, legacy.name, c.name)
	return uninstallAutostart(legacy)
}
