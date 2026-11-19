package workbench

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
)

// Credentials stay out of the repository of assistants (ADR 0014): a git
// pre-commit check refuses commits that carry them, whoever commits (an
// assistant, the workbench, the user), and the root .gitignore leaves out
// the usual credential files. Only on the user's explicit request does a
// commit set ASSISTANTS_ALLOW_SECRETS=1.

//go:embed assistant/root/pre-commit
var preCommitHook []byte

//go:embed assistant/root/gitignore
var rootIgnore string

const hookMarker = "agent-workbench secret check"

// installHook puts the secret check in root's git hooks: when there is none,
// or ours is older. A hook of the user's own is left alone.
func installHook(root string) (bool, error) {
	dir := filepath.Join(root, ".git", "hooks")
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return false, nil
	}
	p := filepath.Join(dir, "pre-commit")
	if old, err := os.ReadFile(p); err == nil {
		if !strings.Contains(string(old), hookMarker) || string(old) == string(preCommitHook) {
			return false, nil
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(p, preCommitHook, 0o755)
}

// ensureIgnored adds the template's credential lines that root's .gitignore
// lacks, below the user's own.
func ensureIgnored(root string) (bool, error) {
	p := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(p)
	if err != nil {
		return false, nil
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(data), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	_, block, _ := strings.Cut(rootIgnore, "# Credentials never go")
	var missing []string
	for _, l := range strings.Split(block, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") && !strings.HasPrefix(l, "~/") && !have[l] {
			missing = append(missing, l)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}
	text := strings.TrimRight(string(data), "\n") + "\n# Credentials never go in this repository (they live in\n# ~/.config/assistant/secrets/); the pre-commit check refuses them too.\n" + strings.Join(missing, "\n") + "\n"
	return true, os.WriteFile(p, []byte(text), 0o644)
}
