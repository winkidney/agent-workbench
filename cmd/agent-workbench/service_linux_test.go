package main

import (
	"strings"
	"testing"
	"time"
)

func TestUnitFileQuotesItsArguments(t *testing.T) {
	c := config{name: "agent-workbench", dataDir: "/home/a b/50%/$x", listen: "0.0.0.0:17880", idle: 5 * time.Minute}
	unit := unitFile(c, `/opt/my "bin"/agent-workbench`)
	want := `ExecStart="/opt/my \"bin\"/agent-workbench" "serve" "--name" "agent-workbench" "--data-dir" "/home/a b/50%%/$$x" "--listen" "0.0.0.0:17880" "--idle" "5m0s" "--env-file" "/home/a b/50%%/$$x/service-env.json"`
	if !strings.Contains(unit, want+"\n") {
		t.Fatalf("unit:\n%s\nwant line:\n%s", unit, want)
	}
	for _, line := range []string{"KillMode=control-group", "Restart=on-failure", "WantedBy=default.target"} {
		if !strings.Contains(unit, line+"\n") {
			t.Fatalf("missing %s", line)
		}
	}
}
