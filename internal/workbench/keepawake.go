package workbench

import "sync"

// Keeping the computer awake (ADR 0018): two switches in Settings → General,
// off by default. While the workbench runs and a switch is on, it holds what
// stops the computer it runs on from sleeping, or from locking (and blanking)
// its screen when idle: a locked screen leaves agents without a display for
// browsers and screenshots. It does not ask whether an agent is working; the
// user decides with the switch. Each hold ends with the workbench's process
// (a file descriptor, a bus connection or a child bound to its pid), so a
// crash never leaves one behind.

// AwakeItem is one switch as the computer has it: held, by what, or why not.
type AwakeItem struct {
	On    bool   `json:"on"`
	Via   string `json:"via,omitempty"`
	Error string `json:"error,omitempty"`
}

// AwakeStatus is what the Settings page shows under the two switches.
type AwakeStatus struct {
	Sleep AwakeItem `json:"sleep"`
	Lock  AwakeItem `json:"lock"`
}

const (
	awakeSleep = "sleep"
	awakeLock  = "lock"
)

// awakeHold is one hold the platform took (keepawake_<os>.go).
type awakeHold interface {
	Release()
	// Alive is false once the hold was lost (its bus connection closed, its
	// child exited); the next check takes it again.
	Alive() bool
}

// awakeHolder takes a hold of a kind (awakeSleep or awakeLock) and says by
// what.
type awakeHolder func(kind string) (awakeHold, string, error)

type keepAwake struct {
	mu   sync.Mutex
	take awakeHolder
	want map[string]bool
	held map[string]awakeHold
	via  map[string]string
	errs map[string]string
}

func newKeepAwake(take awakeHolder) *keepAwake {
	return &keepAwake{
		take: take,
		want: map[string]bool{},
		held: map[string]awakeHold{},
		via:  map[string]string{},
		errs: map[string]string{},
	}
}

// Apply takes or releases the holds to match the switches.
func (k *keepAwake) Apply(sleep, lock bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.want[awakeSleep], k.want[awakeLock] = sleep, lock
	k.sync()
}

// Check takes again a hold that was lost or could not be taken (the session
// bus restarted, the user signed in again); the workbench's sweep calls it.
func (k *keepAwake) Check() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.sync()
}

// Release lets go of everything, as the workbench exits.
func (k *keepAwake) Release() {
	k.Apply(false, false)
}

func (k *keepAwake) Status() AwakeStatus {
	k.mu.Lock()
	defer k.mu.Unlock()
	item := func(kind string) AwakeItem {
		return AwakeItem{On: k.held[kind] != nil, Via: k.via[kind], Error: k.errs[kind]}
	}
	return AwakeStatus{Sleep: item(awakeSleep), Lock: item(awakeLock)}
}

// sync (k.mu held) releases what is no longer wanted or was lost, and takes
// what is wanted and not held.
func (k *keepAwake) sync() {
	for _, kind := range []string{awakeSleep, awakeLock} {
		if h := k.held[kind]; h != nil && (!k.want[kind] || !h.Alive()) {
			h.Release()
			delete(k.held, kind)
			delete(k.via, kind)
		}
		if !k.want[kind] {
			delete(k.errs, kind)
			continue
		}
		if k.held[kind] != nil {
			continue
		}
		h, via, err := k.take(kind)
		if err != nil {
			k.errs[kind] = err.Error()
			continue
		}
		k.held[kind], k.via[kind] = h, via
		delete(k.errs, kind)
	}
}
