package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Updating an installed assistant from its catalog (ADR 0031): the user's
// copy is always the catalog's plus their own changes. A file they did not
// change takes the catalog's new version; one they changed has the
// catalog's change merged into it (git merge-file, the locked version as
// the base), as a rebase does; what does not merge is theirs to decide.

// UpdateFile is what updating does to one definition file.
type UpdateFile struct {
	Rel string `json:"rel"`
	// update (the catalog's, unchanged here) | merge (the catalog's change
	// merged into the user's) | add | same (added, and already the same
	// here) | delete | kept (changed here, deleted there) | conflict
	Result string `json:"result"`
	// It runs: a script, or a routine's command changed.
	Runs bool `json:"runs,omitempty"`
	// A conflict: the catalog's version it was based on and its new one,
	// saved for merging by hand or by an agent.
	Base   string `json:"base,omitempty"`
	Theirs string `json:"theirs,omitempty"`
}

// CatalogUpdate is an assistant's update: from which commit to which, file
// by file. Commit is empty when it is up to date.
type CatalogUpdate struct {
	Dir     string       `json:"dir"`
	Catalog string       `json:"catalog"`
	From    string       `json:"from"`
	Commit  string       `json:"commit,omitempty"`
	Files   []UpdateFile `json:"files,omitempty"`
	Applied bool         `json:"applied,omitempty"`
}

func (u CatalogUpdate) Conflicts() int {
	n := 0
	for _, f := range u.Files {
		if f.Result == "conflict" {
			n++
		}
	}
	return n
}

// UpdateFromCatalog says what updating dir from its catalog does and, with
// apply, does it. A conflicting file is kept as the user has it unless
// choices names it "theirs". expect, when set, is the commit the user was
// shown: a catalog that moved on since is refused, to be checked again.
func (m *Manager) UpdateFromCatalog(dir string, apply bool, choices map[string]string, expect string) (CatalogUpdate, error) {
	root := m.assistantsRoot()
	if apply {
		m.as.lockMu.Lock()
		defer m.as.lockMu.Unlock()
	}
	lock, err := readLock(root)
	if err != nil {
		return CatalogUpdate{}, err
	}
	e, ok := lock.Assistants[dir]
	if !ok {
		return CatalogUpdate{}, fmt.Errorf("%s was not installed from a catalog", dir)
	}
	u := CatalogUpdate{Dir: dir, Catalog: e.Catalog, From: e.Commit}
	clone, head, err := m.fetchCatalog(e.Catalog)
	if err != nil {
		return u, err
	}
	if head == e.Commit {
		return u, nil
	}
	if apply && expect != "" && expect != head {
		return u, fmt.Errorf("the catalog has changed since you looked (now %s): check for updates again", short(head))
	}
	theirs, err := catalogFiles(clone, head, e.Path)
	if err != nil {
		return u, fmt.Errorf("the catalog no longer has %s: %v", e.Path, err)
	}
	base, err := catalogFiles(clone, e.Commit, e.Path)
	if err != nil {
		return u, err
	}
	baseOf, theirsOf := map[string]catalogFile{}, map[string]catalogFile{}
	for _, f := range base {
		baseOf[f.Rel] = f
	}
	for _, f := range theirs {
		theirsOf[f.Rel] = f
	}
	rels := map[string]bool{}
	for r := range baseOf {
		rels[r] = true
	}
	for r := range theirsOf {
		rels[r] = true
	}
	names := make([]string, 0, len(rels))
	for r := range rels {
		names = append(names, r)
	}
	sort.Strings(names)
	path := filepath.Join(root, dir)
	type step struct {
		rel    string
		write  []byte
		exec   bool
		remove bool
	}
	var steps []step
	for _, rel := range names {
		b, inBase := baseOf[rel]
		t, inNew := theirsOf[rel]
		if inBase && inNew && bytes.Equal(b.Data, t.Data) && b.Exec == t.Exec {
			continue
		}
		mine, readErr := os.ReadFile(filepath.Join(path, filepath.FromSlash(rel)))
		changed := readErr == nil && digest(mine) != e.Files[rel]
		f := UpdateFile{Rel: rel, Runs: runsCode(rel)}
		if rel == "assistant.toml" && inBase && inNew {
			f.Runs = commandsChanged(b.Data, t.Data)
		}
		switch {
		case inNew && !inBase:
			switch {
			case readErr != nil:
				f.Result = "add"
				steps = append(steps, step{rel: rel, write: t.Data, exec: t.Exec})
			case bytes.Equal(mine, t.Data):
				f.Result = "same"
			default:
				f.Result = "conflict"
				if choices[rel] == "theirs" {
					steps = append(steps, step{rel: rel, write: t.Data, exec: t.Exec})
				}
			}
		case inBase && !inNew:
			if changed {
				f.Result = "kept"
			} else {
				f.Result = "delete"
				steps = append(steps, step{rel: rel, remove: true})
			}
		case !changed:
			f.Result = "update"
			steps = append(steps, step{rel: rel, write: t.Data, exec: t.Exec})
		default:
			// A binary file (an icon) does not merge: theirs to choose.
			merged, conflicts, err := mergeFile(mine, b.Data, t.Data)
			if err != nil || hasNUL(mine) || hasNUL(t.Data) {
				conflicts = 1
			}
			if conflicts == 0 {
				f.Result = "merge"
				steps = append(steps, step{rel: rel, write: merged, exec: t.Exec})
			} else {
				f.Result = "conflict"
				if choices[rel] == "theirs" {
					steps = append(steps, step{rel: rel, write: t.Data, exec: t.Exec})
				}
			}
		}
		if f.Result == "conflict" {
			f.Base, f.Theirs = m.saveVersions(dir, rel, b, t, inBase)
		}
		u.Files = append(u.Files, f)
	}
	u.Commit = head
	if !apply {
		return u, nil
	}
	touched := []string{lockFile}
	for _, s := range steps {
		to := filepath.Join(path, filepath.FromSlash(s.rel))
		touched = append(touched, dir+"/"+s.rel)
		if s.remove {
			if err := os.Remove(to); err != nil && !errors.Is(err, os.ErrNotExist) {
				return u, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return u, err
		}
		mode := os.FileMode(0o644)
		if s.exec {
			mode = 0o755
		}
		if err := os.WriteFile(to, s.write, mode); err != nil {
			return u, err
		}
		os.Chmod(to, mode)
	}
	// The catalog's new versions are the base of the next update.
	e.Commit, e.Files = head, map[string]string{}
	for _, f := range theirs {
		e.Files[f.Rel] = digest(f.Data)
	}
	lock.Assistants[dir] = e
	if err := writeLock(root, lock); err != nil {
		return u, err
	}
	// A merged file is the user's change; one now as the catalog has it
	// leaves the repository (refreshManaged, in commitPaths).
	m.commitPaths(root, fmt.Sprintf("%s: update from %s@%s", dir, e.Catalog, short(head)), touched)
	m.assistantsChanged()
	m.forgetUpdate(dir)
	u.Applied = true
	return u, nil
}

// saveVersions keeps a conflicting file's two catalog versions in the data
// directory, where the user or an agent merges them.
func (m *Manager) saveVersions(dir, rel string, base, theirs catalogFile, inBase bool) (string, string) {
	to := filepath.Join(m.store.dir, "merges", dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return "", ""
	}
	b := ""
	if inBase && os.WriteFile(to+".base", base.Data, 0o600) == nil {
		b = to + ".base"
	}
	if os.WriteFile(to+".theirs", theirs.Data, 0o600) != nil {
		return b, ""
	}
	return b, to + ".theirs"
}

// runsCode: a definition file that runs on the user's computer when it
// changes: a script, Claude's settings (hooks), a skill's script.
func runsCode(rel string) bool {
	return strings.HasPrefix(rel, "tools/") || rel == ".claude/settings.json" || (strings.HasPrefix(rel, ".agents/") && !strings.HasSuffix(rel, ".md"))
}

func hasNUL(b []byte) bool { return bytes.IndexByte(b, 0) >= 0 }

// mergeFile merges the catalog's change (base → theirs) into mine, as git
// merge-file does; conflicts counts the hunks that do not merge.
func mergeFile(mine, base, theirs []byte) ([]byte, int, error) {
	dir, err := os.MkdirTemp("", "aw-merge-*")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)
	names := []string{"mine", "base", "theirs"}
	for i, b := range [][]byte{mine, base, theirs} {
		if err := os.WriteFile(filepath.Join(dir, names[i]), b, 0o600); err != nil {
			return nil, 0, err
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, 0, errors.New("git is not installed")
	}
	cmd := exec.Command(git, "merge-file", "-p", "mine", "base", "theirs")
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.Bytes(), 0, nil
	case errors.As(err, &exit) && exit.ExitCode() > 0 && exit.ExitCode() < 128:
		return out.Bytes(), exit.ExitCode(), nil
	}
	return nil, 0, err
}

// commandsChanged: whether a routine's command differs between two
// versions of an assistant.toml (what runs on the user's computer).
func commandsChanged(a, b []byte) bool {
	commands := func(data []byte) map[string]any {
		var f AssistantFile
		if _, err := toml.Decode(string(data), &f); err != nil {
			return nil
		}
		out := map[string]any{}
		for _, r := range f.Routines {
			out[r.Name] = r.Command
		}
		return out
	}
	return !reflect.DeepEqual(commands(a), commands(b))
}

// Checking for updates, once a day.

// checkUpdates fetches every catalog an assistant was installed from and
// keeps what each could update, for the page.
func (m *Manager) checkUpdates() {
	root := m.assistantsRoot()
	lock, err := readLock(root)
	if err != nil || len(lock.Assistants) == 0 {
		return
	}
	found := map[string]CatalogUpdate{}
	for dir := range lock.Assistants {
		u, err := m.UpdateFromCatalog(dir, false, nil, "")
		if err == nil && u.Commit != "" && len(u.Files) > 0 {
			found[dir] = u
		}
	}
	m.as.mu.Lock()
	m.as.updates = found
	m.as.mu.Unlock()
	m.assistantsChanged()
}

func (m *Manager) forgetUpdate(dir string) {
	m.as.mu.Lock()
	delete(m.as.updates, dir)
	m.as.mu.Unlock()
}

// updateLoop checks for updates a minute after start and then daily.
func (m *Manager) updateLoop(stop <-chan struct{}) {
	wait := time.Minute
	for {
		select {
		case <-stop:
			return
		case <-time.After(wait):
		}
		m.checkUpdates()
		wait = 24 * time.Hour
	}
}
