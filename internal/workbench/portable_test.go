package workbench

import (
	"path/filepath"
	goruntime "runtime"
	"testing"
)

// posixOnly skips tests whose fixtures are POSIX paths ("/tmp/work",
// symlinks, slash-separated results). Linux and macOS run them in full;
// Windows path handling is a known gap (docs/install.md).
func posixOnly(t *testing.T) {
	t.Helper()
	if goruntime.GOOS == "windows" {
		t.Skip("POSIX path fixtures")
	}
}

// Tests register their own process as a Claude client; off Linux it is
// recognized by name.
func init() { claudeProcessNames = append(claudeProcessNames, "workbench.test") }

// realTempDir is t.TempDir() with symlinks resolved, as the code stores
// directories (macOS's temporary directory is under the /var symlink).
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
