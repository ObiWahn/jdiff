package ui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/diff"
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
	cfg, err := config.Load(dir, nil, "", "")
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

// newRelistApp returns the file selector of a.go, b.go and c.bin that lists
// the files *files on R, or fails with *err.
func newRelistApp(files *[]source.File, err *error) *App {
	*files = []source.File{
		{Path: "a.go", Old: numbered("x", 10), New: numbered("x", 10)[1:], InOld: true, InNew: true},
		{Path: "b.go", New: []string{"new"}, InNew: true},
		{Path: "c.bin", InOld: true, InNew: true, Binary: true},
	}
	a := NewApp(config.Default(), *files).WithRelist(func() ([]source.File, error) { return *files, *err })
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return a
}

func TestAppRelist(t *testing.T) {
	var files []source.File
	var err error
	a := newRelistApp(&files, &err)
	pressApp(a, "enter")
	a.open.view.addAnchor(diff.Anchor{Left: 5, Right: 4}, right, shape{})
	view, d := a.open.view, a.files[0].diff

	// A new file before a.go, b.go gone, a.go with a line on top.
	files = []source.File{
		{Path: "0.go", New: []string{"zero"}, InNew: true},
		{Path: "a.go", Old: numbered("x", 10), New: append([]string{"top"}, numbered("x", 10)[1:]...), InOld: true, InNew: true},
		files[2],
	}
	pressApp(a, "R")
	if a.open == nil || a.open.Path != "a.go" || a.open.view != view || a.cursor != 1 {
		t.Fatalf("after R: open %v cursor %d; want a.go still open in its view, cursor on it", a.open, a.cursor)
	}
	if view.msg != "1 new, 1 gone, 1 changed" {
		t.Errorf("msg %q", view.msg)
	}
	if want := []diff.Anchor{{Left: 5, Right: 5}}; !slices.Equal(d.anchors, want) {
		t.Errorf("anchors %v, want %v: the anchor follows its line", d.anchors, want)
	}
	var paths []string
	for _, e := range a.files {
		paths = append(paths, e.Path)
	}
	if want := []string{"0.go", "a.go", "c.bin"}; !slices.Equal(paths, want) || a.list.n != 3 {
		t.Errorf("files %v (n %d), want %v", paths, a.list.n, want)
	}
	if a.files[1].added != 1 || a.files[1].removed != 1 {
		t.Errorf("a.go +%d -%d, want +1 -1", a.files[1].added, a.files[1].removed)
	}

	// Nothing changed; F5 does the same as R.
	pressApp(a, "f5")
	if view.msg != "nothing changed" {
		t.Errorf("F5 without changes: msg %q", view.msg)
	}

	// git fails: everything stays.
	err = errors.New("git diff: broken")
	pressApp(a, "R")
	if view.msg != "git diff: broken" || len(a.files) != 3 || a.open.view != view {
		t.Errorf("failed R: msg %q, %d files", view.msg, len(a.files))
	}
}

func TestAppRelistOpenFileGone(t *testing.T) {
	var files []source.File
	var err error
	a := newRelistApp(&files, &err)
	pressApp(a, "j", "enter")
	files = files[:1]
	pressApp(a, "R")
	if a.open != nil || a.cursor != 0 || a.msg != "b.go is gone; 2 gone" {
		t.Errorf("open %v cursor %d msg %q; want the list on a.go and the message", a.open, a.cursor, a.msg)
	}
}

func TestAppRelistBinaryCountsAsNew(t *testing.T) {
	var files []source.File
	var err error
	a := newRelistApp(&files, &err)
	files = slices.Clone(files)
	files[2] = source.File{Path: "c.bin", Old: []string{"text"}, New: []string{"text", "now"}, InOld: true, InNew: true}
	pressApp(a, "R")
	if e := a.files[2]; a.msg != "1 new, 1 gone" || e.Binary || e.diff == nil || e.added != 1 {
		t.Errorf("msg %q, c.bin %+v", a.msg, e)
	}
}

func TestAppRelistFindsMovesInNewFile(t *testing.T) {
	var files []source.File
	var err error
	a := newRelistApp(&files, &err)
	files = slices.Clone(files)
	files[0].Old = append(slices.Clone(movedBlock), files[0].Old...)
	pressApp(a, "R")
	if _, ok := a.files[0].diff.movedTo(left, 0); ok {
		t.Fatal("moved before the block was added anywhere")
	}
	files = append(files, source.File{Path: "d.go", New: movedBlock, InNew: true})
	pressApp(a, "R")
	to, ok := a.files[0].diff.movedTo(left, 0)
	if !ok || a.fileByGroup(to.File) != 3 {
		t.Errorf("a.go's removed block: moved %v to %+v, want into d.go", ok, to)
	}
}

func TestAppRelistFlowFollowsLine(t *testing.T) {
	var files []source.File
	var err error
	a := newRelistApp(&files, &err)
	a.cfg.Settings.CollapseUnchanged = false
	pressApp(a, "f", "j", "j", "j") // on a.go
	_, file, line := a.flow.position()
	d := a.files[file].diff
	before := d.lineAt(left, line)
	files = append([]source.File{{Path: "0.go", New: []string{"zero"}, InNew: true}}, files...)
	files[1].New = append([]string{"top"}, files[1].New...)
	pressApp(a, "R")
	_, file, line = a.flow.position()
	if file != 1 || a.files[file].diff != d || d.lineAt(left, line) != before || a.open != a.files[1] {
		t.Errorf("flow on file %d left line %d, want a.go, now file 1, line %d", file, d.lineAt(left, line), before)
	}
	if a.flow.msg != "1 new, 1 changed" {
		t.Errorf("flow msg %q", a.flow.msg)
	}
}

func TestAppWithoutRelist(t *testing.T) {
	a := newTestApp()
	pressApp(a, "R")
	if a.msg != "nothing to load again" {
		t.Errorf("msg %q", a.msg)
	}
}
