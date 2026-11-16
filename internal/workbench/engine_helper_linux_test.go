package workbench

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// copyProgram copies the program name finds on PATH to dir/as.
func copyProgram(t *testing.T, name, dir, as string) string {
	t.Helper()
	src, err := exec.LookPath(name)
	if err != nil {
		t.Skip(name + " not found")
	}
	if src, err = filepath.EvalSymlinks(src); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, as)
	if err := os.WriteFile(dst, b, 0o755); err != nil {
		t.Fatal(err)
	}
	return dst
}

// A program shipped next to the engine's binary, started after the engine
// became ready (Codex's codex-code-mode-host), is part of the engine: it
// does not count as background work. A command it runs from elsewhere does.
func TestEngineHelperIsNotBackgroundWork(t *testing.T) {
	bin := t.TempDir()
	engine := copyProgram(t, "sh", bin, "engine")
	copyProgram(t, "sleep", bin, "engine-helper")
	// Each line on stdin starts the next process.
	script := `read a; "$0-helper" 30 & read b; sleep 30 & wait`
	p, err := startDirect(exec.Command(engine, "-c", script), bin, func(map[string]any) {}, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Stop() })
	p.MarkReady()
	count := func() int { return len(p.group.PIDs()) }
	waitCount := func(n int) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); count() < n; time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("expected %d processes, have %d", n, count())
			}
		}
	}
	before := count()
	if p.Busy() {
		t.Fatal("busy before anything started")
	}
	_ = p.Input("helper")
	waitCount(before + 1)
	if p.Busy() {
		t.Fatal("the engine's own helper counted as background work")
	}
	_ = p.Input("command")
	waitCount(before + 2)
	if !p.Busy() {
		t.Fatal("a command left running did not count as background work")
	}
}
