package ui

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"jdiff/internal/config"
	"jdiff/internal/gitsrc"
	"jdiff/internal/source"
)

func modified(path string, old, new []string) source.File {
	return source.File{Path: path, Old: old, New: new, InOld: true, InNew: true}
}

// newTestLog returns a log of four commits, newest first:
//
//	c3: changes a.go and b.go
//	c2: changes only a.go
//	c1: changes only c.go
//	c0: cannot be loaded
//
// Only c3 has a message body. In the flow view c3 takes 10 rows, more than
// the 9 below the commit and file lines: 6 header rows and two files of a
// bar and one row each.
func newTestLog() (*Log, map[string]int) {
	loads := map[string]int{}
	files := map[string][]source.File{
		"c3": {modified("a.go", []string{"1"}, []string{"2"}), modified("b.go", []string{"x"}, []string{"y"})},
		"c2": {modified("a.go", []string{"0"}, []string{"1"})},
		"c1": {modified("c.go", []string{"p"}, []string{"q", "r"})},
	}
	load := func(c gitsrc.Commit) ([]source.File, error) {
		loads[c.Hash]++
		if f, ok := files[c.Hash]; ok {
			return f, nil
		}
		return nil, errors.New("cannot load " + c.Hash)
	}
	var commits []gitsrc.Commit
	for _, h := range []string{"c3", "c2", "c1", "c0"} {
		commits = append(commits, gitsrc.Commit{Hash: h, Short: h, Date: "2026-10-22", Author: "T",
			Subject: "subject " + h, Added: 12, Removed: 3})
	}
	commits[0].Body = "body c3"
	l := NewLog(config.Default(), "main", commits, load)
	l.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	return l, loads
}

func pressLog(l *Log, keys ...string) { pressRoot(l, keys...) }

// shownFile returns the path of the diff shown, or "" for a list.
func shownFile(l *Log) string {
	if l.open == nil || l.open.app.open == nil {
		return ""
	}
	return l.open.app.open.Path
}

func TestLogListShowsCommits(t *testing.T) {
	l, loads := newTestLog()
	view := ansi.Strip(l.View())
	for _, want := range []string{"main (4 commits)", "c3", "2026-10-22", "+12 -3", "subject c1"} {
		if !strings.Contains(view, want) {
			t.Errorf("log view lacks %q:\n%s", want, view)
		}
	}
	if len(loads) != 0 {
		t.Errorf("commits loaded before opening: %v", loads)
	}
	if top := topLines(l, 3); top[0] != " main (4 commits)" || top[1] != "" || !strings.HasPrefix(top[2], " c3 ") {
		t.Errorf("top lines %q, want the branch, an empty line, then c3", top)
	}
}

func TestLogLevelsAndBackspace(t *testing.T) {
	l, loads := newTestLog()

	// A commit with several files shows its file list.
	pressLog(l, "enter")
	if l.open == nil || shownFile(l) != "" || len(l.open.app.files) != 2 {
		t.Fatalf("enter on c3: open %v, shown %q", l.open, shownFile(l))
	}
	pressLog(l, "enter")
	if shownFile(l) != "a.go" {
		t.Fatalf("enter in the file list: shown %q, want a.go", shownFile(l))
	}
	pressLog(l, "backspace")
	if l.open == nil || shownFile(l) != "" {
		t.Fatalf("backspace from the diff should show the file list")
	}
	pressLog(l, "backspace")
	if l.open != nil {
		t.Fatalf("backspace from the file list should show the commits")
	}

	// A single-file commit opens its file directly, and backspace goes
	// straight back to the commits.
	pressLog(l, "j", "enter")
	if shownFile(l) != "a.go" {
		t.Fatalf("enter on c2: shown %q, want a.go directly", shownFile(l))
	}
	pressLog(l, "backspace")
	if l.open != nil {
		t.Fatalf("backspace from a single-file commit should show the commits")
	}

	// Reopening reuses the loaded files.
	pressLog(l, "k", "enter")
	if loads["c3"] != 1 {
		t.Errorf("c3 loaded %d times, want once", loads["c3"])
	}
}

func TestLogNextPrevCommit(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "enter", "enter") // c3, a.go

	// N keeps a.go, which c2 changed as well.
	pressLog(l, "N")
	if l.cursor != 1 || shownFile(l) != "a.go" {
		t.Fatalf("N: commit %d shown %q, want c2 a.go", l.cursor, shownFile(l))
	}
	// c1 did not change a.go, but it has a single file.
	pressLog(l, "N")
	if l.cursor != 2 || shownFile(l) != "c.go" {
		t.Fatalf("N: commit %d shown %q, want c1 c.go", l.cursor, shownFile(l))
	}
	// c0 cannot be loaded: stay and show the error.
	pressLog(l, "N")
	if l.cursor != 2 || !strings.Contains(l.open.app.open.view.msg, "cannot load c0") {
		t.Errorf("N onto a broken commit: commit %d msg %q", l.cursor, l.open.app.open.view.msg)
	}
	// Back via c2 (single file a.go) to c3, which changed a.go as well.
	pressLog(l, "P", "P")
	if l.cursor != 0 || shownFile(l) != "a.go" {
		t.Errorf("P P: commit %d shown %q, want c3 a.go", l.cursor, shownFile(l))
	}
	pressLog(l, "P")
	if !strings.Contains(l.open.app.open.view.msg, "newest commit") {
		t.Errorf("P on the newest commit: msg %q", l.open.app.open.view.msg)
	}
	// N/P also work from the file list.
	pressLog(l, "backspace", "N")
	if l.cursor != 1 || shownFile(l) != "a.go" {
		t.Errorf("N from the file list: commit %d shown %q", l.cursor, shownFile(l))
	}
	// The open file a.go is kept when going back to c3.
	pressLog(l, "P")
	if l.cursor != 0 || shownFile(l) != "a.go" {
		t.Errorf("P back to c3 with a.go open: commit %d shown %q", l.cursor, shownFile(l))
	}
}

func TestLogFileKeysInCommit(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "enter", "enter", "n")
	if shownFile(l) != "b.go" {
		t.Errorf("n in c3: shown %q, want b.go", shownFile(l))
	}
	pressLog(l, "p")
	if shownFile(l) != "a.go" {
		t.Errorf("p in c3: shown %q, want a.go", shownFile(l))
	}
}

// flowCommitAt returns the commit of the flow cursor, or -1 when the flow view
// is not shown.
func flowCommitAt(l *Log) int {
	if !l.flowShown() {
		return -1
	}
	p, _, _ := l.flow.position()
	return p
}

func TestLogFlowLoadsLazily(t *testing.T) {
	l, loads := newTestLog()
	pressLog(l, "f")
	if flowCommitAt(l) != 0 || len(loads) != 1 || loads["c3"] != 1 {
		t.Fatalf("f: flow at %d, loads %v, want c3 only", flowCommitAt(l), loads)
	}
	view := ansi.Strip(l.View())
	for _, want := range []string{"commit c3 · T · 2026-10-22", "    subject c3", "    body c3", "a.go", "b.go"} {
		if !strings.Contains(view, want) {
			t.Errorf("flow view lacks %q:\n%s", want, view)
		}
	}
	pressLog(l, "j")
	if loads["c2"] != 1 || loads["c1"] != 0 {
		t.Errorf("j: loads %v, want c2 loaded as it comes near, c1 not", loads)
	}
	pressLog(l, "G")
	if flowCommitAt(l) != 3 || !strings.Contains(ansi.Strip(l.View()), "cannot load c0") {
		t.Errorf("G: flow at %d, want c0 with its error:\n%s", flowCommitAt(l), ansi.Strip(l.View()))
	}
	for h, n := range loads {
		if n != 1 {
			t.Errorf("%s loaded %d times", h, n)
		}
	}
}

func TestLogFlowCommitKeys(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "f", "N")
	if flowCommitAt(l) != 1 {
		t.Fatalf("N: flow at %d, want c2", flowCommitAt(l))
	}
	pressLog(l, "N", "N", "N")
	if flowCommitAt(l) != 3 || l.flow.msg != "oldest commit" {
		t.Errorf("N past c0: flow at %d msg %q", flowCommitAt(l), l.flow.msg)
	}
	pressLog(l, "P")
	if flowCommitAt(l) != 2 {
		t.Errorf("P: flow at %d, want c1", flowCommitAt(l))
	}
	pressLog(l, "backspace")
	if l.open != nil || l.cursor != 2 {
		t.Fatalf("backspace: open %v cursor %d, want the list on c1", l.open, l.cursor)
	}
	// Flow stays on: enter shows the flow again.
	pressLog(l, "enter")
	if flowCommitAt(l) != 2 {
		t.Errorf("enter with flow on: flow at %d, want c1", flowCommitAt(l))
	}
	// Off again on c1, a single-file commit: its diff is shown.
	pressLog(l, "f")
	if l.flowShown() || shownFile(l) != "c.go" {
		t.Errorf("f on c1: shown %q, want c.go", shownFile(l))
	}
}

func TestLogFlowFromDiff(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "enter", "enter", "f") // c3, a.go
	if p, f, line := l.flow.position(); !l.flowShown() || p != 0 || f != 0 || line != 0 {
		t.Fatalf("f in the a.go diff: flow at %d %d %d", p, f, line)
	}
	pressLog(l, "n", "j", "f")
	if shownFile(l) != "b.go" || l.open.app.open.view.cursor != 0 {
		t.Errorf("f on the row of b.go: shown %q", shownFile(l))
	}
}

func TestLogFlowHeader(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "f")
	top := topLines(l, 2)
	if !strings.Contains(top[0], "c3 subject c3") || strings.Contains(top[1], "a/") {
		t.Errorf("on the commit bar: %q", top)
	}
	pressLog(l, "n", "j")
	top = topLines(l, 2)
	if !strings.Contains(top[0], "c3 subject c3") || !strings.Contains(top[1], "a/a.go") {
		t.Errorf("in a.go: %q", top)
	}
}

func TestLogListColumns(t *testing.T) {
	l, _ := newTestLog()
	line := topLines(l, 3)[2]
	order := []string{"c3", "+12 -3", "subject c3", "T", "2026-10-22"}
	pos := -1
	for _, want := range order {
		i := strings.Index(line[pos+1:], want)
		if i < 0 {
			t.Fatalf("line %q lacks %q after column %d, want order %q", line, want, pos, order)
		}
		pos += 1 + i
	}
	// When narrow, the subject is cut and the date stays visible.
	l.Update(tea.WindowSizeMsg{Width: 34, Height: 12})
	if line := topLines(l, 3)[2]; !strings.Contains(line, "  subje…  T  ") || !strings.HasSuffix(line, "2026-10-22") {
		t.Errorf("narrow line %q", line)
	}
}

func TestLogMessageBox(t *testing.T) {
	l, _ := newTestLog()
	l.commits[0].Body = "first line\nsecond line\nthird line"
	// Height 12: 9 rows below the branch, 4 for the list, 1 empty, 4 for
	// the box.
	view := ansi.Strip(l.View())
	lines := strings.Split(view, "\n")
	if len(lines) != 12 || l.listHeight() != 4 || l.boxHeight() != 4 {
		t.Fatalf("%d lines, list %d box %d:\n%s", len(lines), l.listHeight(), l.boxHeight(), view)
	}
	if lines[6] != "" {
		t.Errorf("no empty line between list and box: %q", lines[6])
	}
	box := lines[7:11]
	if !strings.HasPrefix(box[0], "╭─ c3 ─") || !strings.HasPrefix(box[3], "╰─") {
		t.Errorf("box borders: %q", box)
	}
	for i, want := range []string{"│ subject c3", "│ …"} {
		if !strings.HasPrefix(box[i+1], want) || !strings.HasSuffix(box[i+1], "│") {
			t.Errorf("box line %d = %q, want %q", i+1, box[i+1], want)
		}
	}

	// The box shows the selected commit and grows when there is room.
	pressLog(l, "j")
	l.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	view = ansi.Strip(l.View())
	if l.listHeight() != 4 || l.boxHeight() != 22 || !strings.Contains(view, "│ subject c2") {
		t.Errorf("height 30: list %d box %d:\n%s", l.listHeight(), l.boxHeight(), view)
	}

	// Too small for a box: only the list.
	l.Update(tea.WindowSizeMsg{Width: 100, Height: 7})
	if l.boxHeight() != 0 || l.listHeight() != 4 {
		t.Errorf("height 7: list %d box %d", l.listHeight(), l.boxHeight())
	}
}

func TestLogFileListHeading(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "enter") // c3 has two files
	if top := topLines(l, 3); top[0] != " c3 subject c3  +2 -2" || top[1] != "" || !strings.Contains(top[2], "a.go") {
		t.Errorf("file list top lines %q, want hash, subject and counts, an empty line, then a.go", top)
	}
}

func TestLogPreviewToggleShared(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "v")
	if l.boxHeight() != 0 || strings.Contains(ansi.Strip(l.View()), "╭─") {
		t.Fatalf("v should hide the message box")
	}
	// The file list of a commit follows the same setting.
	pressLog(l, "enter")
	if strings.Contains(ansi.Strip(l.View()), "╭─") {
		t.Errorf("file list shows a preview after v on the commit list")
	}
	pressLog(l, "v")
	if !strings.Contains(ansi.Strip(l.View()), "╭─ a.go ─") {
		t.Errorf("v in the file list should show the preview:\n%s", ansi.Strip(l.View()))
	}
}

func TestFlowCursorRowsInverted(t *testing.T) {
	defer lipgloss.SetColorProfile(lipgloss.ColorProfile())
	lipgloss.SetColorProfile(termenv.ANSI)
	reverse := regexp.MustCompile(`\x1b\[(\d+;)*7(;\d+)*m`)
	l, _ := newTestLog()
	pressLog(l, "f", "G") // load all commits
	f := l.flow
	kinds := map[flowKind]bool{}
	for i, r := range f.rows {
		if r.kind == flowDiff {
			continue
		}
		f.cursor = i
		row := f.row(i)
		// The first column marks the cursor; the row's text is inverted too.
		if strings.TrimSpace(ansi.Strip(row)) != "" && len(reverse.FindAllString(row, -1)) < 2 {
			t.Errorf("cursor on row %d (%v): %q, want its text inverted", i, r, row)
		}
		f.cursor = (i + 1) % len(f.rows)
		if row := f.row(i); reverse.MatchString(row) {
			t.Errorf("row %d (%v) without the cursor is inverted: %q", i, r, row)
		}
		kinds[r.kind] = true
	}
	if !kinds[flowCommit] || !kinds[flowFile] || !kinds[flowNote] {
		t.Errorf("rows checked: %v, want commit, file and note rows", kinds)
	}
}
