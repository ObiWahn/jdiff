// Package ui implements the interactive side-by-side diff view.
package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"jdiff/internal/config"
	"jdiff/internal/diff"
)

type side int

const (
	left side = iota
	right
)

func (s side) other() side { return 1 - s }

// mark is a line chosen as the first half of a new anchor.
type mark struct {
	side side
	line int
}

// vrow is a displayed row: the diff row row, or when hidden > 0 a fold
// standing for the unchanged rows [row, row+hidden). block is the index of
// the foldable block the row belongs to, or -1.
type vrow struct{ row, hidden, block int }

// sline is a screen line of the diff view: line sub of displayed row v.
// Side by side every displayed row is one line; in the normal layout a
// changed row shows its removed line, then its added line.
type sline struct{ v, sub int }

// block is a foldable run of unchanged rows [start, end).
type block struct{ start, end int }

// Model is the Bubble Tea model of the diff view.
type Model struct {
	cfg   *config.Config
	names [2]string
	lines [2][]string

	anchors []diff.Anchor // sorted by Left, never crossing
	rows    []diff.Row
	rowOf   [2][]int // line index -> row index, per side

	blocks   []block      // foldable blocks of unchanged rows
	foldOpen map[int]bool // open state by the block's first left line
	view     []vrow       // displayed rows
	viewOf   []int        // row index -> view index

	screen        []sline // screen lines of the displayed rows
	screenOf      []int   // view index -> first screen line
	screenUnified bool    // layout screen was built for: normal, not side by side

	cursor int // index into view
	sub    int // line of the cursor row in the normal layout
	active side
	top    int // first screen line
	xoff   int // horizontal scroll in columns

	width, height int
	canBack       bool // shown from the file selector, which handles backspace
	commitNav     bool // shown from the log, which switches commits

	pending *mark        // first half of an anchor being created
	replace *diff.Anchor // anchor waiting for confirmation to replace conflicts
	msg     string
}

// New returns a model comparing oldLines (left) with newLines (right).
func New(cfg *config.Config, oldName, newName string, oldLines, newLines []string) *Model {
	m := &Model{
		cfg:      cfg,
		foldOpen: map[int]bool{},
		names:    [2]string{oldName, newName},
		lines:    [2][]string{oldLines, newLines},
	}
	m.realign()
	if slices.Equal(oldLines, newLines) {
		m.msg = "files are identical"
	}
	if w := cfg.Warning(); w != "" {
		m.msg = w
	}
	return m
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.syncLayout()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.keepScreenRow(func() { m.width, m.height = msg.Width, msg.Height })
	case tea.KeyMsg:
		return m, pressKeys(msg, m.key)
	}
	return m, nil
}

func (m *Model) key(k string) tea.Cmd {
	if m.replace != nil {
		an := *m.replace
		m.replace = nil
		switch k {
		case "enter":
			for _, c := range diff.Conflicts(m.anchors, an) {
				m.removeAnchor(c)
			}
			m.addAnchor(an)
			return nil
		case "esc":
			m.msg = "replace cancelled"
			return nil
		}
		m.msg = ""
	}

	page := max(m.bodyHeight()-1, 1)
	if delta, ok := motion(k, page, len(m.screen)); ok {
		m.moveCursor(delta)
		return nil
	}
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "tab":
		m.active = m.active.other()
	case "ctrl+u":
		m.moveCursor(-page / 2)
	case "ctrl+d":
		m.moveCursor(page / 2)
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
	case "esc":
		if m.pending != nil {
			m.pending = nil
			m.msg = "mark cancelled"
		}
	case "enter":
		m.enter()
	case " ":
		m.toggleNearestBlock()
	case "ctrl+@": // ctrl+space
		m.toggleAllBlocks()
	}
	return nil
}

// enter implements the anchor workflow: mark a line, then pick its partner
// in the other pane. Enter on an anchored line removes that anchor. In the
// normal layout the line under the cursor decides the side.
func (m *Model) enter() {
	if m.screenUnified {
		m.active = m.unifiedSide()
	}
	line := m.lineAt(m.active, m.cursor)
	if line < 0 {
		m.msg = "no line here"
		if m.cursor < len(m.view) && m.view[m.cursor].hidden > 0 {
			m.msg = "unchanged lines folded, space expands"
		}
		return
	}
	if an, ok := m.anchorAt(m.active, line); ok {
		m.keepScreenRow(func() {
			m.removeAnchor(an)
			m.realign()
			m.cursor, m.sub = m.viewOf[m.rowOf[m.active][line]], int(m.active)
		})
		m.msg = "removed anchor " + anchorName(an)
		return
	}
	if m.pending == nil || m.pending.side == m.active {
		if m.pending != nil && m.pending.line == line {
			m.pending = nil
			m.msg = "mark cancelled"
			return
		}
		m.pending = &mark{m.active, line}
		m.active = m.active.other()
		m.msg = fmt.Sprintf("marked %s%d, pick the matching line (esc cancels)",
			sideName(m.pending.side), line+1)
		return
	}

	an := diff.Anchor{Left: m.pending.line, Right: line}
	if m.active == left {
		an = diff.Anchor{Left: line, Right: m.pending.line}
	}
	if conflicts := diff.Conflicts(m.anchors, an); len(conflicts) > 0 {
		names := make([]string, len(conflicts))
		for i, c := range conflicts {
			names[i] = anchorName(c)
		}
		m.replace = &an
		m.msg = fmt.Sprintf("%s crosses %s: enter replaces, esc cancels",
			anchorName(an), strings.Join(names, ", "))
		return
	}
	m.addAnchor(an)
}

func (m *Model) addAnchor(an diff.Anchor) {
	i, _ := slices.BinarySearchFunc(m.anchors, an, func(x, y diff.Anchor) int {
		return x.Left - y.Left
	})
	m.anchors = slices.Insert(m.anchors, i, an)
	m.pending = nil
	// The line picked last keeps its place; the rest aligns around it.
	m.keepScreenRow(func() {
		m.realign()
		m.cursor, m.sub = m.viewOf[m.rowOf[left][an.Left]], int(m.active)
	})
	m.msg = "anchored " + anchorName(an)
}

// unifiedSide returns the side of the line under the cursor in the normal
// layout. An unchanged line belongs to both: it takes the side opposite a
// pending mark, else the old side.
func (m *Model) unifiedSide() side {
	if m.cursor >= len(m.view) || m.view[m.cursor].hidden > 0 {
		return m.active
	}
	switch r := m.rows[m.view[m.cursor].row]; {
	case r.Kind == diff.Delete:
		return left
	case r.Kind == diff.Insert:
		return right
	case r.Kind == diff.Change:
		return side(min(m.sub, 1))
	case m.pending != nil:
		return m.pending.side.other()
	}
	return left
}

func (m *Model) removeAnchor(an diff.Anchor) {
	m.anchors = slices.DeleteFunc(m.anchors, func(x diff.Anchor) bool { return x == an })
}

func (m *Model) anchorAt(s side, line int) (diff.Anchor, bool) {
	for _, an := range m.anchors {
		if (s == left && an.Left == line) || (s == right && an.Right == line) {
			return an, true
		}
	}
	return diff.Anchor{}, false
}

func (m *Model) realign() {
	m.rows = diff.Align(m.lines[left], m.lines[right], m.anchors,
		diff.Options{SplitIndent: m.cfg.Settings.OnlyChangedWhitespaces})
	for s := range m.rowOf {
		m.rowOf[s] = make([]int, len(m.lines[s]))
	}
	for i, r := range m.rows {
		if r.Left >= 0 {
			m.rowOf[left][r.Left] = i
		}
		if r.Right >= 0 {
			m.rowOf[right][r.Right] = i
		}
	}
	m.buildView()
	m.cursor = min(m.cursor, max(len(m.view)-1, 0))
}

// buildView computes the displayed rows. Unchanged rows further than the
// context from any change, anchor or pending mark form foldable blocks,
// shown as one fold row while closed; a single such row is never a block.
func (m *Model) buildView() {
	keep := make([]bool, len(m.rows))
	ctx := max(m.cfg.Settings.Context, 0)
	for i, r := range m.rows {
		if r.Kind != diff.Equal || r.Anchored || m.isPending(r) {
			for j := max(i-ctx, 0); j <= min(i+ctx, len(m.rows)-1); j++ {
				keep[j] = true
			}
		}
	}

	m.blocks = m.blocks[:0]
	m.view = m.view[:0]
	m.viewOf = make([]int, len(m.rows))
	for i := 0; i < len(m.rows); {
		j := i + 1
		if !keep[i] {
			for j < len(m.rows) && !keep[j] {
				j++
			}
		}
		if j-i < 2 {
			m.viewOf[i] = len(m.view)
			m.view = append(m.view, vrow{row: i, block: -1})
			i = j
			continue
		}
		b := len(m.blocks)
		m.blocks = append(m.blocks, block{i, j})
		open := m.blockOpen(m.blocks[b])
		for k := i; k < j; k++ {
			m.viewOf[k] = len(m.view)
			if open {
				m.view = append(m.view, vrow{row: k, block: b})
			}
		}
		if !open {
			m.view = append(m.view, vrow{row: i, hidden: j - i, block: b})
		}
		i = j
	}
	m.buildScreen()
}

func (m *Model) unified() bool { return !m.cfg.Settings.SideBySide }

// rowLines returns the number of screen lines of displayed row v.
func (m *Model) rowLines(v int, unified bool) int {
	if !unified || m.view[v].hidden > 0 {
		return 1
	}
	if m.rows[m.view[v].row].Kind == diff.Change {
		return 2
	}
	return 1
}

// buildScreen computes the screen lines of the displayed rows for the
// current layout.
func (m *Model) buildScreen() {
	m.screenUnified = m.unified()
	m.screen = m.screen[:0]
	m.screenOf = make([]int, len(m.view))
	for v := range m.view {
		m.screenOf[v] = len(m.screen)
		for s := range m.rowLines(v, m.screenUnified) {
			m.screen = append(m.screen, sline{v, s})
		}
	}
}

// syncLayout rebuilds the screen lines when the layout was switched, here
// or in another view, keeping the cursor on its screen row.
func (m *Model) syncLayout() {
	if m.screenUnified == m.unified() {
		return
	}
	m.keepScreenRow(m.buildScreen)
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

// blockOpen reports whether b is unfolded. Blocks start closed when
// collapse_unchanged is set; toggled state is kept by the block's first
// line, so it survives realigning as long as the block starts there.
func (m *Model) blockOpen(b block) bool {
	if open, ok := m.foldOpen[m.rows[b.start].Left]; ok {
		return open
	}
	return !m.cfg.Settings.CollapseUnchanged
}

func (m *Model) isPending(r diff.Row) bool {
	if m.pending == nil {
		return false
	}
	if m.pending.side == left {
		return r.Left == m.pending.line
	}
	return r.Right == m.pending.line
}

// toggleNearestBlock folds or unfolds the block of unchanged rows under the
// cursor, or else the one closest to it (the lower one on a tie). The cursor
// stays on its row, or on the fold containing it.
func (m *Model) toggleNearestBlock() {
	b := m.nearestBlock()
	if b < 0 {
		m.msg = "no unchanged code to fold"
		return
	}
	r := m.view[m.cursor].row
	m.keepScreenRow(func() {
		blk := m.blocks[b]
		m.foldOpen[m.rows[blk.start].Left] = !m.blockOpen(blk)
		m.buildView()
		m.cursor = m.viewOf[r]
	})
}

// toggleAllBlocks unfolds all blocks of unchanged rows when one of them is
// folded, and folds them all otherwise. The cursor stays on its row, or on
// the fold containing it.
func (m *Model) toggleAllBlocks() {
	if len(m.blocks) == 0 {
		m.msg = "no unchanged code to fold"
		return
	}
	open := false
	for _, b := range m.blocks {
		if !m.blockOpen(b) {
			open = true
			break
		}
	}
	r := m.view[m.cursor].row
	m.keepScreenRow(func() {
		for _, b := range m.blocks {
			m.foldOpen[m.rows[b.start].Left] = open
		}
		m.buildView()
		m.cursor = m.viewOf[r]
	})
}

// nearestBlock returns the index of the block at or closest to the cursor,
// or -1 when there is none.
func (m *Model) nearestBlock() int {
	for d := 0; d < len(m.view); d++ {
		for _, v := range []int{m.cursor + d, m.cursor - d} {
			if v >= 0 && v < len(m.view) && m.view[v].block >= 0 {
				return m.view[v].block
			}
		}
	}
	return -1
}

// lineAt returns the line index shown on side s of displayed row v, or -1.
func (m *Model) lineAt(s side, v int) int {
	if v < 0 || v >= len(m.view) || m.view[v].hidden > 0 {
		return -1
	}
	if r := m.rows[m.view[v].row]; s == left {
		return r.Left
	} else {
		return r.Right
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

// changeStart reports whether displayed row v starts a block of non-equal
// rows.
func (m *Model) changeStart(v int) bool {
	changed := func(v int) bool {
		return m.view[v].hidden == 0 && m.rows[m.view[v].row].Kind != diff.Equal
	}
	return changed(v) && (v == 0 || !changed(v-1))
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

func sideName(s side) string {
	if s == left {
		return "L"
	}
	return "R"
}

func anchorName(an diff.Anchor) string {
	return fmt.Sprintf("L%d↔R%d", an.Left+1, an.Right+1)
}
