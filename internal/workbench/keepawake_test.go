package workbench

import (
	"encoding/json"
	"errors"
	"testing"
	"testing/fstest"
)

// fakeAwake stands in for the platform's holds: it counts what is held and
// can fail or lose a hold on request.
type fakeAwake struct {
	held  map[string]*fakeHold
	taken int
	fail  error
}

type fakeHold struct {
	released bool
	lost     bool
}

func (h *fakeHold) Release()    { h.released = true }
func (h *fakeHold) Alive() bool { return !h.lost }

func (f *fakeAwake) take(kind string) (awakeHold, string, error) {
	if f.fail != nil {
		return nil, "", f.fail
	}
	f.taken++
	h := &fakeHold{}
	f.held[kind] = h
	return h, "fake " + kind, nil
}

func newFakeAwake() *fakeAwake { return &fakeAwake{held: map[string]*fakeHold{}} }

func TestKeepAwakeHoldsWhatTheSwitchesAsk(t *testing.T) {
	f := newFakeAwake()
	k := newKeepAwake(f.take)

	if s := k.Status(); s.Sleep.On || s.Lock.On || f.taken != 0 {
		t.Fatalf("held before asked: %+v", s)
	}
	k.Apply(true, false)
	s := k.Status()
	if !s.Sleep.On || s.Sleep.Via != "fake sleep" || s.Lock.On || f.taken != 1 {
		t.Fatalf("sleep only: %+v, taken %d", s, f.taken)
	}
	// The same switches again take nothing more.
	k.Apply(true, false)
	k.Check()
	if f.taken != 1 {
		t.Fatalf("took again: %d", f.taken)
	}
	k.Apply(true, true)
	if s := k.Status(); !s.Sleep.On || !s.Lock.On || f.taken != 2 {
		t.Fatalf("both: %+v", s)
	}
	sleep := f.held[awakeSleep]
	k.Apply(false, true)
	if s := k.Status(); s.Sleep.On || s.Sleep.Via != "" || !s.Lock.On || !sleep.released {
		t.Fatalf("sleep off: %+v, released %v", s, sleep.released)
	}
	lock := f.held[awakeLock]
	k.Release()
	if s := k.Status(); s.Sleep.On || s.Lock.On || !lock.released {
		t.Fatalf("after release: %+v", s)
	}
}

// A hold that could not be taken shows why and is tried again by the sweep;
// one that was lost is taken again.
func TestKeepAwakeTriesAgain(t *testing.T) {
	f := newFakeAwake()
	k := newKeepAwake(f.take)
	f.fail = errors.New("could not reach the session bus")
	k.Apply(false, true)
	if s := k.Status(); s.Lock.On || s.Lock.Error != "could not reach the session bus" {
		t.Fatalf("failure: %+v", s)
	}
	f.fail = nil
	k.Check()
	if s := k.Status(); !s.Lock.On || s.Lock.Error != "" {
		t.Fatalf("retry: %+v", s)
	}
	first := f.held[awakeLock]
	first.lost = true
	k.Check()
	if s := k.Status(); !s.Lock.On || f.taken != 2 || !first.released || f.held[awakeLock] == first {
		t.Fatalf("lost hold not taken again: %+v, taken %d", s, f.taken)
	}
	// Turned off, an earlier error is not shown.
	f.fail = errors.New("no")
	f.held[awakeLock].lost = true
	k.Check()
	k.Apply(false, false)
	if s := k.Status(); s.Lock.Error != "" || s.Lock.On {
		t.Fatalf("off: %+v", s)
	}
}

func TestPreferencesKeepTheComputerAwake(t *testing.T) {
	m, _ := fixture(t)
	f := newFakeAwake()
	m.awake = newKeepAwake(f.take)
	s := NewServer(m, fstest.MapFS{"index.html": {Data: []byte("app")}}, "agent.localhost:17880", "")
	read := func(body string) PreferencesView {
		t.Helper()
		var v PreferencesView
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			t.Fatal(err, body)
		}
		return v
	}

	w := request(t, s, s.Host, "GET", "/api/preferences", "", nil)
	if v := read(w.Body.String()); w.Code != 200 || v.PreventSleep || v.PreventLock || !v.Suggestions || v.Awake.Lock.On {
		t.Fatalf("defaults: %d %s", w.Code, w.Body)
	}
	m.ApplyKeepAwake()
	if f.taken != 0 {
		t.Fatal("held with the switches off")
	}

	w = request(t, s, s.Host, "PATCH", "/api/preferences", `{"preventLock":true}`, nil)
	v := read(w.Body.String())
	if w.Code != 200 || !v.PreventLock || v.PreventSleep || !v.Suggestions || !v.Awake.Lock.On || v.Awake.Lock.Via != "fake lock" || v.Awake.Sleep.On {
		t.Fatalf("lock on: %d %s", w.Code, w.Body)
	}
	// Kept: a restarted workbench takes it again.
	if p := m.Preferences(); !p.PreventLock || p.PreventSleep {
		t.Fatalf("saved: %+v", p)
	}
	m.awake = newKeepAwake(f.take)
	m.ApplyKeepAwake()
	if !m.awake.Status().Lock.On {
		t.Fatal("not taken again at start")
	}

	w = request(t, s, s.Host, "PATCH", "/api/preferences", `{"preventLock":false,"preventSleep":true}`, nil)
	if v := read(w.Body.String()); !v.PreventSleep || v.PreventLock || !v.Awake.Sleep.On || v.Awake.Lock.On {
		t.Fatalf("swap: %s", w.Body)
	}
	m.Shutdown()
	if m.awake.Status().Sleep.On || !f.held[awakeSleep].released {
		t.Fatal("held after shutdown")
	}
}
