package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	view := ansi.Strip(a.frame())
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
	if !strings.Contains(ansi.Strip(a.frame()), "b/b.go") {
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
	pressApp(a, "end", "enter")
	if a.open != nil || !strings.Contains(a.msg, "binary") {
		t.Errorf("binary file opened: open=%v msg=%q", a.open, a.msg)
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

// A single file has its list, as many do: enter opens it, backspace goes back.
func TestAppSingleFileShowsList(t *testing.T) {
	a := NewApp(config.Default(), []source.File{
		{Path: "only.go", Old: []string{"x"}, New: []string{"y"}, InOld: true, InNew: true},
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if a.open != nil {
		t.Fatalf("single file opened directly, want its list")
	}
	if frame := a.frame(); !strings.Contains(frame, "1 file changed") || !strings.Contains(frame, "only.go") {
		t.Fatalf("list of the single file:\n%s", frame)
	}
	pressApp(a, "enter")
	if a.open == nil || a.open.Path != "only.go" {
		t.Fatalf("enter did not open only.go")
	}
	pressApp(a, "backspace")
	if a.open != nil {
		t.Errorf("backspace did not show the list")
	}
}

// Labels are padded by the columns they take: the names stay aligned with wide
// characters in a label of the theme.
func TestAppListWideLabel(t *testing.T) {
	cfg := config.Default()
	cfg.Theme.ModifiedLabel, cfg.Theme.AddedLabel = "変更", "added"
	a := NewApp(cfg, []source.File{
		{Path: "f00", Old: []string{"x"}, New: []string{"y"}, InOld: true, InNew: true},
		{Path: "f01", New: []string{"y"}, InNew: true},
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	lines := topLines(a, 4)
	column := func(line, name string) int { return ansi.StringWidth(line[:strings.Index(line, name)]) }
	if c0, c1 := column(lines[2], "f00"), column(lines[3], "f01"); c0 != c1 {
		t.Errorf("names in columns %d and %d:\n%s\n%s", c0, c1, lines[2], lines[3])
	}
}

// Of an error of several lines, as git writes them, the status bar shows the
// first.
func TestAppRelistErrorFirstLine(t *testing.T) {
	a := NewApp(config.Default(), []source.File{
		{Path: "f00", New: []string{"x"}, InNew: true},
		{Path: "f01", New: []string{"x"}, InNew: true},
	}).WithRelist(func(context.Context) ([]source.File, error) { return nil, errors.New("x0\nx1") })
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	_, cmd := a.Update(refreshMsg{})
	a.Update(cmd()) // the listing, which runs in the background
	if a.msg != "x0" {
		t.Errorf("listing again: %q", a.msg)
	}
}

func TestAppRenameLabel(t *testing.T) {
	a := NewApp(config.Default(), []source.File{
		{Path: "new.go", OldPath: "old.go", Old: []string{"x"}, New: []string{"x", "y"}, InOld: true, InNew: true},
		{Path: "other.go", New: []string{"z"}, InNew: true},
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	if view := ansi.Strip(a.frame()); !strings.Contains(view, "renamed:") || !strings.Contains(view, "old.go → new.go") {
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
	setOption(a, a.cfg, "v", "s")
	setOption(a, a.cfg, "W")
	pressApp(a, "f")
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
// the files *files on r, or fails with *err.
func newRelistApp(files *[]source.File, err *error) *App {
	*files = []source.File{
		{Path: "a.go", Old: numbered("x", 10), New: numbered("x", 10)[1:], InOld: true, InNew: true},
		{Path: "b.go", New: []string{"new"}, InNew: true},
		{Path: "c.bin", InOld: true, InNew: true, Binary: true},
	}
	a := NewApp(config.Default(), *files).WithRelist(func(context.Context) ([]source.File, error) { return *files, *err })
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
	pressApp(a, "r")
	if a.open == nil || a.open.Path != "a.go" || a.open.view != view || a.cursor != 1 {
		t.Fatalf("after r: open %v cursor %d; want a.go still open in its view, cursor on it", a.open, a.cursor)
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

	// Nothing changed; F5 does the same as r.
	pressApp(a, "f5")
	if view.msg != "nothing changed" {
		t.Errorf("F5 without changes: msg %q", view.msg)
	}

	// git fails: everything stays.
	err = errors.New("git diff: broken")
	pressApp(a, "r")
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
	pressApp(a, "r")
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
	pressApp(a, "r")
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
	pressApp(a, "r")
	if _, ok := a.files[0].diff.movedTo(left, 0); ok {
		t.Fatal("moved before the block was added anywhere")
	}
	files = append(files, source.File{Path: "d.go", New: movedBlock, InNew: true})
	pressApp(a, "r")
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
	pressApp(a, "r")
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
	pressApp(a, "r")
	if a.msg != "nothing to load again" {
		t.Errorf("msg %q", a.msg)
	}
}

// fakeGitDiff records the arguments git diff runs with and returns a file
// named after them, or err when set.
type fakeGitDiff struct {
	runs [][]string
	err  error
	ctx  context.Context // of the last run
}

func (g *fakeGitDiff) diff(ctx context.Context, args []string, untracked bool) ([]source.File, error) {
	g.runs, g.ctx = append(g.runs, args), ctx
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
	if popup := ansi.Strip(s.frame()); s.input == nil ||
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
	view := ansi.Strip(s.frame())
	for _, want := range []string{"git diff HEAD~3 HEAD", "diff_HEAD~3_HEAD.go", "git diff HEAD~3 HEAD: 2 files"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	// r runs the new arguments again; up takes them to edit.
	pressRoot(s, "r")
	if len(g.runs) != 2 || !slices.Equal(g.runs[1], []string{"HEAD~3", "HEAD"}) {
		t.Errorf("r after the switch: runs %v", g.runs)
	}
	pressRoot(s, "d", "up")
	if s.input.text != "HEAD~3 HEAD" {
		t.Errorf("up: %q, want the current arguments", s.input.text)
	}
	pressRoot(s, "esc")
	if s.input != nil || len(g.runs) != 2 || shownApp(t, s) != a || strings.Contains(s.frame(), "╭─ git diff") {
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
	for _, row := range strings.Split(ansi.Strip(s.frame()), "\n") {
		if strings.Contains(row, want) {
			_, in, _ := strings.Cut(row, "│ ")
			in, _, _ = strings.Cut(in, " │")
			return in
		}
	}
	t.Fatalf("no popup line holds %q:\n%s", want, ansi.Strip(s.frame()))
	return ""
}

func TestDiffSwitchPopup(t *testing.T) {
	s, g := newDiffSwitch(newTestApp()) // 100 columns
	pressRoot(s, "d")
	if strings.Contains(s.frame(), "↑ current") {
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
	for i, row := range strings.Split(s.frame(), "\n") {
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
	if strings.Contains(s.frame(), "current") {
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
	view := ansi.Strip(s.frame())
	if s.view != m || s.input == nil || !strings.Contains(view, " HEAD~99█ ") ||
		!strings.Contains(view, "bad revision 'HEAD~99'") || strings.Contains(view, "more lines") {
		t.Fatalf("failed git: shows %T, popup\n%s", s.view, view)
	}
	// The message goes with the next key; esc closes the popup, the view stays.
	pressRoot(s, "backspace")
	if strings.Contains(s.frame(), "bad revision") {
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
	return append(keys, "pgdown", "pgdown", "pgup", "end", "pgup", "k", "home", "pgdown", "j")
}

func TestFileListArrowsSweep(t *testing.T) {
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
	// Up to 40 rows: from 35 on, the preview leaves the list fileListRows rows.
	for _, preview := range []bool{false, true} {
		for height := minHeight; height <= 40; height++ {
			t.Run(fmt.Sprintf("preview %v height %d", preview, height), func(t *testing.T) {
				t.Parallel()
				a := NewApp(config.Default(), files)
				a.cfg.Settings.Preview = preview
				a.Update(tea.WindowSizeMsg{Width: 40, Height: height})
				what := fmt.Sprintf("file list, height %d, preview %v", height, preview)
				h := a.listHeight()
				rows := func() []string { return viewRows(a.frame(), 2, 2+h) }
				shown := checkShown(t, what, rows(), h, len(files), a.cursor, nil, index)
				for _, k := range sweepKeys(len(files)) {
					pressApp(a, k)
					shown = checkShown(t, what+", "+k, rows(), h, len(files), a.cursor, shown, index)
				}
			})
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
		got := viewRows(a.frame(), 2, 2+a.listHeight())
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
	pressApp(a, "end")
	check("end", append([]string{"  ↑ 21 more"}, names(21, 29)...)...)

	// With the preview box the list takes at most half of the room.
	a.cfg.Settings.Preview = true
	a.scrollToCursor()
	if h := a.listHeight(); h != 5 {
		t.Fatalf("list with the box: %d rows, want 5", h)
	}
	check("end with the box", append([]string{"  ↑ 26 more"}, names(26, 29)...)...)
	// The window shrinks: the cursor's file stays in view.
	a.Update(tea.WindowSizeMsg{Width: 60, Height: 10}) // 3 rows, 3 for the box
	if h := a.listHeight(); h != 3 {
		t.Fatalf("smaller window: %d rows, want 3", h)
	}
	check("smaller window", "  ↑ 28 more", "f28.go", "f29.go")
	pressApp(a, "home")
	check("home in the smaller window", "f00.go", "f01.go", "  ↓ 28 more")
}

// With the preview, the file list takes at most fileListRows rows, and the
// preview the rest; without it, the list takes all. The commit list has no
// such limit.
func TestFileListRows(t *testing.T) {
	var files []source.File
	for i := range 30 {
		files = append(files, source.File{Path: fmt.Sprintf("f%02d.go", i), Old: []string{"a"}, New: []string{"b"},
			InOld: true, InNew: true})
	}
	a := NewApp(config.Default(), files)
	a.cfg.Settings.Preview = true
	a.Update(tea.WindowSizeMsg{Width: 60, Height: 60}) // room for 57 rows
	if l, b := a.listHeight(), a.boxHeight(); l != fileListRows || b != 57-fileListRows-1 {
		t.Errorf("with the preview: list %d, box %d; want %d, %d", l, b, fileListRows, 57-fileListRows-1)
	}
	if rows := viewRows(a.frame(), 2, 2+fileListRows); rows[fileListRows-1] != "  ↓ 16 more" {
		t.Errorf("last row of the list: %q", rows[fileListRows-1])
	}
	a.cfg.Settings.Preview = false
	if l := a.listHeight(); l != 57 {
		t.Errorf("without the preview: list %d, want 57", l)
	}
	preview := true
	commits := list{n: 30, preview: &preview, height: 60}
	if l := commits.listHeight(); l != 28 {
		t.Errorf("commit list with the box: %d rows, want half of the room, 28", l)
	}
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
	if !strings.Contains(ansi.Strip(s.frame()), msg[:96-4]) {
		t.Errorf("the message is cut before the screen's edge:\n%s", ansi.Strip(s.frame()))
	}
}

func TestAppPathsDrawnPrintable(t *testing.T) {
	evil := "x\x1b]0;pwned\x07.go" // would set the terminal title
	a := NewApp(config.Default(), []source.File{
		{Path: evil, OldPath: "old" + evil, Old: []string{"x"}, New: []string{"y"}, InOld: true, InNew: true},
		{Path: "bin" + evil, InOld: true, InNew: true, Binary: true},
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	check := func(what, view string) {
		t.Helper()
		if strings.Contains(view, "\x1b]") || strings.Contains(view, "\x07") {
			t.Errorf("%s passes the path's escape sequence: %q", what, view)
		}
		if !strings.Contains(view, "x☠]0;pwned☠.go") {
			t.Errorf("%s does not show the path printable:\n%s", what, ansi.Strip(view))
		}
	}
	check("list", a.frame())
	pressApp(a, "j", "enter") // the binary file: not opened, named in the status
	check("binary message", a.frame())
	pressApp(a, "k", "enter")
	check("diff titles", a.frame())
}

// newUntrackedApp returns the file selector of a git diff of f00 and f01,
// with the new file x0 while the untracked files are shown.
func newUntrackedApp() *App {
	cfg := config.Default()
	file := func(name string) source.File {
		return source.File{Path: name, Old: []string{"a"}, New: []string{"b"}, InOld: true, InNew: true}
	}
	list := func(_ context.Context, _ []string, untracked bool) ([]source.File, error) {
		files := []source.File{file("f00"), file("f01")}
		if untracked {
			files = append(files, source.File{Path: "x0", New: []string{"new"}, InNew: true})
		}
		return files, nil
	}
	files, _ := list(context.Background(), nil, cfg.Settings.Untracked)
	a := NewApp(cfg, files).WithGitDiff([]string{"HEAD"}, list)
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return a
}

func TestUntrackedToggle(t *testing.T) {
	a := newUntrackedApp()
	f00 := a.files[0]

	// In the file list.
	setOption(a, a.cfg, "u")
	if !a.cfg.Settings.Untracked || len(a.files) != 3 || a.files[2].Path != "x0" {
		t.Fatalf("u: setting %v, %d files", a.cfg.Settings.Untracked, len(a.files))
	}
	if view := ansi.Strip(a.frame()); !strings.Contains(view, "git diff HEAD + untracked") {
		t.Errorf("u: header lacks the hint:\n%s", view)
	}
	// Files that stay keep their entry, and so anchors and folds.
	if a.files[0] != f00 {
		t.Error("u: f00 got a new entry")
	}

	setOption(a, a.cfg, "u")
	if a.cfg.Settings.Untracked || len(a.files) != 2 {
		t.Fatalf("u again: setting %v, %d files", a.cfg.Settings.Untracked, len(a.files))
	}
	if view := ansi.Strip(a.frame()); strings.Contains(view, "untracked") {
		t.Errorf("u again: header still has the hint:\n%s", view)
	}

	// In the diff view of a file that stays, it stays open.
	pressApp(a, "enter")
	setOption(a, a.cfg, "u")
	if a.open != f00 || len(a.files) != 3 {
		t.Errorf("u in f00's diff: open %v, %d files", a.open, len(a.files))
	}
	// Off in the diff view of the new file, the list is shown.
	pressApp(a, "backspace", "end", "enter")
	if a.open == nil || a.open.Path != "x0" {
		t.Fatalf("x0 not open: %v", a.open)
	}
	setOption(a, a.cfg, "u")
	if a.open != nil || len(a.files) != 2 || !strings.Contains(a.msg, "x0 is gone") {
		t.Errorf("u in x0's diff: open %v, %d files, msg %q", a.open, len(a.files), a.msg)
	}

	// In the flow view.
	pressApp(a, "enter", "f")
	setOption(a, a.cfg, "u")
	if !a.flowShown() || !a.cfg.Settings.Untracked || len(a.files) != 3 {
		t.Errorf("u in the flow view: flow %v, setting %v, %d files", a.flowShown(), a.cfg.Settings.Untracked, len(a.files))
	}
}

// Outside a git diff, u says so and leaves the setting alone.
func TestUntrackedOutsideGitDiff(t *testing.T) {
	a := newTestApp() // as for two directories
	setOption(a, a.cfg, "u")
	if a.cfg.Settings.Untracked || a.msg != untrackedOnlyInDiff {
		t.Errorf("file list: setting %v, msg %q", a.cfg.Settings.Untracked, a.msg)
	}

	f := source.File{Path: "f00", Old: []string{"a"}, New: []string{"b"}, InOld: true, InNew: true}
	m := New(config.Default(), "old", "new", f.Old, f.New)
	setOption(m, m.cfg, "u")
	if m.cfg.Settings.Untracked || m.msg != untrackedOnlyInDiff {
		t.Errorf("two files: setting %v, msg %q", m.cfg.Settings.Untracked, m.msg)
	}

	l, _ := newTestLog()
	setOption(l, l.cfg, "u")
	if l.cfg.Settings.Untracked || l.msg != untrackedOnlyInDiff {
		t.Errorf("log: setting %v, msg %q", l.cfg.Settings.Untracked, l.msg)
	}
}

func TestRefreshedMessage(t *testing.T) {
	for _, c := range []struct {
		added, gone, changed, dropped int
		want                          string
	}{
		{0, 0, 0, 0, "nothing changed"},
		{1, 0, 0, 0, "1 new"},
		{0, 2, 0, 0, "2 gone"},
		{0, 0, 3, 0, "3 changed"},
		{0, 0, 1, 1, "1 changed, 1 anchor dropped"},
		{0, 0, 1, 2, "1 changed, 2 anchors dropped"},
		{1, 2, 3, 4, "1 new, 2 gone, 3 changed, 4 anchors dropped"},
	} {
		if got := refreshed(c.added, c.gone, c.changed, c.dropped); got != c.want {
			t.Errorf("refreshed(%d, %d, %d, %d) = %q, want %q", c.added, c.gone, c.changed, c.dropped, got, c.want)
		}
	}
}

func TestAppRelistDropsAnchors(t *testing.T) {
	var files []source.File
	var err error
	a := newRelistApp(&files, &err)
	pressApp(a, "enter")
	a.open.view.addAnchor(diff.Anchor{Left: 5, Right: 4}, right, shape{})
	a.open.view.addAnchor(diff.Anchor{Left: 8, Right: 7}, right, shape{})
	changed := numbered("x", 10)[1:]
	changed[4] = "changed4"
	files = []source.File{{Path: "a.go", Old: numbered("x", 10), New: changed, InOld: true, InNew: true}, files[1], files[2]}
	pressApp(a, "r")
	if m := a.open.view; m.msg != "1 changed, 1 anchor dropped" || len(m.anchors) != 1 {
		t.Errorf("one anchored line changed: msg %q, anchors %v", m.msg, m.anchors)
	}
}

// Pasted text goes into the line while the popup asks, also clearing why git
// failed, and to the view otherwise.
func TestDiffSwitchPaste(t *testing.T) {
	s, g := newDiffSwitch(newTestApp())
	s.Update(tea.PasteMsg{Content: "x0"})
	if s.input != nil {
		t.Fatalf("pasted with the popup closed: %q", s.input.text)
	}
	pressRoot(s, "d")
	s.Update(tea.PasteMsg{Content: "x0 "})
	s.Update(tea.PasteMsg{Content: "x1"})
	if s.input == nil || s.input.text != "x0 x1" {
		t.Fatalf("pasted: %+v", s.input)
	}
	g.err = errors.New("x2")
	pressRoot(s, "enter")
	if s.err != "x2" {
		t.Fatalf("git failed: err %q", s.err)
	}
	s.Update(tea.PasteMsg{Content: "x3"})
	if s.err != "" || s.input.text != "x0 x1x3" {
		t.Errorf("pasted after the failure: err %q, line %q", s.err, s.input.text)
	}
}

func TestLineInputEdit(t *testing.T) {
	for _, c := range []struct{ text, key, want string }{
		{"x0 x1", "ctrl+w", "x0 "},
		{"x0 x1  ", "ctrl+w", "x0 "},
		{"x0  x1", "ctrl+w", "x0  "},
		{"x0", "ctrl+w", ""},
		{"  ", "ctrl+w", ""},
		{"", "ctrl+w", ""},
		{"äö üß", "ctrl+w", "äö "},
		{"x0 x1", "ctrl+u", ""},
		{"", "ctrl+u", ""},
		{"xä", "backspace", "x"},
		{"", "backspace", ""},
	} {
		in := &lineInput{text: c.text}
		in.edit(keyMsg(c.key))
		if in.text != c.want {
			t.Errorf("%s on %q: %q, want %q", c.key, c.text, in.text, c.want)
		}
	}
}

// r while the files are listed starts no second listing, but runs one once
// the first is done: what to list may have changed meanwhile.
func TestAppRelistWhileListing(t *testing.T) {
	var files []source.File
	var err error
	a := newRelistApp(&files, &err)
	runs := 0
	list := a.relist
	a.relist = func(ctx context.Context) ([]source.File, error) {
		runs++
		return list(ctx)
	}
	_, first := a.Update(refreshMsg{})
	if _, again := a.Update(refreshMsg{}); again != nil || runs != 0 {
		t.Fatalf("r during the listing: command %v, runs %d; want none", again != nil, runs)
	}
	_, next := a.Update(first())
	if runs != 1 || next == nil {
		t.Fatalf("after the first listing: runs %d, next %v; want the second listing", runs, next != nil)
	}
	if _, last := a.Update(next()); runs != 2 || last != nil {
		t.Errorf("after the second listing: runs %d, more %v", runs, last != nil)
	}
}

// esc while git diff runs, and a newer request, drop the files that come
// late and end the git command.
func TestDiffSwitchDropsLateDiff(t *testing.T) {
	view := newTestApp()
	s, g := newDiffSwitch(view)
	ask := func(args string) tea.Cmd {
		s.Update(askDiffMsg{})
		pressRoot(s, args)
		_, cmd := s.Update(keyMsg("enter"))
		if cmd == nil || !strings.Contains(ansi.Strip(s.frame()), "running git diff…") {
			t.Fatalf("%s: command %v, popup:\n%s", args, cmd != nil, ansi.Strip(s.frame()))
		}
		return cmd
	}
	cmd := ask("x0")
	s.Update(keyMsg("esc"))
	s.Update(cmd())
	if s.view != view || g.ctx.Err() == nil {
		t.Errorf("after esc: view replaced %v, git diff ended %v", s.view != view, g.ctx.Err() != nil)
	}
	older := ask("x1")
	newer := ask("x2")
	s.Update(newer())
	s.Update(older())
	if a := shownApp(t, s); a.diffArgs[0] != "x2" {
		t.Errorf("shows git diff %v, want x2, the newer", a.diffArgs)
	}
}

// The arguments of the git diff popup read quotes as a shell does, and
// joinArgs writes them so that they read back the same.
func TestSplitArgs(t *testing.T) {
	for _, c := range []struct {
		text string
		want []string
	}{
		{"", nil},
		{"  HEAD~1  HEAD ", []string{"HEAD~1", "HEAD"}},
		{`-- 'a b.txt' "c d" e\ f`, []string{"--", "a b.txt", "c d", "e f"}},
		{`'it'"'"'s' ""`, []string{"it's", ""}},
		{`a"b c"d`, []string{"ab cd"}},
	} {
		got, err := splitArgs(c.text)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%q: %q, %v; want %q", c.text, got, err, c.want)
		}
		if back, _ := splitArgs(joinArgs(got)); !slices.Equal(back, got) {
			t.Errorf("%q: joined %q reads back as %q", c.text, joinArgs(got), back)
		}
	}
	for _, open := range []string{`'a`, `"a`} {
		if _, err := splitArgs(open); err == nil {
			t.Errorf("%q: no error for the open quote", open)
		}
	}
}

// Pasted text goes on one line, without characters that are not visible,
// and the heading shows the arguments of git diff drawn printable.
func TestPasteAndHeading(t *testing.T) {
	in := &lineInput{text: "x0 "}
	in.paste("a\nb\r\x1b[2Jc\td‎")
	if in.text != "x0 a b [2Jc d" {
		t.Errorf("pasted: %q", in.text)
	}
	s, _ := newDiffSwitch(newTestApp())
	s.Update(askDiffMsg{})
	s.Update(tea.PasteMsg{Content: "x1\x1b[2J"})
	cmd := s.key(keyMsg("enter"))
	s.Update(cmd())
	if view := shownApp(t, s).frame(); strings.Contains(view, "\x1b[2J") || !strings.Contains(ansi.Strip(view), "git diff x1[2J") {
		t.Errorf("heading:\n%q", ansi.Strip(view))
	}
}
