package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/winkidney/agent-workbench/internal/workbench"
)

// `agent-workbench update` installs the newest release (ADR 0028): it
// downloads this platform's archive and checksums.txt from the release,
// checks the archive's SHA-256, takes the program out, checks it is the
// version it should be, and runs its install, which stops this workbench
// and starts the new one. When the new one does not start, the previous
// program, kept beside it, is installed again. Each step is written to
// update.json, which the About tab reads, and to update.log.

// The largest archive it downloads.
const updateMaxBytes = 200 << 20

// updateBlocked is why this workbench cannot install a release itself, or "".
func updateBlocked(c config, exe string) string {
	if packaged(exe) {
		return "a package manager installed it (" + exe + "): update it with that package manager"
	}
	if !autostartFor(c, c.dataDir) {
		return "it is not installed to start at login: run `agent-workbench install` once, then it can update itself"
	}
	return ""
}

// assetName is this platform's archive in a release: macOS has one
// universal binary, Windows a zip.
func assetName(tag, goos, goarch string) string {
	ver := strings.TrimPrefix(tag, "v")
	switch goos {
	case "darwin":
		return fmt.Sprintf("agent-workbench_%s_macos_universal.tar.gz", ver)
	case "windows":
		return fmt.Sprintf("agent-workbench_%s_windows_%s.zip", ver, goarch)
	}
	return fmt.Sprintf("agent-workbench_%s_%s_%s.tar.gz", ver, goos, goarch)
}

// checksumOf finds name's SHA-256 in checksums.txt ("<hex>  <name>").
func checksumOf(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			if len(f[0]) != 64 {
				break
			}
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt does not list %s", name)
}

// download fetches url into w, at most limit bytes, and returns its SHA-256.
func download(ctx context.Context, url string, limit int64, w io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "agent-workbench")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("%s: %s", path.Base(url), res.Status)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(res.Body, limit+1))
	if err != nil {
		return "", err
	}
	if n > limit {
		return "", fmt.Errorf("%s is larger than %d MB", path.Base(url), limit>>20)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractProgram takes the program (and its companions) out of a release
// archive into dir: only those files, from the archive's top level.
func extractProgram(archive, dir string) error {
	want := map[string]bool{binaryName: true}
	for _, c := range companions {
		want[c] = true
	}
	found := map[string]bool{}
	write := func(name string, r io.Reader) error {
		if !want[name] {
			return nil
		}
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(r, updateMaxBytes)); err != nil {
			out.Close()
			return err
		}
		found[name] = true
		return out.Close()
	}
	// The top level only: "agent-workbench", not "x/agent-workbench" or
	// "../agent-workbench".
	top := func(name string) string {
		if strings.Contains(name, "/") || strings.Contains(name, `\`) || name == ".." {
			return ""
		}
		return name
	}
	if strings.HasSuffix(archive, ".zip") {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer z.Close()
		for _, f := range z.File {
			if f.FileInfo().IsDir() {
				continue
			}
			r, err := f.Open()
			if err != nil {
				return err
			}
			err = write(top(f.Name), r)
			r.Close()
			if err != nil {
				return err
			}
		}
	} else {
		f, err := os.Open(archive)
		if err != nil {
			return err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if h.Typeflag != tar.TypeReg {
				continue
			}
			if err := write(top(strings.TrimPrefix(h.Name, "./")), tr); err != nil {
				return err
			}
		}
	}
	if !found[binaryName] {
		return fmt.Errorf("the archive has no %s", binaryName)
	}
	return nil
}

// previousPath is where the program in use is kept while a new one is
// installed: agent-workbench.previous, agent-workbench.previous.exe.
func previousPath(target string) string {
	ext := filepath.Ext(target)
	return strings.TrimSuffix(target, ext) + ".previous" + ext
}

// installArgs install the program into the same instance: its data, name
// and address, and the extra variables the service was given.
func installArgs(c config) []string {
	args := []string{"install", "--data-dir", c.dataDir, "--listen", c.listen, "--name", c.name}
	env, _ := readEnv(envPath(c))
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		known := false
		for _, s := range serviceEnv {
			known = known || s == k
		}
		if !known {
			args = append(args, "--env", k)
		}
	}
	return args
}

func updateDir(c config) string { return filepath.Join(c.dataDir, "update") }

func updateLogPath(c config) string { return filepath.Join(c.dataDir, "update.log") }

// update installs the release named to (the newest one when "").
func update(c config, to string) error {
	logf, err := os.OpenFile(updateLogPath(c), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	out := io.MultiWriter(os.Stdout, logf)
	say := func(format string, a ...any) {
		fmt.Fprintf(out, time.Now().Format("15:04:05 ")+format+"\n", a...)
	}
	st := workbench.UpdateStatus{From: version, To: to, Log: updateLogPath(c)}
	report := func(state string, err error) {
		st.State, st.Error, st.At = state, "", ""
		if err != nil {
			st.Error = err.Error()
			say("%s: %v", state, err)
		} else {
			say("%s", state)
		}
		_ = workbench.WriteUpdateStatus(c.dataDir, st)
	}
	fail := func(err error) error {
		report("failed", err)
		return err
	}
	// The service's environment, whoever started this: where its unit and
	// program are (HOME, XDG_CONFIG_HOME), and what install records again.
	if exists(envPath(c)) {
		if err := loadEnv(envPath(c)); err != nil {
			return fail(err)
		}
	}
	// The program the service runs, kept to put back.
	target := filepath.Join(installDir(), binaryName)
	// Started from the About tab, this is a copy of it (startUpdate); from
	// a terminal, a package manager's program says so.
	exe, err := executable()
	if err != nil {
		return fail(err)
	}
	if strings.HasPrefix(exe, updateDir(c)+string(filepath.Separator)) {
		exe = target
	}
	if why := updateBlocked(c, exe); why != "" {
		return fail(errors.New(why))
	}
	// install puts the new program where target is: it must be the one
	// the workbench runs, not another copy.
	if c.program != "" && !same(c.program, target) {
		return fail(fmt.Errorf("the workbench runs %s, not the installed %s: run `agent-workbench install` with the program you want, then update", c.program, target))
	}
	release, err := workbench.NewestRelease()
	if err != nil {
		return fail(fmt.Errorf("could not check for updates: %w", err))
	}
	if to == "" {
		to = release.Tag
	}
	st.To = to
	if !workbench.SameVersion(release.Tag, to) {
		return fail(fmt.Errorf("the newest release is %s, not %s", release.Tag, to))
	}
	if !workbench.NewerVersion(version, to) {
		say("%s is already the newest release", version)
		report("done", nil)
		return nil
	}
	urls := map[string]string{}
	for _, a := range release.Assets {
		urls[a.Name] = a.URL
	}
	name := assetName(to, runtime.GOOS, runtime.GOARCH)
	if urls[name] == "" || urls["checksums.txt"] == "" {
		return fail(fmt.Errorf("release %s has no %s or checksums.txt", to, name))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	report("downloading", nil)
	dir := filepath.Join(updateDir(c), to)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(err)
	}
	var sums bytes.Buffer
	if _, err := download(ctx, urls["checksums.txt"], 1<<20, &sums); err != nil {
		return fail(err)
	}
	archive := filepath.Join(dir, name)
	f, err := os.Create(archive)
	if err != nil {
		return fail(err)
	}
	got, err := download(ctx, urls[name], updateMaxBytes, f)
	f.Close()
	if err != nil {
		return fail(err)
	}
	report("verifying", nil)
	wantSum, err := checksumOf(sums.Bytes(), name)
	if err != nil {
		return fail(err)
	}
	if got != wantSum {
		return fail(fmt.Errorf("%s does not match its checksum (got %s, want %s)", name, got, wantSum))
	}
	say("%s: SHA-256 %s matches checksums.txt", name, got)
	if err := extractProgram(archive, dir); err != nil {
		return fail(err)
	}
	staged := filepath.Join(dir, binaryName)
	v, err := exec.Command(staged, "version").Output()
	if err != nil {
		return fail(fmt.Errorf("the new program does not run: %w", err))
	}
	if !workbench.SameVersion(string(v), to) {
		return fail(fmt.Errorf("the new program says it is %s, not %s", strings.TrimSpace(string(v)), to))
	}
	report("installing", nil)
	prev := previousPath(target)
	if err := copyFile(target, prev); err != nil {
		return fail(fmt.Errorf("could not keep the program in use: %w", err))
	}
	if err := runInstall(staged, c, out); err != nil {
		say("the new program did not start: %v", err)
		say("putting %s back", version)
		if back := runInstall(prev, c, out); back != nil {
			return fail(fmt.Errorf("%v; putting %s back failed too: %v", err, version, back))
		}
		report("rolled-back", err)
		return err
	}
	report("done", nil)
	_ = os.RemoveAll(dir)
	return nil
}

// runInstall runs a program's install for this instance, and checks the
// workbench that answers afterwards is that program.
func runInstall(program string, c config, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, installArgs(c)...)
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return err
	}
	want, _ := exec.Command(program, "version").Output()
	info, err := workbench.ReadServerInfo(c.dataDir)
	if err != nil {
		return err
	}
	if !workbench.SameVersion(info.Version, string(want)) {
		return fmt.Errorf("%s answers, not %s", info.Version, strings.TrimSpace(string(want)))
	}
	return nil
}

// startUpdate starts `update --to` apart from the server, from a copy of
// this program (the service's own may be replaced while it runs; Windows
// cannot replace a program that runs), so it outlives the server's stop.
func startUpdate(c config, to string) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	dir := filepath.Join(updateDir(c), "bin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	updater := filepath.Join(dir, binaryName)
	if err := copyFile(exe, updater); err != nil {
		return err
	}
	for _, extra := range companions {
		if from := filepath.Join(filepath.Dir(exe), extra); exists(from) {
			if err := copyFile(from, filepath.Join(dir, extra)); err != nil {
				return err
			}
		}
	}
	args := []string{"update", "--to", to, "--data-dir", c.dataDir, "--listen", c.listen, "--name", c.name, "--program", exe}
	return detachUpdate(c, updater, args)
}
