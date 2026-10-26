package ui

import (
	"fmt"
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

// flowAtStart reports whether the flow view is shown on the first row of
// file below its header.
func flowAtStart(a *App, file int) bool {
	_, f, _ := a.flow.position()
	return a.flowShown() && f == file && a.flow.fileStart(a.flow.cursor)
}

// newHeaderFlow returns the flow of three files f00 f01 f02 of the lines
// c0 c1 c2, each with c1 changed, nothing folded, in a terminal height
// lines high.
func newHeaderFlow(height int) *App {
	var files []source.File
	for i := range 3 {
		files = append(files, modified(fmt.Sprintf("f%02d", i), numbered("c", 3), []string{"c0", "new", "c2"}))
	}
	a := NewApp(config.Default(), files)
	a.cfg.Settings.CollapseUnchanged = false
	a.Update(tea.WindowSizeMsg{Width: 40, Height: height})
	pressApp(a, "f")
	return a
}

func TestFlowFileHeaders(t *testing.T) {
	f := newHeaderFlow(30).flow
	var got []string
	for i := range f.rows {
		got = append(got, strings.TrimRight(ansi.Strip(f.row(i)), " "))
	}
	rule := strings.Repeat("─", 40)
	want := []string{
		"  f00   +1 -1", "", rule, "", " 1│ c0", "-2│ c1", " 3│ c2",
		"", "  f01   +1 -1", "", rule, "", " 1│ c0", "-2│ c1", " 3│ c2",
		"", "  f02   +1 -1", "", rule, "", " 1│ c0", "-2│ c1", " 3│ c2",
	}
	if len(got) != len(want) {
		t.Fatalf("flow rows:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) || (want[i] == "" && got[i] != "") {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFlowFoldTakesRulePlace(t *testing.T) {
	b := numbered("c", 30)
	b[15] = "new"
	a := NewApp(config.Default(), []source.File{modified("f00", numbered("c", 30), b), modified("f01", numbered("c", 30), b)})
	a.Update(tea.WindowSizeMsg{Width: 40, Height: 30})
	pressApp(a, "f")
	var got []string
	for i := range a.flow.rows {
		line := strings.TrimSpace(ansi.Strip(a.flow.row(i)))
		if strings.Contains(line, "unchanged lines") {
			line = "fold"
		}
		got = append(got, line)
	}
	file := []string{"fold", "", "13│ c12", "14│ c13", "15│ c14", "-16│ c15", "17│ c16", "18│ c17", "19│ c18", "", "fold"}
	want := append(append([]string{"f00   +1 -1", ""}, file...), "", "f01   +1 -1", "")
	want = append(want, file...)
	if len(got) != len(want) {
		t.Fatalf("flow rows:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) || (want[i] == "" && got[i] != "") {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFlowCursorSkipsHeaders(t *testing.T) {
	// From minHeight, a body of 2 rows, smaller than a header, up to all
	// 23 rows on screen (25) and beyond.
	for height := minHeight; height <= 27; height++ {
		a := newHeaderFlow(height)
		f := a.flow
		var lines []string
		check := func(what string) {
			t.Helper()
			r := f.rows[f.cursor]
			if r.kind != flowDiff {
				t.Fatalf("height %d, %s: cursor on %+v, want a line of a file", height, what, r)
			}
			if f.cursor < f.top || f.cursor >= f.top+f.bodyHeight() {
				t.Fatalf("height %d, %s: cursor %d out of the window from %d", height, what, f.cursor, f.top)
			}
			lines = append(lines, fmt.Sprintf("%d.%d", r.file, r.line))
		}
		check("f")
		for range 9 {
			pressApp(a, "j")
			check("j")
		}
		for range 8 {
			pressApp(a, "k")
			check("k")
		}
		want := "0.0 0.1 0.2 1.0 1.1 1.2 2.0 2.1 2.2 2.2 2.1 2.0 1.2 1.1 1.0 0.2 0.1 0.0"
		if got := strings.Join(lines, " "); got != want {
			t.Errorf("height %d: j and k went over %s, want %s", height, got, want)
		}

		// n and p: on the file's first line, its header at the top when it fits.
		lines = nil
		for _, k := range []string{"n", "n", "p", "p"} {
			pressApp(a, k)
			check(k)
			// The window ends with the flow's last row at the latest.
			title := f.find(flowRow{flowFile, 0, f.rows[f.cursor].file, fileTitle, 0})
			if top := min(title, max(len(f.rows)-f.bodyHeight(), 0)); f.bodyHeight() >= 5 && f.top != top {
				t.Errorf("height %d, %s: window from %d, want %d, the title's row %d at the top or above",
					height, k, f.top, top, title)
			}
		}
		if got := strings.Join(lines, " "); got != "1.0 2.0 1.0 0.0" {
			t.Errorf("height %d: n n p p went over %s, want 1.0 2.0 1.0 0.0", height, got)
		}
	}
}

func TestFlowToggleKeepsLine(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "enter", "]", "]")
	m := a.open.view
	line, screenRow := m.cursor, m.pos()-m.top

	pressApp(a, "f")
	if !flowAt(a, 0, line) || a.flow.cursor-a.flow.top != screenRow {
		_, f, l := a.flow.position()
		t.Fatalf("f: flow at file %d line %d screen row %d, want 0 %d %d",
			f, l, a.flow.cursor-a.flow.top, line, screenRow)
	}
	pressApp(a, "f")
	if a.flowShown() || a.open != a.files[0] || m.cursor != line || m.pos()-m.top != screenRow {
		t.Errorf("f back: open %v line %d screen row %d, want a.go %d %d",
			a.open, m.cursor, m.pos()-m.top, line, screenRow)
	}
}

func TestFlowStaysOn(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f")
	if !flowAtStart(a, 0) {
		t.Fatalf("f in the list should show the flow on the first line of a.go")
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
	if !flowAtStart(a, 2) {
		t.Errorf("enter on c.go with flow on should show the flow on its first line")
	}
}

func TestFlowNavigation(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f")

	// ] and [ go from change to change across files, skipping the binary.
	pressApp(a, "]", "]")
	if _, f, l := a.flow.position(); f != 0 || !a.flow.changeStart(a.flow.cursor) {
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
	if _, f, l := a.flow.position(); f != 0 || !a.flow.changeStart(a.flow.cursor) {
		t.Errorf("[ from c.go: file %d line %d, want the second change of a.go", f, l)
	}

	// n and p go from file to file, to the first row below its header.
	pressApp(a, "n")
	if !flowAtStart(a, 1) {
		t.Errorf("n from a.go should reach the note of b.bin")
	}
	pressApp(a, "n", "n")
	if !flowAtStart(a, 2) || a.flow.msg != "last file" {
		t.Errorf("n n: msg %q, want the first line of c.go and last file", a.flow.msg)
	}
	pressApp(a, "p")
	if !flowAtStart(a, 1) {
		t.Errorf("p from c.go should reach the note of b.bin")
	}

	// j and k cross file borders; G goes to the end.
	pressApp(a, "k")
	if _, f, l := a.flow.position(); f != 0 || l < 0 {
		t.Errorf("k from the note of b.bin: file %d line %d, want the last row of a.go", f, l)
	}
	pressApp(a, "G")
	if _, f, l := a.flow.position(); f != 2 || l != len(a.files[2].diff.view)-1 {
		t.Errorf("G: file %d line %d, want the last row of c.go", f, l)
	}
}

func TestFlowAnchorShared(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f", "]")
	// Mark the changed line on the left, then pick its partner on the right
	// of the same row.
	pressApp(a, "enter", "enter")
	m := a.files[0].diff
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
	if a.files[0].diff.pending == nil {
		t.Fatalf("enter did not mark a line in a.go")
	}
	pressApp(a, "]", "]", "enter")
	if a.files[0].diff.pending != nil || a.files[2].diff.pending == nil {
		t.Errorf("marking in c.go should drop the mark in a.go")
	}
	pressApp(a, "G", "esc")
	if a.files[2].diff.pending != nil || a.flow.msg != "mark cancelled" {
		t.Errorf("esc away from the mark: msg %q", a.flow.msg)
	}
}

func TestFlowTakesOverMarkOfDiffView(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "enter", "]", "enter", "f") // mark in the diff view of a.go, then flow
	if a.files[0].diff.pending == nil {
		t.Fatalf("enter did not mark a line in a.go")
	}
	pressApp(a, "G", "esc")
	if a.files[0].diff.pending != nil || a.flow.msg != "mark cancelled" {
		t.Errorf("esc in c.go: a.go mark %v, msg %q; want the mark of the diff view cancelled",
			a.files[0].diff.pending, a.flow.msg)
	}

	a = newFlowApp()
	pressApp(a, "enter", "]", "enter", "f", "G", "enter")
	if a.files[0].diff.pending != nil || a.files[2].diff.pending == nil {
		t.Errorf("marking in c.go should drop the mark set in the diff view of a.go")
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

func TestAppPreviewOfEmptyFile(t *testing.T) {
	a := NewApp(config.Default(), []source.File{
		{Path: "empty", InNew: true},
		modified("c.go", []string{"old"}, []string{"new"}),
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if view := ansi.Strip(a.View()); !strings.Contains(view, "╭─ empty ─") {
		t.Errorf("preview of the empty file:\n%s", view)
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
	if m := a.open.view; !m.screenShape.unified || m.cursor != line || m.sub != 1 {
		t.Errorf("f: normal %v cursor %d.%d, want %d.1", m.screenShape.unified, m.cursor, m.sub, line)
	}
}

func TestFlowToggleAllBlocks(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f", "]")
	rows := len(a.flow.rows)
	pressApp(a, "ctrl+space")
	if len(folds(a.files[0].diff.view)) != 0 || len(a.flow.rows) <= rows {
		t.Errorf("ctrl+space in the flow: %d folds in a.go, %d rows (was %d)",
			len(folds(a.files[0].diff.view)), len(a.flow.rows), rows)
	}
}

func TestFlowHandsOverOnlyItsFile(t *testing.T) {
	a := NewApp(config.Default(), []source.File{
		modified("a.go", []string{"1", "2", "3"}, []string{"1", "x", "3"}),
		modified("b.go", []string{"p", "q", "r"}, []string{"p", "y", "r"}),
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	press := func(keys ...string) {
		for _, k := range keys {
			pressApp(a, k)
			a.View() // drawing the flow must not change the diff views
		}
	}
	press("enter", "f", "j", "j", "l", "l", "f")
	if m := a.open.view; a.open.Path != "a.go" || m.xoff != 16 {
		t.Fatalf("leaving the flow on a.go: file %s xoff %d, want a.go and 16", a.open.Path, m.xoff)
	}
	press("n")
	if m := a.open.view; a.open.Path != "b.go" || m.xoff != 0 {
		t.Errorf("n to b.go: file %s xoff %d, want b.go and its own 0", a.open.Path, m.xoff)
	}
}

func TestFlowChangesKeepOtherViewsOnTheirLine(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "enter", "]", "]") // a.go on its change at line 25
	if line := a.open.view.lineAt(left, a.open.view.cursor); line != 24 {
		t.Fatalf("a.go cursor on line %d, want 24", line)
	}
	// Unfolding all of a.go in the flow moves its rows; leave the flow on c.go.
	pressApp(a, "f", "ctrl+space", "n", "n", "f")
	if a.open.Path != "c.go" {
		t.Fatalf("left the flow on %s, want c.go", a.open.Path)
	}
	pressApp(a, "p")
	if m := a.open.view; a.open.Path != "a.go" || m.lineAt(left, m.cursor) != 24 || len(folds(m.view)) != 0 {
		t.Errorf("back to a.go: file %s line %d, %d folds; want a.go, 24, unfolded",
			a.open.Path, m.lineAt(left, m.cursor), len(folds(m.view)))
	}
}

func TestFlowPageKeysKeepScreenRow(t *testing.T) {
	a := NewApp(config.Default(), []source.File{modified("a.go", numbered("a", 100), numbered("b", 100))})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressApp(a, "f", "j", "j", "j", "j", "j")
	row := a.flow.cursor - a.flow.top
	for _, k := range []string{"pgdown", "ctrl+d", "pgup", "ctrl+u"} {
		top := a.flow.top
		pressApp(a, k)
		if a.flow.cursor-a.flow.top != row || a.flow.top == top {
			t.Errorf("%s: screen row %d, top %d (was %d); want row %d and the window moved",
				k, a.flow.cursor-a.flow.top, a.flow.top, top, row)
		}
	}
	// Where the window stops, the cursor stops too.
	for range 10 {
		pressApp(a, "pgdown")
	}
	if last := len(a.flow.rows) - a.flow.bodyHeight(); a.flow.top != last || a.flow.cursor-a.flow.top != row {
		t.Errorf("pgdown at the end: top %d, screen row %d; want %d and %d", a.flow.top, a.flow.cursor-a.flow.top, last, row)
	}
}

func TestFlowMessageToggleOnlyInLog(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f", "c")
	if !a.cfg.Settings.FullMessages {
		t.Errorf("c outside the log toggled the commit messages")
	}
}

func TestWrapInFlowKeepsCursorLine(t *testing.T) {
	b := wrapLines("x")
	b[12] = "changed " + b[12]
	a := NewApp(config.Default(), []source.File{modified("a.go", wrapLines("x"), b)})
	a.cfg.Settings.CollapseUnchanged = false
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	d := a.files[0].diff
	d.buildView()
	at := d.spotOf(right, 15, a.open.view.shape())
	a.open.view.show(at, 8)
	line := func() int { _, _, l := a.flow.position(); return d.lineAt(right, l) }

	pressApp(a, "f", "r")
	if line() != 15 || a.flow.cursor-a.flow.top != 8 {
		t.Errorf("flow, r: cursor on line %d at screen row %d, want line 15 at row 8", line(), a.flow.cursor-a.flow.top)
	}
	if !strings.Contains(ansi.Strip(a.View()), "↪") {
		t.Errorf("flow, r: no wrapped lines:\n%s", ansi.Strip(a.View()))
	}
	pressApp(a, "f") // back to the diff view, still wrapped
	if m := a.open.view; m.lineAt(right, m.cursor) != 15 || cursorRow(m) != 8 {
		t.Errorf("f: diff view on line %d at screen row %d, want line 15 at row 8", m.lineAt(right, m.cursor), cursorRow(m))
	}
	pressApp(a, "r", "f") // unwrapped in the diff view, then flow
	if line() != 15 || a.flow.cursor-a.flow.top != 8 {
		t.Errorf("r, f: flow on line %d at screen row %d, want line 15 at row 8", line(), a.flow.cursor-a.flow.top)
	}
}

func TestFlowRebuildsAfterDiffView(t *testing.T) {
	for _, keys := range [][]string{{"ctrl+space"}, {"s"}, {"r"}} {
		a := newFlowApp()
		pressApp(a, "f", "f", "enter") // the flow is built, then the diff view of a.go
		pressApp(a, keys...)
		pressApp(a, "f")
		want := -1 // the first file's header has no empty line above
		for _, e := range a.files {
			want += 5 // the header
			if e.Binary {
				want++
				continue
			}
			if e.diff.view[0].hidden > 0 {
				want -= 2 // the fold takes the place of the rule and the empty line
			}
			screen, _ := e.diff.screenLines(a.flow.built)
			want += len(screen)
		}
		if len(a.flow.rows) != want {
			t.Errorf("%v in the diff view, then f: %d flow rows, the diffs have %d", keys, len(a.flow.rows), want)
		}
	}
}

func TestFlowNormalLayoutShowsBlocks(t *testing.T) {
	a := NewApp(config.Default(), []source.File{
		modified("a.go", []string{"same", "old1", "old2", "end"}, []string{"same", "new1", "new2", "end"}),
		modified("b.go", []string{"x"}, []string{"y"}),
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressApp(a, "s", "f", "g", "]")
	var got []string
	for i := a.flow.cursor; i < a.flow.cursor+4; i++ {
		got = append(got, strings.TrimRight(ansi.Strip(a.flow.row(i)), " "))
	}
	for i, want := range []string{"old1", "old2", "new1", "new2"} {
		if !strings.HasSuffix(got[i], "│ "+want) {
			t.Fatalf("flow from ]:\n%s\nwant old1 old2 new1 new2", strings.Join(got, "\n"))
		}
	}
	pressApp(a, "]")
	if _, file, _ := a.flow.position(); file != 1 {
		t.Errorf("] from a.go's block: file %d, want b.go: a block counts once", file)
	}
}

func TestFlowLineEndTakesPieceWhenWrapped(t *testing.T) {
	probe := newTestModel([]string{"x"}, []string{"x"})
	full := strings.Repeat("x", probe.contentWidth(80, false))
	a := NewApp(config.Default(), []source.File{modified("a.go", []string{full, "old"}, []string{full, "new"})})
	a.cfg.Settings.CollapseUnchanged = false
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressApp(a, "f", "r")
	before := len(a.flow.rows)
	pressApp(a, "w")
	if len(a.flow.rows) != before+1 {
		t.Errorf("w with wrapped lines: %d flow rows, %d before; want one more for the full line's ¶", len(a.flow.rows), before)
	}
}
