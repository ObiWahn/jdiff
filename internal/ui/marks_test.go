package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/source"
)

// newMarkModel returns the view of 40 lines with line 5 changed, the
// unchanged lines folded.
func newMarkModel() *Model {
	b := numbered("x", 40)
	b[4] = "changed"
	return newTestModel(numbered("x", 40), b)
}

func TestMarkSetAndJump(t *testing.T) {
	m := newMarkModel()
	press(m, "ctrl+space") // unfold all
	moveTo(t, m, right, 30)
	press(m, "m", "a")
	if m.msg != "mark a: new:31" {
		t.Fatalf("m a: msg %q", m.msg)
	}
	if mk, ok := m.cfg.Mark("a"); !ok || mk != (config.Mark{Path: "new", Line: 31, Text: "x30"}) {
		t.Errorf("mark a = %+v, %v", mk, ok)
	}
	press(m, "ctrl+space", "g") // fold all again, to the top
	press(m, "'", "a")
	if m.lineAt(right, m.cursor) != 30 || m.msg != "mark a" {
		t.Errorf("' a: right line %d, msg %q; want 30 and mark a, its fold opened", m.lineAt(right, m.cursor), m.msg)
	}
	// A letter key after anything else is the key itself again.
	press(m, "m", "1")
	if _, ok := m.cfg.Mark("1"); ok || m.msg != "" {
		t.Errorf("m 1: msg %q, want no mark and no message", m.msg)
	}
}

func TestMarkMessages(t *testing.T) {
	m := newTestModel([]string{"keep", "gone", "stay"}, []string{"keep", "stay"})
	moveTo(t, m, left, 1) // the removed line
	press(m, "m", "b")
	if _, ok := m.cfg.Mark("b"); ok || m.msg != "no line of the new file here" {
		t.Errorf("m b on a removed line: msg %q", m.msg)
	}
	press(m, "'", "q")
	if m.msg != "no mark q" {
		t.Errorf("' q: msg %q", m.msg)
	}
	if err := m.cfg.SetMark("z", config.Mark{Path: "other.go", Line: 1, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	press(m, "'", "z")
	if m.msg != "mark z: other.go is not in this diff" {
		t.Errorf("' z: msg %q", m.msg)
	}
}

func TestMarkFindsMovedLine(t *testing.T) {
	m := newMarkModel()
	press(m, "ctrl+space")
	moveTo(t, m, right, 30)
	press(m, "m", "a")
	// Three lines inserted above: the line is found by its text.
	m.reload(right, append([]string{"n1", "n2", "n3"}, m.lines[right]...))
	press(m, "g", "'", "a")
	if m.lineAt(right, m.cursor) != 33 || m.msg != "mark a" {
		t.Errorf("after an insert: right line %d, msg %q; want 33", m.lineAt(right, m.cursor), m.msg)
	}
	// Its text gone: the old line number, with a note.
	lines := slices.Clone(m.lines[right])
	lines[33] = "rewritten"
	m.reload(right, lines)
	press(m, "g", "'", "a")
	if m.lineAt(right, m.cursor) != 30 || m.msg != "mark a: line changed" {
		t.Errorf("text gone: right line %d, msg %q; want 30 and the note", m.lineAt(right, m.cursor), m.msg)
	}
}

func TestMarkOpensFileInApp(t *testing.T) {
	a := NewApp(config.Default(), []source.File{
		modified("a.go", numbered("a", 30), append(numbered("a", 30), "added")),
		modified("b.go", numbered("b", 30), append(numbered("b", 30), "added")),
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressApp(a, "enter", "ctrl+space")
	moveTo(t, a.open.view, right, 12)
	pressApp(a, "m", "a", "backspace")
	// From the list.
	pressApp(a, "j", "'", "a")
	if a.open == nil || a.open.Path != "a.go" || a.open.view.lineAt(right, a.open.view.cursor) != 12 {
		t.Fatalf("' a in the list: open %v", a.open)
	}
	// From the other file's diff view.
	pressApp(a, "n")
	if a.open.Path != "b.go" {
		t.Fatalf("n: %s", a.open.Path)
	}
	pressApp(a, "'", "a")
	if m := a.open.view; a.open.Path != "a.go" || m.lineAt(right, m.cursor) != 12 || m.msg != "mark a" {
		t.Errorf("' a in b.go: file %s, right line %d, msg %q", a.open.Path, m.lineAt(right, m.cursor), m.msg)
	}
}

func TestFlowMarkStaysInCommit(t *testing.T) {
	l, _ := newTestLog() // c3: a.go 1 -> 2, b.go x -> y; c2: a.go 0 -> 1
	pressLog(l, "f", "n", "j")
	f := l.flow
	if r := f.rows[f.cursor]; r.kind != flowDiff || r.part != 0 || r.file != 0 {
		t.Fatalf("not on the diff row of a.go in c3: %+v", r)
	}
	pressLog(l, "m", "a")           // a.go line 1, "2"
	pressLog(l, "n", "j", "m", "b") // b.go line 1, "y"
	pressLog(l, "N")                // c2, which changes a.go only
	pressLog(l, "'", "b")
	if f.rows[f.cursor].part != 1 || f.msg != "mark b: b.go is not in this diff" {
		t.Errorf("' b in c2: on commit %d, msg %q; want to stay in c2", f.rows[f.cursor].part, f.msg)
	}
	pressLog(l, "'", "a")
	if r := f.rows[f.cursor]; r.part != 1 || r.kind != flowDiff || r.file != 0 || f.msg != "mark a: line changed" {
		t.Errorf("' a in c2: row %+v, msg %q; want a.go of c2, whose line 1 is now \"1\"", r, f.msg)
	}
}

func TestMarksPopup(t *testing.T) {
	m := newMarkModel()
	p := WithMarks(m.cfg, m)
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressRoot(p, "\\")
	if view := ansi.Strip(p.View()); !strings.Contains(view, "no marks") {
		t.Errorf("empty popup:\n%s", view)
	}
	pressRoot(p, "esc", "ctrl+space")
	moveTo(t, m, right, 10)
	pressRoot(p, "m", "a")
	moveTo(t, m, right, 30)
	pressRoot(p, "m", "b", "g")

	pressRoot(p, "\\")
	view := ansi.Strip(p.View())
	for _, want := range []string{"╭─ marks ", "a  new:11  x10", "b  new:31  x30"} {
		if !strings.Contains(view, want) {
			t.Errorf("popup lacks %q:\n%s", want, view)
		}
	}
	// Keys do not reach the page while the popup is open.
	pressRoot(p, "j", "d")
	if _, ok := m.cfg.Mark("b"); ok || m.cursor != 0 {
		t.Errorf("j d: mark b %v, page cursor %d; want b deleted and the page untouched", ok, m.cursor)
	}
	pressRoot(p, "enter")
	if p.open || m.lineAt(right, m.cursor) != 10 || m.msg != "mark a" {
		t.Errorf("enter: popup open %v, right line %d, msg %q; want closed on mark a", p.open, m.lineAt(right, m.cursor), m.msg)
	}
	pressRoot(p, "\\", "q")
	if p.open || strings.Contains(ansi.Strip(p.View()), "╭─ marks") {
		t.Error("q did not close the popup")
	}
}

func TestMarksPopupJumpsToAnotherFile(t *testing.T) {
	a := NewApp(config.Default(), []source.File{
		modified("a.go", numbered("a", 30), append(numbered("a", 30), "added")),
		modified("b.go", numbered("b", 30), append(numbered("b", 30), "added")),
	})
	p := WithMarks(a.cfg, a)
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if err := a.cfg.SetMark("k", config.Mark{Path: "b.go", Line: 13, Text: "b12"}); err != nil {
		t.Fatal(err)
	}
	pressRoot(p, "enter", "\\", "enter") // in a.go, then the mark in b.go
	if a.open.Path != "b.go" || a.open.view.lineAt(right, a.open.view.cursor) != 12 {
		t.Errorf("enter on a mark in b.go: file %s, line %d", a.open.Path, a.open.view.lineAt(right, a.open.view.cursor))
	}
}
