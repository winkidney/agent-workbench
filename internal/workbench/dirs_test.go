package workbench

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListDirsStaysInsideHome(t *testing.T) {
	posixOnly(t)
	root := realTempDir(t)
	home := filepath.Join(root, "home")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{"work/app/.git", "work/notes", ".config", "Zeta"} {
		os.MkdirAll(filepath.Join(home, d), 0700)
	}
	os.MkdirAll(outside, 0700)
	os.WriteFile(filepath.Join(home, "work", "file.txt"), []byte("x"), 0600)
	os.Symlink(outside, filepath.Join(home, "work", "escape"))
	os.Symlink(filepath.Join(home, "work", "notes"), filepath.Join(home, "work", "notes-link"))
	t.Setenv("HOME", home)

	top, err := listDirs("")
	if err != nil || top.Path != home || top.Parent != "" {
		t.Fatalf("home listing %+v %v", top, err)
	}
	var names []string
	for _, e := range top.Entries {
		names = append(names, e.Name)
		if e.Name == ".config" && !e.Hidden {
			t.Fatal("dot directory not marked hidden")
		}
	}
	if strings.Join(names, ",") != ".config,work,Zeta" {
		t.Fatalf("entries %v", names)
	}
	work, err := listDirs("~/work")
	if err != nil || work.Parent != home {
		t.Fatalf("~ expansion: %+v %v", work, err)
	}
	got := map[string]DirEntry{}
	for _, e := range work.Entries {
		got[e.Name] = e
	}
	if !got["app"].Git || got["notes"].Git || got["notes-link"].Name == "" {
		t.Fatalf("work entries %+v", got)
	}
	if _, ok := got["escape"]; ok {
		t.Fatal("symlink leaving home was listed")
	}
	if _, ok := got["file.txt"]; ok {
		t.Fatal("file listed")
	}
	if _, err := listDirs(outside); !errors.Is(err, errOutsideHome) {
		t.Fatalf("outside home: %v", err)
	}
	if _, err := listDirs(filepath.Join(home, "work", "escape")); !errors.Is(err, errOutsideHome) {
		t.Fatalf("symlink escape: %v", err)
	}
	if _, err := listDirs(filepath.Join(home, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
}

func TestDirsRouteRequiresLogin(t *testing.T) {
	posixOnly(t)
	t.Setenv("HOME", t.TempDir())
	m, _ := fixture(t)
	s := pairedServer(m, nil, "agent.localhost:17880", "pair")
	if w := lan(t, s, "GET", "/api/dirs", "", nil); w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	cookie := request(t, s, s.Host, "POST", "/api/login", `{"code":"pair"}`, nil).Result().Cookies()[0]
	if w := request(t, s, s.Host, "GET", "/api/dirs?path=/etc", "", cookie); w.Code != 403 {
		t.Fatalf("outside home: %d", w.Code)
	}
	w := request(t, s, s.Host, "GET", "/api/dirs", "", cookie)
	var v DirListing
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Home == "" {
		t.Fatalf("home: %d %s", w.Code, w.Body.String())
	}
}

// Without a typed name the CLI keeps its own title; the first message is only
// the display title until the native record exists.
func TestCreateSessionNamesNativeOnlyWhenExplicit(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		m, p := fixture(t)
		title := ""
		if explicit {
			title = "Release prep"
		}
		s, err := m.CreateSession("codex", t.TempDir(), title, "  Fix the flaky\n login test  ")
		if err != nil {
			t.Fatal(err)
		}
		want := "Fix the flaky login test"
		if explicit {
			want = "Release prep"
		}
		if s.Title != want {
			t.Fatalf("title %q", s.Title)
		}
		_ = m.Start(s.ID)
		r := m.sessions[s.ID]
		m.receive(s.ID, r.generation, map[string]any{"id": "init", "result": map[string]any{}})
		m.receive(s.ID, r.generation, map[string]any{"id": "thread", "result": map[string]any{"thread": map[string]any{"id": "native"}}})
		named := false
		for _, in := range p.in {
			if in["method"] == "thread/name/set" {
				named = true
			}
		}
		if named != explicit {
			t.Fatalf("explicit=%v named=%v", explicit, named)
		}
	}
}
