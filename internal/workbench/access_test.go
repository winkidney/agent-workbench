package workbench

import (
	"net/http/httptest"
	"testing"
)

func TestOnlyThisComputerIsLocal(t *testing.T) {
	s := &Server{Host: "agent.localhost:17880"}
	const loop, lan, public = "127.0.0.1:5000", "192.168.1.20:5000", "203.0.113.7:5000"
	for _, c := range []struct {
		host, remote string
		want         hostAccess
	}{
		{"localhost:17880", loop, hostLocal},
		{"agent.localhost:17880", loop, hostLocal},
		{"AGENT.localhost:17880", loop, hostLocal},
		{"127.0.0.1:17880", loop, hostLocal},
		{"127.0.0.2:17880", loop, hostLocal},
		{"[::1]:17880", "[::1]:5000", hostLocal},
		{"localhost:17880", "[::1]:5000", hostLocal},
		{"localhost.:17880", loop, hostLocal},
		{"localhost:17880", "[::ffff:127.0.0.1]:5000", hostLocal},
		// From this computer, names that are not loopback sign in: a
		// website pointed at 127.0.0.1, this machine's name or LAN address.
		{"evil.test:17880", loop, hostAuth},
		{"localhost.evil.test:17880", loop, hostAuth},
		{"devbox:17880", loop, hostAuth},
		{"192.168.1.5:17880", loop, hostAuth},
		// Another device cannot claim to be this computer.
		{"localhost:17880", lan, hostAuth},
		{"agent.localhost:17880", lan, hostAuth},
		{"127.0.0.1:17880", lan, hostAuth},
		{"192.168.1.5:17880", lan, hostAuth},
		{"devbox.local:17880", "100.101.102.103:5000", hostAuth},
		{"192.168.1.5:17880", "[fd00::2]:5000", hostAuth},
		// From the internet only the canonical host, and it signs in.
		{"agent.localhost:17880", public, hostAuth},
		{"192.168.1.5:17880", public, hostDenied},
		{"evil.test:17880", public, hostDenied},
	} {
		r := httptest.NewRequest("GET", "http://"+c.host+"/", nil)
		r.Host, r.RemoteAddr = c.host, c.remote
		if got := s.hostAccess(r); got != c.want {
			t.Errorf("%s from %s: %d, want %d", c.host, c.remote, got, c.want)
		}
	}
}

func TestClientAddresses(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:1": true, "[::1]:1": true, "192.168.0.9:1": true, "10.1.2.3:1": true,
		"172.20.0.1:1": true, "169.254.1.1:1": true, "100.101.102.103:1": true, "[fd00::1]:1": true,
		"[::ffff:192.168.1.2]:1": true, "8.8.8.8:1": false, "192.0.2.1:1234": false, "garbage": false,
	} {
		if got := privateClient(addr); got != want {
			t.Errorf("%s: %v", addr, got)
		}
	}
	for addr, want := range map[string]bool{
		"127.0.0.1:1": true, "127.9.9.9:1": true, "[::1]:1": true, "[::ffff:127.0.0.1]:1": true,
		"192.168.0.9:1": false, "[fd00::1]:1": false, "garbage": false,
	} {
		if got := loopbackClient(addr); got != want {
			t.Errorf("loopback %s: %v", addr, got)
		}
	}
}
