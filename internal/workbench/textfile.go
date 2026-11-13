package workbench

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Text files edited in the preview panel: any UTF-8 text file in a session's
// directory, up to textEditMax, read and written as it is. A save names the
// file as it was read (its stamp), so a file changed since (by the agent,
// say) is not overwritten unless the page says so. Nothing is created, and a
// file is written in place: its permissions, owner and links stay.

const textEditMax = 1 << 20

// looksText: no NUL byte, and valid UTF-8. The head of a longer file (whole
// false) may end inside a character.
func looksText(b []byte, whole bool) bool {
	if bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	if utf8.Valid(b) {
		return true
	}
	if whole {
		return false
	}
	for cut := 1; cut <= 3 && cut < len(b); cut++ {
		if tail := b[len(b)-cut:]; utf8.RuneStart(tail[0]) && !utf8.FullRune(tail) {
			return utf8.Valid(b[:len(b)-cut])
		}
	}
	return false
}

// fileStamp tells one version of a file from another: its size and time.
func fileStamp(info os.FileInfo) string {
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

// editableText reports whether a file preview's file is text the page may
// edit: small enough, and text as far as its head shows (not a PDF).
func editableText(root, entry string) bool {
	r, err := os.OpenRoot(root)
	if err != nil {
		return false
	}
	defer r.Close()
	f, err := r.Open(filepath.FromSlash(strings.TrimPrefix(entry, "/")))
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > textEditMax {
		return false
	}
	head := make([]byte, 8<<10)
	n, _ := io.ReadFull(f, head)
	// A PDF can begin as text; it is a document, shown as one.
	return !bytes.HasPrefix(head[:n], []byte("%PDF-")) && looksText(head[:n], int64(n) == info.Size())
}

// textFile reads (GET) or writes (PUT) a text file in a session's directory,
// named relative to it.
func (s *Server) textFile(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("session")
	if err := s.Manager.Attach(id); err != nil {
		bad(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	full, err := confined(s.Manager.sessionDir(id), q.Get("path"))
	if errors.Is(err, fs.ErrNotExist) {
		http.Error(w, "This file is no longer there.", http.StatusNotFound)
		return
	}
	if err != nil {
		bad(w, err)
		return
	}
	if r.Method == http.MethodPut {
		writeText(w, r, full, q.Get("stamp"))
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, "This file can't be read.", http.StatusForbidden)
		return
	}
	defer f.Close()
	// The stamp is taken first: a change while reading shows as one on save.
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		bad(w, errors.New("not a file"))
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, textEditMax+1))
	switch {
	case err != nil:
		http.Error(w, "This file can't be read.", http.StatusForbidden)
	case len(data) > textEditMax:
		http.Error(w, "Too large to edit here (over 1 MB).", http.StatusRequestEntityTooLarge)
	case !looksText(data, true):
		http.Error(w, "Not a text file.", http.StatusUnsupportedMediaType)
	default:
		writeJSON(w, map[string]any{"text": string(data), "stamp": fileStamp(info)})
	}
}

// writeText saves the request's text over the file, if the file is still the
// version it was read as (stamp; none overwrites it).
func writeText(w http.ResponseWriter, r *http.Request, full, stamp string) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, textEditMax))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Too large to save here (over 1 MB).", http.StatusRequestEntityTooLarge)
			return
		}
		bad(w, err)
		return
	}
	if !looksText(data, true) {
		http.Error(w, "Not UTF-8 text.", http.StatusUnsupportedMediaType)
		return
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "This file is no longer there.", http.StatusNotFound)
		return
	}
	if stamp != "" && stamp != fileStamp(info) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "changed", "stamp": fileStamp(info)})
		return
	}
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_TRUNC, 0)
	if errors.Is(err, fs.ErrPermission) {
		http.Error(w, "This file is read-only.", http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "Couldn't save: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		info, err = os.Stat(full)
	}
	if err != nil {
		http.Error(w, "Couldn't save: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"stamp": fileStamp(info)})
}
