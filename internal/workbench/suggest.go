package workbench

import "strings"

// Prompt suggestions: when a turn ends, the composer offers what the user
// would likely type next, in grey, as Claude Code's own prompt does; Tab
// takes it.
//
// Claude Code makes them itself. With --prompt-suggestions it writes a
// {"type":"prompt_suggestion","suggestion":...} line some seconds after a
// turn's result, from one more request on the cached conversation with tools
// off. It decides when not to (early in a conversation, in plan mode, while
// a request waits, when rate limited or the cache is cold, when unsure), and
// a user's promptSuggestionEnabled: false setting turns it off.
// set_prompt_suggestions_paused stops and restarts it in a running CLI.
//
// Codex 0.167 has none (thread/prediction/request is not implemented yet);
// one would set the same field, and the page would show it the same way.

// Preferences are the workbench's own settings, for every session.
type Preferences struct {
	// Suggestions: offer a next prompt when a turn ends (on unless turned
	// off).
	Suggestions bool `json:"suggestions"`
}

func (m *Manager) Preferences() Preferences {
	p := Preferences{Suggestions: true}
	m.store.readSetting("preferences", &p)
	return p
}

// SetPreferences saves the preferences and applies them to running engines.
func (m *Manager) SetPreferences(p Preferences) error {
	if err := m.store.setting("preferences", p); err != nil {
		return err
	}
	m.mu.Lock()
	var claude []string
	for _, r := range m.sessions {
		if r.proc == nil {
			continue
		}
		if !p.Suggestions {
			r.suggestion = ""
		}
		if r.session.Agent == "claude" && r.ready {
			claude = append(claude, r.session.ID)
		}
	}
	m.mu.Unlock()
	for _, id := range claude {
		go m.pauseSuggestions(id, !p.Suggestions)
	}
	return nil
}

// pauseSuggestions stops (or restarts) a Claude engine's suggestions. A CLI
// without the request keeps making them; the workbench then drops them.
func (m *Manager) pauseSuggestions(id string, paused bool) {
	_, _ = m.rpc(id, "set_prompt_suggestions_paused", map[string]any{"paused": paused})
}

// suggested keeps the suggestion a Claude engine made after its turn
// (m.mu held). One that arrives once the next turn or a request has begun
// is stale.
func (m *Manager) suggested(r *runtime, text string) {
	text = strings.TrimSpace(text)
	if r.turn || len(r.pending) > 0 || strings.ContainsAny(text, "\r\n") || !m.Preferences().Suggestions {
		return
	}
	r.suggestion = text
}

// suggestionOf is what the composer offers now (m.mu held): nothing while
// the engine is asleep, a turn runs or a request waits.
func suggestionOf(r *runtime) string {
	if r.proc == nil || r.turn || len(r.pending) > 0 || r.session.Attention != "" {
		return ""
	}
	return r.suggestion
}
