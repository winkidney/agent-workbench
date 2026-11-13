package workbench

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"

	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func pngOf(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// noisy is a PNG that does not compress, of about n bytes.
func noisy(t *testing.T, n int) []byte {
	t.Helper()
	side := 1
	for side*side*4 < n {
		side++
	}
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	seed := uint32(7)
	for i := range img.Pix {
		seed = seed*1664525 + 1013904223
		img.Pix[i] = byte(seed >> 24)
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func upload(t *testing.T, s *Server, data []byte, contentType string) (int, Upload, string) {
	t.Helper()
	r := httptest.NewRequest("POST", "http://"+s.Host+"/api/uploads", bytes.NewReader(data))
	r.Host, r.RemoteAddr = s.Host, "127.0.0.1:1234"
	r.Header.Set("Origin", "http://"+s.Host)
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var up Upload
	_ = json.Unmarshal(w.Body.Bytes(), &up)
	return w.Code, up, w.Body.String()
}

// Only raster images the agents take are kept, judged by their bytes.
func TestUploadsKeepImagesByTheirContent(t *testing.T) {
	s, m := authServer(t)
	red := pngOf(t, 4, 4, color.RGBA{255, 0, 0, 255})
	code, up, body := upload(t, s, red, "image/png")
	if code != 200 || up.Mime != "image/png" || up.Bytes != int64(len(red)) || !uploadName.MatchString(up.ID) || up.URL != "/api/uploads/"+up.ID {
		t.Fatalf("png: %d %s", code, body)
	}
	// The same image is kept once.
	if _, again, _ := upload(t, s, red, "image/png"); again.ID != up.ID {
		t.Fatalf("same image, new id: %s %s", again.ID, up.ID)
	}
	var jb bytes.Buffer
	jpeg.Encode(&jb, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil)
	if code, up, _ := upload(t, s, jb.Bytes(), "image/jpeg"); code != 200 || !strings.HasSuffix(up.ID, ".jpg") {
		t.Fatalf("jpeg: %d %v", code, up)
	}
	webp := append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 32)...)
	if code, up, _ := upload(t, s, webp, "image/webp"); code != 200 || !strings.HasSuffix(up.ID, ".webp") {
		t.Fatalf("webp: %d %v", code, up)
	}
	// Whatever the request claims, active or other content is refused.
	for name, data := range map[string]string{
		"svg":  `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`,
		"html": `<!doctype html><script>alert(1)</script>`,
		"gif":  "GIF89a\x01\x00\x01\x00\x00\x00\x00;",
		"text": "just words",
	} {
		if code, _, _ := upload(t, s, []byte(data), "image/png"); code != 415 {
			t.Fatalf("%s: %d", name, code)
		}
	}
	if code, _, _ := upload(t, s, noisy(t, uploadMax+1024), "image/png"); code != 413 {
		t.Fatalf("too large: %d", code)
	}
	entries, _ := os.ReadDir(m.uploads.dir)
	if len(entries) != 3 {
		t.Fatalf("kept %d files", len(entries))
	}
	if info, _ := os.Stat(filepath.Join(m.uploads.dir, up.ID)); goruntime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}

	// Served inert and kept by the browser.
	w := request(t, s, s.Host, "GET", "/api/uploads/"+up.ID, "", nil)
	h := w.Header()
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), red) || h.Get("Content-Type") != "image/png" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Content-Security-Policy") != "sandbox; default-src 'none'" || h.Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Fatalf("get: %d %v", w.Code, h)
	}
	for _, bad := range []string{"nothing.png", strings.Repeat("0", 64) + ".png", "../" + up.ID, strings.TrimSuffix(up.ID, ".png") + ".svg", "%2e%2e%2fsecret"} {
		if w := request(t, s, s.Host, "GET", "/api/uploads/"+bad, "", nil); w.Code != 404 {
			t.Fatalf("%s: %d", bad, w.Code)
		}
	}
}

// Uploads answer only signed-in pages of the workbench's own origin.
func TestUploadsNeedSignInAndTheOrigin(t *testing.T) {
	s, _ := authServer(t)
	red := pngOf(t, 2, 2, color.RGBA{255, 0, 0, 255})
	_, up, _ := upload(t, s, red, "image/png")
	setAccess(t, s, `{"remoteAccess":"pairing"}`)
	if w := lan(t, s, "POST", "/api/uploads", string(red), nil); w.Code != 401 {
		t.Fatalf("anonymous upload: %d", w.Code)
	}
	if w := lan(t, s, "GET", "/api/uploads/"+up.ID, "", nil); w.Code != 401 {
		t.Fatalf("anonymous read: %d", w.Code)
	}
	r := httptest.NewRequest("POST", "http://"+s.Host+"/api/uploads", bytes.NewReader(red))
	r.Host, r.RemoteAddr = s.Host, "127.0.0.1:1234"
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin upload: %d", w.Code)
	}
}

func keep(t *testing.T, m *Manager, data []byte) Upload {
	t.Helper()
	up, err := m.uploads.Save(data)
	if err != nil {
		t.Fatal(err)
	}
	return up
}

func TestClaudeGetsImagesBeforeTheText(t *testing.T) {
	m, p, r := claudeReady(t)
	id := r.session.ID
	red := pngOf(t, 3, 3, color.RGBA{255, 0, 0, 255})
	up := keep(t, m, red)
	if err := m.SendWith(id, "what color?", []string{up.ID}); err != nil {
		t.Fatal(err)
	}
	msg := sent(p, func(v map[string]any) bool { return v["type"] == "user" })
	content := list(obj(msg["message"])["content"])
	img, text := obj(content[0]), obj(content[1])
	source := obj(img["source"])
	if len(content) != 2 || img["type"] != "image" || source["type"] != "base64" || source["media_type"] != "image/png" ||
		source["data"] != base64.StdEncoding.EncodeToString(red) || text["text"] != "what color?" {
		t.Fatalf("content: %v", content)
	}
	// The page is shown the image by its address, not its bytes.
	users := eventsOf(m, id, "user")
	if shown := users[len(users)-1]; shown["text"] != "what color?" || len(list(shown["images"])) != 1 || list(shown["images"])[0] != up.URL {
		t.Fatalf("shown: %v", shown)
	}
}

func TestCodexGetsImagesAsDataURLs(t *testing.T) {
	m, p, r, _ := codexReady(t)
	id := r.session.ID
	red := pngOf(t, 3, 3, color.RGBA{255, 0, 0, 255})
	up := keep(t, m, red)
	// Images alone are a message.
	if err := m.SendWith(id, "", []string{up.ID}); err != nil {
		t.Fatal(err)
	}
	input := list(obj(sentMethod(p, "turn/start")["params"])["input"])
	if len(input) != 1 || obj(input[0])["type"] != "image" || obj(input[0])["url"] != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(red) {
		t.Fatalf("input: %v", input)
	}
}

func TestMessagesWithImagesAreChecked(t *testing.T) {
	m, _, r := claudeReady(t)
	id := r.session.ID
	up := keep(t, m, pngOf(t, 2, 2, color.RGBA{0, 0, 255, 255}))
	for name, images := range map[string][]string{
		"nothing":  nil,
		"unknown":  {strings.Repeat("a", 64) + ".png"},
		"made up":  {"../../etc/passwd"},
		"too many": {up.ID, up.ID, up.ID, up.ID, up.ID, up.ID, up.ID, up.ID, up.ID, up.ID, up.ID},
	} {
		if err := m.SendWith(id, "", images); err == nil {
			t.Fatalf("%s: sent", name)
		}
	}
	if _, err := m.EnqueueWith(id, "x", []string{"gone.png"}); err == nil {
		t.Fatal("queued an unknown image")
	}
}

// Queued and steered messages keep their images until they are sent.
func TestQueuedAndSteeredMessagesKeepTheirImages(t *testing.T) {
	m, p, r, answer := codexReady(t)
	id := r.session.ID
	up := keep(t, m, pngOf(t, 2, 2, color.RGBA{0, 255, 0, 255}))
	if err := m.Send(id, "first"); err != nil {
		t.Fatal(err)
	}
	if queued, err := m.EnqueueWith(id, "after", []string{up.ID}); err != nil || !queued {
		t.Fatalf("queue: %v %v", queued, err)
	}
	q := eventsOf(m, id, "queue")
	if items := list(obj(q[len(q)-1])["items"]); len(items) != 1 || list(obj(items[0])["images"])[0] != up.ID {
		t.Fatalf("queue event: %v", q[len(q)-1])
	}
	m.receive(id, r.generation, map[string]any{"method": "turn/started", "params": map[string]any{"turn": map[string]any{"id": "turn-1"}}})
	answer("turn/steer", map[string]any{"turnId": "turn-1"})
	if res, err := m.SteerQueued(id, r.queue[0].ID); err != nil || !res.Steered {
		t.Fatalf("steer queued: %+v %v", res, err)
	}
	input := list(obj(sentMethod(p, "turn/steer")["params"])["input"])
	if len(input) != 2 || obj(input[0])["type"] != "image" || obj(input[1])["text"] != "after" {
		t.Fatalf("steer input: %v", input)
	}

	// Claude: a steer the stop cancels goes back to the queue with them.
	cm, cp, cr := claudeReady(t, "interrupt_send_now_v1", "interrupt_cancel_queued_v1")
	cid := cr.session.ID
	cup := keep(t, cm, pngOf(t, 2, 2, color.RGBA{0, 255, 0, 255}))
	cm.Send(cid, "first")
	if res, err := cm.SteerWith(cid, "look", []string{cup.ID}); err != nil || !res.Steered {
		t.Fatalf("claude steer: %+v %v", res, err)
	}
	msg := sent(cp, func(v map[string]any) bool { return v["uuid"] != nil && v["type"] == "user" })
	if content := list(obj(msg["message"])["content"]); len(content) != 2 || obj(content[0])["type"] != "image" {
		t.Fatalf("claude steer content: %v", content)
	}
	cm.receive(cid, cr.generation, map[string]any{"type": "command_lifecycle", "command_uuid": msg["uuid"], "state": "cancelled"})
	if len(cr.queue) != 1 || len(cr.queue[0].Images) != 1 || cr.queue[0].Images[0] != cup.ID {
		t.Fatalf("returned: %+v", cr.queue)
	}
	// Sent from the queue once the turn ends.
	cm.receive(cid, cr.generation, map[string]any{"type": "result"})
	cm.ResumeQueue(cid)
	again := sent(cp, func(v map[string]any) bool {
		c := list(obj(v["message"])["content"])
		return v["type"] == "user" && v["uuid"] == nil && len(c) == 2
	})
	if again == nil {
		t.Fatal("queued message not sent with its image")
	}
}

// Large images in records go to the page by name, not as text.
func TestHistoryNamesLargeImagesInsteadOfCarryingThem(t *testing.T) {
	big := noisy(t, historyImageLimit)
	b64 := base64.StdEncoding.EncodeToString(big)
	id := uploadID(big, "image/png")
	claude := boundJSON(map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": b64}}).(map[string]any)
	if claude["omitted"] != true || claude["upload"] != id || claude["media_type"] != "image/png" {
		t.Fatalf("claude: %v", claude["upload"])
	}
	codex := boundJSON(map[string]any{"type": "image", "url": "data:image/png;base64," + b64}).(map[string]any)
	if codex["omitted"] != true || codex["upload"] != id || codex["url"] != nil {
		t.Fatalf("codex: %v", codex)
	}
	// A type the workbench does not keep is just left out.
	gif := boundJSON(map[string]any{"type": "image", "url": "data:image/gif;base64," + b64}).(map[string]any)
	if gif["omitted"] != true || gif["upload"] != nil {
		t.Fatalf("gif: %v", gif)
	}
	// Small ones and addresses stay as they are.
	small := map[string]any{"type": "image", "url": "data:image/png;base64,iVBORw0KGgo="}
	if got := boundJSON(small).(map[string]any); got["url"] != small["url"] {
		t.Fatalf("small: %v", got)
	}
	link := map[string]any{"type": "image", "url": "https://example.com/" + strings.Repeat("a", historyImageLimit)}
	if got := boundJSON(link).(map[string]any); got["url"] != link["url"] {
		t.Fatal("an address was changed")
	}
}

func TestUploadsAreKeptAWeekAfterLastUseAndUpToALimit(t *testing.T) {
	u := &Uploads{dir: t.TempDir()}
	old := keepAt(t, u, pngOf(t, 1, 1, color.RGBA{1, 0, 0, 255}), 8*24*time.Hour)
	older := keepAt(t, u, pngOf(t, 1, 1, color.RGBA{2, 0, 0, 255}), 3*time.Hour)
	newer := keepAt(t, u, pngOf(t, 1, 1, color.RGBA{3, 0, 0, 255}), time.Hour)
	stray := filepath.Join(u.dir, ".upload-123")
	os.WriteFile(stray, []byte("half"), 0o600)
	os.Chtimes(stray, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour))
	size := func(id string) int64 { info, _ := os.Stat(filepath.Join(u.dir, id)); return info.Size() }
	// Room for one: the oldest goes first.
	if n := u.Tidy(7*24*time.Hour, size(newer)); n != 3 {
		t.Fatalf("removed %d", n)
	}
	for id, want := range map[string]bool{old: false, older: false, newer: true} {
		if _, ok := u.Path(id); ok != want {
			t.Fatalf("%s kept=%v", id, ok)
		}
	}
	// Reading for a message counts as a use.
	path := filepath.Join(u.dir, newer)
	os.Chtimes(path, time.Now().Add(-3*time.Hour), time.Now().Add(-3*time.Hour))
	if _, err := u.Read([]string{newer}); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); time.Since(info.ModTime()) > time.Minute {
		t.Fatal("use not recorded")
	}
}

func keepAt(t *testing.T, u *Uploads, data []byte, age time.Duration) string {
	t.Helper()
	up, err := u.Save(data)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	os.Chtimes(filepath.Join(u.dir, up.ID), at, at)
	return up.ID
}

// Files of any kind are kept under their own name, and the message tells
// the agent where they are.
func TestFilesAreKeptAndNamedInTheMessage(t *testing.T) {
	m, p, r := claudeReady(t)
	s := NewServer(m, fstest.MapFS{"index.html": {Data: []byte("app")}}, "agent.localhost:17880", "")
	post := func(path, body string) *httptest.ResponseRecorder {
		return request(t, s, s.Host, "POST", path, body, nil)
	}
	w := post("/api/uploads/files?name="+url.QueryEscape(`..\..\notes: v1?.txt`), "hello, file")
	var up Upload
	json.Unmarshal(w.Body.Bytes(), &up)
	if w.Code != 200 || up.Name != "notes_ v1_.txt" || up.Bytes != 11 || up.URL != "" || !strings.HasSuffix(up.ID, "/notes_ v1_.txt") {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	path := filepath.Join(m.uploads.dir, "files", filepath.FromSlash(up.ID))
	if b, _ := os.ReadFile(path); string(b) != "hello, file" {
		t.Fatalf("kept %q", b)
	}
	// Never served back.
	if w := request(t, s, s.Host, "GET", "/api/uploads/"+strings.ReplaceAll(up.ID, " ", "%20"), "", nil); w.Code != 404 {
		t.Fatalf("served: %d", w.Code)
	}
	body, _ := json.Marshal(map[string]any{"text": "read it", "files": []string{up.ID}})
	if w := post("/api/sessions/"+r.session.ID+"/send", string(body)); w.Code != 200 {
		t.Fatalf("send: %d %s", w.Code, w.Body.String())
	}
	msg := sent(p, func(v map[string]any) bool { return v["type"] == "user" })
	if got := obj(msg["message"])["content"]; got != "read it\n\n[File (11 B): "+path+"]" {
		t.Fatalf("content: %q", got)
	}
	// Ids that are not kept files are refused.
	for _, id := range []string{strings.Repeat("0", 64) + "/x.txt", "../../etc/passwd", strings.Split(up.ID, "/")[0] + "/../" + up.Name} {
		body, _ := json.Marshal(map[string]any{"text": "x", "files": []string{id}})
		if w := post("/api/sessions/"+r.session.ID+"/send", string(body)); w.Code != 400 {
			t.Fatalf("%s: %d", id, w.Code)
		}
	}
	if w := post("/api/uploads/files?name=big.bin", strings.Repeat("x", uploadFileMax+1)); w.Code != 413 {
		t.Fatalf("too large: %d", w.Code)
	}
}

func TestFileLinesAndNames(t *testing.T) {
	files := []attached{{Name: "a.pdf", Path: "/k/a.pdf", Bytes: 2048}, {Name: "b.zip", Path: "/k/b.zip", Bytes: 3 << 20}}
	if got := fileLines("look\n", files); got != "look\n\n[File (2 KB): /k/a.pdf]\n[File (3.0 MB): /k/b.zip]" {
		t.Fatalf("%q", got)
	}
	if got := fileLines("", files[:1]); got != "[File (2 KB): /k/a.pdf]" {
		t.Fatalf("%q", got)
	}
	for in, want := range map[string]string{"": "file", "..": "file", "a/b/c.txt": "c.txt", `C:\x\y.doc`: "y.doc", "bad\x00name.txt": "bad_name.txt", strings.Repeat("n", 150) + ".tar.gz": strings.Repeat("n", 97) + ".gz"} {
		if got := fileName(in); got != want {
			t.Fatalf("%q: %q", in, got)
		}
	}
	// Kept files count for the retention, and their folders go with them.
	u := &Uploads{dir: t.TempDir()}
	up, _ := u.SaveFile(strings.NewReader("x"), "old.txt")
	at := time.Now().Add(-8 * 24 * time.Hour)
	os.Chtimes(filepath.Join(u.dir, "files", filepath.FromSlash(up.ID)), at, at)
	if n := u.Tidy(7*24*time.Hour, 1<<30); n != 1 {
		t.Fatalf("removed %d", n)
	}
	if _, err := os.Stat(filepath.Join(u.dir, "files", strings.Split(up.ID, "/")[0])); err == nil {
		t.Fatal("folder left")
	}
}
