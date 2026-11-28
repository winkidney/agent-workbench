package workbench

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func squarePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAssistantIconIsChecked(t *testing.T) {
	for _, ok := range []string{"", "camera", "mail", "toolbox", "icon.png", "my-icon_2.png"} {
		if err := checkIcon(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"../x.png", "a/b.png", `a\b.png`, ".png", ".hidden.png", "icon.svg", "icon.PNG.exe", "rocket"} {
		if err := checkIcon(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "assistant.toml")
	writeToml(t, p, "name = \"x\"\nagent = \"codex\"\nicon = \"../secret.png\"")
	if _, err := readAssistant(p); err == nil || !strings.Contains(err.Error(), "icon") {
		t.Fatalf("a path outside its folder: %v", err)
	}
}

// iconAssistant: a set-up assistants folder with one assistant, and a
// server for its icon requests.
func iconAssistant(t *testing.T) (*Server, *Manager, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git")
	}
	s, m := authServer(t)
	root := filepath.Join(t.TempDir(), "assistants")
	if _, err := m.setUp(root, "claude", "", "en"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.NewAssistant("Mail", "", "codex", "per-task"); err != nil {
		t.Fatal(err)
	}
	return s, m, filepath.Join(m.assistantsRoot(), "mail")
}

func lastCommit(t *testing.T, dir string) string {
	t.Helper()
	out, _ := exec.Command("git", "-C", dir, "log", "-1", "--name-only", "--format=%s").Output()
	return string(out)
}

func TestAssistantIconIsChosenUploadedAndRemoved(t *testing.T) {
	s, m, dir := iconAssistant(t)
	// A drawing by name, in its file and committed.
	mail := "mail"
	a, err := m.PatchAssistant("mail", AssistantPatch{Icon: &mail})
	if err != nil || a.Icon != "mail" || a.IconVersion != 0 {
		t.Fatalf("%+v %v", a, err)
	}
	// The owner's choice: in settings.toml, not the author's assistant.toml.
	if b, _ := os.ReadFile(filepath.Join(dir, "settings.toml")); !strings.Contains(string(b), `icon = "mail"`) {
		t.Fatalf("toml:\n%s", b)
	}
	if c := lastCommit(t, dir); !strings.Contains(c, "mail: icon mail") {
		t.Fatalf("commit %q", c)
	}
	rocket := "rocket"
	if _, err := m.PatchAssistant("mail", AssistantPatch{Icon: &rocket}); err == nil {
		t.Fatal("an unknown drawing was accepted")
	}
	// Uploaded: a 128-pixel PNG becomes its icon, file and line committed
	// together.
	put := func(body []byte) int {
		return request(t, s, s.Host, "PUT", "/api/assistants/mail/icon", string(body), nil).Code
	}
	for _, bad := range [][]byte{[]byte("not an image"), squarePNG(t, 64, 64), squarePNG(t, 128, 96), bytes.Repeat([]byte{0}, iconMaxBytes+1)} {
		if code := put(bad); code != 400 {
			t.Fatalf("bad upload answered %d", code)
		}
	}
	if code := put(squarePNG(t, 128, 128)); code != 200 {
		t.Fatalf("upload answered %d", code)
	}
	a, _ = m.assistant("mail")
	if a.Icon != "file" || a.IconVersion == 0 || a.file.Icon != "icon.png" {
		t.Fatalf("after upload %+v", a)
	}
	if c := lastCommit(t, dir); !strings.Contains(c, "icon.png") || !strings.Contains(c, "settings.toml") {
		t.Fatalf("commit %q", c)
	}
	w := request(t, s, s.Host, "GET", "/api/assistants/mail/icon?v=1", "", nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" || !bytes.HasPrefix(w.Body.Bytes(), []byte("\x89PNG")) {
		t.Fatalf("get %d %v", w.Code, w.Header())
	}
	// Uploaded again: only the image changes, and is committed.
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	img.Pix[0], img.Pix[3] = 9, 255
	var b bytes.Buffer
	png.Encode(&b, img)
	if code := put(b.Bytes()); code != 200 {
		t.Fatalf("second upload %d", code)
	}
	if c := lastCommit(t, dir); !strings.HasPrefix(c, "mail: icon\n") || !strings.Contains(c, "icon.png") {
		t.Fatalf("commit %q", c)
	}
	// Removed: back to the camera, the image gone.
	w = request(t, s, s.Host, "DELETE", "/api/assistants/mail/icon", "", nil)
	var v Assistant
	json.Unmarshal(w.Body.Bytes(), &v)
	if w.Code != 200 || v.Icon != "camera" {
		t.Fatalf("delete %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "icon.png")); err == nil {
		t.Fatal("the image stayed")
	}
	if w := request(t, s, s.Host, "GET", "/api/assistants/mail/icon", "", nil); w.Code != 404 {
		t.Fatalf("get after delete %d", w.Code)
	}
}

// An icon file named in assistant.toml but missing shows the camera; a
// file that is not the right PNG is not served.
func TestAssistantIconFileMustBeThere(t *testing.T) {
	s, m, dir := iconAssistant(t)
	name := "other.png"
	if _, err := m.PatchAssistant("mail", AssistantPatch{Icon: &name}); err != nil {
		t.Fatal(err)
	}
	if a, _ := m.assistant("mail"); a.Icon != "" {
		t.Fatalf("missing file shown as %q", a.Icon)
	}
	os.WriteFile(filepath.Join(dir, "other.png"), []byte("<svg onload=alert(1)>"), 0o644)
	m.assistantsChanged()
	if w := request(t, s, s.Host, "GET", "/api/assistants/mail/icon", "", nil); w.Code != 422 {
		t.Fatalf("not a PNG answered %d", w.Code)
	}
}
