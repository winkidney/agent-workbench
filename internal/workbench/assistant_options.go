package workbench

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// An assistant's own defaults for the sessions it starts (ADR 0017): a
// table per agent in its assistant.toml, so switching agent keeps both.
//
//	[options.claude]
//	model = "opus"
//	effort = "high"
//
// What a table leaves out comes from Settings → Agents; a request's own
// choice goes on top.

// AgentOptions is one agent's table.
type AgentOptions struct {
	Model       string `toml:"model" json:"model,omitempty"`
	Effort      string `toml:"effort" json:"effort,omitempty"`
	Mode        string `toml:"mode" json:"mode,omitempty"`
	Permissions string `toml:"permissions" json:"permissions,omitempty"`
}

func (o AgentOptions) options() Options {
	return Options{Model: o.Model, Effort: o.Effort, Mode: o.Mode, Permissions: o.Permissions}
}

func (o AgentOptions) empty() bool { return o == AgentOptions{} }

// optionKeys are the table's keys in the order they are written.
func (o AgentOptions) pairs() [][2]string {
	return [][2]string{{"model", o.Model}, {"effort", o.Effort}, {"mode", o.Mode}, {"permissions", o.Permissions}}
}

func checkAgentOptions(all map[string]AgentOptions) error {
	for agent, o := range all {
		if agent != "claude" && agent != "codex" {
			return fmt.Errorf("options.%s: only [options.claude] and [options.codex]", agent)
		}
		for _, kv := range o.pairs() {
			if strings.ContainsAny(kv[1], "\n\r\"\\") || len(kv[1]) > 100 {
				return fmt.Errorf("options.%s.%s: one short value", agent, kv[0])
			}
		}
	}
	return nil
}

// assistantOptions are the settings a new session of the assistant starts
// with: the saved defaults for its agent, then its own table.
func (m *Manager) assistantOptions(a Assistant) Options {
	return mergeOptions(m.newSessionDefaults(a.Agent), a.file.Options[a.Agent].options())
}

var tableHeader = regexp.MustCompile(`(?m)^[ \t]*\[`)

// setOptionsTable writes [options.<agent>] with o's non-empty keys, in
// place of the table there was (its comments go with it), or before the
// first table and the comment lines above it; an empty o removes it.
func setOptionsTable(text, agent string, o AgentOptions) string {
	block := ""
	if !o.empty() {
		block = "[options." + agent + "]\n"
		for _, kv := range o.pairs() {
			if kv[1] != "" {
				block += kv[0] + " = " + strconv.Quote(kv[1]) + "\n"
			}
		}
		block += "\n"
	}
	header := regexp.MustCompile(`(?m)^[ \t]*\[[ \t]*options\.` + agent + `[ \t]*\][^\n]*(\n|$)`)
	if loc := header.FindStringIndex(text); loc != nil {
		end := len(text)
		if next := tableHeader.FindStringIndex(text[loc[1]:]); next != nil {
			end = loc[1] + next[0]
		}
		return text[:loc[0]] + block + text[end:]
	}
	if block == "" {
		return text
	}
	// After the other agent's table, so they stay together.
	if loc := regexp.MustCompile(`(?m)^[ \t]*\[[ \t]*options\.[a-z]+[ \t]*\][^\n]*(\n|$)`).FindStringIndex(text); loc != nil {
		end := len(text)
		if next := tableHeader.FindStringIndex(text[loc[1]:]); next != nil {
			end = loc[1] + next[0]
		}
		body := strings.TrimRight(text[:end], "\n") + "\n\n"
		if end == len(text) {
			return body + strings.TrimRight(block, "\n") + "\n"
		}
		return body + block + text[end:]
	}
	first := tableHeader.FindStringIndex(text)
	if first == nil {
		return strings.TrimRight(text, "\n") + "\n\n" + strings.TrimRight(block, "\n") + "\n"
	}
	// Step back over the comment lines that introduce the first table.
	at := first[0]
	for at > 0 {
		prev := strings.LastIndex(text[:at-1], "\n") + 1
		if !strings.HasPrefix(strings.TrimSpace(text[prev:at-1]), "#") {
			break
		}
		at = prev
	}
	return text[:at] + block + text[at:]
}
