package workbench

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

// The workbench commits what it changed, never an assistant's work in
// progress: setting changes, icons, shared files and installs leave other
// changes uncommitted.
func TestWorkbenchCommitsOnlyWhatItChanged(t *testing.T) {
	m, root, _, url := installFixture(t)
	if _, err := m.InstallAssistant(url, "mail", "", ""); err != nil {
		t.Fatal(err)
	}
	dirty := filepath.Join(root, "mail/state/loops.md")
	os.WriteFile(dirty, []byte("- [ ] half written\n"), 0o644)
	os.WriteFile(filepath.Join(root, "main/state/handoff.md"), []byte("moss at work\n"), 0o644)
	stillDirty := func(step string) {
		t.Helper()
		out, _ := exec.Command("git", "-C", root, "status", "--porcelain").Output()
		for _, f := range []string{"mail/state/loops.md", "main/state/handoff.md"} {
			if !strings.Contains(string(out), f) {
				t.Fatalf("%s committed %s:\n%s", step, f, out)
			}
		}
	}
	bell := "bell"
	if _, err := m.PatchAssistant("mail", AssistantPatch{Icon: &bell}); err != nil {
		t.Fatal(err)
	}
	stillDirty("a setting")
	if c := lastCommit(t, root); !strings.Contains(c, "mail/settings.toml") || strings.Contains(c, "state/") {
		t.Fatalf("commit %q", c)
	}
	// Shared files written again at start (here the credentials block of
	// .gitignore, which the start puts back).
	gi := filepath.Join(root, ".gitignore")
	b, _ := os.ReadFile(gi)
	os.WriteFile(gi, []byte(strings.Replace(string(b), "*.pem\n", "", 1)), 0o644)
	m.commitPaths(root, "drop a line", []string{".gitignore"})
	m.ensureShared()
	stillDirty("shared files")
	if c := lastCommit(t, root); !strings.Contains(c, ".gitignore") || strings.Contains(c, "state/") {
		t.Fatalf("commit %q", c)
	}
	// A definition file put back as the catalog has it leaves the index in
	// a commit of its own paths; it is not added back.
	agents := filepath.Join(root, "mail/AGENTS.md")
	os.WriteFile(agents, []byte("# mine\n"), 0o644)
	m.commitPaths(root, "mine", []string{"mail/AGENTS.md"})
	if !tracked(t, root)["mail/AGENTS.md"] {
		t.Fatal("the change was not committed")
	}
	os.WriteFile(agents, []byte("# Mail\n"), 0o644)
	m.commitPaths(root, "back", []string{"mail/AGENTS.md"})
	if tracked(t, root)["mail/AGENTS.md"] {
		t.Fatal("put back as the catalog has it, it stayed committed")
	}
	stillDirty("putting a file back")
	// An uploaded icon goes in with its setting; removing it takes it out.
	os.WriteFile(filepath.Join(root, "mail/icon.png"), squarePNG(t, 128, 128), 0o644)
	png := "icon.png"
	if _, err := m.PatchAssistant("mail", AssistantPatch{Icon: &png}); err != nil {
		t.Fatal(err)
	}
	if !tracked(t, root)["mail/icon.png"] {
		t.Fatal("the icon was not committed")
	}
	os.Remove(filepath.Join(root, "mail/icon.png"))
	camera := "camera"
	if _, err := m.PatchAssistant("mail", AssistantPatch{Icon: &camera}); err != nil {
		t.Fatal(err)
	}
	if tracked(t, root)["mail/icon.png"] {
		t.Fatal("the removed icon stayed committed")
	}
	stillDirty("icons")
}

// Updating keeps the user's copy the catalog's plus their changes: what
// they did not change follows the catalog, their changes take the
// catalog's merged in, and what does not merge stays theirs unless they
// choose the catalog's.
func TestAssistantsFollowTheirCatalogWithTheirChanges(t *testing.T) {
	m, root, work, url := installFixture(t)
	writeCat := func(rel, body string) {
		os.MkdirAll(filepath.Dir(filepath.Join(work, rel)), 0o755)
		os.WriteFile(filepath.Join(work, rel), []byte(body), 0o644)
	}
	writeCat("mail/tools/fetch.py", "import sys\n\nprint('fetch')\n\nprint('done')\n")
	writeCat("mail/.agents/skills/check/SKILL.md", "---\nname: check\n---\nstep one\n")
	gitIn := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", work, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	gitIn("add", "-A")
	gitIn("commit", "-q", "-m", "longer files")
	if _, err := m.InstallAssistant(url, "mail", "", ""); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "mail")
	// The user's changes: a line of their own in the script (merges), and
	// the same line of the skill the catalog changes too (conflicts).
	os.WriteFile(filepath.Join(dir, "tools/fetch.py"), []byte("import sys\n\nprint('fetch')\n\nprint('done')\nprint('mine')\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".agents/skills/check/SKILL.md"), []byte("---\nname: check\n---\nmy step one\n"), 0o644)
	m.commitPaths(root, "mine", []string{"mail"})
	if u, err := m.UpdateFromCatalog("mail", false, nil, ""); err != nil || u.Commit != "" {
		t.Fatalf("an update with nothing new: %+v %v", u, err)
	}
	// The catalog's next version.
	writeCat("mail/AGENTS.md", "# Mail, version two\n")
	writeCat("mail/tools/fetch.py", "import os, sys\n\nprint('fetch')\n\nprint('done')\n")
	writeCat("mail/.agents/skills/check/SKILL.md", "---\nname: check\n---\nstep one, better\n")
	writeCat("mail/tools/new.py", "print('new')\n")
	os.Remove(filepath.Join(work, "mail/config.example.toml"))
	toml, _ := os.ReadFile(filepath.Join(work, "mail/assistant.toml"))
	writeCat("mail/assistant.toml", strings.Replace(string(toml), "uv run tools/fetch.py", "uv run tools/fetch.py --all", 1))
	gitIn("add", "-A")
	gitIn("commit", "-q", "-m", "version two")

	check, err := m.UpdateFromCatalog("mail", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	runs := map[string]bool{}
	for _, f := range check.Files {
		got[f.Rel], runs[f.Rel] = f.Result, f.Runs
	}
	want := map[string]string{"AGENTS.md": "update", "tools/fetch.py": "merge", ".agents/skills/check/SKILL.md": "conflict", "tools/new.py": "add", "config.example.toml": "delete", "assistant.toml": "update"}
	if !reflect.DeepEqual(got, want) || !runs["assistant.toml"] || !runs["tools/new.py"] || runs["AGENTS.md"] || check.Conflicts() != 1 {
		t.Fatalf("check %v runs %v", got, runs)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md")); string(b) != "# Mail\n" {
		t.Fatal("a check changed a file")
	}
	// Not what was shown: the catalog moved on since.
	if _, err := m.UpdateFromCatalog("mail", true, nil, "0000000000000000000000000000000000000000"); err == nil || !strings.Contains(err.Error(), "check for updates again") {
		t.Fatalf("applied an update the user did not see: %v", err)
	}
	u, err := m.UpdateFromCatalog("mail", true, nil, check.Commit)
	if err != nil || !u.Applied {
		t.Fatalf("apply %+v %v", u, err)
	}
	read := func(rel string) string {
		b, _ := os.ReadFile(filepath.Join(dir, rel))
		return string(b)
	}
	if read("AGENTS.md") != "# Mail, version two\n" || read("tools/new.py") != "print('new')\n" {
		t.Fatal("the catalog's files were not taken")
	}
	if read("tools/fetch.py") != "import os, sys\n\nprint('fetch')\n\nprint('done')\nprint('mine')\n" {
		t.Fatalf("not merged: %q", read("tools/fetch.py"))
	}
	if read(".agents/skills/check/SKILL.md") != "---\nname: check\n---\nmy step one\n" {
		t.Fatal("a conflict did not keep the user's file")
	}
	if _, err := os.Stat(filepath.Join(dir, "config.example.toml")); err == nil {
		t.Fatal("a file the catalog deleted stayed")
	}
	in := tracked(t, root)
	if in["mail/AGENTS.md"] || in["mail/tools/new.py"] || !in["mail/tools/fetch.py"] || !in["mail/.agents/skills/check/SKILL.md"] {
		t.Fatalf("tracking after the update: %v", in)
	}
	lock, _ := readLock(root)
	if lock.Assistants["mail"].Commit != u.Commit || lock.Assistants["mail"].Files["tools/new.py"] == "" {
		t.Fatalf("lock %+v", lock.Assistants["mail"])
	}
	if out, _ := exec.Command("git", "-C", root, "status", "--porcelain").Output(); len(out) != 0 {
		t.Fatalf("not clean:\n%s", out)
	}
	// Next time, the conflicting file is chosen as the catalog's.
	writeCat("mail/.agents/skills/check/SKILL.md", "---\nname: check\n---\nstep one, best\n")
	gitIn("add", "-A")
	gitIn("commit", "-q", "-m", "version three")
	if _, err := m.UpdateFromCatalog("mail", true, map[string]string{".agents/skills/check/SKILL.md": "theirs"}, ""); err != nil {
		t.Fatal(err)
	}
	if read(".agents/skills/check/SKILL.md") != "---\nname: check\n---\nstep one, best\n" || tracked(t, root)["mail/.agents/skills/check/SKILL.md"] {
		t.Fatal("choosing the catalog's did not take it")
	}
}

// An agent (Moss) lists and installs assistants from a catalog, and checks
// for an update, through the local commands' API; only with its token.
func TestAgentsInstallFromACatalog(t *testing.T) {
	m, _, _, url := installFixture(t)
	m.SetCatalogSources([]CatalogSource{{URL: url}})
	s := NewServer(m, nil, "agent.localhost:17880", "")
	if err := s.writeAgentToken(); err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body, tok string) (int, string) {
		r := httptest.NewRequest(method, "http://127.0.0.1:17880/api/local/"+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:4000"
		if tok != "" {
			r.Header.Set(agentHeader, tok)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	if code, _ := call("GET", "assistants/catalog", "", ""); code != 403 {
		t.Fatalf("without the token: %d", code)
	}
	code, body := call("GET", "assistants/catalog", "", s.agentToken)
	if code != 200 || !strings.Contains(body, `"path":"mail"`) {
		t.Fatalf("catalog %d %s", code, body)
	}
	code, body = call("POST", "assistants/install", `{"path":"mail","agent":"codex"}`, s.agentToken)
	if code != 200 || !strings.Contains(body, `"agent":"codex"`) {
		t.Fatalf("install %d %s", code, body)
	}
	code, body = call("POST", "assistants/mail/update", `{"apply":false}`, s.agentToken)
	if code != 200 || strings.Contains(body, `"commit"`) {
		t.Fatalf("update check %d %s", code, body)
	}
	if code, _ := call("POST", "assistants/install", `{"path":"mail"}`, s.agentToken); code != 400 {
		t.Fatalf("installed over a folder: %d", code)
	}
}
