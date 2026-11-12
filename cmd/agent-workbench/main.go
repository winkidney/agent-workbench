package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/winkidney/agent-workbench/internal/workbench"
	"github.com/winkidney/agent-workbench/web"
)

// version is set at release builds with -ldflags "-X main.version=…".
var version = "dev"

const usage = `usage: agent-workbench <command> [flags]

Commands:
  start       start the workbench in the background (default)
  stop        stop it
  status      show whether it runs, where, and the autostart state
  install     copy this binary to the user's program folder, start the
              workbench at login, and start it now (also upgrades)
  uninstall   remove the login start (--purge also deletes data)
  serve       run in the foreground
  doctor      check the agents and the platform
  version     print the version

Flags:
  --data-dir PATH      private state directory
  --listen HOST:PORT   listen address (default 0.0.0.0:17880)
  --idle 5m            idle engine timeout
  --env NAME           install: also pass this variable to the service
  --purge              uninstall: also delete the data directory
`

// config is the command line, shared by all commands.
type config struct {
	// name names the service: systemd unit, launchd label, Run value.
	name    string
	dataDir string
	listen  string
	idle    time.Duration
	envFile string
	env     []string
	purge   bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	action := "start"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action = args[0]
		args = args[1:]
	}
	var c config
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flags.StringVar(&c.dataDir, "data-dir", defaultDataDir(), "private state directory")
	// All interfaces, so LAN devices can connect (see hostAccess for who may
	// use which Host); 127.0.0.1:PORT keeps the workbench local-only.
	flags.StringVar(&c.listen, "listen", "0.0.0.0:17880", "listen address")
	flags.DurationVar(&c.idle, "idle", 5*time.Minute, "idle engine timeout")
	flags.StringVar(&c.envFile, "env-file", "", "serve: load the environment install captured")
	flags.Func("env", "install: also pass this variable to the service", func(v string) error {
		c.env = append(c.env, v)
		return nil
	})
	flags.BoolVar(&c.purge, "purge", false, "uninstall: also delete the data directory")
	// A second instance (tests, trials) needs its own service name.
	flags.StringVar(&c.name, "name", "agent-workbench", "service name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q\n%s", flags.Arg(0), usage)
	}
	dir, err := filepath.Abs(c.dataDir)
	if err != nil {
		return err
	}
	c.dataDir = dir
	switch action {
	case "serve":
		return serve(c)
	case "start":
		return start(c)
	case "stop":
		return stop(c, true)
	case "status":
		return status(c)
	case "install":
		return install(c)
	case "uninstall":
		return uninstall(c)
	case "doctor":
		return doctor(c)
	case "version":
		fmt.Println(version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n%s", action, usage)
}

func serve(c config) error {
	if c.envFile != "" {
		if err := loadEnv(c.envFile); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(c.dataDir, 0o700); err != nil {
		return err
	}
	code, err := os.ReadFile(filepath.Join(c.dataDir, "pairing-code"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	debug.SetMemoryLimit(32 * 1024 * 1024)
	store, err := workbench.OpenStore(c.dataDir)
	if err != nil {
		return err
	}
	defer store.Close()
	manager, err := workbench.NewManager(store, workbench.DefaultLaunch(), c.idle)
	if err != nil {
		return err
	}
	assets, err := fs.Sub(web.Assets, "dist")
	if err != nil {
		return err
	}
	workbench.Version = version
	workbench.Autostart = func() string { return autostartState(c) }
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return workbench.Run(ctx, manager, assets, c.listen, strings.TrimSpace(string(code)))
}

// running reads server.json and checks that the workbench answers.
func running(c config) (workbench.ServerInfo, bool) {
	info, err := workbench.ReadServerInfo(c.dataDir)
	if err != nil || !workbench.ProcessAlive(info.PID) {
		return info, false
	}
	client := http.Client{Timeout: 2 * time.Second}
	res, err := client.Get(localURL(info.Listen) + "/api/auth/status")
	if err != nil {
		return info, false
	}
	res.Body.Close()
	return info, res.StatusCode == 200
}

// localURL is where this machine reaches a workbench listening on listen.
func localURL(listen string) string {
	host, port, _ := net.SplitHostPort(listen)
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func start(c config) error {
	if info, ok := running(c); ok {
		fmt.Println("Already running.")
		printURLs(info.Listen)
		return nil
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	var exited <-chan error
	handled, err := startService(c, exe)
	if err != nil {
		return err
	}
	if !handled {
		if exited, err = startDetached(c, exe); err != nil {
			return err
		}
	}
	info, err := waitRunning(c, exited, 20*time.Second)
	if err != nil {
		return err
	}
	printURLs(info.Listen)
	fmt.Printf("Stop: %s stop\n", exe)
	return nil
}

// startDetached runs serve in the background, its output appended to
// <data dir>/workbench.log. exited reports if it ends.
func startDetached(c config, exe string) (<-chan error, error) {
	if err := os.MkdirAll(c.dataDir, 0o700); err != nil {
		return nil, err
	}
	logs, err := openLog(c.dataDir)
	if err != nil {
		return nil, err
	}
	defer logs.Close()
	args := []string{"serve", "--data-dir", c.dataDir, "--listen", c.listen, "--idle", c.idle.String()}
	if env := envPath(c); exists(env) {
		args = append(args, "--env-file", env)
	}
	cmd, err := startBackground(func() *exec.Cmd {
		cmd := exec.Command(backgroundExe(exe), args...)
		cmd.Stdout, cmd.Stderr = logs, logs
		return cmd
	})
	if err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	return exited, nil
}

// waitRunning waits until the workbench answers, or reports why it ended.
func waitRunning(c config, exited <-chan error, limit time.Duration) (workbench.ServerInfo, error) {
	deadline := time.Now().Add(limit)
	for {
		if info, ok := running(c); ok {
			return info, nil
		}
		select {
		case err := <-exited:
			return workbench.ServerInfo{}, fmt.Errorf("the workbench exited (%v):\n%s", err, logTail(c.dataDir))
		case <-time.After(200 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return workbench.ServerInfo{}, fmt.Errorf("the workbench did not answer within %s; see %s", limit, logPath(c.dataDir))
		}
	}
}

// stop ends the workbench: through the service manager when it runs the
// workbench, otherwise through the local shutdown route.
func stop(c config, verbose bool) error {
	handled, err := stopService(c)
	if handled || err != nil {
		if err == nil && verbose {
			fmt.Println("Stopped.")
		}
		return err
	}
	info, err := workbench.ReadServerInfo(c.dataDir)
	if err != nil || !workbench.ProcessAlive(info.PID) {
		if verbose {
			fmt.Println("Not running.")
		}
		return nil
	}
	req, _ := http.NewRequest("POST", localURL(info.Listen)+"/api/local/shutdown", nil)
	req.Header.Set("X-Workbench-Stop", info.Token)
	client := http.Client{Timeout: 5 * time.Second}
	if res, err := client.Do(req); err == nil {
		res.Body.Close()
	}
	deadline := time.Now().Add(20 * time.Second)
	for workbench.ProcessAlive(info.PID) {
		if time.Now().After(deadline) {
			if p, err := os.FindProcess(info.PID); err == nil {
				_ = p.Kill()
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if verbose {
		fmt.Println("Stopped.")
	}
	return nil
}

func status(c config) error {
	if info, ok := running(c); ok {
		fmt.Printf("Running   pid %d, version %s, engines as %s\n", info.PID, info.Version, map[string]string{"systemd": "systemd units", "direct": "child processes"}[info.Mode])
		printURLs(info.Listen)
	} else {
		fmt.Println("Not running.")
	}
	fmt.Printf("Autostart %s\n", autostartState(c))
	fmt.Printf("Data      %s\n", c.dataDir)
	return nil
}

func install(c config) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	// Change nothing where the login start cannot be set up.
	if err := canAutostart(exe); err != nil {
		return err
	}
	if err := stop(c, false); err != nil {
		return err
	}
	target, err := installBinary(exe)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.dataDir, 0o700); err != nil {
		return err
	}
	if err := writeEnv(envPath(c), c.env); err != nil {
		return err
	}
	exited, err := installAutostart(c, target)
	if err != nil {
		return err
	}
	info, err := waitRunning(c, exited, 20*time.Second)
	if err != nil {
		return err
	}
	fmt.Printf("Installed %s\nAutostart %s\n", target, autostartState(c))
	printURLs(info.Listen)
	if dir := filepath.Dir(target); !onPath(dir) {
		fmt.Printf("Note: %s is not on your PATH; add it to run agent-workbench by name.\n", dir)
	}
	return nil
}

func uninstall(c config) error {
	if err := stop(c, false); err != nil {
		return err
	}
	if err := uninstallAutostart(c); err != nil {
		return err
	}
	_ = os.Remove(envPath(c))
	fmt.Println("Autostart removed.")
	if c.purge {
		if err := os.RemoveAll(c.dataDir); err != nil {
			return err
		}
		fmt.Printf("Deleted %s\n", c.dataDir)
		target := filepath.Join(installDir(), binaryName)
		if exists(target) {
			if err := os.Remove(target); err != nil {
				fmt.Printf("Delete %s yourself: %v\n", target, err)
			} else {
				fmt.Printf("Deleted %s\n", target)
			}
		}
	}
	return nil
}

func doctor(c config) error {
	fmt.Printf("agent-workbench %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	fmt.Printf("data dir: %s\n", c.dataDir)
	for _, name := range []string{"claude", "codex"} {
		path, err := exec.LookPath(name)
		if err != nil {
			fmt.Printf("%s: not found on PATH\n", name)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		out, err := exec.CommandContext(ctx, path, "--version").Output()
		cancel()
		v := strings.TrimSpace(string(out))
		if err != nil {
			v = err.Error()
		}
		fmt.Printf("%s: %s (%s)\n", name, path, v)
	}
	fmt.Printf("autostart: %s\n", autostartState(c))
	return doctorPlatform()
}

func printURLs(listen string) {
	host, port, _ := net.SplitHostPort(listen)
	fmt.Printf("Open http://agent.localhost:%s\n", port)
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		lan := lanURLs(port)
		for _, u := range lan {
			fmt.Printf("LAN  %s\n", u)
		}
		if len(lan) > 0 {
			fmt.Println("     Other devices sign in with a pairing code or password; choose one in Settings (off by default).")
		}
	}
}

// lanURLs lists this machine's private IPv4 addresses for LAN access.
func lanURLs(port string) []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.To4() == nil || !n.IP.IsPrivate() {
			continue
		}
		out = append(out, "http://"+net.JoinHostPort(n.IP.String(), port))
	}
	return out
}

func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// installBinary copies exe (and on Windows its windowless twin) into the
// user's program folder, unless a package manager put it in place.
func installBinary(exe string) (string, error) {
	if packaged(exe) {
		return exe, nil
	}
	dir := installDir()
	target := filepath.Join(dir, binaryName)
	if same(exe, target) {
		return target, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := copyFile(exe, target); err != nil {
		return "", err
	}
	for _, extra := range companions {
		from := filepath.Join(filepath.Dir(exe), extra)
		if exists(from) {
			if err := copyFile(from, filepath.Join(dir, extra)); err != nil {
				return "", err
			}
		}
	}
	return target, nil
}

// packaged is whether a package manager installed exe (deb, rpm, Homebrew).
func packaged(exe string) bool {
	return strings.HasPrefix(exe, "/usr/") || strings.HasPrefix(exe, "/opt/")
}

func same(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	return err == nil && os.SameFile(ia, ib)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := to + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, to)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func onPath(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if same(p, dir) {
			return true
		}
	}
	return false
}

// The environment install hands to the service, which does not start from
// a shell: where the agents are, and how to reach the network. Secrets are
// left out unless named with --env.
var serviceEnv = []string{"PATH", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy", "CODEX_HOME", "CLAUDE_CONFIG_DIR"}

func envPath(c config) string { return filepath.Join(c.dataDir, "service-env.json") }

func writeEnv(path string, extra []string) error {
	env := map[string]string{}
	for _, name := range append(append([]string{}, serviceEnv...), extra...) {
		if v, ok := os.LookupEnv(name); ok {
			env[name] = v
		}
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func loadEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	env := map[string]string{}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for k, v := range env {
		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}
	return nil
}

func logPath(dir string) string { return filepath.Join(dir, "workbench.log") }

// openLog appends to the log, keeping one previous file past 10 MB.
func openLog(dir string) (*os.File, error) {
	path := logPath(dir)
	if info, err := os.Stat(path); err == nil && info.Size() > 10<<20 {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

func logTail(dir string) string {
	f, err := os.Open(logPath(dir))
	if err != nil {
		return ""
	}
	defer f.Close()
	var lines []string
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		lines = append(lines, scan.Text())
		if len(lines) > 20 {
			lines = lines[1:]
		}
	}
	return strings.Join(lines, "\n")
}
