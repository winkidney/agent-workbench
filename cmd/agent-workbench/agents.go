package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/winkidney/agent-workbench/internal/workbench"
)

// The agents' commands (ADR 0012): routine add|list|rm, assistant send and
// session new (ADR 0019), for an agent in a workbench session (or the user in a terminal). They
// talk to the running workbench over /api/local/* with the token in its
// data directory.

const agentUsage = `usage:
  agent-workbench routine add (--daily HH:MM [--days mon,fri] | --every 2h |
                               --at "YYYY-MM-DD HH:MM" | --in 30m | --cron "0 9 1 * *")
                              --message TEXT [--to this|ASSISTANT|SESSION] [--name NAME]
  agent-workbench routine list
  agent-workbench routine rm ID
  agent-workbench assistant send ASSISTANT TEXT
  agent-workbench assistant new --name NAME [--folder DIR] [--agent claude|codex]
                                [--sessions per-task|continuous]
  agent-workbench assistant catalog [--catalog URL]
  agent-workbench assistant install NAME [--folder DIR] [--agent claude|codex]
                                    [--catalog URL]
  agent-workbench assistant update NAME [--check]
  agent-workbench assistant reload
  agent-workbench assistant memory
  agent-workbench session new [--folder DIR] [--agent claude|codex] [--title T] TEXT

--to this (the default) is the session the command runs in; in a
continuous assistant's folder it is that assistant, whose topic changes
daily. An agent's routines send messages only, at most every 15 minutes,
and at most 20 per session. session new starts a session the user asked an
assistant for (the start-session skill); it works only in an assistant's
session, and --folder defaults to the current directory.
`

// localClient calls the running workbench's /api/local/*.
type localClient struct {
	base, token, caller string
}

func newLocalClient(dataDir string) (*localClient, error) {
	info, err := workbench.ReadServerInfo(dataDir)
	if err != nil || !workbench.ProcessAlive(info.PID) {
		return nil, fmt.Errorf("the workbench is not running (data directory %s)", dataDir)
	}
	token, err := os.ReadFile(filepath.Join(dataDir, "agent-token"))
	if err != nil {
		return nil, fmt.Errorf("the workbench has no agent token yet (an older version?): %w", err)
	}
	host, port, err := net.SplitHostPort(info.Listen)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return &localClient{base: "http://" + net.JoinHostPort(host, port) + "/api/local/", token: strings.TrimSpace(string(token)), caller: os.Getenv(workbench.EnvSession)}, nil
}

func (c *localClient) call(method, path string, body, out any) error {
	var in io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		in = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, in)
	if err != nil {
		return err
	}
	req.Header.Set("X-Workbench-Agent", c.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		return errors.New(strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// agentCommand runs "routine …" or "assistant …".
func agentCommand(group string, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(agentUsage)
		return nil
	}
	verb, args := args[0], args[1:]
	flags := flag.NewFlagSet(group+" "+verb, flag.ContinueOnError)
	flags.Usage = func() { fmt.Fprint(os.Stderr, agentUsage) }
	dataDir := os.Getenv(workbench.EnvDataDir)
	if dataDir == "" {
		dataDir = defaultDataDir()
	}
	flags.StringVar(&dataDir, "data-dir", dataDir, "the workbench's data directory")
	var req workbench.AgentRoutineRequest
	var days, in string
	var made struct{ name, dir, agent, sessions string }
	var started struct{ dir, agent, title string }
	if group == "session" && verb == "new" {
		flags.StringVar(&started.dir, "folder", "", "where it works (default: the current directory)")
		flags.StringVar(&started.agent, "agent", "", "claude or codex (default: the asking session's)")
		flags.StringVar(&started.title, "title", "", "its title (default: from the message)")
	}
	var fromCatalog struct {
		catalog, dir, agent string
		check               bool
	}
	if group == "assistant" && (verb == "catalog" || verb == "install") {
		flags.StringVar(&fromCatalog.catalog, "catalog", "", "the catalog's git address (default: the first in Settings)")
	}
	if group == "assistant" && verb == "install" {
		flags.StringVar(&fromCatalog.dir, "folder", "", "its folder (default: its folder in the catalog)")
		flags.StringVar(&fromCatalog.agent, "agent", "", "claude or codex (default: its author's)")
	}
	if group == "assistant" && verb == "update" {
		flags.BoolVar(&fromCatalog.check, "check", false, "only say what updating would do")
	}
	if group == "assistant" && verb == "new" {
		flags.StringVar(&made.name, "name", "", "its name")
		flags.StringVar(&made.dir, "folder", "", "its folder (default: from the name)")
		flags.StringVar(&made.agent, "agent", "", "claude or codex (default: the installed one)")
		flags.StringVar(&made.sessions, "sessions", "", "per-task (default) or continuous")
	}
	if group == "routine" && verb == "add" {
		flags.StringVar(&req.Daily, "daily", "", "every day at HH:MM")
		flags.StringVar(&days, "days", "", "with --daily: only these days (mon,tue,…)")
		flags.StringVar(&req.Every, "every", "", "every 15m, 2h, 1d, …")
		flags.StringVar(&req.Cron, "cron", "", `a cron expression, "0 9 1 * *" or "@weekly" (local time)`)
		flags.StringVar(&req.At, "at", "", `once, at "YYYY-MM-DD HH:MM" (local time)`)
		flags.StringVar(&in, "in", "", "once, after 30m, 2h, …")
		flags.StringVar(&req.Message, "message", "", "what to send")
		flags.StringVar(&req.To, "to", "this", "this, an assistant's folder, or a session id")
		flags.StringVar(&req.Name, "name", "", "a name for it")
	}
	// Flags may follow the arguments: "rm ID --data-dir …".
	var rest []string
	for {
		if err := flags.Parse(args); err != nil {
			return err
		}
		args = flags.Args()
		if len(args) == 0 {
			break
		}
		rest = append(rest, args[0])
		args = args[1:]
	}
	c, err := newLocalClient(dataDir)
	if err != nil {
		return err
	}
	switch group + " " + verb {
	case "routine add":
		if len(rest) > 0 {
			return fmt.Errorf("unexpected argument %q (quote the message: --message \"…\")", rest[0])
		}
		if in != "" {
			d, err := time.ParseDuration(in)
			if err != nil || d < time.Minute {
				return errors.New("--in: like 30m or 2h")
			}
			req.At = time.Now().Add(d).Format("2006-01-02 15:04")
		}
		if days != "" {
			req.Days = strings.Split(days, ",")
		}
		req.Caller = c.caller
		var v workbench.Routine
		if err := c.call("POST", "routines", req, &v); err != nil {
			return err
		}
		fmt.Printf("Added routine %s: %s, %s.\n", v.ID, v.Name, describe(v))
		return nil
	case "routine list":
		var list []workbench.Routine
		if err := c.call("GET", "routines", nil, &list); err != nil {
			return err
		}
		for _, v := range list {
			state := "on"
			if !v.Enabled {
				state = "off"
			}
			fmt.Printf("%s  %-3s  %s  (%s; by %s)\n", v.ID, state, v.Name, describe(v), v.CreatedBy)
		}
		return nil
	case "routine rm":
		if len(rest) != 1 {
			return errors.New("routine rm ID")
		}
		if err := c.call("DELETE", "routines/"+rest[0], nil, nil); err != nil {
			return err
		}
		fmt.Println("Removed.")
		return nil
	case "assistant new":
		if len(rest) > 0 {
			return fmt.Errorf("unexpected argument %q (quote the name: --name \"…\")", rest[0])
		}
		var out struct {
			Assistant workbench.Assistant `json:"assistant"`
		}
		if err := c.call("POST", "assistants", map[string]string{"name": made.name, "dir": made.dir, "agent": made.agent, "sessions": made.sessions}, &out); err != nil {
			return err
		}
		a := out.Assistant
		fmt.Printf("Made the assistant %s (%s, %s) in %s.\nIt is in the sidebar under Assistants. Write its procedure in AGENTS.md there, as the create-assistant skill says.\n", a.Name, a.Agent, a.Sessions, a.Path)
		return nil
	case "assistant memory":
		var out struct {
			Started bool `json:"started"`
		}
		if err := c.call("POST", "assistants/memory", map[string]string{"caller": c.caller}, &out); err != nil {
			return err
		}
		if out.Started {
			fmt.Println("Memory will be updated from this conversation once the current turn ends; the conversation shows \"Memory updated\" when it is done.")
		} else {
			fmt.Println("Nothing new to remember since the last update.")
		}
		return nil
	case "assistant catalog":
		var out struct {
			Catalog    string                   `json:"catalog"`
			Assistants []workbench.CatalogEntry `json:"assistants"`
		}
		if err := c.call("GET", "assistants/catalog?url="+url.QueryEscape(fromCatalog.catalog), nil, &out); err != nil {
			return err
		}
		fmt.Printf("Catalog %s:\n", out.Catalog)
		for _, e := range out.Assistants {
			agents := strings.Join(e.Agents, ", ")
			if agents == "" {
				agents = "claude, codex"
			}
			state := "not installed"
			if e.Installed != "" {
				state = "installed as " + e.Installed
			}
			fmt.Printf("%s  %s  (%s; %s) %s\n", e.Path, e.Name, agents, state, e.Description)
		}
		return nil
	case "assistant install":
		if len(rest) != 1 {
			return errors.New("assistant install NAME [--folder DIR] [--agent claude|codex]")
		}
		var a workbench.Assistant
		if err := c.call("POST", "assistants/install", map[string]string{"catalog": fromCatalog.catalog, "path": rest[0], "dir": fromCatalog.dir, "agent": fromCatalog.agent}, &a); err != nil {
			return err
		}
		fmt.Printf("Installed %s (%s) in %s. It needs setting up: open it in the sidebar and press Set up; its routines stay off until then.\n", a.Name, a.Agent, a.Path)
		return nil
	case "assistant update":
		if len(rest) != 1 {
			return errors.New("assistant update NAME [--check]")
		}
		var out workbench.CatalogUpdate
		if err := c.call("POST", "assistants/"+rest[0]+"/update", map[string]any{"apply": !fromCatalog.check}, &out); err != nil {
			return err
		}
		printUpdate(out, !fromCatalog.check)
		return nil
	case "assistant reload":
		if err := c.call("POST", "assistants/reload", nil, nil); err != nil {
			return err
		}
		fmt.Println("Reloaded: changes to assistant.toml files apply now.")
		return nil
	case "assistant send":
		if len(rest) < 2 {
			return errors.New(`assistant send ASSISTANT "message"`)
		}
		var s workbench.Session
		if err := c.call("POST", "assistants/"+rest[0]+"/send", map[string]string{"caller": c.caller, "text": strings.Join(rest[1:], " ")}, &s); err != nil {
			return err
		}
		fmt.Printf("Sent to %s (session %s).\n", rest[0], s.ID)
		return nil
	case "session new":
		if len(rest) == 0 {
			return errors.New(`session new [--folder DIR] "first message"`)
		}
		dir := started.dir
		if dir == "" {
			if dir, err = os.Getwd(); err != nil {
				return err
			}
		}
		if dir, err = filepath.Abs(dir); err != nil {
			return err
		}
		var s workbench.Session
		if err := c.call("POST", "sessions", map[string]string{"caller": c.caller, "dir": dir, "agent": started.agent, "title": started.title, "text": strings.Join(rest, " ")}, &s); err != nil {
			return err
		}
		fmt.Printf("Started %q in %s (session %s); it is in the sidebar under its project.\n", s.Title, s.Cwd, s.ID)
		return nil
	}
	return fmt.Errorf("unknown command %q\n%s", group+" "+verb, agentUsage)
}

// describe says when a routine runs and where its message goes.
func describe(v workbench.Routine) string {
	var when string
	switch v.When.Kind {
	case "once":
		at, _ := time.Parse(time.RFC3339, v.When.At)
		when = "once at " + at.Local().Format("2006-01-02 15:04")
	case "daily":
		when = "daily at " + v.When.Time
		if len(v.When.Days) > 0 && len(v.When.Days) < 7 {
			names := []string{}
			for _, d := range v.When.Days {
				names = append(names, time.Weekday(d).String()[:3])
			}
			when = strings.Join(names, ", ") + " at " + v.When.Time
		}
	case "interval":
		when = fmt.Sprintf("every %d minutes", v.When.Minutes)
	case "cron":
		when = "cron " + v.When.Cron
	}
	if t := v.Target; t != nil {
		switch t.Kind {
		case "assistant":
			when += " to the assistant " + t.Assistant
		case "session":
			when += " to session " + t.Session
		case "new":
			when += " to a new session in " + t.Cwd
		}
	}
	if v.Next != "" {
		if next, err := time.Parse(time.RFC3339, v.Next); err == nil {
			when += ", next " + next.Local().Format("2006-01-02 15:04")
		}
	}
	return when
}

// printUpdate says what updating an assistant from its catalog does, or did.
func printUpdate(u workbench.CatalogUpdate, applied bool) {
	if u.Commit == "" || len(u.Files) == 0 {
		fmt.Println("Up to date with its catalog.")
		return
	}
	verb := map[bool]string{true: "Updated to", false: "Its catalog has"}[applied]
	fmt.Printf("%s %s:\n", verb, u.Commit[:min(7, len(u.Commit))])
	for _, f := range u.Files {
		note := ""
		if f.Runs {
			note = "  (runs: read it)"
		}
		fmt.Printf("  %-10s %s%s\n", f.Result, f.Rel, note)
	}
	if n := u.Conflicts(); n > 0 {
		fmt.Printf("%d conflicting files kept as they are: choose in the assistant's settings (Update), or merge them with the user.\n", n)
	}
}
