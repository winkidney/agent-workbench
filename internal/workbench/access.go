package workbench

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Who must sign in.
//
// Only this computer is open: a request that comes from a loopback address
// and names a loopback host (localhost, *.localhost, 127.0.0.1, [::1]). The
// Host header alone proves nothing, any client can send "localhost", so the
// source address must be loopback too; and a website that points its own
// name at 127.0.0.1 (DNS rebinding) names a host that is not loopback.
// Everything else signs in with the method chosen in Settings, or is refused
// while access from other devices is off. From public addresses only the
// canonical host is served.
type hostAccess int

const (
	hostDenied hostAccess = iota
	hostLocal             // this computer: never signs in
	hostAuth              // another device: signs in with the remote access mode
)

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func clientAddr(remote string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func privateClient(remote string) bool {
	addr, ok := clientAddr(remote)
	return ok && (addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || cgnat.Contains(addr))
}

func loopbackClient(remote string) bool {
	addr, ok := clientAddr(remote)
	return ok && addr.IsLoopback()
}

// loopbackName reports Host values that can only mean this computer.
func loopbackName(host string) bool {
	name := strings.ToLower(host)
	if h, _, err := net.SplitHostPort(name); err == nil {
		name = h
	}
	name = strings.TrimSuffix(strings.Trim(name, "[]"), ".")
	if name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(name)
	return err == nil && addr.Unmap().IsLoopback()
}

func (s *Server) hostAccess(r *http.Request) hostAccess {
	if loopbackClient(r.RemoteAddr) && loopbackName(r.Host) {
		return hostLocal
	}
	if !privateClient(r.RemoteAddr) && !strings.EqualFold(r.Host, s.Host) {
		return hostDenied
	}
	return hostAuth
}
