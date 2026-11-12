package workbench

import (
	"archive/zip"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Away from the workbench computer, "Show in Files" would open a window on
// a desktop nobody there sees: a path a session wrote downloads instead. A
// file is sent as it is (with ranges, so a download can resume); a folder
// as a zip made while it is sent, with nothing held in memory or on disk.

func attachment(name string) string {
	if v := mime.FormatMediaType("attachment", map[string]string{"filename": name}); v != "" {
		return v
	}
	return "attachment"
}

// download sends a file or folder a session wrote, from inside its
// directory (ResolvePath).
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("session")
	if err := s.Manager.Attach(id); err != nil {
		bad(w, err)
		return
	}
	full, rel, err := s.Manager.ResolvePath(id, q.Get("base"), q.Get("path"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		bad(w, err)
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	name := filepath.Base(full)
	if rel == "." || rel == "" {
		name = filepath.Base(filepath.Clean(full))
	}
	switch {
	case info.Mode().IsRegular():
		f, err := os.Open(full)
		if err != nil {
			http.Error(w, "not readable", http.StatusForbidden)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", attachment(name))
		http.ServeContent(w, r, name, info.ModTime(), f)
	case info.IsDir():
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", attachment(name+".zip"))
		// A HEAD (the page checking before it downloads) gets the headers.
		if r.Method == http.MethodHead {
			return
		}
		_ = zipFolder(w, full, name)
	default:
		http.Error(w, "not a file or folder", http.StatusBadRequest)
	}
}

// zipFolder writes dir as a zip, its entries under prefix/. Links are left
// out, so nothing outside dir is read.
func zipFolder(w io.Writer, dir, prefix string) error {
	z := zip.NewWriter(w)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable entry is left out, not the whole folder.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || !(d.IsDir() || d.Type().IsRegular()) {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		name := prefix
		if rel != "." {
			name += "/" + filepath.ToSlash(rel)
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return nil
		}
		h.Name = name
		if d.IsDir() {
			h.Name += "/"
			_, err = z.CreateHeader(h)
			return err
		}
		h.Method = zip.Deflate
		if compressed(path) {
			h.Method = zip.Store
		}
		out, err := z.CreateHeader(h)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		_, err = io.Copy(out, f)
		return err
	})
	if err != nil {
		return err
	}
	return z.Close()
}

// compressed are files deflating would not shrink.
func compressed(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".zip", ".gz", ".tgz", ".zst", ".xz", ".bz2", ".7z", ".rar", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".mp4", ".webm", ".mov", ".mkv", ".mp3", ".ogg", ".flac", ".m4a", ".pdf", ".woff", ".woff2":
		return true
	}
	return false
}
