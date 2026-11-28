package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/winkidney/agent-workbench/internal/workbench"
)

// program is a stand-in for agent-workbench: it prints its version, and its
// install records a server of that version (or fails).
func program(ver string, installs bool) string {
	install := `exit 1`
	if installs {
		install = `printf '{"pid":1,"listen":"127.0.0.1:17999","version":"` + ver + `"}' > "$3/server.json"`
	}
	return "#!/bin/sh\ncase \"$1\" in\nversion) echo " + ver + ";;\ninstall) " + install + ";;\nesac\n"
}

// updateFixture: an installed workbench (its unit names its data
// directory, its program in ~/.local/bin) and a release on a stand-in
// GitHub. sum overrides the archive's listed checksum.
func updateFixture(t *testing.T, newProgram, sum string) (config, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	c := config{name: "agent-workbench-17999", dataDir: filepath.Join(home, "data"), listen: "127.0.0.1:17999"}
	os.MkdirAll(c.dataDir, 0o700)
	os.MkdirAll(filepath.Dir(unitPath(c)), 0o700)
	os.WriteFile(unitPath(c), []byte("ExecStart=x --data-dir "+unitQuote(c.dataDir)+"\n"), 0o600)
	target := filepath.Join(installDir(), binaryName)
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte(program("v0.7.0", true)), 0o755)

	dir := t.TempDir()
	name := assetName("v0.7.25", "linux", runtimeArch())
	archive := filepath.Join(dir, name)
	writeTarGz(t, archive, map[string]string{binaryName: newProgram})
	body, _ := os.ReadFile(archive)
	h := sha256.Sum256(body)
	if sum == "" {
		sum = hex.EncodeToString(h[:])
	}
	mux := http.NewServeMux()
	gh := httptest.NewServer(mux)
	t.Cleanup(gh.Close)
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.7.25", "html_url": gh.URL, "assets": []map[string]any{
			{"name": name, "browser_download_url": gh.URL + "/dl/" + name},
			{"name": "checksums.txt", "browser_download_url": gh.URL + "/dl/checksums.txt"},
		}})
	})
	mux.HandleFunc("/dl/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", sum, name)
	})
	workbench.SetReleasesURL(gh.URL + "/latest")
	t.Cleanup(func() {
		workbench.SetReleasesURL("https://api.github.com/repos/winkidney/agent-workbench/releases/latest")
	})
	old := version
	version = "v0.7.0"
	t.Cleanup(func() { version = old })
	return c, target
}

func runtimeArch() string { return runtime.GOARCH }

func TestUpdateInstallsTheNewRelease(t *testing.T) {
	c, target := updateFixture(t, program("0.7.25", true), "")
	if err := update(c, ""); err != nil {
		t.Fatal(err)
	}
	st, _ := workbench.ReadUpdateStatus(c.dataDir)
	if st.State != "done" || st.From != "v0.7.0" || st.To != "v0.7.25" {
		t.Fatalf("status %+v", st)
	}
	if info, _ := workbench.ReadServerInfo(c.dataDir); info.Version != "0.7.25" {
		t.Fatalf("serves %q", info.Version)
	}
	// The program in use is kept beside it.
	if b, _ := os.ReadFile(previousPath(target)); !strings.Contains(string(b), "v0.7.0") {
		t.Fatalf("previous %q", b)
	}
	if _, err := os.Stat(filepath.Join(updateDir(c), "v0.7.25")); err == nil {
		t.Fatal("the download stays")
	}
}

func TestUpdatePutsThePreviousProgramBack(t *testing.T) {
	c, _ := updateFixture(t, program("0.7.25", false), "")
	if err := update(c, "v0.7.25"); err == nil {
		t.Fatal("a failed install passed")
	}
	st, _ := workbench.ReadUpdateStatus(c.dataDir)
	if st.State != "rolled-back" || st.Error == "" {
		t.Fatalf("status %+v", st)
	}
	if info, _ := workbench.ReadServerInfo(c.dataDir); info.Version != "v0.7.0" {
		t.Fatalf("serves %q", info.Version)
	}
}

func TestUpdateRefusesAnArchiveThatDoesNotMatch(t *testing.T) {
	c, target := updateFixture(t, program("0.7.25", true), strings.Repeat("0", 64))
	if err := update(c, ""); err == nil || !strings.Contains(err.Error(), "does not match its checksum") {
		t.Fatalf("err %v", err)
	}
	st, _ := workbench.ReadUpdateStatus(c.dataDir)
	if st.State != "failed" {
		t.Fatalf("status %+v", st)
	}
	// Nothing changed: no install ran, nothing kept aside.
	if _, err := workbench.ReadServerInfo(c.dataDir); err == nil {
		t.Fatal("an install ran")
	}
	if _, err := os.Stat(previousPath(target)); err == nil {
		t.Fatal("the program was set aside")
	}
}

func TestUpdateRefusesAProgramOfAnotherVersion(t *testing.T) {
	c, _ := updateFixture(t, program("0.7.24", true), "")
	if err := update(c, ""); err == nil || !strings.Contains(err.Error(), "says it is 0.7.24") {
		t.Fatalf("err %v", err)
	}
}

func TestUpdateNeedsAnInstalledWorkbench(t *testing.T) {
	c, _ := updateFixture(t, program("0.7.25", true), "")
	os.Remove(unitPath(c))
	if err := update(c, ""); err == nil || !strings.Contains(err.Error(), "agent-workbench install") {
		t.Fatalf("err %v", err)
	}
	if why := updateBlocked(c, "/usr/bin/agent-workbench"); !strings.Contains(why, "package manager") {
		t.Fatalf("packaged: %q", why)
	}
}

// The workbench runs another copy than the installed one: nothing is
// replaced.
func TestUpdateReplacesOnlyTheProgramTheWorkbenchRuns(t *testing.T) {
	c, target := updateFixture(t, program("0.7.25", true), "")
	other := filepath.Join(t.TempDir(), binaryName)
	os.WriteFile(other, []byte(program("v0.7.0", true)), 0o755)
	c.program = other
	if err := update(c, ""); err == nil || !strings.Contains(err.Error(), "the workbench runs") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(previousPath(target)); err == nil {
		t.Fatal("the installed program was set aside")
	}
	c.program = target
	if err := update(c, ""); err != nil {
		t.Fatal(err)
	}
}

// Started with another environment (systemd-run passes PATH and HOME
// only), it finds the service's unit and program from the service's own.
func TestUpdateTakesTheServicesEnvironment(t *testing.T) {
	c, _ := updateFixture(t, program("0.7.25", true), "")
	b, _ := json.Marshal(map[string]string{"XDG_CONFIG_HOME": os.Getenv("XDG_CONFIG_HOME")})
	os.WriteFile(envPath(c), b, 0o600)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := update(c, ""); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateSaysWhenItIsTheNewest(t *testing.T) {
	c, _ := updateFixture(t, program("0.7.25", true), "")
	version = "v0.7.25"
	if err := update(c, ""); err != nil {
		t.Fatal(err)
	}
	if st, _ := workbench.ReadUpdateStatus(c.dataDir); st.State != "done" {
		t.Fatalf("status %+v", st)
	}
	if _, err := workbench.ReadServerInfo(c.dataDir); err == nil {
		t.Fatal("an install ran")
	}
}
