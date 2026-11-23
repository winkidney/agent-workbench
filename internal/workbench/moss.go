package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Moss's definition (ADR 0016): the files Agent Workbench ships for the
// personal assistant and the rules and skills every assistant shares. They
// go down to the user's repository on every start (a file the user did not
// change is replaced by the new version; one they changed is kept and
// offered), and come back with `agent-workbench moss export`, which copies
// only these files into an agent-workbench checkout. The user's name,
// language, routines, state, log and records never leave their repository.

// mossFiles maps a file in the repository of assistants to its source in
// internal/workbench/assistant.
var mossFiles = map[string]string{
	"main/AGENTS.md":                           "moss/AGENTS.md",
	"main/tools/repo_sync.py":                  "moss/tools/repo_sync.py",
	"main/tools/test_repo_sync.py":             "moss/tools/test_repo_sync.py",
	"AGENTS.md":                                "root/AGENTS.md",
	".agents/skills/create-assistant/SKILL.md": "root/skills/create-assistant/SKILL.md",
	".agents/skills/help/SKILL.md":             "root/skills/help/SKILL.md",
	".agents/skills/start-session/SKILL.md":    "root/skills/start-session/SKILL.md",
	"shared/tools/secret.py":                   "root/tools/secret.py",
	"shared/tools/test_secret.py":              "root/tools/test_secret.py",
}

func mossFile(src string) []byte {
	data, err := assistantTemplates.ReadFile("assistant/" + src)
	if err != nil {
		panic(err)
	}
	return data
}

// shipped is a definition file as this version installs it for lang.
func shipped(path, lang string) []byte {
	data := mossFile(mossFiles[path])
	if path == "AGENTS.md" {
		data = []byte(strings.Replace(string(data), "{{.LanguageLine}}", languageLine(lang), 1))
	}
	return data
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// mossState is what the workbench remembers of the files it shipped: each
// one's digest as last written, and the ones waiting for the user.
type mossState struct {
	Shipped map[string]string `json:"shipped"`
	Pending []string          `json:"pending,omitempty"`
	// Versions the user chose not to take, by file: not offered again.
	Declined map[string]string `json:"declined,omitempty"`
}

func (m *Manager) mossState() mossState {
	var v mossState
	m.store.readSetting("moss", &v)
	if v.Shipped == nil {
		v.Shipped = map[string]string{}
	}
	if v.Declined == nil {
		v.Declined = map[string]string{}
	}
	return v
}

// updateMoss brings the user's copy of Moss's definition up to this
// version: missing or unchanged files are written, changed ones are kept
// and listed as pending. It reports whether it wrote anything.
func (m *Manager) updateMoss(root string) (bool, error) {
	st := m.mossState()
	lang := m.language()
	st.Pending = nil
	wrote := false
	paths := make([]string, 0, len(mossFiles))
	for p := range mossFiles {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		dest := filepath.Join(root, filepath.FromSlash(p))
		if strings.HasPrefix(p, "main/") {
			if _, err := os.Stat(filepath.Join(root, "main", "assistant.toml")); err != nil {
				continue
			}
		}
		next := shipped(p, lang)
		have, err := os.ReadFile(dest)
		switch {
		case err != nil:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return wrote, err
			}
		case digest(have) == digest(next):
			st.Shipped[p] = digest(next)
			continue
		case st.Shipped[p] != "" && digest(have) == st.Shipped[p]:
			// Unchanged since it was shipped: the new version replaces it.
		default:
			// The user changed it (or it predates this record): keep it,
			// and offer this version unless they declined it already.
			if st.Declined[p] != digest(next) {
				st.Pending = append(st.Pending, p)
			}
			continue
		}
		if err := os.WriteFile(dest, next, 0o644); err != nil {
			return wrote, err
		}
		st.Shipped[p] = digest(next)
		wrote = true
	}
	return wrote, m.store.setting("moss", st)
}

// ResolveMoss answers the offer of a newer Moss: take replaces the user's
// changed files with the new version; otherwise they are kept and not
// offered again until the next version.
func (m *Manager) ResolveMoss(take bool) error {
	root := m.assistantsRoot()
	if root == "" {
		return errors.New("set assistants up first")
	}
	st := m.mossState()
	lang := m.language()
	for _, p := range st.Pending {
		next := shipped(p, lang)
		if take {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(p)), next, 0o644); err != nil {
				return err
			}
			st.Shipped[p] = digest(next)
			delete(st.Declined, p)
		} else {
			st.Declined[p] = digest(next)
		}
	}
	if take && len(st.Pending) > 0 {
		commitAll(root, "chore: take the new Moss from Agent Workbench")
	}
	st.Pending = nil
	if err := m.store.setting("moss", st); err != nil {
		return err
	}
	m.assistantsChanged()
	return nil
}

// ExportMoss copies the user's Moss definition into an agent-workbench
// checkout, for review and a commit there. The language rule goes back to
// its placeholder; a file that looks like it holds a credential stops it.
// It returns the repository paths it changed.
func ExportMoss(root, repo string) ([]string, error) {
	base := filepath.Join(repo, "internal", "workbench", "assistant")
	if _, err := os.Stat(filepath.Join(base, "moss", "AGENTS.md")); err != nil {
		return nil, fmt.Errorf("%s is not an agent-workbench checkout", repo)
	}
	paths := make([]string, 0, len(mossFiles))
	for p := range mossFiles {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var changed []string
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		text := string(data)
		if p == "AGENTS.md" {
			text = languageRule.ReplaceAllLiteralString(text, "{{.LanguageLine}}")
		}
		for _, pat := range secretPatterns {
			if pat.MatchString(text) {
				return changed, fmt.Errorf("%s looks like it holds a credential; nothing more was copied", p)
			}
		}
		dest := filepath.Join(base, filepath.FromSlash(mossFiles[p]))
		if old, err := os.ReadFile(dest); err == nil && string(old) == text {
			continue
		}
		if err := os.WriteFile(dest, []byte(text), 0o644); err != nil {
			return changed, err
		}
		changed = append(changed, filepath.ToSlash(filepath.Join("internal/workbench/assistant", mossFiles[p])))
	}
	return changed, nil
}
