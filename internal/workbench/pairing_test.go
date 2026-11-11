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
)

func TestOptionalPairing(t *testing.T) {
	m, _ := fixture(t)
	assets := fstest.MapFS{"index.html": {Data: []byte("app")}}
	s := NewServer(m, assets, "agent.localhost:17880", "")
	if s.initErr != nil {
		t.Fatal(s.initErr)
	}
	check := func(path string, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := request(t, s, s.Host, "GET", path, "", cookie)
		if w.Code != want {
			t.Fatalf("%s: got %d, want %d", path, w.Code, want)
		}
		return w
	}
	if body := check("/api/settings", nil, 200).Body.String(); !strings.Contains(body, `"pairingRequired":false`) {
		t.Fatal(body)
	}
	if _, err := os.Stat(filepath.Join(m.store.dir, "pairing-code")); !os.IsNotExist(err) {
		t.Fatal("default mode created pairing code")
	}
	check("/api/auth/status", nil, 200)
	// Host and Origin guards still apply without pairing: a domain that is
	// not a local name needs a login (DNS rebinding).
	if w := request(t, s, "evil.test:17880", "GET", "/api/settings", "", nil); w.Code != 401 {
		t.Fatalf("other domain served without login: %d", w.Code)
	}
	r := httptest.NewRequest("PUT", "http://"+s.Host+"/api/settings", strings.NewReader(`{"pairingRequired":true}`))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Origin", "http://evil.test")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("invalid origin accepted")
	}
	if w := request(t, s, s.Host, "PUT", "/api/settings", `{}`, nil); w.Code != 400 {
		t.Fatal("missing mode accepted")
	}
	enable := request(t, s, s.Host, "PUT", "/api/settings", `{"pairingRequired":true}`, nil)
	if enable.Code != 200 || len(enable.Result().Cookies()) != 1 {
		t.Fatal("enable did not authenticate current browser")
	}
	cookie := enable.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.Domain != "" || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	check("/api/settings", nil, 401)
	check("/api/settings/pairing-code", nil, 401)
	if w := request(t, s, s.Host, "PUT", "/api/settings", `{"pairingRequired":false}`, nil); w.Code != 401 {
		t.Fatal("anonymous disable accepted")
	}
	status := check("/api/auth/status", nil, 200)
	if !strings.Contains(status.Body.String(), `"authenticated":false`) || strings.Contains(status.Body.String(), s.Pairing) {
		t.Fatal("bad public auth status")
	}
	secret := check("/api/settings/pairing-code", cookie, 200)
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(secret.Body.Bytes(), &payload); err != nil || payload.Code == "" {
		t.Fatal("missing code")
	}
	if secret.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("secret may be cached")
	}
	persisted, err := os.ReadFile(filepath.Join(m.store.dir, "pairing-code"))
	if err != nil || string(persisted) != payload.Code {
		t.Fatal("pairing code not persisted")
	}
	info, err := os.Stat(filepath.Join(m.store.dir, "pairing-code"))
	// Windows has no permission bits (the data directory is per user).
	if err != nil || (goruntime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatal("pairing code permissions")
	}
	restarted := NewServer(m, assets, s.Host, string(persisted))
	if restarted.initErr != nil || !restarted.pairingRequired {
		t.Fatal("enabled mode not persisted")
	}
	if w := request(t, restarted, s.Host, "GET", "/api/settings", "", cookie); w.Code != 401 {
		t.Fatal("restart retained old auth")
	}
	loginBody, _ := json.Marshal(map[string]string{"code": payload.Code})
	if w := request(t, restarted, s.Host, "POST", "/api/login", string(loginBody), nil); w.Code != 200 {
		t.Fatal("persisted pairing code rejected")
	}
	disable := request(t, s, s.Host, "PUT", "/api/settings", `{"pairingRequired":false}`, cookie)
	if disable.Code != 200 {
		t.Fatal(disable.Code)
	}
	check("/api/settings", nil, 200)
	check("/api/settings/pairing-code", nil, 400)
	off := NewServer(m, assets, s.Host, string(persisted))
	if off.pairingRequired {
		t.Fatal("disabled mode not persisted")
	}
	// Existing key files alone must not enable pairing, including upgrades.
	_, err = m.store.db.Exec("DELETE FROM settings")
	if err != nil {
		t.Fatal(err)
	}
	legacy := NewServer(m, assets, s.Host, string(persisted))
	if legacy.pairingRequired {
		t.Fatal("legacy code enabled pairing")
	}
	again := request(t, s, s.Host, "PUT", "/api/settings", `{"pairingRequired":true}`, nil)
	if again.Code != 200 {
		t.Fatal(again.Code)
	}
	check("/api/settings", cookie, 401)
}
