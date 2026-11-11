package workbench

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLocalNames(t *testing.T) {
	for name, want := range map[string]bool{
		"192.168.1.5": true, "10.0.0.2": true, "[::1]": true, "8.8.8.8": true,
		"localhost": true, "agent.localhost": true, "mybox": true, "MyBox": true,
		"nas.local": true, "router.lan": true, "pi.home.arpa": true, "db.internal": true,
		"devbox.example.com": true, // this machine's own name
		"evil.com":           false, "agent.localhost.evil.com": false, "": false,
	} {
		if got := localName(name, "devbox.example.com"); got != want {
			t.Fatalf("%q: %v", name, got)
		}
	}
	for addr, want := range map[string]bool{
		"127.0.0.1:1": true, "[::1]:1": true, "192.168.0.9:1": true, "10.1.2.3:1": true,
		"172.20.0.1:1": true, "169.254.1.1:1": true, "100.101.102.103:1": true, "[fd00::1]:1": true,
		"[::ffff:192.168.1.2]:1": true, "8.8.8.8:1": false, "192.0.2.1:1234": false,
	} {
		if got := privateClient(addr); got != want {
			t.Fatalf("%s: %v", addr, got)
		}
	}
}

func TestHostAccessByNetworkAndName(t *testing.T) {
	m, _ := fixture(t)
	assets := fstest.MapFS{"index.html": {Data: []byte("app")}}
	// Pairing is off: local names are open; a code exists for logins.
	s := NewServer(m, assets, "agent.localhost:17880", "pair")
	s.hostname = "devbox"
	do := func(method, host, remote, origin string, cookie *http.Cookie) int {
		r := httptest.NewRequest(method, "http://"+host+"/api/sessions", strings.NewReader(`{}`))
		r.Host, r.RemoteAddr = host, remote
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	const local, lan, public = "127.0.0.1:5000", "192.168.1.20:5000", "203.0.113.7:5000"
	for _, c := range []struct {
		host, remote string
		want         int
	}{
		{"agent.localhost:17880", local, 200},
		{"192.168.1.5:17880", lan, 200},
		{"devbox:17880", lan, 200},
		{"devbox.local:17880", local, 200},
		{"[::1]:17880", local, 200},
		// A domain a website controls needs a login (DNS rebinding).
		{"evil.example:17880", local, 401},
		// From the internet only the canonical host, and only with a login.
		{"agent.localhost:17880", public, 401},
		{"192.168.1.5:17880", public, 403},
	} {
		if got := do("GET", c.host, c.remote, "", nil); got != c.want {
			t.Fatalf("%s from %s: %d, want %d", c.host, c.remote, got, c.want)
		}
	}
	// Changes need an Origin matching the host the request names.
	if got := do("POST", "192.168.1.5:17880", lan, "http://192.168.1.5:17880", nil); got == 403 {
		t.Fatal("same-origin LAN change refused")
	}
	if got := do("POST", "192.168.1.5:17880", lan, "http://agent.localhost:17880", nil); got != 403 {
		t.Fatalf("cross-origin change accepted: %d", got)
	}
	// Logging in on the other domain opens it there.
	login := func(host, remote string) *http.Cookie {
		r := httptest.NewRequest("POST", "http://"+host+"/api/login", strings.NewReader(`{"code":"pair"}`))
		r.Host, r.RemoteAddr = host, remote
		r.Header.Set("Origin", "http://"+host)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("login on %s: %d", host, w.Code)
		}
		return w.Result().Cookies()[0]
	}
	if got := do("GET", "evil.example:17880", local, "", login("evil.example:17880", local)); got != 200 {
		t.Fatalf("logged-in other domain: %d", got)
	}
	if got := do("GET", "agent.localhost:17880", public, "", login("agent.localhost:17880", public)); got != 200 {
		t.Fatalf("logged-in public client: %d", got)
	}
	// The auth status tells the page why the code is needed.
	r := httptest.NewRequest("GET", "http://evil.example:17880/api/auth/status", nil)
	r.Host, r.RemoteAddr = "evil.example:17880", local
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if body := w.Body.String(); !strings.Contains(body, `"reason":"host"`) || !strings.Contains(body, `"pairingRequired":true`) {
		t.Fatalf("auth status: %s", body)
	}
}
