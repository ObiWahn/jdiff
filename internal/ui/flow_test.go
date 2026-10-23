package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/source"
)

// newFlowApp returns a file selector with a.go (changes at lines 10 and 25
// of 30), the binary b.bin and c.go (one change).
func newFlowApp() *App {
	a, b := numbered("x", 30), numbered("x", 30)
	b[9], b[24] = "new9", "new24"
	app := NewApp(config.Default(), []source.File{
		modified("a.go", a, b),
		{Path: "b.bin", InOld: true, InNew: true, Binary: true},
		modified("c.go", []string{"same", "old"}, []string{"same", "new"}),
	})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return app
}

// flowAt reports whether the flow view is shown with its cursor on file
// and line (-1 for the file bar).
func flowAt(a *App, file, line int) bool {
	if !a.flowShown() {
		return false
	}
	_, f, l := a.flow.position()
	return f == file && l == line
}

func TestFlowToggleKeepsLine(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "enter", "]", "]")
	m := a.open.view
	line, screenRow := m.cursor, m.cursor-m.top

	pressApp(a, "f")
	if !flowAt(a, 0, line) || a.flow.cursor-a.flow.top != screenRow {
		_, f, l := a.flow.position()
		t.Fatalf("f: flow at file %d line %d screen row %d, want 0 %d %d",
			f, l, a.flow.cursor-a.flow.top, line, screenRow)
	}
	pressApp(a, "f")
	if a.flowShown() || a.open != a.files[0] || m.cursor != line || m.cursor-m.top != screenRow {
		t.Errorf("f back: open %v line %d screen row %d, want a.go %d %d",
			a.open, m.cursor, m.cursor-m.top, line, screenRow)
	}
}

func TestFlowStaysOn(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f")
	if !flowAt(a, 0, -1) {
		t.Fatalf("f in the list should show the flow on the bar of a.go")
	}
	view := ansi.Strip(a.View())
	for _, want := range []string{"a.go   +2 -2", "new9"} {
		if !strings.Contains(view, want) {
			t.Errorf("flow view lacks %q:\n%s", want, view)
		}
	}
	pressApp(a, "n")
	view = ansi.Strip(a.View())
	for _, want := range []string{"b.bin   binary", "binary file"} {
		if !strings.Contains(view, want) {
			t.Errorf("flow view lacks %q:\n%s", want, view)
		}
	}
	pressApp(a, "backspace")
	if a.open != nil || !a.cfg.Settings.Flow || a.cursor != 1 {
		t.Fatalf("backspace should show the list on b.bin with flow still on")
	}
	pressApp(a, "j", "enter")
	if !flowAt(a, 2, -1) {
		t.Errorf("enter on c.go with flow on should show the flow on its bar")
	}
}

func TestFlowNavigation(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f")

	// ] and [ go from change to change across files, skipping the binary.
	pressApp(a, "]", "]")
	if _, f, l := a.flow.position(); f != 0 || !a.files[0].view.changeStart(l) {
		t.Errorf("] ]: file %d line %d, want the second change of a.go", f, l)
	}
	pressApp(a, "]")
	if _, f, _ := a.flow.position(); f != 2 {
		t.Errorf("third ]: file %d, want c.go", f)
	}
	pressApp(a, "]")
	if a.flow.msg != "no more changes" {
		t.Errorf("] after the last change: msg %q", a.flow.msg)
	}
	pressApp(a, "[")
	if _, f, l := a.flow.position(); f != 0 || !a.files[0].view.changeStart(l) {
		t.Errorf("[ from c.go: file %d line %d, want the second change of a.go", f, l)
	}

	// n and p go from file bar to file bar.
	pressApp(a, "n")
	if !flowAt(a, 1, -1) {
		t.Errorf("n from a.go should reach the bar of b.bin")
	}
	pressApp(a, "n", "n")
	if !flowAt(a, 2, -1) || a.flow.msg != "last file" {
		t.Errorf("n n: msg %q, want the bar of c.go and last file", a.flow.msg)
	}
	pressApp(a, "p")
	if !flowAt(a, 1, -1) {
		t.Errorf("p from c.go should reach the bar of b.bin")
	}

	// j and k cross file borders; G goes to the end.
	pressApp(a, "k")
	if _, f, l := a.flow.position(); f != 0 || l < 0 {
		t.Errorf("k from the bar of b.bin: file %d line %d, want the last row of a.go", f, l)
	}
	pressApp(a, "G")
	if _, f, l := a.flow.position(); f != 2 || l != len(a.files[2].view.view)-1 {
		t.Errorf("G: file %d line %d, want the last row of c.go", f, l)
	}
}

func TestFlowAnchorShared(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f", "]")
	// Mark the changed line on the left, then pick its partner on the right
	// of the same row.
	pressApp(a, "enter", "enter")
	m := a.files[0].view
	if len(m.anchors) != 1 || !strings.HasPrefix(a.flow.msg, "anchored L10↔R10") {
		t.Fatalf("anchors %v, msg %q", m.anchors, a.flow.msg)
	}
	if !strings.Contains(ansi.Strip(a.View()), "anchors 1") {
		t.Errorf("flow status lacks the anchor count")
	}
	pressApp(a, "f")
	if a.open != a.files[0] || len(a.open.view.anchors) != 1 {
		t.Errorf("anchor not in the single file view")
	}
}

func TestFlowMarkMovesBetweenFiles(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f", "]", "enter")
	if a.files[0].view.pending == nil {
		t.Fatalf("enter did not mark a line in a.go")
	}
	pressApp(a, "]", "]", "enter")
	if a.files[0].view.pending != nil || a.files[2].view.pending == nil {
		t.Errorf("marking in c.go should drop the mark in a.go")
	}
	pressApp(a, "G", "esc")
	if a.files[2].view.pending != nil || a.flow.msg != "mark cancelled" {
		t.Errorf("esc away from the mark: msg %q", a.flow.msg)
	}
}

func TestFlowFoldKeepsScreenRow(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f", "]")
	rows, screenRow := len(a.flow.rows), a.flow.cursor-a.flow.top
	pressApp(a, " ")
	if len(a.flow.rows) <= rows || a.flow.cursor-a.flow.top != screenRow {
		t.Errorf("space: %d rows (was %d), screen row %d (was %d)",
			len(a.flow.rows), rows, a.flow.cursor-a.flow.top, screenRow)
	}
}

// topLines returns the first n lines of the view of m.
func topLines(m tea.Model, n int) []string {
	return strings.SplitN(ansi.Strip(m.View()), "\n", n+1)[:n]
}

func TestFlowHeaderFollowsFile(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f")
	if top := topLines(a, 1)[0]; !strings.Contains(top, "a/a.go") || !strings.Contains(top, "│ b/a.go") {
		t.Errorf("top line on a.go: %q", top)
	}
	pressApp(a, "n", "n")
	if top := topLines(a, 1)[0]; !strings.Contains(top, "a/c.go") || !strings.Contains(top, "│ b/c.go") {
		t.Errorf("top line on c.go: %q", top)
	}
}

func TestAppDiffPreview(t *testing.T) {
	a := newFlowApp() // height 20: 17 rows, 3 for the list, 13 for the box
	view := ansi.Strip(a.View())
	lines := strings.Split(view, "\n")
	if len(lines) != 20 || a.listHeight() != 3 || a.boxHeight() != 13 {
		t.Fatalf("%d lines, list %d box %d:\n%s", len(lines), a.listHeight(), a.boxHeight(), view)
	}
	// The preview starts with the context of the first change.
	if !strings.HasPrefix(lines[6], "╭─ a.go ─") || !strings.Contains(lines[7], "  7│ x6") ||
		!strings.Contains(lines[10], "+10│ new9") {
		t.Errorf("preview of a.go:\n%s", view)
	}
	pressApp(a, "j")
	if view := ansi.Strip(a.View()); !strings.Contains(view, "╭─ b.bin ─") || !strings.Contains(view, "│ binary file") {
		t.Errorf("preview of b.bin:\n%s", view)
	}
	pressApp(a, "v")
	if a.boxHeight() != 0 || a.listHeight() != 17 || strings.Contains(ansi.Strip(a.View()), "╭─") {
		t.Errorf("v should hide the preview: list %d box %d", a.listHeight(), a.boxHeight())
	}
}

func TestFlowAndPreviewLayout(t *testing.T) {
	a := newFlowApp()
	if view := ansi.Strip(a.View()); !strings.Contains(view, "+10│ new9") {
		t.Errorf("side by side preview lacks +10│ new9:\n%s", view)
	}
	pressApp(a, "s")
	if view := ansi.Strip(a.View()); !strings.Contains(view, "-10   │ x9") || !strings.Contains(view, "+   10│ new9") {
		t.Errorf("normal preview:\n%s", view)
	}

	pressApp(a, "s", "f", "]")
	rows := len(a.flow.rows)
	_, file, line := a.flow.position()
	pressApp(a, "s")
	if len(a.flow.rows) != rows+3 || !flowAt(a, file, line) || a.flow.cursorSub() != 0 {
		t.Fatalf("s in the flow: %d rows (was %d, 3 changed rows), cursor %d.%d",
			len(a.flow.rows), rows, line, a.flow.cursorSub())
	}
	pressApp(a, "j")
	if !flowAt(a, file, line) || a.flow.cursorSub() != 1 {
		t.Errorf("j in the flow should reach the added line of the same row")
	}
	// The single file view follows, on the same line of the row.
	pressApp(a, "f")
	if m := a.open.view; !m.screenUnified || m.cursor != line || m.sub != 1 {
		t.Errorf("f: normal %v cursor %d.%d, want %d.1", m.screenUnified, m.cursor, m.sub, line)
	}
}

func TestFlowToggleAllBlocks(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f", "]")
	rows := len(a.flow.rows)
	pressApp(a, "ctrl+space")
	if len(folds(a.files[0].view)) != 0 || len(a.flow.rows) <= rows {
		t.Errorf("ctrl+space in the flow: %d folds in a.go, %d rows (was %d)",
			len(folds(a.files[0].view)), len(a.flow.rows), rows)
	}
}
