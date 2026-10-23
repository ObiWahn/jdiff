package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/source"
)

func newTestApp() *App {
	a := NewApp(config.Default(), []source.File{
		{Path: "a.go", Old: []string{"x", "y"}, New: []string{"x", "z", "w"}, InOld: true, InNew: true},
		{Path: "b.go", New: []string{"new"}, InNew: true},
		{Path: "c.bin", InOld: true, InNew: true, Binary: true},
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	return a
}

func pressApp(a *App, keys ...string) { pressRoot(a, keys...) }

func TestAppListShowsCounts(t *testing.T) {
	a := newTestApp()
	view := ansi.Strip(a.View())
	for _, want := range []string{"+2 -1  a.go", "added:  +1     b.go", "binary", " 3 files changed  +3 -1\n\n"} {
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
	pressApp(a, "enter", "n")
	if a.open == nil || a.open.Path != "b.go" || a.cursor != 1 {
		t.Fatalf("n: open %v cursor %d, want b.go", a.open, a.cursor)
	}
	// c.bin is binary and skipped: b.go is the last file that can be shown.
	pressApp(a, "n")
	if a.open.Path != "b.go" || !strings.Contains(a.open.view.msg, "last file") {
		t.Errorf("n on the last file: open %s msg %q", a.open.Path, a.open.view.msg)
	}
	pressApp(a, "p")
	if a.open.Path != "a.go" || a.cursor != 0 {
		t.Errorf("p: open %s cursor %d, want a.go", a.open.Path, a.cursor)
	}
	// Without a log, N and P do nothing.
	pressApp(a, "N")
	if a.open.Path != "a.go" {
		t.Errorf("N outside the log switched to %s", a.open.Path)
	}
}

func TestAppSingleFileOpensDirectly(t *testing.T) {
	a := NewApp(config.Default(), []source.File{
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
	a := NewApp(config.Default(), []source.File{
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

func TestAppListNextPrev(t *testing.T) {
	a := newTestApp()
	pressApp(a, "n", "n")
	if a.open != nil || a.cursor != 2 {
		t.Errorf("n n in the list: open %v cursor %d, want c.bin selected", a.open, a.cursor)
	}
	pressApp(a, "p")
	if a.cursor != 1 {
		t.Errorf("p in the list: cursor %d, want b.go", a.cursor)
	}
}

func TestSpaceDoesNotNavigateLists(t *testing.T) {
	a := newTestApp()
	pressApp(a, " ")
	if a.cursor != 0 || a.open != nil {
		t.Errorf("space in the file list: cursor %d open %v", a.cursor, a.open)
	}
	l, _ := newTestLog()
	pressLog(l, " ")
	if l.cursor != 0 || l.open != nil {
		t.Errorf("space in the commit list: cursor %d open %v", l.cursor, l.open)
	}
}

func TestTogglesSaveState(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp(cfg, []source.File{{Path: "a.go", Old: []string{"x"}, New: []string{"y"}, InOld: true, InNew: true},
		{Path: "b.go", New: []string{"new"}, InNew: true}})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressApp(a, "v", "s", "w", "f")
	data, err := os.ReadFile(filepath.Join(dir, "state.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"flow = true", "side_by_side = false", "preview = false", "show_whitespaces = true"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("state.toml lacks %q:\n%s", want, data)
		}
	}
}
