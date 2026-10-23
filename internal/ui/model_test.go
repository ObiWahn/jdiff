package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"jdiff/internal/config"
	"jdiff/internal/diff"
)

func newTestModel(a, b []string) *Model {
	m := New(config.Default(), "old", "new", a, b)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m
}

func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+space":
		return tea.KeyMsg{Type: tea.KeyCtrlAt}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(keyMsg(k))
	}
}

// pressRoot presses keys on the top-level model m, running the returned
// commands like the Bubble Tea runtime does.
func pressRoot(m tea.Model, keys ...string) {
	for _, k := range keys {
		_, cmd := m.Update(keyMsg(k))
		for cmd != nil {
			_, cmd = m.Update(cmd())
		}
	}
}

// moveTo puts the cursor on the row showing line of side s.
func moveTo(t *testing.T, m *Model, s side, line int) {
	t.Helper()
	if m.active != s {
		press(m, "tab")
	}
	m.cursor = m.viewOf[m.rowOf[s][line]]
}

func TestEnterMarksAndJumpsToOtherPane(t *testing.T) {
	m := newTestModel([]string{"a", "b"}, []string{"a", "c"})
	press(m, "j", "enter")
	if m.pending == nil || *m.pending != (mark{left, 1}) {
		t.Fatalf("pending = %+v, want left line 1", m.pending)
	}
	if m.active != right {
		t.Errorf("active = %v, want right", m.active)
	}
}

func TestAnchorCreateAndDelete(t *testing.T) {
	m := newTestModel([]string{"foo", "bar"}, []string{"bar", "foo"})

	moveTo(t, m, left, 1)
	press(m, "enter")
	moveTo(t, m, right, 0)
	press(m, "enter")
	if want := []diff.Anchor{{Left: 1, Right: 0}}; !slices.Equal(m.anchors, want) {
		t.Fatalf("anchors = %v, want %v", m.anchors, want)
	}
	if m.pending != nil {
		t.Errorf("pending not cleared")
	}
	if r := m.rows[m.rowOf[left][1]]; !r.Anchored || r.Right != 0 {
		t.Errorf("left line 1 is in row %+v, want anchored to right line 0", r)
	}

	// Enter on the anchored line (either pane) deletes the anchor.
	moveTo(t, m, right, 0)
	press(m, "enter")
	if len(m.anchors) != 0 {
		t.Fatalf("anchors = %v after delete, want none", m.anchors)
	}
}

func TestAnchorStartedFromRightPane(t *testing.T) {
	m := newTestModel([]string{"foo", "bar"}, []string{"bar", "foo"})
	moveTo(t, m, right, 1)
	press(m, "enter")
	if m.active != left {
		t.Fatalf("active = %v, want left", m.active)
	}
	moveTo(t, m, left, 0)
	press(m, "enter")
	if want := []diff.Anchor{{Left: 0, Right: 1}}; !slices.Equal(m.anchors, want) {
		t.Fatalf("anchors = %v, want %v", m.anchors, want)
	}
}

func TestEscAndReenterCancelMark(t *testing.T) {
	m := newTestModel([]string{"a"}, []string{"a"})
	press(m, "enter", "esc")
	if m.pending != nil {
		t.Errorf("esc did not cancel the mark")
	}

	press(m, "tab", "enter", "tab", "enter")
	if m.pending != nil {
		t.Errorf("enter on the marked line did not cancel the mark")
	}
}

func TestCrossingAnchorAsksBeforeReplacing(t *testing.T) {
	a := []string{"1", "2", "3", "4"}
	b := []string{"w", "x", "y", "z"}
	m := newTestModel(a, b)
	m.anchors = []diff.Anchor{{Left: 1, Right: 2}}
	m.realign()

	moveTo(t, m, left, 3)
	press(m, "enter")
	moveTo(t, m, right, 0)
	press(m, "enter")
	if m.replace == nil || !strings.Contains(m.msg, "L2↔R3") {
		t.Fatalf("no replace prompt: replace=%v msg=%q", m.replace, m.msg)
	}
	if want := []diff.Anchor{{Left: 1, Right: 2}}; !slices.Equal(m.anchors, want) {
		t.Fatalf("anchors changed before confirmation: %v", m.anchors)
	}

	press(m, "esc")
	if m.replace != nil || m.pending == nil {
		t.Fatalf("esc should cancel the replace but keep the mark")
	}

	press(m, "enter", "enter")
	if want := []diff.Anchor{{Left: 3, Right: 0}}; !slices.Equal(m.anchors, want) {
		t.Fatalf("anchors = %v after replace, want %v", m.anchors, want)
	}
}

func TestEnterOnFillerDoesNothing(t *testing.T) {
	m := newTestModel([]string{"a"}, []string{"a", "b"})
	press(m, "j", "enter")
	if m.pending != nil {
		t.Errorf("marked a filler row")
	}
}

func TestViewFitsWidth(t *testing.T) {
	m := newTestModel([]string{"\tshort", strings.Repeat("x", 200)}, []string{"\t\tshort", "y"})
	press(m, "enter")
	for i, l := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(l); w > 80 {
			t.Errorf("line %d is %d columns wide", i, w)
		}
	}
}

// numbered returns lines "prefix0" .. "prefix<n-1>".
func numbered(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return out
}

func folds(view []vrow) []vrow {
	var out []vrow
	for _, v := range view {
		if v.hidden > 0 {
			out = append(out, v)
		}
	}
	return out
}

func TestFoldUnchanged(t *testing.T) {
	// 20 unchanged lines, one change, 20 unchanged lines.
	a := append(append(numbered("x", 20), "old"), numbered("y", 20)...)
	b := append(append(numbered("x", 20), "new"), numbered("y", 20)...)
	m := newTestModel(a, b)

	// Context 3: rows 0-16 and 24-40 are folded around the change at row 20.
	if got, want := folds(m.view), []vrow{{0, 17, 0}, {24, 17, 1}}; !slices.Equal(got, want) {
		t.Fatalf("folds = %v, want %v", got, want)
	}
	if len(m.view) != 9 {
		t.Errorf("%d displayed rows, want 9", len(m.view))
	}
	if !strings.Contains(ansi.Strip(m.View()), "17 unchanged lines") {
		t.Errorf("fold row not rendered:\n%s", ansi.Strip(m.View()))
	}

	// On the change, both folds are 4 rows away: space opens the lower one.
	press(m, "]")
	changeRow := m.view[m.cursor].row
	press(m, " ")
	if got, want := folds(m.view), []vrow{{0, 17, 0}}; !slices.Equal(got, want) {
		t.Fatalf("after space: folds = %v, want only the upper one", got)
	}
	if m.view[m.cursor].row != changeRow {
		t.Errorf("cursor moved from row %d to %d", changeRow, m.view[m.cursor].row)
	}
	// Space again closes the same block.
	press(m, " ")
	if got := len(folds(m.view)); got != 2 {
		t.Fatalf("after second space: %d folds, want 2", got)
	}

	// On a fold row, space opens that fold; inside an open block, space
	// closes it and the cursor lands on the fold row.
	press(m, "g", " ")
	if got, want := folds(m.view), []vrow{{24, 17, 1}}; !slices.Equal(got, want) {
		t.Fatalf("after space on the first fold: folds = %v, want %v", got, want)
	}
	press(m, "j", "j", " ")
	if m.cursor != 0 || m.view[0].hidden == 0 || len(folds(m.view)) != 2 {
		t.Errorf("closing from inside: cursor %d, view[0] %v, folds %v", m.cursor, m.view[0], folds(m.view))
	}
	press(m, "enter")
	if m.pending != nil {
		t.Errorf("enter on a fold marked a line")
	}
}

func TestFoldShortRunsAndAnchors(t *testing.T) {
	// One unchanged row between the contexts is shown, not folded.
	a := append(append(numbered("x", 4), "old1"), append(numbered("y", 7), "old2")...)
	b := append(append(numbered("x", 4), "new1"), append(numbered("y", 7), "new2")...)
	m := newTestModel(a, b)
	if got, want := folds(m.view), []vrow(nil); !slices.Equal(got, want) {
		t.Errorf("folds = %v, want none", got)
	}

	// An anchored unchanged line stays visible with its context.
	a, b = numbered("x", 30), numbered("x", 30)
	m = newTestModel(a, b)
	m.anchors = []diff.Anchor{{Left: 15, Right: 15}}
	m.realign()
	if got, want := folds(m.view), []vrow{{0, 12, 0}, {19, 11, 1}}; !slices.Equal(got, want) {
		t.Errorf("folds = %v, want %v", got, want)
	}
}

func TestCollapseUnchangedSetting(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.CollapseUnchanged = false
	a, b := numbered("x", 30), append(numbered("x", 30), "new")
	m := New(cfg, "old", "new", a, b)
	if len(folds(m.view)) != 0 || len(m.blocks) != 1 {
		t.Errorf("collapse_unchanged=false: %d folds, %d blocks, want 0 and 1", len(folds(m.view)), len(m.blocks))
	}
	press(m, " ")
	if len(folds(m.view)) != 1 {
		t.Errorf("space did not fold: %v", m.view)
	}
}

// screenRow returns the screen row of the cursor within the diff body.
func screenRow(m *Model) int { return m.cursor - m.top }

func TestCursorKeepsScreenRow(t *testing.T) {
	// Changes every 20 lines, so folding changes the rows above the cursor.
	var a, b []string
	for i := range 10 {
		block := numbered(fmt.Sprintf("b%d-", i), 19)
		a = append(append(a, block...), fmt.Sprintf("old%d", i))
		b = append(append(b, block...), fmt.Sprintf("new%d", i))
	}
	cfg := config.Default()
	cfg.Settings.CollapseUnchanged = false
	m := New(cfg, "old", "new", a, b)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20}) // body height 18

	// Line 100 is context after the change at 99; the block below it folds.
	m.cursor = m.viewOf[m.rowOf[left][100]]
	m.top = m.cursor - 7
	press(m, " ")
	if got := screenRow(m); got != 7 || len(folds(m.view)) != 1 {
		t.Errorf("after folding below: screen row %d, %d folds, want 7 and 1", got, len(folds(m.view)))
	}
	// Folding the block above the cursor must not move it either.
	m.cursor = m.viewOf[m.rowOf[left][118]]
	m.top = m.cursor - 7
	press(m, " ")
	if got := screenRow(m); got != 7 || m.view[m.cursor].row != m.rowOf[left][118] {
		t.Errorf("after folding above: screen row %d on row %d, want 7 on row %d",
			got, m.view[m.cursor].row, m.rowOf[left][118])
	}

	// Anchoring keeps the cursor line in place too. The partner line is one
	// row above the marked one, at screen row 4.
	m.cursor = m.viewOf[m.rowOf[left][119]]
	m.top = m.cursor - 5
	press(m, "enter")
	m.cursor = m.viewOf[m.rowOf[right][118]]
	press(m, "enter")
	if len(m.anchors) != 1 {
		t.Fatalf("anchors = %v", m.anchors)
	}
	if got := screenRow(m); got != 4 {
		t.Errorf("after anchoring: screen row %d, want 4", got)
	}
	press(m, "enter")
	if len(m.anchors) != 0 || screenRow(m) != 4 {
		t.Errorf("after removing anchor: anchors %v, screen row %d, want 4", m.anchors, screenRow(m))
	}
}

func TestCursorScreenRowAtFileEdges(t *testing.T) {
	a := append(numbered("x", 40), "old")
	b := append(numbered("x", 40), "new")
	m := newTestModel(a, b)
	press(m, " ")

	// Near the start the view cannot scroll above line 1.
	m.cursor, m.top = 2, 0
	press(m, " ")
	if m.top != 0 || m.cursor != 0 {
		t.Errorf("top %d cursor %d, want both 0 on the first fold", m.top, m.cursor)
	}

	// Shrinking the window keeps the cursor visible.
	press(m, " ")
	m.cursor = m.viewOf[m.rowOf[left][30]]
	m.top = m.cursor - 15
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	if got := screenRow(m); got < 0 || got >= m.bodyHeight() {
		t.Errorf("cursor off screen after resize: screen row %d, height %d", got, m.bodyHeight())
	}
}

func TestKeyBurstCountsEachKey(t *testing.T) {
	m := newTestModel(numbered("x", 10), append(numbered("x", 10), "new"))
	press(m, " ")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjjj")})
	if m.cursor != 4 {
		t.Errorf("cursor = %d after a burst of 4 j, want 4", m.cursor)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjq")})
	if m.cursor != 6 {
		t.Errorf("cursor = %d, want 6", m.cursor)
	}
}

func TestHighlightVisibleWhitespace(t *testing.T) {
	tests := []struct {
		text    string
		spans   []diff.Span
		showAll bool
		want    string
	}{
		{"  x()", []diff.Span{{Start: 0, End: 2}}, false, "··x()"},
		{"\t\tx", []diff.Span{{Start: 1, End: 2}}, false, "    →   x"},
		{"a  b", []diff.Span{{Start: 1, End: 4}}, false, "a··b"},
		{"\tx y", nil, false, "    x y"},
		{"\tx y ", nil, true, "→   x·y·"},
		{"\t\tx", []diff.Span{{Start: 1, End: 2}}, true, "→   →   x"},
	}
	for _, tt := range tests {
		st := lineStyles{showAll: tt.showAll}
		if got := ansi.Strip(highlight(tt.text, tt.spans, st)); got != tt.want {
			t.Errorf("highlight(%q, %v, showAll %v) = %q, want %q", tt.text, tt.spans, tt.showAll, got, tt.want)
		}
	}
}

func TestShowWhitespacesSetting(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.ShowWhitespaces = true
	m := New(cfg, "old", "new", []string{"\tsame", "x"}, []string{"\tsame", "y"})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	if view := ansi.Strip(m.View()); !strings.Contains(view, "→   same") {
		t.Errorf("unchanged tab not shown:\n%s", view)
	}
}

// newUnifiedTestModel returns a model with an unchanged, a changed, an
// unchanged and a removed line, nothing folded, in the normal layout.
func newUnifiedTestModel() *Model {
	cfg := config.Default()
	cfg.Settings.CollapseUnchanged = false
	m := New(cfg, "old", "new", []string{"same", "alpha beta", "keep", "gone"}, []string{"same", "alpha gamma", "keep"})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	press(m, "s")
	return m
}

func TestUnifiedLayout(t *testing.T) {
	m := newUnifiedTestModel()
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for i, want := range []string{" 1 1│ same", "-2  │ alpha beta", "+  2│ alpha gamma", " 3 3│ keep", "-4  │ gone"} {
		if !strings.HasPrefix(lines[i+1], want) {
			t.Errorf("line %d = %q, want prefix %q", i+1, lines[i+1], want)
		}
	}
}

func TestUnifiedCursor(t *testing.T) {
	m := newUnifiedTestModel()
	// j steps through both lines of the changed row.
	var got []string
	for range 4 {
		press(m, "j")
		got = append(got, fmt.Sprintf("%d.%d", m.cursor, m.sub))
	}
	if want := []string{"1.0", "1.1", "2.0", "3.0"}; !slices.Equal(got, want) {
		t.Errorf("j: cursor rows %v, want %v", got, want)
	}
	// Switching layouts keeps the cursor row, and back on the added line.
	press(m, "k", "k", "s")
	if m.cursor != 1 || m.pos() != 1 {
		t.Errorf("s to side by side: cursor %d pos %d, want row 1", m.cursor, m.pos())
	}
	press(m, "s")
	if m.cursor != 1 || m.pos() != 2 {
		t.Errorf("s to normal: cursor %d pos %d, want the added line of row 1", m.cursor, m.pos())
	}
}

func TestUnifiedShowsAnchors(t *testing.T) {
	m := newUnifiedTestModel()
	press(m, "s")
	moveTo(t, m, left, 1)
	press(m, "enter", "enter")
	if len(m.anchors) != 1 {
		t.Fatalf("anchoring side by side: anchors %v", m.anchors)
	}
	press(m, "s")
	if lines := strings.Split(ansi.Strip(m.View()), "\n"); !strings.HasPrefix(lines[2], "◆2  │ alpha beta") ||
		!strings.HasPrefix(lines[3], "◆  2│ alpha gamma") {
		t.Errorf("anchored lines in the normal layout: %q", lines[2:4])
	}
}

// cursorLine returns the screen line under the cursor without styles.
func cursorLine(m *Model) string {
	return ansi.Strip(m.screenLine(m.screen[m.pos()], false))
}

func TestUnifiedMarks(t *testing.T) {
	m := newUnifiedTestModel()
	// An unchanged line without a pending mark takes the old side.
	press(m, "enter")
	if m.pending == nil || *m.pending != (mark{left, 0}) {
		t.Fatalf("enter on an unchanged line: pending %v, want L1", m.pending)
	}
	press(m, "esc")

	// A removed line takes the old side; the mark shows as ●.
	press(m, "j", "j", "j", "j", "enter")
	if m.pending == nil || *m.pending != (mark{left, 3}) || !strings.HasPrefix(cursorLine(m), "●4  │ gone") {
		t.Fatalf("enter on the removed line: pending %v, line %q", m.pending, cursorLine(m))
	}
	// An unchanged line then takes the other side: new line 3.
	press(m, "k", "enter")
	if len(m.anchors) != 1 || m.anchors[0] != (diff.Anchor{Left: 3, Right: 2}) {
		t.Fatalf("anchors %v, want L4↔R3", m.anchors)
	}
	// The cursor stays on the line picked last.
	if m.sub != 1 || !strings.HasPrefix(cursorLine(m), "◆  3│ keep") {
		t.Errorf("cursor after anchoring on %q (sub %d), want the new line 3", cursorLine(m), m.sub)
	}
	// Enter on it removes the anchor and keeps the cursor on that line.
	press(m, "enter")
	if len(m.anchors) != 0 || m.lineAt(right, m.cursor) != 2 {
		t.Errorf("removing: anchors %v, cursor on R%d", m.anchors, m.lineAt(right, m.cursor)+1)
	}
}

func TestAnchorKeepsLastPickOnScreenRow(t *testing.T) {
	// 4 lines that only match when anchored by hand, amid 80 equal lines
	// shown unfolded, so the view can scroll either way.
	a, b := numbered("x", 40), numbered("x", 40)
	a = append(a, "old1", "old2", "old3", "target")
	b = append(b, "new1", "renamed target", "new3", "new4")
	a, b = append(a, numbered("y", 40)...), append(b, numbered("y", 40)...)
	cfg := config.Default()
	cfg.Settings.CollapseUnchanged = false
	m := New(cfg, "old", "new", a, b)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	moveTo(t, m, left, 43)
	press(m, "enter")
	moveTo(t, m, right, 41)
	m.scrollToCursor()
	row := m.pos() - m.top
	press(m, "enter")
	if len(m.anchors) != 1 || m.lineAt(right, m.cursor) != 41 || m.pos()-m.top != row {
		t.Errorf("anchors %v, cursor on R%d at screen row %d, want R42 at %d",
			m.anchors, m.lineAt(right, m.cursor)+1, m.pos()-m.top, row)
	}
}

func TestToggleAllBlocks(t *testing.T) {
	// Changes at lines 10 and 40 of 60: three folded blocks.
	a := numbered("x", 60)
	b := slices.Clone(a)
	b[9], b[39] = "new9", "new39"
	m := newTestModel(a, b)
	if len(folds(m.view)) != 3 {
		t.Fatalf("%d folds at start, want 3", len(folds(m.view)))
	}
	press(m, "]", "ctrl+space")
	if len(folds(m.view)) != 0 {
		t.Errorf("ctrl+space with folds: %d folds left", len(folds(m.view)))
	}
	line := m.lineAt(left, m.cursor)
	press(m, "ctrl+space")
	if len(folds(m.view)) != 3 || m.lineAt(left, m.cursor) != line {
		t.Errorf("ctrl+space without folds: %d folds, cursor on line %d, want 3 and %d",
			len(folds(m.view)), m.lineAt(left, m.cursor), line)
	}
	// One open block is enough to fold all, one folded block to unfold all.
	press(m, " ", "ctrl+space")
	if len(folds(m.view)) != 0 {
		t.Errorf("ctrl+space with one fold opened: %d folds, want all opened", len(folds(m.view)))
	}
}

func TestCursorLineBold(t *testing.T) {
	defer lipgloss.SetColorProfile(lipgloss.ColorProfile())
	lipgloss.SetColorProfile(termenv.ANSI)
	m := newTestModel([]string{"same", "old"}, []string{"same", "new"})
	for _, layout := range []string{"side by side", "normal"} {
		row := m.screenLine(m.screen[0], true) // the unchanged line
		if strings.Contains(row, "\x1b[7m") || !strings.Contains(row, "\x1b[1m") {
			t.Errorf("%s: cursor line %q, want bold and no reverse video", layout, row)
		}
		if plain := m.screenLine(m.screen[0], false); strings.Contains(plain, "\x1b[1m") {
			t.Errorf("%s: line without cursor is bold: %q", layout, plain)
		}
		press(m, "s")
	}
}
