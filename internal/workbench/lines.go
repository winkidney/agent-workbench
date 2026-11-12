package workbench

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// lineReader reads newline-delimited output of any length, like a
// bufio.Scanner without its token limit. Engines and app-servers can write a
// single record of tens of megabytes (a page of Codex turns carries every
// generated image as base64), and a capped reader would stop at it, losing
// the record and everything after.
type lineReader struct {
	r    *bufio.Reader
	line []byte
	err  error
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{r: bufio.NewReaderSize(r, 65536)}
}

// Scan reads the next line, without its line ending; false once the input
// ends or cannot be read.
func (l *lineReader) Scan() bool {
	if l.err != nil {
		return false
	}
	line, err := l.r.ReadBytes('\n')
	if err != nil {
		l.err = err
		// A last line without a newline is still a line.
		if len(line) == 0 {
			return false
		}
	}
	line = bytes.TrimSuffix(line, []byte("\n"))
	l.line = bytes.TrimSuffix(line, []byte("\r"))
	return true
}

func (l *lineReader) Bytes() []byte { return l.line }
func (l *lineReader) Text() string  { return string(l.line) }

// Err is the read error that ended Scan; nil at the end of the input.
func (l *lineReader) Err() error {
	if errors.Is(l.err, io.EOF) {
		return nil
	}
	return l.err
}
