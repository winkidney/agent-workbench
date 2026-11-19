package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The language assistants write to the user in (ADR 0014): chosen when
// setting up, changed in Settings, named by a rule in the root AGENTS.md.

// Languages the welcome and set-up messages are written in.
var languageNames = map[string]string{
	"en":      "English",
	"zh-Hans": "Simplified Chinese (简体中文)",
	"zh-Hant": "Traditional Chinese (繁體中文)",
}

const defaultLanguage = "zh-Hans"

func validLanguage(lang string) bool { _, ok := languageNames[lang]; return ok }

// language is the language set up chose; zh-Hans until one is chosen.
func (m *Manager) language() string {
	var v assistantsSetting
	m.store.readSetting("assistants", &v)
	if validLanguage(v.Language) {
		return v.Language
	}
	return defaultLanguage
}

// languageLine is the root AGENTS.md rule naming the language.
func languageLine(lang string) string {
	return "- **Language.** Write to the user in " + languageNames[lang] + " unless they write in another language."
}

var languageRule = regexp.MustCompile(`(?m)^- \*\*Language\.\*\* Write to the user in .* unless they write in another language\.[ \t]*\r?$`)

// SetLanguage changes the language and the root rule that names it: the
// line is replaced, or added under the rules' heading when it is missing.
func (m *Manager) SetLanguage(lang string) error {
	if !validLanguage(lang) {
		return errors.New("language: en, zh-Hans or zh-Hant")
	}
	var v assistantsSetting
	m.store.readSetting("assistants", &v)
	v.Language = lang
	if err := m.store.setting("assistants", v); err != nil {
		return err
	}
	if v.Root == "" {
		return nil
	}
	p := filepath.Join(v.Root, "AGENTS.md")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	text := string(data)
	switch {
	case languageRule.MatchString(text):
		text = languageRule.ReplaceAllLiteralString(text, languageLine(lang))
	case strings.Contains(text, "## Rules for every assistant\n\n"):
		text = strings.Replace(text, "## Rules for every assistant\n\n", "## Rules for every assistant\n\n"+languageLine(lang)+"\n", 1)
	default:
		return nil
	}
	if text == string(data) {
		return nil
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		return err
	}
	commitAll(v.Root, "Write to the user in "+languageNames[lang])
	return nil
}
