// Package ui implements the interactive side-by-side diff view.
package ui

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/source"
)

// Model is the Bubble Tea model of the diff view: a single file view of a
// fileDiff, which other views may show and change too.
type Model struct {
	*fileDiff

	screen        []sline // screen lines of the displayed rows
	screenOf      []int   // view index -> first screen line
	screenUnified bool    // layout screen was built for: normal, not side by side
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
	canBack       bool // shown from the file selector, which handles backspace
	commitNav     bool // shown from the log, which switches commits

	msg   string
	watch *source.File // the files on disk, reloaded when they change; nil when not watched
}

// New returns a model comparing oldLines (left) with newLines (right).
func New(cfg *config.Config, oldName, newName string, oldLines, newLines []string) *Model {
	d := newFileDiff(cfg, [2]string{oldName, newName}, [2][]string{oldLines, newLines})
	(&moveGroup{}).add(d, newName)
	return newModel(d)
}

// NewWatched returns a model comparing the sides of f, which it reloads
// when they change on disk (see watch_interval).
func NewWatched(cfg *config.Config, f source.File) *Model {
	m := New(cfg, f.OldFile, f.NewFile, f.Old, f.New)
	m.watch = &f
	return m
}

// newModel returns the single file view of d.
func newModel(d *fileDiff) *Model {
	m := &Model{fileDiff: d}
	m.buildScreen()
	if slices.Equal(d.lines[left], d.lines[right]) {
		m.msg = "files are identical"
	}
	if w := d.cfg.Warning(); w != "" {
		m.msg = w
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	if m.watch != nil {
		return watchTick(m.cfg)
	}
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.syncLayout()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.keepScreenRow(func() { m.width, m.height = msg.Width, msg.Height })
	case tea.KeyMsg:
		return m, pressKeys(msg, m.key)
	case watchTickMsg:
		if msg := refreshFile(m.watch, m.fileDiff, m); msg != "" {
			m.msg = msg
		}
		return m, watchTick(m.cfg)
	}
	return m, nil
}

func (m *Model) key(k string) tea.Cmd {
	if m.replace != nil && k != "enter" && k != "esc" {
		m.replace = nil
		m.msg = ""
	}

	page := max(m.bodyHeight()-1, 1)
	if delta, paging, ok := motion(k, page, len(m.screen)); ok {
		if paging {
			m.scroll(delta)
		} else {
			m.moveCursor(delta)
		}
		return nil
	}
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "tab":
		m.active = m.active.other()
	case "ctrl+u":
		m.scroll(-page / 2)
	case "ctrl+d":
		m.scroll(page / 2)
	case "left", "h":
		m.xoff = max(m.xoff-8, 0)
	case "right", "l":
		m.xoff += 8
	case "]":
		m.nextChange(1)
	case "[":
		m.nextChange(-1)
	case "n", "p":
		if m.canBack {
			dir := switchFileMsg(direction(k == "n"))
			return func() tea.Msg { return dir }
		}
	case "N", "P":
		if m.commitNav {
			return switchCommit(k)
		}
	case "f":
		if m.canBack {
			return toggleFlow
		}
	case "s":
		m.msg = toggle(m.cfg, &m.cfg.Settings.SideBySide)
		m.syncLayout()
	case "w":
		m.msg = toggle(m.cfg, &m.cfg.Settings.ShowWhitespaces)
	case "enter", "esc", " ", "ctrl+@":
		m.act(k)
	case "o":
		return m.jump()
	}
	return nil
}

// jump moves the cursor to the other end of the moved line under it, which
// keeps its screen row. An end in another file is shown by the file
// selector.
func (m *Model) jump() tea.Cmd {
	end, msg, ok := m.otherEnd(spot{m.cursor, m.sub, m.active}, m.screenUnified)
	if !ok {
		m.msg = msg
		return nil
	}
	if end.file != m.groupIndex {
		j := jumpMsg{end, m.pos() - m.top, msg}
		return func() tea.Msg { return j }
	}
	m.show(m.spotOf(end.side, end.line), m.pos()-m.top)
	m.msg = msg
	return nil
}

// show puts the cursor on at, at screen row screenRow.
func (m *Model) show(at spot, screenRow int) {
	m.active = at.side
	m.place(at.v, at.sub, screenRow)
}

// act runs an anchor or fold key on the diff at the cursor, which stays on
// its screen row.
func (m *Model) act(k string) {
	var msg string
	m.keepScreenRow(func() {
		changes := m.changes
		var at spot
		at, msg = m.fileDiff.act(k, spot{m.cursor, m.sub, m.active}, m.screenUnified)
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
func (m *Model) realign() {
	m.align()
	m.buildScreen()
	m.cursor = min(m.cursor, max(len(m.view)-1, 0))
}

// buildScreen computes the screen lines of the displayed rows for the
// current layout.
func (m *Model) buildScreen() {
	m.screenUnified = m.unified()
	m.screen, m.screenOf = m.screenLines(m.screenUnified)
	m.screenChanges, m.screenReloads, m.screenView, m.screenRows = m.changes, m.reloads, m.view, m.rows
}

// syncLayout rebuilds the screen lines when another view or a reload changed
// the diff, keeping the cursor on its line, or when the layout was switched, here or
// in another view, keeping the cursor on its screen row.
func (m *Model) syncLayout() {
	switch {
	case m.screenChanges != m.changes:
		m.cursor = m.cursorAfterChange()
		m.buildScreen()
		m.scrollToCursor()
	case m.screenUnified != m.unified():
		m.keepScreenRow(m.buildScreen)
	}
}

// cursorAfterChange returns the displayed row that shows the line of the
// cursor's row in the rows screen was built for: its old line, else its new
// one, followed through a reload since.
func (m *Model) cursorAfterChange() int {
	if m.cursor >= len(m.screenView) || len(m.view) == 0 {
		return 0
	}
	if v := m.follow(m.screenRows[m.screenView[m.cursor].row], m.screenReloads != m.reloads); v >= 0 {
		return v
	}
	return min(m.cursor, len(m.view)-1)
}

// pos returns the screen line of the cursor.
func (m *Model) pos() int {
	if m.cursor >= len(m.view) {
		return 0
	}
	return m.screenOf[m.cursor] + min(m.sub, m.rowLines(m.cursor, m.screenUnified)-1)
}

// place puts the cursor on line sub of displayed row v at screen row
// screenRow.
func (m *Model) place(v, sub, screenRow int) {
	m.syncLayout()
	m.cursor, m.sub = v, sub
	m.top = m.pos() - screenRow
	m.scrollToCursor()
}

// takeOver continues where flow f leaves this file: with its pane and
// scrolling, and, unless line is -1, its cursor on diff row line at screen
// row screenRow.
func (m *Model) takeOver(f *Flow, line, screenRow int) {
	m.active, m.xoff = f.active, f.xoff
	if line >= 0 {
		m.place(line, f.cursorSub(), screenRow)
	}
}

// moveCursor moves the cursor by delta screen lines.
func (m *Model) moveCursor(delta int) {
	if len(m.screen) == 0 {
		return
	}
	s := m.screen[max(min(m.pos()+delta, len(m.screen)-1), 0)]
	m.cursor, m.sub = s.v, s.sub
	m.scrollToCursor()
}

// scroll moves the window by delta screen lines, as far as it can, and the
// cursor along, so that the cursor keeps its screen row.
func (m *Model) scroll(delta int) {
	if len(m.screen) == 0 {
		return
	}
	top := m.top
	m.top = max(min(m.top+delta, len(m.screen)-m.bodyHeight()), 0)
	m.moveCursor(m.top - top)
}

// nextChange moves the cursor to the start of the next (dir > 0) or previous
// block of non-equal rows.
func (m *Model) nextChange(dir int) {
	for i := m.cursor + dir; i >= 0 && i < len(m.view); i += dir {
		if m.changeStart(i) {
			m.cursor, m.sub = i, 0
			m.scrollToCursor()
			return
		}
	}
	m.msg = "no more changes"
}

func (m *Model) bodyHeight() int { return max(m.height-2, 1) }

// keepScreenRow runs f, which may rebuild the view, move the cursor or
// resize, and then scrolls so that the cursor stays on the same screen row.
// Near the start or end of the file it moves as little as possible.
func (m *Model) keepScreenRow(f func()) {
	offset := m.pos() - m.top
	f()
	m.top = m.pos() - offset
	m.scrollToCursor()
}

func (m *Model) scrollToCursor() {
	m.top = scrollWindow(m.pos(), m.top, m.bodyHeight(), len(m.screen))
}
