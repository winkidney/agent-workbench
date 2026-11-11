package workbench

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const lanHost = "192.168.1.5:17880"

// fromLAN is a browser on another device, opening this machine's LAN
// address; basic is an optional username and password.
func fromLAN(t *testing.T, s *Server, remote, method, path, body string, cookie *http.Cookie, basic ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://"+lanHost+path, strings.NewReader(body))
	r.Host, r.RemoteAddr = lanHost, remote
	r.Header.Set("Origin", "http://"+lanHost)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if len(basic) == 2 {
		r.SetBasicAuth(basic[0], basic[1])
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func lan(t *testing.T, s *Server, method, path, body string, cookie *http.Cookie, basic ...string) *httptest.ResponseRecorder {
	t.Helper()
	return fromLAN(t, s, "192.168.1.20:5000", method, path, body, cookie, basic...)
}

func authServer(t *testing.T) (*Server, *Manager) {
	t.Helper()
	m, _ := fixture(t)
	s := NewServer(m, fstest.MapFS{"index.html": {Data: []byte("app")}}, "agent.localhost:17880", "")
	if s.initErr != nil {
		t.Fatal(s.initErr)
	}
	return s, m
}

func setAccess(t *testing.T, s *Server, body string) map[string]any {
	t.Helper()
	w := request(t, s, s.Host, "PUT", "/api/settings", body, nil)
	if w.Code != 200 {
		t.Fatalf("PUT settings %s: %d %s", body, w.Code, w.Body.String())
	}
	var v map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	return v
}

func secrets(t *testing.T, s *Server) map[string]string {
	t.Helper()
	w := request(t, s, s.Host, "GET", "/api/settings/secrets", "", nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("secrets: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	var v map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	return v
}

func privateFile(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Stat(path)
	// Windows has no permission bits (the data directory is per user).
	if err != nil || (goruntime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatalf("%s: %v %v", path, err, info)
	}
	data, _ := os.ReadFile(path)
	return string(data)
}

func TestOtherDevicesAreRefusedByDefault(t *testing.T) {
	s, m := authServer(t)
	w := request(t, s, s.Host, "GET", "/api/settings", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"remoteAccess":"off"`) || !strings.Contains(w.Body.String(), `"thisComputer":true`) || !strings.Contains(w.Body.String(), `"localSignin":false`) {
		t.Fatalf("local settings: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/", "/assets/index.js", "/api/sessions", "/api/auth/status", "/api/events"} {
		w := lan(t, s, "GET", path, "", nil)
		if w.Code != 403 || w.Header().Get("X-Workbench-Access") != "off" || !strings.Contains(w.Body.String(), "Access from other devices is off") {
			t.Fatalf("%s from the LAN: %d %s", path, w.Code, w.Body.String())
		}
	}
	// A website pointed at 127.0.0.1 is not this computer.
	if w := request(t, s, "evil.test:17880", "GET", "/api/sessions", "", nil); w.Code != 403 {
		t.Fatalf("rebinding: %d", w.Code)
	}
	for _, name := range []string{"pairing-code", passwordFile} {
		if _, err := os.Stat(filepath.Join(m.store.dir, name)); !os.IsNotExist(err) {
			t.Fatalf("default mode created %s", name)
		}
	}
	if w := request(t, s, s.Host, "PUT", "/api/settings", `{"remoteAccess":"open"}`, nil); w.Code != 400 {
		t.Fatalf("unknown mode: %d", w.Code)
	}
	if w := request(t, s, s.Host, "PUT", "/api/settings", `{}`, nil); w.Code != 400 {
		t.Fatalf("missing mode: %d", w.Code)
	}
}

func TestPairingCodeForOtherDevices(t *testing.T) {
	s, m := authServer(t)
	if w := request(t, s, s.Host, "PUT", "/api/settings", `{"remoteAccess":"pairing"}`, nil); w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("this computer needs no session: %d %v", w.Code, w.Result().Cookies())
	}
	code := secrets(t, s)["pairingCode"]
	if code == "" || privateFile(t, filepath.Join(m.store.dir, "pairing-code")) != code {
		t.Fatal("pairing code not persisted")
	}
	// This computer still never signs in.
	if w := request(t, s, s.Host, "GET", "/api/sessions", "", nil); w.Code != 200 {
		t.Fatalf("local: %d", w.Code)
	}
	if w := lan(t, s, "GET", "/", "", nil); w.Code != 200 || w.Body.String() != "app" {
		t.Fatalf("the sign-in page: %d", w.Code)
	}
	if w := lan(t, s, "GET", "/api/sessions", "", nil); w.Code != 401 {
		t.Fatalf("anonymous API: %d", w.Code)
	}
	status := lan(t, s, "GET", "/api/auth/status", "", nil).Body.String()
	if !strings.Contains(status, `"authenticated":false`) || !strings.Contains(status, `"method":"pairing"`) || strings.Contains(status, code) {
		t.Fatalf("status: %s", status)
	}
	if w := lan(t, s, "POST", "/api/login", `{"code":"wrong"}`, nil); w.Code != 401 {
		t.Fatalf("wrong code: %d", w.Code)
	}
	body, _ := json.Marshal(map[string]string{"code": code})
	login := lan(t, s, "POST", "/api/login", string(body), nil)
	if login.Code != 200 || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login: %d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.Domain != "" || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	if w := lan(t, s, "GET", "/api/sessions", "", cookie); w.Code != 200 {
		t.Fatalf("signed in: %d", w.Code)
	}
	// A signed-in device may change settings; turning access off ends it.
	if w := lan(t, s, "PUT", "/api/settings", `{"remoteAccess":"off"}`, cookie); w.Code != 200 {
		t.Fatalf("remote change: %d", w.Code)
	}
	if w := lan(t, s, "GET", "/api/sessions", "", cookie); w.Code != 403 {
		t.Fatalf("after off: %d", w.Code)
	}
	setAccess(t, s, `{"remoteAccess":"pairing"}`)
	if w := lan(t, s, "GET", "/api/sessions", "", cookie); w.Code != 401 {
		t.Fatalf("old session survived a mode change: %d", w.Code)
	}
	// The mode and the code survive a restart; sessions do not.
	cookie = lan(t, s, "POST", "/api/login", string(body), nil).Result().Cookies()[0]
	restarted := NewServer(m, s.Assets, s.Host, code)
	if restarted.initErr != nil || restarted.remote != remotePairing {
		t.Fatalf("restart: %v %s", restarted.initErr, restarted.remote)
	}
	if w := lan(t, restarted, "GET", "/api/sessions", "", cookie); w.Code != 401 {
		t.Fatalf("restart kept a session: %d", w.Code)
	}
	if w := lan(t, restarted, "POST", "/api/login", string(body), nil); w.Code != 200 {
		t.Fatalf("persisted code: %d", w.Code)
	}
}

func TestPairingAttemptsArePerDevice(t *testing.T) {
	s, _ := authServer(t)
	setAccess(t, s, `{"remoteAccess":"pairing"}`)
	for i := 0; i < signInLimit; i++ {
		fromLAN(t, s, "192.168.1.30:5000", "POST", "/api/login", `{"code":"wrong"}`, nil)
	}
	if w := fromLAN(t, s, "192.168.1.30:5000", "POST", "/api/login", `{"code":"wrong"}`, nil); w.Code != 429 {
		t.Fatalf("limit: %d", w.Code)
	}
	body, _ := json.Marshal(map[string]string{"code": s.Pairing})
	if w := fromLAN(t, s, "192.168.1.31:5000", "POST", "/api/login", string(body), nil); w.Code != 200 {
		t.Fatalf("another device locked out: %d", w.Code)
	}
}

func TestPasswordForOtherDevices(t *testing.T) {
	s, m := authServer(t)
	settings := setAccess(t, s, `{"remoteAccess":"password"}`)
	if settings["remoteAccess"] != "password" || settings["username"] != defaultUsername {
		t.Fatalf("settings: %v", settings)
	}
	sec := secrets(t, s)
	user, pass := sec["username"], sec["password"]
	if user != defaultUsername || len(pass) != 16 || strings.ContainsAny(pass, "01ilo") {
		t.Fatalf("default credentials: %q %q", user, pass)
	}
	var stored credentials
	_ = json.Unmarshal([]byte(privateFile(t, filepath.Join(m.store.dir, passwordFile))), &stored)
	if stored.Username != user || stored.Password != pass {
		t.Fatal("credentials not persisted")
	}
	// Every request from another device needs them, pages included.
	for _, path := range []string{"/", "/api/sessions", "/api/events", "/api/auth/status"} {
		w := lan(t, s, "GET", path, "", nil)
		if w.Code != 401 || w.Header().Get("WWW-Authenticate") != `Basic realm="Agent Workbench", charset="UTF-8"` || w.Header().Get("X-Workbench-Access") != "sign-in" {
			t.Fatalf("%s: %d %q", path, w.Code, w.Header().Get("WWW-Authenticate"))
		}
	}
	if w := lan(t, s, "GET", "/", "", nil, user, "wrong-password"); w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	if w := lan(t, s, "GET", "/", "", nil, user, pass); w.Code != 200 || w.Body.String() != "app" {
		t.Fatalf("page: %d", w.Code)
	}
	if w := lan(t, s, "GET", "/api/sessions", "", nil, user, pass); w.Code != 200 {
		t.Fatalf("API: %d", w.Code)
	}
	if body := lan(t, s, "GET", "/api/auth/status", "", nil, user, pass).Body.String(); !strings.Contains(body, `"authenticated":true`) || !strings.Contains(body, `"method":"password"`) {
		t.Fatalf("status: %s", body)
	}
	// This computer still never signs in.
	if w := request(t, s, s.Host, "GET", "/api/sessions", "", nil); w.Code != 200 {
		t.Fatalf("local: %d", w.Code)
	}
	// With the password on, a pairing session is not enough.
	s.mu.Lock()
	s.auth["old"] = time.Now().Add(time.Hour)
	s.mu.Unlock()
	if w := lan(t, s, "GET", "/api/sessions", "", &http.Cookie{Name: "workbench_session", Value: "old"}); w.Code != 401 {
		t.Fatalf("pairing cookie: %d", w.Code)
	}
	// Writes still need the page's own origin.
	r := httptest.NewRequest("PUT", "http://"+lanHost+"/api/settings", strings.NewReader(`{"remoteAccess":"off"}`))
	r.Host, r.RemoteAddr = lanHost, "192.168.1.20:5000"
	r.Header.Set("Origin", "http://evil.test")
	r.SetBasicAuth(user, pass)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 || s.remote != remotePassword {
		t.Fatalf("cross-site change: %d", w.Code)
	}
	// Editing: the old credentials stop working at once.
	for _, invalid := range []string{
		`{"remoteAccess":"password","username":"a:b","password":"long enough"}`,
		`{"remoteAccess":"password","username":"","password":"long enough"}`,
		`{"remoteAccess":"password","username":"me","password":"short"}`,
		`{"remoteAccess":"password","username":"me","password":"tab\tinside"}`,
	} {
		if w := request(t, s, s.Host, "PUT", "/api/settings", invalid, nil); w.Code != 400 {
			t.Fatalf("accepted %s: %d", invalid, w.Code)
		}
	}
	if w := lan(t, s, "GET", "/", "", nil, user, pass); w.Code != 200 {
		t.Fatal("a rejected edit changed the credentials")
	}
	if w := lan(t, s, "PUT", "/api/settings", `{"remoteAccess":"password","username":"me","password":"correct horse"}`, nil, user, pass); w.Code != 200 {
		t.Fatalf("remote edit: %d %s", w.Code, w.Body.String())
	}
	if w := lan(t, s, "GET", "/", "", nil, user, pass); w.Code != 401 {
		t.Fatalf("old credentials: %d", w.Code)
	}
	if w := lan(t, s, "GET", "/", "", nil, "me", "correct horse"); w.Code != 200 {
		t.Fatalf("new credentials: %d", w.Code)
	}
	// They survive a restart, and are made anew if the file is gone.
	restarted := NewServer(m, s.Assets, s.Host, "")
	if restarted.initErr != nil || restarted.remote != remotePassword {
		t.Fatalf("restart: %v %s", restarted.initErr, restarted.remote)
	}
	if w := lan(t, restarted, "GET", "/", "", nil, "me", "correct horse"); w.Code != 200 {
		t.Fatalf("after restart: %d", w.Code)
	}
	_ = os.Remove(filepath.Join(m.store.dir, passwordFile))
	fresh := NewServer(m, s.Assets, s.Host, "")
	if fresh.initErr != nil || fresh.user != defaultUsername || fresh.pass == "correct horse" || len(fresh.pass) != 16 {
		t.Fatalf("missing file: %v %q", fresh.initErr, fresh.user)
	}
	// Turning the password off keeps the credentials for next time.
	setAccess(t, s, `{"remoteAccess":"off"}`)
	if secrets(t, s)["username"] != "me" {
		t.Fatal("credentials dropped")
	}
}

func TestPasswordAttemptsArePerDevice(t *testing.T) {
	s, _ := authServer(t)
	setAccess(t, s, `{"remoteAccess":"password"}`)
	sec := secrets(t, s)
	// Requests without credentials (the browser's first try) don't count.
	for i := 0; i < 2*signInLimit; i++ {
		fromLAN(t, s, "192.168.1.40:5000", "GET", "/", "", nil)
	}
	for i := 0; i < signInLimit; i++ {
		if w := fromLAN(t, s, "192.168.1.40:5000", "GET", "/", "", nil, sec["username"], "guess"); w.Code != 401 {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	if w := fromLAN(t, s, "192.168.1.40:5000", "GET", "/", "", nil, sec["username"], sec["password"]); w.Code != 429 {
		t.Fatalf("after %d failures: %d", signInLimit, w.Code)
	}
	if w := fromLAN(t, s, "192.168.1.41:5000", "GET", "/", "", nil, sec["username"], sec["password"]); w.Code != 200 {
		t.Fatalf("another device locked out: %d", w.Code)
	}
}

func TestRemoteAccessMigration(t *testing.T) {
	for value, want := range map[string]string{"true": remotePairing, "false": remoteOff, "": remoteOff} {
		m, _ := fixture(t)
		if value != "" {
			if _, err := m.store.db.Exec("INSERT INTO settings VALUES('pairing_required',?)", value); err != nil {
				t.Fatal(err)
			}
		}
		s := NewServer(m, nil, "agent.localhost:17880", "")
		if s.initErr != nil || s.remote != want {
			t.Fatalf("pairing_required=%q: %v %s", value, s.initErr, s.remote)
		}
		if want == remotePairing && s.Pairing == "" {
			t.Fatal("pairing without a code")
		}
	}
}

// local is a request from this computer, optionally relayed by a proxy.
func local(t *testing.T, s *Server, method, path, body string, cookie *http.Cookie, header http.Header, basic ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://localhost:17880"+path, strings.NewReader(body))
	r.Host, r.RemoteAddr = "localhost:17880", "127.0.0.1:5000"
	r.Header.Set("Origin", "http://localhost:17880")
	for k, v := range header {
		r.Header[k] = v
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if len(basic) == 2 {
		r.SetBasicAuth(basic[0], basic[1])
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestProxyOnThisComputerSignsIn(t *testing.T) {
	s, _ := authServer(t)
	// nginx in front, forwarding Host: localhost and adding X-Forwarded-For:
	// the visitor is another device, so with access off it is refused.
	viaProxy := http.Header{"X-Forwarded-For": {"203.0.113.7"}}
	if w := local(t, s, "GET", "/api/sessions", "", nil, viaProxy); w.Code != 403 || w.Header().Get("X-Workbench-Access") != "off" {
		t.Fatalf("relayed while off: %d", w.Code)
	}
	setAccess(t, s, `{"remoteAccess":"password"}`)
	sec := secrets(t, s)
	if w := local(t, s, "PUT", "/api/settings", `{"remoteAccess":"off"}`, nil, viaProxy); w.Code != 401 {
		t.Fatalf("relayed change without sign-in: %d", w.Code)
	}
	if w := local(t, s, "GET", "/api/sessions", "", nil, viaProxy, sec["username"], sec["password"]); w.Code != 200 {
		t.Fatalf("relayed with the password: %d", w.Code)
	}
	if body := local(t, s, "GET", "/api/settings", "", nil, viaProxy, sec["username"], sec["password"]).Body.String(); !strings.Contains(body, `"thisComputer":false`) {
		t.Fatalf("relayed settings: %s", body)
	}
}

func TestSignInOnThisComputerToo(t *testing.T) {
	s, m := authServer(t)
	// Not without a way to sign in.
	if w := local(t, s, "PUT", "/api/settings", `{"remoteAccess":"off","localSignin":true}`, nil, nil); w.Code != 400 {
		t.Fatalf("local sign-in with access off: %d", w.Code)
	}
	// Pairing: the browser that turns it on stays connected; others sign in.
	on := local(t, s, "PUT", "/api/settings", `{"remoteAccess":"pairing","localSignin":true}`, nil, nil)
	if on.Code != 200 || len(on.Result().Cookies()) != 1 || !strings.Contains(on.Body.String(), `"localSignin":true`) {
		t.Fatalf("turn on: %d %s", on.Code, on.Body.String())
	}
	cookie := on.Result().Cookies()[0]
	if w := local(t, s, "GET", "/api/sessions", "", nil, nil); w.Code != 401 {
		t.Fatalf("this computer without a session: %d", w.Code)
	}
	if w := local(t, s, "GET", "/", "", nil, nil); w.Code != 200 {
		t.Fatalf("sign-in page: %d", w.Code)
	}
	if w := local(t, s, "GET", "/api/sessions", "", cookie, nil); w.Code != 200 {
		t.Fatalf("this computer with a session: %d", w.Code)
	}
	status := local(t, s, "GET", "/api/auth/status", "", nil, nil).Body.String()
	if !strings.Contains(status, `"authenticated":false`) || !strings.Contains(status, `"local":false`) {
		t.Fatalf("status: %s", status)
	}
	// Access cannot be switched off while it is on.
	if w := local(t, s, "PUT", "/api/settings", `{"remoteAccess":"off"}`, cookie, nil); w.Code != 400 {
		t.Fatalf("off while local sign-in: %d", w.Code)
	}
	// Password: this computer gets the browser's prompt too.
	if w := local(t, s, "PUT", "/api/settings", `{"remoteAccess":"password","localSignin":true}`, cookie, nil); w.Code != 200 {
		t.Fatalf("password: %d", w.Code)
	}
	user, pass := s.user, s.pass
	if w := local(t, s, "GET", "/", "", nil, nil); w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("this computer without the password: %d", w.Code)
	}
	if w := local(t, s, "GET", "/api/sessions", "", nil, nil, user, pass); w.Code != 200 {
		t.Fatalf("this computer with the password: %d", w.Code)
	}
	// It survives a restart.
	restarted := NewServer(m, s.Assets, s.Host, "")
	if restarted.initErr != nil || !restarted.localSignin.Load() {
		t.Fatalf("restart: %v", restarted.initErr)
	}
	// Turning it off (with access off) opens this computer again.
	if w := local(t, s, "PUT", "/api/settings", `{"remoteAccess":"off","localSignin":false}`, nil, nil, user, pass); w.Code != 200 {
		t.Fatalf("turn off: %d", w.Code)
	}
	if w := local(t, s, "GET", "/api/sessions", "", nil, nil); w.Code != 200 {
		t.Fatalf("this computer after turning off: %d", w.Code)
	}
	// A hand-edited database that asks for it without a method is ignored.
	if err := saveSettings(m.store.db, map[string]string{"remote_access": "off", "local_signin": "true"}); err != nil {
		t.Fatal(err)
	}
	if NewServer(m, s.Assets, s.Host, "").localSignin.Load() {
		t.Fatal("local sign-in without a method")
	}
}
