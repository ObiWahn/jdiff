package main

import (
	"strings"
	"testing"
)

func TestWithoutBadges(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"none", "# t\n\nx0\n", "# t\n\nx0\n"},
		{"under the title", "# t\n[![a](b)](c)\n\nx0\n", "# t\n\nx0\n"},
		{"several", "[![a](b)](c)\n[![d](e)](f)\nx0\n", "x0\n"},
		{"CRLF", "# t\r\n[![a](b)](c)\r\n\r\nx0\r\n", "# t\r\n\r\nx0\r\n"},
		{"last, no newline", "x0\n[![a](b)](c)", "x0\n"},
		{"inside a line stays", "x0 [![a](b)](c)\n", "x0 [![a](b)](c)\n"},
		{"plain image stays", "![a](b)\n", "![a](b)\n"},
		{"empty", "", ""},
	} {
		if got := withoutBadges(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// What --readme prints: the README has its badge, the output neither that nor
// a gap in its place.
func TestReadmeBadge(t *testing.T) {
	if !strings.Contains(readme, "\n[![coverage](") {
		t.Error("the README has no coverage badge")
	}
	// git may check out the README with CRLF on Windows.
	got := strings.ReplaceAll(withoutBadges(readme), "\r\n", "\n")
	if strings.Contains(got, "[![") {
		t.Error("--readme prints a badge")
	}
	if !strings.HasPrefix(got, "# jdiff\n\nAn interactive") {
		t.Errorf("--readme starts with %.40q", got)
	}
}
