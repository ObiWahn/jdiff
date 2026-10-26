package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

func TestMarkFindsLineInShrunkFile(t *testing.T) {
	m := newMarkModel()
	press(m, "ctrl+space")
	moveTo(t, m, right, 30)
	press(m, "m", "a")
	// The file shrank to 3 lines, the marked line among them: it is found
	// by its text, although its old number is far past the end.
	m.reload(right, []string{"x0", m.lines[right][30], "x2"})
	press(m, "g", "'", "a")
	if m.lineAt(right, m.cursor) != 1 || m.msg != "mark a" {
		t.Errorf("shrunk file: right line %d, msg %q; want line 1 found by its text", m.lineAt(right, m.cursor), m.msg)
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
	pressLog(l, "f", "n")
	f := l.flow
	if r := f.rows[f.cursor]; r.kind != flowDiff || r.part != 0 || r.file != 0 {
		t.Fatalf("not on the diff row of a.go in c3: %+v", r)
	}
	pressLog(l, "m", "a")      // a.go line 1, "2"
	pressLog(l, "n", "m", "b") // b.go line 1, "y"
	pressLog(l, "P")           // c2, which changes a.go only
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
	if view := ansi.Strip(p.frame()); !strings.Contains(view, "no marks") {
		t.Errorf("empty popup:\n%s", view)
	}
	pressRoot(p, "esc", "ctrl+space")
	moveTo(t, m, right, 10)
	pressRoot(p, "m", "a")
	moveTo(t, m, right, 30)
	pressRoot(p, "m", "b", "g")

	pressRoot(p, "\\")
	view := ansi.Strip(p.frame())
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
	if p.open || strings.Contains(ansi.Strip(p.frame()), "╭─ marks") {
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

// rawText reports the first line of view, stripped of styles, that holds a
// tab or control character or is wider than width, or "".
func rawText(view string, width int) string {
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		if strings.ContainsFunc(line, func(r rune) bool { return r < 0x20 || r == 0x7f }) ||
			ansi.StringWidth(line) > width {
			return line
		}
	}
	return ""
}

func TestMarksPopupExpandsTabs(t *testing.T) {
	m := newMarkModel()
	p := WithMarks(m.cfg, m)
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if err := m.cfg.SetMark("t", config.Mark{Path: "new", Line: 1, Text: "a\tb\x01c"}); err != nil {
		t.Fatal(err)
	}
	pressRoot(p, "\\")
	view := p.frame()
	if line := rawText(view, 80); line != "" {
		t.Errorf("popup line with raw text: %q", line)
	}
	if !strings.Contains(ansi.Strip(view), "a   b☠c") {
		t.Errorf("mark text not shown with its tab expanded:\n%s", ansi.Strip(view))
	}
}

// popupRows returns the rows inside the popup's border in view, without
// styles and the border's padding.
func popupRows(view string) []string {
	var out []string
	x, w := -1, 0
	for _, row := range strings.Split(ansi.Strip(view), "\n") {
		r := []rune(row)
		if x < 0 {
			if i := strings.Index(row, "╭"); i >= 0 {
				x = len([]rune(row[:i]))
				w = slices.Index(r[x:], '╮') + 1
			}
			continue
		}
		if r[x] == '╰' {
			break
		}
		out = append(out, strings.TrimRight(string(r[x+2:x+w-2]), " "))
	}
	return out
}

func TestMarksPopupArrows(t *testing.T) {
	m := newMarkModel()
	for i := range 26 {
		if err := m.cfg.SetMark(string(rune('a'+i)), config.Mark{Path: "new", Line: i + 1, Text: fmt.Sprintf("t%02d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	p := WithMarks(m.cfg, m)
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 12}) // 8 rows in the popup
	pressRoot(p, "\\")
	shown := func() []string {
		var out []string
		for _, r := range popupRows(p.frame()) {
			if strings.HasPrefix(r, "  ↑ ") || strings.HasPrefix(r, "  ↓ ") {
				out = append(out, r)
			} else {
				out = append(out, strings.Fields(r)[2]) // the mark's text, t00 to t25
			}
		}
		return out
	}
	marks := func(from, to int) []string {
		var out []string
		for i := from; i <= to; i++ {
			out = append(out, fmt.Sprintf("t%02d", i))
		}
		return out
	}
	for _, tt := range []struct {
		down int
		want []string
	}{
		{0, append(marks(0, 6), "  ↓ 19 more")},
		{6, append(marks(0, 6), "  ↓ 19 more")},
		{1, append(append([]string{"  ↑ 2 more"}, marks(2, 7)...), "  ↓ 18 more")},
		{25, append([]string{"  ↑ 19 more"}, marks(19, 25)...)},
	} {
		for range tt.down {
			pressRoot(p, "j")
		}
		if got := shown(); !slices.Equal(got, tt.want) {
			t.Errorf("cursor t%02d:\n got %q\nwant %q", p.cursor, got, tt.want)
		}
	}
	for range 24 {
		pressRoot(p, "k")
	}
	if got, want := shown(), append(marks(0, 6), "  ↓ 19 more"); p.cursor != 1 || !slices.Equal(got, want) {
		t.Errorf("back up to t01:\n got %q\nwant %q", got, want)
	}
}

func TestMarksPopupArrowsSweep(t *testing.T) {
	t.Parallel() // a sweep: its own config and views, nothing shared
	const n = 26
	index := func(r string) int {
		var k int
		fmt.Sscanf(strings.Fields(r)[2], "t%d", &k)
		return k
	}
	// Rows in the popup: height-4, up to all 26 marks; 31 leaves room for
	// one more.
	for height := minHeight; height <= n+5; height++ {
		t.Run(fmt.Sprint("height ", height), func(t *testing.T) {
			t.Parallel()
			m := newTestModel([]string{"a"}, []string{"b"})
			for i := range n {
				if err := m.cfg.SetMark(string(rune('a'+i)), config.Mark{Path: "new", Line: 1, Text: fmt.Sprintf("t%02d", i)}); err != nil {
					t.Fatal(err)
				}
			}
			p := WithMarks(m.cfg, m)
			p.Update(tea.WindowSizeMsg{Width: 40, Height: height})
			pressRoot(p, "\\")
			what := fmt.Sprintf("jump list, height %d", height)
			h := p.boxHeight() - 2
			shown := checkShown(t, what, popupRows(p.frame()), h, n, p.cursor, nil, index)
			keys := append(slices.Repeat([]string{"j"}, n), slices.Repeat([]string{"k"}, n)...)
			for _, k := range keys {
				pressRoot(p, k)
				shown = checkShown(t, what+", "+k, popupRows(p.frame()), h, n, p.cursor, shown, index)
			}
		})
	}
}

func TestRepoPath(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "repo")
	for _, c := range []struct {
		name, repo, file, want string
	}{
		{"in the repository", repo, filepath.Join(repo, "x0"), "x0"},
		{"in a directory of it", repo, filepath.Join(repo, "d0", "x0"), "d0/x0"},
		{"a name starting with two dots", repo, filepath.Join(repo, "..x0"), "..x0"},
		{"the repository itself", repo, repo, "."},
		{"above it", repo, dir, dir},
		{"beside it", repo, filepath.Join(dir, "other", "x0"), filepath.Join(dir, "other", "x0")},
		{"its name a prefix", repo, filepath.Join(dir, "repo2", "x0"), filepath.Join(dir, "repo2", "x0")},
		{"no repository", "", filepath.Join(repo, "x0"), filepath.Join(repo, "x0")},
	} {
		if got := repoPath(c.repo, c.file); got != c.want {
			t.Errorf("%s: repoPath(%q, %q) = %q, want %q", c.name, c.repo, c.file, got, c.want)
		}
	}
	// Relative to the current directory.
	t.Chdir(dir)
	if got, want := repoPath(repo, filepath.Join("repo", "x0")), "x0"; got != want {
		t.Errorf("relative: %q, want %q", got, want)
	}
	if got, want := repoPath("", "x0"), filepath.Join(dir, "x0"); got != want {
		t.Errorf("relative, no repository: %q, want %q", got, want)
	}
}

// The repository as git names it, without symlinks: a file reached through a
// link to it is in it.
func TestRepoPathThroughSymlink(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo, link := filepath.Join(dir, "repo"), filepath.Join(dir, "link")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repo, link); err != nil {
		t.Skip(err) // on Windows it takes a privilege
	}
	writeLines(t, filepath.Join(repo, "x0"), []string{"x0"})
	if got := repoPath(repo, filepath.Join(link, "x0")); got != "x0" {
		t.Errorf("through a link: %q, want x0", got)
	}
}

// A mark set in the diff of two files is kept relative to the repository, so
// that it is found again from any directory.
func TestWatchedMarkIsRelativeToRepo(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := source.File{Path: "x0", OldFile: filepath.Join(dir, "x0"), NewFile: filepath.Join(dir, "d0", "x0"),
		New: numbered("x", 3), Old: numbered("x", 3), InOld: true, InNew: true}
	cfg := polling()
	cfg.LoadMarks(dir)
	m := NewWatched(cfg, f)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	press(m, "ctrl+space")
	moveTo(t, m, right, 2)
	press(m, "m", "a")
	if mk, ok := cfg.Mark("a"); !ok || mk != (config.Mark{Path: "d0/x0", Line: 3, Text: "x2"}) {
		t.Errorf("mark a = %+v, %v", mk, ok)
	}
	if m.msg != "mark a: d0/x0:3" {
		t.Errorf("msg %q", m.msg)
	}
	press(m, "g", "'", "a")
	if m.lineAt(right, m.cursor) != 2 || m.msg != "mark a" {
		t.Errorf("' a: right line %d, msg %q", m.lineAt(right, m.cursor), m.msg)
	}
}

func TestIsMarkLetter(t *testing.T) {
	for k, want := range map[string]bool{
		"a": true, "z": true, "A": true, "Z": true,
		"": false, "0": false, "@": false, "[": false, "`": false, "{": false, "ä": false, "ab": false, "enter": false,
	} {
		if got := isMarkLetter(k); got != want {
			t.Errorf("isMarkLetter(%q) = %v", k, got)
		}
	}
}

func TestMarkOnEmptyNewFile(t *testing.T) {
	m := newTestModel([]string{"x0"}, nil)
	if err := m.cfg.SetMark("a", config.Mark{Path: "new", Line: 1, Text: "x0"}); err != nil {
		t.Fatal(err)
	}
	press(m, "'", "a")
	if m.msg != "mark a: the new file is empty" {
		t.Errorf("' a: msg %q", m.msg)
	}
}

func TestMarkMessagesInList(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "'", "q")
	if a.msg != "no mark q" || a.open != nil {
		t.Errorf("' q: msg %q, open %v", a.msg, a.open)
	}
	if err := a.cfg.SetMark("z", config.Mark{Path: "x0", Line: 1, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	pressApp(a, "'", "z")
	if a.msg != "mark z: x0 is not in this diff" || a.open != nil {
		t.Errorf("' z: msg %q, open %v", a.msg, a.open)
	}
}

func TestFlowMarkMessages(t *testing.T) {
	a := newFlowApp()
	pressApp(a, "f")
	if !flowAt(a, 0, -1) {
		t.Fatal("not on the file bar of a.go")
	}
	// On the file bar there is no line to mark, and a non-letter is no mark.
	pressApp(a, "m", "a")
	if _, ok := a.cfg.Mark("a"); ok || a.flow.msg != "no line of the new file here" {
		t.Errorf("m a on the file bar: msg %q", a.flow.msg)
	}
	pressApp(a, "j", "m", "1")
	if _, ok := a.cfg.Mark("1"); ok || a.flow.msg != "" {
		t.Errorf("m 1: msg %q, want no mark and no message", a.flow.msg)
	}
	pressApp(a, "'", "q")
	if a.flow.msg != "no mark q" {
		t.Errorf("' q: msg %q", a.flow.msg)
	}
	if err := a.cfg.SetMark("z", config.Mark{Path: "x0", Line: 1, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	pressApp(a, "'", "z")
	if a.flow.msg != "mark z: x0 is not in this diff" {
		t.Errorf("' z: msg %q", a.flow.msg)
	}
	// In a file of the diff the cursor goes to the line.
	if err := a.cfg.SetMark("c", config.Mark{Path: "c.go", Line: 2, Text: "new"}); err != nil {
		t.Fatal(err)
	}
	pressApp(a, "'", "c")
	if !flowAt(a, 2, a.flow.rows[a.flow.cursor].line) || a.flow.msg != "mark c" {
		_, f, _ := a.flow.position()
		t.Errorf("' c: on file %d, msg %q; want c.go", f, a.flow.msg)
	}
}

func TestFlowMarkOnEmptyNewFile(t *testing.T) {
	a := NewApp(config.Default(), []source.File{
		{Path: "x0", Old: []string{"x0"}, InOld: true},
		modified("x1", []string{"x1"}, []string{"x2"}),
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if err := a.cfg.SetMark("a", config.Mark{Path: "x0", Line: 1, Text: "x0"}); err != nil {
		t.Fatal(err)
	}
	pressApp(a, "f", "'", "a")
	if a.flow.msg != "mark a: the new file is empty" {
		t.Errorf("' a: msg %q", a.flow.msg)
	}
}

func TestMarksPopupJumpsInFlow(t *testing.T) {
	a := newFlowApp()
	p := WithMarks(a.cfg, a)
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if err := a.cfg.SetMark("c", config.Mark{Path: "c.go", Line: 2, Text: "new"}); err != nil {
		t.Fatal(err)
	}
	pressRoot(p, "f", "\\", "enter")
	if _, file, line := a.flow.position(); !a.flowShown() || file != 2 || line < 0 || a.flow.msg != "mark c" {
		t.Errorf("enter on a mark in c.go: flow %v, file %d, line %d, msg %q", a.flowShown(), file, line, a.flow.msg)
	}
}

func TestFlowMarkInCommitNotLoaded(t *testing.T) {
	l, _ := newTestLog() // the oldest commit, c0, cannot be loaded
	if err := l.cfg.SetMark("a", config.Mark{Path: "a.go", Line: 1, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	pressLog(l, "f", "G", "'", "a")
	if r := l.flow.rows[l.flow.cursor]; l.flow.parts[r.part].app != nil || l.flow.msg != "mark a: a.go is not in this diff" {
		t.Errorf("' a in c0: row %+v, msg %q", r, l.flow.msg)
	}
}

// From the file list with flow on, a mark opens the flow view at its line.
func TestMarkFromListOpensFlow(t *testing.T) {
	a := newFlowApp()
	if err := a.cfg.SetMark("c", config.Mark{Path: "c.go", Line: 2, Text: "new"}); err != nil {
		t.Fatal(err)
	}
	pressApp(a, "f", "backspace")
	if a.open != nil {
		t.Fatalf("backspace: open %v, want the list", a.open)
	}
	pressApp(a, "'", "c")
	if _, file, line := a.flow.position(); !a.flowShown() || file != 2 || line < 0 || a.flow.msg != "mark c" {
		t.Errorf("' c: flow %v, file %d, line %d, msg %q", a.flowShown(), file, line, a.flow.msg)
	}
}
