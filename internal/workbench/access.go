package workbench

import (
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
)

// Who may use which Host name.
//
// From this machine and private networks (LAN, link-local, CGNAT as used by
// Tailscale), any IP address and the names that only resolve locally
// (localhost, single-label names, .local and similar, this machine's
// hostname) follow the pairing switch. Any other domain name needs a login:
// a website could point its own name at this machine (DNS rebinding) and
// drive the agents from the visitor's browser. From public addresses only
// the canonical host is served, and always with a login.
type hostAccess int

const (
	hostDenied hostAccess = iota
	hostOpen              // follows the pairing switch
	hostLogin             // needs a login whatever the switch says
)

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func privateClient(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || cgnat.Contains(addr)
}

var localSuffixes = []string{".localhost", ".local", ".lan", ".home", ".home.arpa", ".internal", ".localdomain"}

// localName reports names that resolve only on this machine or the local
// network, which no website can point elsewhere.
func localName(name, hostname string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	if name == "" {
		return false
	}
	if _, err := netip.ParseAddr(strings.Trim(name, "[]")); err == nil {
		return true
	}
	if name == "localhost" || !strings.Contains(name, ".") {
		return true
	}
	for _, s := range localSuffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	if hostname = strings.ToLower(hostname); hostname != "" {
		short, _, _ := strings.Cut(hostname, ".")
		if name == hostname || name == short {
			return true
		}
	}
	return false
}

func (s *Server) hostAccess(r *http.Request) hostAccess {
	host := strings.ToLower(r.Host)
	if !privateClient(r.RemoteAddr) {
		if host == s.Host {
			return hostLogin
		}
		return hostDenied
	}
	if host == s.Host {
		return hostOpen
	}
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	if localName(name, s.hostname) {
		return hostOpen
	}
	return hostLogin
}

var machineHostname = func() string {
	h, _ := os.Hostname()
	return h
}
