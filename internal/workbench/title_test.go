package workbench

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNativeTitleUnicodeAndCommands(t *testing.T) {
	text := "<command-name>/plan</command-name><command-message>plan</command-message><command-args>我有一个想法：Claude 已休眠 😀</command-args>"
	if got := sessionTitle(text); got != "我有一个想法：Claude 已休眠 😀" {
		t.Fatal(got)
	}
	if got := sessionTitle("<command-name>/plan</command-name>"); got != "/plan" {
		t.Fatal(got)
	}
	if !utf8.ValidString(safeText(strings.Repeat("中😀", 100), 120)) {
		t.Fatal("invalid UTF-8")
	}
	if !utf8.ValidString(sessionTitle(strings.Repeat("中😀", 100))) {
		t.Fatal("invalid title")
	}
	if sessionTitle("<system-reminder>internal</system-reminder>") != "" {
		t.Fatal("internal wrapper retained")
	}
}
