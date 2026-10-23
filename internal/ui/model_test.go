package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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

func folds(m *Model) []vrow {
	var out []vrow
	for _, v := range m.view {
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
	if got, want := folds(m), []vrow{{0, 17, 0}, {24, 17, 1}}; !slices.Equal(got, want) {
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
	if got, want := folds(m), []vrow{{0, 17, 0}}; !slices.Equal(got, want) {
		t.Fatalf("after space: folds = %v, want only the upper one", got)
	}
	if m.view[m.cursor].row != changeRow {
		t.Errorf("cursor moved from row %d to %d", changeRow, m.view[m.cursor].row)
	}
	// Space again closes the same block.
	press(m, " ")
	if got := len(folds(m)); got != 2 {
		t.Fatalf("after second space: %d folds, want 2", got)
	}

	// On a fold row, space opens that fold; inside an open block, space
	// closes it and the cursor lands on the fold row.
	press(m, "g", " ")
	if got, want := folds(m), []vrow{{24, 17, 1}}; !slices.Equal(got, want) {
		t.Fatalf("after space on the first fold: folds = %v, want %v", got, want)
	}
	press(m, "j", "j", " ")
	if m.cursor != 0 || m.view[0].hidden == 0 || len(folds(m)) != 2 {
		t.Errorf("closing from inside: cursor %d, view[0] %v, folds %v", m.cursor, m.view[0], folds(m))
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
	if got, want := folds(m), []vrow(nil); !slices.Equal(got, want) {
		t.Errorf("folds = %v, want none", got)
	}

	// An anchored unchanged line stays visible with its context.
	a, b = numbered("x", 30), numbered("x", 30)
	m = newTestModel(a, b)
	m.anchors = []diff.Anchor{{Left: 15, Right: 15}}
	m.realign()
	if got, want := folds(m), []vrow{{0, 12, 0}, {19, 11, 1}}; !slices.Equal(got, want) {
		t.Errorf("folds = %v, want %v", got, want)
	}
}

func TestCollapseUnchangedSetting(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.CollapseUnchanged = false
	a, b := numbered("x", 30), append(numbered("x", 30), "new")
	m := New(cfg, "old", "new", a, b)
	if len(folds(m)) != 0 || len(m.blocks) != 1 {
		t.Errorf("collapse_unchanged=false: %d folds, %d blocks, want 0 and 1", len(folds(m)), len(m.blocks))
	}
	press(m, " ")
	if len(folds(m)) != 1 {
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
	if got := screenRow(m); got != 7 || len(folds(m)) != 1 {
		t.Errorf("after folding below: screen row %d, %d folds, want 7 and 1", got, len(folds(m)))
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
		{"  x()", []diff.Span{{0, 2}}, false, "··x()"},
		{"\t\tx", []diff.Span{{1, 2}}, false, "    →   x"},
		{"a  b", []diff.Span{{1, 4}}, false, "a··b"},
		{"\tx y", nil, false, "    x y"},
		{"\tx y ", nil, true, "→   x·y·"},
		{"\t\tx", []diff.Span{{1, 2}}, true, "→   →   x"},
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
