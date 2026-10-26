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
	m.addAnchor(diff.Anchor{Left: 0, Right: 4}, right, shape{})
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

// newMovedModel returns a view of a file whose first three lines moved
// below 40 lines that stay, more than a screen, followed by 30 lines and a
// last line that changed in place.
func newMovedModel() *Model {
	stay, tail := numbered("stay line ", 40), numbered("tail line ", 30)
	a := append(append(append(append([]string{}, movedBlock...), stay...), tail...), "old line")
	b := append(append(append(append([]string{}, stay...), movedBlock...), tail...), "new line")
	m := newTestModel(a, b)
	m.cfg.Settings.CollapseUnchanged = false
	m.realign()
	return m
}

func TestJumpWithinFile(t *testing.T) {
	for _, layout := range []string{"side by side", "normal"} {
		m := newMovedModel()
		if layout == "normal" {
			press(m, "s")
		}
		moveTo(t, m, left, 1) // "return x * 2", moved down
		row := m.pos() - m.top
		press(m, "o")
		if m.lineAt(right, m.cursor) != 41 || m.active != right || m.msg != "moved from new:2" {
			t.Errorf("%s: o on a moved-away line: right line %d, active %v, msg %q; want 41, right, moved from new:2",
				layout, m.lineAt(right, m.cursor), m.active, m.msg)
		}
		if m.pos()-m.top != row {
			t.Errorf("%s: screen row %d, want %d kept", layout, m.pos()-m.top, row)
		}
		if layout == "normal" && m.sub != 1 && m.rows[m.view[m.cursor].row].Kind == diff.Change {
			t.Errorf("normal: cursor on the removed line of the row, want the added one")
		}
		press(m, "o")
		if m.lineAt(left, m.cursor) != 1 || m.active != left || m.msg != "moved to new:42" {
			t.Errorf("%s: o back: left line %d, active %v, msg %q; want 1, left, moved to new:42",
				layout, m.lineAt(left, m.cursor), m.active, m.msg)
		}
	}
}

func TestJumpFromEitherPane(t *testing.T) {
	m := newMovedModel()
	moveTo(t, m, left, 1) // "return x * 2", moved down; nothing on the right
	press(m, "tab")       // the right pane, empty in this row
	press(m, "o")
	if m.lineAt(right, m.cursor) != 41 || m.active != right || m.msg != "moved from new:2" {
		t.Errorf("o from the empty pane: right line %d, active %v, msg %q; want 41, right, moved from new:2",
			m.lineAt(right, m.cursor), m.active, m.msg)
	}
	press(m, "tab", "o") // the left pane, empty in the added line's row
	if m.lineAt(left, m.cursor) != 1 || m.active != left || m.msg != "moved to new:42" {
		t.Errorf("o back from the empty pane: left line %d, active %v, msg %q; want 1, left", m.lineAt(left, m.cursor), m.active, m.msg)
	}
}

func TestFlowJumpFromEitherPane(t *testing.T) {
	a := newMovedApp()
	pressApp(a, "f", "j", "j", "tab", "o") // a.go line 2, from the empty right pane
	if _, file, line := a.flow.position(); file != 1 || a.files[1].diff.lineAt(right, line) != 3 || a.flow.msg != "moved from a.go:3" {
		t.Errorf("o in the flow from the empty pane: file %d, right line %d, msg %q", file, a.files[1].diff.lineAt(right, line), a.flow.msg)
	}
}

func TestJumpNotMoved(t *testing.T) {
	m := newMovedModel()
	moveTo(t, m, left, 73) // the line changed in place
	cursor := m.cursor
	press(m, "o")
	if m.cursor != cursor || m.msg != "not moved" {
		t.Errorf("o on a line changed in place: cursor %d (was %d), msg %q", m.cursor, cursor, m.msg)
	}
}

// newMovedApp returns the file selector of a.go, whose block moved to b.go.
func newMovedApp() *App {
	a := NewApp(config.Default(), []source.File{
		modified("a.go", append([]string{"package a"}, movedBlock...), []string{"package a"}),
		modified("b.go", []string{"package b"}, append([]string{"package b", ""}, movedBlock...)),
	})
	a.cfg.Settings.CollapseUnchanged = false
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	return a
}

func TestJumpToAnotherFile(t *testing.T) {
	a := newMovedApp()
	pressApp(a, "enter") // a.go
	moveTo(t, a.open.view, left, 2)
	pressApp(a, "o")
	m := a.open.view
	if a.open.Path != "b.go" || m.lineAt(right, m.cursor) != 3 || m.active != right || m.msg != "moved from a.go:3" {
		t.Fatalf("o into b.go: file %s, right line %d, active %v, msg %q", a.open.Path, m.lineAt(right, m.cursor), m.active, m.msg)
	}
	pressApp(a, "o")
	if m := a.open.view; a.open.Path != "a.go" || m.lineAt(left, m.cursor) != 2 || m.msg != "moved to b.go:4" {
		t.Errorf("o back: file %s, left line %d, msg %q", a.open.Path, m.lineAt(left, m.cursor), m.msg)
	}
}

func TestFlowJumpToAnotherFile(t *testing.T) {
	a := newMovedApp()
	pressApp(a, "f", "j", "j") // a.go: line 1, line 2 on the left
	_, file, line := a.flow.position()
	if file != 0 || a.files[0].diff.lineAt(left, line) != 2 {
		t.Fatalf("flow cursor on file %d left line %d, want a.go line 2", file, a.files[0].diff.lineAt(left, line))
	}
	pressApp(a, "o")
	_, file, line = a.flow.position()
	if file != 1 || a.files[1].diff.lineAt(right, line) != 3 || a.flow.active != right || a.flow.msg != "moved from a.go:3" {
		t.Errorf("o in the flow: file %d, right line %d, active %v, msg %q",
			file, a.files[1].diff.lineAt(right, line), a.flow.active, a.flow.msg)
	}
}

func TestJumpTop(t *testing.T) {
	// A window of 10 lines over 100, showing lines 20 to 29.
	for _, tt := range []struct {
		name         string
		from, to     int
		top, wantTop int
	}{
		{"target on screen: no scroll", 22, 27, 20, 20},
		{"both fit, target below", 25, 33, 20, 24},
		{"both fit, target above", 22, 14, 20, 14},
		{"too far: screen row kept", 22, 60, 20, 58},
		{"too far, near the end", 22, 99, 20, 90},
	} {
		if got := jumpTop(tt.from, tt.to, tt.top, 10, 100); got != tt.wantTop {
			t.Errorf("%s: top %d, want %d", tt.name, got, tt.wantTop)
		}
	}
}

func TestJumpOnScreenDoesNotScroll(t *testing.T) {
	// The block moved down by 5 lines: both ends on one screen.
	stay := numbered("stay line ", 5)
	a := append(append(append([]string{}, numbered("top line ", 10)...), movedBlock...), stay...)
	b := append(append(append([]string{}, numbered("top line ", 10)...), stay...), movedBlock...)
	m := newTestModel(a, b)
	m.cfg.Settings.CollapseUnchanged = false
	m.realign()
	moveTo(t, m, left, 11)
	m.top = 3
	m.scrollToCursor()
	top := m.top
	press(m, "o")
	if m.lineAt(right, m.cursor) != 16 || m.top != top {
		t.Errorf("o: right line %d, top %d; want 16 and the screen as it was, %d", m.lineAt(right, m.cursor), m.top, top)
	}
	press(m, "o")
	if m.lineAt(left, m.cursor) != 11 || m.top != top {
		t.Errorf("o back: left line %d, top %d; want 11, %d", m.lineAt(left, m.cursor), m.top, top)
	}

	// A mark on screen: the cursor goes there, the screen stays.
	moveTo(t, m, right, 3)
	press(m, "m", "a")
	moveTo(t, m, right, 9)
	top = m.top
	press(m, "'", "a")
	if m.lineAt(right, m.cursor) != 3 || m.top != top {
		t.Errorf("' a: right line %d, top %d; want 3, %d", m.lineAt(right, m.cursor), m.top, top)
	}
}

func TestFlowJumpKeepsBothEndsInView(t *testing.T) {
	// As newMovedApp, with lines after the block in b.go, so that the window
	// can scroll past both ends.
	a := NewApp(config.Default(), []source.File{
		modified("a.go", append([]string{"package a"}, movedBlock...), []string{"package a"}),
		modified("b.go", []string{"package b"}, append(append([]string{"package b", ""}, movedBlock...), numbered("more ", 20)...)),
	})
	a.cfg.Settings.CollapseUnchanged = false
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 12}) // 10 rows of a longer flow
	pressApp(a, "f", "j", "j")                         // a.go line 2, moved to b.go line 3
	inView := func(i int) bool { return i >= a.flow.top && i < a.flow.top+a.flow.bodyHeight() }
	from := a.flow.cursor
	pressApp(a, "o")
	if _, file, _ := a.flow.position(); file != 1 || !inView(from) || !inView(a.flow.cursor) {
		t.Errorf("o in the flow: file %d, rows %d and %d, window from %d; want b.go with both in view",
			file, from, a.flow.cursor, a.flow.top)
	}
	// A mark there, back with o, and to the mark: again both in view.
	to := a.flow.cursor
	pressApp(a, "m", "k", "o", "'", "k")
	if a.flow.cursor != to || !inView(from) || !inView(to) {
		t.Errorf("' k in the flow: cursor %d, want %d, with row %d in view too (window from %d)", a.flow.cursor, to, from, a.flow.top)
	}
}
