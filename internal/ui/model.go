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

// block is a foldable run of unchanged rows [start, end).
type block struct{ start, end int }

// switchFileMsg asks the file selector to show the next (1) or previous (-1)
// file.
type switchFileMsg int

// switchCommitMsg asks the log to show the next (1, older) or previous (-1)
// commit.
type switchCommitMsg int

// backMsg asks the log to return to the commit list, or the file selector to
// its list when it shows the flow view.
type backMsg struct{}

// toggleFlowMsg asks the log or the file selector to switch between the diff
// view of one file and the flow view of all files.
type toggleFlowMsg struct{}

func toggleFlow() tea.Msg { return toggleFlowMsg{} }

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

	cursor int // index into view
	active side
	top    int // first displayed row
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
	if len(cfg.Theme.Warnings) > 0 {
		m.msg = "theme: " + strings.Join(cfg.Theme.Warnings, "; ")
	}
	return m
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.keepScreenRow(func() { m.width, m.height = msg.Width, msg.Height })
	case tea.KeyMsg:
		for _, k := range keyNames(msg) {
			if cmd := m.key(k); cmd != nil {
				return m, cmd
			}
		}
	}
	return m, nil
}

// keyNames returns the key presses of msg. Bubble Tea delivers runes read
// together (held keys, fast typing over ssh) as one message; they are split so
// that each counts.
func keyNames(msg tea.KeyMsg) []string {
	if msg.Type != tea.KeyRunes || msg.Paste || msg.Alt || len(msg.Runes) < 2 {
		return []string{msg.String()}
	}
	out := make([]string, len(msg.Runes))
	for i, r := range msg.Runes {
		out[i] = string(r)
	}
	return out
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
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "tab":
		m.active = m.active.other()
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup", "ctrl+b":
		m.moveCursor(-page)
	case "pgdown", "ctrl+f":
		m.moveCursor(page)
	case "ctrl+u":
		m.moveCursor(-page / 2)
	case "ctrl+d":
		m.moveCursor(page / 2)
	case "home", "g":
		m.moveCursor(-len(m.view))
	case "end", "G":
		m.moveCursor(len(m.view))
	case "left", "h":
		m.xoff = max(m.xoff-8, 0)
	case "right", "l":
		m.xoff += 8
	case "]":
		m.nextChange(1)
	case "[":
		m.nextChange(-1)
	case "N", "P":
		if m.canBack {
			dir := switchFileMsg(1)
			if k == "P" {
				dir = -1
			}
			return func() tea.Msg { return dir }
		}
	case "n", "p":
		if m.commitNav {
			return switchCommit(k)
		}
	case "f":
		if m.canBack {
			return toggleFlow
		}
	case "esc":
		if m.pending != nil {
			m.pending = nil
			m.msg = "mark cancelled"
		}
	case "enter":
		m.enter()
	case " ":
		m.toggleNearestBlock()
	}
	return nil
}

// enter implements the anchor workflow: mark a line, then pick its partner
// in the other pane. Enter on an anchored line removes that anchor.
func (m *Model) enter() {
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
			m.cursor = m.viewOf[m.rowOf[m.active][line]]
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
	m.keepScreenRow(func() {
		m.realign()
		m.cursor = m.viewOf[m.rowOf[left][an.Left]]
	})
	m.msg = "anchored " + anchorName(an)
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

func (m *Model) moveCursor(delta int) {
	m.cursor = max(min(m.cursor+delta, len(m.view)-1), 0)
	m.scrollToCursor()
}

// nextChange moves the cursor to the start of the next (dir > 0) or previous
// block of non-equal rows.
func (m *Model) nextChange(dir int) {
	for i := m.cursor + dir; i >= 0 && i < len(m.view); i += dir {
		if m.changeStart(i) {
			m.cursor = i
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
	offset := m.cursor - m.top
	f()
	m.top = m.cursor - offset
	m.scrollToCursor()
}

func (m *Model) scrollToCursor() {
	h := m.bodyHeight()
	if m.cursor < m.top {
		m.top = m.cursor
	} else if m.cursor >= m.top+h {
		m.top = m.cursor - h + 1
	}
	m.top = max(min(m.top, len(m.view)-h), 0)
}

// switchCommit returns the command asking the log for the next (n) or
// previous (p) commit.
func switchCommit(k string) tea.Cmd {
	dir := switchCommitMsg(1)
	if k == "p" {
		dir = -1
	}
	return func() tea.Msg { return dir }
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
