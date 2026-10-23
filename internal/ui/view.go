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
	"jdiff/internal/theme"
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
	h := "tab pane · enter mark/anchor · space fold · ctrl+space all · ]/[ change · s layout · h/l scroll · esc cancel · "
	if m.commitNav {
		h += "N/P commit · "
	}
	if m.canBack {
		h += "n/p file · f flow · backspace back · "
	}
	return h + "q quit"
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
	lineStyle, _, _, _ := m.sideStyles(s)

	marker := " "
	switch {
	case row.Anchored:
		marker = t.Anchor.Render("◆")
	case changed && s == left:
		marker = lineStyle.Render("-")
	case changed:
		marker = lineStyle.Render("+")
	}
	number := func(line int, of side) string {
		if line < 0 || (changed && of != s) {
			return strings.Repeat(" ", nw)
		}
		return fmt.Sprintf("%*d", nw, line+1)
	}
	nums := number(row.Left, left) + " " + number(row.Right, right)
	numStyle := t.NumZero
	switch {
	case cursor:
		numStyle = t.Cursor
	case changed && s == left:
		numStyle = t.NumMinus
	case changed:
		numStyle = t.NumPlus
	}
	gutter := marker + numStyle.Render(nums) + t.Decoration.Render("│") + " "
	line := row.Right
	if s == left {
		line = row.Left
	}
	return gutter + m.content(s, row, line, max(m.width-2*nw-4, 0))
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

// paneTitles renders names above the panes of a view width columns wide,
// the name of the active pane highlighted.
func paneTitles(t *theme.Theme, names [2]string, active side, width int) string {
	w := (width - 1) / 2
	title := func(s side) string {
		style := t.Title
		if s != active {
			style = style.Faint(true).Underline(false)
		}
		return pad(style.Render(ansi.Truncate(" "+names[s], w, "…")), w, lipgloss.NewStyle())
	}
	return title(left) + t.Decoration.Render("│") + title(right)
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

// statusLine renders the status bar with left and right aligned parts; left
// is shortened when both do not fit.
func statusLine(t *theme.Theme, width int, left, right string) string {
	left = ansi.Truncate(left, max(width-ansi.StringWidth(right), 0), "…")
	gap := max(width-ansi.StringWidth(left)-ansi.StringWidth(right), 0)
	return ansi.Truncate(t.Hint.Render(left+strings.Repeat(" ", gap)+right), width, "")
}

// fold renders displayed row v, a fold of unchanged rows, across both panes.
func (m *Model) fold(v int, cursor bool) string {
	label := fmt.Sprintf(" ⋯ %d unchanged lines ⋯ ", m.view[v].hidden)
	if cursor {
		label = m.cfg.Theme.Cursor.Render(label)
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
	lineStyle, _, _, _ := m.sideStyles(s)
	numStyle, sepStyle := t.NumMinus, t.NumLeft
	if s == right {
		numStyle, sepStyle = t.NumPlus, t.NumRight
	}

	marker := " "
	switch {
	case m.pending != nil && m.pending.side == s && m.pending.line == line && line >= 0:
		marker = t.Pending.Render("●")
	case row.Anchored:
		marker = t.Anchor.Render("◆")
	case changed && s == left:
		marker = lineStyle.Render("-")
	case changed:
		marker = lineStyle.Render("+")
	}

	num := strings.Repeat(" ", numWidth)
	if line >= 0 {
		num = fmt.Sprintf("%*d", numWidth, line+1)
	}
	switch {
	case cursor && s == m.active:
		num = t.Cursor.Render(num)
	case changed:
		num = numStyle.Render(num)
	default:
		num = t.NumZero.Render(num)
	}
	gutter := marker + num + sepStyle.Render("│") + " "

	contentWidth := max(width-numWidth-3, 0)
	if line < 0 {
		return gutter + t.Filler.Render(strings.Repeat("╱", contentWidth))
	}

	return gutter + m.content(s, row, line, contentWidth)
}

// content renders line of side s, shown in row, width columns wide with
// changes emphasized and scrolled by the horizontal offset.
func (m *Model) content(s side, row diff.Row, line, width int) string {
	t := m.cfg.Theme
	lineStyle, nonEmph, emph, indent := m.sideStyles(s)
	st := lineStyles{base: t.Zero, ws: t.Whitespace, showAll: m.cfg.Settings.ShowWhitespaces}
	var spans []diff.Span
	switch {
	case row.Kind == diff.Equal:
	case row.Kind != diff.Change:
		st.base = lineStyle
	case s == left:
		st.base, st.emph, st.space, spans = nonEmph, emph, indent, row.LeftEmph
	default:
		st.base, st.emph, st.space, spans = nonEmph, emph, indent, row.RightEmph
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

// minBoxHeight fits the borders and one line.
const minBoxHeight = 3

// splitList divides a screen height high between a heading, an empty line,
// a list of n items, the box below it when shown and the status bar. The
// list takes at most half of the room and the box, after an empty line, the
// rest; box is 0 when hidden, for an empty list or when the screen is too
// small for it.
func splitList(height, n int, showBox bool) (list, box int) {
	room := max(height-3, 1)
	if !showBox || n == 0 {
		return room, 0
	}
	list = max(min(n, room/2), 1)
	box = room - list - 1
	if box < minBoxHeight {
		return room, 0
	}
	return list, box
}

// box renders lines in a rounded box width columns wide and height lines
// high, with title in the top border. Lines that do not fit are cut.
func box(t *theme.Theme, width, height int, title string, lines []string) string {
	border := t.Decoration
	inner := max(width-4, 0) // border and a space on each side
	title = ansi.Truncate(title, inner, "")
	var b strings.Builder
	b.WriteString(border.Render("╭─") + title +
		border.Render(strings.Repeat("─", max(width-3-ansi.StringWidth(title), 0))+"╮") + "\n")
	for i := range height - 2 {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], inner, "…")
		}
		b.WriteString(border.Render("│") + " " + pad(line, inner, lipgloss.NewStyle()) + " " + border.Render("│") + "\n")
	}
	b.WriteString(border.Render("╰"+strings.Repeat("─", max(width-2, 0))+"╯") + "\n")
	return b.String()
}

// pad fills s with spaces in style up to width columns.
func pad(s string, width int, style lipgloss.Style) string {
	if n := width - ansi.StringWidth(s); n > 0 {
		return s + style.Render(strings.Repeat(" ", n))
	}
	return s
}
