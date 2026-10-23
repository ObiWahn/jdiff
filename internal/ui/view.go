package ui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/diff"
)

const tabWidth = 4

// lineStyles are the styles of one rendered line.
type lineStyles struct {
	base  lipgloss.Style // text outside emphasized spans
	emph  lipgloss.Style // changed words
	space lipgloss.Style // changed whitespace, drawn as · and →
	ws    lipgloss.Style // other whitespace, drawn as · and → when showAll
	// showAll draws all whitespace visibly, not only changed whitespace.
	showAll bool
}

// sideStyles returns the styles of side s for whole-line changes, the
// unchanged parts of changed lines, changed words and changed whitespace.
func (m *Model) sideStyles(s side) (line, nonEmph, emph, indent lipgloss.Style) {
	t := m.cfg.Theme
	if s == left {
		return t.Minus, t.MinusNonEmph, t.MinusEmph, t.MinusIndent
	}
	return t.Plus, t.PlusNonEmph, t.PlusEmph, t.PlusIndent
}

func (m *Model) help() string {
	h := "enter mark/anchor · space fold · ]/[ change · "
	if m.canBack {
		h += "n/p file · "
	}
	return h + "? help · q quit"
}

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.header() + "\n")
	m.syncLayout()
	pos := m.pos()
	for i := m.top; i < m.top+m.bodyHeight(); i++ {
		if i < len(m.screen) {
			b.WriteString(m.screenLine(m.screen[i], i == pos))
		}
		b.WriteString("\n")
	}
	b.WriteString(m.status())
	return b.String()
}

func (m *Model) paneWidth() int { return (m.width - 1) / 2 }

func (m *Model) numWidth() int {
	return len(strconv.Itoa(max(len(m.lines[left]), len(m.lines[right]), 1)))
}

// header renders the file names above both panes.
func (m *Model) header() string { return paneTitles(m.cfg.Theme, m.names, m.active, m.width) }

// screenLine renders screen line s in the current layout, marked as the
// cursor line when cursor is set.
func (m *Model) screenLine(s sline, cursor bool) string {
	if m.screenUnified {
		return m.unifiedLine(s.v, s.sub, cursor)
	}
	return m.bodyRow(s.v, cursor)
}

// unifiedLine renders line sub of displayed row v in the normal layout:
// marker, old and new line number, separator and the content. Unchanged
// lines show both numbers, changed ones the number of their side.
func (m *Model) unifiedLine(v, sub int, cursor bool) string {
	if m.view[v].hidden > 0 {
		return m.fold(v, cursor)
	}
	t := m.cfg.Theme
	row := m.rows[m.view[v].row]
	s := right
	if row.Kind == diff.Delete || (row.Kind == diff.Change && sub == 0) {
		s = left
	}
	nw := m.numWidth()
	changed := row.Kind != diff.Equal
	number := func(line int, of side) string {
		if line < 0 || (changed && of != s) {
			return strings.Repeat(" ", nw)
		}
		return fmt.Sprintf("%*d", nw, line+1)
	}
	nums := number(row.Left, left) + " " + number(row.Right, right)
	pending := m.isPending(row) && (row.Kind == diff.Equal || m.pending.side == s)
	gutter := m.gutter(row, s, changed, pending, nums, t.Decoration, cursor)
	return gutter + m.content(s, row, lineOf(row, s), max(m.width-2*nw-4, 0), cursor)
}

// bodyRow renders displayed row v side by side, marked as the cursor row
// when cursor is set.
func (m *Model) bodyRow(v int, cursor bool) string {
	if m.view[v].hidden > 0 {
		return m.fold(v, cursor)
	}
	w, nw := m.paneWidth(), m.numWidth()
	return m.cell(left, v, w, nw, cursor) + m.cfg.Theme.Decoration.Render("│") + m.cell(right, v, w, nw, cursor)
}

func (m *Model) status() string {
	left := m.msg
	if left == "" {
		left = m.help()
	}
	row := 0
	if m.cursor < len(m.view) {
		row = m.view[m.cursor].row + 1
	}
	right := fmt.Sprintf(" anchors %d · row %d/%d", len(m.anchors), row, len(m.rows))
	return statusLine(m.cfg.Theme, m.width, left, right)
}

// fold renders displayed row v, a fold of unchanged rows, across both panes.
func (m *Model) fold(v int, cursor bool) string {
	label := fmt.Sprintf(" ⋯ %d unchanged lines ⋯ ", m.view[v].hidden)
	if cursor {
		label = lipgloss.NewStyle().Bold(true).Render(label)
	}
	side := max(m.width-ansi.StringWidth(label), 0)
	line := m.cfg.Theme.Decoration.Render(strings.Repeat("─", side/2)) + label +
		m.cfg.Theme.Decoration.Render(strings.Repeat("─", side-side/2))
	return ansi.Truncate(line, m.width, "")
}

// cell renders side s of displayed row v: marker, line number, separator and
// the line content. The cursor shows on the line number of the active side.
func (m *Model) cell(s side, v, width, numWidth int, cursor bool) string {
	t := m.cfg.Theme
	row := m.rows[m.view[v].row]
	line := m.lineAt(s, v)
	changed := row.Kind != diff.Equal && line >= 0
	sepStyle := t.NumLeft
	if s == right {
		sepStyle = t.NumRight
	}
	// The cursor shows on the active side.
	cursor = cursor && s == m.active

	num := strings.Repeat(" ", numWidth)
	if line >= 0 {
		num = fmt.Sprintf("%*d", numWidth, line+1)
	}
	pending := m.pending != nil && m.pending.side == s && m.pending.line == line && line >= 0
	gutter := m.gutter(row, s, changed, pending, num, sepStyle, cursor)

	contentWidth := max(width-numWidth-3, 0)
	if line < 0 {
		return gutter + t.Filler.Render(strings.Repeat("╱", contentWidth))
	}

	return gutter + m.content(s, row, line, contentWidth, cursor)
}

// gutter renders what precedes a line of side s in row: the marker, the line
// numbers nums and the separator in sepStyle. The marker is ● when pending
// holds, ◆ on anchors and - or + on changed lines, which also have their
// side's number style.
func (m *Model) gutter(row diff.Row, s side, changed, pending bool, nums string, sepStyle lipgloss.Style, cursor bool) string {
	t := m.cfg.Theme
	lineStyle, _, _, _ := m.sideStyles(s)
	numStyle := t.NumZero
	marker := " "
	switch {
	case pending:
		marker = t.Pending.Render("●")
	case row.Anchored:
		marker = boldIf(t.Anchor, cursor).Render("◆")
	case changed && s == left:
		marker = boldIf(lineStyle, cursor).Render("-")
	case changed:
		marker = boldIf(lineStyle, cursor).Render("+")
	}
	if changed {
		numStyle = t.NumPlus
		if s == left {
			numStyle = t.NumMinus
		}
	}
	return marker + boldIf(numStyle, cursor).Render(nums) + sepStyle.Render("│") + " "
}

// boldIf returns style, bold when on is set.
func boldIf(style lipgloss.Style, on bool) lipgloss.Style {
	if on {
		return style.Bold(true)
	}
	return style
}

// content renders line of side s, shown in row, width columns wide with
// changes emphasized and scrolled by the horizontal offset. On the cursor
// line unchanged text is bold.
func (m *Model) content(s side, row diff.Row, line, width int, cursor bool) string {
	t := m.cfg.Theme
	lineStyle, nonEmph, emph, indent := m.sideStyles(s)
	st := lineStyles{base: boldIf(t.Zero, cursor && row.Kind == diff.Equal), ws: t.Whitespace,
		showAll: m.cfg.Settings.ShowWhitespaces}
	var spans []diff.Span
	switch row.Kind {
	case diff.Equal:
	case diff.Change:
		st.base, st.emph, st.space, spans = nonEmph, emph, indent, emphOf(row, s)
	default:
		st.base = lineStyle
	}
	content := highlight(m.lines[s][line], spans, st)
	content = ansi.Cut(content, m.xoff, m.xoff+width)
	return pad(content, width, lipgloss.NewStyle())
}

// highlight renders text with the given spans emphasized. Whitespace inside
// spans, and with showAll all whitespace, is drawn visibly: · for a space, →
// for a tab. Tabs are expanded and control characters made visible.
func highlight(text string, spans []diff.Span, st lineStyles) string {
	var b strings.Builder
	col := 0
	render := func(part string, style lipgloss.Style, visible bool) {
		if part == "" {
			return
		}
		var p strings.Builder
		for _, r := range part {
			switch {
			case r == '\t':
				n := tabWidth - col%tabWidth
				if visible {
					p.WriteString("→" + strings.Repeat(" ", n-1))
				} else {
					p.WriteString(strings.Repeat(" ", n))
				}
				col += n
			case r == ' ' && visible:
				p.WriteRune('·')
				col++
			case r < 0x20 || r == 0x7f:
				p.WriteRune('·')
				col++
			default:
				p.WriteRune(r)
				col += ansi.StringWidth(string(r))
			}
		}
		b.WriteString(style.Render(p.String()))
	}
	// renderPart splits a part into whitespace and other runs.
	renderPart := func(part string, emph bool) {
		for part != "" {
			r, _ := utf8.DecodeRuneInString(part)
			ws := unicode.IsSpace(r)
			i := strings.IndexFunc(part, func(r rune) bool { return unicode.IsSpace(r) != ws })
			if i < 0 {
				i = len(part)
			}
			switch {
			case ws && emph:
				render(part[:i], st.space, true)
			case ws && st.showAll:
				render(part[:i], st.ws, true)
			case emph:
				render(part[:i], st.emph, false)
			default:
				render(part[:i], st.base, false)
			}
			part = part[i:]
		}
	}
	pos := 0
	for _, sp := range spans {
		renderPart(text[pos:sp.Start], false)
		renderPart(text[sp.Start:sp.End], true)
		pos = sp.End
	}
	renderPart(text[pos:], false)
	return b.String()
}
