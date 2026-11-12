package workbench

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInstanceNamesUnitsAndCookie(t *testing.T) {
	defer SetInstance(DefaultInstance)
	if sessionCookie != "workbench_session" || Service != "agent-workbench.service" {
		t.Fatalf("default instance: %s %s", sessionCookie, Service)
	}
	SetInstance("agent-workbench-17881")
	if Service != "agent-workbench-17881.service" || sessionCookie != "workbench_session_17881" {
		t.Fatalf("instance: %s %s", Service, sessionCookie)
	}
	args := strings.Join(unitArgs(instance+"-session-x.service", "/tmp"), " ")
	if !strings.Contains(args, "--unit=agent-workbench-17881-session-x.service") || !strings.Contains(args, "BindsTo=agent-workbench-17881.service") {
		t.Fatalf("unit args %s", args)
	}
	if cookieFor("odd name!") != "workbench_session_odd_name_" {
		t.Fatalf("cookie name %s", cookieFor("odd name!"))
	}

	// Its devices sign in with its own cookie.
	s, _ := authServer(t)
	setAccess(t, s, `{"remoteAccess":"pairing"}`)
	code := secrets(t, s)["pairingCode"]
	body, _ := json.Marshal(map[string]string{"code": code})
	w := fromLAN(t, s, "192.168.1.20:5000", "POST", "/api/login", string(body), nil)
	cookie := w.Result().Cookies()[0]
	if cookie.Name != "workbench_session_17881" {
		t.Fatalf("cookie %s", cookie.Name)
	}
	if lan(t, s, "GET", "/api/sessions", "", cookie).Code != 200 {
		t.Fatal("own cookie refused")
	}
	cookie.Name = "workbench_session"
	if lan(t, s, "GET", "/api/sessions", "", cookie).Code != 401 {
		t.Fatal("another instance's cookie accepted")
	}
}
