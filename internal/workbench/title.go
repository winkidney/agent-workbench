package workbench

import (
	"strings"
	"unicode/utf8"
)

func safeText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
func sessionTitle(text string) string {
	text = strings.TrimSpace(text)
	tag := func(name string) string {
		start := "<" + name + ">"
		end := "</" + name + ">"
		i := strings.Index(text, start)
		if i < 0 {
			return ""
		}
		tail := text[i+len(start):]
		j := strings.Index(tail, end)
		if j < 0 {
			return ""
		}
		return tail[:j]
	}
	if strings.HasPrefix(text, "<command-name>") {
		args := tag("command-args")
		if strings.TrimSpace(args) == "" {
			args = tag("command-name")
		}
		text = args
	}
	for _, prefix := range []string{"<local-command-stdout>", "<local-command-caveat>", "<system-reminder>"} {
		if strings.HasPrefix(text, prefix) {
			return ""
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	r := []rune(text)
	if len(r) > 120 {
		text = string(r[:120]) + "…"
	}
	return text
}
