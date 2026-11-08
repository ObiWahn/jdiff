package ui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
func newTestLog() (*Log, map[string]int) { return newTestLogOrder(false) }

// newTestLogOrder returns the log of newTestLog, with oldestFirst listed as
// git log --reverse lists it.
func newTestLogOrder(oldestFirst bool) (*Log, map[string]int) {
	loads := map[string]int{}
	files := map[string][]source.File{
		"c3": {modified("a.go", []string{"1"}, []string{"2"}), modified("b.go", []string{"x"}, []string{"y"})},
		"c2": {modified("a.go", []string{"0"}, []string{"1"})},
		"c1": {modified("c.go", []string{"p"}, []string{"q", "r"})},
	}
	load := func(_ context.Context, c gitsrc.Commit) ([]source.File, error) {
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
	if oldestFirst {
		slices.Reverse(commits)
	}
	l := NewLog(config.Default(), "main", commits, oldestFirst, load, nil)
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
	view := ansi.Strip(l.frame())
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

	// A single-file commit shows its file list as well, with its one file.
	pressLog(l, "j", "enter")
	if l.open == nil || shownFile(l) != "" || len(l.open.app.files) != 1 {
		t.Fatalf("enter on c2: open %v, shown %q, want its file list", l.open, shownFile(l))
	}
	pressLog(l, "enter")
	if shownFile(l) != "a.go" {
		t.Fatalf("enter in c2's file list: shown %q, want a.go", shownFile(l))
	}
	pressLog(l, "backspace")
	if l.open == nil || shownFile(l) != "" {
		t.Fatalf("backspace from c2's diff should show its file list")
	}
	pressLog(l, "backspace")
	if l.open != nil {
		t.Fatalf("backspace from c2's file list should show the commits")
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

	// P keeps a.go, which c2 changed as well.
	pressLog(l, "P")
	if l.cursor != 1 || shownFile(l) != "a.go" {
		t.Fatalf("P: commit %d shown %q, want c2 a.go", l.cursor, shownFile(l))
	}
	// c1 did not change a.go: its file list, even of a single file.
	pressLog(l, "P")
	if l.cursor != 2 || l.open == nil || shownFile(l) != "" {
		t.Fatalf("P: commit %d shown %q, want c1's file list", l.cursor, shownFile(l))
	}
	pressLog(l, "enter")
	if shownFile(l) != "c.go" {
		t.Fatalf("enter in c1's file list: shown %q, want c.go", shownFile(l))
	}
	// c0 cannot be loaded: stay and show the error.
	pressLog(l, "P")
	if l.cursor != 2 || !strings.Contains(l.open.app.open.view.msg, "cannot load c0") {
		t.Errorf("P onto a broken commit: commit %d msg %q", l.cursor, l.open.app.open.view.msg)
	}
	// Back via c2 (single file a.go) to c3, which changed a.go as well.
	pressLog(l, "backspace", "N", "enter", "N")
	if l.cursor != 0 || shownFile(l) != "a.go" {
		t.Errorf("N N: commit %d shown %q, want c3 a.go", l.cursor, shownFile(l))
	}
	pressLog(l, "N")
	if !strings.Contains(l.open.app.open.view.msg, "last commit") {
		t.Errorf("N on the newest commit: msg %q", l.open.app.open.view.msg)
	}
	// N/P also work from the file list, showing the list of the other
	// commit, also of a single file.
	pressLog(l, "backspace", "P")
	if l.cursor != 1 || l.open == nil || shownFile(l) != "" {
		t.Errorf("P from the file list: commit %d shown %q, want c2's file list", l.cursor, shownFile(l))
	}
	pressLog(l, "N")
	if l.cursor != 0 || l.open == nil || shownFile(l) != "" {
		t.Errorf("N from c2's file list: commit %d shown %q, want c3's file list", l.cursor, shownFile(l))
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
	view := ansi.Strip(l.frame())
	for _, want := range []string{"commit c3 · T · 2026-10-22", "    subject c3", "    body c3", "a.go", "b.go"} {
		if !strings.Contains(view, want) {
			t.Errorf("flow view lacks %q:\n%s", want, view)
		}
	}
	pressLog(l, "j")
	if loads["c2"] != 1 || loads["c1"] != 0 {
		t.Errorf("j: loads %v, want c2 loaded as it comes near, c1 not", loads)
	}
	pressLog(l, "end")
	if flowCommitAt(l) != 3 || !strings.Contains(ansi.Strip(l.frame()), "cannot load c0") {
		t.Errorf("end: flow at %d, want c0 with its error:\n%s", flowCommitAt(l), ansi.Strip(l.frame()))
	}
	for h, n := range loads {
		if n != 1 {
			t.Errorf("%s loaded %d times", h, n)
		}
	}
}

// newNumberedLog returns a log of commits c00 ... with one changed file each,
// counting the loads.
func newNumberedLog(n int) (*Log, map[string]int) {
	loads := map[string]int{}
	load := func(_ context.Context, c gitsrc.Commit) ([]source.File, error) {
		loads[c.Hash]++
		return []source.File{modified(c.Hash+".go", []string{"1"}, []string{"2"})}, nil
	}
	var commits []gitsrc.Commit
	for i := range n {
		h := fmt.Sprintf("c%02d", i)
		commits = append(commits, gitsrc.Commit{Hash: h, Short: h, Date: "2026-10-22", Author: "T", Subject: "subject " + h})
	}
	l := NewLog(config.Default(), "main", commits, false, load, nil)
	l.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	return l, loads
}

// home and end load the commits around where the cursor lands, not those on
// the way; these load as they come near, and the cursor keeps its commit.
func TestLogFlowEndsLoadOnlyAround(t *testing.T) {
	l, loads := newNumberedLog(30)
	pressLog(l, "f", "end")
	if flowCommitAt(l) != 29 || l.flow.cursor != len(l.flow.rows)-1 {
		t.Fatalf("end: at commit %d row %d of %d, want the last row of c29", flowCommitAt(l), l.flow.cursor, len(l.flow.rows))
	}
	if loads["c29"] != 1 || loads["c15"] != 0 {
		t.Errorf("end: loads %v, want c29 loaded, c15 not", loads)
	}
	for at := 29; l.flow.cursor > 0; {
		pressLog(l, "k")
		if got := flowCommitAt(l); got != at && got != at-1 {
			t.Fatalf("k: from c%02d to c%02d", at, got)
		} else {
			at = got
		}
	}
	if len(loads) != 30 {
		t.Errorf("k up to the top: %d commits loaded, want all 30", len(loads))
	}
	for h, n := range loads {
		if n != 1 {
			t.Errorf("%s loaded %d times", h, n)
		}
	}

	l, loads = newNumberedLog(30)
	pressLog(l, "f", "end", "home")
	if flowCommitAt(l) != 0 || l.flow.cursor != 0 || loads["c15"] != 0 {
		t.Errorf("end home: at commit %d row %d, loads %v; want the first row, c15 not loaded", flowCommitAt(l), l.flow.cursor, loads)
	}
}

// Staying in the commit, end has no use for the commits further on.
func TestLogFlowEndStayingInCommitLoadsNoMore(t *testing.T) {
	l, loads := newNumberedLog(30)
	l.cfg.Settings.StayInCommit = true
	pressLog(l, "f", "end")
	if flowCommitAt(l) != 0 || loads["c29"] != 0 || loads["c15"] != 0 {
		t.Errorf("end: at commit %d, loads %v; want c00, neither c15 nor c29 loaded", flowCommitAt(l), loads)
	}
}

func TestLogReversedNextPrevCommit(t *testing.T) {
	l, _ := newTestLogOrder(true) // c0 c1 c2 c3
	pressLog(l, "j", "enter")     // c1's file list
	pressLog(l, "N")
	if l.cursor != 2 || l.open == nil || shownFile(l) != "" {
		t.Fatalf("N: commit %d shown %q, want c2's file list", l.cursor, shownFile(l))
	}
	pressLog(l, "enter")
	if shownFile(l) != "a.go" {
		t.Fatalf("enter in c2's file list: shown %q, want a.go", shownFile(l))
	}
	pressLog(l, "N")
	if l.cursor != 3 || shownFile(l) != "a.go" {
		t.Fatalf("N: commit %d shown %q, want c3 a.go", l.cursor, shownFile(l))
	}
	pressLog(l, "N")
	if l.cursor != 3 || !strings.Contains(l.open.app.open.view.msg, "last commit") {
		t.Errorf("N on the newest commit: commit %d msg %q", l.cursor, l.open.app.open.view.msg)
	}
	pressLog(l, "P")
	if l.cursor != 2 {
		t.Errorf("P: commit %d, want c2", l.cursor)
	}

	pressLog(l, "f", "N")
	if flowCommitAt(l) != 3 {
		t.Fatalf("N in the flow: at %d, want c3", flowCommitAt(l))
	}
	pressLog(l, "N")
	if flowCommitAt(l) != 3 || l.flow.msg != "last commit" {
		t.Errorf("N past c3 in the flow: at %d msg %q", flowCommitAt(l), l.flow.msg)
	}
	pressLog(l, "P")
	if flowCommitAt(l) != 2 {
		t.Errorf("P in the flow: at %d, want c2", flowCommitAt(l))
	}
}

func TestLogFlowCommitKeys(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "f", "P")
	if flowCommitAt(l) != 1 {
		t.Fatalf("P: flow at %d, want c2", flowCommitAt(l))
	}
	pressLog(l, "P", "P", "P")
	if flowCommitAt(l) != 3 || l.flow.msg != "first commit" {
		t.Errorf("P past c0: flow at %d msg %q", flowCommitAt(l), l.flow.msg)
	}
	pressLog(l, "N")
	if flowCommitAt(l) != 2 {
		t.Errorf("N: flow at %d, want c1", flowCommitAt(l))
	}
	pressLog(l, "backspace")
	if l.open == nil || l.open.Hash != "c1" || l.open.app.open != nil {
		t.Fatalf("backspace: open %v, want c1's file list", l.open)
	}
	pressLog(l, "backspace")
	if l.open != nil || l.cursor != 2 {
		t.Fatalf("backspace twice: open %v cursor %d, want the commits on c1", l.open, l.cursor)
	}
	// Flow stays on: enter shows the flow again.
	pressLog(l, "enter")
	if flowCommitAt(l) != 2 {
		t.Errorf("enter with flow on: flow at %d, want c1", flowCommitAt(l))
	}
	// Off again on c1's message, a single-file commit: its file list is
	// shown, as of any commit.
	pressLog(l, "f")
	if l.flowShown() || l.open == nil || l.open.Hash != "c1" || shownFile(l) != "" {
		t.Errorf("f on c1: shown %q, want c1's file list", shownFile(l))
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

// Authors are padded by the columns they take: the date stays aligned with
// wide characters in the name.
func TestLogListWideAuthor(t *testing.T) {
	commits := []gitsrc.Commit{
		{Hash: "c00", Short: "c00", Date: "2026-10-22", Author: "日本語", Subject: "subject c00"},
		{Hash: "c01", Short: "c01", Date: "2026-10-22", Author: "abcdefgh", Subject: "subject c01"},
	}
	l := NewLog(config.Default(), "main", commits, false, nil, nil)
	l.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	lines := topLines(l, 4)
	column := func(line string) int { return ansi.StringWidth(line[:strings.Index(line, "2026-10-22")]) }
	if a, b := column(lines[2]), column(lines[3]); a != b {
		t.Errorf("date in columns %d and %d:\n%s\n%s", a, b, lines[2], lines[3])
	}
}

// Of an error of several lines, as git writes them, the log shows the first:
// the status bar and a note have one line.
func TestLogErrorFirstLine(t *testing.T) {
	fail := errors.New("x0\nx1")
	commits := []gitsrc.Commit{{Hash: "c00", Short: "c00", Subject: "subject c00"}}
	load := func(context.Context, gitsrc.Commit) ([]source.File, error) { return nil, fail }
	l := NewLog(config.Default(), "main", commits, false, load, nil).
		WithRelist(func(context.Context) (string, []gitsrc.Commit, error) { return "", nil, fail })
	l.Update(tea.WindowSizeMsg{Width: 80, Height: 12})

	_, cmd := l.Update(refreshMsg{})
	l.Update(cmd()) // the listing, which runs in the background
	if l.msg != "x0" {
		t.Errorf("listing again: %q", l.msg)
	}
	l.openCommit(0, "")
	if l.msg != "x0" {
		t.Errorf("opening a commit: %q", l.msg)
	}
	l.addCounts(countsMsg{err: fail})
	if l.msg != "line counts: x0" {
		t.Errorf("counting: %q", l.msg)
	}
	l.Update(l.flow.load(0)()) // the flow's load, which runs in the background
	if got := l.flow.parts[0].err; got != "x0" {
		t.Errorf("in the flow view: %q", got)
	}
}

func TestLogMessageBox(t *testing.T) {
	l, _ := newTestLog()
	l.commits[0].Body = "first line\nsecond line\nthird line"
	// Height 12: 9 rows below the branch, 4 for the list, 1 empty, 4 for
	// the box.
	view := ansi.Strip(l.frame())
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
	view = ansi.Strip(l.frame())
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
	setOption(l, l.cfg, "v")
	if l.boxHeight() != 0 || strings.Contains(ansi.Strip(l.frame()), "╭─") {
		t.Fatalf("v should hide the message box")
	}
	// The file list of a commit follows the same setting.
	pressLog(l, "enter")
	if strings.Contains(ansi.Strip(l.frame()), "╭─") {
		t.Errorf("file list shows a preview after v on the commit list")
	}
	setOption(l, l.cfg, "v")
	if !strings.Contains(ansi.Strip(l.frame()), "╭─ a.go ─") {
		t.Errorf("v in the file list should show the preview:\n%s", ansi.Strip(l.frame()))
	}
}

func TestFlowCursorRowsInverted(t *testing.T) {
	reverse := regexp.MustCompile(`\x1b\[(\d+;)*7(;\d+)*m`)
	l, _ := newTestLog()
	pressLog(l, "f", "end") // load all commits
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

func TestFlowMessageToggle(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "f") // on the bar of c3, the only commit with a body
	headerRows := func() int {
		n := 0
		for _, r := range l.flow.rows {
			if r.kind == flowCommit && r.part == 0 {
				n++
			}
		}
		return n
	}
	if n := headerRows(); n != 6 || !l.cfg.Settings.FullMessages {
		t.Fatalf("c3 header: %d rows, full %v; want 6 with the body", n, l.cfg.Settings.FullMessages)
	}
	pressLog(l, "j", "j", "j", "j") // on the body line
	setOption(l, l.cfg, "c")
	if r := l.flow.rows[l.flow.cursor]; headerRows() != 4 || l.cfg.Settings.FullMessages ||
		r.kind != flowCommit || r.line != 2 {
		t.Errorf("c: %d header rows, full %v, cursor on %v; want 4, off and the subject",
			headerRows(), l.cfg.Settings.FullMessages, r)
	}
	if strings.Contains(ansi.Strip(l.frame()), "body c3") {
		t.Errorf("short messages still show the body")
	}
	setOption(l, l.cfg, "c")
	if r := l.flow.rows[l.flow.cursor]; headerRows() != 6 || r.kind != flowCommit || r.line != 2 {
		t.Errorf("c again: %d header rows, cursor on %v; want 6 and still the subject", headerRows(), r)
	}
}

func TestFlowStayInCommit(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "f")
	setOption(l, l.cfg, "C") // on the bar of c3
	f := l.flow
	part := func() int { return f.rows[f.cursor].part }
	last := 0
	for i, r := range f.rows {
		if r.part == 0 {
			last = i
		}
	}
	if !l.cfg.Settings.StayInCommit || f.msg != "staying in commit c3" {
		t.Fatalf("C: setting %v, msg %q", l.cfg.Settings.StayInCommit, f.msg)
	}
	pressLog(l, "end")
	if f.cursor != last || f.msg != "stays in commit c3 (C)" {
		t.Errorf("end: cursor %d (last row of c3: %d), msg %q", f.cursor, last, f.msg)
	}
	for _, k := range []string{"j", "n", "]", "pgdown"} {
		pressLog(l, k)
		if f.cursor != last || f.msg != "stays in commit c3 (C)" {
			t.Errorf("%s at the end of c3: cursor %d, msg %q; want %d and the warning", k, f.cursor, f.msg, last)
		}
	}
	if !strings.Contains(ansi.Strip(l.frame()), "commit 1/4 (C)") {
		t.Errorf("status lacks (C)")
	}
	// N and P still switch; the commit switched to is the one to stay in.
	pressLog(l, "P")
	if part() != 1 {
		t.Fatalf("P: on commit %d, want 1", part())
	}
	first := f.cursor
	for _, k := range []string{"k", "pgup", "home"} {
		pressLog(l, k)
		if f.cursor != first || f.msg != "stays in commit c2 (C)" {
			t.Errorf("%s at the start of c2: cursor %d, msg %q; want %d and the warning", k, f.cursor, f.msg, first)
		}
	}
	setOption(l, l.cfg, "C")
	pressLog(l, "k")
	if l.cfg.Settings.StayInCommit || part() != 0 {
		t.Errorf("C off, k: setting %v, on commit %d; want off and back in c3", l.cfg.Settings.StayInCommit, part())
	}
}

func TestLogExpandsTabsInCommits(t *testing.T) {
	commits := []gitsrc.Commit{{Hash: "c1", Short: "c1", Date: "2026-10-22", Author: "A\tB",
		Subject: "sub\tject\x01", Body: "body\tline\nsecond\t\tline"}}
	load := func(context.Context, gitsrc.Commit) ([]source.File, error) {
		return []source.File{modified("a.go", []string{"1"}, []string{"2"})}, nil
	}
	l := NewLog(config.Default(), "main", commits, false, load, nil)
	l.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if line := rawText(l.frame(), 60); line != "" {
		t.Errorf("commit list line with raw text: %q", line)
	}
	l.cfg.Settings.Flow = true
	pressLog(l, "enter")
	if line := rawText(l.frame(), 60); line != "" {
		t.Errorf("flow line with raw text: %q", line)
	}
	if !strings.Contains(ansi.Strip(l.frame()), "    body    line") {
		t.Errorf("body not shown with its tab expanded:\n%s", ansi.Strip(l.frame()))
	}
}

func TestMarkOnCommitListExplains(t *testing.T) {
	l, _ := newTestLog()
	if err := l.cfg.SetMark("a", config.Mark{Path: "a.go", Line: 1, Text: "2"}); err != nil {
		t.Fatal(err)
	}
	p := WithMarks(l.cfg, l)
	p.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	pressRoot(p, "\\", "enter")
	if want := "mark a: open a commit to jump to it"; l.msg != want {
		t.Errorf("enter in the marks popup on the commit list: msg %q, want %q", l.msg, want)
	}
	if l.open != nil || l.cursor != 0 {
		t.Errorf("the mark letter moved the list: open %v, cursor %d", l.open, l.cursor)
	}
}

// newCountingLog returns a log of 100 commits h0 to h99 whose line counts
// come from a counter, +n+1 -2 for hn, and the hashes it was asked for, per
// call.
func newCountingLog(fail bool) (*Log, *[][]string) {
	var commits []gitsrc.Commit
	for i := range 100 {
		h := fmt.Sprintf("h%d", i)
		commits = append(commits, gitsrc.Commit{Hash: h, Short: h, Date: "2026-10-22", Author: "T", Subject: "subject " + h})
	}
	var asked [][]string
	counts := func(_ context.Context, hashes []string) (map[string]gitsrc.LineCounts, error) {
		asked = append(asked, hashes)
		if fail {
			return nil, errors.New("git broke")
		}
		out := map[string]gitsrc.LineCounts{}
		for _, h := range hashes {
			n, _ := strconv.Atoi(h[1:])
			out[h] = gitsrc.LineCounts{Added: n + 1, Removed: 2}
		}
		return out, nil
	}
	return NewLog(config.Default(), "main", commits, false, nil, counts), &asked
}

// listLine returns the line of the commit list that shows commit short.
func listLine(l *Log, short string) string {
	for _, line := range strings.Split(ansi.Strip(l.frame()), "\n") {
		if strings.HasPrefix(line, " "+short+" ") {
			return line
		}
	}
	return ""
}

func TestLogCountsInBackground(t *testing.T) {
	l, asked := newCountingLog(false)
	_, cmd := l.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	// The list is drawn before any count: blank count columns.
	before := listLine(l, "h0")
	if len(*asked) != 0 || cmd == nil || strings.Contains(before, "+") {
		t.Fatalf("before counting: asked %v, cmd %v, h0 line %q", *asked, cmd != nil, before)
	}
	// While the count runs, keys work and start no second count.
	if _, cmd2 := l.Update(keyMsg("j")); cmd2 != nil || l.cursor != 1 {
		t.Errorf("j while counting: cursor %d, cmd %v; want 1 and no second count", l.cursor, cmd2 != nil)
	}
	_, cmd = l.Update(cmd())
	// The 4 commits shown and 4 beyond, at once.
	if want := []string{"h0", "h1", "h2", "h3", "h4", "h5", "h6", "h7"}; len(*asked) != 1 || !slices.Equal((*asked)[0], want) {
		t.Errorf("first count asked %v, want %v", *asked, want)
	}
	if cmd != nil {
		t.Error("all commits near the screen are counted, yet another count follows")
	}
	after := listLine(l, "h0")
	if !strings.Contains(after, "+1") || !strings.Contains(after, "-2") {
		t.Errorf("h0 after counting: %q, want +1 -2", after)
	}
	// The columns keep their place as counts arrive.
	if strings.Index(before, "subject") != strings.Index(after, "subject") {
		t.Errorf("subject moved from column %d to %d:\n%q\n%q", strings.Index(before, "subject"),
			strings.Index(after, "subject"), before, after)
	}

	// At the end of the list, its commits are counted, the ones between not.
	pressLog(l, "end")
	if line := listLine(l, "h99"); !strings.Contains(line, "+100") {
		t.Errorf("end: h99 line %q, want +100", line)
	}
	seen := map[string]bool{}
	for _, call := range *asked {
		for _, h := range call {
			if seen[h] {
				t.Errorf("%s counted twice: %v", h, *asked)
			}
			seen[h] = true
		}
	}
	if seen["h50"] {
		t.Errorf("h50, never near the screen, was counted: %v", *asked)
	}
}

func TestLogCountsFail(t *testing.T) {
	l, asked := newCountingLog(true)
	_, cmd := l.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	l.Update(cmd()) // the first count, which fails
	if !strings.Contains(l.msg, "line counts: git broke") {
		t.Errorf("msg %q, want the error", l.msg)
	}
	pressLog(l, "end", "home")
	if len(*asked) != 1 {
		t.Errorf("counted %d times after an error, want once: %v", len(*asked), *asked)
	}
}

// relistTo makes r list the commits hashes of l, on branch, newest first.
func relistTo(l *Log, branch string, hashes ...string) {
	l.WithRelist(func(context.Context) (string, []gitsrc.Commit, error) {
		var commits []gitsrc.Commit
		for _, h := range hashes {
			commits = append(commits, gitsrc.Commit{Hash: h, Short: h, Date: "2026-10-24", Author: "T", Subject: "subject " + h})
		}
		return branch, commits, nil
	})
}

func TestLogRelist(t *testing.T) {
	l, loads := newTestLog()
	pressLog(l, "enter", "enter") // c3, a.go
	view := l.open.app.open.view
	relistTo(l, "topic", "c4", "c3", "c2", "c1")
	pressLog(l, "r")
	if l.cursor != 1 || shownFile(l) != "a.go" || l.open.app.open.view != view {
		t.Fatalf("after r: commit %d shown %q; want c3 a.go still open in its view", l.cursor, shownFile(l))
	}
	if view.msg != "1 new, 1 gone" || l.branch != "topic" || l.list.n != 4 {
		t.Errorf("msg %q branch %q n %d", view.msg, l.branch, l.list.n)
	}
	if loads["c3"] != 1 {
		t.Errorf("c3 loaded %d times, want once: its files are kept", loads["c3"])
	}
	// N goes to the new commit, which the test cannot load.
	pressLog(l, "N")
	if !strings.Contains(view.msg, "cannot load c4") {
		t.Errorf("N after r: msg %q, want it tried c4", view.msg)
	}
}

func TestLogRelistOpenCommitGone(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "j", "enter") // c2
	relistTo(l, "main", "c3", "c1")
	pressLog(l, "f5")
	if l.open != nil || l.cursor != 1 || l.msg != "commit c2 is gone; 2 gone" {
		t.Errorf("open %v cursor %d msg %q; want the list and the message", l.open, l.cursor, l.msg)
	}
}

func TestLogRelistFlowStaysOnCommit(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "f", "P") // c2 in the flow
	relistTo(l, "main", "c4", "c3", "c2", "c1", "c0")
	pressLog(l, "r")
	if flowCommitAt(l) != 2 || l.cursor != 2 || l.flow.msg != "1 new" {
		t.Errorf("flow at %d, cursor %d, msg %q; want c2, now 2", flowCommitAt(l), l.cursor, l.flow.msg)
	}
	relistTo(l, "main", "c4", "c3")
	pressLog(l, "r")
	if l.flowShown() || l.msg != "commit c2 is gone; 3 gone" {
		t.Errorf("flow shown %v msg %q; want the list after c2 is gone", l.flowShown(), l.msg)
	}
}

func TestLogRelistFails(t *testing.T) {
	l, _ := newTestLog()
	l.WithRelist(func(context.Context) (string, []gitsrc.Commit, error) { return "", nil, errors.New("git log: broken") })
	pressLog(l, "r")
	if l.msg != "git log: broken" || len(l.commits) != 4 || l.branch != "main" {
		t.Errorf("msg %q, %d commits, branch %q", l.msg, len(l.commits), l.branch)
	}
}

func TestCommitListArrows(t *testing.T) {
	var commits []gitsrc.Commit
	for i := range 30 {
		h := fmt.Sprintf("c%02d", i)
		commits = append(commits, gitsrc.Commit{Hash: h, Short: h, Date: "2026-10-24", Author: "T", Subject: "subject " + h})
	}
	l := NewLog(config.Default(), "main", commits, false, nil, nil)
	l.cfg.Settings.Preview = false
	l.Update(tea.WindowSizeMsg{Width: 80, Height: 8}) // 5 rows
	shown := func() []string {
		var out []string
		for _, r := range viewRows(l.frame(), 2, 2+l.listHeight()) {
			if strings.HasPrefix(r, "  ↑") || strings.HasPrefix(r, "  ↓") {
				out = append(out, r)
			} else {
				out = append(out, strings.Fields(r)[0])
			}
		}
		return out
	}
	for _, tt := range []struct {
		keys []string
		want []string
	}{
		{nil, []string{"c00", "c01", "c02", "c03", "  ↓ 26 more"}},
		{[]string{"j", "j", "j", "j"}, []string{"  ↑ 2 more", "c02", "c03", "c04", "  ↓ 25 more"}},
		{[]string{"end"}, []string{"  ↑ 26 more", "c26", "c27", "c28", "c29"}},
		{[]string{"k", "k", "k", "k"}, []string{"  ↑ 25 more", "c25", "c26", "c27", "  ↓ 2 more"}},
	} {
		pressLog(l, tt.keys...)
		if got := shown(); !slices.Equal(got, tt.want) {
			t.Errorf("after %q (cursor c%02d):\n got %q\nwant %q", tt.keys, l.cursor, got, tt.want)
		}
	}
}

func TestLogFlowBackToFiles(t *testing.T) {
	l, _ := newTestLog()       // c3: a.go, b.go; c2: a.go; c1: c.go
	pressLog(l, "f", "n", "n") // the flow on c3's b.go
	if _, file, _ := l.flow.position(); file != 1 {
		t.Fatalf("not on b.go: file %d", file)
	}
	// backspace: the file list of c3, on b.go, in flow mode.
	pressLog(l, "backspace")
	if l.flowShown() || l.open == nil || l.open.Hash != "c3" || l.open.app.open != nil || l.open.app.cursor != 1 {
		t.Fatalf("backspace from the flow: flow %v, open %v; want c3's file list on b.go", l.flowShown(), l.open)
	}
	if v := ansi.Strip(l.frame()); !strings.Contains(v, "b.go") || !strings.Contains(v, "backspace commits") {
		t.Errorf("file list view:\n%s", v)
	}
	// enter: back into the flow, on the file chosen.
	pressLog(l, "k", "enter")
	if _, file, _ := l.flow.position(); !l.flowShown() || file != 0 {
		t.Errorf("enter in flow mode: flow %v on file %d, want the flow on a.go", l.flowShown(), file)
	}
	// backspace twice: the file list, then the commits.
	pressLog(l, "backspace", "backspace")
	if l.open != nil || l.cursor != 0 {
		t.Errorf("backspace twice: open %v cursor %d, want the commits on c3", l.open, l.cursor)
	}
	// A commit of one file has its list too: backspace shows it, then the
	// commits.
	pressLog(l, "enter", "P", "backspace")
	if l.open == nil || l.open.Hash != "c2" || l.open.app.open != nil {
		t.Errorf("backspace from c2 in the flow: open %v, want c2's file list", l.open)
	}
	pressLog(l, "backspace")
	if l.open != nil || l.cursor != 1 {
		t.Errorf("backspace from c2's file list: open %v cursor %d, want the commits on c2", l.open, l.cursor)
	}
	// f in the file list turns flow mode off and keeps the list.
	pressLog(l, "k", "enter", "n", "backspace", "f")
	if l.cfg.Settings.Flow || l.open == nil || l.open.app.open != nil {
		t.Errorf("f in the file list: flow %v, open %v; want flow off and the list", l.cfg.Settings.Flow, l.open)
	}
	pressLog(l, "enter")
	if shownFile(l) != "a.go" {
		t.Errorf("enter without flow mode: shown %q, want a.go's diff view", shownFile(l))
	}
}

func TestStatusBarKeys(t *testing.T) {
	l, _ := newTestLog()
	status := func() string {
		v := ansi.Strip(l.frame())
		return strings.TrimSpace(v[strings.LastIndex(v, "\n")+1:])
	}
	l.Update(tea.WindowSizeMsg{Width: 250, Height: 12}) // room for all keys
	for _, tt := range []struct {
		keys []string
		want string
	}{
		{nil, "enter open · f flow · s unified · d git diff · o settings · ? help · q quit"},
		{[]string{"enter"}, "enter open · backspace commits · N/P commit · f flow · s unified · d git diff · o settings · ? help · q quit"},
		{[]string{"enter"}, "enter mark/anchor · backspace files · N/P commit · f flow · s unified · d git diff · o settings · ? help · q quit"},
		{[]string{"f"}, "enter mark/anchor · backspace files · N/P commit · f file · s unified · d git diff · o settings · ? help · q quit"},
	} {
		pressLog(l, tt.keys...)
		if got := status(); !strings.HasPrefix(got, tt.want) {
			t.Errorf("after %q:\n got %q\nwant %q", tt.keys, got, tt.want)
		}
	}
	// The anchors at the very right.
	pressLog(l, "f")
	if got := status(); !strings.HasSuffix(got, "row 1/1 · anchors 0") {
		t.Errorf("diff view status %q, want the anchors last", got)
	}
	// The label of s names the layout it switches to.
	l.cfg.Settings.SideBySide = false
	if got := status(); !strings.Contains(got, " · f flow · s split · d git diff") {
		t.Errorf("normal layout: %q", got)
	}
	// Two files, without a list: neither N/P, f nor backspace.
	m := newTestModel([]string{"a"}, []string{"b"})
	if got := m.help(); got != "enter mark/anchor · s unified · d git diff · o settings · ? help · q quit" {
		t.Errorf("two files: %q", got)
	}
}

func TestCommitListArrowsSweep(t *testing.T) {
	t.Parallel() // a sweep: its own config and views, nothing shared
	var commits []gitsrc.Commit
	for i := range 30 {
		h := fmt.Sprintf("c%02d", i)
		commits = append(commits, gitsrc.Commit{Hash: h, Short: h, Date: "2026-10-24", Author: "T", Subject: "s"})
	}
	index := func(r string) int {
		var k int
		fmt.Sscanf(strings.TrimSpace(r), "c%d", &k)
		return k
	}
	for _, preview := range []bool{false, true} {
		for height := minHeight; height <= 30; height++ {
			t.Run(fmt.Sprintf("preview %v height %d", preview, height), func(t *testing.T) {
				t.Parallel()
				l := NewLog(config.Default(), "main", commits, false, nil, nil)
				l.cfg.Settings.Preview = preview
				l.Update(tea.WindowSizeMsg{Width: 40, Height: height})
				what := fmt.Sprintf("commit list, height %d, preview %v", height, preview)
				h := l.listHeight()
				rows := func() []string { return viewRows(l.frame(), 2, 2+h) }
				shown := checkShown(t, what, rows(), h, len(commits), l.cursor, nil, index)
				for _, k := range sweepKeys(len(commits)) {
					pressLog(l, k)
					shown = checkShown(t, what+", "+k, rows(), h, len(commits), l.cursor, shown, index)
				}
			})
		}
	}
}

func TestCommitListHistoryKeys(t *testing.T) {
	for _, oldestFirst := range []bool{false, true} {
		l, _ := newTestLogOrder(oldestFirst)
		hash := func() string { return l.commits[l.cursor].Hash }
		// From c2, the next commit in history is c3, the previous one c1,
		// in either order of the list; n and p do as N and P.
		l.cursor = slices.IndexFunc(l.commits, func(e *commitEntry) bool { return e.Hash == "c2" })
		l.scrollToCursor()
		for _, tt := range []struct{ key, want string }{
			{"N", "c3"}, {"P", "c2"}, {"p", "c1"}, {"n", "c2"},
		} {
			pressLog(l, tt.key)
			if hash() != tt.want {
				t.Errorf("oldest first %v: %s on to %s, want %s", oldestFirst, tt.key, hash(), tt.want)
			}
		}
		// At the ends the cursor stays, with the messages of N and P from a
		// commit's files.
		pressLog(l, "N", "n")
		if hash() != "c3" || l.msg != "last commit" {
			t.Errorf("oldest first %v: past the newest: on %s, msg %q", oldestFirst, hash(), l.msg)
		}
		pressLog(l, "P", "P", "P", "p")
		if hash() != "c0" || l.msg != "first commit" {
			t.Errorf("oldest first %v: past the oldest: on %s, msg %q", oldestFirst, hash(), l.msg)
		}
		// The same direction as N from the commit's files.
		pressLog(l, "N", "N", "enter")
		if l.open == nil || l.open.Hash != "c2" {
			t.Fatalf("oldest first %v: open %v, want c2", oldestFirst, l.open)
		}
		pressLog(l, "N")
		if l.open.Hash != "c3" {
			t.Errorf("oldest first %v: N from c2's files to %s, want c3", oldestFirst, l.open.Hash)
		}
	}
}

func TestStatusBarOneColor(t *testing.T) {
	l, _ := newTestLog()
	hint := l.cfg.Theme.Hint
	if v := l.frame(); !strings.Contains(v, hint.Render("enter open · f flow · s unified · d git diff · o settings · ? help · q quit")) {
		t.Errorf("status bar not in the hint style alone:\n%q", v[strings.LastIndex(v, "\n")+1:])
	}
	if got := popupTitle(l.cfg.Theme, "marks", "", "enter jump · esc close"); !strings.Contains(got, hint.Render("enter jump · esc close")) {
		t.Errorf("popup title %q", got)
	}
}

func TestLogFlowOffInCommits(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "f", "n", "backspace", "backspace")
	if l.open != nil || !l.cfg.Settings.Flow {
		t.Fatalf("backspace twice: open %v, flow %v; want the commits with flow on", l.open, l.cfg.Settings.Flow)
	}
	if v := ansi.Strip(l.frame()); !strings.Contains(v, " · f file · ") {
		t.Errorf("flow on, the bar should offer the single file view:\n%s", v)
	}
	pressLog(l, "f")
	if l.cfg.Settings.Flow || l.msg != "" || l.open != nil {
		t.Errorf("f: flow %v, msg %q, open %v; want the commits with flow off", l.cfg.Settings.Flow, l.msg, l.open)
	}
	if v := ansi.Strip(l.frame()); !strings.Contains(v, " · f flow · ") {
		t.Errorf("flow off, the bar should offer the flow view:\n%s", v)
	}
}

func TestLogFlowFromFileList(t *testing.T) {
	l, _ := newTestLog() // c3: a.go, b.go
	pressLog(l, "enter", "j")
	if l.open == nil || l.open.app.open != nil || l.open.app.cursor != 1 {
		t.Fatalf("enter j: open %v; want c3's file list on b.go", l.open)
	}
	pressLog(l, "f")
	if _, file, _ := l.flow.position(); !l.flowShown() || file != 1 {
		t.Errorf("f: flow %v on file %d, want the flow on b.go", l.flowShown(), file)
	}
}

// newLoadingLog returns a log of commits c00 ... with two changed files each,
// whose loads record their contexts, in the order they run.
func newLoadingLog(n int) (*Log, *[]context.Context) {
	ctxs := &[]context.Context{}
	load := func(ctx context.Context, c gitsrc.Commit) ([]source.File, error) {
		*ctxs = append(*ctxs, ctx)
		return []source.File{modified(c.Hash+"a.go", []string{"1"}, []string{"2"}),
			modified(c.Hash+"b.go", []string{"3"}, []string{"4"})}, nil
	}
	var commits []gitsrc.Commit
	for i := range n {
		h := fmt.Sprintf("c%02d", i)
		commits = append(commits, gitsrc.Commit{Hash: h, Short: h, Date: "2026-10-22", Author: "T", Subject: "subject " + h})
	}
	l := NewLog(config.Default(), "main", commits, false, load, nil)
	l.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	return l, ctxs
}

// drain runs cmd and the commands that follow, as the runtime does.
func drain(m tea.Model, cmd tea.Cmd) {
	for cmd != nil {
		_, cmd = m.Update(cmd())
	}
}

// A log that loading again emptied has no commit for f, or enter in the
// flow view, to show.
func TestLogEmptiedByReload(t *testing.T) {
	l, _ := newTestLog()
	relistTo(l, "main")
	pressLog(l, "r")
	pressLog(l, "f")
	if l.flowShown() || l.msg != "no commits" {
		t.Errorf("f: flow shown %v, msg %q", l.flowShown(), l.msg)
	}
	l.msg = ""
	pressLog(l, "enter")
	if l.flowShown() || l.msg != "no commits" {
		t.Errorf("enter with flow on: flow shown %v, msg %q", l.flowShown(), l.msg)
	}
}

// Of two commits asked for, the one asked for last opens: the files of the
// other, which come late, are dropped, and its git commands were ended.
func TestLogLatestRequestWins(t *testing.T) {
	l, ctxs := newLoadingLog(4)
	_, first := l.Update(keyMsg("enter"))
	if !strings.HasPrefix(l.msg, "loading c00") {
		t.Errorf("while loading: msg %q", l.msg)
	}
	l.Update(keyMsg("j"))
	_, second := l.Update(keyMsg("enter"))
	l.Update(second())
	late := first()
	if (*ctxs)[1].Err() == nil {
		t.Error("the load replaced was not ended")
	}
	l.Update(late)
	if l.open == nil || l.open.Hash != "c01" || l.msg != "" {
		t.Errorf("open %v, msg %q; want c01, no message", l.open, l.msg)
	}
}

// Commits not loaded yet show that they load; end stays on the last row as
// they come.
func TestLogFlowLoadsInTheBackground(t *testing.T) {
	l, loads := newNumberedLog(30)
	pressLog(l, "f")
	_, cmd := l.Update(keyMsg("end"))
	if !strings.Contains(ansi.Strip(l.frame()), "loading…") || cmd == nil {
		t.Fatalf("end: load %v, frame:\n%s", cmd != nil, ansi.Strip(l.frame()))
	}
	for cmd != nil {
		if l.flow.cursor != len(l.flow.rows)-1 {
			t.Fatalf("row %d of %d, want the last", l.flow.cursor, len(l.flow.rows))
		}
		_, cmd = l.Update(cmd())
	}
	if l.flow.cursor != len(l.flow.rows)-1 || strings.Contains(ansi.Strip(l.frame()), "loading…") || loads["c29"] != 1 {
		t.Errorf("loaded: row %d of %d, loads %v, frame:\n%s", l.flow.cursor, len(l.flow.rows), loads, ansi.Strip(l.frame()))
	}
}

// next file waits for a commit not loaded yet on its way, saying so, and
// goes on when it comes; another key ends the wait.
func TestLogFlowSearchWaits(t *testing.T) {
	// untilWaiting presses n, holding the loads back, until n waits.
	untilWaiting := func(l *Log) tea.Cmd {
		var held tea.Cmd
		for range 60 {
			cursor := l.flow.cursor
			_, cmd := l.Update(keyMsg("n"))
			if cmd != nil {
				held = cmd
			}
			if l.flow.waiting != nil {
				if l.flow.cursor != cursor || !strings.HasPrefix(l.flow.msg, "loading c") {
					t.Fatalf("waiting: cursor moved %v, msg %q", l.flow.cursor != cursor, l.flow.msg)
				}
				return held
			}
		}
		t.Fatal("n never waits")
		return nil
	}
	l, _ := newNumberedLog(30)
	pressLog(l, "f")
	held := untilWaiting(l)
	want := slices.Index(l.flow.parts, l.flow.waiting.part)
	drain(l, held)
	if r := l.flow.rows[l.flow.cursor]; l.flow.waiting != nil || r.kind != flowFile || r.part != want {
		t.Errorf("loaded: waiting %v, at %+v; want the file of commit %d", l.flow.waiting != nil, r, want)
	}

	l, _ = newNumberedLog(30)
	pressLog(l, "f")
	held = untilWaiting(l)
	l.Update(keyMsg("k"))
	cursor := l.flow.cursor
	drain(l, held)
	if l.flow.waiting != nil || l.flow.cursor != cursor && l.flow.rows[l.flow.cursor] != l.flow.rows[cursor] {
		t.Errorf("k ended the wait: waiting %v, cursor %d → %d", l.flow.waiting != nil, cursor, l.flow.cursor)
	}
}

// backspace on a commit still loading shows its files once they come.
func TestLogFlowBackWhileLoading(t *testing.T) {
	l, _ := newLoadingLog(20)
	pressLog(l, "f")
	var held []tea.Cmd
	for range 20 {
		if p, _, _ := l.flow.position(); l.flow.unloaded(p) {
			break
		}
		if _, cmd := l.Update(keyMsg("P")); cmd != nil {
			held = append(held, cmd)
		}
	}
	p, _, _ := l.flow.position()
	if !l.flow.unloaded(p) {
		t.Fatal("no commit left that is not loaded")
	}
	_, back := l.Update(keyMsg("backspace"))
	_, load := l.Update(back())
	if load == nil || l.files {
		t.Fatalf("backspace: load %v, files shown %v", load != nil, l.files)
	}
	drain(l, load)
	for _, cmd := range held {
		drain(l, cmd)
	}
	if !l.files || l.open != l.commits[p] {
		t.Errorf("files shown %v, open %v; want the list of commit %d", l.files, l.open, p)
	}
}

// The flow view takes the files the list loaded; files that come twice are
// made into views once.
func TestLogFlowTakesLoadedFiles(t *testing.T) {
	l, loads := newTestLog()
	pressLog(l, "enter", "backspace", "f")
	if loads["c3"] != 1 || l.flow.parts[0].app != l.commits[0].app {
		t.Errorf("loads %v, the flow has the list's files %v", loads, l.flow.parts[0].app == l.commits[0].app)
	}
	once, twice := l.flow.load(1)(), l.flow.load(1)()
	l.Update(once)
	app := l.commits[1].app
	l.Update(twice)
	if app == nil || l.commits[1].app != app {
		t.Error("the files that came again replaced the views")
	}
}

// The end of the program ends the git commands still running.
func TestStopEndsLoads(t *testing.T) {
	l, ctxs := newLoadingLog(3)
	cmd := l.flow.load(1)
	WithSizeGuard(l.cfg, WithDiffSwitch(l.cfg, l, func(m tea.Model) tea.Model { return m }, nil)).Stop()
	cmd()
	if (*ctxs)[0].Err() == nil {
		t.Error("the load of the flow view runs on")
	}
}
