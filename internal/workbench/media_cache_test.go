package workbench

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// An agent rewrites an image under the same name: the page must not keep
// showing the old copy, yet an unchanged file costs no download.
func TestMediaRevalidatesRewrittenFiles(t *testing.T) {
	s, session, root, _ := mediaServer(t)
	cookie := request(t, s, s.Host, "POST", "/api/login", `{"code":"pair"}`, nil).Result().Cookies()[0]
	get := func(etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://"+s.Host+"/api/media?session="+url.QueryEscape(session.ID)+"&path=out.png", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Host = s.Host
		r.AddCookie(cookie)
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	checkRevalidation(t, filepath.Join(root, "out.png"), get)
}

func TestFilePreviewsRevalidateRewrittenFiles(t *testing.T) {
	dir := t.TempDir()
	p := &Preview{root: dir}
	get := func(etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://preview.localhost/out.png", nil)
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		return w
	}
	checkRevalidation(t, filepath.Join(dir, "out.png"), get)
}

func checkRevalidation(t *testing.T, file string, get func(etag string) *httptest.ResponseRecorder) {
	t.Helper()
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00")
	// Same second, same size, different content: only the nanoseconds differ.
	at := time.Now().Truncate(time.Second)
	write := func(last byte, ns int) {
		data := append(append([]byte{}, png...), last)
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}
		mt := at.Add(time.Duration(ns))
		if err := os.Chtimes(file, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	write(1, 1000)
	first := get("")
	tag := first.Header().Get("ETag")
	if first.Code != 200 || tag == "" || first.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("first: %d etag=%q cache=%q", first.Code, tag, first.Header().Get("Cache-Control"))
	}
	if again := get(tag); again.Code != http.StatusNotModified {
		t.Fatalf("unchanged: %d", again.Code)
	}
	write(2, 2000)
	if fresh := get(tag); fresh.Code != 200 || fresh.Header().Get("ETag") == tag || fresh.Body.Bytes()[len(png)] != 2 {
		t.Fatalf("rewritten: %d etag=%q", fresh.Code, fresh.Header().Get("ETag"))
	}
}
