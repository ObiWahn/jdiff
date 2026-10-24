package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// numberedList returns a list of n items, f00 to f(n-1), height rows high
// without the preview box, and the function moving its cursor.
func numberedList(n, height int) (*list, func(delta int)) {
	off := false
	li := &list{n: n, preview: &off, width: 40, height: height}
	return li, li.moveCursor
}

// listRows returns the rows of the list li draws, between the heading and
// the status line, without styles.
func listRows(li *list) []string {
	item := func(i int, _ func(lipgloss.Style) lipgloss.Style) string { return fmt.Sprintf("f%02d", i) }
	out := strings.Split(ansi.Strip(li.render(config.Default().Theme, "head", item, nil, "status")), "\n")
	return out[2 : len(out)-1]
}

// rows returns the expected rows: an arrow line when up or down hide items,
// and the items from first to last.
func rows(up, first, last, down int) []string {
	var out []string
	if up > 0 {
		out = append(out, fmt.Sprintf("  ↑ %d more", up))
	}
	for i := first; i <= last; i++ {
		out = append(out, fmt.Sprintf("f%02d", i))
	}
	if down > 0 {
		out = append(out, fmt.Sprintf("  ↓ %d more", down))
	}
	return out
}

func TestListArrows(t *testing.T) {
	li, move := numberedList(30, 13) // 10 rows for 30 items
	check := func(step string, want []string) {
		t.Helper()
		if got := listRows(li); !slices.Equal(got, want) {
			t.Errorf("%s (cursor f%02d):\n got %q\nwant %q", step, li.cursor, got, want)
		}
	}
	check("start", rows(0, 0, 8, 21))
	move(8)
	check("on the last item above the arrow", rows(0, 0, 8, 21))
	move(1)
	check("one further: both arrows", rows(2, 2, 9, 20))
	move(-7)
	check("back up to the first item below the arrow", rows(2, 2, 9, 20))
	move(-1)
	check("up past the arrow: f00 instead of an arrow for it", rows(0, 0, 8, 21))
	move(far)
	check("the end", rows(21, 21, 29, 0))
	move(-8)
	check("the first item below the arrow", rows(21, 21, 29, 0))
	move(-1)
	check("up past the arrow at the end", rows(20, 20, 27, 2))
	move(-far)
	check("the top", rows(0, 0, 8, 21))
}

func TestListArrowsEdges(t *testing.T) {
	for _, tt := range []struct {
		name      string
		n, height int
		moves     []int
		want      []string
	}{
		{"all fit: no arrows", 10, 13, []int{far}, rows(0, 0, 9, 0)},
		{"one more than fits: an arrow for two", 11, 13, nil, rows(0, 0, 8, 2)},
		{"one more than fits, at the end", 11, 13, []int{far}, rows(2, 2, 10, 0)},
		{"two more than fits", 12, 13, []int{9}, rows(2, 2, 9, 2)},
		{"three rows: arrow, item, arrow", 30, 6, []int{2}, rows(2, 2, 2, 27)},
		{"three rows at the top", 30, 6, []int{1}, rows(0, 0, 1, 28)},
		{"three rows at the end", 30, 6, []int{far}, rows(28, 28, 29, 0)},
		{"two rows: no arrows", 30, 5, []int{5}, rows(0, 4, 5, 0)},
		{"one row: no arrows", 30, 4, []int{5}, rows(0, 5, 5, 0)},
	} {
		li, move := numberedList(tt.n, tt.height)
		for _, d := range tt.moves {
			move(d)
		}
		if got := listRows(li); !slices.Equal(got, tt.want) {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, got, tt.want)
		}
	}
}

// checkWindow checks the rows of li, h rows high, as checkShown does.
func checkWindow(t *testing.T, li *list, h int, before []int) []int {
	t.Helper()
	index := func(r string) int {
		var k int
		fmt.Sscanf(r, "f%d", &k)
		return k
	}
	return checkShown(t, "list", listRows(li), h, li.n, li.cursor, before, index)
}

// checkShown checks rows, the h rows of a window over n items with the
// cursor on item cursor, after the cursor moved from a window that showed
// the items before; index returns the item a row shows. The cursor's item
// is shown, not under an arrow; an arrow says how many items it hides, two
// or more; the items between are consecutive; the window is full; and it
// moved only when the cursor's item was not in it. It returns the items
// shown.
func checkShown(t *testing.T, what string, rows []string, h, n, cursor int, before []int, index func(string) int) []int {
	t.Helper()
	fail := func(why string) { t.Fatalf("%s, n %d, %d rows, cursor %d: %s\n%q", what, n, h, cursor, why, rows) }
	if len(rows) != h {
		fail("not h rows")
	}
	var items []int
	up, down := -1, -1
	for i, r := range rows {
		switch {
		case r == "":
			if i < len(rows)-1 && rows[i+1] != "" || n > h {
				fail("an empty row inside a full window")
			}
		case strings.HasPrefix(r, "  ↑ "):
			if i != 0 || up >= 0 {
				fail("↑ not in the first row")
			}
			fmt.Sscanf(r, "  ↑ %d more", &up)
		case strings.HasPrefix(r, "  ↓ "):
			if i != len(rows)-1 {
				fail("↓ not in the last row")
			}
			fmt.Sscanf(r, "  ↓ %d more", &down)
		default:
			k := index(r)
			if len(items) > 0 && k != items[len(items)-1]+1 {
				fail("items not consecutive")
			}
			items = append(items, k)
		}
	}
	if !slices.Contains(items, cursor) {
		fail("the cursor's item is not shown")
	}
	if (up >= 0 || down >= 0) && h < 3 {
		fail("arrows in fewer than three rows")
	}
	arrows := h >= 3 // fewer rows hide items without arrows
	if up >= 0 && (up < 2 || up != items[0]) || arrows && up < 0 && items[0] != 0 {
		fail("the arrow above is wrong or missing")
	}
	last := items[len(items)-1]
	if down >= 0 && (down < 2 || down != n-1-last) || arrows && down < 0 && last != n-1 {
		fail("the arrow below is wrong or missing")
	}
	if before != nil && slices.Contains(before, cursor) && !slices.Equal(before, items) {
		fail(fmt.Sprintf("moved, though the cursor's item was shown in %v", before))
	}
	return items
}

// sweepKeys are the keys a sweep presses after the start: down to the end
// one at a time and back up, then pages and the ends.
func sweepKeys(n int) []string {
	keys := append(slices.Repeat([]string{"j"}, n), slices.Repeat([]string{"k"}, n)...)
	return append(keys, "pgdown", "pgdown", "pgup", "G", "pgup", "k", "g", "pgdown", "j")
}

func TestFileListArrowsAllHeights(t *testing.T) {
	t.Parallel() // a sweep: its own config and views, nothing shared
	var files []source.File
	for i := range 30 {
		files = append(files, source.File{Path: fmt.Sprintf("f%02d.go", i), Old: []string{"a"}, New: []string{"b"},
			InOld: true, InNew: true})
	}
	index := func(r string) int {
		var k int
		fmt.Sscanf(r[strings.LastIndex(r, " ")+1:], "f%d.go", &k)
		return k
	}
	for _, preview := range []bool{false, true} {
		for height := minHeight; height <= 30; height++ {
			a := NewApp(config.Default(), files)
			a.cfg.Settings.Preview = preview
			a.Update(tea.WindowSizeMsg{Width: 40, Height: height})
			what := fmt.Sprintf("file list, height %d, preview %v", height, preview)
			h := a.listHeight()
			rows := func() []string { return viewRows(a.View(), 2, 2+h) }
			shown := checkShown(t, what, rows(), h, len(files), a.cursor, nil, index)
			for _, k := range sweepKeys(len(files)) {
				pressApp(a, k)
				shown = checkShown(t, what+", "+k, rows(), h, len(files), a.cursor, shown, index)
			}
		}
	}
}

func TestListArrowsEverywhere(t *testing.T) {
	t.Parallel() // a sweep: its own config and views, nothing shared
	// Up to 20 items in up to 12 rows: for every height there are fewer
	// items, as many, one and two more, and many more; and 1, 2 and 3 rows.
	for n := 1; n <= 20; n++ {
		for h := 1; h <= 12; h++ {
			li, move := numberedList(n, h+3)
			shown := checkWindow(t, li, h, nil)
			// Down to the end and back up one at a time, then in jumps.
			steps := append(slices.Repeat([]int{1}, n), slices.Repeat([]int{-1}, n)...)
			steps = append(steps, 7, -3, far, -5, 2, -far, h, -1, h-1)
			for _, d := range steps {
				move(d)
				shown = checkWindow(t, li, h, shown)
			}
		}
	}
}

// viewRows returns rows from to to of a view, without styles, trimmed.
func viewRows(view string, from, to int) []string {
	out := strings.Split(ansi.Strip(view), "\n")[from:to]
	for i, r := range out {
		out[i] = strings.TrimRight(r, " ")
	}
	return out
}

func TestFileListArrows(t *testing.T) {
	var files []source.File
	for i := range 30 {
		files = append(files, source.File{Path: fmt.Sprintf("f%02d.go", i), Old: []string{"a"}, New: []string{"b"},
			InOld: true, InNew: true})
	}
	a := NewApp(config.Default(), files)
	a.cfg.Settings.Preview = false
	a.Update(tea.WindowSizeMsg{Width: 60, Height: 13}) // 10 rows
	name := func(r string) string { return r[strings.LastIndex(r, " ")+1:] }
	check := func(step string, want ...string) {
		t.Helper()
		got := viewRows(a.View(), 2, 2+a.listHeight())
		for i := range got {
			if !strings.HasPrefix(got[i], "  ↑") && !strings.HasPrefix(got[i], "  ↓") {
				got[i] = name(got[i])
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", step, got, want)
		}
	}
	names := func(from, to int) []string {
		var out []string
		for i := from; i <= to; i++ {
			out = append(out, fmt.Sprintf("f%02d.go", i))
		}
		return out
	}
	check("start", append(names(0, 8), "  ↓ 21 more")...)
	pressApp(a, "j", "j", "j", "j", "j", "j", "j", "j", "j") // f09
	check("past the arrow", append(append([]string{"  ↑ 2 more"}, names(2, 9)...), "  ↓ 20 more")...)
	pressApp(a, "G")
	check("G", append([]string{"  ↑ 21 more"}, names(21, 29)...)...)

	// With the preview box the list takes at most half of the room.
	a.cfg.Settings.Preview = true
	a.scrollToCursor()
	if h := a.listHeight(); h != 5 {
		t.Fatalf("list with the box: %d rows, want 5", h)
	}
	check("G with the box", append([]string{"  ↑ 26 more"}, names(26, 29)...)...)
	// The window shrinks: the cursor's file stays in view.
	a.Update(tea.WindowSizeMsg{Width: 60, Height: 10}) // 3 rows, 3 for the box
	if h := a.listHeight(); h != 3 {
		t.Fatalf("smaller window: %d rows, want 3", h)
	}
	check("smaller window", "  ↑ 28 more", "f28.go", "f29.go")
	pressApp(a, "g")
	check("g in the smaller window", "f00.go", "f01.go", "  ↓ 28 more")
}

func TestDiffSwitchBackspaceCloses(t *testing.T) {
	s, g := newDiffSwitch(newTestApp())
	pressRoot(s, "d", "ab", "backspace")
	if s.input == nil || s.input.text != "a" {
		t.Fatalf("backspace on text: %v", s.input)
	}
	pressRoot(s, "backspace", "backspace")
	if s.input != nil || len(g.runs) != 0 {
		t.Errorf("backspace on nothing typed: open %v, runs %v; want closed, no git", s.input != nil, g.runs)
	}
}

func TestDiffSwitchErrorWidens(t *testing.T) {
	s, g := newDiffSwitch(newTestApp()) // 100 columns
	msg := "git diff: fatal: ambiguous argument 'HEAD~99': unknown revision or path not in the working tree"
	g.err = errors.New(msg + "\nmore")
	pressRoot(s, "d", "HEAD~99", "enter")
	if !strings.Contains(ansi.Strip(s.View()), msg[:96-4]) {
		t.Errorf("the message is cut before the screen's edge:\n%s", ansi.Strip(s.View()))
	}
}

func TestPopupTitleKeys(t *testing.T) {
	ansiColors(t)
	s, _ := newDiffSwitch(newTestApp())
	th := s.cfg.Theme
	pressRoot(s, "d")
	if !strings.Contains(s.View(), th.HintKey.Render("enter")+th.Hint.Render(" show")) {
		t.Error("the git diff popup's title without the key in the hint key style")
	}
	if got := popupTitle(th, "marks", "a problem", "enter jump"); strings.Contains(got, th.HintKey.Render("a")) ||
		!strings.Contains(got, th.Hint.Render("a problem")) {
		t.Errorf("a message in a title: %q", got)
	}
}
