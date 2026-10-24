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

// fakeGitDiff records the arguments git diff runs with and returns a file
// named after them, or err when set.
type fakeGitDiff struct {
	runs [][]string
	err  error
}

func (g *fakeGitDiff) diff(args []string) ([]source.File, error) {
	g.runs = append(g.runs, args)
	if g.err != nil {
		return nil, g.err
	}
	name := strings.Join(append([]string{"diff"}, args...), "_") + ".go"
	return []source.File{
		{Path: name, Old: []string{"a"}, New: []string{"b"}, InOld: true, InNew: true},
		{Path: "other.go", Old: []string{"x"}, New: []string{"y"}, InOld: true, InNew: true},
	}, nil
}

// newDiffSwitch returns the switch around view, wrapped in the help popup.
func newDiffSwitch(view tea.Model) (*DiffSwitch, *fakeGitDiff) {
	g := &fakeGitDiff{}
	wrap := func(m tea.Model) tea.Model { return WithHelp(config.Default(), m) }
	s := WithDiffSwitch(config.Default(), view, wrap, g.diff)
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	return s, g
}

// shownApp returns the file selector s shows, failing when it shows another
// view.
func shownApp(t *testing.T, s *DiffSwitch) *App {
	t.Helper()
	a, ok := s.view.(*App)
	if !ok {
		t.Fatalf("shows %T, want the file selector", s.view)
	}
	return a
}

func TestDiffSwitchTyped(t *testing.T) {
	g := &fakeGitDiff{}
	start := NewApp(config.Default(), []source.File{
		{Path: "a.go", Old: []string{"x"}, New: []string{"y"}, InOld: true, InNew: true},
		{Path: "b.go", Old: []string{"x"}, New: []string{"y"}, InOld: true, InNew: true},
	}).WithGitDiff([]string{"--cached"}, g.diff)
	s, _ := newDiffSwitch(start)
	s.gitDiff = g.diff

	pressRoot(s, "d")
	if popup := ansi.Strip(s.View()); s.input == nil ||
		!strings.Contains(popup, "╭─ git diff  enter show · ↑ current · esc cancel") || strings.Contains(popup, "working tree") {
		t.Errorf("d: popup\n%s", popup)
	}
	// ? goes into the line, not to the help popup around the view.
	pressRoot(s, "HEAD~3 HEAD", "?", "backspace")
	if line := popupLine(t, s, "HEAD~3 HEAD█"); strings.TrimSpace(line) != "HEAD~3 HEAD█" {
		t.Errorf("typed: line %q", line)
	}
	pressRoot(s, "enter")
	a := shownApp(t, s)
	if a == start || s.input != nil || len(g.runs) != 1 || !slices.Equal(g.runs[0], []string{"HEAD~3", "HEAD"}) {
		t.Fatalf("enter: runs %v, a new view %v", g.runs, a != start)
	}
	view := ansi.Strip(s.View())
	for _, want := range []string{"git diff HEAD~3 HEAD", "diff_HEAD~3_HEAD.go", "git diff HEAD~3 HEAD: 2 files"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	// R runs the new arguments again; up takes them to edit.
	pressRoot(s, "R")
	if len(g.runs) != 2 || !slices.Equal(g.runs[1], []string{"HEAD~3", "HEAD"}) {
		t.Errorf("R after the switch: runs %v", g.runs)
	}
	pressRoot(s, "d", "up")
	if s.input.text != "HEAD~3 HEAD" {
		t.Errorf("up: %q, want the current arguments", s.input.text)
	}
	pressRoot(s, "esc")
	if s.input != nil || len(g.runs) != 2 || shownApp(t, s) != a || strings.Contains(s.View(), "╭─ git diff") {
		t.Errorf("esc: still asking %v, runs %v", s.input != nil, g.runs)
	}
	// enter on the empty line changes nothing.
	pressRoot(s, "d", "enter")
	if s.input != nil || len(g.runs) != 2 || shownApp(t, s) != a {
		t.Errorf("enter on the empty line: still asking %v, runs %v", s.input != nil, g.runs)
	}
}

// popupLine returns the line of the popup of s that holds want, without
// the borders, its spaces kept, failing when there is none.
func popupLine(t *testing.T, s *DiffSwitch, want string) string {
	t.Helper()
	for _, row := range strings.Split(ansi.Strip(s.View()), "\n") {
		if strings.Contains(row, want) {
			_, in, _ := strings.Cut(row, "│ ")
			in, _, _ = strings.Cut(in, " │")
			return in
		}
	}
	t.Fatalf("no popup line holds %q:\n%s", want, ansi.Strip(s.View()))
	return ""
}

func TestDiffSwitchPopup(t *testing.T) {
	s, g := newDiffSwitch(newTestApp()) // 100 columns
	pressRoot(s, "d")
	if strings.Contains(s.View(), "↑ current") {
		t.Error("offers the current arguments without a git diff shown")
	}
	// The line is centered in the popup, at least minInputWidth wide.
	pressRoot(s, "wip")
	line := popupLine(t, s, "wip█")
	if ansi.StringWidth(line) != minInputWidth || strings.TrimSpace(line) != "wip█" ||
		len(line)-len(strings.TrimLeft(line, " ")) != (minInputWidth-4)/2 {
		t.Errorf("short line %q, want it centered in %d columns", line, minInputWidth)
	}
	// A longer one widens the popup.
	long := "feature/" + strings.Repeat("x", 50) + " HEAD"
	pressRoot(s, "ctrl+u", long)
	if line := popupLine(t, s, "HEAD█"); strings.TrimSpace(line) != long+"█" {
		t.Errorf("long line %q, want all of it", line)
	}
	// Past the screen, the start is cut and the end stays in view.
	pressRoot(s, " "+strings.Repeat("y", 60))
	line = popupLine(t, s, "y█")
	if ansi.StringWidth(line) != 100-4 || !strings.HasSuffix(line, "yyy█") || strings.Contains(line, "feature") {
		t.Errorf("line past the screen %q, want its end in 96 columns", line)
	}
	for i, row := range strings.Split(s.View(), "\n") {
		if w := ansi.StringWidth(row); w > 100 {
			t.Errorf("row %d is %d columns wide, more than the screen", i, w)
		}
	}
	// Letters are text: w, i and h too.
	pressRoot(s, "ctrl+u", "w", "enter")
	if len(g.runs) != 1 || !slices.Equal(g.runs[0], []string{"w"}) {
		t.Errorf("w: runs %q", g.runs)
	}
}

func TestDiffSwitchFromLogAndTwoFiles(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "enter", "enter") // c3, a.go
	s, g := newDiffSwitch(l)
	pressRoot(s, "d")
	if strings.Contains(s.View(), "current") {
		t.Error("the log offers its current git diff arguments")
	}
	pressRoot(s, "HEAD", "enter")
	if a := shownApp(t, s); a.inLog() || a.gitDiff == nil || len(g.runs) != 1 {
		t.Errorf("from the log: in log %v, runs %v", a.inLog(), g.runs)
	}
	// backspace stays in the file list: the log is gone.
	pressRoot(s, "backspace")
	shownApp(t, s)

	s, g = newDiffSwitch(newTestModel([]string{"a"}, []string{"b"}))
	pressRoot(s, "d", "--cached", "enter")
	if a := shownApp(t, s); !slices.Equal(a.diffArgs, []string{"--cached"}) || len(g.runs) != 1 {
		t.Errorf("from two files: args %v runs %v", a.diffArgs, g.runs)
	}
}

func TestDiffSwitchFails(t *testing.T) {
	m := newTestModel([]string{"a"}, []string{"b"})
	s, g := newDiffSwitch(m)
	g.err = errors.New("git diff: fatal: bad revision 'HEAD~99'\nmore lines of git")
	pressRoot(s, "d", "HEAD~99", "enter")
	// The popup stays with the line typed, to be fixed, and git's first line.
	view := ansi.Strip(s.View())
	if s.view != m || s.input == nil || !strings.Contains(view, " HEAD~99█ ") ||
		!strings.Contains(view, "bad revision 'HEAD~99'") || strings.Contains(view, "more lines") {
		t.Fatalf("failed git: shows %T, popup\n%s", s.view, view)
	}
	// The message goes with the next key; esc closes the popup, the view stays.
	pressRoot(s, "backspace")
	if strings.Contains(s.View(), "bad revision") {
		t.Error("the message stays after the next key")
	}
	pressRoot(s, "esc")
	if s.input != nil || s.view != m {
		t.Errorf("esc after the failure: still asking %v, shows %T", s.input != nil, s.view)
	}
}
