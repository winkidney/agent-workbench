package workbench

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// File previews on the workbench's own address.
//
// A preview normally runs on its own origin (p-<id>.agent.localhost), which
// only a browser on this computer reaches. Other devices and tunnels open a
// file preview at /__files/<grant>/<path> instead, sandboxed into an opaque
// origin of its own (Preview.serveFile).
//
// Every request there is signed in by the auth system, with a file grant: a
// sandboxed page's module scripts and fetches never carry the session cookie
// or the password, so the grant rides in the URL. It is issued only to a
// signed-in page, and only works for as long as that sign-in does:
//   - issued to this computer while it skips sign-in: only on this computer;
//   - under pairing: while the pairing session that asked is valid;
//   - under the password: until the sign-in rules change (mode, credentials,
//     sign-in on this computer), which bumps signinGen;
//
// and never beyond the preview, 12 hours, or a restart.
const filesPrefix = "/__files/"

const fileGrantLife = 12 * time.Hour

type fileGrant struct {
	preview string
	expires time.Time
	gen     uint64
	local   bool   // issued to this computer while it skipped sign-in
	session string // the pairing session it was issued under
}

// previewResponse answers a preview the page may open: the one-time grant
// for the preview origin and, for files, a URL with a file grant.
func (s *Server) previewResponse(w http.ResponseWriter, r *http.Request, p *Preview) {
	now := time.Now()
	access := s.hostAccess(r)
	key := token()
	out := *p
	s.mu.Lock()
	for k, g := range s.grants {
		if now.After(g.expires) {
			delete(s.grants, k)
		}
	}
	s.grants[key] = grant{p.ID, now.Add(time.Minute)}
	if p.target == nil {
		for k, g := range s.fileGrants {
			if now.After(g.expires) {
				delete(s.fileGrants, k)
			}
		}
		g := fileGrant{preview: p.ID, expires: now.Add(fileGrantLife), gen: s.signinGen, local: access == hostLocal}
		if !g.local && s.remote == remotePairing {
			if c, err := r.Cookie("workbench_session"); err == nil {
				g.session = c.Value
			}
		}
		tok := token()[:32]
		s.fileGrants[tok] = g
		out.FileURL = (&url.URL{Path: filesPrefix + tok + p.Entry}).EscapedPath()
	}
	s.mu.Unlock()
	writeJSON(w, map[string]any{"preview": &out, "grant": key})
}

// fileGrantFor checks the grant in a /__files/ URL against the sign-in it
// came from.
func (s *Server) fileGrantFor(tok string, access hostAccess) *Preview {
	if len(tok) != 32 {
		return nil
	}
	now := time.Now()
	s.mu.Lock()
	g, ok := s.fileGrants[tok]
	valid := ok && now.Before(g.expires) && g.gen == s.signinGen
	switch {
	case !valid:
	case g.local:
		// Not a sign-in anywhere else.
		valid = access == hostLocal
	case access == hostLocal:
		// This computer is signed in by being this computer.
	case s.remote == remoteOff:
		valid = false
	case g.session != "":
		expires, live := s.auth[g.session]
		valid = live && now.Before(expires)
	case s.remote == remotePairing:
		// Issued without a pairing session (under the password, or here).
		valid = false
	}
	s.mu.Unlock()
	if !valid {
		return nil
	}
	return s.Manager.FindPreview(g.preview)
}

// serveFiles serves a file preview on the workbench's own address.
func (s *Server) serveFiles(w http.ResponseWriter, r *http.Request, access hostAccess) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tok, path, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, filesPrefix), "/")
	p := s.fileGrantFor(tok, access)
	if p == nil || p.target != nil {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "this preview link has ended: open the file again", http.StatusForbidden)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex")
	// The sandboxed page has an opaque origin: its module scripts and
	// fetches of its own files are cross-origin. The grant is the sign-in.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	p.serveFile(w, r, path, true)
}
