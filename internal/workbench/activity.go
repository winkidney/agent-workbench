package workbench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ActivityIndex records which subprojects of a session's launch directory its
// tool calls touched. It only feeds "related project" chips: grouping is
// always the launch directory. Native files are read incrementally from the
// last offset, so append-only transcripts are scanned once.
type ActivityIndex struct {
	store   *Store
	mu      sync.Mutex
	related map[string][]string
}

type activityRow struct {
	path   string
	offset int64
	counts map[string]int
}

func NewActivityIndex(store *Store) *ActivityIndex {
	a := &ActivityIndex{store: store, related: map[string][]string{}}
	rows, err := store.db.Query(`SELECT id,counts FROM session_activity`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, raw string
			var counts map[string]int
			if rows.Scan(&id, &raw) == nil && json.Unmarshal([]byte(raw), &counts) == nil {
				a.related[id] = relatedProjects(counts)
			}
		}
	}
	return a
}

func (a *ActivityIndex) Related(id string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.related[id]
}

// Records that describe tool calls rather than their output.
var toolMarkers = [][]byte{
	[]byte(`"type":"function_call"`),
	[]byte(`"type":"custom_tool_call"`),
	[]byte(`"type":"local_shell_call"`),
	[]byte(`"type":"exec_command_begin"`),
	[]byte(`"type":"patch_apply_begin"`),
	[]byte(`"type":"tool_use"`),
}

func toolRecord(line []byte) bool {
	for _, m := range toolMarkers {
		if bytes.Contains(line, m) {
			// Claude puts tool_use inside assistant records; skip user tool results.
			if bytes.Equal(m, toolMarkers[5]) && !bytes.Contains(line, []byte(`"type":"assistant"`)) {
				continue
			}
			return true
		}
	}
	return false
}

// subprojectMatcher finds references to the named first-level subdirectories
// of root, as absolute paths or relative tokens in commands and file paths.
type subprojectMatcher struct {
	abs, rel *regexp.Regexp
	names    map[string]bool
}

func newSubprojectMatcher(root string, names []string) *subprojectMatcher {
	if len(names) == 0 {
		return nil
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	quoted := make([]string, len(names))
	set := map[string]bool{}
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
		set[n] = true
	}
	return &subprojectMatcher{
		abs:   regexp.MustCompile(regexp.QuoteMeta(root) + `/([A-Za-z0-9._-]+)`),
		rel:   regexp.MustCompile(`(?:^|[\s'"=:(\[,])(?:\./)?(` + strings.Join(quoted, "|") + `)(?:/|[\s'"\]),\\]|$)`),
		names: set,
	}
}

func (m *subprojectMatcher) count(line []byte, counts map[string]int) {
	hits := map[string]bool{}
	for _, g := range m.abs.FindAllSubmatch(line, -1) {
		if n := string(g[1]); m.names[n] {
			hits[n] = true
		}
	}
	for _, g := range m.rel.FindAllSubmatch(line, -1) {
		hits[string(g[1])] = true
	}
	for n := range hits {
		counts[n]++
	}
}

// relatedProjects keeps subprojects with enough references to matter:
// at least 5 and 15% of the most referenced one, at most four.
func relatedProjects(counts map[string]int) []string {
	top := 0
	for _, n := range counts {
		if n > top {
			top = n
		}
	}
	limit := 5
	if t := top * 15 / 100; t > limit {
		limit = t
	}
	names := []string{}
	for name, n := range counts {
		if n >= limit {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > 4 {
		names = names[:4]
	}
	return names
}

// subprojects lists the first-level directories under root that are projects:
// git repositories or the launch directory of another session.
func subprojects(root string, namespaces map[string]bool) []string {
	set := map[string]bool{}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			if _, err := os.Stat(filepath.Join(root, e.Name(), ".git")); err == nil {
				set[e.Name()] = true
			}
		}
	}
	for ns := range namespaces {
		if rel, err := filepath.Rel(root, ns); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			set[strings.Split(rel, string(filepath.Separator))[0]] = true
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	return out
}

// scanFile reads new bytes of one transcript from row.offset, stopping at the
// last complete line within budget. It returns the bytes consumed.
func scanFile(path string, row *activityRow, m *subprojectMatcher, budget int64) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0
	}
	if info.Size() < row.offset {
		// Rewritten transcript: start over.
		row.offset = 0
		row.counts = map[string]int{}
	}
	if info.Size() == row.offset || budget <= 0 {
		return 0
	}
	if _, err := f.Seek(row.offset, io.SeekStart); err != nil {
		return 0
	}
	reader := bufio.NewReaderSize(io.LimitReader(f, budget), 1<<20)
	var read int64
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			// A partial last line is left for the next pass.
			break
		}
		read += int64(len(line))
		if len(line) < 8<<20 && toolRecord(line) {
			m.count(line, row.counts)
		}
	}
	row.offset += read
	return read
}

// scanCompressed indexes a compressed record. It does not change, so it is
// read once, from its start: bytes already counted (perhaps while it was
// still plain) are decoded and skipped. Unless first in a pass, a record
// that does not fit the budget waits for a pass that has room.
func scanCompressed(f recordFile, row *activityRow, m *subprojectMatcher, budget int64, first bool) int64 {
	size, known := zstdSize(f.Path)
	if known && row.offset >= size {
		return 0
	}
	if !known && row.path == f.Path && row.offset > 0 {
		return 0
	}
	if !first && known && size-row.offset > budget {
		return 0
	}
	r, err := f.open()
	if err != nil {
		return 0
	}
	defer r.Close()
	skipped := int64(0)
	if row.offset > 0 {
		if skipped, err = io.CopyN(io.Discard, r, row.offset); err != nil {
			return 0
		}
	}
	read, err := eachRecordLine(r, true, toolRecord, func(line []byte) {
		if len(line) < 8<<20 {
			m.count(line, row.counts)
		}
	})
	if err != nil {
		return 0
	}
	row.path = f.Path
	row.offset += read
	return skipped + read
}

// Scan indexes project sessions within a byte budget per pass.
func (a *ActivityIndex) Scan(sessions []Session, files map[string]recordFile, budget int64) {
	namespaces := map[string]bool{}
	for _, s := range sessions {
		if s.Origin == "project" && s.ParentID == "" {
			namespaces[s.Workspace] = true
		}
	}
	matchers := map[string]*subprojectMatcher{}
	first := true
	for _, s := range sessions {
		if budget <= 0 {
			return
		}
		file, ok := files[s.ID]
		if !ok || s.ParentID != "" || s.Origin != "project" || s.Workspace == "" {
			continue
		}
		m, ok := matchers[s.Workspace]
		if !ok {
			m = newSubprojectMatcher(s.Workspace, subprojects(s.Workspace, namespaces))
			matchers[s.Workspace] = m
		}
		if m == nil {
			continue
		}
		row := a.load(s.ID, file.Path)
		var used int64
		if file.Zstd {
			used = scanCompressed(file, &row, m, budget, first)
		} else {
			used = scanFile(file.Path, &row, m, budget)
		}
		if used > 0 {
			first = false
			budget -= used
			a.save(s.ID, row)
		}
	}
}

func (a *ActivityIndex) load(id, path string) activityRow {
	row := activityRow{path: path, counts: map[string]int{}}
	var raw, saved string
	if a.store.db.QueryRow(`SELECT path,offset,counts FROM session_activity WHERE id=?`, id).Scan(&saved, &row.offset, &raw) == nil {
		// A record compressed since keeps its content: continue counting.
		if saved == path || saved+".zst" == path {
			row.path = saved
			_ = json.Unmarshal([]byte(raw), &row.counts)
		} else {
			row.offset = 0
		}
	}
	return row
}

func (a *ActivityIndex) save(id string, row activityRow) {
	raw, _ := json.Marshal(row.counts)
	_, _ = a.store.db.Exec(`INSERT INTO session_activity(id,path,offset,counts) VALUES(?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET path=excluded.path,offset=excluded.offset,counts=excluded.counts`, id, row.path, row.offset, string(raw))
	a.mu.Lock()
	a.related[id] = relatedProjects(row.counts)
	a.mu.Unlock()
}

// keep drops what is held for sessions not in seen (gone as long as the
// store keeps their counts).
func (a *ActivityIndex) keep(seen map[string]bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id := range a.related {
		if !seen[id] {
			delete(a.related, id)
		}
	}
}
