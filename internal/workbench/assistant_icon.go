package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// An assistant's icon (ADR 0030): one of the page's drawings by name, or a
// PNG in its folder that Settings uploaded, square and iconSide pixels.

// builtinIcons are the drawings the page has (web/src/components/
// assistantIcons.ts and the camera, the default).
var builtinIcons = []string{"camera", "mail", "review", "video", "calendar", "bell", "globe", "chart", "book", "chat", "doc", "toolbox"}

const (
	iconFile     = "icon.png"
	iconSide     = 128
	iconMaxBytes = 512 << 10
)

var iconFileRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.png$`)

func builtinIcon(name string) bool {
	for _, b := range builtinIcons {
		if b == name {
			return true
		}
	}
	return false
}

// checkIcon: "", a drawing's name, or a .png file name in the folder.
func checkIcon(v string) error {
	if v == "" || builtinIcon(v) || iconFileRE.MatchString(v) {
		return nil
	}
	return fmt.Errorf("icon: one of %s, or a .png file in the assistant's folder", strings.Join(builtinIcons, ", "))
}

// iconShown is what the page shows: the drawing's name, or "file" and the
// image's time; a missing image is the default drawing.
func iconShown(dir, icon string) (string, int64) {
	if icon == "" || builtinIcon(icon) {
		return icon, 0
	}
	info, err := os.Stat(filepath.Join(dir, icon))
	if err != nil || !info.Mode().IsRegular() {
		return "", 0
	}
	return "file", info.ModTime().UnixMilli()
}

// checkIconImage: a PNG, square, iconSide pixels (the page crops and scales
// what the user picks to that).
func checkIconImage(b []byte) error {
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return errors.New("the icon must be a PNG image")
	}
	if cfg.Width != iconSide || cfg.Height != iconSide {
		return fmt.Errorf("the icon must be %d×%d pixels, not %d×%d", iconSide, iconSide, cfg.Width, cfg.Height)
	}
	return nil
}

// assistantIconAPI: GET the image an assistant's icon names; PUT a new one
// (it becomes its icon); DELETE it (back to the camera).
func (m *Manager) assistantIconAPI(w http.ResponseWriter, r *http.Request, dir string) {
	a, err := m.assistant(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	switch r.Method {
	case "GET":
		if a.Icon != "file" {
			http.Error(w, "no icon image", http.StatusNotFound)
			return
		}
		f, err := os.Open(filepath.Join(a.Path, a.file.Icon))
		if err != nil {
			http.Error(w, "no icon image", http.StatusNotFound)
			return
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, iconMaxBytes+1))
		if err != nil || len(b) > iconMaxBytes || checkIconImage(b) != nil {
			http.Error(w, "the icon image is not a "+strconv.Itoa(iconSide)+"-pixel PNG", http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// The page asks with its time (?v=), so a new one is a new address.
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		w.Write(b)
	case "PUT":
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, iconMaxBytes))
		if err != nil {
			bad(w, fmt.Errorf("the icon must be at most %d KB", iconMaxBytes>>10))
			return
		}
		if err := checkIconImage(b); err != nil {
			bad(w, err)
			return
		}
		path := filepath.Join(a.Path, iconFile)
		if err := os.WriteFile(path+".tmp", b, 0o644); err != nil {
			bad(w, err)
			return
		}
		if err := os.Rename(path+".tmp", path); err != nil {
			bad(w, err)
			return
		}
		name := iconFile
		if a.file.Icon == iconFile {
			// Only the image changed: committed as the patch would.
			m.commitPaths(m.assistantsRoot(), a.Dir+": icon", []string{a.Dir + "/" + iconFile})
			m.assistantsChanged()
			a, err = m.assistant(dir)
		} else {
			a, err = m.PatchAssistant(dir, AssistantPatch{Icon: &name})
		}
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, a)
	case "DELETE":
		if a.file.Icon != "" && !builtinIcon(a.file.Icon) {
			if err := os.Remove(filepath.Join(a.Path, a.file.Icon)); err != nil && !errors.Is(err, os.ErrNotExist) {
				bad(w, err)
				return
			}
		}
		camera := "camera"
		a, err = m.PatchAssistant(dir, AssistantPatch{Icon: &camera})
		if err != nil {
			bad(w, err)
			return
		}
		writeJSON(w, a)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
