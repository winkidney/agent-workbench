package workbench

import (
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

// testCatalog makes a git repository of assistants, as a catalog is: one
// assistant, "mail", with its definition, the files made where it is
// installed (which the catalog may hold but are not installed from it),
// and an example config. It returns its address.
func testCatalog(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git")
	}
	work := t.TempDir()
	files := map[string]string{
		"mail/assistant.toml":                "name = \"Mail\"\nagent = \"claude\"\nagents = [\"claude\", \"codex\"]\nready = false\nicon = \"mail\"\n\n[[routines]]\nname = \"Check mail\"\nevery = \"30m\"\ncommand = \"uv run tools/fetch.py\"\nmessage = \"Sort it.\"\nenabled = false\n",
		"mail/AGENTS.md":                     "# Mail\n",
		"mail/CLAUDE.md":                     "@AGENTS.md\n",
		"mail/config.example.toml":           "days = 2\n",
		"mail/tools/fetch.py":                "print('fetch')\n",
		"mail/.agents/skills/check/SKILL.md": "---\nname: check\n---\n",
		"mail/state/loops.md":                "should not be installed\n",
		"README.md":                          "# Catalog\n",
		"other/assistant.toml":               "name = \"Other\"\nagent = \"codex\"\nagents = [\"codex\"]\n",
		"other/AGENTS.md":                    "# Other\n",
	}
	for p, body := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(work, p)), 0o755)
		os.WriteFile(filepath.Join(work, p), []byte(body), 0o644)
	}
	os.Chmod(filepath.Join(work, "mail/tools/fetch.py"), 0o755)
	os.MkdirAll(filepath.Join(work, "mail/.claude"), 0o755)
	os.Symlink("../.agents/skills", filepath.Join(work, "mail/.claude/skills"))
	git := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git(work, "init", "-q", "-b", "main")
	git(work, "add", "-A")
	git(work, "commit", "-q", "-m", "catalog")
	return work, "file://" + work
}

func installFixture(t *testing.T) (*Manager, string, string, string) {
	t.Helper()
	work, url := testCatalog(t)
	m, _ := fixture(t)
	if _, err := m.setUp(filepath.Join(t.TempDir(), "assistants"), "claude", "", "en"); err != nil {
		t.Fatal(err)
	}
	return m, m.assistantsRoot(), work, url
}

func tracked(t *testing.T, root string) map[string]bool {
	t.Helper()
	out, _ := exec.Command("git", "-C", root, "ls-files").Output()
	in := map[string]bool{}
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		in[f] = true
	}
	return in
}

func TestAssistantsInstallFromACatalog(t *testing.T) {
	m, root, _, url := installFixture(t)
	list, err := m.CatalogEntries(url)
	if err != nil || len(list) != 2 || list[0].Path != "mail" || list[0].Name != "Mail" || list[1].Agents[0] != "codex" {
		t.Fatalf("catalog %+v %v", list, err)
	}
	if w := codexWarning(AssistantFile{}, true); w == "" {
		t.Fatal("no warning for Claude's limits without a Codex sandbox")
	}
	if codexWarning(AssistantFile{Options: map[string]AgentOptions{"codex": {Permissions: ":workspace"}}}, true) != "" || codexWarning(AssistantFile{}, false) != "" || codexWarning(AssistantFile{Agents: []string{"claude"}}, true) != "" {
		t.Fatal("a warning where Codex keeps the limits, or runs not")
	}
	if _, err := m.InstallAssistant(url, "other", "", "claude"); err == nil {
		t.Fatal("installed for an agent its author did not make it for")
	}
	a, err := m.InstallAssistant(url, "mail", "", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if a.Agent != "codex" || a.Ready || a.Icon != "mail" {
		t.Fatalf("installed %+v", a)
	}
	dir := filepath.Join(root, "mail")
	// Its definition, the owner's files made from it, and links for both
	// agents; not the catalog's state.
	for _, f := range []string{"assistant.toml", "AGENTS.md", "tools/fetch.py", ".agents/skills/check/SKILL.md", "config.example.toml", "config.toml", "CLAUDE.md", "settings.toml", "state/loops.md", "log.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "state/loops.md")); strings.Contains(string(b), "should not") {
		t.Fatal("the catalog's state was installed")
	}
	// A link where links can be made; else the note that says what to do.
	if l, err := os.Readlink(filepath.Join(dir, ".claude/skills")); err != nil || l != filepath.Join("..", ".agents", "skills") {
		if _, note := os.Stat(filepath.Join(dir, ".claude/SKILLS.md")); note != nil {
			t.Fatalf("skills link %q %v", l, err)
		}
	}
	if info, _ := os.Stat(filepath.Join(dir, "tools/fetch.py")); goruntime.GOOS != "windows" && info.Mode()&0o100 == 0 {
		t.Fatal("a script lost its execute bit")
	}
	lock, _ := readLock(root)
	e := lock.Assistants["mail"]
	if e.Catalog != url || e.Path != "mail" || len(e.Commit) != 40 || len(e.Files) != 5 || e.Files["CLAUDE.md"] != "" {
		t.Fatalf("lock %+v", e)
	}
	// The repository holds the owner's files and the lock, not the
	// definition as the catalog has it.
	in := tracked(t, root)
	for _, f := range []string{"assistants.lock.toml", "mail/settings.toml", "mail/config.toml", "mail/state/loops.md", "mail/log.md"} {
		if !in[f] {
			t.Errorf("%s not committed", f)
		}
	}
	for _, f := range []string{"mail/assistant.toml", "mail/AGENTS.md", "mail/tools/fetch.py", "mail/CLAUDE.md", "mail/.claude/skills", "mail/config.example.toml"} {
		if in[f] {
			t.Errorf("%s committed", f)
		}
	}
	if out, _ := exec.Command("git", "-C", root, "status", "--porcelain").Output(); len(out) != 0 {
		t.Fatalf("not clean after install:\n%s", out)
	}
	// A definition file the owner changes is theirs: committed. Put back as
	// the catalog has it, it leaves the repository again.
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Mail, my way\n"), 0o644)
	m.commitAll(root, "mine")
	if !tracked(t, root)["mail/AGENTS.md"] {
		t.Fatal("a changed definition file was not committed")
	}
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Mail\n"), 0o644)
	m.commitAll(root, "back")
	if tracked(t, root)["mail/AGENTS.md"] {
		t.Fatal("a file as the catalog has it stayed committed")
	}
	// Another computer: the repository without the definition files gets
	// them back from the catalog.
	os.Remove(filepath.Join(dir, "tools/fetch.py"))
	os.RemoveAll(filepath.Join(dir, ".agents"))
	m.ensureShared()
	if b, err := os.ReadFile(filepath.Join(dir, "tools/fetch.py")); err != nil || string(b) != "print('fetch')\n" {
		t.Fatalf("not restored: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents/skills/check/SKILL.md")); err != nil {
		t.Fatal("skill not restored")
	}
	if out, _ := exec.Command("git", "-C", root, "status", "--porcelain").Output(); len(out) != 0 {
		t.Fatalf("not clean after restore:\n%s", out)
	}
	// Set up from a clone of the repository on another computer: the
	// definition comes back, and the clone stays clean.
	other := filepath.Join(t.TempDir(), "assistants")
	if out, err := exec.Command("git", "clone", "-q", root, other).CombinedOutput(); err != nil {
		t.Fatalf("clone: %s", out)
	}
	if _, err := os.Stat(filepath.Join(other, "mail/assistant.toml")); err == nil {
		t.Fatal("the clone has the definition")
	}
	m2, _ := fixture(t)
	if _, err := m2.setUp(other, "claude", "", "en"); err != nil {
		t.Fatal(err)
	}
	if a, err := m2.assistant("mail"); err != nil || a.Agent != "codex" || a.Catalog == nil || len(a.Catalog.Missing) != 0 {
		t.Fatalf("clone: %+v %v", a, err)
	}
	if out, _ := exec.Command("git", "-C", other, "status", "--porcelain").Output(); len(out) != 0 {
		t.Fatalf("clone not clean:\n%s", out)
	}
	// Installed twice into one folder: refused.
	if _, err := m.InstallAssistant(url, "mail", "", ""); err == nil {
		t.Fatal("installed over a folder")
	}
	if list, _ := m.CatalogEntries(url); list[0].Installed != "mail" {
		t.Fatalf("not shown installed: %+v", list[0])
	}
}

// A repository made before catalogs: its mail assistant is the catalog's
// with the owner's settings, a changed procedure, a changed script and
// data. Migrating binds it: settings move, matching files leave the
// repository, changed ones stay, and data stays.
func TestOlderRepositoriesBindToTheirCatalog(t *testing.T) {
	m, root, work, url := installFixture(t)
	dir := filepath.Join(root, "mail")
	write := func(rel, body string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755)
		os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644)
	}
	theirs, _ := os.ReadFile(filepath.Join(work, "mail/assistant.toml"))
	mine := strings.Replace(string(theirs), "ready = false", "ready = true", 1)
	mine = strings.Replace(mine, "enabled = false", "enabled = true", 1)
	mine = strings.Replace(mine, "icon = \"mail\"\n", "icon = \"bell\"\n\n[options.claude]\nmodel = \"opus\"\n", 1)
	write("assistant.toml", mine)
	write("AGENTS.md", "# Mail, with my own steps\n")
	write("tools/fetch.py", "print('mine')\n")
	write(".agents/skills/check/SKILL.md", "---\nname: check\n---\n")
	write("config.toml", "days = 7\n")
	write("state/loops.md", "- [ ] reply to Ana\n")
	write("CLAUDE.md", "@AGENTS.md\n")
	m.commitAll(root, "legacy mail")
	m.assistantsChanged()

	plan, err := m.MigrateAssistants(url, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var mail MigrationItem
	for _, it := range plan {
		if it.Dir == "mail" {
			mail = it
		}
		if it.Dir == "main" && it.Skipped == "" {
			t.Fatal("Moss was bound")
		}
	}
	if strings.Join(mail.FromCatalog, ",") != ".agents/skills/check/SKILL.md,assistant.toml" || strings.Join(mail.Changed, ",") != "AGENTS.md,tools/fetch.py" || strings.Join(mail.Added, ",") != "config.example.toml" || strings.Join(mail.Settings, ",") != "icon,options.claude,ready,routine Check mail" {
		t.Fatalf("plan %+v", mail)
	}
	if _, err := os.Stat(filepath.Join(root, lockFile)); err == nil {
		t.Fatal("a dry run wrote the lock")
	}
	// Not with uncommitted changes.
	write("notes.md", "x")
	if _, err := m.MigrateAssistants(url, true, nil); err == nil {
		t.Fatal("migrated a repository with uncommitted changes")
	}
	os.Remove(filepath.Join(dir, "notes.md"))

	if _, err := m.MigrateAssistants(url, true, []string{"mail/tools/fetch.py"}); err != nil {
		t.Fatal(err)
	}
	a, err := m.assistant("mail")
	if err != nil || !a.Ready || a.Icon != "bell" || a.Options["claude"].Model != "opus" {
		t.Fatalf("settings lost: %+v %v", a, err)
	}
	list, _ := m.Routines()
	found := false
	for _, r := range list {
		if r.Name == "Check mail" {
			found = r.Enabled
		}
	}
	if !found {
		t.Fatal("routine switch lost")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "assistant.toml")); string(b) != string(theirs) {
		t.Fatalf("assistant.toml is not the catalog's:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "tools/fetch.py")); string(b) != "print('fetch')\n" {
		t.Fatalf("the catalog's script was not taken: %q", b)
	}
	in := tracked(t, root)
	for _, f := range []string{"mail/AGENTS.md", "mail/settings.toml", "mail/config.toml", "mail/state/loops.md", "assistants.lock.toml"} {
		if !in[f] {
			t.Errorf("%s not committed", f)
		}
	}
	for _, f := range []string{"mail/assistant.toml", "mail/tools/fetch.py", "mail/.agents/skills/check/SKILL.md", "mail/CLAUDE.md", "mail/config.example.toml"} {
		if in[f] {
			t.Errorf("%s still committed", f)
		}
	}
	if out, _ := exec.Command("git", "-C", root, "status", "--porcelain").Output(); len(out) != 0 {
		t.Fatalf("not clean:\n%s", out)
	}
	// Once bound, it is not bound again.
	again, _ := m.MigrateAssistants(url, false, nil)
	for _, it := range again {
		if it.Dir == "mail" && !strings.HasPrefix(it.Skipped, "already from") {
			t.Fatalf("bound twice: %+v", it)
		}
	}
}

func TestCatalogClonesHaveShortNames(t *testing.T) {
	m, _ := fixture(t)
	a := m.catalogDir("https://github.com/winkidney/agent-workbench-assistants.git")
	b := m.catalogDir(`file://C:\Users\RUNNER~1\AppData\Local\Temp\TestOlderRepositoriesBindToTheirCatalog3498831074\001`)
	if filepath.Base(a) != "agent-workbench-assistants-"+digest([]byte("https://github.com/winkidney/agent-workbench-assistants.git"))[:8] {
		t.Fatalf("a: %s", a)
	}
	if len(filepath.Base(b)) > 41 || a == b {
		t.Fatalf("b: %s", b)
	}
}
