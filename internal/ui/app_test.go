package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/settings"
	"jdiff/internal/source"
	"jdiff/internal/theme"
)

func newTestApp() *App {
	a := NewApp(theme.Default(), settings.Default(), []source.File{
		{Path: "a.go", Old: []string{"x", "y"}, New: []string{"x", "z", "w"}, InOld: true, InNew: true},
		{Path: "b.go", New: []string{"new"}, InNew: true},
		{Path: "c.bin", InOld: true, InNew: true, Binary: true},
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	return a
}

func pressApp(a *App, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		// Run returned commands like the Bubble Tea runtime does.
		_, cmd := a.Update(msg)
		for cmd != nil {
			_, cmd = a.Update(cmd())
		}
	}
}

func TestAppListShowsCounts(t *testing.T) {
	a := newTestApp()
	view := ansi.Strip(a.View())
	for _, want := range []string{"+2 -1  a.go", "added:  +1     b.go", "binary", "3 files changed, +3, -1"} {
		if !strings.Contains(view, want) {
			t.Errorf("list view lacks %q:\n%s", want, view)
		}
	}
}

func TestAppOpenAndBack(t *testing.T) {
	a := newTestApp()
	pressApp(a, "j", "enter")
	if a.open == nil || a.open.Path != "b.go" {
		t.Fatalf("open = %v, want b.go", a.open)
	}
	if !strings.Contains(ansi.Strip(a.View()), "b/b.go") {
		t.Errorf("diff view not shown")
	}
	pressApp(a, "backspace")
	if a.open != nil {
		t.Fatalf("backspace did not return to the list")
	}
	if a.cursor != 1 {
		t.Errorf("cursor = %d after back, want 1", a.cursor)
	}
}

func TestAppKeepsAnchors(t *testing.T) {
	a := newTestApp()
	pressApp(a, "enter")
	v := a.open.view
	// Anchor old "y" to new "w" instead of the positional pairing with "z".
	v.cursor = v.viewOf[v.rowOf[left][1]]
	pressApp(a, "enter")
	v.cursor = v.viewOf[v.rowOf[right][2]]
	pressApp(a, "enter", "backspace", "enter")
	if len(a.open.view.anchors) != 1 {
		t.Fatalf("anchors = %v after reopening, want 1", a.open.view.anchors)
	}
	pressApp(a, "backspace")
	if e := a.files[0]; e.added != 2 || e.removed != 1 {
		t.Errorf("stats = +%d -%d after anchoring, want +2 -1", e.added, e.removed)
	}
}

func TestAppBinaryNotOpened(t *testing.T) {
	a := newTestApp()
	pressApp(a, "G", "enter")
	if a.open != nil || !strings.Contains(a.msg, "binary") {
		t.Errorf("binary file opened: open=%v msg=%q", a.open, a.msg)
	}
}

func TestAppKeyBurst(t *testing.T) {
	a := newTestApp()
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jj")})
	if a.cursor != 2 {
		t.Errorf("cursor = %d after a burst of 2 j, want 2", a.cursor)
	}
}

func TestAppNextPrevFile(t *testing.T) {
	a := newTestApp()
	pressApp(a, "enter", "N")
	if a.open == nil || a.open.Path != "b.go" || a.cursor != 1 {
		t.Fatalf("N: open %v cursor %d, want b.go", a.open, a.cursor)
	}
	// c.bin is binary and skipped: b.go is the last file that can be shown.
	pressApp(a, "N")
	if a.open.Path != "b.go" || !strings.Contains(a.open.view.msg, "last file") {
		t.Errorf("N on the last file: open %s msg %q", a.open.Path, a.open.view.msg)
	}
	pressApp(a, "P")
	if a.open.Path != "a.go" || a.cursor != 0 {
		t.Errorf("P: open %s cursor %d, want a.go", a.open.Path, a.cursor)
	}
	// Without a log, n and p do nothing.
	pressApp(a, "n")
	if a.open.Path != "a.go" {
		t.Errorf("n outside the log switched to %s", a.open.Path)
	}
}

func TestAppSingleFileOpensDirectly(t *testing.T) {
	a := NewApp(theme.Default(), settings.Default(), []source.File{
		{Path: "only.go", Old: []string{"x"}, New: []string{"y"}, InOld: true, InNew: true},
	})
	if a.open == nil || a.open.Path != "only.go" {
		t.Fatalf("single file not opened directly")
	}
	// Outside the log, backspace still shows the list.
	pressApp(a, "backspace")
	if a.open != nil {
		t.Errorf("backspace did not show the list")
	}
}

func TestAppRenameLabel(t *testing.T) {
	a := NewApp(theme.Default(), settings.Default(), []source.File{
		{Path: "new.go", OldPath: "old.go", Old: []string{"x"}, New: []string{"x", "y"}, InOld: true, InNew: true},
		{Path: "other.go", New: []string{"z"}, InNew: true},
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	if view := ansi.Strip(a.View()); !strings.Contains(view, "renamed:") || !strings.Contains(view, "old.go → new.go") {
		t.Errorf("rename not shown:\n%s", view)
	}
	pressApp(a, "enter")
	if a.open.view.names != [2]string{"a/old.go", "b/new.go"} {
		t.Errorf("diff titles = %v", a.open.view.names)
	}
}
