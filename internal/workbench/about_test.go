package workbench

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAboutReportsAgentsAndAsksForVersionsOnce(t *testing.T) {
	posixOnly(t)
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	claude := filepath.Join(dir, "claude")
	write := func(version string) {
		script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo x >> " + calls + "; echo '" + version + " (Claude Code)'; exit 0; fi\n" +
			"printf '  --prompt-suggestions [value]  Enable prompt suggestions\\n'\n"
		if err := os.WriteFile(claude, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("9.9.9")
	t.Setenv("PATH", dir)
	cliVersions.Lock()
	cliVersions.byAgent = nil
	cliVersions.Unlock()

	s, _ := authServer(t)
	read := func() map[string]any {
		w := request(t, s, s.Host, "GET", "/api/about", "", nil)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("about: %d %s", w.Code, w.Body.String())
		}
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return v
	}
	asked := func() int {
		b, _ := os.ReadFile(calls)
		return strings.Count(string(b), "x")
	}
	v := read()
	wb := obj(v["workbench"])
	if wb["version"] != Version || wb["dataDir"] == "" || wb["platform"] == "" || wb["go"] == "" {
		t.Fatalf("workbench %v", wb)
	}
	agents := asList(v["agents"])
	c, x := obj(agents[0]), obj(agents[1])
	if c["name"] != "claude" || c["version"] != "9.9.9 (Claude Code)" || c["path"] != claude {
		t.Fatalf("claude %v", c)
	}
	if strings.Join(strs(c["features"]), ",") != "--prompt-suggestions" || strings.Join(strs(c["missing"]), ",") != "--include-hook-events" {
		t.Fatalf("features %v missing %v", c["features"], c["missing"])
	}
	if x["name"] != "codex" || x["error"] != "not found on PATH" || x["path"] != nil {
		t.Fatalf("codex %v", x)
	}
	if d := obj(v["data"]); d["retentionDays"] == nil || d["storageBytes"] == nil {
		t.Fatalf("data %v", d)
	}
	// Asked once per binary; again after an update.
	read()
	if asked() != 1 {
		t.Fatalf("asked %d times", asked())
	}
	write("10.0.0")
	if got := obj(asList(read()["agents"])[0])["version"]; got != "10.0.0 (Claude Code)" || asked() != 2 {
		t.Fatalf("after an update: %v, asked %d times", got, asked())
	}
}

func TestSignOutOtherDevices(t *testing.T) {
	s, _ := authServer(t)
	// Only pairing sign-ins can be ended one by one.
	if w := request(t, s, s.Host, "POST", "/api/settings/signout-others", "", nil); w.Code != 400 {
		t.Fatalf("while off: %d", w.Code)
	}
	setAccess(t, s, `{"remoteAccess":"pairing"}`)
	code := secrets(t, s)["pairingCode"]
	body, _ := json.Marshal(map[string]string{"code": code})
	login := func(remote string) *http.Cookie {
		w := fromLAN(t, s, remote, "POST", "/api/login", string(body), nil)
		if w.Code != 200 {
			t.Fatalf("login: %d", w.Code)
		}
		return w.Result().Cookies()[0]
	}
	phone, laptop, tablet := login("192.168.1.20:5000"), login("192.168.1.21:5000"), login("192.168.1.22:5000")
	settings := func(cookie *http.Cookie) map[string]any {
		w := lan(t, s, "GET", "/api/settings", "", cookie)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return v
	}
	if n := settings(phone)["signedIn"]; n != float64(3) {
		t.Fatalf("signed in %v", n)
	}
	// From the phone: the others end, the phone stays.
	w := lan(t, s, "POST", "/api/settings/signout-others", "", phone)
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res["ended"] != float64(2) || obj(res["settings"])["signedIn"] != float64(1) {
		t.Fatalf("sign out: %d %v", w.Code, res)
	}
	if lan(t, s, "GET", "/api/sessions", "", phone).Code != 200 {
		t.Fatal("the phone was signed out")
	}
	for _, c := range []*http.Cookie{laptop, tablet} {
		if lan(t, s, "GET", "/api/sessions", "", c).Code != 401 {
			t.Fatal("another device stayed signed in")
		}
	}
	// Signed out devices cannot do it either.
	if w := lan(t, s, "POST", "/api/settings/signout-others", "", laptop); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	// From this computer (no sign-in), every device ends.
	if w := request(t, s, s.Host, "POST", "/api/settings/signout-others", "", nil); w.Code != 200 {
		t.Fatalf("local: %d", w.Code)
	}
	if lan(t, s, "GET", "/api/sessions", "", phone).Code != 401 {
		t.Fatal("the phone stayed signed in")
	}
}

func TestLatestReleaseIsAskedOnlyWhenNeeded(t *testing.T) {
	asked := 0
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		if r.Header.Get("User-Agent") != "agent-workbench" || r.Header.Get("Cookie") != "" {
			t.Errorf("request carries %v", r.Header)
		}
		w.Write([]byte(`{"tag_name":"v9.9.9","html_url":"https://example.com/r/v9.9.9","body":"notes"}`))
	}))
	defer gh.Close()
	old := releasesURL
	releasesURL = gh.URL
	defer func() { releasesURL = old }()
	latestRelease.Lock()
	latestRelease.tag = ""
	latestRelease.Unlock()
	s, _ := authServer(t)
	for i := 0; i < 2; i++ {
		w := request(t, s, s.Host, "GET", "/api/updates", "", nil)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		if w.Code != 200 || v["latest"] != "v9.9.9" || v["url"] != "https://example.com/r/v9.9.9" || v["current"] != Version {
			t.Fatalf("updates: %d %v", w.Code, v)
		}
	}
	if asked != 1 {
		t.Fatalf("asked GitHub %d times", asked)
	}
}

func TestTheTabIconIsServedAsAnImage(t *testing.T) {
	m, _ := fixture(t)
	s := NewServer(m, fstest.MapFS{
		"index.html":  {Data: []byte("app")},
		"favicon.svg": {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
	}, "agent.localhost:17880", "")
	w := request(t, s, s.Host, "GET", "/favicon.svg", "", nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/svg+xml" || !strings.HasPrefix(w.Body.String(), "<svg") {
		t.Fatalf("favicon: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
}
