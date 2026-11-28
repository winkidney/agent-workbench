package workbench

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewerVersionReadsReleaseAndBuildVersions(t *testing.T) {
	for _, c := range []struct {
		current, latest string
		newer           bool
	}{
		{"v0.7.24", "v0.7.25", true},
		{"0.7.24", "v0.7.25", true},
		{"v0.7.24", "v0.8.0", true},
		{"v0.7.24", "v1.0.0", true},
		{"v0.7.25", "v0.7.25", false},
		{"0.7.25", "v0.7.25", false},
		{"v0.7.26", "v0.7.25", false},
		// A build past a tag is that tag's or later.
		{"v0.7.25-3-gabc", "v0.7.25", false},
		{"v0.7.24-3-gabc", "v0.7.25", true},
		{"dev", "v0.7.25", false},
		{"v0.7.24", "nightly", false},
		{"v0.7.24", "v0.7.10", false},
	} {
		if got := NewerVersion(c.current, c.latest); got != c.newer {
			t.Errorf("NewerVersion(%q, %q) = %v", c.current, c.latest, got)
		}
	}
	if !SameVersion("v0.7.25", "0.7.25\n") || SameVersion("v0.7.25", "v0.7.24") {
		t.Fatal("SameVersion")
	}
}

// updateServer: a workbench of v0.7.0 that can update itself, and a
// stand-in GitHub whose newest release is v0.7.25.
func updateServer(t *testing.T) (*Server, *[]string) {
	t.Helper()
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.7.25","html_url":"https://example.com/r","assets":[]}`))
	}))
	t.Cleanup(gh.Close)
	old := releasesURL
	SetReleasesURL(gh.URL)
	t.Cleanup(func() { SetReleasesURL(old) })
	oldVersion, oldStart, oldBlocked := Version, StartUpdate, UpdateBlocked
	t.Cleanup(func() { Version, StartUpdate, UpdateBlocked = oldVersion, oldStart, oldBlocked })
	Version = "v0.7.0"
	started := []string{}
	StartUpdate = func(to string) error {
		started = append(started, to)
		return nil
	}
	UpdateBlocked = func() string { return "" }
	s, _ := authServer(t)
	return s, &started
}

func TestUpdateStartsOnceForTheNewestRelease(t *testing.T) {
	s, started := updateServer(t)
	w := request(t, s, s.Host, "POST", "/api/update", `{"to":"v0.7.25"}`, nil)
	if w.Code != http.StatusAccepted || len(*started) != 1 || (*started)[0] != "v0.7.25" {
		t.Fatalf("%d %s %v", w.Code, w.Body, *started)
	}
	st, ok := ReadUpdateStatus(s.Manager.store.dir)
	if !ok || st.State != "starting" || st.From != "v0.7.0" || st.To != "v0.7.25" {
		t.Fatalf("status %+v", st)
	}
	// One at a time.
	if w := request(t, s, s.Host, "POST", "/api/update", `{"to":"v0.7.25"}`, nil); w.Code != http.StatusConflict {
		t.Fatalf("second: %d", w.Code)
	}
	// One that has said nothing for ten minutes has ended.
	st.At = time.Now().Add(-11 * time.Minute).UTC().Format(time.RFC3339)
	WriteUpdateStatus(s.Manager.store.dir, st)
	if w := request(t, s, s.Host, "POST", "/api/update", `{"to":"v0.7.25"}`, nil); w.Code != http.StatusAccepted {
		t.Fatalf("after a stale one: %d %s", w.Code, w.Body)
	}
	// About says how it went and that it can.
	var about map[string]any
	json.Unmarshal(request(t, s, s.Host, "GET", "/api/about", "", nil).Body.Bytes(), &about)
	u, _ := about["update"].(map[string]any)
	if u["can"] != true || u["last"].(map[string]any)["to"] != "v0.7.25" {
		t.Fatalf("about %v", u)
	}
}

func TestUpdateIsRefusedWithItsReason(t *testing.T) {
	s, started := updateServer(t)
	// Another release than the newest, which is asked afresh.
	if w := request(t, s, s.Host, "POST", "/api/update", `{"to":"v0.7.24"}`, nil); w.Code != http.StatusConflict {
		t.Fatalf("not the newest: %d", w.Code)
	}
	// Already the newest.
	Version = "v0.7.25"
	if w := request(t, s, s.Host, "POST", "/api/update", `{"to":"v0.7.25"}`, nil); w.Code != 400 || !strings.Contains(w.Body.String(), "already") {
		t.Fatalf("newest: %d %s", w.Code, w.Body)
	}
	// A package manager's install says so, and About says why.
	Version = "v0.7.0"
	UpdateBlocked = func() string { return "a package manager installed it" }
	if w := request(t, s, s.Host, "POST", "/api/update", `{"to":"v0.7.25"}`, nil); w.Code != 400 || !strings.Contains(w.Body.String(), "package manager") {
		t.Fatalf("packaged: %d %s", w.Code, w.Body)
	}
	var about map[string]any
	json.Unmarshal(request(t, s, s.Host, "GET", "/api/about", "", nil).Body.Bytes(), &about)
	if u := about["update"].(map[string]any); u["can"] != false || u["blocked"] != "a package manager installed it" {
		t.Fatalf("about %v", u)
	}
	// A development build cannot tell.
	UpdateBlocked = func() string { return "" }
	Version = "dev"
	if w := request(t, s, s.Host, "POST", "/api/update", `{"to":"v0.7.25"}`, nil); w.Code != 400 || !strings.Contains(w.Body.String(), "development build") {
		t.Fatalf("dev: %d %s", w.Code, w.Body)
	}
	// Not from another site.
	Version = "v0.7.0"
	r := httptest.NewRequest("POST", "http://"+s.Host+"/api/update", strings.NewReader(`{"to":"v0.7.25"}`))
	r.Host, r.RemoteAddr = s.Host, "127.0.0.1:1234"
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code < 400 {
		t.Fatalf("cross-site: %d", w.Code)
	}
	if len(*started) != 0 {
		t.Fatalf("started %v", *started)
	}
}
