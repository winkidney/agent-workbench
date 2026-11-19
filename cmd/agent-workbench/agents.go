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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/winkidney/agent-workbench/internal/workbench"
)

// The agents' commands (ADR 0012): routine add|list|rm and assistant send,
// for an agent in a workbench session (or the user in a terminal). They
// talk to the running workbench over /api/local/* with the token in its
// data directory.

const agentUsage = `usage:
  agent-workbench routine add (--daily HH:MM [--days mon,fri] | --every 2h |
                               --at "YYYY-MM-DD HH:MM" | --in 30m)
                              --message TEXT [--to this|ASSISTANT|SESSION] [--name NAME]
  agent-workbench routine list
  agent-workbench routine rm ID
  agent-workbench assistant send ASSISTANT TEXT

--to this (the default) is the session the command runs in; in a
continuous assistant's folder it is that assistant, whose topic changes
daily. An agent's routines send messages only, at most every 15 minutes,
and at most 20 per session.
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
	if group == "routine" && verb == "add" {
		flags.StringVar(&req.Daily, "daily", "", "every day at HH:MM")
		flags.StringVar(&days, "days", "", "with --daily: only these days (mon,tue,…)")
		flags.StringVar(&req.Every, "every", "", "every 15m, 2h, 1d, …")
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
