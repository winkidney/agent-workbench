package workbench

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"html"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// How other devices sign in (this computer never does, see hostAccess).
// One choice: with the password on, the pairing code is not asked.
const (
	remoteOff      = "off"      // other devices are refused
	remotePairing  = "pairing"  // the pairing code, then a session cookie
	remotePassword = "password" // HTTP Basic: the browser's own sign-in prompt
)

const (
	passwordFile    = "access-password.json"
	defaultUsername = "workbench"
	basicChallenge  = `Basic realm="Agent Workbench", charset="UTF-8"`
	// Failed sign-ins allowed per client address and minute.
	signInLimit = 10
)

// loadRemoteAccess reads the mode; installs from before it existed keep
// pairing if it was on, and are otherwise closed to other devices.
func loadRemoteAccess(db *sql.DB) (string, error) {
	var v string
	err := db.QueryRow("SELECT value FROM settings WHERE key='remote_access'").Scan(&v)
	switch {
	case err == nil:
		if validRemoteAccess(v) {
			return v, nil
		}
		return remoteOff, nil
	case err != sql.ErrNoRows:
		return remoteOff, err
	}
	err = db.QueryRow("SELECT value FROM settings WHERE key='pairing_required'").Scan(&v)
	if err == sql.ErrNoRows {
		return remoteOff, nil
	}
	if err != nil {
		return remoteOff, err
	}
	if v == "true" {
		return remotePairing, nil
	}
	return remoteOff, nil
}

// loadLocalSignin reads whether this computer signs in too (off by default).
func loadLocalSignin(db *sql.DB) (bool, error) {
	var v string
	err := db.QueryRow("SELECT value FROM settings WHERE key='local_signin'").Scan(&v)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return v == "true", err
}

func saveSettings(db *sql.DB, values map[string]string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for k, v := range values {
		if _, err := tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, v); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func validRemoteAccess(v string) bool {
	return v == remoteOff || v == remotePairing || v == remotePassword
}

// writePrivateFile replaces dir/name atomically with a file only this user
// can read.
func writePrivateFile(dir, name string, data []byte) error {
	f, err := os.CreateTemp(dir, "."+name+"-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loadCredentialsLocked reads the stored username and password, if any.
func (s *Server) loadCredentialsLocked() error {
	data, err := os.ReadFile(filepath.Join(s.Manager.store.dir, passwordFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var c credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return err
	}
	if checkCredentials(c.Username, c.Password) != nil {
		// A hand-edited file that no longer validates: start over.
		return nil
	}
	s.user, s.pass = c.Username, c.Password
	return nil
}

// ensureCredentialsLocked creates the default username and a new password
// the first time the password is turned on.
func (s *Server) ensureCredentialsLocked() error {
	if s.user != "" {
		return nil
	}
	return s.saveCredentialsLocked(defaultUsername, newPassword())
}

func (s *Server) saveCredentialsLocked(user, pass string) error {
	data, _ := json.Marshal(credentials{user, pass})
	if err := writePrivateFile(s.Manager.store.dir, passwordFile, data); err != nil {
		return err
	}
	s.user, s.pass = user, pass
	return nil
}

func checkCredentials(user, pass string) error {
	switch {
	case user == "" || utf8.RuneCountInString(user) > 64:
		return errors.New("the username needs 1 to 64 characters")
	case strings.Contains(user, ":"):
		return errors.New("the username cannot contain a colon")
	case utf8.RuneCountInString(pass) < 8 || utf8.RuneCountInString(pass) > 128:
		return errors.New("the password needs 8 to 128 characters")
	case strings.IndexFunc(user+pass, unicode.IsControl) >= 0:
		return errors.New("the username and password cannot contain control characters")
	}
	return nil
}

// newPassword returns 16 characters without look-alikes (no 0/o, 1/l/i),
// about 80 bits, easy to type on a phone.
func newPassword() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 16)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			panic(err)
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b)
}

func sameSecret(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}

func (s *Server) validPasswordLocked(r *http.Request) bool {
	user, pass, ok := r.BasicAuth()
	if !ok || s.user == "" {
		return false
	}
	// Both comparisons always run.
	u, p := sameSecret(user, s.user), sameSecret(pass, s.pass)
	return u && p
}

func clientIP(remote string) string {
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return host
	}
	return remote
}

// limitedLocked reports whether the client used up its sign-in attempts of
// the last minute. Per address, so one device cannot lock out the others.
func (s *Server) limitedLocked(kind, remote string, now time.Time) bool {
	key := kind + ":" + clientIP(remote)
	recent := s.attempts[key][:0]
	for _, t := range s.attempts[key] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(s.attempts, key)
	} else {
		s.attempts[key] = recent
	}
	return len(recent) >= signInLimit
}

func (s *Server) attemptLocked(kind, remote string, now time.Time) {
	key := kind + ":" + clientIP(remote)
	s.attempts[key] = append(s.attempts[key], now)
	// Forget addresses that stopped trying.
	if len(s.attempts) > 256 {
		for k, list := range s.attempts {
			if len(list) == 0 || now.Sub(list[len(list)-1]) >= time.Minute {
				delete(s.attempts, k)
			}
		}
	}
}

// checkPassword is the HTTP Basic gate for other devices: every request,
// pages included, carries the username and password the browser remembers.
func (s *Server) checkPassword(w http.ResponseWriter, r *http.Request) bool {
	now := time.Now()
	_, _, sent := r.BasicAuth()
	s.mu.Lock()
	if s.limitedLocked("password", r.RemoteAddr, now) {
		s.mu.Unlock()
		remotePage(w, http.StatusTooManyRequests, "limited", "Too many attempts", "Wrong username or password too many times. Try again in a minute.")
		return false
	}
	ok := s.validPasswordLocked(r)
	if !ok && sent {
		s.attemptLocked("password", r.RemoteAddr, now)
	}
	s.mu.Unlock()
	if !ok {
		w.Header().Set("WWW-Authenticate", basicChallenge)
		remotePage(w, http.StatusUnauthorized, "sign-in", "Sign in to Agent Workbench", "This workbench asks for a username and password. They are in Settings on the computer that runs it, and in access-password.json in its data directory. Reload this page to sign in again.")
	}
	return ok
}

// remotePage answers other devices without the app: when access from them
// is off, the browser's sign-in prompt was cancelled, or too many attempts
// failed. X-Workbench-Access tells an open page to reload and show it.
func remotePage(w http.ResponseWriter, code int, kind, title, text string) {
	w.Header().Set("X-Workbench-Access", kind)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Agent Workbench</title><style>body{font:15px/1.6 system-ui,sans-serif;max-width:34rem;margin:14vh auto;padding:0 20px;color:#1f1f1f;background:#fff}h1{font-size:20px;margin:0 0 8px}p{color:#555}@media (prefers-color-scheme:dark){body{color:#eee;background:#161616}p{color:#aaa}}</style></head><body><h1>` + html.EscapeString(title) + `</h1><p>` + html.EscapeString(text) + `</p></body></html>`))
}
