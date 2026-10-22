// Package ui implements the interactive side-by-side diff view.
package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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

// Model is the Bubble Tea model of the diff view.
type Model struct {
	names [2]string
	lines [2][]string

	anchors []diff.Anchor // sorted by Left, never crossing
	rows    []diff.Row
	rowOf   [2][]int // line index -> row index, per side

	cursor int // row under the cursor
	active side
	top    int // first visible row
	xoff   int // horizontal scroll in columns

	width, height int
	canBack       bool // shown from the file selector, which handles backspace

	pending *mark        // first half of an anchor being created
	replace *diff.Anchor // anchor waiting for confirmation to replace conflicts
	msg     string
}

// New returns a model comparing oldLines (left) with newLines (right).
func New(oldName, newName string, oldLines, newLines []string) *Model {
	m := &Model{
		names: [2]string{oldName, newName},
		lines: [2][]string{oldLines, newLines},
	}
	m.realign()
	if slices.Equal(oldLines, newLines) {
		m.msg = "files are identical"
	}
	return m
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollToCursor()
	case tea.KeyMsg:
		return m, m.key(msg.String())
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
	case "pgdown", "ctrl+f", " ":
		m.moveCursor(page)
	case "ctrl+u":
		m.moveCursor(-page / 2)
	case "ctrl+d":
		m.moveCursor(page / 2)
	case "home", "g":
		m.moveCursor(-len(m.rows))
	case "end", "G":
		m.moveCursor(len(m.rows))
	case "left", "h":
		m.xoff = max(m.xoff-8, 0)
	case "right", "l":
		m.xoff += 8
	case "n":
		m.nextChange(1)
	case "N":
		m.nextChange(-1)
	case "esc":
		if m.pending != nil {
			m.pending = nil
			m.msg = "mark cancelled"
		}
	case "enter":
		m.enter()
	}
	return nil
}

// enter implements the anchor workflow: mark a line, then pick its partner
// in the other pane. Enter on an anchored line removes that anchor.
func (m *Model) enter() {
	line := m.lineAt(m.active, m.cursor)
	if line < 0 {
		m.msg = "no line here"
		return
	}
	if an, ok := m.anchorAt(m.active, line); ok {
		m.removeAnchor(an)
		m.realign()
		m.cursor = m.rowOf[m.active][line]
		m.scrollToCursor()
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
	m.realign()
	m.cursor = m.rowOf[left][an.Left]
	m.scrollToCursor()
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
	m.rows = diff.Align(m.lines[left], m.lines[right], m.anchors)
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
	m.cursor = min(m.cursor, max(len(m.rows)-1, 0))
}

// lineAt returns the line index shown on side s of row r, or -1.
func (m *Model) lineAt(s side, r int) int {
	if r < 0 || r >= len(m.rows) {
		return -1
	}
	if s == left {
		return m.rows[r].Left
	}
	return m.rows[r].Right
}

func (m *Model) moveCursor(delta int) {
	m.cursor = max(min(m.cursor+delta, len(m.rows)-1), 0)
	m.scrollToCursor()
}

// nextChange moves the cursor to the start of the next (dir > 0) or previous
// block of non-equal rows.
func (m *Model) nextChange(dir int) {
	isStart := func(i int) bool {
		return m.rows[i].Kind != diff.Equal && (i == 0 || m.rows[i-1].Kind == diff.Equal)
	}
	for i := m.cursor + dir; i >= 0 && i < len(m.rows); i += dir {
		if isStart(i) {
			m.cursor = i
			m.scrollToCursor()
			return
		}
	}
	m.msg = "no more changes"
}

func (m *Model) bodyHeight() int { return max(m.height-2, 1) }

func (m *Model) scrollToCursor() {
	h := m.bodyHeight()
	if m.cursor < m.top {
		m.top = m.cursor
	} else if m.cursor >= m.top+h {
		m.top = m.cursor - h + 1
	}
	m.top = max(min(m.top, len(m.rows)-h), 0)
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
