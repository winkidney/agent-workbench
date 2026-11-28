package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Catalogs (ADR 0031): git repositories of assistant definitions, one
// folder with an assistant.toml each, that the user installs assistants
// from. What was installed is in assistants.lock.toml at the top of the
// repository of assistants: where from, at which commit, and each
// definition file's digest. A definition file as the catalog has it is not
// committed there (.git/info/exclude names it); one the user changed is, so
// the repository holds the user's data and changes only, and an install
// comes back from the catalog on another computer.

// DefaultCatalog is the catalog offered when the user has named none.
const DefaultCatalog = "https://github.com/winkidney/agent-workbench-assistants.git"

// CatalogSource is a catalog the user installs from; Checkout, when set,
// is the user's own clone of it, where Publish writes (phase 3).
type CatalogSource struct {
	URL      string `json:"url"`
	Checkout string `json:"checkout,omitempty"`
}

func (m *Manager) catalogSources() []CatalogSource {
	var v []CatalogSource
	m.store.readSetting("catalogs", &v)
	if len(v) == 0 {
		v = []CatalogSource{{URL: DefaultCatalog}}
	}
	return v
}

func (m *Manager) SetCatalogSources(v []CatalogSource) error {
	for _, c := range v {
		if strings.TrimSpace(c.URL) == "" {
			return errors.New("a catalog needs its git address")
		}
	}
	return m.store.setting("catalogs", v)
}

var catalogDirRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// catalogDir is where the workbench keeps its clone of a catalog: its
// name and a digest of its address, short enough for Windows' path limit.
func (m *Manager) catalogDir(url string) string {
	base := strings.TrimSuffix(strings.TrimRight(url, "/\\"), ".git")
	if i := strings.LastIndexAny(base, "/\\:"); i >= 0 {
		base = base[i+1:]
	}
	name := strings.Trim(catalogDirRE.ReplaceAllString(base, "_"), "_")
	if len(name) > 32 {
		name = name[:32]
	}
	return filepath.Join(m.store.dir, "catalogs", name+"-"+digest([]byte(url))[:8])
}

// fetchCatalog brings the workbench's clone of a catalog up to date and
// returns it and its newest commit.
func (m *Manager) fetchCatalog(url string) (string, string, error) {
	m.as.catalog.Lock()
	defer m.as.catalog.Unlock()
	dir := m.catalogDir(url)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
			return "", "", err
		}
		os.RemoveAll(dir)
		if _, err := gitRun(filepath.Dir(dir), append(slowAbort, "clone", "-q", "--no-checkout", "--", url, dir)...); err != nil {
			return "", "", fmt.Errorf("could not fetch the catalog %s: %v", url, err)
		}
	} else if _, err := gitRun(dir, append(slowAbort, "fetch", "-q", "origin")...); err != nil {
		return dir, "", fmt.Errorf("could not fetch the catalog %s: %v", url, err)
	}
	head, err := gitRun(dir, "rev-parse", "origin/HEAD")
	if err != nil {
		head, err = gitRun(dir, "rev-parse", "FETCH_HEAD")
	}
	if err != nil {
		return dir, "", err
	}
	return dir, strings.TrimSpace(head), nil
}

// catalogFile is a definition file of an assistant in a catalog at a
// commit: its path in the assistant's folder, content and whether it runs.
type catalogFile struct {
	Rel  string
	Data []byte
	Exec bool
}

// catalogFiles reads an assistant's definition files from a catalog clone
// at a commit; links and generated files (defs.go) are left out.
func catalogFiles(dir, commit, path string) ([]catalogFile, error) {
	out, err := gitRun(dir, "ls-tree", "-r", "-z", commit, "--", path+"/")
	if err != nil {
		return nil, err
	}
	var files []catalogFile
	for _, entry := range strings.Split(out, "\x00") {
		meta, name, ok := strings.Cut(entry, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		if len(f) < 3 || f[1] != "blob" || f[0] == "120000" {
			continue
		}
		rel := strings.TrimPrefix(name, path+"/")
		if !safeRel(rel) {
			return nil, fmt.Errorf("the catalog names a file outside the assistant's folder: %q", rel)
		}
		if generatedFile(rel) || !definitionFile(rel) {
			continue
		}
		data, err := gitBytes(dir, "show", commit+":"+name)
		if err != nil {
			return nil, err
		}
		files = append(files, catalogFile{Rel: rel, Data: data, Exec: f[0] == "100755"})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("the catalog has no assistant %q", path)
	}
	return files, nil
}

// CatalogEntry is an assistant a catalog offers, for the page.
type CatalogEntry struct {
	Catalog      string   `json:"catalog"`
	Path         string   `json:"path"`
	Commit       string   `json:"commit"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Requires     []string `json:"requires,omitempty"`
	Agent        string   `json:"agent"`
	Agents       []string `json:"agents,omitempty"`
	Icon         string   `json:"icon,omitempty"`
	// The folder it is installed in, if it is.
	Installed string `json:"installed,omitempty"`
	// What Codex does not keep as Claude does: Claude's settings deny it
	// things, and its Codex options name no sandbox.
	CodexWarning string `json:"codexWarning,omitempty"`
}

// codexWarning: an assistant whose Claude settings deny it things should
// run Codex in a sandbox too (ADR 0031, agents alike).
func codexWarning(f AssistantFile, claudeDenies bool) string {
	if !claudeDenies || f.Options["codex"].Permissions != "" || (len(f.Agents) > 0 && !inList(f.Agents, "codex")) {
		return ""
	}
	return "Its Claude settings deny it some tools, but its Codex options name no sandbox (permissions): with Codex it runs without those limits."
}

// CatalogEntries lists what a catalog offers at its newest commit.
func (m *Manager) CatalogEntries(url string) ([]CatalogEntry, error) {
	dir, commit, err := m.fetchCatalog(url)
	if err != nil {
		return nil, err
	}
	out, err := gitRun(dir, "ls-tree", "-z", "--name-only", commit)
	if err != nil {
		return nil, err
	}
	lock, _ := readLock(m.assistantsRoot())
	installed := map[string]string{}
	for d, e := range lock.Assistants {
		if e.Catalog == url {
			installed[e.Path] = d
		}
	}
	var list []CatalogEntry
	for _, name := range strings.Split(out, "\x00") {
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		data, err := gitBytes(dir, "show", commit+":"+name+"/assistant.toml")
		if err != nil {
			continue
		}
		var f AssistantFile
		if _, err := toml.Decode(string(data), &f); err != nil || f.Name == "" {
			continue
		}
		settings, _ := gitBytes(dir, "show", commit+":"+name+"/.claude/settings.json")
		denies := bytes.Contains(settings, []byte(`"deny"`))
		list = append(list, CatalogEntry{Catalog: url, Path: name, Commit: commit, Name: f.Name, Description: f.Description, Capabilities: f.Capabilities, Requires: f.Requires, Agent: f.Agent, Agents: f.Agents, Icon: f.Icon, Installed: installed[name], CodexWarning: codexWarning(f, denies)})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	return list, nil
}

// gitBytes is a git command's output as it is: a file's content, without
// what git says on stderr.
func gitBytes(dir string, args ...string) ([]byte, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git is not installed")
	}
	cmd := exec.Command(git, args...)
	cmd.Dir = dir
	cmd.Env = append(routineEnv(), "GIT_TERMINAL_PROMPT=0")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}

// The lock file.

const lockFile = "assistants.lock.toml"

type Lock struct {
	Assistants map[string]LockEntry `toml:"assistants"`
}

// LockEntry is an installed assistant: its catalog, folder there, commit,
// and the digest of each definition file as that commit has it.
type LockEntry struct {
	Catalog string            `toml:"catalog"`
	Path    string            `toml:"path"`
	Commit  string            `toml:"commit"`
	Files   map[string]string `toml:"files"`
}

func readLock(root string) (Lock, error) {
	var l Lock
	if root == "" {
		return l, nil
	}
	_, err := toml.DecodeFile(filepath.Join(root, lockFile), &l)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if l.Assistants == nil {
		l.Assistants = map[string]LockEntry{}
	}
	return l, err
}

const lockHeader = `# The assistants installed from a catalog (Agent Workbench, ADR 0031): where
# each came from and the digest of each definition file there. A file that
# still matches is not committed in this repository; Agent Workbench puts it
# back from the catalog when it is missing.

`

func writeLock(root string, l Lock) error {
	var b bytes.Buffer
	b.WriteString(lockHeader)
	enc := toml.NewEncoder(&b)
	enc.Indent = ""
	if err := enc.Encode(l); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, lockFile), b.Bytes(), 0o644)
}

// Keeping the repository to the user's data.

const (
	excludeBegin = "# Agent Workbench: definition files as their catalog has them (ADR 0031)"
	excludeEnd   = "# end of Agent Workbench's definition files"
)

// refreshManaged names, in .git/info/exclude, the installed assistants'
// definition files that match their catalog and the files made where they
// are installed, and takes them out of the index if they were committed.
// It returns what it took out of the index (the caller commits that).
func refreshManaged(root string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return nil, nil
	}
	lock, err := readLock(root)
	if err != nil {
		return nil, err
	}
	var managed []string
	dirs := make([]string, 0, len(lock.Assistants))
	for d := range lock.Assistants {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		e := lock.Assistants[d]
		rels := make([]string, 0, len(e.Files))
		for rel := range e.Files {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		for _, rel := range rels {
			data, err := os.ReadFile(filepath.Join(root, d, filepath.FromSlash(rel)))
			if err == nil && digest(data) == e.Files[rel] {
				managed = append(managed, d+"/"+rel)
			}
		}
		for _, gen := range []string{"CLAUDE.md", ".claude/skills", ".claude/SKILLS.md", ".agents/skills/.gitkeep"} {
			if _, ok := e.Files[gen]; !ok {
				managed = append(managed, d+"/"+gen)
			}
		}
	}
	if err := writeExclude(root, managed); err != nil {
		return nil, err
	}
	tracked, err := gitRun(root, "ls-files", "-z")
	if err != nil {
		return nil, nil
	}
	in := map[string]bool{}
	for _, f := range strings.Split(tracked, "\x00") {
		in[f] = true
	}
	var drop []string
	for _, f := range managed {
		if in[f] {
			drop = append(drop, f)
		}
	}
	if len(drop) == 0 {
		return nil, nil
	}
	if _, err := gitRun(root, append([]string{"rm", "-q", "--cached", "--"}, drop...)...); err != nil {
		return nil, err
	}
	return drop, nil
}

func writeExclude(root string, paths []string) error {
	path := filepath.Join(root, ".git", "info", "exclude")
	old, _ := os.ReadFile(path)
	text := string(old)
	if i := strings.Index(text, excludeBegin); i >= 0 {
		if j := strings.Index(text[i:], excludeEnd); j >= 0 {
			text = text[:i] + strings.TrimPrefix(text[i+j+len(excludeEnd):], "\n")
		}
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(text, "\n"))
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	b.WriteString(excludeBegin + "\n")
	for _, p := range paths {
		b.WriteString("/" + gitignoreEscape(p) + "\n")
	}
	// Made for Codex from AGENTS.md and AGENTS.local.md (defs.go), anywhere.
	b.WriteString(agentsOverride + "\n")
	b.WriteString(excludeEnd + "\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// gitignoreEscape: a path matches only itself in an exclude file.
func gitignoreEscape(p string) string {
	r := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `!`, `\!`, `#`, `\#`)
	return r.Replace(p)
}

// Installing.

// writeDefinition writes an assistant's definition files and what is made
// where it is installed: the link and file that let Claude Code read what
// Codex reads (defs.go).
func writeDefinition(path string, files []catalogFile) error {
	for _, f := range files {
		to := filepath.Join(path, filepath.FromSlash(f.Rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.Exec {
			mode = 0o755
		}
		if err := os.WriteFile(to, f.Data, mode); err != nil {
			return err
		}
		// WriteFile keeps an existing file's mode.
		os.Chmod(to, mode)
	}
	if err := writeNew(filepath.Join(path, "CLAUDE.md"), []byte("@AGENTS.md\n")); err != nil {
		return err
	}
	if err := skillsLink(path); err != nil {
		return err
	}
	return writeAgentFiles(path)
}

// InstallAssistant installs an assistant from a catalog into the folder dir
// ("" for the catalog's name), for agent ("" for its author's choice).
func (m *Manager) InstallAssistant(url, path, dir, agent string) (Assistant, error) {
	m.as.lockMu.Lock()
	defer m.as.lockMu.Unlock()
	root := m.assistantsRoot()
	if root == "" {
		return Assistant{}, errors.New("set up assistants first (Settings → Assistants)")
	}
	if dir == "" {
		dir = path
	}
	if err := validAssistantDir(dir); err != nil {
		return Assistant{}, err
	}
	target := filepath.Join(root, dir)
	if _, err := os.Stat(target); err == nil {
		return Assistant{}, fmt.Errorf("%s already exists", dir)
	}
	clone, commit, err := m.fetchCatalog(url)
	if err != nil {
		return Assistant{}, err
	}
	files, err := catalogFiles(clone, commit, path)
	if err != nil {
		return Assistant{}, err
	}
	var f AssistantFile
	for _, c := range files {
		if c.Rel == "assistant.toml" {
			if _, err := toml.Decode(string(c.Data), &f); err != nil {
				return Assistant{}, fmt.Errorf("its assistant.toml: %v", err)
			}
		}
	}
	if agent != "" && len(f.Agents) > 0 && !inList(f.Agents, agent) {
		return Assistant{}, fmt.Errorf("its author made it for %s", strings.Join(f.Agents, " and "))
	}
	if err := writeDefinition(target, files); err != nil {
		os.RemoveAll(target)
		return Assistant{}, err
	}
	// The owner's files: settings, the settings its scripts read, state.
	if agent != "" && agent != f.Agent {
		if err := writeAssistantSettings(target, AssistantSettings{Agent: agent}); err != nil {
			return Assistant{}, err
		}
	}
	if b, err := os.ReadFile(filepath.Join(target, "config.example.toml")); err == nil {
		writeNew(filepath.Join(target, "config.toml"), b)
	}
	for to, from := range map[string]string{"state/loops.md": "each/loops.md", "state/handoff.md": "each/handoff.md", "log.md": "each/log.md"} {
		if err := writeNew(filepath.Join(target, filepath.FromSlash(to)), mossFile(from)); err != nil {
			return Assistant{}, err
		}
	}
	if _, err := readAssistant(filepath.Join(target, "assistant.toml")); err != nil {
		os.RemoveAll(target)
		return Assistant{}, fmt.Errorf("its assistant.toml: %v", err)
	}
	lock, err := readLock(root)
	if err != nil {
		return Assistant{}, err
	}
	entry := LockEntry{Catalog: url, Path: path, Commit: commit, Files: map[string]string{}}
	for _, c := range files {
		entry.Files[c.Rel] = digest(c.Data)
	}
	lock.Assistants[dir] = entry
	if err := writeLock(root, lock); err != nil {
		return Assistant{}, err
	}
	m.commitPaths(root, fmt.Sprintf("%s: install %s from %s@%s", dir, f.Name, url, short(commit)), []string{dir, lockFile})
	m.assistantsChanged()
	return m.assistant(dir)
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func inList(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

var assistantDirRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func validAssistantDir(dir string) error {
	if !assistantDirRE.MatchString(dir) {
		return errors.New("folder: lowercase letters, digits and dashes")
	}
	return nil
}

// restoreDefinitions writes back, from their catalogs, the definition files
// an installed assistant is missing (a clone of the repository on another
// computer has only the user's files). It reports what it could not.
func (m *Manager) restoreDefinitions(root string) []error {
	m.as.lockMu.Lock()
	defer m.as.lockMu.Unlock()
	lock, err := readLock(root)
	if err != nil {
		return []error{err}
	}
	var errs []error
	removed := false
	for d, e := range lock.Assistants {
		if !lockEntryValid(d, e) {
			errs = append(errs, fmt.Errorf("%s: not a valid entry of %s", d, lockFile))
			continue
		}
		path := filepath.Join(root, d)
		// Its folder gone (the user deleted the assistant): uninstalled, not
		// brought back. A clone from another computer keeps the folder for
		// its data and settings.
		if _, err := os.Stat(path); err != nil {
			delete(lock.Assistants, d)
			removed = true
			continue
		}
		var missing bool
		for rel := range e.Files {
			if _, err := os.Stat(filepath.Join(path, filepath.FromSlash(rel))); err != nil {
				missing = true
				break
			}
		}
		if !missing {
			continue
		}
		clone, _, err := m.fetchCatalog(e.Catalog)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", d, err))
			continue
		}
		files, err := catalogFiles(clone, e.Commit, e.Path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", d, err))
			continue
		}
		var put []catalogFile
		for _, f := range files {
			if _, err := os.Stat(filepath.Join(path, filepath.FromSlash(f.Rel))); err != nil {
				put = append(put, f)
			}
		}
		if err := writeDefinition(path, put); err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", d, err))
		}
	}
	if removed {
		if err := writeLock(root, lock); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// AssistantCatalog is where an installed assistant came from, for the page.
type AssistantCatalog struct {
	URL    string `json:"url"`
	Path   string `json:"path"`
	Commit string `json:"commit"`
	// Its definition files the user changed (committed as theirs), and
	// those missing here.
	Changed []string `json:"changed,omitempty"`
	Missing []string `json:"missing,omitempty"`
	// What its catalog could update (checked daily).
	Update *CatalogUpdate `json:"update,omitempty"`
}

// withCatalogs says, of each installed assistant, where it came from.
func withCatalogs(root string, list []Assistant) {
	lock, err := readLock(root)
	if err != nil || len(lock.Assistants) == 0 {
		return
	}
	for i, a := range list {
		e, ok := lock.Assistants[a.Dir]
		if !ok {
			continue
		}
		c := &AssistantCatalog{URL: e.Catalog, Path: e.Path, Commit: e.Commit}
		rels := make([]string, 0, len(e.Files))
		for rel := range e.Files {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		for _, rel := range rels {
			data, err := os.ReadFile(filepath.Join(a.Path, filepath.FromSlash(rel)))
			switch {
			case err != nil:
				c.Missing = append(c.Missing, rel)
			case digest(data) != e.Files[rel]:
				c.Changed = append(c.Changed, rel)
			}
		}
		list[i].Catalog = c
	}
}

// safeRel: a path inside an assistant's folder, as a catalog or the lock
// names it: relative, slash separated, no "..", no backslash, not an
// option.
func safeRel(rel string) bool {
	if rel == "" || strings.ContainsAny(rel, "\\\x00") || strings.HasPrefix(rel, "-") || strings.HasPrefix(rel, "/") {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return filepath.IsLocal(filepath.FromSlash(rel))
}

// lockEntryValid: an entry of the lock names a folder of the repository and
// files inside it (the lock is a file anyone with the repository edits).
func lockEntryValid(dir string, e LockEntry) bool {
	if validAssistantDir(dir) != nil {
		return false
	}
	for rel := range e.Files {
		if !safeRel(rel) {
			return false
		}
	}
	return true
}

// slowAbort ends a fetch that stalls (a catalog's server that stops
// answering), instead of holding the catalog's lock.
var slowAbort = []string{"-c", "protocol.file.allow=always", "-c", "http.lowSpeedLimit=1000", "-c", "http.lowSpeedTime=60"}

// Background runs and definition files (ADR 0021, 0027): a definition file
// as its catalog has it is not tracked, so git cannot say a run changed it.
// The run's writes to them are put back from a copy taken before.

type savedDef struct {
	data []byte
	mode os.FileMode
}

// snapshotDefinitions copies the installed assistants' definition files.
func snapshotDefinitions(root string) map[string]savedDef {
	lock, err := readLock(root)
	if err != nil {
		return nil
	}
	out := map[string]savedDef{}
	for d, e := range lock.Assistants {
		if !lockEntryValid(d, e) {
			continue
		}
		files, _ := definitionFiles(filepath.Join(root, d))
		for _, rel := range files {
			p := filepath.Join(root, d, filepath.FromSlash(rel))
			if info, err := os.Stat(p); err == nil {
				if b, err := os.ReadFile(p); err == nil {
					out[d+"/"+rel] = savedDef{b, info.Mode().Perm()}
				}
			}
		}
	}
	return out
}

// putBackDefinitions undoes a run's changes to the definition files saved
// before it: changed or removed ones are written back, new ones in an
// installed assistant's definition removed. With only set, just those
// paths. It returns what it put back.
func putBackDefinitions(root string, saved map[string]savedDef, only []string) []string {
	if saved == nil || only != nil && len(only) == 0 {
		return nil
	}
	take := func(p string) bool { return only == nil || inList(only, p) }
	var back []string
	for rel, s := range saved {
		if !take(rel) {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		if b, err := os.ReadFile(p); err == nil && bytes.Equal(b, s.data) {
			continue
		}
		os.MkdirAll(filepath.Dir(p), 0o755)
		if os.WriteFile(p, s.data, s.mode) == nil {
			os.Chmod(p, s.mode)
			back = append(back, rel)
		}
	}
	lock, _ := readLock(root)
	for d := range lock.Assistants {
		files, _ := definitionFiles(filepath.Join(root, d))
		for _, rel := range files {
			if _, ok := saved[d+"/"+rel]; !ok && take(d+"/"+rel) {
				if os.Remove(filepath.Join(root, d, filepath.FromSlash(rel))) == nil {
					back = append(back, d+"/"+rel)
				}
			}
		}
	}
	sort.Strings(back)
	return back
}
