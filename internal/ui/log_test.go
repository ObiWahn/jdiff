package ui

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
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

	// P keeps a.go, which c2 changed as well.
	pressLog(l, "P")
	if l.cursor != 1 || shownFile(l) != "a.go" {
		t.Fatalf("P: commit %d shown %q, want c2 a.go", l.cursor, shownFile(l))
	}
	// c1 did not change a.go, but it has a single file.
	pressLog(l, "P")
	if l.cursor != 2 || shownFile(l) != "c.go" {
		t.Fatalf("P: commit %d shown %q, want c1 c.go", l.cursor, shownFile(l))
	}
	// c0 cannot be loaded: stay and show the error.
	pressLog(l, "P")
	if l.cursor != 2 || !strings.Contains(l.open.app.open.view.msg, "cannot load c0") {
		t.Errorf("P onto a broken commit: commit %d msg %q", l.cursor, l.open.app.open.view.msg)
	}
	// Back via c2 (single file a.go) to c3, which changed a.go as well.
	pressLog(l, "N", "N")
	if l.cursor != 0 || shownFile(l) != "a.go" {
		t.Errorf("N N: commit %d shown %q, want c3 a.go", l.cursor, shownFile(l))
	}
	pressLog(l, "N")
	if !strings.Contains(l.open.app.open.view.msg, "last commit") {
		t.Errorf("N on the newest commit: msg %q", l.open.app.open.view.msg)
	}
	// N/P also work from the file list.
	pressLog(l, "backspace", "P")
	if l.cursor != 1 || shownFile(l) != "a.go" {
		t.Errorf("P from the file list: commit %d shown %q", l.cursor, shownFile(l))
	}
	// The open file a.go is kept when going back to c3.
	pressLog(l, "N")
	if l.cursor != 0 || shownFile(l) != "a.go" {
		t.Errorf("N back to c3 with a.go open: commit %d shown %q", l.cursor, shownFile(l))
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

func TestLogReversedNextPrevCommit(t *testing.T) {
	l, _ := newTestLogOrder(true) // c0 c1 c2 c3
	pressLog(l, "j", "enter")     // c1, c.go
	pressLog(l, "N")
	if l.cursor != 2 || shownFile(l) != "a.go" {
		t.Fatalf("N: commit %d shown %q, want c2 a.go", l.cursor, shownFile(l))
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
	pressLog(l, "c")
	if r := l.flow.rows[l.flow.cursor]; headerRows() != 4 || l.cfg.Settings.FullMessages ||
		r.kind != flowCommit || r.line != 2 {
		t.Errorf("c: %d header rows, full %v, cursor on %v; want 4, off and the subject",
			headerRows(), l.cfg.Settings.FullMessages, r)
	}
	if strings.Contains(ansi.Strip(l.View()), "body c3") {
		t.Errorf("short messages still show the body")
	}
	pressLog(l, "c")
	if r := l.flow.rows[l.flow.cursor]; headerRows() != 6 || r.kind != flowCommit || r.line != 2 {
		t.Errorf("c again: %d header rows, cursor on %v; want 6 and still the subject", headerRows(), r)
	}
}

func TestFlowStayInCommit(t *testing.T) {
	l, _ := newTestLog()
	pressLog(l, "f", "C") // on the bar of c3
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
	pressLog(l, "G")
	if f.cursor != last || f.msg != "stays in commit c3 (C)" {
		t.Errorf("G: cursor %d (last row of c3: %d), msg %q", f.cursor, last, f.msg)
	}
	for _, k := range []string{"j", "n", "]", "pgdown"} {
		pressLog(l, k)
		if f.cursor != last || f.msg != "stays in commit c3 (C)" {
			t.Errorf("%s at the end of c3: cursor %d, msg %q; want %d and the warning", k, f.cursor, f.msg, last)
		}
	}
	if !strings.Contains(ansi.Strip(l.View()), "commit 1/4 (C)") {
		t.Errorf("status lacks (C)")
	}
	// N and P still switch; the commit switched to is the one to stay in.
	pressLog(l, "P")
	if part() != 1 {
		t.Fatalf("P: on commit %d, want 1", part())
	}
	first := f.cursor
	for _, k := range []string{"k", "pgup", "g"} {
		pressLog(l, k)
		if f.cursor != first || f.msg != "stays in commit c2 (C)" {
			t.Errorf("%s at the start of c2: cursor %d, msg %q; want %d and the warning", k, f.cursor, f.msg, first)
		}
	}
	pressLog(l, "C", "k")
	if l.cfg.Settings.StayInCommit || part() != 0 {
		t.Errorf("C off, k: setting %v, on commit %d; want off and back in c3", l.cfg.Settings.StayInCommit, part())
	}
}

func TestLogExpandsTabsInCommits(t *testing.T) {
	commits := []gitsrc.Commit{{Hash: "c1", Short: "c1", Date: "2026-10-22", Author: "A\tB",
		Subject: "sub\tject\x01", Body: "body\tline\nsecond\t\tline"}}
	load := func(gitsrc.Commit) ([]source.File, error) {
		return []source.File{modified("a.go", []string{"1"}, []string{"2"})}, nil
	}
	l := NewLog(config.Default(), "main", commits, false, load, nil)
	l.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if line := rawText(l.View(), 60); line != "" {
		t.Errorf("commit list line with raw text: %q", line)
	}
	l.cfg.Settings.Flow = true
	pressLog(l, "enter")
	if line := rawText(l.View(), 60); line != "" {
		t.Errorf("flow line with raw text: %q", line)
	}
	if !strings.Contains(ansi.Strip(l.View()), "    body    line") {
		t.Errorf("body not shown with its tab expanded:\n%s", ansi.Strip(l.View()))
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
	counts := func(hashes []string) (map[string]gitsrc.LineCounts, error) {
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
	for _, line := range strings.Split(ansi.Strip(l.View()), "\n") {
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
	pressLog(l, "G")
	if line := listLine(l, "h99"); !strings.Contains(line, "+100") {
		t.Errorf("G: h99 line %q, want +100", line)
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
	pressLog(l, "G", "g")
	if len(*asked) != 1 {
		t.Errorf("counted %d times after an error, want once: %v", len(*asked), *asked)
	}
}
