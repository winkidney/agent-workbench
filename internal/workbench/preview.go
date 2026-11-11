package workbench

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Preview struct {
	ID      string `json:"id"`
	Session string `json:"session"`
	URL     string `json:"url"`
	Entry   string `json:"entry"`
	Owned   bool   `json:"owned"`
	// stop ends a managed service preview (its unit or process group).
	stop   func() error
	root   string
	target *url.URL
	secret string
}

func confined(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", errors.New("path must be relative")
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, path))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path outside working directory")
	}
	return full, nil
}

// resolveSessionPath maps a path as a session wrote it to a file under the
// session's launch directory (root). A relative path is relative to where
// the agent was (base), so it is tried there first, then at the root, then
// in subprojects the session worked in. Nothing outside root resolves.
func resolveSessionPath(root, base, path string, related []string) (string, string, error) {
	path = strings.TrimSpace(strings.TrimPrefix(path, "file://"))
	if path == "" {
		return "", "", errors.New("empty path")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	// within reports p relative to the root (as configured or resolved).
	within := func(p string) (string, bool) {
		for _, r := range []string{realRoot, filepath.Clean(root)} {
			rel, err := filepath.Rel(r, p)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return rel, true
			}
		}
		return "", false
	}
	var dirs []string
	if filepath.IsAbs(path) {
		dirs = []string{""}
	} else {
		if filepath.IsAbs(base) {
			b := filepath.Clean(base)
			if _, ok := within(b); !ok {
				// Written through a symlink or a short name (macOS /var,
				// Windows RUNNER~1): judged by where it really is.
				if real, err := filepath.EvalSymlinks(b); err == nil {
					b = real
				}
			}
			if _, ok := within(b); ok {
				dirs = append(dirs, b)
			}
		}
		dirs = append(dirs, filepath.Clean(root))
		for _, sub := range related {
			dirs = append(dirs, filepath.Join(root, sub))
		}
	}
	var looked []string
	seen := map[string]bool{}
	for _, dir := range dirs {
		candidate := filepath.Join(dir, path)
		if dir == "" {
			candidate = filepath.Clean(path)
		}
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		// A ../ escape is refused before looking, existing or not. An
		// absolute path through a symlink is judged by where it really is.
		if _, ok := within(candidate); !ok {
			real, err := filepath.EvalSymlinks(candidate)
			if dir != "" || err != nil {
				return "", "", errors.New("path outside working directory")
			}
			rel, ok := within(real)
			if !ok {
				return "", "", errors.New("path outside working directory")
			}
			return real, rel, nil
		}
		real, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			rel, ok := within(real)
			if !ok {
				return "", "", errors.New("path outside working directory")
			}
			return real, rel, nil
		}
		if rel, ok := within(dir); ok && dir != "" {
			looked = append(looked, filepath.ToSlash(rel)+"/")
		}
	}
	msg := "not found: " + path
	if len(looked) > 0 {
		msg += " (looked in " + strings.Join(looked, ", ") + ")"
	}
	return "", "", notFound(msg)
}

// notFound reads as written and still matches fs.ErrNotExist.
type notFound string

func (e notFound) Error() string        { return string(e) }
func (e notFound) Is(target error) bool { return target == os.ErrNotExist }

// ResolvePath resolves a path a session wrote (see resolveSessionPath).
func (m *Manager) ResolvePath(id, base, path string) (string, string, error) {
	m.mu.Lock()
	r := m.find(id)
	if r == nil {
		m.mu.Unlock()
		return "", "", errors.New("session not found")
	}
	root := r.session.Cwd
	m.mu.Unlock()
	return resolveSessionPath(root, base, path, m.activity.Related(id))
}

func (m *Manager) CreatePreview(id, kind, path, base, target, command string, port int, hostPort string) (*Preview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(id)
	if r == nil {
		return nil, errors.New("session not found")
	}
	if r.session.Status == "stopping" {
		return nil, errors.New("session is stopping")
	}
	p := &Preview{ID: token()[:24], Session: id, secret: token(), root: r.session.Cwd, Entry: "/"}
	_, listenPort, _ := net.SplitHostPort(hostPort)
	p.URL = "http://p-" + p.ID + ".agent.localhost:" + listenPort
	switch kind {
	case "file":
		full, rel, err := resolveSessionPath(r.session.Cwd, base, path, m.activity.Related(id))
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(full)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("select a regular file")
		}
		p.Entry = "/" + filepath.ToSlash(rel)
	case "service":
		logs := ""
		if command != "" {
			if port < 1024 || port > 65535 {
				return nil, errors.New("managed service requires a port between 1024 and 65535")
			}
			listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				return nil, errors.New("preview port is already in use; attach the existing service instead")
			}
			_ = listener.Close()
			if p.stop, logs, err = m.startPreviewService(p.ID, r.session.Cwd, command); err != nil {
				return nil, err
			}
			p.Owned = true
			target = "http://127.0.0.1:" + strconv.Itoa(port)
		}
		u, err := url.Parse(target)
		if err != nil || u.Scheme != "http" || u.User != nil {
			return nil, errors.New("target must be an http loopback URL")
		}
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsLoopback() || u.Port() == "" {
			if p.stop != nil {
				_ = p.stop()
			}
			return nil, errors.New("use a numeric loopback address and port")
		}
		if u.Port() == listenPort {
			if p.stop != nil {
				_ = p.stop()
			}
			return nil, errors.New("cannot proxy the workbench itself")
		}
		if p.Owned {
			deadline := time.Now().Add(10 * time.Second)
			for {
				conn, err := net.DialTimeout("tcp", u.Host, 200*time.Millisecond)
				if err == nil {
					_ = conn.Close()
					break
				}
				if time.Now().After(deadline) {
					_ = p.stop()
					return nil, errors.New("preview did not start listening within 10 seconds; see " + logs)
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
		// Proxy the origin; a URL path becomes the iframe's entry page.
		if entry := u.RequestURI(); entry != "" && !strings.HasPrefix(entry, "//") {
			p.Entry = entry
		}
		p.target = &url.URL{Scheme: u.Scheme, Host: u.Host}
	default:
		return nil, errors.New("kind must be file or service")
	}
	r.previews[p.ID] = p
	r.last = time.Now()
	return p, nil
}
func (m *Manager) FindPreview(id string) *Preview {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.sessions {
		if p := r.previews[id]; p != nil {
			return p
		}
	}
	return nil
}
func (m *Manager) RemovePreview(id string) error {
	m.mu.Lock()
	var found *Preview
	for _, r := range m.sessions {
		if p := r.previews[id]; p != nil {
			found = p
			delete(r.previews, id)
			r.last = time.Now()
			break
		}
	}
	m.mu.Unlock()
	if found == nil {
		return errors.New("preview not found")
	}
	if found.stop != nil {
		return found.stop()
	}
	return nil
}
func (p *Preview) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.target != nil {
		proxy := httputil.NewSingleHostReverseProxy(p.target)
		director := proxy.Director
		proxy.Director = func(req *http.Request) {
			director(req)
			req.Host = p.target.Host
			req.Header.Del("Cookie")
			req.Header.Del("Authorization")
			req.Header.Del("Origin")
		}
		proxy.ModifyResponse = func(resp *http.Response) error { resp.Header.Del("Set-Cookie"); return nil }
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "Preview is starting or unavailable", 502)
		}
		proxy.ServeHTTP(w, r)
		return
	}
	root, err := os.OpenRoot(p.root)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	file, err := root.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

// startPreviewService starts a preview's command line in the session's
// directory: a systemd unit bound to the workbench (logs in the journal) when
// the workbench is a systemd service, otherwise a child process group whose
// output goes to <data dir>/previews/<id>.log.
func (m *Manager) startPreviewService(id, cwd, command string) (stop func() error, logs string, err error) {
	if UnderSystemd() {
		sh, err := exec.LookPath("sh")
		if err != nil {
			return nil, "", err
		}
		unit := "agent-workbench-preview-" + id + ".service"
		args := []string{}
		for _, a := range unitArgs(unit, cwd) {
			// No --pipe: this child is independently managed and logs to the journal.
			if a != "--pipe" && a != "--wait" {
				args = append(args, a)
			}
		}
		args = append(args, "--", sh, "-lc", command)
		if out, err := exec.Command("systemd-run", args...).CombinedOutput(); err != nil {
			return nil, "", fmt.Errorf("start preview: %s: %w", out, err)
		}
		return func() error { return stopUnit(unit) }, "journalctl --user -u " + unit, nil
	}
	cmd, err := shellCommand(command)
	if err != nil {
		return nil, "", err
	}
	cmd.Dir = cwd
	dir := filepath.Join(m.store.dir, "previews")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	logs = filepath.Join(dir, id+".log")
	f, err := os.OpenFile(logs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, "", err
	}
	cmd.Stdout, cmd.Stderr = f, f
	group, err := startGroup(cmd)
	f.Close() // The child has its own copy.
	if err != nil {
		return nil, "", fmt.Errorf("start preview: %w", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	var once sync.Once
	return func() error {
		once.Do(func() {
			pids := group.PIDs()
			group.Terminate(pids)
			if !waitClosed(exited, 10*time.Second) {
				group.Kill(pids)
				waitClosed(exited, 5*time.Second)
			}
			// The shell may be gone while what it started still runs.
			group.Kill(nil)
			group.Release()
		})
		return nil
	}, logs, nil
}
