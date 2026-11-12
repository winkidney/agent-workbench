package workbench

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Session is a native conversation. Cwd is the directory the session was
// launched from: it decides the project grouping and where resume runs; later
// working-directory changes inside the session are ignored.
type Session struct {
	// How to start this session's CLI, set as its engine starts.
	run       agentRun
	Workspace string `json:"workspace"`
	// Origin: project, automation (codex exec), temporary or chat (home / app chats).
	Origin string `json:"origin,omitempty"`
	// Related lists subprojects this session\'s tool calls touched (display only).
	Related  []string `json:"related,omitempty"`
	ParentID string   `json:"parentId,omitempty"`
	Source   string   `json:"source,omitempty"`
	ReadOnly bool     `json:"readOnly"`
	// Archived: archived in the native client (Codex); read-only until
	// unarchived.
	Archived bool   `json:"archived,omitempty"`
	ID       string `json:"id"`
	Agent    string `json:"agent"`
	Cwd      string `json:"cwd"`
	Title    string `json:"title"`
	NativeID string `json:"nativeId"`
	Status   string `json:"status"`
	// Attention: what the session waits for from the user (attention.go);
	// computed, not stored.
	Attention string `json:"attention,omitempty"`
	// Background: how many tasks or processes run outside its turns
	// (background.go); computed, not stored.
	Background int    `json:"background,omitempty"`
	Updated    string `json:"updated"`
	// ExplicitName: the user typed a name, so it is written as the native name.
	ExplicitName bool `json:"-"`
}
type Event struct {
	Seq     int64           `json:"seq"`
	Session string          `json:"session"`
	Kind    string          `json:"kind"`
	Data    json.RawMessage `json:"data"`
	Time    string          `json:"time"`
}
type Store struct {
	db  *sql.DB
	dir string
}

func token() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "workbench.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS native_creations(request_id TEXT PRIMARY KEY,payload_hash TEXT,response TEXT);
 CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY,agent TEXT,cwd TEXT,title TEXT,native_id TEXT,status TEXT,updated TEXT);
 CREATE TABLE IF NOT EXISTS events(seq INTEGER PRIMARY KEY AUTOINCREMENT,session TEXT,kind TEXT,data TEXT,time TEXT);
 CREATE INDEX IF NOT EXISTS events_session ON events(session,seq);
 CREATE TABLE IF NOT EXISTS session_activity(id TEXT PRIMARY KEY,path TEXT,offset INTEGER NOT NULL DEFAULT 0,counts TEXT NOT NULL DEFAULT '{}');
 UPDATE sessions SET status='sleeping' WHERE status NOT IN ('stopped','sleeping');`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, dir: dir}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Save(v Session) error {
	_, err := s.db.Exec(`INSERT INTO sessions VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET native_id=excluded.native_id,status=excluded.status,title=excluded.title,updated=excluded.updated`, v.ID, v.Agent, v.Cwd, v.Title, v.NativeID, v.Status, time.Now().UTC().Format(time.RFC3339))
	return err
}

// Forget removes everything the workbench kept about a session: its row,
// events, settings and activity counts.
func (s *Store) Forget(id string) error {
	for _, q := range []string{
		"DELETE FROM events WHERE session = ?1",
		"DELETE FROM sessions WHERE id = ?1",
		"DELETE FROM session_activity WHERE id = ?1",
		"DELETE FROM settings WHERE key = 'options:' || ?1 OR key = 'telemetry:' || ?1",
	} {
		if _, err := s.db.Exec(q, id); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) List() ([]Session, error) {
	rows, err := s.db.Query(`SELECT * FROM sessions ORDER BY updated DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var v Session
		if err := rows.Scan(&v.ID, &v.Agent, &v.Cwd, &v.Title, &v.NativeID, &v.Status, &v.Updated); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Append(id, kind string, data any) (Event, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return Event{}, err
	}
	// Native records are already bounded structurally (boundJSON); this is the last resort.
	if kind == "native" && len(b) > 1<<20 {
		b, _ = json.Marshal(map[string]any{"truncated": true, "originalBytes": len(b), "preview": safeText(string(b), 4096)})
	}
	e := Event{Session: id, Kind: kind, Data: b, Time: time.Now().UTC().Format(time.RFC3339Nano)}
	r, err := s.db.Exec(`INSERT INTO events(session,kind,data,time) VALUES(?,?,?,?)`, id, kind, string(b), e.Time)
	if err != nil {
		return e, err
	}
	e.Seq, err = r.LastInsertId()
	return e, err
}
func (s *Store) Events(id string, after int64, limit int) ([]Event, error) {
	rows, err := s.db.Query(`SELECT seq,session,kind,data,time FROM events WHERE (?='' OR session=?) AND seq>? ORDER BY seq LIMIT ?`, id, id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var data string
		if err := rows.Scan(&e.Seq, &e.Session, &e.Kind, &data, &e.Time); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(data)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Events are a delivery buffer (stream catch-up and turns not yet in the
// native record), never the conversation itself, so old ones are dropped.
const (
	eventRetention   = 7 * 24 * time.Hour
	eventsPerSession = 2000
)

// Prune deletes events older than before and all but the newest keep
// events of each session.
func (s *Store) Prune(before time.Time, keep int) (int64, error) {
	res, err := s.db.Exec("DELETE FROM events WHERE time < ?", before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	rows, err := s.db.Query("SELECT session FROM events GROUP BY session HAVING count(*) > ?", keep)
	if err != nil {
		return n, err
	}
	var over []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			over = append(over, id)
		}
	}
	rows.Close()
	for _, id := range over {
		res, err := s.db.Exec(`DELETE FROM events WHERE session = ? AND seq < (
			SELECT seq FROM events WHERE session = ? ORDER BY seq DESC LIMIT 1 OFFSET ?)`, id, id, keep-1)
		if err != nil {
			return n, err
		}
		d, _ := res.RowsAffected()
		n += d
	}
	return n, nil
}

// Compact folds the write-ahead log back and, when much of the file is
// free pages, rewrites it so the space returns to the disk.
func (s *Store) Compact() error {
	if _, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return err
	}
	var pages, free, size int64
	_ = s.db.QueryRow("PRAGMA page_count").Scan(&pages)
	_ = s.db.QueryRow("PRAGMA freelist_count").Scan(&free)
	_ = s.db.QueryRow("PRAGMA page_size").Scan(&size)
	if free*size < 8<<20 || free*4 < pages {
		return nil
	}
	if _, err := s.db.Exec("VACUUM"); err != nil {
		return err
	}
	_, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

// Size reports the bytes the store occupies on disk (database and log).
func (s *Store) Size() int64 {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if info, err := os.Stat(filepath.Join(s.dir, "workbench.db"+suffix)); err == nil {
			total += info.Size()
		}
	}
	return total
}

// Tidy prunes old events and compacts the file.
func (s *Store) Tidy() {
	if n, err := s.Prune(time.Now().Add(-eventRetention), eventsPerSession); err != nil {
		fmt.Fprintln(os.Stderr, "prune events:", err)
	} else if n > 0 {
		fmt.Fprintln(os.Stderr, "pruned", n, "old events")
	}
	if err := s.Compact(); err != nil {
		fmt.Fprintln(os.Stderr, "compact store:", err)
	}
}
