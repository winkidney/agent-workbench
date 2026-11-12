package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type grant struct {
	preview string
	expires time.Time
}
type Server struct {
	Manager    *Manager
	Assets     fs.FS
	Host       string
	Pairing    string
	initErr    error
	creationMu sync.Mutex
	mu         sync.Mutex
	// remote is how other devices sign in (auth.go); user and pass are the
	// password sign-in, empty until first turned on.
	remote     string
	user, pass string
	// fileGrants sign in file previews on the workbench's own address;
	// signinGen ends those (and pairing sessions) when sign-in rules change.
	fileGrants map[string]fileGrant
	signinGen  uint64
	// localSignin makes this computer sign in too, for a workbench reached
	// through a tunnel here (read without the lock by hostAccess).
	localSignin atomic.Bool
	// allowedOrigins may make changes besides the page's own host (auth.go).
	allowedOrigins []string
	auth           map[string]time.Time
	grants         map[string]grant
	attempts       map[string][]time.Time
	// build identifies the embedded web build, so open pages can tell
	// they run an older one.
	build string
	// stopToken and stop serve the command's local shutdown (control.go).
	stopToken string
	stop      func()
	// Where and how this workbench runs, for the About tab (about.go).
	listen, mode string
	started      time.Time
}

// assetBuild hashes the embedded index page; Vite renames the bundles it
// links whenever the web build changes.
func assetBuild(assets fs.FS) string {
	if assets == nil {
		return ""
	}
	data, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:6])
}

func NewServer(m *Manager, assets fs.FS, host, pairing string) *Server {
	s := &Server{Manager: m, Assets: assets, Host: host, Pairing: pairing, auth: map[string]time.Time{}, grants: map[string]grant{}, attempts: map[string][]time.Time{}, fileGrants: map[string]fileGrant{}, build: assetBuild(assets)}
	if _, port, err := net.SplitHostPort(host); err == nil {
		m.mu.Lock()
		m.previewPort = port
		m.mu.Unlock()
	}
	// On errors other devices stay refused.
	s.remote, s.initErr = loadRemoteAccess(m.store.db)
	if s.initErr == nil {
		s.initErr = s.loadCredentialsLocked()
	}
	if s.initErr == nil {
		var on bool
		on, s.initErr = loadLocalSignin(m.store.db)
		// Without a sign-in method nobody could get in.
		s.localSignin.Store(on && s.remote != remoteOff)
	}
	if s.initErr == nil {
		s.allowedOrigins, s.initErr = loadAllowedOrigins(m.store.db)
	}
	if s.initErr == nil {
		switch s.remote {
		case remotePairing:
			s.initErr = s.ensurePairingCode()
		case remotePassword:
			s.initErr = s.ensureCredentialsLocked()
		}
	}
	return s
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func bad(w http.ResponseWriter, err error) { http.Error(w, err.Error(), http.StatusBadRequest) }
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*1024*1024)).Decode(v)
}
func (s *Server) validSessionLocked(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	expires := s.auth[c.Value]
	if time.Now().After(expires) {
		delete(s.auth, c.Value)
		return false
	}
	return true
}

// authorized: this computer always; other devices by the remote access mode.
func (s *Server) authorized(r *http.Request) bool {
	switch s.hostAccess(r) {
	case hostLocal:
		return true
	case hostAuth:
		s.mu.Lock()
		defer s.mu.Unlock()
		switch s.remote {
		case remotePassword:
			return s.validPasswordLocked(r)
		case remotePairing:
			return s.validSessionLocked(r)
		}
	}
	return false
}

// settings is what the Settings dialog shows; secrets are fetched apart.
func (s *Server) settings(r *http.Request) map[string]any {
	s.mu.Lock()
	out := map[string]any{"remoteAccess": s.remote, "localSignin": s.localSignin.Load(), "thisComputer": thisComputer(r), "allowedOrigins": s.allowedOrigins, "signedIn": s.signedInLocked()}
	if s.user != "" {
		out["username"] = s.user
	}
	s.mu.Unlock()
	// Local data: events are a delivery buffer, kept for a week.
	out["storageBytes"] = s.Manager.store.Size()
	out["retentionDays"] = int(eventRetention / (24 * time.Hour))
	return out
}
func (s *Server) ensurePairingCode() error {
	if s.Pairing != "" {
		return nil
	}
	code := token()
	if err := writePrivateFile(s.Manager.store.dir, "pairing-code", []byte(code)); err != nil {
		return err
	}
	s.Pairing = code
	return nil
}
func (s *Server) sessionCookie(w http.ResponseWriter, key string) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: key, HttpOnly: true, SameSite: http.SameSiteStrictMode, Path: "/", MaxAge: 43200})
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.URL.Path == "/api/local/shutdown" {
		// Before the host and origin rules: the command is not a page.
		s.localShutdown(w, r)
		return
	}
	if strings.HasPrefix(r.Host, "p-") && strings.HasSuffix(r.Host, ".agent.localhost:"+strings.Split(s.Host, ":")[1]) {
		s.preview(w, r)
		return
	}
	access := s.hostAccess(r)
	if access == hostDenied {
		http.Error(w, "unrecognized host", 403)
		return
	}
	if strings.HasPrefix(r.URL.Path, filesPrefix) {
		// Signed in by a file grant the sign-in issued (files.go).
		s.serveFiles(w, r, access)
		return
	}
	if access == hostAuth {
		s.mu.Lock()
		mode := s.remote
		s.mu.Unlock()
		switch mode {
		case remotePairing:
			// The app is served; its API needs the pairing session.
		case remotePassword:
			if !s.checkPassword(w, r) {
				return
			}
		default:
			remotePage(w, http.StatusForbidden, "off", "Access from other devices is off", "This workbench only answers on the computer that runs it. To use it from here, open Settings there and choose a pairing code or a password for other devices.")
			return
		}
	}
	w.Header().Set("Referrer-Policy", "strict-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-src 'self' http://*.localhost:*; frame-ancestors 'none'")
	// Changes must come from a page on the same host the request names, or
	// an allowed origin (auth.go).
	if r.Method != "GET" && r.Method != "HEAD" && !s.originAllowed(r, access) {
		http.Error(w, originRefusal(r, access), 403)
		return
	}
	if r.URL.Path == "/api/auth/status" && r.Method == "GET" {
		authenticated := s.authorized(r)
		s.mu.Lock()
		mode := s.remote
		s.mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		// thisComputer: a browser on the workbench computer, where "Show in
		// Files" opens; elsewhere paths download.
		writeJSON(w, map[string]any{"authenticated": authenticated, "local": access == hostLocal, "thisComputer": thisComputer(r), "method": mode})
		return
	}
	if r.URL.Path == "/api/login" && r.Method == "POST" {
		s.mu.Lock()
		now := time.Now()
		limited := s.limitedLocked("pairing", r.RemoteAddr, now)
		if !limited {
			s.attemptLocked("pairing", r.RemoteAddr, now)
		}
		s.mu.Unlock()
		if limited {
			http.Error(w, "try again in a minute", 429)
			return
		}
		var input struct {
			Code string `json:"code"`
		}
		if err := decode(w, r, &input); err != nil {
			bad(w, err)
			return
		}
		s.mu.Lock()
		if s.remote != remotePairing || s.Pairing == "" || !sameSecret(input.Code, s.Pairing) {
			s.mu.Unlock()
			http.Error(w, "invalid pairing code", 401)
			return
		}
		key := token()
		for k, t := range s.auth {
			if now.After(t) {
				delete(s.auth, k)
			}
		}
		s.auth[key] = now.Add(12 * time.Hour)
		s.mu.Unlock()
		s.sessionCookie(w, key)
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
		if !s.authorized(r) {
			http.Error(w, "pair this browser first", 401)
			return
		}
		s.api(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	data, err := fs.ReadFile(s.Assets, path)
	if err != nil {
		data, err = fs.ReadFile(s.Assets, "index.html")
	}
	if err != nil {
		http.Error(w, "Build the web frontend first", 503)
		return
	}
	if strings.HasSuffix(path, ".js") {
		w.Header().Set("Content-Type", "text/javascript")
	}
	if strings.HasSuffix(path, ".css") {
		w.Header().Set("Content-Type", "text/css")
	}
	if strings.HasSuffix(path, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	// The tab icon: with nosniff, browsers show it only by its own type.
	if strings.HasSuffix(path, ".svg") {
		w.Header().Set("Content-Type", "image/svg+xml")
	}
	_, _ = w.Write(data)
}
func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	m := s.Manager
	switch {
	case path == "agents/setup":
		// Which program runs each agent and where it keeps its data.
		switch r.Method {
		case "GET":
		case "PUT":
			var v AgentsSetup
			if err := decode(w, r, &v); err != nil {
				bad(w, err)
				return
			}
			if err := m.SetAgents(v); err != nil {
				var conflict conflictError
				if errors.As(err, &conflict) {
					http.Error(w, err.Error(), http.StatusConflict)
					return
				}
				bad(w, err)
				return
			}
		default:
			http.Error(w, "method not allowed", 405)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, m.AgentsView())
	case strings.HasPrefix(path, "agents/") && (strings.HasSuffix(path, "/check") || strings.Contains(path, "/login")):
		parts := strings.Split(path, "/")
		if len(parts) < 3 || (parts[1] != "codex" && parts[1] != "claude") {
			http.NotFound(w, r)
			return
		}
		agent := parts[1]
		switch {
		case len(parts) == 3 && parts[2] == "check" && r.Method == "POST":
			var v AgentSetup
			if err := decode(w, r, &v); err != nil {
				bad(w, err)
				return
			}
			view, err := m.CheckAgent(agent, v)
			if err != nil {
				bad(w, err)
				return
			}
			writeJSON(w, view)
		case len(parts) == 3 && parts[2] == "login":
			switch r.Method {
			case "POST":
				var v struct{ Method string }
				if err := decode(w, r, &v); err != nil {
					bad(w, err)
					return
				}
				st, err := m.StartLogin(agent, v.Method)
				if err != nil {
					bad(w, err)
					return
				}
				writeJSON(w, st)
			case "GET":
				w.Header().Set("Cache-Control", "no-store")
				if st := m.loginState(agent); st != nil {
					writeJSON(w, st)
				} else {
					writeJSON(w, loginState{State: "none"})
				}
			case "DELETE":
				m.CancelLogin(agent)
				writeJSON(w, map[string]any{"ok": true})
			default:
				http.Error(w, "method not allowed", 405)
			}
		case len(parts) == 4 && parts[2] == "login" && parts[3] == "code" && r.Method == "POST":
			var v struct{ Code string }
			if err := decode(w, r, &v); err != nil {
				bad(w, err)
				return
			}
			st, err := m.LoginCode(agent, v.Code)
			if err != nil {
				bad(w, err)
				return
			}
			writeJSON(w, st)
		default:
			http.NotFound(w, r)
		}
	case strings.HasPrefix(path, "agents/"):
		parts := strings.Split(path, "/")
		if len(parts) != 3 || (parts[1] != "codex" && parts[1] != "claude") {
			http.NotFound(w, r)
			return
		}
		agent := parts[1]
		if parts[2] == "defaults" {
			if r.Method != "GET" && r.Method != "PUT" {
				http.Error(w, "method not allowed", 405)
				return
			}
			key := "defaults:" + agent
			if r.Method == "PUT" {
				var o Options
				if err := decode(w, r, &o); err != nil {
					bad(w, err)
					return
				}
				if err := m.store.setting(key, o); err != nil {
					bad(w, err)
					return
				}
			}
			// What new sessions start with: the built-in defaults (automatic
			// approvals) under the saved ones.
			writeJSON(w, m.newSessionDefaults(agent))
			return
		}
		if r.Method != "GET" || (parts[2] != "capabilities" && parts[2] != "usage") {
			http.NotFound(w, r)
			return
		}
		cwd := r.URL.Query().Get("cwd")
		if cwd == "" {
			cwd = os.TempDir()
		}
		v := m.Metadata(agent, cwd)
		writeJSON(w, v)
	case strings.HasPrefix(path, "sessions/") && strings.HasSuffix(path, "/preview-tabs") && strings.Count(path, "/") == 2:
		// What the session's preview panel had open (preview_tabs.go).
		id := strings.TrimSuffix(strings.TrimPrefix(path, "sessions/"), "/preview-tabs")
		if _, ok := m.catalog.Find(id); !ok && m.findUnlocked(id) == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		switch r.Method {
		case "GET":
			writeJSON(w, m.PreviewTabsOf(id))
		case "PUT":
			var v PreviewTabs
			if err := decode(w, r, &v); err != nil {
				bad(w, err)
				return
			}
			out, err := m.SetPreviewTabs(id, v)
			if err != nil {
				bad(w, err)
				return
			}
			writeJSON(w, out)
		default:
			http.Error(w, "method not allowed", 405)
		}
	case strings.HasPrefix(path, "sessions/") && (r.Method == "GET" || r.Method == "PATCH"):
		parts := strings.Split(path, "/")
		if len(parts) != 3 {
			http.NotFound(w, r)
			return
		}
		id := parts[1]
		if parts[2] == "live" && r.Method == "GET" {
			writeJSON(w, m.Live(id))
			return
		}
		if parts[2] == "background-output" && r.Method == "GET" {
			text, err := m.TaskOutput(id, r.URL.Query().Get("task"))
			if err != nil {
				bad(w, err)
				return
			}
			writeJSON(w, map[string]any{"text": text})
			return
		}
		if parts[2] == "commands" && r.Method == "GET" {
			// "/" commands and "$" skills for the composer.
			cat, err := m.Catalog(id)
			if err != nil {
				bad(w, err)
				return
			}
			writeJSON(w, cat)
			return
		}
		if parts[2] == "telemetry" && r.Method == "GET" {
			m.ContextUsage(id)
			writeJSON(w, m.Telemetry(id))
			return
		}
		if parts[2] == "options" && r.Method == "PATCH" {
			var o Options
			if err := decode(w, r, &o); err != nil {
				bad(w, err)
				return
			}
			if err := m.SetOptions(id, o); err != nil {
				bad(w, err)
				return
			}
			writeJSON(w, m.Telemetry(id))
			return
		}
		http.NotFound(w, r)
	case path == "settings" && r.Method == "GET":
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, s.settings(r))
	case path == "settings" && r.Method == "PUT":
		var input struct {
			RemoteAccess *string `json:"remoteAccess"`
			LocalSignin  *bool   `json:"localSignin"`
			// Allowed origins, replaced as a whole when given.
			AllowedOrigins *[]string `json:"allowedOrigins"`
			Username       *string   `json:"username"`
			Password       *string   `json:"password"`
		}
		if err := decode(w, r, &input); err != nil {
			bad(w, err)
			return
		}
		if input.RemoteAccess == nil || !validRemoteAccess(*input.RemoteAccess) {
			bad(w, errors.New("remoteAccess must be off, pairing or password"))
			return
		}
		mode := *input.RemoteAccess
		s.mu.Lock()
		before, user, pass := s.remote, s.user, s.pass
		localBefore := s.localSignin.Load()
		local := localBefore
		if input.LocalSignin != nil {
			local = *input.LocalSignin
		}
		if local && mode == remoteOff {
			s.mu.Unlock()
			bad(w, errors.New("signing in on this computer needs a pairing code or a password"))
			return
		}
		origins := s.allowedOrigins
		if input.AllowedOrigins != nil {
			var err error
			if origins, err = cleanOrigins(*input.AllowedOrigins); err != nil {
				s.mu.Unlock()
				bad(w, err)
				return
			}
		}
		if input.Username != nil {
			user = *input.Username
		}
		if input.Password != nil {
			pass = *input.Password
		}
		changed := user != s.user || pass != s.pass
		var err error
		switch {
		case changed:
			if err = checkCredentials(user, pass); err == nil {
				err = s.saveCredentialsLocked(user, pass)
			}
		case mode == remotePassword:
			err = s.ensureCredentialsLocked()
		}
		if err == nil && mode == remotePairing {
			err = s.ensurePairingCode()
		}
		if err == nil {
			list, _ := json.Marshal(origins)
			err = saveSettings(m.store.db, map[string]string{"remote_access": mode, "local_signin": strconv.FormatBool(local), "allowed_origins": string(list)})
		}
		if err != nil {
			s.mu.Unlock()
			bad(w, err)
			return
		}
		s.remote = mode
		s.localSignin.Store(local)
		s.allowedOrigins = origins
		if mode != before || changed || local != localBefore {
			// Sign-ins under the old rules end, file grants with them.
			s.auth = map[string]time.Time{}
			s.signinGen++
		}
		if mode == remotePairing && s.hostAccess(r) == hostAuth && !s.validSessionLocked(r) {
			// The device that saved stays connected where pairing applies.
			key := token()
			s.auth[key] = time.Now().Add(12 * time.Hour)
			s.sessionCookie(w, key)
		}
		s.mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, s.settings(r))
	case path == "settings/signout-others" && r.Method == "POST":
		s.mu.Lock()
		if s.remote != remotePairing {
			s.mu.Unlock()
			bad(w, errors.New("only pairing sign-ins end here; a new password signs every device out"))
			return
		}
		ended := s.signOutOthersLocked(r)
		s.mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]any{"ended": ended, "settings": s.settings(r)})
	case path == "about" && r.Method == "GET":
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, s.about())
	case path == "updates" && r.Method == "GET":
		tag, link, err := LatestRelease()
		if err != nil {
			http.Error(w, "could not check for updates: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{"current": Version, "latest": tag, "url": link})
	case path == "settings/secrets" && r.Method == "GET":
		s.mu.Lock()
		secrets := map[string]any{}
		if s.remote == remotePairing {
			secrets["pairingCode"] = s.Pairing
		}
		if s.user != "" {
			secrets["username"], secrets["password"] = s.user, s.pass
		}
		s.mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, secrets)
	case path == "dirs" && r.Method == "GET":
		v, err := listDirs(r.URL.Query().Get("path"))
		switch {
		case errors.Is(err, errOutsideHome):
			http.Error(w, err.Error(), http.StatusForbidden)
		case errors.Is(err, fs.ErrNotExist):
			http.Error(w, "folder not found", http.StatusNotFound)
		case err != nil:
			bad(w, err)
		default:
			writeJSON(w, v)
		}
	case path == "preferences" && r.Method == "GET":
		writeJSON(w, m.Preferences())
	case path == "preferences" && r.Method == "PATCH":
		var v struct {
			Suggestions *bool `json:"suggestions"`
		}
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		p := m.Preferences()
		if v.Suggestions != nil {
			p.Suggestions = *v.Suggestions
		}
		if err := m.SetPreferences(p); err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, p)
	case path == "sidebar" && r.Method == "GET":
		writeJSON(w, m.Prefs())
	case path == "sidebar" && r.Method == "POST":
		var v struct{ Op, ID string }
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		p, err := m.UpdatePrefs(v.Op, v.ID)
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, p)
	case path == "workspaces" && r.Method == "GET":
		v, err := m.catalog.Workspaces()
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, v)
	case path == "sessions" && r.Method == "GET":
		v, err := m.NativeList()
		if err != nil {
			bad(w, err)
			return
		}
		if size, _ := strconv.Atoi(r.URL.Query().Get("limit")); size > 0 {
			if size > 200 {
				size = 200
			}
			start, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
			if start < 0 {
				start = 0
			}
			if start > len(v) {
				start = len(v)
			}
			end := start + size
			if end > len(v) {
				end = len(v)
			}
			next := ""
			if end < len(v) {
				next = strconv.Itoa(end)
			}
			writeJSON(w, map[string]any{"data": v[start:end], "nextCursor": next})
			return
		}
		writeJSON(w, v)
	case path == "sessions" && r.Method == "POST":
		var v struct {
			Agent, Cwd, Title, InitialMessage, RequestID string
			Options                                      *Options `json:",omitempty"`
		}
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		if strings.TrimSpace(v.InitialMessage) == "" {
			bad(w, errors.New("first message is required"))
			return
		}
		s.creationMu.Lock()
		defer s.creationMu.Unlock()
		raw, _ := json.Marshal(v)
		hash := fmt.Sprintf("%x", sha256.Sum256(raw))
		if v.RequestID != "" {
			var savedHash, response string
			if m.store.db.QueryRow("SELECT payload_hash,response FROM native_creations WHERE request_id=?", v.RequestID).Scan(&savedHash, &response) == nil {
				if savedHash != hash {
					bad(w, errors.New("creation request changed; open a new creation dialog"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(response))
				return
			}
		}
		session, err := m.CreateSession(v.Agent, v.Cwd, v.Title, v.InitialMessage)
		if err != nil {
			bad(w, err)
			return
		}
		options := m.newSessionDefaults(v.Agent)
		if v.Options != nil {
			options = *v.Options
		}
		m.store.setting("options:"+session.ID, options)
		if err := m.Send(session.ID, v.InitialMessage); err != nil {
			bad(w, err)
			return
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			m.mu.Lock()
			ready := m.find(session.ID).session.NativeID != ""
			m.mu.Unlock()
			if ready {
				break
			}
			if time.Now().After(deadline) {
				bad(w, errors.New("native session creation not confirmed; inspect session errors"))
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		m.mu.Lock()
		created := m.find(session.ID).session
		m.store.setting("options:"+created.ID, options)
		m.mu.Unlock()
		m.catalog.mu.Lock()
		m.catalog.updated = time.Time{}
		m.catalog.mu.Unlock()
		if v.RequestID != "" {
			data, _ := json.Marshal(created)
			if _, err := m.store.db.Exec("INSERT INTO native_creations(request_id,payload_hash,response,created) VALUES(?,?,?,?)", v.RequestID, hash, string(data), nowStamp()); err != nil {
				bad(w, err)
				return
			}
		}
		writeJSON(w, created)
	case path == "events" && r.Method == "GET":
		s.events(w, r)
	case path == "history" && r.Method == "GET":
		id := r.URL.Query().Get("session")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 500 {
			limit = 200
		}
		if err := m.Attach(id); err != nil {
			bad(w, err)
			return
		}
		m.mu.Lock()
		rt := m.find(id)
		native := rt.session
		live := rt.turn
		m.mu.Unlock()
		v, err := m.catalog.History(native, r.URL.Query().Get("before"), limit)
		if err != nil {
			bad(w, err)
			return
		}
		if !live && m.isExternal(native) {
			live, _ = m.externalRunning(native)
		}
		// A running turn's unfinished tools are still running, and the
		// separate reader reports a running Codex turn as interrupted.
		v.Running = live
		if live {
			v.Events = dropLiveTurnSummary(v.Events)
		}
		writeJSON(w, v)
	case path == "download" && (r.Method == "GET" || r.Method == "HEAD"):
		s.download(w, r)
	case (path == "media" || path == "thumb") && r.Method == "GET":
		if err := m.Attach(r.URL.Query().Get("session")); err != nil {
			bad(w, err)
			return
		}
		s.media(w, r)
	case path == "files" && r.Method == "GET":
		if err := m.Attach(r.URL.Query().Get("session")); err != nil {
			bad(w, err)
			return
		}
		s.files(w, r)
	case path == "previews" && r.Method == "GET":
		m.mu.Lock()
		list := []*Preview{}
		for _, session := range m.sessions {
			for _, p := range session.previews {
				list = append(list, p)
			}
		}
		m.mu.Unlock()
		writeJSON(w, list)
	case strings.HasPrefix(path, "previews/") && strings.HasSuffix(path, "/authorize") && r.Method == "POST":
		parts := strings.Split(path, "/")
		if len(parts) != 3 {
			http.NotFound(w, r)
			return
		}
		p := m.FindPreview(parts[1])
		if p == nil {
			http.NotFound(w, r)
			return
		}
		s.previewResponse(w, r, p)
	case path == "previews" && r.Method == "POST":
		var v struct {
			Session, Kind, Path, Base, Target, Command string
			Port                                       int
		}
		if err := decode(w, r, &v); err != nil {
			bad(w, err)
			return
		}
		if err := m.Attach(v.Session); err != nil {
			bad(w, err)
			return
		}
		p, err := m.CreatePreview(v.Session, v.Kind, v.Path, v.Base, v.Target, v.Command, v.Port, s.Host)
		if err != nil {
			bad(w, err)
			return
		}
		s.previewResponse(w, r, p)
	case strings.HasPrefix(path, "previews/") && r.Method == "DELETE":
		if err := m.RemovePreview(strings.TrimPrefix(path, "previews/")); err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case strings.HasPrefix(path, "sessions/") && r.Method == "DELETE":
		parts := strings.Split(path, "/")
		if len(parts) != 4 || parts[2] != "queue" {
			http.NotFound(w, r)
			return
		}
		if err := m.RemoveQueued(parts[1], parts[3]); err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case strings.HasPrefix(path, "sessions/") && r.Method == "POST" && strings.HasSuffix(path, "/steer") && strings.Count(path, "/") == 4:
		// sessions/{id}/queue/{qid}/steer: send a queued message into the turn.
		parts := strings.Split(path, "/")
		if parts[2] != "queue" {
			http.NotFound(w, r)
			return
		}
		res, err := m.SteerQueued(parts[1], parts[3])
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, res)
	case strings.HasPrefix(path, "sessions/") && r.Method == "POST":
		parts := strings.Split(path, "/")
		if len(parts) != 3 {
			http.NotFound(w, r)
			return
		}
		id, action := parts[1], parts[2]
		var err error
		switch action {
		case "send":
			var v struct {
				Text  string
				Queue bool
				// Steer: into the running turn now (a plain send when idle).
				Steer bool
			}
			err = decode(w, r, &v)
			if err == nil && v.Steer {
				var res SteerResult
				if res, err = m.Steer(id, v.Text); err == nil {
					writeJSON(w, res)
					return
				}
			} else if err == nil && v.Queue {
				var wasQueued bool
				if wasQueued, err = m.Enqueue(id, v.Text); err == nil {
					writeJSON(w, map[string]any{"ok": true, "queued": wasQueued})
					return
				}
			} else if err == nil {
				err = m.Send(id, v.Text)
			}
		case "queue-resume":
			err = m.ResumeQueue(id)
		case "send-now":
			// Claude: take a steered message that is still waiting in now.
			var v struct{ UUID string }
			if err = decode(w, r, &v); err == nil {
				var answer string
				if answer, err = m.SendNow(id, v.UUID); err == nil {
					writeJSON(w, map[string]any{"ok": true, "sendNow": answer})
					return
				}
			}
		case "command":
			var v struct{ Name, Args string }
			if err = decode(w, r, &v); err == nil {
				var res CommandResult
				if res, err = m.RunCommand(id, v.Name, v.Args); err == nil {
					writeJSON(w, res)
					return
				}
			}
		case "reveal":
			// Shows the file in the desktop file manager of this machine:
			// only for a browser on it (elsewhere, paths download).
			var v struct{ Path, Base string }
			if !thisComputer(r) {
				err = errors.New("Show in Files opens on the workbench computer; download the file instead")
			} else if err = decode(w, r, &v); err == nil {
				if err = m.Attach(id); err == nil {
					var full, rel string
					if full, rel, err = m.ResolvePath(id, v.Base, v.Path); err == nil {
						if err = reveal(full); err == nil {
							writeJSON(w, map[string]any{"ok": true, "path": filepath.ToSlash(rel)})
							return
						}
					}
				}
			}
		case "rename":
			var v struct{ Title string }
			if err = decode(w, r, &v); err == nil {
				var native bool
				if native, err = m.Rename(id, v.Title); err == nil {
					writeJSON(w, map[string]any{"ok": true, "native": native})
					return
				}
			}
		case "interrupt":
			err = m.Interrupt(id)
		case "plan-dismiss":
			err = m.DismissPlan(id)
		case "archive":
			err = m.Archive(id)
		case "unarchive":
			err = m.Unarchive(id)
		case "delete":
			err = m.Delete(id)
		case "sleep":
			// force: stop the background work too, which would otherwise keep
			// the engine awake.
			var v struct{ Force bool }
			_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&v)
			if v.Force {
				err = m.SleepStoppingBackground(id)
			} else {
				err = m.Stop(id, true)
			}
		case "background-stop":
			var v struct{ Task string }
			if err = decode(w, r, &v); err == nil {
				err = m.StopBackground(id, v.Task)
			}
		case "to-background":
			var v struct{ ToolUseID string }
			if err = decode(w, r, &v); err == nil {
				err = m.MoveToBackground(id, v.ToolUseID)
			}
		case "stop":
			err = m.Stop(id, false)
		case "resume":
			err = m.Start(id)
		case "reply":
			var v struct {
				ID, Decision, Message string
				Allow                 bool
				Answers               map[string]any
				// The permission mode an approved Claude plan continues in.
				Mode string
			}
			err = decode(w, r, &v)
			if err == nil {
				if v.Decision == "" {
					v.Decision = "deny"
					if v.Allow {
						v.Decision = "allow"
					}
				}
				err = m.ReplyDecision(id, v.ID, v.Decision, v.Message, v.Answers, v.Mode)
			}
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.NotFound(w, r)
	}
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	after, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	if after == 0 {
		after, _ = strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	}
	if after == 0 {
		_ = s.Manager.store.db.QueryRow("SELECT coalesce(max(seq),0) FROM events").Scan(&after)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	session := r.URL.Query().Get("session")
	var telemetrySent time.Time
	hello, _ := json.Marshal(map[string]string{"build": s.build})
	fmt.Fprintf(w, "event: hello\ndata: %s\n\n", hello)
	// A session another client runs is followed through its native record
	// for as long as this page views it.
	var follow *externalTail
	if session != "" {
		follow = s.Manager.followExternal(session)
		defer follow.close()
	}
	for {
		if !s.authorized(r) {
			return
		}
		changed := s.Manager.changes.C()
		events, err := s.Manager.store.Events("", after, 100)
		if err != nil {
			return
		}
		for _, e := range events {
			b, _ := json.Marshal(e)
			if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.Seq, b); err != nil {
				return
			}
			after = e.Seq
		}
		if len(events) == 100 {
			flusher.Flush()
			continue
		}
		if session != "" && time.Since(telemetrySent) >= time.Second {
			data, _ := json.Marshal(s.Manager.Telemetry(session))
			fmt.Fprintf(w, "event: telemetry\ndata: %s\n\n", data)
			telemetrySent = time.Now()
		}
		if follow != nil {
			if u := follow.poll(); u != nil {
				data, _ := json.Marshal(u)
				fmt.Fprintf(w, "event: external\ndata: %s\n\n", data)
			}
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
		}
	}
}

// media serves raster images and videos from the session's launch directory
// for thumbnails and the media viewer. A path is resolved where the agent
// wrote it (base), as in previews. Active content (SVG, HTML) only opens in
// the isolated preview origin, never on the workbench origin.
func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	ext := strings.ToLower(filepath.Ext(path))
	var video bool
	var limit int64
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".avif":
		limit = 32 << 20
	case ".mp4", ".webm", ".mov", ".m4v":
		video, limit = true, 4<<30
	default:
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	full, _, err := s.Manager.ResolvePath(q.Get("session"), q.Get("base"), path)
	if errors.Is(err, fs.ErrNotExist) {
		// Paths mentioned in text are probed; a missing file is not a client error.
		http.NotFound(w, r)
		return
	}
	if err != nil {
		bad(w, err)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		http.NotFound(w, r)
		return
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	mime := http.DetectContentType(head[:n])
	switch {
	case !video && strings.HasPrefix(mime, "image/") && !strings.Contains(mime, "svg"):
	case video && strings.HasPrefix(mime, "video/"):
	case video && mime == "application/octet-stream" && (ext == ".mov" || ext == ".m4v"):
		// QuickTime brands are not sniffed; browsers play H.264 ones as MP4.
		mime = "video/mp4"
	default:
		http.Error(w, "not a supported image or video", http.StatusUnsupportedMediaType)
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "private, max-age=30")
	// ServeContent answers Range requests, so videos can seek.
	http.ServeContent(w, r, "", info.ModTime(), f)
}
func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	m := s.Manager
	m.mu.Lock()
	v := m.find(r.URL.Query().Get("session"))
	if v == nil {
		m.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	root := v.session.Cwd
	m.mu.Unlock()
	path := r.URL.Query().Get("path")
	full, err := confined(root, path)
	if err != nil {
		bad(w, err)
		return
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		bad(w, err)
		return
	}
	out := []map[string]any{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" || e.Name() == "vendor" || e.Name() == "target" {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, map[string]any{"name": e.Name(), "path": filepath.ToSlash(filepath.Join(path, e.Name())), "directory": e.IsDir(), "size": info.Size()})
		if len(out) == 1000 {
			break
		}
	}
	writeJSON(w, out)
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	host := strings.Split(r.Host, ":")[0]
	id := strings.TrimSuffix(strings.TrimPrefix(host, "p-"), ".agent.localhost")
	p := s.Manager.FindPreview(id)
	if p == nil {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/__authorize" && r.Method == "POST" {
		if r.Header.Get("Origin") != "http://"+s.Host {
			http.Error(w, "invalid origin", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			bad(w, err)
			return
		}
		key := r.FormValue("grant")
		s.mu.Lock()
		g, ok := s.grants[key]
		delete(s.grants, key)
		s.mu.Unlock()
		if !ok || g.preview != id || time.Now().After(g.expires) {
			http.Error(w, "expired preview grant", 401)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "preview_access", Value: p.secret, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, p.Entry, http.StatusSeeOther)
		return
	}
	c, err := r.Cookie("preview_access")
	if err != nil || c.Value != p.secret {
		http.Error(w, "preview requires authorization", 401)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") && p.target == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", "frame-ancestors http://"+s.Host)
	p.ServeHTTP(w, r)
}
func Run(ctx context.Context, m *Manager, assets fs.FS, addr, code string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return errors.New("listen address must be <host>:<port>")
	}
	handler := NewServer(m, assets, "agent.localhost:"+port, code)
	if handler.initErr != nil {
		return handler.initErr
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	handler.stopToken, handler.stop = token(), cancel
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	mode := "direct"
	if UnderSystemd() {
		mode = "systemd"
	}
	handler.listen, handler.mode, handler.started = addr, mode, time.Now()
	if err := writeServerInfo(m.store.dir, ServerInfo{PID: os.Getpid(), Listen: addr, Version: Version, Mode: mode, Token: handler.stopToken}); err != nil {
		listener.Close()
		return err
	}
	defer removeServerInfo(m.store.dir)
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		// Related-project chips: a bounded, incremental pass every minute.
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				m.IndexActivity(512 << 20)
				timer.Reset(time.Minute)
			}
		}
	}()
	go func() {
		// Old events, unused caches and the rows of sessions gone go at
		// startup and then hourly.
		m.Tidy()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.Tidy()
			}
		}
	}()
	stopped := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				m.Shutdown()
				close(stopped)
				// Open event streams hold Shutdown up to its timeout; the
				// process may exit before that.
				shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				_ = server.Shutdown(shutdown)
				return
			case <-ticker.C:
				m.Sweep()
			}
		}
	}()
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		// Serve returns as Shutdown begins, after the engines stopped.
		<-stopped
		return nil
	}
	return err
}
