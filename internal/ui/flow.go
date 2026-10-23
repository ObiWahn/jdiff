package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
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
	header []string     // commit header lines, the first one is the bar
	app    *App         // the files, nil until loaded
	err    string       // why the files could not be loaded
}

// far moves the cursor to the first or last row.
const far = 1 << 30

// Flow is the Bubble Tea model of the flow view: the diff views of all files
// one after the other, each starting with a bar, and in the log each commit
// starting with its message. Files share their diff views with the single
// file view, so anchors and folds carry over. Commits are loaded once they
// come near the cursor.
type Flow struct {
	cfg   *config.Config
	parts []*flowPart
	load  func(part int) (*App, error) // loads the files of a commit, nil outside the log

	rows      []flowRow
	cursor    int
	top       int
	xoff      int
	active    side
	marked    *Model // diff view holding a pending mark
	replacing *Model // diff view waiting for confirmation to replace anchors

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

// commitHeader returns the header lines of c: the bar, then the message
// indented as git log does.
func commitHeader(c gitsrc.Commit) []string {
	h := []string{fmt.Sprintf("commit %s · %s · %s", c.Short, c.Author, c.Date), "", "    " + c.Subject}
	if c.Body != "" {
		h = append(h, "")
		for l := range strings.SplitSeq(c.Body, "\n") {
			h = append(h, strings.TrimRight("    "+l, " "))
		}
	}
	return append(h, "")
}

func (f *Flow) Init() tea.Cmd { return nil }

func (f *Flow) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.keepScreenRow(func() { f.width, f.height = msg.Width, msg.Height })
	case tea.KeyMsg:
		for _, k := range keyNames(msg) {
			if cmd = f.key(k); cmd != nil {
				break
			}
		}
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
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "f":
		return toggleFlow
	case "backspace":
		return func() tea.Msg { return backMsg{} }
	case "tab":
		f.active = f.active.other()
	case "up", "k":
		f.moveCursor(-1)
	case "down", "j":
		f.moveCursor(1)
	case "pgup", "ctrl+b":
		f.moveCursor(-page)
	case "pgdown", "ctrl+f":
		f.moveCursor(page)
	case "ctrl+u":
		f.moveCursor(-page / 2)
	case "ctrl+d":
		f.moveCursor(page / 2)
	case "home", "g":
		f.moveCursor(-far)
	case "end", "G":
		f.moveCursor(far)
	case "left", "h":
		f.xoff = max(f.xoff-8, 0)
	case "right", "l":
		f.xoff += 8
	case "]", "[":
		if !f.search(direction(k == "]"), f.changeStart) {
			f.msg = "no more changes"
		}
	case "n", "p":
		if f.search(direction(k == "n"), f.isKind(flowFile, 0)) {
			f.barToTop()
		} else {
			f.msg = ends(k == "n", "last file", "first file")
		}
	case "N", "P":
		switch {
		case f.load == nil: // outside the log
		case f.search(direction(k == "N"), f.isKind(flowCommit, 0)):
			f.barToTop()
		default:
			f.msg = ends(k == "N", "oldest commit", "newest commit")
		}
	case "enter", "esc", " ", "ctrl+@":
		f.delegate(k)
	case "s":
		f.cfg.Settings.SideBySide = !f.cfg.Settings.SideBySide
		f.keepScreenRow(f.relocate)
		f.msg = saveState(f.cfg)
	case "w":
		f.cfg.Settings.ShowWhitespaces = !f.cfg.Settings.ShowWhitespaces
		f.msg = saveState(f.cfg)
	}
	return nil
}

// delegate passes an anchor or fold key to the diff view of the file under
// the cursor.
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
	m := e.view
	if k == "enter" && f.marked != m {
		f.dropMark()
	}
	m.syncLayout()
	m.cursor, m.sub, m.active, m.msg = r.line, r.sub, f.active, ""
	f.keepScreenRow(func() {
		m.key(k)
		e.updateStats()
		f.build()
		if i := f.findDiff(r.part, r.file, m.cursor, m.sub); i >= 0 {
			f.cursor = i
		}
	})
	f.active, f.msg = m.active, m.msg
	f.replacing = nil
	if m.replace != nil {
		f.replacing = m
	}
	if m.pending != nil {
		f.marked = m
	} else if f.marked == m {
		f.marked = nil
	}
}

// dropMark cancels a pending mark; the caller rebuilds the rows.
func (f *Flow) dropMark() {
	if m := f.marked; m != nil && m.pending != nil {
		m.pending = nil
		m.buildView()
		m.cursor = min(m.cursor, max(len(m.view)-1, 0))
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
		for i := range part.header {
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
			m := part.app.viewOf(e)
			for v := range m.view {
				for s := range m.rowLines(v, m.unified()) {
					f.rows = append(f.rows, flowRow{flowDiff, p, i, v, s})
				}
			}
		}
	}
	f.cursor = max(min(f.cursor, len(f.rows)-1), 0)
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

func (f *Flow) unloaded(part int) bool {
	p := f.parts[part]
	return p.app == nil && p.err == ""
}

// unloadedIn returns a commit with rows in [a, b] that is not loaded yet, or
// -1.
func (f *Flow) unloadedIn(a, b int) int {
	if f.load == nil {
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
	f.cursor = max(min(f.cursor+delta, len(f.rows)-1), 0)
	f.scrollToCursor()
}

// search moves the cursor to the next row in direction dir for which ok
// holds, loading commits on the way, and reports whether there is one.
func (f *Flow) search(dir int, ok func(int) bool) bool {
	for {
		loaded := false
		for i := f.cursor + dir; i >= 0 && i < len(f.rows); i += dir {
			if p := f.rows[i].part; f.load != nil && f.unloaded(p) {
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
	return r.kind == flowDiff && r.sub == 0 && f.parts[r.part].app.files[r.file].view.changeStart(r.line)
}

// show puts the cursor on line sub of diff row line of file in part, on
// the file bar for line -1 and on the commit bar for file -1, at screen row
// screenRow.
func (f *Flow) show(part, file, line, sub, screenRow int) {
	if f.load != nil && f.unloaded(part) {
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
	if f.load != nil {
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
	h := f.bodyHeight()
	if f.cursor < f.top {
		f.top = f.cursor
	} else if f.cursor >= f.top+h {
		f.top = f.cursor - h + 1
	}
	f.top = max(min(f.top, len(f.rows)-h), 0)
}

func (f *Flow) help() string {
	h := "f single file · space fold · ]/[ change · n/p file · "
	if f.load != nil {
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
	if f.load != nil {
		c := f.parts[p].commit
		line := " " + t.CommitHash.Render(c.Short) + " " + t.Title.Render(c.Subject)
		b.WriteString(ansi.Truncate(line, f.width, "…") + "\n")
	}
	if file >= 0 {
		b.WriteString(paneTitles(t, f.parts[p].app.files[file].paneNames(), f.active, f.width))
	}
	return b.String() + "\n"
}

func (f *Flow) status() string {
	p, file, _ := f.position()
	var where []string
	if f.load != nil {
		where = append(where, fmt.Sprintf("commit %d/%d", p+1, len(f.parts)))
	}
	if file >= 0 {
		files := f.parts[p].app.files
		where = append(where, fmt.Sprintf("file %d/%d", file+1, len(files)))
		if m := files[file].view; m != nil {
			where = append(where, fmt.Sprintf("anchors %d", len(m.anchors)))
		}
	}
	left := f.msg
	if left == "" {
		left = f.help()
	}
	return statusLine(f.cfg.Theme, f.width, left, " "+strings.Join(where, " · "))
}

// row renders row i. Diff rows are drawn by the file's diff view with the
// flow's pane, scroll offset and width; other rows show the cursor in the
// first column.
func (f *Flow) row(i int) string {
	t := f.cfg.Theme
	r := f.rows[i]
	part := f.parts[r.part]
	mark := " "
	if i == f.cursor {
		mark = t.Cursor.Render(" ")
	}
	w := max(f.width-1, 0)
	fill := func(s string) string {
		s = ansi.Truncate(s, w, "…")
		return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
	}

	switch r.kind {
	case flowCommit:
		text := part.header[r.line]
		switch r.line {
		case 0:
			return mark + t.CommitBar.Render(fill(" "+text))
		case 2:
			return mark + t.Zero.Bold(true).Render(ansi.Truncate(text, w, "…"))
		}
		return mark + t.Zero.Render(ansi.Truncate(text, w, "…"))
	case flowFile:
		e := part.app.files[r.file]
		text := " "
		if lbl := e.label(t); lbl != "" {
			text += lbl + " "
		}
		text += e.name()
		if e.Binary {
			text += "   binary"
		} else if e.added > 0 || e.removed > 0 {
			text += fmt.Sprintf("   +%d -%d", e.added, e.removed)
		}
		return mark + t.FileBar.Render(fill(text))
	case flowNote:
		text := "no changed files"
		switch {
		case r.file >= 0:
			text = "binary file"
		case part.err != "":
			text = part.err
		}
		return mark + t.Hint.Render(ansi.Truncate("  "+text, w, "…"))
	}
	m := part.app.files[r.file].view
	m.width, m.xoff, m.active = f.width, f.xoff, f.active
	m.syncLayout()
	return m.screenLine(sline{r.line, r.sub}, i == f.cursor)
}
