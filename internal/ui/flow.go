package ui

import (
	"fmt"
	"slices"
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
	order historyOrder                 // of the commits, the parts in the log

	rows      []flowRow
	built     shape // what the rows of the diffs were built for
	cursor    int
	top       int
	xoff      int
	active    side
	marked    *fileDiff // diff holding a pending mark
	replacing *fileDiff // diff waiting for confirmation to replace anchors
	prefix    string    // "m" or "'" while waiting for the letter of a jump mark

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
		f.keepScreenRow(func() {
			f.width, f.height = msg.Width, msg.Height
			f.relocate() // wrapped lines take other pieces
		})
	case tea.KeyMsg:
		cmd = pressKeys(msg, f.key)
	case findMsg:
		f.findText(int(msg))
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
	if p := f.prefix; p != "" {
		f.prefix = ""
		switch {
		case !isMarkLetter(k):
		case p == "m":
			f.setMark(k)
		default:
			f.gotoMark(k)
		}
		return nil
	}
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
		case f.search(f.order.step(historyDir(k)), f.isKind(flowCommit, 0), false):
			f.barToTop()
		default:
			f.msg = ends(historyDir(k) > 0, "last commit", "first commit")
		}
	case "enter", "esc", " ", "ctrl+@":
		f.delegate(k)
	case "R", "f5":
		return refreshAll
	case diffKey:
		return askDiff
	case searchKey:
		return askSearch
	case nextKey:
		f.findText(1)
	case prevKey:
		f.findText(-1)
	case "o":
		f.jump()
	case "m":
		f.prefix, f.msg = k, "set mark: press a letter"
	case "'":
		f.prefix, f.msg = k, "jump to mark: press its letter"
	case "s":
		f.msg = toggle(f.cfg, &f.cfg.Settings.SideBySide)
		f.keepScreenRow(f.relocate)
	case "w":
		f.msg = toggle(f.cfg, &f.cfg.Settings.ShowWhitespaces)
		f.keepScreenRow(f.relocate) // wrapped lines end in line_end, which takes a column
	case "W":
		f.msg = toggle(f.cfg, &f.cfg.Settings.MarkWhitespace)
	case "t":
		f.msg = toggle(f.cfg, &f.cfg.Settings.ShowTrailing)
	case "e":
		f.msg = toggle(f.cfg, &f.cfg.Settings.ShowCR)
		f.keepScreenRow(f.relocate) // wrapped lines end in ␍, which takes a column
	case "b":
		f.msg = toggle(f.cfg, &f.cfg.Settings.BoldChanges)
	case "r":
		f.msg = toggle(f.cfg, &f.cfg.Settings.Wrap)
		f.keepScreenRow(f.relocate)
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
		at, f.msg = d.act(k, spot{r.line, r.sub, f.active}, f.built)
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
// or another file of the commit (see jumpTo).
func (f *Flow) jump() {
	if len(f.rows) == 0 || f.rows[f.cursor].kind != flowDiff {
		f.msg = "not moved"
		return
	}
	r := f.rows[f.cursor]
	app := f.parts[r.part].app
	d := app.files[r.file].diff
	end, msg, ok := d.otherEnd(spot{r.line, r.sub, f.active}, f.built)
	f.msg = msg
	if !ok {
		return
	}
	i := app.fileByGroup(end.file)
	if i < 0 {
		return
	}
	at := app.files[i].diff.spotOf(end.side, end.line, f.built)
	f.jumpTo(f.findDiff(r.part, i, at.v, at.sub))
	f.active = at.side
}

// jumpTo moves the cursor to row i, unless it is -1, for a jump within the
// flow (o, '): the screen stays when i is on it, see jumpTop.
func (f *Flow) jumpTo(i int) {
	if i < 0 {
		return
	}
	from := f.cursor
	f.cursor = i
	f.top = jumpTop(from, i, f.top, f.bodyHeight(), len(f.rows))
}

// setMark sets jump mark letter on the new-file line under the cursor.
func (f *Flow) setMark(letter string) {
	if len(f.rows) == 0 || f.rows[f.cursor].kind != flowDiff {
		f.msg = "no line of the new file here"
		return
	}
	r := f.rows[f.cursor]
	f.msg = setMark(f.cfg, f.parts[r.part].app.files[r.file].diff, letter, r.line)
}

// gotoMark moves the cursor to the line of jump mark letter when its file is
// in the commit under the cursor, or outside the log among the files (see
// jumpTo).
func (f *Flow) gotoMark(letter string) {
	mk, ok := f.cfg.Mark(letter)
	if !ok {
		f.msg = "no mark " + letter
		return
	}
	if len(f.rows) == 0 || f.parts[f.rows[f.cursor].part].app == nil {
		f.msg = notInDiff(letter, mk)
		return
	}
	r := f.rows[f.cursor]
	app := f.parts[r.part].app
	i := app.fileByPath(mk.Path)
	if i < 0 {
		f.msg = notInDiff(letter, mk)
		return
	}
	d := app.files[i].diff
	line, note, ok := d.markLine(mk)
	if !ok {
		f.msg = markMsg(letter, "the new file is empty")
		return
	}
	at := d.reveal(right, line, f.built)
	f.relocate() // a fold may have opened: the cursor keeps its row
	f.jumpTo(f.findDiff(r.part, i, at.v, at.sub))
	f.active, f.msg = right, markMsg(letter, note)
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
	f.built = newShape(f.cfg, f.width)
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
			screen, _ := part.app.diffOf(e).screenLines(f.built)
			for _, s := range screen {
				f.rows = append(f.rows, flowRow{flowDiff, p, i, s.v, s.sub})
			}
		}
	}
	f.cursor = max(min(f.cursor, len(f.rows)-1), 0)
}

// refresh runs reload, which may reload files, calling its argument after
// each reload of a diff, and rebuilds the rows. The cursor stays on its
// screen row and, in a reloaded file, on its line.
func (f *Flow) refresh(reload func(after func(*fileDiff))) {
	var cur flowRow
	var d *fileDiff
	var row diff.Row
	reloads, lost := 0, false
	if f.cursor < len(f.rows) && f.rows[f.cursor].kind == flowDiff {
		cur = f.rows[f.cursor]
		d = f.parts[cur.part].app.files[cur.file].diff
		row, reloads = d.rows[d.view[cur.line].row], d.reloads
	}
	f.keepScreenRow(func() {
		// The cursor's row is followed through each reload: a diff may
		// reload both sides, and each reload maps only its own side.
		reload(func(rd *fileDiff) {
			if rd != d || lost {
				return
			}
			if i := d.rowAfterReload(row); i >= 0 {
				row = d.rows[i]
			} else {
				lost = true
			}
		})
		if d != nil {
			// The files may have been listed again, in another order.
			cur.file = slices.IndexFunc(f.parts[cur.part].app.files, func(e *entry) bool { return e.diff == d })
		}
		f.relocate()
		if d == nil || cur.file < 0 || d.reloads == reloads || lost {
			return
		}
		if v := d.follow(row, false); v >= 0 {
			if i := f.findDiff(cur.part, cur.file, v, cur.sub); i >= 0 {
				f.cursor = i
			}
		}
	})
}

// followFiles keeps the cursor on its file of part when the files were
// listed again, from old to new: on the file's row, or for a file that is
// gone on the bar of the next file left, else of the last. The rows are
// rebuilt next.
func (f *Flow) followFiles(part int, old, new []*entry) {
	if f.cursor >= len(f.rows) {
		return
	}
	r := &f.rows[f.cursor]
	if r.part != part || r.file < 0 {
		return
	}
	for i := r.file; i < len(old); i++ {
		if j := slices.Index(new, old[i]); j >= 0 {
			if i == r.file {
				r.file = j
			} else {
				*r = flowRow{flowFile, part, j, 0, 0}
			}
			return
		}
	}
	*r = flowRow{flowFile, part, len(new) - 1, 0, 0}
}

// followParts keeps the cursor on its commit when the commits were listed
// again, from the parts old to f.parts, which still hold it. The rows are
// rebuilt next.
func (f *Flow) followParts(old []*flowPart) {
	if f.cursor < len(f.rows) {
		r := &f.rows[f.cursor]
		r.part = slices.Index(f.parts, old[r.part])
	}
}

// relocate rebuilds the rows, keeping the cursor on its row: with wrapped
// lines on the same piece of the same line, else on the first line of its
// diff row when the layout changed.
func (f *Flow) relocate() {
	var cur flowRow
	ok := f.cursor < len(f.rows)
	if ok {
		cur = f.rows[f.cursor]
	}
	from := f.built
	f.build()
	i := f.find(cur)
	if i < 0 && cur.kind == flowCommit {
		// A message line no longer shown: on to the subject.
		i = f.find(flowRow{flowCommit, cur.part, -1, 2, 0})
	}
	if cur.kind == flowDiff {
		sub := cur.sub
		if d := f.parts[cur.part].app.files[cur.file].diff; cur.line < len(d.view) {
			sub = d.convertSub(cur.line, cur.sub, from, f.built, f.active)
		}
		i = f.findDiff(cur.part, cur.file, cur.line, sub)
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

// changeStart reports whether row i starts a block of changed rows.
func (f *Flow) changeStart(i int) bool {
	changed := func(i int) bool {
		r := f.rows[i]
		return r.kind == flowDiff && f.parts[r.part].app.files[r.file].diff.changed(r.line)
	}
	return changed(i) && (i == 0 || !changed(i-1))
}

// show puts the cursor on line sub of diff row line of file in part, on
// the file bar for line -1 and on the commit bar for file -1, at screen row
// screenRow.
func (f *Flow) show(part, file, line, sub, screenRow int) {
	if f.inLog() && f.unloaded(part) {
		f.loadPart(part)
	}
	// While hidden, diff views may have changed the diffs or the layout.
	f.build()
	f.adopt(part, file)
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

// showFrom shows file of part where its diff view from leaves it: on the
// cursor's line at its screen row, in its pane. For from nil it shows the
// file's bar, or the commit's bar for file -1. Model.takeOver is the way
// back.
func (f *Flow) showFrom(part, file int, from *Model) {
	if from == nil {
		f.show(part, file, -1, 0, 0)
		return
	}
	from.syncLayout()
	f.show(part, file, from.cursor, from.sub, from.pos()-from.top)
	f.active = from.active
}

// adopt takes over the pending mark and replace prompt that diff views of
// the files of part left, so that esc and enter treat them as the flow's
// own. Of several pending marks the one of file is kept, else the first;
// the others are dropped, as the flow keeps one.
func (f *Flow) adopt(part, file int) {
	f.marked, f.replacing = nil, nil
	app := f.parts[part].app
	if app == nil {
		return
	}
	var marked []*fileDiff
	for i, e := range app.files {
		d := e.diff
		if d == nil {
			continue
		}
		if d.pending != nil {
			if i == file {
				marked = append([]*fileDiff{d}, marked...)
			} else {
				marked = append(marked, d)
			}
		}
		if d.replace != nil {
			f.replacing = d
		}
	}
	if len(marked) == 0 {
		return
	}
	for _, d := range marked[1:] {
		f.marked = d
		f.dropMark()
	}
	f.marked = marked[0]
	if len(marked) > 1 {
		f.build() // the dropped marks may have folded rows
	}
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

// help returns the keys of the status bar, in the order of all pages.
func (f *Flow) help() string {
	keys := []string{"enter mark/anchor", "space fold", "]/[ change", "/ search", "n/p file"}
	if f.inLog() {
		keys = append(keys, "N/P commit")
	}
	keys = append(keys, "f single file", "d git diff", "backspace files", "? help", "q quit")
	return strings.Join(keys, " · ")
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
	return d.render(sline{r.line, r.sub}, i == f.cursor, layout{f.width, f.xoff, f.active, f.built})
}
