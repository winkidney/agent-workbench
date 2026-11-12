package workbench

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Native records as they are on disk now. Codex compresses a record idle
// for a week to <path>.zst (one zstd frame, the same JSON lines) while its
// database keeps the .jsonl path, and restores the plain file when the
// thread is resumed. A Codex home copied or moved from elsewhere keeps
// paths under the old home.

// recordFile is a native record to read: plain JSON lines, or compressed.
type recordFile struct {
	Path string
	Zstd bool
}

// codexRecord finds a Codex record from the path its database recorded: the
// file itself, the same file compressed, or either under home when the
// recorded path belongs to another home.
func codexRecord(home, recorded string) (recordFile, bool) {
	if recorded == "" {
		return recordFile{}, false
	}
	if !filepath.IsAbs(recorded) {
		recorded = filepath.Join(home, recorded)
	}
	candidates := []string{recorded}
	if rel, ok := homeRelative(recorded); ok {
		if p := filepath.Join(home, rel); p != recorded {
			candidates = append(candidates, p)
		}
	}
	for _, p := range candidates {
		if regularFile(p) {
			return recordFile{Path: p}, true
		}
		if regularFile(p + ".zst") {
			return recordFile{Path: p + ".zst", Zstd: true}, true
		}
	}
	return recordFile{}, false
}

// originReader reads where Codex threads were created. It keeps one
// decoder: setting one up costs several times reading a first line.
type originReader struct{ dec *zstd.Decoder }

// createdIn is the directory a Codex thread was created in: the cwd of its
// record's first line, session_meta. "" when it cannot be read yet.
func (o *originReader) createdIn(f recordFile) string {
	file, err := os.Open(f.Path)
	if err != nil {
		return ""
	}
	defer file.Close()
	var r io.Reader = file
	if f.Zstd {
		if o.dec == nil {
			// One goroutine: nothing reads a file after it is closed.
			if o.dec, err = zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxWindow(recordMaxWindow)); err != nil {
				return ""
			}
		}
		if o.dec.Reset(file) != nil {
			return ""
		}
		r = o.dec
	}
	lines := newLineReader(r)
	if !lines.Scan() {
		return ""
	}
	var first struct {
		Type    string `json:"type"`
		Payload struct {
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}
	if json.Unmarshal(lines.Bytes(), &first) != nil || first.Type != "session_meta" {
		return ""
	}
	return first.Payload.Cwd
}

func (o *originReader) Close() {
	if o.dec != nil {
		o.dec.Close()
	}
}

// homeRelative is a record's path inside a Codex home: from its sessions/
// or archived_sessions/ directory on.
func homeRelative(p string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(p), "/")
	for i := len(parts) - 2; i >= 0; i-- {
		if parts[i] == "sessions" || parts[i] == "archived_sessions" {
			return filepath.FromSlash(strings.Join(parts[i:], "/")), true
		}
	}
	return "", false
}

func regularFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// Decoding keeps to one goroutine and a bounded window, so a record of
// gigabytes is read with a few megabytes of memory.
const recordMaxWindow = 128 << 20

// open returns the record's JSON lines, decompressing as they are read.
func (r recordFile) open() (io.ReadCloser, error) {
	f, err := os.Open(r.Path)
	if err != nil {
		return nil, err
	}
	if !r.Zstd {
		return f, nil
	}
	d, err := zstd.NewReader(f, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxWindow(recordMaxWindow))
	if err != nil {
		f.Close()
		return nil, err
	}
	return &zstdRecord{d: d, f: f}, nil
}

type zstdRecord struct {
	d *zstd.Decoder
	f *os.File
}

func (z *zstdRecord) Read(p []byte) (int, error) { return z.d.Read(p) }
func (z *zstdRecord) Close() error {
	z.d.Close()
	return z.f.Close()
}

// zstdSize is the decompressed size a compressed record declares, when it
// does.
func zstdSize(path string) (int64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	head := make([]byte, zstd.HeaderMaxSize)
	n, _ := io.ReadFull(f, head)
	var h zstd.Header
	if h.Decode(head[:n]) != nil || !h.HasFCS {
		return 0, false
	}
	return int64(h.FrameContentSize), true
}

// Lines longer than a read buffer are kept only when their start passes the
// filter, and only up to maxRecordLine; longer ones are skipped unread.
const maxRecordLine = 8 << 20

// eachRecordLine calls fn with each line whose first megabyte passes keep,
// and returns the bytes of the complete lines consumed. A last line without
// a newline counts only when final: a compressed record is complete, a plain
// one may still be being written. fn must not keep the line.
func eachRecordLine(r io.Reader, final bool, keep func([]byte) bool, fn func([]byte)) (int64, error) {
	reader := bufio.NewReaderSize(r, 1<<20)
	var read, pending int64
	var long []byte
	inLong, keepLong := false, false
	for {
		chunk, err := reader.ReadSlice('\n')
		full := errors.Is(err, bufio.ErrBufferFull)
		eof := errors.Is(err, io.EOF)
		if err != nil && !full && !eof {
			return read, err
		}
		if eof && (!final || (len(chunk) == 0 && !inLong)) {
			return read, nil
		}
		pending += int64(len(chunk))
		if full {
			// A line longer than the buffer: its start decides it.
			if !inLong {
				inLong, keepLong = true, keep(chunk)
				long = long[:0]
			}
			if keepLong {
				if len(long)+len(chunk) > maxRecordLine {
					keepLong, long = false, long[:0]
				} else {
					long = append(long, chunk...)
				}
			}
			continue
		}
		if inLong {
			if keepLong && len(long)+len(chunk) <= maxRecordLine {
				fn(append(long, chunk...))
			}
			inLong, keepLong, long = false, false, long[:0]
		} else if keep(chunk) {
			fn(chunk)
		}
		read += pending
		pending = 0
		if eof {
			return read, nil
		}
	}
}

// containsAny reports whether line holds one of the markers.
func containsAny(line []byte, markers ...[]byte) bool {
	for _, m := range markers {
		if bytes.Contains(line, m) {
			return true
		}
	}
	return false
}

// recordRemains is what Codex's index keeps of a thread whose record is
// gone.
type recordRemains struct {
	FirstMessage string `json:"firstMessage,omitempty"`
	TokensUsed   int64  `json:"tokensUsed,omitempty"`
	Created      string `json:"created,omitempty"`
}

// codexRemains reads a thread's remains from Codex's index. Columns that
// this Codex version lacks are left out.
func (c *NativeCatalog) codexRemains(nativeID string) (recordRemains, bool) {
	db, err := c.codexState()
	if err != nil {
		return recordRemains{}, false
	}
	defer db.Close()
	var out recordRemains
	var created sql.NullInt64
	if db.QueryRow(`SELECT created_at FROM threads WHERE id=?`, nativeID).Scan(&created) != nil {
		return recordRemains{}, false
	}
	if created.Valid && created.Int64 > 0 {
		out.Created = time.Unix(created.Int64, 0).UTC().Format(time.RFC3339)
	}
	var first sql.NullString
	if db.QueryRow(`SELECT first_user_message FROM threads WHERE id=?`, nativeID).Scan(&first) == nil {
		out.FirstMessage = clip(first.String, 4000)
	}
	var tokens sql.NullInt64
	if db.QueryRow(`SELECT tokens_used FROM threads WHERE id=?`, nativeID).Scan(&tokens) == nil {
		out.TokensUsed = tokens.Int64
	}
	return out, true
}
