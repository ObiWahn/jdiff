package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/gitsrc"
)

// flowKind is the kind of a row of the flow view.
type flowKind int

const (
	flowCommit flowKind = iota // a line of a commit's header
	flowFile                   // the bar starting a file
	flowNote                   // a note in place of a diff: binary file, load error
	flowDiff                   // a displayed row of a file's diff view
)

// flowRow is a row of the flow view. part is the index of its part, file
// the index of its file within the part (-1 for rows of the commit), and
// line the line of the commit header or the index into the file's
// displayed rows; sub is the line of a diff row in the normal layout.
type flowRow struct {
	kind                  flowKind
	part, file, line, sub int
}

// flowPart is one commit of the log, or all files outside the log.
type flowPart struct {
	commit *commitEntry // nil outside the log
	full   []string     // commit header lines with the whole message, the first one is the bar
	short  []string     // commit header lines with the subject only
	app    *App         // the files, nil until loaded
	err    string       // why the files could not be loaded
}

// far moves the cursor to the first or last row.
const far = 1 << 30

// Flow is the Bubble Tea model of the flow view: the diffs of all files one
// after the other, each starting with a bar, and in the log each commit
// starting with its message. Files share their diffs with the single file
// view, so anchors and folds carry over; cursor and scrolling are the
// flow's own. Commits are loaded once they come near the cursor.
type Flow struct {
	cfg   *config.Config
	parts []*flowPart
	load  func(part int) (*App, error) // loads the files of a commit, nil outside the log

	rows      []flowRow
	cursor    int
	top       int
	xoff      int
	active    side
	marked    *fileDiff // diff holding a pending mark
	replacing *fileDiff // diff waiting for confirmation to replace anchors

	width, height int
	msg           string
}

func newFlow(cfg *config.Config, parts []*flowPart, load func(int) (*App, error)) *Flow {
	f := &Flow{cfg: cfg, parts: parts, load: load}
	f.build()
	return f
}

// flowOfApp returns the flow view of the files of a, outside the log.
func flowOfApp(a *App) *Flow { return newFlow(a.cfg, []*flowPart{{app: a}}, nil) }

// newCommitPart returns the part of the flow view for commit e.
func newCommitPart(e *commitEntry) *flowPart {
	return &flowPart{commit: e, full: commitHeader(e.Commit, true), short: commitHeader(e.Commit, false)}
}

// commitHeader returns the header lines of c: the bar, then the message, or
// with full unset its subject only, indented as git log does.
func commitHeader(c gitsrc.Commit, full bool) []string {
	h := []string{fmt.Sprintf("commit %s · %s · %s", c.Short, c.Author, c.Date), "", "    " + c.Subject}
	if full && c.Body != "" {
		h = append(h, "")
		for l := range strings.SplitSeq(c.Body, "\n") {
			h = append(h, strings.TrimRight("    "+l, " "))
		}
	}
	return append(h, "")
}

func (f *Flow) Init() tea.Cmd { return nil }

// commitLines returns the header lines of part as currently shown.
func (f *Flow) commitLines(part *flowPart) []string {
	if f.cfg.Settings.FullMessages {
		return part.full
	}
	return part.short
}

func (f *Flow) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.keepScreenRow(func() { f.width, f.height = msg.Width, msg.Height })
	case tea.KeyMsg:
		cmd = pressKeys(msg, f.key)
	}
	f.loadVisible()
	return f, cmd
}

func (f *Flow) key(k string) tea.Cmd {
	if f.replacing != nil && k != "enter" && k != "esc" {
		f.replacing.replace = nil
		f.replacing = nil
	}
	f.msg = ""
	page := max(f.bodyHeight()-1, 1)
	if delta, paging, ok := motion(k, page, far); ok {
		if paging {
			f.scroll(delta)
		} else {
			f.moveCursor(delta)
		}
		return nil
	}
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "f":
		return toggleFlow
	case "backspace":
		return func() tea.Msg { return backMsg{} }
	case "tab":
		f.active = f.active.other()
	case "ctrl+u":
		f.scroll(-page / 2)
	case "ctrl+d":
		f.scroll(page / 2)
	case "left", "h":
		f.xoff = max(f.xoff-8, 0)
	case "right", "l":
		f.xoff += 8
	case "]", "[":
		if !f.search(direction(k == "]"), f.changeStart, true) && f.msg == "" {
			f.msg = "no more changes"
		}
	case "n", "p":
		if f.search(direction(k == "n"), f.isKind(flowFile, 0), true) {
			f.barToTop()
		} else if f.msg == "" {
			f.msg = ends(k == "n", "last file", "first file")
		}
	case "N", "P":
		switch {
		case !f.inLog():
		case f.search(direction(k == "N"), f.isKind(flowCommit, 0), false):
			f.barToTop()
		default:
			f.msg = ends(k == "N", "oldest commit", "newest commit")
		}
	case "enter", "esc", " ", "ctrl+@":
		f.delegate(k)
	case "o":
		f.jump()
	case "s":
		f.msg = toggle(f.cfg, &f.cfg.Settings.SideBySide)
		f.keepScreenRow(f.relocate)
	case "w":
		f.msg = toggle(f.cfg, &f.cfg.Settings.ShowWhitespaces)
	case "c":
		if f.inLog() {
			f.msg = toggle(f.cfg, &f.cfg.Settings.FullMessages)
			f.keepScreenRow(f.relocate)
		}
	case "C":
		if f.inLog() {
			f.msg = toggle(f.cfg, &f.cfg.Settings.StayInCommit)
			if f.msg == "" && f.cfg.Settings.StayInCommit {
				f.msg = "staying in commit " + f.parts[f.rows[f.cursor].part].commit.Short
			}
		}
	}
	return nil
}

// delegate runs an anchor or fold key on the diff of the file under the
// cursor.
func (f *Flow) delegate(k string) {
	if k == "esc" && f.replacing == nil && f.marked != nil && f.marked.pending != nil {
		// The mark may be in another file than the cursor.
		f.keepScreenRow(func() {
			f.dropMark()
			f.relocate()
		})
		f.msg = "mark cancelled"
		return
	}
	if len(f.rows) == 0 || f.rows[f.cursor].kind != flowDiff {
		if k != "esc" {
			f.msg = "no line here"
		}
		return
	}
	r := f.rows[f.cursor]
	e := f.parts[r.part].app.files[r.file]
	d := e.diff
	if k == "enter" && f.marked != d {
		f.dropMark()
	}
	var at spot
	f.keepScreenRow(func() {
		at, f.msg = d.act(k, spot{r.line, r.sub, f.active}, d.unified())
		e.updateStats()
		f.build()
		if i := f.findDiff(r.part, r.file, at.v, at.sub); i >= 0 {
			f.cursor = i
		}
	})
	f.active = at.side
	f.replacing = nil
	if d.replace != nil {
		f.replacing = d
	}
	if d.pending != nil {
		f.marked = d
	} else if f.marked == d {
		f.marked = nil
	}
}

// jump moves the cursor to the other end of the moved line under it, in this
// or another file of the commit; it keeps its screen row.
func (f *Flow) jump() {
	if len(f.rows) == 0 || f.rows[f.cursor].kind != flowDiff {
		f.msg = "not moved"
		return
	}
	r := f.rows[f.cursor]
	app := f.parts[r.part].app
	d := app.files[r.file].diff
	end, msg, ok := d.otherEnd(spot{r.line, r.sub, f.active}, d.unified())
	f.msg = msg
	if !ok {
		return
	}
	for i, e := range app.files {
		if e.diff == nil || e.diff.groupIndex != end.file {
			continue
		}
		at := e.diff.spotOf(end.side, end.line)
		f.keepScreenRow(func() {
			if j := f.findDiff(r.part, i, at.v, at.sub); j >= 0 {
				f.cursor = j
			}
		})
		f.active = at.side
		return
	}
}

// dropMark cancels a pending mark; the caller rebuilds the rows.
func (f *Flow) dropMark() {
	if d := f.marked; d != nil && d.pending != nil {
		d.pending = nil
		d.buildView()
	}
	f.marked = nil
}

// build computes the rows from the parts.
func (f *Flow) build() {
	f.rows = f.rows[:0]
	add := func(k flowKind, part, file, line int) {
		f.rows = append(f.rows, flowRow{k, part, file, line, 0})
	}
	for p, part := range f.parts {
		for i := range f.commitLines(part) {
			add(flowCommit, p, -1, i)
		}
		if part.err != "" || (part.app != nil && len(part.app.files) == 0) {
			add(flowNote, p, -1, 0)
		}
		if part.app == nil {
			continue
		}
		for i, e := range part.app.files {
			add(flowFile, p, i, 0)
			if e.Binary {
				add(flowNote, p, i, 0)
				continue
			}
			d := part.app.diffOf(e)
			for v := range d.view {
				for s := range d.rowLines(v, d.unified()) {
					f.rows = append(f.rows, flowRow{flowDiff, p, i, v, s})
				}
			}
		}
	}
	f.cursor = max(min(f.cursor, len(f.rows)-1), 0)
}

// refresh runs reload, which may reload files, and rebuilds the rows. The
// cursor stays on its screen row and, in a reloaded file, on its line.
func (f *Flow) refresh(reload func()) {
	var cur flowRow
	var d *fileDiff
	var row diff.Row
	reloads := 0
	if f.cursor < len(f.rows) && f.rows[f.cursor].kind == flowDiff {
		cur = f.rows[f.cursor]
		d = f.parts[cur.part].app.files[cur.file].diff
		row, reloads = d.rows[d.view[cur.line].row], d.reloads
	}
	f.keepScreenRow(func() {
		reload()
		f.relocate()
		if d == nil || d.reloads == reloads {
			return
		}
		if v := d.follow(row, true); v >= 0 {
			if i := f.findDiff(cur.part, cur.file, v, cur.sub); i >= 0 {
				f.cursor = i
			}
		}
	})
}

// relocate rebuilds the rows, keeping the cursor on its row, or on the
// first line of its diff row when the layout changed.
func (f *Flow) relocate() {
	var cur flowRow
	ok := f.cursor < len(f.rows)
	if ok {
		cur = f.rows[f.cursor]
	}
	f.build()
	i := f.find(cur)
	if i < 0 && cur.kind == flowCommit {
		// A message line no longer shown: on to the subject.
		i = f.find(flowRow{flowCommit, cur.part, -1, 2, 0})
	}
	if cur.kind == flowDiff {
		i = f.findDiff(cur.part, cur.file, cur.line, cur.sub)
	}
	if ok && i >= 0 {
		f.cursor = i
	}
}

// findDiff returns the index of line sub of diff row line, or of its first
// line when it has no line sub, or -1.
func (f *Flow) findDiff(part, file, line, sub int) int {
	if i := f.find(flowRow{flowDiff, part, file, line, sub}); i >= 0 {
		return i
	}
	return f.find(flowRow{flowDiff, part, file, line, 0})
}

func (f *Flow) find(r flowRow) int {
	for i, x := range f.rows {
		if x == r {
			return i
		}
	}
	return -1
}

// inLog reports whether f shows the commits of the log, rather than the
// files outside it.
func (f *Flow) inLog() bool { return f.load != nil }

func (f *Flow) unloaded(part int) bool {
	p := f.parts[part]
	return p.app == nil && p.err == ""
}

// unloadedIn returns a commit with rows in [a, b] that is not loaded yet, or
// -1.
func (f *Flow) unloadedIn(a, b int) int {
	if !f.inLog() {
		return -1
	}
	for i := max(a, 0); i <= min(b, len(f.rows)-1); i++ {
		if p := f.rows[i].part; f.unloaded(p) {
			return p
		}
	}
	return -1
}

// loadPart loads the files of commit p; the cursor stays on its row and its
// screen row.
func (f *Flow) loadPart(p int) {
	part := f.parts[p]
	app, err := f.load(p)
	if err != nil {
		part.err = err.Error()
	} else {
		part.app = app
	}
	f.keepScreenRow(f.relocate)
}

// loadVisible loads the commits shown on screen.
func (f *Flow) loadVisible() {
	for {
		p := f.unloadedIn(f.top, f.top+f.bodyHeight()-1)
		if p < 0 {
			return
		}
		f.loadPart(p)
	}
}

// moveCursor moves the cursor by delta rows. Commits on the way and a
// screen beyond are loaded first, so it lands where it would with all
// commits loaded.
func (f *Flow) moveCursor(delta int) {
	f.loadAhead(delta)
	f.cursor = f.confine(max(min(f.cursor+delta, len(f.rows)-1), 0))
	f.scrollToCursor()
}

// bounds returns the first and last row the cursor may go to: those of its
// commit while stay_in_commit is set, else all.
func (f *Flow) bounds() (lo, hi int) {
	lo, hi = 0, len(f.rows)-1
	if !f.inLog() || !f.cfg.Settings.StayInCommit || len(f.rows) == 0 {
		return lo, hi
	}
	p := f.rows[f.cursor].part
	for lo = f.cursor; lo > 0 && f.rows[lo-1].part == p; lo-- {
	}
	for hi = f.cursor; hi < len(f.rows)-1 && f.rows[hi+1].part == p; hi++ {
	}
	return lo, hi
}

// confine returns row i, or the nearest row within the bounds with a
// warning that the cursor stays in its commit.
func (f *Flow) confine(i int) int {
	lo, hi := f.bounds()
	if i >= lo && i <= hi {
		return i
	}
	f.msg = "stays in commit " + f.parts[f.rows[f.cursor].part].commit.Short + " (C)"
	return max(min(i, hi), lo)
}

// scroll moves the window by delta rows, as far as it can, and the cursor
// along, so that the cursor keeps its screen row. Commits are loaded first,
// as for moveCursor.
func (f *Flow) scroll(delta int) {
	f.loadAhead(delta)
	top := f.top
	f.top = max(min(f.top+delta, len(f.rows)-f.bodyHeight()), 0)
	f.cursor = f.confine(max(min(f.cursor+f.top-top, len(f.rows)-1), 0))
	f.scrollToCursor()
}

// loadAhead loads the commits on the way of a cursor move by delta rows and
// a screen beyond.
func (f *Flow) loadAhead(delta int) {
	h := f.bodyHeight()
	for {
		lo, hi := f.cursor, f.cursor+delta+h
		if delta < 0 {
			lo, hi = f.cursor+delta-h, f.cursor
		}
		p := f.unloadedIn(lo, hi)
		if p < 0 {
			break
		}
		f.loadPart(p)
	}
}

// search moves the cursor to the next row in direction dir for which ok
// holds, loading commits on the way, and reports whether there is one. With
// stay set it does not leave the bounds; reaching them sets a warning.
func (f *Flow) search(dir int, ok func(int) bool, stay bool) bool {
	for {
		loaded := false
		for i := f.cursor + dir; i >= 0 && i < len(f.rows); i += dir {
			if lo, hi := f.bounds(); stay && (i < lo || i > hi) {
				f.confine(i)
				return false
			}
			if p := f.rows[i].part; f.inLog() && f.unloaded(p) {
				f.loadPart(p)
				loaded = true
				break
			}
			if ok(i) {
				f.cursor = i
				f.scrollToCursor()
				return true
			}
		}
		if !loaded {
			return false
		}
	}
}

// barToTop scrolls the bar under the cursor to the top of the screen, so
// that what it starts is visible.
func (f *Flow) barToTop() {
	f.top = f.cursor
	f.scrollToCursor()
}

func (f *Flow) isKind(k flowKind, line int) func(int) bool {
	return func(i int) bool { return f.rows[i].kind == k && f.rows[i].line == line }
}

func (f *Flow) changeStart(i int) bool {
	r := f.rows[i]
	return r.kind == flowDiff && r.sub == 0 && f.parts[r.part].app.files[r.file].diff.changeStart(r.line)
}

// show puts the cursor on line sub of diff row line of file in part, on
// the file bar for line -1 and on the commit bar for file -1, at screen row
// screenRow.
func (f *Flow) show(part, file, line, sub, screenRow int) {
	if f.inLog() && f.unloaded(part) {
		f.loadPart(part)
	}
	f.marked, f.replacing = nil, nil
	i := f.find(flowRow{flowCommit, part, -1, 0, 0})
	switch {
	case f.parts[part].app == nil:
	case file >= 0 && line >= 0:
		i = f.findDiff(part, file, line, sub)
	case file >= 0:
		i = f.find(flowRow{flowFile, part, file, 0, 0})
	}
	f.cursor = max(i, 0)
	f.top = f.cursor - screenRow
	f.scrollToCursor()
	f.loadVisible()
}

// position returns the part of the cursor, its file (-1 on rows of the
// commit) and the line in the file's diff view (-1 on other rows).
func (f *Flow) position() (part, file, line int) {
	if len(f.rows) == 0 {
		return 0, -1, -1
	}
	r := f.rows[f.cursor]
	line = -1
	if r.kind == flowDiff {
		line = r.line
	}
	return r.part, r.file, line
}

// headerLines is the number of lines above the rows: the commit in the log,
// and the file names.
func (f *Flow) headerLines() int {
	if f.inLog() {
		return 2
	}
	return 1
}

// cursorSub returns the line of the cursor within its diff row.
func (f *Flow) cursorSub() int {
	if len(f.rows) == 0 {
		return 0
	}
	return f.rows[f.cursor].sub
}

func (f *Flow) bodyHeight() int { return max(f.height-1-f.headerLines(), 1) }

func (f *Flow) keepScreenRow(fn func()) {
	offset := f.cursor - f.top
	fn()
	f.top = f.cursor - offset
	f.scrollToCursor()
}

func (f *Flow) scrollToCursor() {
	f.top = scrollWindow(f.cursor, f.top, f.bodyHeight(), len(f.rows))
}

func (f *Flow) help() string {
	h := "f single file · space fold · ]/[ change · n/p file · "
	if f.inLog() {
		h += "N/P commit · "
	}
	return h + "? help · q quit"
}

func (f *Flow) View() string {
	if f.width == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(f.header())
	for i := f.top; i < f.top+f.bodyHeight(); i++ {
		if i < len(f.rows) {
			b.WriteString(f.row(i))
		}
		b.WriteString("\n")
	}
	b.WriteString(f.status())
	return b.String()
}

// header renders the lines above the rows: in the log the commit of the
// cursor, then the names of its file as in the single file view.
func (f *Flow) header() string {
	t := f.cfg.Theme
	p, file, _ := f.position()
	var b strings.Builder
	if f.inLog() {
		c := f.parts[p].commit
		line := " " + t.CommitHash.Render(c.Short) + " " + t.Title.Render(c.Subject)
		b.WriteString(ansi.Truncate(line, f.width, "…") + "\n")
	}
	if file >= 0 {
		b.WriteString(paneTitles(t, f.parts[p].app.files[file].paneNames(), f.active, f.cfg.Settings.SideBySide, f.width))
	}
	return b.String() + "\n"
}

func (f *Flow) status() string {
	p, file, _ := f.position()
	var where []string
	if f.inLog() {
		c := fmt.Sprintf("commit %d/%d", p+1, len(f.parts))
		if f.cfg.Settings.StayInCommit {
			c += " (C)"
		}
		where = append(where, c)
	}
	if file >= 0 {
		files := f.parts[p].app.files
		where = append(where, fmt.Sprintf("file %d/%d", file+1, len(files)))
		if d := files[file].diff; d != nil {
			where = append(where, fmt.Sprintf("anchors %d", len(d.anchors)))
		}
	}
	left := f.msg
	if left == "" {
		left = f.help()
	}
	return statusLine(f.cfg.Theme, f.width, left, " "+strings.Join(where, " · "))
}

// row renders row i. Diff rows are drawn by the file's diff with the flow's
// pane, scroll offset and width; other rows show the cursor in the first
// column and in their text, in the cursor style.
func (f *Flow) row(i int) string {
	t := f.cfg.Theme
	r := f.rows[i]
	part := f.parts[r.part]
	cursor := i == f.cursor
	mark := " "
	if cursor {
		mark = t.Cursor.Render(" ")
	}
	w := max(f.width-1, 0)
	// bar renders s across the row in st.
	bar := func(s string, st lipgloss.Style) string {
		s = ansi.Truncate(s, w, "…")
		if cursor {
			st = t.Cursor.Inherit(st)
		}
		return st.Render(s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0)))
	}
	// text renders s in st, on the cursor row past its indentation.
	text := func(s string, st lipgloss.Style) string {
		s = ansi.Truncate(s, w, "…")
		if !cursor {
			return st.Render(s)
		}
		body := strings.TrimLeft(s, " ")
		return st.Render(s[:len(s)-len(body)]) + t.Cursor.Inherit(st).Render(body)
	}

	switch r.kind {
	case flowCommit:
		line := f.commitLines(part)[r.line]
		switch r.line {
		case 0:
			return mark + bar(" "+line, t.CommitBar)
		case 2:
			return mark + text(line, t.Zero.Bold(true))
		}
		return mark + text(line, t.Zero)
	case flowFile:
		e := part.app.files[r.file]
		line := " "
		if lbl := e.label(t); lbl != "" {
			line += lbl + " "
		}
		line += e.name()
		if e.Binary {
			line += "   binary"
		} else if e.added > 0 || e.removed > 0 {
			line += fmt.Sprintf("   +%d -%d", e.added, e.removed)
		}
		return mark + bar(line, t.FileBar)
	case flowNote:
		note := "no changed files"
		switch {
		case r.file >= 0:
			note = "binary file"
		case part.err != "":
			note = part.err
		}
		return mark + text("  "+note, t.Hint)
	}
	d := part.app.files[r.file].diff
	return d.render(sline{r.line, r.sub}, i == f.cursor, layout{f.width, f.xoff, f.active, d.unified()})
}
