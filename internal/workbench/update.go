package workbench

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Install and restart (ADR 0028): Settings → About installs the newest
// release. The server only checks and starts `agent-workbench update`
// apart from itself (StartUpdate, set by the program); that process
// downloads the release, checks it against its checksums, installs it,
// which stops and restarts this server, and puts the previous program back
// when the new one does not start. It reports in update.json.

// StartUpdate starts the update to a release apart from this process, so it
// outlives the restart; nil where the program cannot update itself.
var StartUpdate func(to string) error

// UpdateBlocked says why this workbench cannot install a release itself
// (a package manager installed it, or it is not installed as a service);
// "" when it can.
var UpdateBlocked func() string

// UpdateStatus is the last update, as update.json keeps it.
type UpdateStatus struct {
	// starting | downloading | verifying | installing | done | failed |
	// rolled-back
	State string `json:"state"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
	Error string `json:"error,omitempty"`
	// Its log, for when it failed.
	Log string `json:"log,omitempty"`
	At  string `json:"at"`
}

func UpdateStatusPath(dataDir string) string { return filepath.Join(dataDir, "update.json") }

func ReadUpdateStatus(dataDir string) (UpdateStatus, bool) {
	var v UpdateStatus
	b, err := os.ReadFile(UpdateStatusPath(dataDir))
	if err != nil || json.Unmarshal(b, &v) != nil || v.State == "" {
		return UpdateStatus{}, false
	}
	return v, true
}

func WriteUpdateStatus(dataDir string, v UpdateStatus) error {
	if v.At == "" {
		v.At = time.Now().UTC().Format(time.RFC3339)
	}
	b, _ := json.Marshal(v)
	tmp := UpdateStatusPath(dataDir) + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, UpdateStatusPath(dataDir))
}

// Under way: begun and not ended. One that has said nothing for ten
// minutes is taken as ended (its process died).
func (v UpdateStatus) underWay(now time.Time) bool {
	switch v.State {
	case "starting", "downloading", "verifying", "installing":
		at, err := time.Parse(time.RFC3339, v.At)
		return err == nil && now.Sub(at) < 10*time.Minute
	}
	return false
}

// versionParts reads "v0.7.24", "0.7.24" or "v0.7.24-3-gabc"; ok is false
// for anything else ("dev").
func versionParts(v string) (p [3]int, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	base, _, _ := strings.Cut(v, "-")
	f := strings.Split(base, ".")
	if len(f) != 3 {
		return p, false
	}
	for i, s := range f {
		n, err := strconv.Atoi(s)
		if err != nil {
			return p, false
		}
		p[i] = n
	}
	return p, true
}

// NewerVersion is whether latest is a later release than current. A build
// past a tag (v0.7.24-3-gabc) is later than that tag.
func NewerVersion(current, latest string) bool {
	a, okA := versionParts(current)
	b, okB := versionParts(latest)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return b[i] > a[i]
		}
	}
	return false
}

// SameVersion: "v0.7.24" and "0.7.24" (a release build prints no v).
func SameVersion(a, b string) bool {
	return strings.TrimPrefix(strings.TrimSpace(a), "v") == strings.TrimPrefix(strings.TrimSpace(b), "v")
}

// updateBlocked is why this workbench cannot update itself now, or "".
func updateBlocked() string {
	if StartUpdate == nil {
		return "this program cannot install updates itself"
	}
	if _, ok := versionParts(Version); !ok {
		return "a development build (" + Version + ") cannot tell which release is newer"
	}
	if UpdateBlocked != nil {
		return UpdateBlocked()
	}
	return ""
}

// startUpdate: POST /api/update {to}. It answers before the update starts
// to stop this server.
func (s *Server) startUpdate(w http.ResponseWriter, r *http.Request) {
	var v struct {
		To string `json:"to"`
	}
	if err := decode(w, r, &v); err != nil {
		bad(w, err)
		return
	}
	if why := updateBlocked(); why != "" {
		bad(w, errors.New(why))
		return
	}
	dir := s.Manager.store.dir
	if st, ok := ReadUpdateStatus(dir); ok && st.underWay(time.Now()) {
		http.Error(w, "an update to "+st.To+" is under way", http.StatusConflict)
		return
	}
	// Asked afresh: the update reads the newest release itself, and one
	// published since the last check would not be the one asked for.
	forgetRelease()
	latest, err := NewestRelease()
	if err != nil {
		http.Error(w, "could not check for updates: "+err.Error(), http.StatusBadGateway)
		return
	}
	if !SameVersion(latest.Tag, v.To) {
		http.Error(w, "the newest release is now "+latest.Tag+": check for updates again", http.StatusConflict)
		return
	}
	if !NewerVersion(Version, latest.Tag) {
		bad(w, errors.New("this is already "+Version+", the newest release"))
		return
	}
	if err := WriteUpdateStatus(dir, UpdateStatus{State: "starting", From: Version, To: latest.Tag}); err != nil {
		bad(w, err)
		return
	}
	if err := StartUpdate(latest.Tag); err != nil {
		_ = WriteUpdateStatus(dir, UpdateStatus{State: "failed", From: Version, To: latest.Tag, Error: err.Error()})
		bad(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"to": latest.Tag})
}

// aboutUpdate is what the About tab shows of updates: whether this
// workbench can install one, and how the last one went.
func (s *Server) aboutUpdate() map[string]any {
	out := map[string]any{"can": true}
	if why := updateBlocked(); why != "" {
		out["can"], out["blocked"] = false, why
	}
	if st, ok := ReadUpdateStatus(s.Manager.store.dir); ok {
		out["last"] = st
	}
	return out
}
