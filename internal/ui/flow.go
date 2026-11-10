package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/gitsrc"
	"jdiff/internal/keys"
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
	diff   *Diff        // the files, nil until loaded
	err    string       // why the files could not be loaded
}

// far moves the cursor to the first or last row.
const far = 1 << 30

// Flow is the Bubble Tea model of the flow view (keys of flow_view): the
// diffs of all files one after the other, each starting with a bar, and in
// the log each commit starting with its message. Files share their diffs with the single file
// view, so anchors and folds carry over; cursor and scrolling are the
// flow's own. In the log, commits load in the background once they are on
// the screen, one at a time; until then a note says that they load.
type Flow struct {
	cfg   *config.Config
	parts []*flowPart
	// load returns the command loading the files of the commit of part,
	// whose result the log gives back to partLoaded; nil outside the log.
	load  func(part int) tea.Cmd
	order historyOrder // of the commits, the parts in the log

	loading *flowPart // the commit loading, nil for none
	ahead   int       // the way of the last move, whose commits a screen beyond load too
	atEnd   bool      // end was pressed last: the cursor stays on the last row
	waiting *flowWait // a search waiting for a commit to load

	rows      []flowRow
	rowAt     map[flowRow]int // index of each row, see find
	stale     bool            // a row changed since rowAt was built
	built     shape           // what the rows of the diffs were built for
	cursor    int
	top       int
	xoff      int
	active    side
	marked    *fileAlign // diff holding a pending mark
	replacing *fileAlign // diff waiting for confirmation to replace anchors
	prefix    string     // the action, "set_mark" or "jump", while waiting for the letter of a mark

	width, height int
	msg           string
}

// flowWait is a search that reached a commit not loaded yet: redo runs it
// again once part is loaded.
type flowWait struct {
	part *flowPart
	redo func()
}

func newFlow(cfg *config.Config, parts []*flowPart, load func(int) tea.Cmd) *Flow {
	f := &Flow{cfg: cfg, parts: parts, load: load}
	f.build()
	return f
}

// flowOfDiff returns the flow view of the files of d, outside the log.
func flowOfDiff(d *Diff) *Flow { return newFlow(d.cfg, []*flowPart{{diff: d}}, nil) }

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
	case tea.KeyPressMsg:
		cmd = f.key(msg.String())
	case findMsg:
		f.findText(int(msg))
	case settingChangedMsg:
		// Wrapped lines take other pieces, and end in line_end or ␍, which take
		// a column.
		f.keepScreenRow(f.relocate)
		if msg.action == "stay_in_commit" && f.inLog() && f.cfg.Settings.StayInCommit {
			f.msg = "staying in commit " + f.parts[f.rows[f.cursor].part].commit.Short
		}
		if msg.problem != "" {
			f.msg = msg.problem
		}
	}
	// A key's own command comes first, alone, as the tests run commands one
	// at a time; the next message starts the load.
	if cmd == nil {
		cmd = f.nextLoad()
	}
	return f, cmd
}

func (f *Flow) key(k string) tea.Cmd {
	h := f.cfg.Keys.Key(keys.FlowView, k)
	if f.replacing != nil && h.Action != "anchor" && h.Action != "cancel_mark" {
		f.replacing.replace = nil
		f.replacing = nil
	}
	f.msg = ""
	f.atEnd, f.waiting = false, nil // another key: end and searches let go
	if p := f.prefix; p != "" {
		f.prefix = ""
		switch {
		case !isMarkLetter(k):
		case p == "set_mark":
			f.setMark(k)
		default:
			f.gotoMark(k)
		}
		return nil
	}
	if cmd, ok := diffKey(f, h, max(f.bodyHeight()-1, 1), far); ok {
		return cmd
	}
	switch h.Action {
	case "flow":
		return toggleFlow
	case "to_files":
		return func() tea.Msg { return backMsg{} }
	case "next_file":
		f.nextFile(direction(h.Index == 0))
	case "history":
		if !f.inLog() {
			break
		}
		// The bars of all commits are there, loaded or not: no waiting.
		if r, _ := f.search(f.order.step(historyDir(h)), f.isKind(flowCommit, 0), false, false); r == found {
			f.barToTop()
		} else {
			f.msg = ends(historyDir(h) > 0, "last commit", "first commit")
		}
	case "anchor", "cancel_mark", "fold", "fold_all":
		f.delegate(h.Action)
	case "moved":
		f.jump()
	default:
		return pageKey(f.cfg.Keys, h, f.inLog())
	}
	return nil
}

// scrollSideways, switchPane and askLetter act for diffKey.
func (f *Flow) scrollSideways(cols int)      { f.xoff = max(f.xoff+cols, 0) }
func (f *Flow) switchPane()                  { f.active = f.active.other() }
func (f *Flow) askLetter(action, msg string) { f.prefix, f.msg = action, msg }

// delegate runs an anchor or fold action on the diff of the file under the
// cursor.
func (f *Flow) delegate(action string) {
	if action == "cancel_mark" && f.replacing == nil && f.marked != nil && f.marked.pending != nil {
		// The mark may be in another file than the cursor.
		f.keepScreenRow(func() {
			f.dropMark()
			f.relocate()
		})
		f.msg = "mark cancelled"
		return
	}
	if len(f.rows) == 0 || f.rows[f.cursor].kind != flowDiff {
		if action != "cancel_mark" {
			f.msg = "no line here"
		}
		return
	}
	r := f.rows[f.cursor]
	e := f.parts[r.part].diff.files[r.file]
	a := e.align
	if action == "anchor" && f.marked != a {
		f.dropMark()
	}
	var at spot
	f.keepScreenRow(func() {
		at, f.msg = a.act(action, spot{r.line, r.sub, f.active}, f.built)
		e.updateStats()
		f.build()
		if i := f.findDiff(r.part, r.file, at.v, at.sub); i >= 0 {
			f.cursor = i
		}
	})
	f.active = at.side
	f.replacing = nil
	if a.replace != nil {
		f.replacing = a
	}
	if a.pending != nil {
		f.marked = a
	} else if f.marked == a {
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
	d := f.parts[r.part].diff
	a := d.files[r.file].align
	end, msg, ok := a.otherEnd(spot{r.line, r.sub, f.active}, f.built)
	f.msg = msg
	if !ok {
		return
	}
	i := d.fileByGroup(end.file)
	if i < 0 {
		return
	}
	at := d.files[i].align.spotOf(end.side, end.line, f.built)
	f.jumpTo(f.findDiff(r.part, i, at.v, at.sub))
	f.active = at.side
}

// jumpTo moves the cursor to row i, unless it is -1, for a jump within the
// flow (g, '): the screen stays when i is on it, see jumpTop.
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
	f.msg = setMark(f.cfg, f.parts[r.part].diff.files[r.file].align, letter, r.line)
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
	if len(f.rows) == 0 || f.parts[f.rows[f.cursor].part].diff == nil {
		f.msg = notInDiff(letter, mk)
		return
	}
	r := f.rows[f.cursor]
	d := f.parts[r.part].diff
	i := d.fileByPath(mk.Path)
	if i < 0 {
		f.msg = notInDiff(letter, mk)
		return
	}
	a := d.files[i].align
	line, note, ok := a.markLine(mk)
	if !ok {
		f.msg = markMsg(letter, "the new file is empty")
		return
	}
	at := a.reveal(right, line, f.built)
	f.relocate() // a fold may have opened: the cursor keeps its row
	f.jumpTo(f.findDiff(r.part, i, at.v, at.sub))
	f.active, f.msg = right, markMsg(letter, note)
}

// dropMark cancels a pending mark; the caller rebuilds the rows.
func (f *Flow) dropMark() {
	if a := f.marked; a != nil && a.pending != nil {
		a.pending = nil
		a.buildView()
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
		if part.diff == nil && part.commit != nil {
			part.diff = part.commit.diff // loaded for the list meanwhile
		}
		for i := range f.commitLines(part) {
			add(flowCommit, p, -1, i)
		}
		if part.diff == nil || len(part.diff.files) == 0 {
			add(flowNote, p, -1, 0) // loading, why it cannot, or no files
		}
		if part.diff == nil {
			continue
		}
		for i, e := range part.diff.files {
			add(flowFile, p, i, 0)
			if e.Binary {
				add(flowNote, p, i, 0)
				continue
			}
			screen, _ := part.diff.diffOf(e).screenLines(f.built)
			for _, s := range screen {
				f.rows = append(f.rows, flowRow{flowDiff, p, i, s.v, s.sub})
			}
		}
	}
	f.rowAt, f.stale = make(map[flowRow]int, len(f.rows)), false
	for i, r := range slices.Backward(f.rows) {
		f.rowAt[r] = i // the first of equal rows, as a search finds
	}
	f.cursor = max(min(f.cursor, len(f.rows)-1), 0)
}

// refresh runs reload, which may reload files, calling its argument after
// each reload of a diff, and rebuilds the rows. The cursor stays on its
// screen row and, in a reloaded file, on its line.
func (f *Flow) refresh(reload func(after func(*fileAlign))) {
	var cur flowRow
	var a *fileAlign
	var row diff.Row
	reloads, lost := 0, false
	if f.cursor < len(f.rows) && f.rows[f.cursor].kind == flowDiff {
		cur = f.rows[f.cursor]
		a = f.parts[cur.part].diff.files[cur.file].align
		row, reloads = a.rows[a.view[cur.line].row], a.reloads
	}
	f.keepScreenRow(func() {
		// The cursor's row is followed through each reload: a diff may
		// reload both sides, and each reload maps only its own side.
		reload(func(rd *fileAlign) {
			if rd != a || lost {
				return
			}
			if i := a.rowAfterReload(row); i >= 0 {
				row = a.rows[i]
			} else {
				lost = true
			}
		})
		if a != nil {
			// The files may have been listed again, in another order.
			cur.file = slices.IndexFunc(f.parts[cur.part].diff.files, func(e *fileEntry) bool { return e.align == a })
		}
		f.relocate()
		if a == nil || cur.file < 0 || a.reloads == reloads || lost {
			return
		}
		if v := a.follow(row, false); v >= 0 {
			if i := f.findDiff(cur.part, cur.file, v, cur.sub); i >= 0 {
				f.cursor = i
			}
		}
	})
}

// followFiles keeps the cursor on its file of part when the files were
// listed again, from before to after: on the file's row, or for a file that is
// gone on the bar of the next file left, else of the last. The rows are
// rebuilt next.
func (f *Flow) followFiles(part int, before, after []*fileEntry) {
	if f.cursor >= len(f.rows) {
		return
	}
	r := &f.rows[f.cursor]
	if r.part != part || r.file < 0 {
		return
	}
	f.stale = true
	for i := r.file; i < len(before); i++ {
		if j := slices.Index(after, before[i]); j >= 0 {
			if i == r.file {
				r.file = j
			} else {
				*r = flowRow{flowFile, part, j, 0, 0}
			}
			return
		}
	}
	*r = flowRow{flowFile, part, len(after) - 1, 0, 0}
}

// followParts keeps the cursor on its commit when the commits were listed
// again, from the parts old to f.parts, which still hold it. The rows are
// rebuilt next.
func (f *Flow) followParts(old []*flowPart) {
	if f.cursor < len(f.rows) {
		r := &f.rows[f.cursor]
		r.part = slices.Index(f.parts, old[r.part])
		f.stale = true
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
		if a := f.parts[cur.part].diff.files[cur.file].align; cur.line < len(a.view) {
			sub = a.convertSub(cur.line, cur.sub, from, f.built, f.active)
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

// find returns the index of row r, or -1. The rows are indexed by build: a
// long log has many, and each move looks for rows. After a row changed in
// place, as followFiles and followParts do before the rows are rebuilt, they
// are searched.
func (f *Flow) find(r flowRow) int {
	if !f.stale {
		if i, ok := f.rowAt[r]; ok {
			return i
		}
		return -1
	}
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

// unloaded reports whether the files of part are not loaded yet, neither
// for the flow nor for the list.
func (f *Flow) unloaded(part int) bool {
	p := f.parts[part]
	return p.diff == nil && p.err == "" && (p.commit == nil || p.commit.diff == nil)
}

// nextLoad returns the command loading the next commit, nil while one loads
// or none is to: the one a search waits for, else the first on the screen
// not loaded yet, else the first within a screen from the cursor on the way
// of the last move, so that it is there when the cursor comes.
func (f *Flow) nextLoad() tea.Cmd {
	if !f.inLog() || f.loading != nil {
		return nil
	}
	p := -1
	if w := f.waiting; w != nil {
		p = slices.Index(f.parts, w.part)
	}
	h := f.bodyHeight()
	first := func(from, to int) {
		for i := max(from, 0); p < 0 && i < min(to, len(f.rows)); i++ {
			if q := f.rows[i].part; f.unloaded(q) {
				p = q
			}
		}
	}
	first(f.top, f.top+h)
	switch {
	case f.ahead > 0:
		first(f.cursor, f.cursor+h+1)
	case f.ahead < 0:
		first(f.cursor-h, f.cursor+1)
	}
	if p < 0 || !f.unloaded(p) {
		return nil
	}
	f.loading = f.parts[p]
	return f.load(p)
}

// partLoaded takes the files of the commit of part, which the log loaded,
// or why they could not be. The cursor stays on its row and screen row, or
// after end on the last row; a search waiting for the commit goes on. It
// returns the command loading the next commit.
func (f *Flow) partLoaded(part *flowPart, err error) tea.Cmd {
	if f.loading == part {
		f.loading = nil
	}
	if err != nil {
		part.err = firstLine(err)
	}
	f.keepScreenRow(f.relocate) // build takes the files from the commit
	if f.atEnd {
		_, f.cursor = f.bounds()
		f.scrollToCursor()
	}
	if w := f.waiting; w != nil && !f.unloaded(slices.Index(f.parts, w.part)) {
		f.waiting = nil
		w.redo()
	}
	return f.nextLoad()
}

// await makes the search redo wait for commit p to load, saying so.
func (f *Flow) await(p int, redo func()) {
	f.waiting = &flowWait{f.parts[p], redo}
	f.msg = "loading " + f.parts[p].commit.Short + "…"
}

// moveCursor moves the cursor by delta rows, over the rows there are: the
// commits on the way load as they come on the screen. For far, end, the
// cursor stays on the last row as they load, until another key.
func (f *Flow) moveCursor(delta int) {
	f.cursor = f.confine(max(min(f.cursor+delta, len(f.rows)-1), 0))
	f.atEnd, f.ahead = delta >= far, direction(delta > 0)
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
// along, so that the cursor keeps its screen row.
func (f *Flow) scroll(delta int) {
	f.ahead = direction(delta > 0)
	top := f.top
	f.top = max(min(f.top+delta, len(f.rows)-f.bodyHeight()), 0)
	f.cursor = f.confine(max(min(f.cursor+f.top-top, len(f.rows)-1), 0))
	f.scrollToCursor()
}

// searchResult is how a search ended.
type searchResult int

const (
	notFound searchResult = iota
	found
	unloadedOnTheWay // a commit on the way is not loaded yet, see search
)

// search moves the cursor to the next row in direction dir for which ok
// holds. With stay set it does not leave the bounds; reaching them sets a
// warning. With wait, a commit on the way that is not loaded yet ends it,
// at the first of its rows that are there that ok does not take: its part
// is returned, for the caller to wait for it (see await), else -1.
func (f *Flow) search(dir int, ok func(int) bool, stay, wait bool) (searchResult, int) {
	for i := f.cursor + dir; i >= 0 && i < len(f.rows); i += dir {
		if lo, hi := f.bounds(); stay && (i < lo || i > hi) {
			f.confine(i)
			return notFound, -1
		}
		if ok(i) {
			f.cursor = i
			f.scrollToCursor()
			return found, -1
		}
		if p := f.rows[i].part; wait && f.inLog() && f.unloaded(p) {
			return unloadedOnTheWay, p
		}
	}
	return notFound, -1
}

// nextChange moves the cursor to the start of the next (dir > 0) or previous
// block of changed rows, waiting for the commits on the way to load.
func (f *Flow) nextChange(dir int) {
	switch r, p := f.search(dir, f.changeStart, true, true); {
	case r == unloadedOnTheWay:
		f.await(p, func() { f.nextChange(dir) })
	case r == notFound && f.msg == "":
		f.msg = "no more changes"
	}
}

// nextFile moves the cursor to the bar of the next (dir > 0) or previous
// file, at the top of the screen, waiting for the commits on the way to
// load.
func (f *Flow) nextFile(dir int) {
	switch r, p := f.search(dir, f.isKind(flowFile, 0), true, true); {
	case r == found:
		f.barToTop()
	case r == unloadedOnTheWay:
		f.await(p, func() { f.nextFile(dir) })
	case f.msg == "":
		f.msg = ends(dir > 0, "last file", "first file")
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
		return r.kind == flowDiff && f.parts[r.part].diff.files[r.file].align.changed(r.line)
	}
	return changed(i) && (i == 0 || !changed(i-1))
}

// show puts the cursor on line sub of diff row line of file in part, on
// the file bar for line -1 and on the commit bar for file -1, at screen row
// screenRow.
func (f *Flow) show(part, file, line, sub, screenRow int) {
	f.ahead, f.atEnd, f.waiting = 0, false, nil
	// While hidden, diff views may have changed the diffs or the layout.
	f.build()
	f.adopt(part, file)
	i := f.find(flowRow{flowCommit, part, -1, 0, 0})
	switch {
	case f.parts[part].diff == nil:
	case file >= 0 && line >= 0:
		i = f.findDiff(part, file, line, sub)
	case file >= 0:
		i = f.find(flowRow{flowFile, part, file, 0, 0})
	}
	f.cursor = max(i, 0)
	f.top = f.cursor - screenRow
	f.scrollToCursor()
}

// showFrom shows file of part where its diff view from leaves it: on the
// cursor's line at its screen row, in its pane. For from nil it shows the
// file's bar, or the commit's bar for file -1. FileDiff.takeOver is the way
// back.
func (f *Flow) showFrom(part, file int, from *FileDiff) {
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
	d := f.parts[part].diff
	if d == nil {
		return
	}
	var marked []*fileAlign
	for i, e := range d.files {
		a := e.align
		if a == nil {
			continue
		}
		if a.pending != nil {
			if i == file {
				marked = append([]*fileAlign{a}, marked...)
			} else {
				marked = append(marked, a)
			}
		}
		if a.replace != nil {
			f.replacing = a
		}
	}
	if len(marked) == 0 {
		return
	}
	for _, a := range marked[1:] {
		f.marked = a
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

// help returns the keys of the status bar.
func (f *Flow) help() string {
	return statusKeys(f.cfg, barKeys{enter: "anchor", back: "to_files", commits: f.inLog(), flow: true})
}

func (f *Flow) View() tea.View { return tea.NewView(f.frame()) }

func (f *Flow) frame() string {
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
	if f.inLog() && p < len(f.parts) {
		c := f.parts[p].commit
		line := " " + t.CommitHash.Render(c.Short) + " " + t.Title.Render(c.Subject)
		b.WriteString(ansi.Truncate(line, f.width, "…") + "\n")
	}
	if file >= 0 {
		b.WriteString(paneTitles(t, f.parts[p].diff.files[file].paneNames(), f.active, f.cfg.Settings.SideBySide, f.width))
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
		files := f.parts[p].diff.files
		where = append(where, fmt.Sprintf("file %d/%d", file+1, len(files)))
		if a := files[file].align; a != nil {
			where = append(where, fmt.Sprintf("anchors %d", len(a.anchors)))
		}
	}
	msg := f.msg
	if msg == "" && f.marked != nil {
		msg = f.marked.markPrompt()
	}
	left := pageStatus(f.cfg, msg, f.help())
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
			return mark + text(line, t.Heading.Inherit(t.Zero))
		}
		return mark + text(line, t.Zero)
	case flowFile:
		e := part.diff.files[r.file]
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
			note = printable(part.err)
		case part.diff == nil:
			note = "loading…"
		}
		return mark + text("  "+note, t.Hint)
	}
	a := part.diff.files[r.file].align
	return a.render(sline{r.line, r.sub}, i == f.cursor, layout{f.width, f.xoff, f.active, f.built})
}
