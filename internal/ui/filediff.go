package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/keys"
	"jdiff/internal/source"
	"jdiff/watch"
)

// FileDiff is the Bubble Tea model of the diff view (keys of diff_view): a
// single file view of a fileAlign, which other views may show and change too.
type FileDiff struct {
	*fileAlign

	screen      []sline       // screen lines of the displayed rows
	screenAt    map[sline]int // screen line -> its index in screen
	screenShape shape         // what screen was built for
	// The displayed rows screen was built for, to find the cursor's line
	// again when another view or a reload changed them.
	screenChanges int
	screenReloads int
	screenView    []vrow
	screenRows    []diff.Row

	cursor int // index into view
	sub    int // line of the cursor row in the normal layout
	active side
	top    int // first screen line
	xoff   int // horizontal scroll in columns

	width, height int
	canBack       bool   // shown from the file list, which handles backspace
	commitNav     bool   // shown from the log, which switches commits
	head          string // the line of the commit above the names, shown from the log; "" for none

	msg    string
	onDisk *source.File // the files on disk, reloaded when they change; nil when not watched
	// disk watches them; those it does not watch are checked every
	// watch_interval.
	disk   diskWatch
	prefix string // the action, "set_mark" or "jump", while waiting for the letter of a mark
}

// NewFileDiff returns a model comparing oldLines (left) with newLines (right).
func NewFileDiff(cfg *config.Config, oldName, newName string, oldLines, newLines []string) *FileDiff {
	a := newFileAlign(cfg, [2]string{oldName, newName}, [2][]string{oldLines, newLines})
	a.markPath = newName
	(&moveGroup{}).add(a, a.names)
	return newFileDiff(a)
}

// NewWatchedFileDiff returns a model comparing the sides of f, which it reloads
// when they change on disk (see watch_interval).
func NewWatchedFileDiff(cfg *config.Config, f source.File) *FileDiff {
	m := NewFileDiff(cfg, f.OldFile, f.NewFile, f.Old, f.New)
	m.onDisk = &f
	m.markPath = repoPath(cfg.Repo(), f.NewFile)
	return m
}

// newFileDiff returns the single file view of a.
func newFileDiff(a *fileAlign) *FileDiff {
	m := &FileDiff{fileAlign: a}
	m.buildScreen()
	if slices.Equal(a.lines[left], a.lines[right]) {
		m.msg = "files are identical"
	}
	if w := a.cfg.Warning(); w != "" {
		m.msg = w
	}
	return m
}

// Init starts watching the files, when they are on disk, see watchFiles.
func (m *FileDiff) Init() tea.Cmd { return m.watchFiles() }

// watchFiles watches the files, when they are on disk, while auto reload is
// on, and returns the command waiting for their changes and the check of
// those not watched; off, it watches them no more.
func (m *FileDiff) watchFiles() tea.Cmd {
	if m.onDisk == nil {
		return nil
	}
	if !m.cfg.AutoReload {
		m.disk.stop()
		return nil
	}
	cmd, msg := m.disk.set(m.cfg, diskPaths(*m.onDisk))
	if msg != "" {
		m.msg = msg
	}
	return batch(cmd, m.poll())
}

// shareWatcher makes m watch its files by w, which App shares.
func (m *FileDiff) shareWatcher(w *watch.Watcher) { m.disk.share(w) }

// poll returns the next check of the files, unless one is pending or both
// are watched.
func (m *FileDiff) poll() tea.Cmd {
	if m.onDisk == nil {
		return nil
	}
	return m.disk.poll(m.cfg, m, diskPaths(*m.onDisk))
}

func (m *FileDiff) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.syncLayout()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.keepScreenRow(func() {
			m.width, m.height = msg.Width, msg.Height
			m.syncLayout() // wrapped lines take other pieces
		})
	case tea.KeyPressMsg:
		return m, m.key(msg.String())
	case autoReloadMsg:
		if msg.on && m.onDisk != nil { // changed while off
			if msg := refreshFile(m.onDisk, m.fileAlign, m.syncLayout); msg != "" {
				m.msg = msg
			}
		}
		return m, m.watchFiles()
	case watchTickMsg:
		if !m.disk.ticked(m.cfg, m, msg) {
			return m, nil
		}
		if msg := refreshFile(m.onDisk, m.fileAlign, m.syncLayout); msg != "" {
			m.msg = msg
		}
		return m, m.poll()
	case changedMsg:
		if msg.w == nil || msg.w != m.disk.w {
			return m, nil
		}
		if msg := refreshFile(m.onDisk, m.fileAlign, m.syncLayout); msg != "" {
			m.msg = msg
		}
		return m, batch(m.disk.next(), m.poll())
	case watchEndedMsg:
		if !m.disk.ended(msg.w) {
			return m, nil
		}
		return m, m.poll()
	case refreshMsg:
		m.refresh(msg.quiet)
	case showMsg:
		m.msg = string(msg)
	case toggleUntrackedMsg:
		m.msg = untrackedOnlyInDiff
	case toggleFlowMsg:
		m.msg = flowOnlyOfSeveral
	case settingChangedMsg:
		if msg.problem != "" {
			m.msg = msg.problem
		}
		if msg.action == "inotify_max_dirs" {
			m.disk.limit(m.cfg)
			return m, m.poll()
		}
	case findMsg:
		m.findText(int(msg))
	}
	return m, nil
}

// stop stops watching the files: the view is replaced.
func (m *FileDiff) stop() { m.disk.stop() }

// refresh reads both files again, whether their stamps changed or not. With
// quiet, it does not say when nothing changed.
func (m *FileDiff) refresh(quiet bool) {
	if m.onDisk == nil {
		m.msg = nothingToReload
		return
	}
	m.onDisk.OldStamp, m.onDisk.NewStamp = source.Stamp{}, source.Stamp{}
	switch msg := refreshFile(m.onDisk, m.fileAlign, m.syncLayout); {
	case msg != "":
		m.msg = msg
	case !quiet:
		m.msg = nothingChanged
	}
}

func (m *FileDiff) key(k string) tea.Cmd {
	h := m.cfg.Keys.Key(keys.DiffView, k)
	if m.replace != nil && h.Action != "anchor" && h.Action != "cancel_mark" {
		m.replace = nil
	}
	m.msg = ""
	if p := m.prefix; p != "" {
		m.prefix = ""
		switch {
		case !isMarkLetter(k):
		case p == "set_mark":
			m.msg = setMark(m.cfg, m.fileAlign, k, m.cursor)
		default:
			return m.gotoMark(k, m.pos()-m.top)
		}
		return nil
	}
	if cmd, ok := diffKey(m, h, max(m.bodyHeight()-1, 1), len(m.screen)); ok {
		return cmd
	}
	switch h.Action {
	case "next_file":
		if m.canBack {
			dir := switchFileMsg(direction(h.Index == 0))
			return func() tea.Msg { return dir }
		}
	case "history":
		if m.commitNav {
			return switchCommit(h)
		}
	case "flow":
		if m.canBack {
			return toggleFlow
		}
	case "anchor", "cancel_mark", "fold", "fold_all":
		m.act(h.Action)
	case "moved":
		return m.jump()
	default:
		return pageKey(m.cfg.Keys, h, false)
	}
	return nil
}

// scrollSideways, switchPane and askLetter act for diffKey.
func (m *FileDiff) scrollSideways(cols int)      { m.xoff = max(m.xoff+cols, 0) }
func (m *FileDiff) switchPane()                  { m.active = m.active.other() }
func (m *FileDiff) askLetter(action, msg string) { m.prefix, m.msg = action, msg }

// gotoMark moves the cursor to the line of mark letter, at screen row
// screenRow. A mark in another file of the diff is shown by the file
// list.
func (m *FileDiff) gotoMark(letter string, screenRow int) tea.Cmd {
	mk, ok := m.cfg.Mark(letter)
	switch {
	case !ok:
		m.msg = "no mark " + letter
	case mk.Path == m.markPath:
		if at, ok := m.markSpot(letter, mk); ok {
			m.jumpTo(at)
		}
	case m.canBack:
		j := markJumpMsg{letter, screenRow}
		return func() tea.Msg { return j }
	default:
		m.msg = notInDiff(letter, mk)
	}
	return nil
}

// markSpot returns the cursor on the line of mark mk, called letter, of
// this file, unfolding it, and sets the message. ok is false for an empty
// new file.
func (m *FileDiff) markSpot(letter string, mk config.Mark) (at spot, ok bool) {
	line, note, ok := m.markLine(mk)
	if !ok {
		m.msg = markMsg(letter, "the new file is empty")
		return spot{}, false
	}
	m.msg = markMsg(letter, note)
	return m.reveal(right, line, m.shape()), true
}

// jump moves the cursor to the other end of the moved line under it (see
// jumpTo). An end in another file is shown by the file list, at the
// cursor's screen row.
func (m *FileDiff) jump() tea.Cmd {
	end, msg, ok := m.otherEnd(spot{m.cursor, m.sub, m.active}, m.screenShape)
	if !ok {
		m.msg = msg
		return nil
	}
	if end.file != m.groupIndex {
		j := jumpMsg{end, m.pos() - m.top, msg}
		return func() tea.Msg { return j }
	}
	m.jumpTo(m.spotOf(end.side, end.line, m.screenShape))
	m.msg = msg
	return nil
}

// jumpTo puts the cursor on at, for a jump within this view (g, '): the
// screen stays when at is on it, see jumpTop.
func (m *FileDiff) jumpTo(at spot) {
	m.syncLayout() // a fold opened for at: the cursor keeps its line
	from := m.pos()
	m.active, m.cursor, m.sub = at.side, at.v, at.sub
	m.top = jumpTop(from, m.pos(), m.top, m.bodyHeight(), len(m.screen))
}

// show puts the cursor on at, at screen row screenRow.
func (m *FileDiff) show(at spot, screenRow int) {
	m.active = at.side
	m.place(at.v, at.sub, screenRow)
}

// act runs an anchor or fold action on the diff at the cursor, which stays on
// its screen row.
func (m *FileDiff) act(action string) {
	var msg string
	m.keepScreenRow(func() {
		changes := m.changes
		var at spot
		at, msg = m.fileAlign.act(action, spot{m.cursor, m.sub, m.active}, m.screenShape)
		if m.changes != changes {
			m.buildScreen()
		}
		m.cursor, m.sub, m.active = at.v, at.sub, at.side
	})
	if msg != "" {
		m.msg = msg
	}
}

// realign recomputes the diff, after its settings changed.
func (m *FileDiff) realign() {
	m.align()
	m.buildScreen()
	m.cursor = min(m.cursor, max(len(m.view)-1, 0))
}

// shape returns the shape of the screen lines for the current layout and
// width.
func (m *FileDiff) shape() shape { return newShape(m.cfg, m.width) }

// buildScreen computes the screen lines of the displayed rows for the
// current layout and width.
func (m *FileDiff) buildScreen() {
	m.screenShape = m.shape()
	m.screen, m.screenAt = m.screenLines(m.screenShape)
	m.screenChanges, m.screenReloads, m.screenView, m.screenRows = m.changes, m.reloads, m.view, m.rows
}

// syncLayout rebuilds the screen lines when another view or a reload changed
// the diff, keeping the cursor on its line, or when the layout was switched,
// here or in another view, or wrapped lines need other pieces, keeping the
// cursor on its line, piece and screen row.
func (m *FileDiff) syncLayout() {
	switch {
	case m.screenChanges != m.changes:
		m.cursor = m.cursorAfterChange()
		m.buildScreen()
		m.scrollToCursor()
	case m.screenShape != m.shape():
		from := m.screenShape
		m.keepScreenRow(func() {
			m.buildScreen()
			if m.cursor < len(m.view) {
				m.sub = m.convertSub(m.cursor, m.sub, from, m.screenShape, m.active)
			}
		})
	}
}

// cursorAfterChange returns the displayed row that shows the line of the
// cursor's row in the rows screen was built for: its old line, else its new
// one, followed through a reload since.
func (m *FileDiff) cursorAfterChange() int {
	if m.cursor >= len(m.screenView) || len(m.view) == 0 {
		return 0
	}
	if v := m.follow(m.screenRows[m.screenView[m.cursor].row], m.screenReloads != m.reloads); v >= 0 {
		return v
	}
	return min(m.cursor, len(m.view)-1)
}

// pos returns the screen line of the cursor.
func (m *FileDiff) pos() int {
	if m.cursor >= len(m.view) {
		return 0
	}
	return m.screenAt[sline{m.cursor, min(m.sub, m.rowLines(m.cursor, m.screenShape)-1)}]
}

// place puts the cursor on line sub of displayed row v at screen row
// screenRow.
func (m *FileDiff) place(v, sub, screenRow int) {
	m.syncLayout()
	m.cursor, m.sub = v, sub
	m.top = m.pos() - screenRow
	m.scrollToCursor()
}

// takeOver continues where flow f leaves this file: with its pane and
// scrolling, and, unless line is -1, its cursor on diff row line at screen
// row screenRow.
func (m *FileDiff) takeOver(f *Flow, line, screenRow int) {
	m.active, m.xoff = f.active, f.xoff
	if line >= 0 {
		m.place(line, f.cursorSub(), screenRow)
	}
}

// moveCursor moves the cursor by delta screen lines.
func (m *FileDiff) moveCursor(delta int) {
	if len(m.screen) == 0 {
		return
	}
	s := m.screen[max(min(m.pos()+delta, len(m.screen)-1), 0)]
	m.cursor, m.sub = s.v, s.sub
	m.scrollToCursor()
}

// scroll moves the window by delta screen lines, as far as it can, and the
// cursor along, so that the cursor keeps its screen row.
func (m *FileDiff) scroll(delta int) {
	if len(m.screen) == 0 {
		return
	}
	top := m.top
	m.top = max(min(m.top+delta, len(m.screen)-m.bodyHeight()), 0)
	m.moveCursor(m.top - top)
}

// nextChange moves the cursor to the first line of the next (dir > 0) or
// previous block of changed rows.
func (m *FileDiff) nextChange(dir int) {
	for i := m.pos() + dir; i >= 0 && i < len(m.screen); i += dir {
		if m.blockStart(m.screen, i) {
			m.cursor, m.sub = m.screen[i].v, m.screen[i].sub
			m.scrollToCursor()
			return
		}
	}
	m.msg = "no more changes"
}

func (m *FileDiff) bodyHeight() int {
	if m.head != "" {
		return max(m.height-3, 1)
	}
	return max(m.height-2, 1)
}

// keepScreenRow runs f, which may rebuild the view, move the cursor or
// resize, and then scrolls so that the cursor stays on the same screen row.
// Near the start or end of the file it moves as little as possible.
func (m *FileDiff) keepScreenRow(f func()) {
	offset := m.pos() - m.top
	f()
	m.top = m.pos() - offset
	m.scrollToCursor()
}

func (m *FileDiff) scrollToCursor() {
	m.top = scrollWindow(m.pos(), m.top, m.bodyHeight(), len(m.screen))
}
