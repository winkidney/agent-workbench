package workbench

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// File previews and preview tabs live in the store, not in memory: a file
// preview is a row (session, file, its secret) read when its page is
// served, and both go when unused for the event retention (7 days).

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// filePreview is the row for a session's file, made on first use; using it
// again keeps it.
func (s *Store) filePreview(session, root, entry string) (id, secret string, err error) {
	err = s.db.QueryRow(`SELECT id,secret FROM file_previews WHERE session=? AND root=? AND entry=?`, session, root, entry).Scan(&id, &secret)
	if err == nil {
		_, err = s.db.Exec(`UPDATE file_previews SET used=? WHERE id=?`, nowStamp(), id)
		return id, secret, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	id, secret = token()[:24], token()
	_, err = s.db.Exec(`INSERT INTO file_previews(id,session,root,entry,secret,used) VALUES(?,?,?,?,?,?)`, id, session, root, entry, secret, nowStamp())
	return id, secret, err
}

// filePreviewByID reads a file preview's row.
func (s *Store) filePreviewByID(id string) (session, root, entry, secret string, ok bool) {
	err := s.db.QueryRow(`SELECT session,root,entry,secret FROM file_previews WHERE id=?`, id).Scan(&session, &root, &entry, &secret)
	return session, root, entry, secret, err == nil
}

// forgetFilePreviewsOf drops a session's file previews (it ended); its
// tabs stay, to open them again.
func (s *Store) forgetFilePreviewsOf(session string) error {
	_, err := s.db.Exec(`DELETE FROM file_previews WHERE session=?`, session)
	return err
}

func (s *Store) forgetFilePreview(id string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM file_previews WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// previewTabs reads a session's tabs; reading them counts as using them.
func (s *Store) previewTabs(session string) (PreviewTabs, bool) {
	var raw string
	if s.db.QueryRow(`SELECT value FROM preview_tabs WHERE session=?`, session).Scan(&raw) != nil {
		return PreviewTabs{}, false
	}
	_, _ = s.db.Exec(`UPDATE preview_tabs SET used=? WHERE session=?`, nowStamp(), session)
	var v PreviewTabs
	return v, json.Unmarshal([]byte(raw), &v) == nil
}

func (s *Store) setPreviewTabs(session string, v PreviewTabs) error {
	if len(v.Tabs) == 0 {
		_, err := s.db.Exec(`DELETE FROM preview_tabs WHERE session=?`, session)
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO preview_tabs(session,value,used) VALUES(?,?,?)
		ON CONFLICT(session) DO UPDATE SET value=excluded.value,used=excluded.used`, session, string(b), nowStamp())
	return err
}

// PrunePreviews drops file previews and tabs unused since before.
func (s *Store) PrunePreviews(before time.Time) (int64, error) {
	cut := before.UTC().Format(time.RFC3339Nano)
	var total int64
	for _, q := range []string{`DELETE FROM file_previews WHERE used < ?`, `DELETE FROM preview_tabs WHERE used < ?`} {
		res, err := s.db.Exec(q, cut)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}
