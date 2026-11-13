package workbench

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Images and files added to the composer (images pasted, dropped or picked;
// other files dropped or picked). The page uploads each one as it is added
// and sends only their names.
//
// Images go to the agents inline with the message (Claude: base64 blocks,
// Codex: data URLs), so their own records hold them. The workbench keeps its
// copy, named by content, for the page to show: a large image in a native
// record is found here again by its hash (boundJSON), instead of going to the
// page as megabytes of text.
//
// Other files are kept under their own name (files/<sha256>/<name>), and the
// message tells the agent where, a line for each (fileLines): the agents
// read them with their own tools. They are never served back to the page.
//
// Kept for the event retention after last use, and 1 GB at most.

const (
	// Claude takes at most 5 MB of base64 per image.
	uploadMax = 5 * 1024 * 1024 * 3 / 4
	// Per message.
	uploadsPerMessage   = 10
	uploadsMessageBytes = 15 << 20
	uploadsKeepBytes    = 1 << 30
	// A file of any kind.
	uploadFileMax = 100 << 20
	// Last use is written at most this often.
	uploadTouch = time.Hour
)

var (
	uploadName          = regexp.MustCompile(`^[0-9a-f]{64}\.(png|jpg|webp)$`)
	uploadFileID        = regexp.MustCompile(`^([0-9a-f]{64})/([^/\\]{1,120})$`)
	unsafeNameChars     = regexp.MustCompile(`[\x00-\x1f\x7f/\\:*?"<>|]`)
	errUnsupportedImage = errors.New("only PNG, JPEG and WebP images can be sent")
	uploadExt           = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}
	uploadMime          = map[string]string{"png": "image/png", "jpg": "image/jpeg", "webp": "image/webp"}
)

type Uploads struct{ dir string }

// Upload is a kept image or file as the page knows it.
type Upload struct {
	ID string `json:"id"`
	// Images only: where the page shows it from.
	URL   string `json:"url,omitempty"`
	Mime  string `json:"mime,omitempty"`
	Name  string `json:"name,omitempty"`
	Bytes int64  `json:"bytes"`
}

func uploadURL(id string) string { return "/api/uploads/" + id }

// uploadID names an image by its content and type.
func uploadID(data []byte, mime string) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) + "." + uploadExt[mime]
}

// Save keeps an image; the type is read from the bytes, never taken from
// the request.
func (u *Uploads) Save(data []byte) (Upload, error) {
	mime := http.DetectContentType(data)
	if _, ok := uploadExt[mime]; !ok {
		return Upload{}, errUnsupportedImage
	}
	if len(data) > uploadMax {
		return Upload{}, fmt.Errorf("an image can be at most %.2f MB", float64(uploadMax)/(1<<20))
	}
	id := uploadID(data, mime)
	up := Upload{ID: id, URL: uploadURL(id), Mime: mime, Bytes: int64(len(data))}
	path := filepath.Join(u.dir, id)
	if _, err := os.Stat(path); err == nil {
		u.touch(path)
		return up, nil
	}
	if err := os.MkdirAll(u.dir, 0o700); err != nil {
		return Upload{}, err
	}
	tmp, err := os.CreateTemp(u.dir, ".upload-*")
	if err != nil {
		return Upload{}, err
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o600)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return Upload{}, err
	}
	return up, nil
}

// Path is where a kept image is, for a well-formed name that exists.
func (u *Uploads) Path(id string) (string, bool) {
	if !uploadName.MatchString(id) {
		return "", false
	}
	path := filepath.Join(u.dir, id)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return path, true
}

// pasted is an image for a message, as the agents take it.
type pasted struct {
	ID, Mime, Base64 string
}

// Read loads a message's images, checking the per-message limits.
func (u *Uploads) Read(ids []string) ([]pasted, error) {
	if len(ids) > uploadsPerMessage {
		return nil, fmt.Errorf("a message can have at most %d images", uploadsPerMessage)
	}
	out := make([]pasted, 0, len(ids))
	total := 0
	for _, id := range ids {
		path, ok := u.Path(id)
		if !ok {
			return nil, errors.New("an image is no longer available; add it again")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if total += len(data); total > uploadsMessageBytes {
			return nil, fmt.Errorf("the images of a message can be at most %d MB together", uploadsMessageBytes>>20)
		}
		u.touch(path)
		out = append(out, pasted{ID: id, Mime: uploadMime[strings.TrimPrefix(filepath.Ext(id), ".")], Base64: base64.StdEncoding.EncodeToString(data)})
	}
	return out, nil
}

// touch records a use, at most hourly.
func (u *Uploads) touch(path string) {
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > uploadTouch {
		now := time.Now()
		_ = os.Chtimes(path, now, now)
	}
}

// Tidy drops images and files unused for keep, then the oldest past the
// size limit, and temporary files left by an interrupted upload.
func (u *Uploads) Tidy(keep time.Duration, limit int64) (removed int) {
	type kept struct {
		path string
		used time.Time
		size int64
	}
	var all []kept
	var total int64
	now := time.Now()
	consider := func(path string, info os.FileInfo) {
		if now.Sub(info.ModTime()) > keep {
			if os.Remove(path) == nil {
				removed++
			}
			return
		}
		all = append(all, kept{path, info.ModTime(), info.Size()})
		total += info.Size()
	}
	entries, err := os.ReadDir(u.dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(u.dir, e.Name())
		switch {
		case strings.HasPrefix(e.Name(), ".upload-"):
			if now.Sub(info.ModTime()) > time.Hour && os.Remove(path) == nil {
				removed++
			}
		case uploadName.MatchString(e.Name()):
			consider(path, info)
		}
	}
	files := filepath.Join(u.dir, "files")
	dirs, _ := os.ReadDir(files)
	for _, d := range dirs {
		dir := filepath.Join(files, d.Name())
		names, _ := os.ReadDir(dir)
		for _, n := range names {
			if info, err := n.Info(); err == nil && info.Mode().IsRegular() && uploadFileID.MatchString(d.Name()+"/"+n.Name()) {
				consider(filepath.Join(dir, n.Name()), info)
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].used.Before(all[j].used) })
	for _, k := range all {
		if total <= limit {
			break
		}
		if os.Remove(k.path) == nil {
			removed++
			total -= k.size
		}
	}
	// What emptied.
	for _, d := range dirs {
		_ = os.Remove(filepath.Join(files, d.Name()))
	}
	return removed
}

// fileName is a name a kept file can have: the last part of what the page
// gave, with characters no file system takes replaced.
func fileName(name string) string {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	name = unsafeNameChars.ReplaceAllString(name, "_")
	if r := []rune(name); len(r) > 100 {
		ext := filepath.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
		name = string([]rune(strings.TrimSuffix(name, ext))[:100-len([]rune(ext))]) + ext
	}
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	return name
}

// SaveFile keeps a file of any kind under its name; it is written as it
// arrives, never held whole.
func (u *Uploads) SaveFile(body io.Reader, name string) (Upload, error) {
	name = fileName(name)
	if err := os.MkdirAll(u.dir, 0o700); err != nil {
		return Upload{}, err
	}
	tmp, err := os.CreateTemp(u.dir, ".upload-*")
	if err != nil {
		return Upload{}, err
	}
	defer os.Remove(tmp.Name())
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, sum), body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Upload{}, err
	}
	id := hex.EncodeToString(sum.Sum(nil)) + "/" + name
	path := filepath.Join(u.dir, "files", filepath.FromSlash(id))
	if _, err := os.Stat(path); err == nil {
		u.touch(path)
		return Upload{ID: id, Name: name, Bytes: n}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Upload{}, err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return Upload{}, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return Upload{}, err
	}
	return Upload{ID: id, Name: name, Bytes: n}, nil
}

// attached is a kept file a message names.
type attached struct {
	Name, Path string
	Bytes      int64
}

// Files finds a message's files.
func (u *Uploads) Files(ids []string) ([]attached, error) {
	if len(ids) > uploadsPerMessage {
		return nil, fmt.Errorf("a message can have at most %d files", uploadsPerMessage)
	}
	out := make([]attached, 0, len(ids))
	for _, id := range ids {
		m := uploadFileID.FindStringSubmatch(id)
		if m == nil || fileName(m[2]) != m[2] {
			return nil, errors.New("a file is no longer available; add it again")
		}
		path := filepath.Join(u.dir, "files", m[1], m[2])
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("a file is no longer available; add it again")
		}
		u.touch(path)
		out = append(out, attached{Name: m[2], Path: path, Bytes: info.Size()})
	}
	return out, nil
}

// fileLines tells the agent where the message's files are, a line for each
// after the text: "[File (12 KB): /path/to/name]". The page shows these
// lines as the files (web/src/transcript/files.ts).
func fileLines(text string, files []attached) string {
	if len(files) == 0 {
		return text
	}
	lines := make([]string, len(files))
	for i, f := range files {
		lines[i] = fmt.Sprintf("[File (%s): %s]", sizeText(f.Bytes), f.Path)
	}
	if strings.TrimSpace(text) == "" {
		return strings.Join(lines, "\n")
	}
	return strings.TrimRight(text, "\n") + "\n\n" + strings.Join(lines, "\n")
}

func sizeText(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%d KB", (n+512)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
}

// serveUpload answers GET /api/uploads/<id>: inert (sandboxed, never
// sniffed), and kept by the browser, as the name is the content.
func (s *Server) serveUpload(w http.ResponseWriter, r *http.Request, id string) {
	path, ok := s.Manager.uploads.Path(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.Manager.uploads.touch(path)
	w.Header().Set("Content-Type", uploadMime[strings.TrimPrefix(filepath.Ext(id), ".")])
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
