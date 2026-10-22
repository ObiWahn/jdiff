package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/diff"
	"jdiff/internal/settings"
	"jdiff/internal/theme"
)

func newTestModel(a, b []string) *Model {
	m := New(theme.Default(), settings.Default(), "old", "new", a, b)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m.Update(msg)
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
	if got, want := folds(m), []vrow{{0, 17}, {24, 17}}; !slices.Equal(got, want) {
		t.Fatalf("folds = %v, want %v", got, want)
	}
	if len(m.view) != 9 {
		t.Errorf("%d displayed rows, want 9", len(m.view))
	}
	if !strings.Contains(ansi.Strip(m.View()), "17 unchanged lines") {
		t.Errorf("fold row not rendered:\n%s", ansi.Strip(m.View()))
	}

	// Space unfolds the whole file and keeps the cursor on its row.
	press(m, "n")
	changeRow := m.view[m.cursor].row
	press(m, " ")
	if len(folds(m)) != 0 || len(m.view) != len(m.rows) {
		t.Fatalf("space did not unfold: %d folds", len(folds(m)))
	}
	if m.view[m.cursor].row != changeRow {
		t.Errorf("cursor moved from row %d to %d", changeRow, m.view[m.cursor].row)
	}

	// Space again folds; a cursor inside a fold lands on the fold row.
	press(m, "g", " ")
	if m.cursor != 0 || m.view[0].hidden == 0 {
		t.Errorf("cursor %d not on the first fold %v", m.cursor, m.view[0])
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
	if got, want := folds(m), []vrow{{0, 12}, {19, 11}}; !slices.Equal(got, want) {
		t.Errorf("folds = %v, want %v", got, want)
	}
}

func TestCollapseUnchangedSetting(t *testing.T) {
	st := settings.Default()
	st.CollapseUnchanged = false
	a, b := numbered("x", 30), append(numbered("x", 30), "new")
	m := New(theme.Default(), st, "old", "new", a, b)
	if len(folds(m)) != 0 {
		t.Errorf("collapse_unchanged=false started folded")
	}
	press(m, " ")
	if len(folds(m)) != 1 {
		t.Errorf("space did not fold: %v", m.view)
	}
}
