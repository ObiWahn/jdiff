package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/source"
)

// newFlowDiff returns a file list with a.go (changes at lines 10 and 25
// of 30), the binary b.bin and c.go (one change).
func newFlowDiff() *Diff {
	a, b := numbered("x", 30), numbered("x", 30)
	b[9], b[24] = "new9", "new24"
	d := NewDiff(config.Default(), []source.File{
		modified("a.go", a, b),
		{Path: "b.bin", InOld: true, InNew: true, Binary: true},
		modified("c.go", []string{"same", "old"}, []string{"same", "new"}),
	})
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return d
}

// flowAt reports whether the flow view is shown with its cursor on file
// and line (-1 for the file bar).
func flowAt(d *Diff, file, line int) bool {
	if !d.flowShown() {
		return false
	}
	_, f, l := d.flow.position()
	return f == file && l == line
}

func TestFlowToggleKeepsLine(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "enter", "]", "]")
	m := d.open.view
	line, screenRow := m.cursor, m.cursor-m.top

	pressDiff(d, "f")
	if !flowAt(d, 0, line) || d.flow.cursor-d.flow.top != screenRow {
		_, f, l := d.flow.position()
		t.Fatalf("f: flow at file %d line %d screen row %d, want 0 %d %d",
			f, l, d.flow.cursor-d.flow.top, line, screenRow)
	}
	pressDiff(d, "f")
	if d.flowShown() || d.open != d.files[0] || m.cursor != line || m.cursor-m.top != screenRow {
		t.Errorf("f back: open %v line %d screen row %d, want a.go %d %d",
			d.open, m.cursor, m.cursor-m.top, line, screenRow)
	}
}

func TestFlowStaysOn(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "f")
	if !flowAt(d, 0, -1) {
		t.Fatalf("f in the list should show the flow on the bar of a.go")
	}
	view := ansi.Strip(d.frame())
	for _, want := range []string{"a.go   +2 -2", "new9"} {
		if !strings.Contains(view, want) {
			t.Errorf("flow view lacks %q:\n%s", want, view)
		}
	}
	pressDiff(d, "n")
	view = ansi.Strip(d.frame())
	for _, want := range []string{"b.bin   binary", "binary file"} {
		if !strings.Contains(view, want) {
			t.Errorf("flow view lacks %q:\n%s", want, view)
		}
	}
	pressDiff(d, "backspace")
	if d.open != nil || !d.cfg.Settings.Flow || d.cursor != 1 {
		t.Fatalf("backspace should show the list on b.bin with flow still on")
	}
	pressDiff(d, "j", "enter")
	if !flowAt(d, 2, -1) {
		t.Errorf("enter on c.go with flow on should show the flow on its bar")
	}
}

func TestFlowNavigation(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "f")

	// ] and [ go from change to change across files, skipping the binary.
	pressDiff(d, "]", "]")
	if _, f, l := d.flow.position(); f != 0 || !d.flow.changeStart(d.flow.cursor) {
		t.Errorf("] ]: file %d line %d, want the second change of a.go", f, l)
	}
	pressDiff(d, "]")
	if _, f, _ := d.flow.position(); f != 2 {
		t.Errorf("third ]: file %d, want c.go", f)
	}
	pressDiff(d, "]")
	if d.flow.msg != "no more changes" {
		t.Errorf("] after the last change: msg %q", d.flow.msg)
	}
	pressDiff(d, "[")
	if _, f, l := d.flow.position(); f != 0 || !d.flow.changeStart(d.flow.cursor) {
		t.Errorf("[ from c.go: file %d line %d, want the second change of a.go", f, l)
	}

	// n and p go from file bar to file bar.
	pressDiff(d, "n")
	if !flowAt(d, 1, -1) {
		t.Errorf("n from a.go should reach the bar of b.bin")
	}
	pressDiff(d, "n", "n")
	if !flowAt(d, 2, -1) || d.flow.msg != "last file" {
		t.Errorf("n n: msg %q, want the bar of c.go and last file", d.flow.msg)
	}
	pressDiff(d, "p")
	if !flowAt(d, 1, -1) {
		t.Errorf("p from c.go should reach the bar of b.bin")
	}

	// j and k cross file borders; end goes to the last row.
	pressDiff(d, "k")
	if _, f, l := d.flow.position(); f != 0 || l < 0 {
		t.Errorf("k from the bar of b.bin: file %d line %d, want the last row of a.go", f, l)
	}
	pressDiff(d, "end")
	if _, f, l := d.flow.position(); f != 2 || l != len(d.files[2].align.view)-1 {
		t.Errorf("end: file %d line %d, want the last row of c.go", f, l)
	}
}

func TestFlowAnchorShared(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "f", "]")
	// Mark the changed line on the left, then pick its partner on the right
	// of the same row.
	pressDiff(d, "enter", "enter")
	m := d.files[0].align
	if len(m.anchors) != 1 || !strings.HasPrefix(d.flow.msg, "anchored L10↔R10") {
		t.Fatalf("anchors %v, msg %q", m.anchors, d.flow.msg)
	}
	if !strings.Contains(ansi.Strip(d.frame()), "anchors 1") {
		t.Errorf("flow status lacks the anchor count")
	}
	pressDiff(d, "f")
	if d.open != d.files[0] || len(d.open.view.anchors) != 1 {
		t.Errorf("anchor not in the single file view")
	}
}

func TestFlowMarkMovesBetweenFiles(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "f", "]", "enter")
	if d.files[0].align.pending == nil {
		t.Fatalf("enter did not mark a line in a.go")
	}
	pressDiff(d, "]", "]", "enter")
	if d.files[0].align.pending != nil || d.files[2].align.pending == nil {
		t.Errorf("marking in c.go should drop the mark in a.go")
	}
	pressDiff(d, "end", "esc")
	if d.files[2].align.pending != nil || d.flow.msg != "mark cancelled" {
		t.Errorf("esc away from the mark: msg %q", d.flow.msg)
	}
}

func TestFlowTakesOverMarkOfDiffView(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "enter", "]", "enter", "f") // mark in the diff view of a.go, then flow
	if d.files[0].align.pending == nil {
		t.Fatalf("enter did not mark a line in a.go")
	}
	pressDiff(d, "end", "esc")
	if d.files[0].align.pending != nil || d.flow.msg != "mark cancelled" {
		t.Errorf("esc in c.go: a.go mark %v, msg %q; want the mark of the diff view cancelled",
			d.files[0].align.pending, d.flow.msg)
	}

	d = newFlowDiff()
	pressDiff(d, "enter", "]", "enter", "f", "end", "enter")
	if d.files[0].align.pending != nil || d.files[2].align.pending == nil {
		t.Errorf("marking in c.go should drop the mark set in the diff view of a.go")
	}
}

func TestFlowFoldKeepsScreenRow(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "f", "]")
	rows, screenRow := len(d.flow.rows), d.flow.cursor-d.flow.top
	pressDiff(d, " ")
	if len(d.flow.rows) <= rows || d.flow.cursor-d.flow.top != screenRow {
		t.Errorf("space: %d rows (was %d), screen row %d (was %d)",
			len(d.flow.rows), rows, d.flow.cursor-d.flow.top, screenRow)
	}
}

// topLines returns the first n lines of the view of m.
func topLines(m tea.Model, n int) []string {
	return strings.SplitN(ansi.Strip(m.View().Content), "\n", n+1)[:n]
}

func TestFlowHeaderFollowsFile(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "f")
	if top := topLines(d, 1)[0]; !strings.Contains(top, "a/a.go") || !strings.Contains(top, "│ b/a.go") {
		t.Errorf("top line on a.go: %q", top)
	}
	pressDiff(d, "n", "n")
	if top := topLines(d, 1)[0]; !strings.Contains(top, "a/c.go") || !strings.Contains(top, "│ b/c.go") {
		t.Errorf("top line on c.go: %q", top)
	}
}

func TestDiffPreview(t *testing.T) {
	d := newFlowDiff() // height 20: 17 rows, 3 for the list, 13 for the box
	view := ansi.Strip(d.frame())
	lines := strings.Split(view, "\n")
	if len(lines) != 20 || d.listHeight() != 3 || d.boxHeight() != 13 {
		t.Fatalf("%d lines, list %d box %d:\n%s", len(lines), d.listHeight(), d.boxHeight(), view)
	}
	// The preview starts with the context of the first change.
	if !strings.HasPrefix(lines[6], "╭─ a.go ─") || !strings.Contains(lines[7], "  7│ x6") ||
		!strings.Contains(lines[10], "+10│ new9") {
		t.Errorf("preview of a.go:\n%s", view)
	}
	pressDiff(d, "j")
	if view := ansi.Strip(d.frame()); !strings.Contains(view, "╭─ b.bin ─") || !strings.Contains(view, "│ binary file") {
		t.Errorf("preview of b.bin:\n%s", view)
	}
	setOption(d, d.cfg, "v")
	if d.boxHeight() != 0 || d.listHeight() != 17 || strings.Contains(ansi.Strip(d.frame()), "╭─") {
		t.Errorf("v should hide the preview: list %d box %d", d.listHeight(), d.boxHeight())
	}
}

func TestDiffPreviewScrolls(t *testing.T) {
	// From minHeight, without a box, past the heights where it shows part
	// of the 60 lines of a.go to where it shows them all (64).
	for height := minHeight; height <= 66; height++ {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			t.Parallel()
			b := numbered("x", 60)
			b[30], b[50] = "new30", "new50"
			cfg := config.Default()
			cfg.Settings.CollapseUnchanged = false
			d := NewDiff(cfg, []source.File{modified("a.go", numbered("x", 60), b), modified("c.go", []string{"old"}, []string{"new"})})
			d.Update(tea.WindowSizeMsg{Width: 80, Height: height})
			inner := d.boxHeight() - 2
			if d.boxHeight() == 0 {
				pressDiff(d, "pgdown")
				if d.cursor != 1 {
					t.Errorf("height %d without a preview: pgdn on file %d, want the list paged to c.go", height, d.cursor)
				}
				return
			}
			// first returns the number of the first line in the preview.
			first := func() int {
				lines := strings.Split(ansi.Strip(d.frame()), "\n")
				for i, l := range lines {
					if strings.HasPrefix(l, "╭─ ") {
						var n int
						fmt.Sscanf(strings.TrimLeft(lines[i+1], "│ -+"), "%d", &n)
						return n - 1
					}
				}
				return -1
			}
			last := max(60-inner, 27) // the start of a.go's last page, unless it starts later
			check := func(keys string, want int) {
				t.Helper()
				if got := first(); got != want || d.cursor != 0 {
					t.Fatalf("height %d, %s: preview from x%d, cursor on file %d; want x%d on a.go", height, keys, got, d.cursor, want)
				}
			}
			check("start", 27) // the context of the first change
			page := max(inner-1, 1)
			pressDiff(d, "pgdown")
			check("pgdn", min(27+page, last))
			for range 60 {
				pressDiff(d, "pgdown")
			}
			check("pgdn to the end", last)
			pressDiff(d, "pgup")
			check("pgup", max(last-page, 0))
			for range 60 {
				pressDiff(d, "ctrl+b")
			}
			check("ctrl+b to the top", 0)
			pressDiff(d, "ctrl+f", "j", "k")
			check("j k", 27)
			setOption(d, d.cfg, "v")
			pressDiff(d, "pgdown")
			if d.cursor != 1 {
				t.Errorf("height %d, preview hidden: pgdn on file %d, want the list paged to c.go", height, d.cursor)
			}
		})
	}
}

func TestDiffPreviewOfEmptyFile(t *testing.T) {
	d := NewDiff(config.Default(), []source.File{
		{Path: "empty", InNew: true},
		modified("c.go", []string{"old"}, []string{"new"}),
	})
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if view := ansi.Strip(d.frame()); !strings.Contains(view, "╭─ empty ─") {
		t.Errorf("preview of the empty file:\n%s", view)
	}
}

func TestFlowAndPreviewLayout(t *testing.T) {
	d := newFlowDiff()
	if view := ansi.Strip(d.frame()); !strings.Contains(view, "+10│ new9") {
		t.Errorf("side by side preview lacks +10│ new9:\n%s", view)
	}
	setOption(d, d.cfg, "s")
	if view := ansi.Strip(d.frame()); !strings.Contains(view, "-10   │ x9") || !strings.Contains(view, "+   10│ new9") {
		t.Errorf("normal preview:\n%s", view)
	}

	setOption(d, d.cfg, "s")
	pressDiff(d, "f", "]")
	rows := len(d.flow.rows)
	_, file, line := d.flow.position()
	setOption(d, d.cfg, "s")
	if len(d.flow.rows) != rows+3 || !flowAt(d, file, line) || d.flow.cursorSub() != 0 {
		t.Fatalf("s in the flow: %d rows (was %d, 3 changed rows), cursor %d.%d",
			len(d.flow.rows), rows, line, d.flow.cursorSub())
	}
	pressDiff(d, "j")
	if !flowAt(d, file, line) || d.flow.cursorSub() != 1 {
		t.Errorf("j in the flow should reach the added line of the same row")
	}
	// The single file view follows, on the same line of the row.
	pressDiff(d, "f")
	if m := d.open.view; !m.screenShape.unified || m.cursor != line || m.sub != 1 {
		t.Errorf("f: normal %v cursor %d.%d, want %d.1", m.screenShape.unified, m.cursor, m.sub, line)
	}
}

func TestFlowToggleAllBlocks(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "f", "]")
	rows := len(d.flow.rows)
	pressDiff(d, "ctrl+space")
	if len(folds(d.files[0].align.view)) != 0 || len(d.flow.rows) <= rows {
		t.Errorf("ctrl+space in the flow: %d folds in a.go, %d rows (was %d)",
			len(folds(d.files[0].align.view)), len(d.flow.rows), rows)
	}
}

func TestFlowHandsOverOnlyItsFile(t *testing.T) {
	d := NewDiff(config.Default(), []source.File{
		modified("a.go", []string{"1", "2", "3"}, []string{"1", "x", "3"}),
		modified("b.go", []string{"p", "q", "r"}, []string{"p", "y", "r"}),
	})
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	press := func(keys ...string) {
		for _, k := range keys {
			pressDiff(d, k)
			d.frame() // drawing the flow must not change the diff views
		}
	}
	press("enter", "f", "j", "j", "l", "l", "f")
	if m := d.open.view; d.open.Path != "a.go" || m.xoff != 16 {
		t.Fatalf("leaving the flow on a.go: file %s xoff %d, want a.go and 16", d.open.Path, m.xoff)
	}
	press("n")
	if m := d.open.view; d.open.Path != "b.go" || m.xoff != 0 {
		t.Errorf("n to b.go: file %s xoff %d, want b.go and its own 0", d.open.Path, m.xoff)
	}
}

func TestFlowChangesKeepOtherViewsOnTheirLine(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "enter", "]", "]") // a.go on its change at line 25
	if line := d.open.view.lineAt(left, d.open.view.cursor); line != 24 {
		t.Fatalf("a.go cursor on line %d, want 24", line)
	}
	// Unfolding all of a.go in the flow moves its rows; leave the flow on c.go.
	pressDiff(d, "f", "ctrl+space", "n", "n", "f")
	if d.open.Path != "c.go" {
		t.Fatalf("left the flow on %s, want c.go", d.open.Path)
	}
	pressDiff(d, "p")
	if m := d.open.view; d.open.Path != "a.go" || m.lineAt(left, m.cursor) != 24 || len(folds(m.view)) != 0 {
		t.Errorf("back to a.go: file %s line %d, %d folds; want a.go, 24, unfolded",
			d.open.Path, m.lineAt(left, m.cursor), len(folds(m.view)))
	}
}

func TestFlowPageKeysKeepScreenRow(t *testing.T) {
	d := NewDiff(config.Default(), []source.File{modified("a.go", numbered("a", 100), numbered("b", 100))})
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressDiff(d, "f", "j", "j", "j", "j", "j")
	row := d.flow.cursor - d.flow.top
	for _, k := range []string{"pgdown", "ctrl+d", "pgup", "ctrl+u"} {
		top := d.flow.top
		pressDiff(d, k)
		if d.flow.cursor-d.flow.top != row || d.flow.top == top {
			t.Errorf("%s: screen row %d, top %d (was %d); want row %d and the window moved",
				k, d.flow.cursor-d.flow.top, d.flow.top, top, row)
		}
	}
	// Where the window stops, the cursor stops too.
	for range 10 {
		pressDiff(d, "pgdown")
	}
	if last := len(d.flow.rows) - d.flow.bodyHeight(); d.flow.top != last || d.flow.cursor-d.flow.top != row {
		t.Errorf("pgdown at the end: top %d, screen row %d; want %d and %d", d.flow.top, d.flow.cursor-d.flow.top, last, row)
	}
}

// Outside the log, c and C only keep the preference for the log's flow view:
// the flow view of files has no commits to change.
func TestFlowMessageOptionsOutsideLog(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "f")
	rows, cursor := len(d.flow.rows), d.flow.cursor
	setOption(d, d.cfg, "c", "C")
	if d.cfg.Settings.FullMessages || !d.cfg.Settings.StayInCommit {
		t.Errorf("c C: full messages %v, stay in commit %v; want both flipped", d.cfg.Settings.FullMessages, d.cfg.Settings.StayInCommit)
	}
	if len(d.flow.rows) != rows || d.flow.cursor != cursor || d.flow.msg != "" {
		t.Errorf("c C outside the log: %d rows, cursor %d, msg %q; want %d, %d and no message", len(d.flow.rows), d.flow.cursor, d.flow.msg, rows, cursor)
	}
}

func TestWrapInFlowKeepsCursorLine(t *testing.T) {
	b := wrapLines("x")
	b[12] = "changed " + b[12]
	d := NewDiff(config.Default(), []source.File{modified("a.go", wrapLines("x"), b)})
	d.cfg.Settings.CollapseUnchanged = false
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressDiff(d, "enter")
	a := d.files[0].align
	a.buildView()
	at := a.spotOf(right, 15, d.open.view.shape())
	d.open.view.show(at, 8)
	line := func() int { _, _, l := d.flow.position(); return a.lineAt(right, l) }

	pressDiff(d, "f")
	setOption(d, d.cfg, "w")
	if line() != 15 || d.flow.cursor-d.flow.top != 8 {
		t.Errorf("flow, r: cursor on line %d at screen row %d, want line 15 at row 8", line(), d.flow.cursor-d.flow.top)
	}
	if !strings.Contains(ansi.Strip(d.frame()), "↪") {
		t.Errorf("flow, r: no wrapped lines:\n%s", ansi.Strip(d.frame()))
	}
	pressDiff(d, "f") // back to the diff view, still wrapped
	if m := d.open.view; m.lineAt(right, m.cursor) != 15 || cursorRow(m) != 8 {
		t.Errorf("f: diff view on line %d at screen row %d, want line 15 at row 8", m.lineAt(right, m.cursor), cursorRow(m))
	}
	setOption(d, d.cfg, "w")
	pressDiff(d, "f") // unwrapped in the diff view, then flow
	if line() != 15 || d.flow.cursor-d.flow.top != 8 {
		t.Errorf("r, f: flow on line %d at screen row %d, want line 15 at row 8", line(), d.flow.cursor-d.flow.top)
	}
}

func TestFlowRebuildsAfterDiffView(t *testing.T) {
	for _, keys := range [][]string{{"ctrl+space"}, {"s"}, {"o", "w", "esc"}} {
		d := newFlowDiff()
		pressDiff(d, "f", "f", "enter") // the flow is built, then the diff view of a.go
		pressRoot(WithSettings(d.cfg, d), keys...)
		pressDiff(d, "f")
		want := 0
		for _, e := range d.files {
			want++ // the bar
			if e.Binary {
				want++
				continue
			}
			for v := range e.align.view {
				want += e.align.rowLines(v, d.flow.built)
			}
		}
		if len(d.flow.rows) != want {
			t.Errorf("%v in the diff view, then f: %d flow rows, the diffs have %d", keys, len(d.flow.rows), want)
		}
	}
}

func TestFlowNormalLayoutShowsBlocks(t *testing.T) {
	d := NewDiff(config.Default(), []source.File{
		modified("a.go", []string{"same", "old1", "old2", "end"}, []string{"same", "new1", "new2", "end"}),
		modified("b.go", []string{"x"}, []string{"y"}),
	})
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	setOption(d, d.cfg, "s")
	pressDiff(d, "f", "home", "]")
	var got []string
	for i := d.flow.cursor; i < d.flow.cursor+4; i++ {
		got = append(got, strings.TrimRight(ansi.Strip(d.flow.row(i)), " "))
	}
	for i, want := range []string{"old1", "old2", "new1", "new2"} {
		if !strings.HasSuffix(got[i], "│ "+want) {
			t.Fatalf("flow from ]:\n%s\nwant old1 old2 new1 new2", strings.Join(got, "\n"))
		}
	}
	pressDiff(d, "]")
	if _, file, _ := d.flow.position(); file != 1 {
		t.Errorf("] from a.go's block: file %d, want b.go: a block counts once", file)
	}
}

func TestFlowLineEndTakesPieceWhenWrapped(t *testing.T) {
	probe := newTestFileDiff([]string{"x"}, []string{"x"})
	full := strings.Repeat("x", probe.contentWidth(80, false))
	d := NewDiff(config.Default(), []source.File{modified("a.go", []string{full, "old"}, []string{full, "new"})})
	d.cfg.Settings.CollapseUnchanged = false
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressDiff(d, "f")
	setOption(d, d.cfg, "w")
	before := len(d.flow.rows)
	setOption(d, d.cfg, "W")
	if len(d.flow.rows) != before+1 {
		t.Errorf("w with wrapped lines: %d flow rows, %d before; want one more for the full line's ¶", len(d.flow.rows), before)
	}
}

// newFlowSearch returns the flow view of newFlowDiff, on its file bar, with
// new searched for: lines 10 and 25 of a.go and line 2 of c.go.
func newFlowSearch() *Diff {
	d := newFlowDiff()
	d.cfg.Search.Query = config.Query{Text: "new"}
	pressDiff(d, "f")
	return d
}

func TestFlowSearchAgainBothWays(t *testing.T) {
	d := newFlowSearch()
	at := func() string {
		_, file, line := d.flow.position()
		a := d.flow.parts[0].diff.files[file].align
		return fmt.Sprintf("file %d: %s", file, a.lines[right][a.lineAt(right, line)])
	}
	for _, c := range []struct{ key, want, msg string }{
		{".", "file 0: new9", "/new: match 1/3 · . next · , previous"},
		{".", "file 0: new24", "/new: match 2/3 · . next · , previous"},
		{".", "file 2: new", "/new: match 3/3 · . next · , previous"},
		{".", "file 2: new", "/new: no match further down"},
		{",", "file 0: new24", "/new: match 2/3 · . next · , previous"},
		{",", "file 0: new9", "/new: match 1/3 · . next · , previous"},
		{",", "file 0: new9", "/new: no match further up"},
	} {
		pressDiff(d, c.key)
		if got := at(); got != c.want || d.flow.msg != c.msg {
			t.Errorf("%s: on %q, msg %q; want %q, msg %q", c.key, got, d.flow.msg, c.want, c.msg)
		}
	}
}

// Enter in the search bar finds the text in the flow view, as . does.
func TestFlowSearchFromBar(t *testing.T) {
	d := newFlowDiff()
	s := WithSearch(d.cfg, d)
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressRoot(s, "f", "/", "new24", "enter")
	_, file, line := d.flow.position()
	a := d.flow.parts[0].diff.files[file].align
	if file != 0 || a.lines[right][a.lineAt(right, line)] != "new24" {
		t.Errorf("enter: file %d, row %d, msg %q; want new24 in a.go", file, line, d.flow.msg)
	}
}

func TestFlowScrollsSideways(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "f")
	for _, c := range []struct {
		key  string
		want int
	}{{"l", 8}, {"right", 16}, {"h", 8}, {"left", 0}, {"h", 0}} {
		pressDiff(d, c.key)
		if d.flow.xoff != c.want {
			t.Errorf("%s: xoff %d, want %d", c.key, d.flow.xoff, c.want)
		}
	}
}

func TestFlowOffInList(t *testing.T) {
	d := newFlowDiff()
	pressDiff(d, "f", "backspace")
	if d.open != nil || !d.cfg.Settings.Flow {
		t.Fatalf("backspace: open %v, flow %v; want the list with flow on", d.open, d.cfg.Settings.Flow)
	}
	if v := ansi.Strip(d.frame()); !strings.Contains(v, " · f file · ") {
		t.Errorf("flow on, the bar should offer the single file view:\n%s", v)
	}
	pressDiff(d, "f")
	if d.cfg.Settings.Flow || d.msg != "" || d.open != nil {
		t.Errorf("f: flow %v, msg %q, open %v; want the list with flow off", d.cfg.Settings.Flow, d.msg, d.open)
	}
	if v := ansi.Strip(d.frame()); !strings.Contains(v, " · f flow · ") {
		t.Errorf("flow off, the bar should offer the flow view:\n%s", v)
	}
}

// Without files there is nothing to show in the flow, only the toggle to keep.
func TestFlowOnWithoutFiles(t *testing.T) {
	d := NewDiff(config.Default(), nil)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if d.msg != "no changed files" {
		t.Errorf("msg %q", d.msg)
	}
	pressDiff(d, "f")
	if !d.cfg.Settings.Flow || d.flowShown() {
		t.Errorf("f: flow %v, shown %v; want the toggle on and nothing shown", d.cfg.Settings.Flow, d.flowShown())
	}
	if v := ansi.Strip(d.frame()); !strings.Contains(v, "0 files changed") {
		t.Errorf("view:\n%s", v)
	}
}

// d asks for the arguments of git diff from the flow views too.
func TestFlowAsksForDiff(t *testing.T) {
	log, _ := newTestLog()
	for name, view := range map[string]tea.Model{"files": newFlowDiff(), "log": log} {
		s, _ := newTestApp(view)
		pressRoot(s, "f", "d")
		if s.input == nil {
			t.Errorf("%s: d in the flow does not ask", name)
		}
	}
}
