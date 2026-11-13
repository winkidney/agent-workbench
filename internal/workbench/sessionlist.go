package workbench

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/klauspost/compress/gzhttp"
)

// What pages poll, sent as little as will do.
//
// The session list (a thousand sessions and more, a megabyte) is polled
// every few seconds, and nearly all of it stays the same. A page names the
// list it last had (its token) and gets only the rows that changed since,
// and the ones gone. The last few lists are remembered as row hashes only;
// the same list has the same token, so pages share them. A page whose token
// is unknown (first load, a restart, or one too old) gets the whole list.

const listSnapshots = 8

type listSnapshot struct {
	token string
	rows  map[string]uint64
}

type sessionLists struct {
	mu sync.Mutex
	// Least recently used first.
	kept []listSnapshot
}

func hash64(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}

// sessionsDelta is a page's answer: the whole list (full, every row in
// changed), or what changed since. Count is how many sessions the list has,
// so a page whose copy drifted (rows it added from the event stream that
// the list never had) can tell and ask for the whole list.
type sessionsDelta struct {
	Token   string            `json:"token"`
	Count   int               `json:"count"`
	Full    bool              `json:"full,omitempty"`
	Changed []json.RawMessage `json:"changed,omitempty"`
	Removed []string          `json:"removed,omitempty"`
}

// delta answers a page that had the list named since ("" for none).
func (l *sessionLists) delta(list []Session, since string) (sessionsDelta, error) {
	raws := make([]json.RawMessage, len(list))
	rows := make(map[string]uint64, len(list))
	ids := make([]string, 0, len(list))
	for i, s := range list {
		b, err := json.Marshal(s)
		if err != nil {
			return sessionsDelta{}, err
		}
		raws[i] = b
		if _, seen := rows[s.ID]; !seen {
			ids = append(ids, s.ID)
		}
		rows[s.ID] = hash64(b)
	}
	sort.Strings(ids)
	h := fnv.New64a()
	var n [8]byte
	for _, id := range ids {
		_, _ = h.Write([]byte(id))
		binary.LittleEndian.PutUint64(n[:], rows[id])
		_, _ = h.Write(n[:])
	}
	token := strconv.FormatUint(h.Sum64(), 36)

	l.mu.Lock()
	var old map[string]uint64
	keep := l.kept[:0]
	for _, k := range l.kept {
		if k.token == since {
			old = k.rows
		}
		if k.token != token {
			keep = append(keep, k)
		}
	}
	keep = append(keep, listSnapshot{token, rows})
	if len(keep) > listSnapshots {
		keep = keep[len(keep)-listSnapshots:]
	}
	l.kept = keep
	l.mu.Unlock()

	if old == nil {
		return sessionsDelta{Token: token, Count: len(list), Full: true, Changed: raws}, nil
	}
	out := sessionsDelta{Token: token, Count: len(list)}
	for i, s := range list {
		if was, ok := old[s.ID]; !ok || was != rows[s.ID] {
			out.Changed = append(out.Changed, raws[i])
		}
	}
	for id := range old {
		if _, ok := rows[id]; !ok {
			out.Removed = append(out.Removed, id)
		}
	}
	sort.Strings(out.Removed)
	return out, nil
}

// writeJSONCached answers a poll for small JSON: unchanged since the page's
// copy (its ETag), only that. Browsers revalidate on their own.
func writeJSONCached(w http.ResponseWriter, r *http.Request, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b = append(b, '\n')
	tag := fmt.Sprintf(`"%x"`, hash64(b))
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", tag)
	if etagMatches(r.Header.Get("If-None-Match"), tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// etagMatches compares weakly: a proxy that compresses (Cloudflare) marks
// the tag weak on the way.
func etagMatches(header, tag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || strings.TrimPrefix(t, "W/") == tag {
			return true
		}
	}
	return false
}

// compressor gzips text the workbench sends (JSON, scripts, styles, pages)
// to browsers that take it; media, downloads and the event stream are left
// as they are.
func compressor() func(http.Handler) http.HandlerFunc {
	wrap, err := gzhttp.NewWrapper(
		gzhttp.MinSize(1024),
		gzhttp.ContentTypes([]string{
			"application/json",
			"text/javascript",
			"text/css",
			"text/html",
			"image/svg+xml",
		}),
	)
	if err != nil {
		// The options are fixed: not reached.
		return func(h http.Handler) http.HandlerFunc { return h.ServeHTTP }
	}
	return wrap
}
