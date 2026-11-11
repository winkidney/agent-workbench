package workbench

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// stopFails is an engine whose stop request fails, as systemctl's does when
// systemd is already stopping the unit with the workbench.
type stopFails struct{ fakeProcess }

func (p *stopFails) Stop() error { return errors.New("Job canceled") }

func TestShutdownPutsLoadedSessionsToSleep(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	m, err := NewManager(s, func(_ Session, _ func(map[string]any), _ func(error)) (Process, error) { return &stopFails{}, nil }, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	loaded, _ := m.Create("codex", t.TempDir(), "loaded")
	other, _ := m.Create("codex", t.TempDir(), "other")
	if err := m.Start(loaded.ID); err != nil {
		t.Fatal(err)
	}
	before := m.sessions[other.ID].session.Status
	// Outside a shutdown, a failed stop is an error.
	if err := m.Stop(loaded.ID, false); err == nil || m.sessions[loaded.ID].session.Status != "error" {
		t.Fatalf("failed stop: %v %s", err, m.sessions[loaded.ID].session.Status)
	}
	if err := m.Start(loaded.ID); err != nil {
		t.Fatal(err)
	}
	m.Shutdown()
	if got := m.sessions[loaded.ID].session.Status; got != "sleeping" {
		t.Fatalf("after shutdown: %s, want sleeping", got)
	}
	if m.sessions[loaded.ID].proc != nil {
		t.Fatal("engine still attached")
	}
	if got := m.sessions[other.ID].session.Status; got != before {
		t.Fatalf("unloaded session changed: %s -> %s", before, got)
	}
}

func TestStopUnitCountsAUnitThatStopped(t *testing.T) {
	orig := systemctl
	t.Cleanup(func() { systemctl = orig })
	for _, c := range []struct {
		stopErr error
		state   string
		ok      bool
	}{
		{nil, "", true},
		{errors.New("Job canceled"), "inactive", true},
		{errors.New("Job canceled"), "failed", true},
		{errors.New("Unit not loaded"), "unknown", true},
	} {
		var calls []string
		systemctl = func(args ...string) (string, error) {
			calls = append(calls, strings.Join(args, " "))
			if args[0] == "stop" {
				return "", c.stopErr
			}
			return c.state, errors.New("exit status 3")
		}
		if err := stopUnit("u.service"); (err == nil) != c.ok {
			t.Errorf("%v/%s: %v (calls %v)", c.stopErr, c.state, err, calls)
		}
	}
}

// exitOf runs a shell command and returns how it ended, as cmd.Wait does.
func exitOf(t *testing.T, script string) error {
	t.Helper()
	err := exec.Command("sh", "-c", script).Run()
	if err == nil {
		t.Fatalf("%q exited 0", script)
	}
	return err
}

func TestStoppedOnRequest(t *testing.T) {
	posixOnly(t)
	for script, want := range map[string]bool{
		"exit 143":      true, // Claude Code after catching SIGTERM
		"exit 130":      true,
		"kill -TERM $$": true,
		"kill -INT $$":  true,
		"exit 1":        false,
		"exit 2":        false,
		"kill -KILL $$": false,
		"kill -SEGV $$": false,
	} {
		if got := stoppedOnRequest(exitOf(t, script)); got != want {
			t.Errorf("%s: %v", script, got)
		}
	}
	if stoppedOnRequest(nil) || stoppedOnRequest(errors.New("broken pipe")) {
		t.Fatal("non-exit errors are not stops")
	}
}

func kinds(t *testing.T, m *Manager, id string, after int64) []string {
	t.Helper()
	events, err := m.store.Events(id, after, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

func TestEngineExitsAreErrorsOnlyWhenTheyCrash(t *testing.T) {
	posixOnly(t)
	m, _ := fixture(t)
	s, _ := m.Create("claude", t.TempDir(), "exits")
	for _, c := range []struct {
		name    string
		err     error
		closing bool
		want    string
	}{
		{"clean exit", nil, false, "sleeping"},
		{"stopped by systemd (Claude exits 143)", exitOf(t, "exit 143"), false, "sleeping"},
		{"client terminated with the workbench", exitOf(t, "kill -TERM $$"), false, "sleeping"},
		{"crash", exitOf(t, "exit 1"), false, "error"},
		{"crash while the workbench closes", exitOf(t, "exit 1"), true, "sleeping"},
	} {
		if err := m.Start(s.ID); err != nil {
			t.Fatal(err)
		}
		r := m.sessions[s.ID]
		// A request the agent is waiting on.
		r.pending["req-1"] = map[string]any{"request": map[string]any{"subtype": "can_use_tool"}}
		m.closing = c.closing
		last, _ := m.store.Events(s.ID, 0, 1000)
		after := int64(0)
		if len(last) > 0 {
			after = last[len(last)-1].Seq
		}
		m.exited(s.ID, r.generation, c.err)
		if r.session.Status != c.want {
			t.Errorf("%s: status %s, want %s", c.name, r.session.Status, c.want)
		}
		got := strings.Join(kinds(t, m, s.ID, after), ",")
		if !strings.Contains(got, "approval_resolved") || len(r.pending) != 0 {
			t.Errorf("%s: pending request not released: %s", c.name, got)
		}
		if strings.Contains(got, "error") != (c.want == "error") {
			t.Errorf("%s: error event %v: %s", c.name, strings.Contains(got, "error"), got)
		}
	}
}
