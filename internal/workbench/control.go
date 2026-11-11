package workbench

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

// Version is the release this binary was built as (set by the command).
var Version = "dev"

// ServerInfo is how the command finds a running workbench: written to
// <data dir>/server.json (mode 0600) once it listens, removed on exit.
type ServerInfo struct {
	PID     int    `json:"pid"`
	Listen  string `json:"listen"`
	Version string `json:"version"`
	// Mode is "systemd" (engines are units) or "direct" (child processes).
	Mode string `json:"mode"`
	// Token authorizes POST /api/local/shutdown.
	Token string `json:"token"`
}

const serverInfoFile = "server.json"

func writeServerInfo(dir string, info ServerInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, serverInfoFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, serverInfoFile))
}

// ReadServerInfo reads what the last workbench using dir wrote; it may
// have exited without removing it.
func ReadServerInfo(dir string) (ServerInfo, error) {
	var info ServerInfo
	data, err := os.ReadFile(filepath.Join(dir, serverInfoFile))
	if err != nil {
		return info, err
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return info, err
	}
	if info.PID <= 0 || info.Listen == "" {
		return info, errors.New("incomplete " + serverInfoFile)
	}
	return info, nil
}

// removeServerInfo removes server.json if it is still this process's.
func removeServerInfo(dir string) {
	if info, err := ReadServerInfo(dir); err == nil && info.PID == os.Getpid() {
		_ = os.Remove(filepath.Join(dir, serverInfoFile))
	}
}

const stopHeader = "X-Workbench-Stop"

// localShutdown stops the workbench for its own command (stop, install,
// uninstall): a POST from this machine carrying the token in server.json.
// Browsers cannot send the header across origins without a preflight, which
// is never answered.
func (s *Server) localShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !localAddress(net.ParseIP(host)) {
		http.Error(w, "forbidden", 403)
		return
	}
	got := r.Header.Get(stopHeader)
	if s.stopToken == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.stopToken)) != 1 {
		http.Error(w, "forbidden", 403)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	s.stop()
}

// localAddress is whether ip is loopback or one of this machine's own
// addresses (a workbench listening on one LAN address only).
func localAddress(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
			return true
		}
	}
	return false
}

// ProcessAlive is whether pid names a running process.
func ProcessAlive(pid int) bool { return processAlive(pid) }
