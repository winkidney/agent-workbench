package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateFindsThisPlatformsArchive(t *testing.T) {
	for _, c := range []struct{ goos, arch, want string }{
		{"linux", "amd64", "agent-workbench_0.7.25_linux_amd64.tar.gz"},
		{"linux", "arm64", "agent-workbench_0.7.25_linux_arm64.tar.gz"},
		{"darwin", "arm64", "agent-workbench_0.7.25_macos_universal.tar.gz"},
		{"darwin", "amd64", "agent-workbench_0.7.25_macos_universal.tar.gz"},
		{"windows", "amd64", "agent-workbench_0.7.25_windows_amd64.zip"},
		{"windows", "arm64", "agent-workbench_0.7.25_windows_arm64.zip"},
	} {
		if got := assetName("v0.7.25", c.goos, c.arch); got != c.want {
			t.Errorf("%s/%s: %s", c.goos, c.arch, got)
		}
	}
}

func TestUpdateReadsChecksums(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	sums := []byte(strings.Repeat("0", 64) + "  other.tar.gz\n" + strings.ToUpper(sum) + "  a.tar.gz\n")
	if got, err := checksumOf(sums, "a.tar.gz"); err != nil || got != sum {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := checksumOf(sums, "missing.zip"); err == nil {
		t.Fatal("a file checksums.txt does not list passed")
	}
	if _, err := checksumOf([]byte("short  a.tar.gz\n"), "a.tar.gz"); err == nil {
		t.Fatal("a malformed checksum passed")
	}
}

// writeTarGz writes an archive of name → content.
func writeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateTakesOnlyTheProgramOutOfTheArchive(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o700)
	archive := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, archive, map[string]string{
		binaryName:              "program",
		"docs/install.md":       "docs",
		"../" + binaryName:      "outside",
		"sub/" + binaryName:     "nested",
		"./" + "unrelated.file": "x",
	})
	if err := extractProgram(archive, out); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 1 || entries[0].Name() != binaryName {
		t.Fatalf("extracted %v", entries)
	}
	if b, _ := os.ReadFile(filepath.Join(out, binaryName)); string(b) != "program" {
		t.Fatalf("program %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, binaryName)); err == nil {
		t.Fatal("wrote outside the folder")
	}
	writeTarGz(t, archive, map[string]string{"docs/install.md": "docs"})
	if err := extractProgram(archive, out); err == nil {
		t.Fatal("an archive without the program passed")
	}
	// Windows archives are zips.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{binaryName, "../evil", "docs/install.md"} {
		w, _ := zw.Create(name)
		w.Write([]byte("z:" + name))
	}
	zw.Close()
	zipPath := filepath.Join(dir, "a.zip")
	os.WriteFile(zipPath, buf.Bytes(), 0o644)
	out2 := filepath.Join(dir, "out2")
	os.Mkdir(out2, 0o700)
	if err := extractProgram(zipPath, out2); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out2, binaryName)); string(b) != "z:"+binaryName {
		t.Fatalf("zip program %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
		t.Fatal("zip wrote outside the folder")
	}
}

func TestUpdateKeepsThePreviousProgramBesideIt(t *testing.T) {
	if got := previousPath("/h/.local/bin/agent-workbench"); got != "/h/.local/bin/agent-workbench.previous" {
		t.Fatal(got)
	}
	if got := previousPath(`C:\p\agent-workbench.exe`); got != `C:\p\agent-workbench.previous.exe` {
		t.Fatal(got)
	}
}

func TestUpdateInstallsIntoTheSameInstance(t *testing.T) {
	dir := t.TempDir()
	c := config{name: "agent-workbench-17999", dataDir: dir, listen: "127.0.0.1:17999"}
	os.WriteFile(envPath(c), []byte(`{"PATH":"/bin","HTTPS_PROXY":"x","MY_TOKEN_DIR":"/t","A_DIR":"/a"}`), 0o600)
	got := strings.Join(installArgs(c), " ")
	want := "install --data-dir " + dir + " --listen 127.0.0.1:17999 --name agent-workbench-17999 --env A_DIR --env MY_TOKEN_DIR"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}
