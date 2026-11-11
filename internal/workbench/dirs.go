package workbench

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DirEntry is one directory offered by the new-session folder browser.
type DirEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Git    bool   `json:"git"`
	Hidden bool   `json:"hidden"`
}

type DirListing struct {
	Path    string     `json:"path"`
	Parent  string     `json:"parent,omitempty"`
	Home    string     `json:"home"`
	Entries []DirEntry `json:"entries"`
}

var errOutsideHome = errors.New("folder browsing is limited to the home directory")

const dirListLimit = 500

// expandHome turns "~" and "~/…" into absolute paths.
func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}

func insideHome(path, home string) bool {
	return path == home || strings.HasPrefix(path, home+string(filepath.Separator))
}

// listDirs lists the subdirectories of path for the folder browser. Only the
// home directory tree is browsable; nothing but directory names is returned.
func listDirs(path string) (DirListing, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return DirListing{}, err
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		return DirListing{}, err
	}
	if path = expandHome(path); path == "" {
		path = home
	}
	if !filepath.IsAbs(path) {
		return DirListing{}, errors.New("path must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return DirListing{}, err
	}
	if !insideHome(resolved, home) {
		return DirListing{}, errOutsideHome
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return DirListing{}, err
	}
	if !info.IsDir() {
		return DirListing{}, fs.ErrNotExist
	}
	out := DirListing{Path: resolved, Home: home, Entries: []DirEntry{}}
	if resolved != home {
		out.Parent = filepath.Dir(resolved)
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return DirListing{}, err
	}
	for _, e := range entries {
		full := filepath.Join(resolved, e.Name())
		if e.Type()&fs.ModeSymlink != 0 {
			// Follow a symlink only when it stays inside home.
			target, err := filepath.EvalSymlinks(full)
			if err != nil || !insideHome(target, home) {
				continue
			}
			if st, err := os.Stat(target); err != nil || !st.IsDir() {
				continue
			}
		} else if !e.IsDir() {
			continue
		}
		_, gitErr := os.Stat(filepath.Join(full, ".git"))
		out.Entries = append(out.Entries, DirEntry{Name: e.Name(), Path: full, Git: gitErr == nil, Hidden: strings.HasPrefix(e.Name(), ".")})
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		return strings.ToLower(out.Entries[i].Name) < strings.ToLower(out.Entries[j].Name)
	})
	if len(out.Entries) > dirListLimit {
		out.Entries = out.Entries[:dirListLimit]
	}
	return out, nil
}
