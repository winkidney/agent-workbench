package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/winkidney/agent-workbench/internal/workbench"
)

// mossCommand: `moss export --to REPO [--from ~/assistants]` copies the
// user's Moss definition (ADR 0016) into an agent-workbench checkout.
func mossCommand(args []string) error {
	if len(args) == 0 || args[0] != "export" {
		return errors.New("usage: agent-workbench moss export --to AGENT_WORKBENCH_CHECKOUT [--from ~/assistants]")
	}
	flags := flag.NewFlagSet("moss export", flag.ContinueOnError)
	to := flags.String("to", "", "an agent-workbench checkout")
	from := flags.String("from", "~/assistants", "the repository of assistants")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *to == "" {
		return errors.New("--to: the agent-workbench checkout to copy into")
	}
	home, _ := os.UserHomeDir()
	expand := func(p string) string {
		if strings.HasPrefix(p, "~/") {
			return filepath.Join(home, p[2:])
		}
		return p
	}
	changed, err := workbench.ExportMoss(expand(*from), expand(*to))
	for _, p := range changed {
		fmt.Println("updated", p)
	}
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		fmt.Println("Moss's definition is the same as in the checkout.")
		return nil
	}
	fmt.Println("Review with `git diff`, then commit in the checkout.")
	return nil
}
