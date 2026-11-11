// fakeagent stands in for claude or codex in process tests: it echoes each
// JSON line, starts a long-lived child on {"spawn":true} (as a tool would),
// and exits on {"exit":true}.
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "sleep" {
		time.Sleep(time.Hour)
		return
	}
	out := json.NewEncoder(os.Stdout)
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var in map[string]any
		if json.Unmarshal(scan.Bytes(), &in) != nil {
			continue
		}
		switch {
		case in["spawn"] == true:
			child := exec.Command(os.Args[0], "sleep")
			if err := child.Start(); err != nil {
				out.Encode(map[string]any{"type": "error", "text": err.Error()})
				continue
			}
			out.Encode(map[string]any{"type": "spawned", "pid": child.Process.Pid})
		case in["exit"] == true:
			os.Exit(0)
		default:
			out.Encode(map[string]any{"type": "echo", "input": in, "args": os.Args[1:]})
		}
	}
}
