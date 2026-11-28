package workbench

import (
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

// A catalog or a lock names files inside an assistant's folder only.
func TestCatalogPathsStayInsideTheFolder(t *testing.T) {
	for _, rel := range []string{"AGENTS.md", "tools/fetch.py", ".agents/skills/a/SKILL.md"} {
		if !safeRel(rel) {
			t.Errorf("%q refused", rel)
		}
	}
	for _, rel := range []string{"", "../x", "a/../../x", "/etc/passwd", "a//b", "./a", `a\b`, "-x", "a/./b", "a\x00b"} {
		if safeRel(rel) {
			t.Errorf("%q allowed", rel)
		}
	}
	if lockEntryValid("../x", LockEntry{Path: "mail"}) || lockEntryValid("mail", LockEntry{Path: "mail", Files: map[string]string{"../../.bashrc": "x"}}) {
		t.Error("a lock entry outside the repository was taken")
	}
}

func commitCatalog(t *testing.T, work, message string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", message}} {
		if out, err := exec.Command("git", append([]string{"-C", work, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
}

// An icon both sides changed does not merge: it is the user's to choose.
// A script that only becomes runnable is an update, and runs.
func TestUpdatesOfBinaryFilesAndModes(t *testing.T) {
	m, root, work, url := installFixture(t)
	png := func(b byte) string { return string([]byte{0x89, 'P', 'N', 'G', 0, b}) }
	os.WriteFile(filepath.Join(work, "mail/icon.png"), []byte(png(1)), 0o644)
	os.Chmod(filepath.Join(work, "mail/tools/fetch.py"), 0o644)
	commitCatalog(t, work, "icon")
	if _, err := m.InstallAssistant(url, "mail", "", ""); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "mail")
	os.WriteFile(filepath.Join(dir, "icon.png"), []byte(png(2)), 0o644)
	m.commitPaths(root, "my icon", []string{"mail/icon.png"})
	os.WriteFile(filepath.Join(work, "mail/icon.png"), []byte(png(3)), 0o644)
	os.Chmod(filepath.Join(work, "mail/tools/fetch.py"), 0o755)
	commitCatalog(t, work, "new icon, runnable script")

	u, err := m.UpdateFromCatalog("mail", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]UpdateFile{}
	for _, f := range u.Files {
		got[f.Rel] = f
	}
	if got["icon.png"].Result != "conflict" || got["icon.png"].Theirs == "" {
		t.Fatalf("icon: %+v", got["icon.png"])
	}
	if goruntime.GOOS == "windows" {
		return
	}
	if f := got["tools/fetch.py"]; f.Result != "update" || !f.Runs {
		t.Fatalf("a mode change: %+v", f)
	}
	if _, err := m.UpdateFromCatalog("mail", true, nil, u.Commit); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(filepath.Join(dir, "tools/fetch.py")); info.Mode().Perm()&0o100 == 0 {
		t.Fatal("the script is not runnable after the update")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "icon.png")); string(b) != png(2) {
		t.Fatal("the user's icon was not kept")
	}
}

// What a background run changes in the definition files (not tracked when
// as the catalog has them) is put back; with known writes, only those.
func TestBackgroundRunsDoNotChangeDefinitions(t *testing.T) {
	m, root, _, url := installFixture(t)
	if _, err := m.InstallAssistant(url, "mail", "", ""); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(root, "mail/AGENTS.md")
	skill := filepath.Join(root, "mail/.agents/skills/check/SKILL.md")
	defs := snapshotDefinitions(root)
	os.WriteFile(agents, []byte("# taken over\n"), 0o644)
	os.WriteFile(skill, []byte("edited in a session meanwhile\n"), 0o644)
	os.WriteFile(filepath.Join(root, "mail/tools/evil.py"), []byte("x\n"), 0o644)
	back := putBackDefinitions(root, defs, []string{"mail/AGENTS.md", "mail/tools/evil.py"})
	if strings.Join(back, ",") != "mail/AGENTS.md,mail/tools/evil.py" {
		t.Fatalf("put back %v", back)
	}
	if b, _ := os.ReadFile(agents); string(b) != "# Mail\n" {
		t.Fatalf("AGENTS.md: %q", b)
	}
	if _, err := os.Stat(filepath.Join(root, "mail/tools/evil.py")); err == nil {
		t.Fatal("a new script stayed")
	}
	if b, _ := os.ReadFile(skill); string(b) != "edited in a session meanwhile\n" {
		t.Fatal("an edit the run did not make was undone")
	}
	// Writes not known (Codex): everything since the copy.
	if back := putBackDefinitions(root, defs, nil); strings.Join(back, ",") != "mail/.agents/skills/check/SKILL.md" {
		t.Fatalf("put back %v", back)
	}
}

// An assistant whose folder the user deleted is uninstalled, not brought
// back from its catalog.
func TestDeletingAnInstalledAssistantUninstallsIt(t *testing.T) {
	m, root, _, url := installFixture(t)
	if _, err := m.InstallAssistant(url, "mail", "", ""); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(root, "mail"))
	if errs := m.restoreDefinitions(root); len(errs) > 0 {
		t.Fatal(errs)
	}
	if _, err := os.Stat(filepath.Join(root, "mail")); err == nil {
		t.Fatal("brought back")
	}
	if lock, _ := readLock(root); len(lock.Assistants) != 0 {
		t.Fatalf("still in the lock: %v", lock.Assistants)
	}
}

// A changed definition file put back as the catalog has it leaves the
// repository with the start-up commit, not only the index.
func TestStartUpCommitsWhatLeavesTheRepository(t *testing.T) {
	m, root, _, url := installFixture(t)
	if _, err := m.InstallAssistant(url, "mail", "", ""); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(root, "mail/AGENTS.md")
	os.WriteFile(agents, []byte("# Mine\n"), 0o644)
	m.commitPaths(root, "mine", []string{"mail/AGENTS.md"})
	os.WriteFile(agents, []byte("# Mail\n"), 0o644)
	m.ensureShared()
	if out, _ := exec.Command("git", "-C", root, "ls-tree", "-r", "--name-only", "HEAD", "mail/AGENTS.md").Output(); len(out) != 0 {
		t.Fatal("still committed")
	}
	if out, _ := exec.Command("git", "-C", root, "status", "--porcelain").Output(); len(out) != 0 {
		t.Fatalf("not clean:\n%s", out)
	}
}

// The default icon chosen over an author's is saved as the camera.
func TestChoosingTheDefaultIconOverTheAuthors(t *testing.T) {
	m, _, _, url := installFixture(t)
	if _, err := m.InstallAssistant(url, "mail", "", ""); err != nil {
		t.Fatal(err)
	}
	empty := ""
	a, err := m.PatchAssistant("mail", AssistantPatch{Icon: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if a.Icon != "camera" {
		t.Fatalf("icon %q", a.Icon)
	}
}
