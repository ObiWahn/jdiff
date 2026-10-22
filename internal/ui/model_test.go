package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/diff"
)

func newTestModel(a, b []string) *Model {
	m := New("old", "new", a, b)
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
	m.cursor = m.rowOf[s][line]
}

func TestEnterMarksAndJumpsToOtherPane(t *testing.T) {
	m := newTestModel([]string{"a", "b"}, []string{"a", "b"})
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
