package workbench

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Images and videos are shown wherever they are (ADR 0008); what is not an
// image or video still is not.
func TestMediaShowsFilesOutsideTheWorkingDirectory(t *testing.T) {
	posixOnly(t)
	_, _, root, get := mediaServer(t)
	outside := t.TempDir()
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	mp4 := append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 2, 0, 'i', 's', 'o', 'm', 'm', 'p', '4', '1'}, make([]byte, 100)...)
	os.WriteFile(filepath.Join(outside, "shot é.png"), png, 0o644)
	os.WriteFile(filepath.Join(outside, "clip.mp4"), mp4, 0o644)
	os.WriteFile(filepath.Join(outside, "page.png"), []byte("<html><script>alert(1)</script>"), 0o644)
	os.Symlink(filepath.Join(outside, "shot é.png"), filepath.Join(outside, "link.png"))
	rel, _ := filepath.Rel(root, filepath.Join(outside, "shot é.png"))
	q := func(p string) string { return "path=" + url.QueryEscape(p) }
	for query, want := range map[string]string{
		q(filepath.Join(outside, "shot é.png")):             "206 image/png",
		q("file://" + filepath.Join(outside, "shot é.png")): "206 image/png",
		q(filepath.Join(outside, "link.png")):               "206 image/png",
		q(rel):                                              "206 image/png",
		q(filepath.Join(outside, "clip.mp4")):               "206 video/mp4",
		q(filepath.Join(outside, "missing.png")):            "404",
		q(filepath.Join(outside, "page.png")):               "415",
		q("/etc/passwd"):                                    "415",
	} {
		if got := get(query); !strings.HasPrefix(got, want) {
			t.Fatalf("%s: %s, want %s", query, got, want)
		}
	}
}
