package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/gitsrc"
	"jdiff/internal/settings"
	"jdiff/internal/source"
	"jdiff/internal/theme"
)

func modified(path string, old, new []string) source.File {
	return source.File{Path: path, Old: old, New: new, InOld: true, InNew: true}
}

// newTestLog returns a log of four commits, newest first:
//
//	c3: changes a.go and b.go
//	c2: changes only a.go
//	c1: changes only c.go
//	c0: cannot be loaded
func newTestLog() (*Log, map[string]int) {
	loads := map[string]int{}
	files := map[string][]source.File{
		"c3": {modified("a.go", []string{"1"}, []string{"2"}), modified("b.go", []string{"x"}, []string{"y"})},
		"c2": {modified("a.go", []string{"0"}, []string{"1"})},
		"c1": {modified("c.go", []string{"p"}, []string{"q", "r"})},
	}
	load := func(c gitsrc.Commit) ([]source.File, error) {
		loads[c.Hash]++
		if f, ok := files[c.Hash]; ok {
			return f, nil
		}
		return nil, errors.New("cannot load " + c.Hash)
	}
	var commits []gitsrc.Commit
	for _, h := range []string{"c3", "c2", "c1", "c0"} {
		commits = append(commits, gitsrc.Commit{Hash: h, Short: h, Date: "2026-10-22", Author: "T",
			Subject: "subject " + h, Added: 12, Removed: 3})
	}
	l := NewLog(theme.Default(), settings.Default(), commits, load)
	l.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	return l, loads
}

func pressLog(l *Log, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		_, cmd := l.Update(msg)
		for cmd != nil {
			_, cmd = l.Update(cmd())
		}
	}
}

// shownFile returns the path of the diff shown, or "" for a list.
func shownFile(l *Log) string {
	if l.open == nil || l.open.app.open == nil {
		return ""
	}
	return l.open.app.open.Path
}

func TestLogListShowsCommits(t *testing.T) {
	l, loads := newTestLog()
	view := ansi.Strip(l.View())
	for _, want := range []string{"4 commits", "c3", "2026-10-22", "+12 -3", "subject c1"} {
		if !strings.Contains(view, want) {
			t.Errorf("log view lacks %q:\n%s", want, view)
		}
	}
	if len(loads) != 0 {
		t.Errorf("commits loaded before opening: %v", loads)
	}
}

func TestLogLevelsAndBackspace(t *testing.T) {
	l, loads := newTestLog()

	// A commit with several files shows its file list.
	pressLog(l, "enter")
	if l.open == nil || shownFile(l) != "" || len(l.open.app.files) != 2 {
		t.Fatalf("enter on c3: open %v, shown %q", l.open, shownFile(l))
	}
	pressLog(l, "enter")
	if shownFile(l) != "a.go" {
		t.Fatalf("enter in the file list: shown %q, want a.go", shownFile(l))
	}
	pressLog(l, "backspace")
	if l.open == nil || shownFile(l) != "" {
		t.Fatalf("backspace from the diff should show the file list")
	}
	pressLog(l, "backspace")
	if l.open != nil {
		t.Fatalf("backspace from the file list should show the commits")
	}

	// A single-file commit opens its file directly, and backspace goes
	// straight back to the commits.
	pressLog(l, "j", "enter")
	if shownFile(l) != "a.go" {
		t.Fatalf("enter on c2: shown %q, want a.go directly", shownFile(l))
	}
	pressLog(l, "backspace")
	if l.open != nil {
		t.Fatalf("backspace from a single-file commit should show the commits")
	}

	// Reopening reuses the loaded files.
	pressLog(l, "k", "enter")
	if loads["c3"] != 1 {
		t.Errorf("c3 loaded %d times, want once", loads["c3"])
	}
}

func TestLogNextPrevCommit(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "enter", "enter") // c3, a.go

	// n keeps a.go, which c2 changed as well.
	pressLog(l, "n")
	if l.cursor != 1 || shownFile(l) != "a.go" {
		t.Fatalf("n: commit %d shown %q, want c2 a.go", l.cursor, shownFile(l))
	}
	// c1 did not change a.go, but it has a single file.
	pressLog(l, "n")
	if l.cursor != 2 || shownFile(l) != "c.go" {
		t.Fatalf("n: commit %d shown %q, want c1 c.go", l.cursor, shownFile(l))
	}
	// c0 cannot be loaded: stay and show the error.
	pressLog(l, "n")
	if l.cursor != 2 || !strings.Contains(l.open.app.open.view.msg, "cannot load c0") {
		t.Errorf("n onto a broken commit: commit %d msg %q", l.cursor, l.open.app.open.view.msg)
	}
	// Back via c2 (single file a.go) to c3, which changed a.go as well.
	pressLog(l, "p", "p")
	if l.cursor != 0 || shownFile(l) != "a.go" {
		t.Errorf("p p: commit %d shown %q, want c3 a.go", l.cursor, shownFile(l))
	}
	pressLog(l, "p")
	if !strings.Contains(l.open.app.open.view.msg, "newest commit") {
		t.Errorf("p on the newest commit: msg %q", l.open.app.open.view.msg)
	}
	// n/p also work from the file list.
	pressLog(l, "backspace", "n")
	if l.cursor != 1 || shownFile(l) != "a.go" {
		t.Errorf("n from the file list: commit %d shown %q", l.cursor, shownFile(l))
	}
	// The open file a.go is kept when going back to c3.
	pressLog(l, "p")
	if l.cursor != 0 || shownFile(l) != "a.go" {
		t.Errorf("p back to c3 with a.go open: commit %d shown %q", l.cursor, shownFile(l))
	}
}

func TestLogFileKeysInCommit(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "enter", "enter", "N")
	if shownFile(l) != "b.go" {
		t.Errorf("N in c3: shown %q, want b.go", shownFile(l))
	}
	pressLog(l, "P")
	if shownFile(l) != "a.go" {
		t.Errorf("P in c3: shown %q, want a.go", shownFile(l))
	}
}
