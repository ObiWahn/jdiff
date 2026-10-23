package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/source"
)

// movedBlock is long enough to count as moved code.
var movedBlock = []string{"func helper(x int) int {", "	return x * 2", "}"}

// renderedLine returns the screen line of m showing line of side s.
func renderedLine(t *testing.T, m *Model, s side, line int) string {
	t.Helper()
	for _, sl := range m.screen {
		if m.lineAt(s, sl.v) == line {
			return m.screenLine(sl, false)
		}
	}
	t.Fatalf("no screen line shows %v line %d", s, line)
	return ""
}

func ansiColors(t *testing.T) {
	t.Helper()
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	lipgloss.SetColorProfile(termenv.ANSI)
}

func TestMovedLinesMarked(t *testing.T) {
	ansiColors(t)
	stay := []string{"stay one", "stay two", "stay three", "stay four"}
	a := append(append(append([]string{}, movedBlock...), stay...), "old line")
	b := append(append(append([]string{}, stay...), movedBlock...), "new line")
	m := newTestModel(a, b)
	m.cfg.Settings.CollapseUnchanged = false
	m.realign()
	from, to := renderedLine(t, m, left, 0), renderedLine(t, m, right, 4)
	if !strings.Contains(from, "\x1b[35m-") || !strings.Contains(from, "\x1b[35mfunc\x1b") {
		t.Errorf("moved-away line %q, want marker and text magenta", from)
	}
	if !strings.Contains(to, "\x1b[36m+") || !strings.Contains(to, "\x1b[36mfunc\x1b") {
		t.Errorf("moved-here line %q, want marker and text cyan", to)
	}
	// The last line is a change in place: red and green as before.
	if row := renderedLine(t, m, left, 7); strings.Contains(row, "\x1b[35m") || !strings.Contains(row, "\x1b[31m") {
		t.Errorf("changed line %q, want red and not magenta", row)
	}
}

func TestAnchorEndsMove(t *testing.T) {
	stay := []string{"stay one", "stay two", "stay three", "stay four"}
	m := newTestModel(append(append([]string{}, movedBlock...), stay...), append(append([]string{}, stay...), movedBlock...))
	if _, ok := m.movedTo(left, 0); !ok {
		t.Fatal("block not found as moved")
	}
	// Anchoring the block to its new place lines it up: it is no longer a move.
	m.addAnchor(diff.Anchor{Left: 0, Right: 4}, right)
	if _, ok := m.movedTo(left, 0); ok {
		t.Error("anchored line still marked as moved")
	}
}

func TestMovedAcrossFilesInApp(t *testing.T) {
	ansiColors(t)
	a := NewApp(config.Default(), []source.File{
		modified("a.go", append([]string{"package a"}, movedBlock...), []string{"package a"}),
		modified("b.go", []string{"package b"}, append([]string{"package b", ""}, movedBlock...)),
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	pressApp(a, "j", "enter") // b.go
	if to := renderedLine(t, a.open.view, right, 2); !strings.Contains(to, "\x1b[36mfunc\x1b") {
		t.Errorf("b.go: line moved from a.go %q, want it cyan", to)
	}
	if r, ok := a.files[1].diff.movedTo(right, 2); !ok || r != (diff.Ref{File: 0, Line: 1}) {
		t.Errorf("b.go line 3 came from %v (%v), want a.go line 2", r, ok)
	}
}

func TestReloadUpdatesMoves(t *testing.T) {
	stay := []string{"stay one", "stay two", "stay three", "stay four"}
	old := append(append([]string{}, movedBlock...), stay...)
	m, dir := newWatchedModel(t, old, old)
	if _, ok := m.movedTo(left, 0); ok {
		t.Fatal("identical files have moved code")
	}
	// One more line, so the stamp changes for sure (see refreshFile).
	writeLines(t, filepath.Join(dir, "new.txt"), append(append(append([]string{}, stay...), movedBlock...), "end"))
	m.Update(watchTickMsg{})
	if r, ok := m.movedTo(left, 0); !ok || r.Line != 4 {
		t.Errorf("after the reload: line 1 moved to %v (%v), want line 5", r, ok)
	}
}
