package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMossFollowsNewVersionsAndKeepsUserChanges(t *testing.T) {
	m, _ := fixture(t)
	root := filepath.Join(t.TempDir(), "assistants")
	if _, err := m.setUp(root, "claude", "", "zh-Hans"); err != nil {
		t.Fatal(err)
	}
	root = m.assistantsRoot()
	procedure := filepath.Join(root, "main", "AGENTS.md")
	if b, _ := os.ReadFile(procedure); string(b) != string(mossFile("moss/AGENTS.md")) {
		t.Fatal("Moss's procedure is not the shipped file")
	}
	if st := m.mossState(); len(st.Shipped) != len(mossFiles) || len(st.Pending) != 0 {
		t.Fatalf("after setting up: %+v", st)
	}
	// A new version: an unchanged file is replaced, a changed one is kept
	// and offered.
	st := m.mossState()
	st.Shipped["main/AGENTS.md"] = digest([]byte("old shipped\n"))
	st.Shipped[".agents/skills/help/SKILL.md"] = digest([]byte("old shipped\n"))
	_ = m.store.setting("moss", st)
	os.WriteFile(procedure, []byte("old shipped\n"), 0o644)
	help := filepath.Join(root, ".agents", "skills", "help", "SKILL.md")
	os.WriteFile(help, []byte("my own help\n"), 0o644)
	if _, err := m.updateMoss(root); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(procedure); string(b) != string(mossFile("moss/AGENTS.md")) {
		t.Fatal("an unchanged file was not updated")
	}
	if b, _ := os.ReadFile(help); string(b) != "my own help\n" {
		t.Fatal("the user's change was overwritten")
	}
	if p := m.mossState().Pending; len(p) != 1 || p[0] != ".agents/skills/help/SKILL.md" {
		t.Fatalf("pending: %v", p)
	}
	// Keep mine: not offered again; take: the new version.
	if err := m.ResolveMoss(false); err != nil || len(m.mossState().Pending) != 0 {
		t.Fatalf("keep: %v %+v", err, m.mossState())
	}
	m.updateMoss(root)
	if len(m.mossState().Pending) != 0 {
		t.Fatal("offered again after keeping")
	}
	st = m.mossState()
	st.Pending = []string{".agents/skills/help/SKILL.md"}
	_ = m.store.setting("moss", st)
	if err := m.ResolveMoss(true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(help); !strings.Contains(string(b), "name: help") {
		t.Fatal("take did not write the new version")
	}
}

func TestMossExportsOnlyItsDefinition(t *testing.T) {
	m, _ := fixture(t)
	root := filepath.Join(t.TempDir(), "assistants")
	if _, err := m.setUp(root, "claude", "", "zh-Hant"); err != nil {
		t.Fatal(err)
	}
	root = m.assistantsRoot()
	// A checkout: the assistant templates as in the source tree.
	repo := t.TempDir()
	base := filepath.Join(repo, "internal", "workbench", "assistant")
	for _, src := range mossFiles {
		writeToml(t, filepath.Join(base, filepath.FromSlash(src)), string(mossFile(src)))
	}
	if changed, err := ExportMoss(root, repo); err != nil || len(changed) != 0 {
		t.Fatalf("nothing changed yet: %v %v", changed, err)
	}
	// The user's Moss learns something; their state stays home.
	os.WriteFile(filepath.Join(root, "main", "AGENTS.md"), append(mossFile("moss/AGENTS.md"), []byte("- Answer briefly.\n")...), 0o644)
	os.WriteFile(filepath.Join(root, "main", "state", "loops.md"), []byte("- call Ana\n"), 0o644)
	changed, err := ExportMoss(root, repo)
	if err != nil || len(changed) != 1 || changed[0] != "internal/workbench/assistant/moss/AGENTS.md" {
		t.Fatalf("export: %v %v", changed, err)
	}
	if b, _ := os.ReadFile(filepath.Join(base, "root", "AGENTS.md")); !strings.Contains(string(b), "{{.LanguageLine}}") {
		t.Fatal("the language rule did not go back to its placeholder")
	}
	// A credential stops it.
	os.WriteFile(filepath.Join(root, ".agents", "skills", "help", "SKILL.md"), []byte("token ghp_"+strings.Repeat("a1B2", 9)+"\n"), 0o644)
	if _, err := ExportMoss(root, repo); err == nil {
		t.Fatal("exported a token")
	}
	if _, err := ExportMoss(root, t.TempDir()); err == nil {
		t.Fatal("exported into something that is not a checkout")
	}
}
