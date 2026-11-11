package workbench

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"time"
)

// revealRun runs a desktop helper with an argument vector (never a shell);
// replaced in tests.
var revealRun = func(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	rawArgs(cmd, args)
	return cmd.Run()
}

// revealOS is the platform reveal targets; replaced in tests.
var revealOS = goruntime.GOOS

// reveal shows a file selected in its folder, or opens a folder, in the
// desktop file manager of the machine running the workbench. A file is
// never handed to an opener, so nothing is executed or opened by type.
func reveal(full string) error {
	info, err := os.Stat(full)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch revealOS {
	case "linux":
		method := "ShowItems"
		if info.IsDir() {
			method = "ShowFolders"
		}
		// org.freedesktop.FileManager1 (Dolphin, Nautilus, Nemo, Thunar…).
		uri := (&url.URL{Scheme: "file", Path: full}).String()
		if revealRun(ctx, "busctl", "--user", "call", "org.freedesktop.FileManager1", "/org/freedesktop/FileManager1", "org.freedesktop.FileManager1", method, "ass", "1", uri, "") == nil {
			return nil
		}
		folder := full
		if !info.IsDir() {
			folder = filepath.Dir(full)
		}
		return revealRun(ctx, "xdg-open", folder)
	case "darwin":
		if info.IsDir() {
			return revealRun(ctx, "open", full)
		}
		return revealRun(ctx, "open", "-R", full)
	case "windows":
		// Explorer reads its command line its own way (passed as is) and
		// exits with 1 even when it worked. Paths cannot contain quotes.
		arg := `"` + full + `"`
		if !info.IsDir() {
			arg = "/select," + arg
		}
		var exit *exec.ExitError
		if err := revealRun(ctx, "explorer.exe", arg); err != nil && !errors.As(err, &exit) {
			return err
		}
		return nil
	}
	return errors.New("showing files is not supported on " + revealOS)
}
