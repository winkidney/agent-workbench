package workbench

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// What the workbench keeps is bounded: by what is running, by a size, or
// by the event retention (7 days). Nothing grows with how long it runs or
// how many sessions it has shown.

// A session's runtime outlives its engine only briefly: one asleep, with
// nothing waiting on it, is dropped after this and made again when the
// session is next opened (what it needs is in the store).
const runtimeIdle = 15 * time.Minute

// Capability caches are fresh for a minute; older ones are dropped.
const metadataKeep = 2 * time.Minute

// evictIdle drops runtimes nothing needs, and stale cache entries (m.mu
// not held).
func (m *Manager) evictIdle() {
	now := time.Now()
	m.mu.Lock()
	for key, r := range m.sessions {
		if r.proc != nil || len(r.pending) > 0 || len(r.queue) > 0 || len(r.previews) > 0 || len(r.steering) > 0 {
			continue
		}
		if r.session.Attention != "" || r.asking || r.planReady || r.bufTimer != nil {
			continue
		}
		switch r.session.Status {
		case "starting", "running", "waiting", "stopping":
			continue
		}
		if now.Sub(r.last) < runtimeIdle {
			continue
		}
		if r.goalTimer != nil {
			r.goalTimer.Stop()
		}
		if r.terminalPoll != nil {
			r.terminalPoll.Stop()
		}
		delete(m.sessions, key)
	}
	m.mu.Unlock()

	m.metaMu.Lock()
	for k, v := range m.metadata {
		if now.Sub(v.at) > metadataKeep {
			delete(m.metadata, k)
		}
	}
	m.metaMu.Unlock()

	m.extMu.Lock()
	for id, a := range m.external {
		if now.Sub(a.used) > runtimeIdle {
			delete(m.external, id)
		}
	}
	m.extMu.Unlock()
}

// forgetHeavy drops what a runtime keeps only for a running engine (m.mu
// held): the last Codex turn request (with the whole prompt), Claude task
// output names, speed samples and the CLI's capabilities.
func forgetHeavy(r *runtime) {
	r.turnRequest = nil
	r.taskOutputs = nil
	r.samples = nil
	r.caps = nil
	r.mcp = nil
}

// Caches the store keeps are rows with a time of last use, dropped when
// unused for the event retention.

func (s *Store) cacheGet(key string, v any) bool {
	var raw, used string
	if s.db.QueryRow(`SELECT value,used FROM cache WHERE key=?`, key).Scan(&raw, &used) != nil {
		return false
	}
	// Using it keeps it; recorded at most hourly, as reads are frequent.
	if t, err := time.Parse(time.RFC3339Nano, used); err != nil || time.Since(t) > time.Hour {
		_, _ = s.db.Exec(`UPDATE cache SET used=? WHERE key=?`, nowStamp(), key)
	}
	return json.Unmarshal([]byte(raw), v) == nil
}

func (s *Store) cacheSet(key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO cache(key,value,used) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value,used=excluded.used`, key, string(b), nowStamp())
	return err
}

func (s *Store) deleteSetting(key string) error {
	_, err := s.db.Exec(`DELETE FROM settings WHERE key=?`, key)
	return err
}

// pruneKept drops cache rows unused, and request records older, than
// before.
func (s *Store) pruneKept(before time.Time) (int64, error) {
	cut := before.UTC().Format(time.RFC3339Nano)
	var total int64
	for _, q := range []string{
		`DELETE FROM cache WHERE used < ?`,
		// Creation requests are retried within minutes, not days.
		`DELETE FROM native_creations WHERE coalesce(created,'') < ?`,
	} {
		res, err := s.db.Exec(q, cut)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}

// migrateRetention moves what older builds kept forever to where it
// expires (the caches are rebuilt on use).
func migrateRetention(db *sql.DB) error {
	var created int
	_ = db.QueryRow(`SELECT count(*) FROM pragma_table_info('native_creations') WHERE name='created'`).Scan(&created)
	if created == 0 {
		if _, err := db.Exec(`ALTER TABLE native_creations ADD COLUMN created TEXT`); err != nil {
			return err
		}
	}
	_, err := db.Exec(`DELETE FROM settings WHERE key LIKE 'history_usage:%' OR key LIKE 'claude-init:%' OR (key LIKE 'goal:%' AND value='null')`)
	return err
}

// Tidy runs the store's hourly upkeep and drops what the workbench kept for
// sessions gone for the retention.
func (m *Manager) Tidy() {
	m.store.Tidy()
	if n, err := m.store.pruneKept(time.Now().Add(-eventRetention)); err != nil {
		fmt.Fprintln(os.Stderr, "prune caches:", err)
	} else if n > 0 {
		fmt.Fprintln(os.Stderr, "pruned", n, "cache rows")
	}
	if err := m.Reconcile(); err != nil {
		fmt.Fprintln(os.Stderr, "reconcile sessions:", err)
	}
}

// Reconcile records which sessions exist (listed, or loaded here) and drops
// the rows of sessions not seen for the retention: deleted in their CLI,
// or renamed. Skipped when the list cannot be read or is empty, so a
// missing home never wipes anything.
func (m *Manager) Reconcile() error {
	list, err := m.catalog.List()
	if err != nil || len(list) == 0 {
		return err
	}
	m.mu.Lock()
	ids := make([]string, 0, len(list)+len(m.sessions))
	for key, r := range m.sessions {
		ids = append(ids, key, r.session.ID)
	}
	m.mu.Unlock()
	for _, s := range list {
		ids = append(ids, s.ID)
	}
	now := nowStamp()
	tx, err := m.store.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO session_seen(id,seen) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET seen=excluded.seen`)
	if err != nil {
		tx.Rollback()
		return err
	}
	for _, id := range ids {
		if _, err := stmt.Exec(id, now); err != nil {
			stmt.Close()
			tx.Rollback()
			return err
		}
	}
	stmt.Close()
	cut := time.Now().Add(-eventRetention).UTC().Format(time.RFC3339Nano)
	gone := `NOT IN (SELECT id FROM session_seen WHERE seen >= ?1)`
	for _, q := range []string{
		`DELETE FROM settings WHERE (key LIKE 'options:%' OR key LIKE 'telemetry:%' OR key LIKE 'goal:%') AND substr(key, instr(key, ':') + 1) ` + gone,
		`DELETE FROM cache WHERE key LIKE 'history_usage:%' AND substr(key, 15) ` + gone,
		`DELETE FROM sessions WHERE id ` + gone,
		`DELETE FROM session_activity WHERE id ` + gone,
		`DELETE FROM preview_tabs WHERE session ` + gone,
		`DELETE FROM file_previews WHERE session ` + gone,
		`DELETE FROM session_seen WHERE seen < ?1`,
	} {
		if _, err := tx.Exec(q, cut); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Display names and related projects of sessions gone as long (not of
	// those in another home, while it is not the one in use).
	seen := map[string]bool{}
	rows, err := m.store.db.Query(`SELECT id FROM session_seen`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			seen[id] = true
		}
	}
	rows.Close()
	m.prefsMu.Lock()
	p := m.Prefs()
	changed := false
	for id := range p.Aliases {
		if !seen[id] {
			delete(p.Aliases, id)
			changed = true
		}
	}
	if changed {
		_ = m.savePrefs(p)
	}
	m.prefsMu.Unlock()
	m.activity.keep(seen)
	return nil
}
