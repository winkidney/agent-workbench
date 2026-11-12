package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoginStartsOfOtherInstances(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := "/data/work bench"
	work := config{name: "agent-workbench-17881", dataDir: dir, listen: "127.0.0.1:17881", idle: 5 * time.Minute}
	legacy := work
	legacy.name = "agent-workbench"
	os.MkdirAll(filepath.Dir(unitPath(work)), 0o755)

	// An older install of this data directory under the default name.
	os.WriteFile(unitPath(legacy), []byte(unitFile(legacy, "/bin/agent-workbench")), 0o644)
	if !autostartFor(legacy, dir) || autostartFor(legacy, "/data/other") {
		t.Fatal("legacy login start not recognised by its data directory")
	}
	if !otherAutostarts(work) {
		t.Fatal("the default instance's login start not seen")
	}
	// Its own unit alone: nothing else uses the program.
	os.Remove(unitPath(legacy))
	os.WriteFile(unitPath(work), []byte(unitFile(work, "/bin/agent-workbench")), 0o644)
	if otherAutostarts(work) {
		t.Fatal("own login start counted as another")
	}
	if unit := unitFile(work, "/bin/agent-workbench"); !strings.Contains(unit, `"--name" "agent-workbench-17881"`) {
		t.Fatalf("unit does not name the instance:\n%s", unit)
	}
}
