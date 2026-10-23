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
func (d *fileDiff) sideStyles(s side) (line, nonEmph, emph, indent lipgloss.Style) {
	t := d.cfg.Theme
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

func (d *fileDiff) numWidth() int {
	return len(strconv.Itoa(max(len(d.lines[left]), len(d.lines[right]), 1)))
}

// header renders the file names above both panes.
func (m *Model) header() string { return paneTitles(m.cfg.Theme, m.names, m.active, m.width) }

// screenLine renders screen line s in the current layout, marked as the
// cursor line when cursor is set.
func (m *Model) screenLine(s sline, cursor bool) string {
	return m.render(s, cursor, layout{m.width, m.xoff, m.active, m.screenUnified})
}

// render renders screen line s in layout lo, marked as the cursor line when
// cursor is set.
func (d *fileDiff) render(s sline, cursor bool, lo layout) string {
	if lo.unified {
		return d.unifiedLine(s.v, s.sub, cursor, lo)
	}
	return d.bodyRow(s.v, cursor, lo)
}

// unifiedLine renders line sub of displayed row v in the normal layout:
// marker, old and new line number, separator and the content. Unchanged
// lines show both numbers, changed ones the number of their side.
func (d *fileDiff) unifiedLine(v, sub int, cursor bool, lo layout) string {
	if d.view[v].hidden > 0 {
		return d.fold(v, cursor, lo.width)
	}
	t := d.cfg.Theme
	row := d.rows[d.view[v].row]
	s := right
	if row.Kind == diff.Delete || (row.Kind == diff.Change && sub == 0) {
		s = left
	}
	nw := d.numWidth()
	changed := row.Kind != diff.Equal
	number := func(line int, of side) string {
		if line < 0 || (changed && of != s) {
			return strings.Repeat(" ", nw)
		}
		return fmt.Sprintf("%*d", nw, line+1)
	}
	nums := number(row.Left, left) + " " + number(row.Right, right)
	pending := d.isPending(row) && (row.Kind == diff.Equal || d.pending.side == s)
	line := lineOf(row, s)
	_, moved := d.movedTo(s, line)
	gutter := d.gutter(row, s, changed, pending, moved, nums, t.Decoration, cursor)
	return gutter + d.content(s, row, line, max(lo.width-2*nw-4, 0), cursor, moved, lo.xoff)
}

// bodyRow renders displayed row v side by side, marked as the cursor row
// when cursor is set.
func (d *fileDiff) bodyRow(v int, cursor bool, lo layout) string {
	if d.view[v].hidden > 0 {
		return d.fold(v, cursor, lo.width)
	}
	w, nw := (lo.width-1)/2, d.numWidth()
	return d.cell(left, v, w, nw, cursor, lo) + d.cfg.Theme.Decoration.Render("│") + d.cell(right, v, w, nw, cursor, lo)
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

// fold renders displayed row v, a fold of unchanged rows, across both panes
// of a view width columns wide.
func (d *fileDiff) fold(v int, cursor bool, width int) string {
	label := fmt.Sprintf(" ⋯ %d unchanged lines ⋯ ", d.view[v].hidden)
	if cursor {
		label = d.cfg.Theme.Cursor.Render(label)
	}
	side := max(width-ansi.StringWidth(label), 0)
	line := d.cfg.Theme.Decoration.Render(strings.Repeat("─", side/2)) + label +
		d.cfg.Theme.Decoration.Render(strings.Repeat("─", side-side/2))
	return ansi.Truncate(line, width, "")
}

// cell renders side s of displayed row v: marker, line number, separator and
// the line content. The cursor shows on the line number of the active side.
func (d *fileDiff) cell(s side, v, width, numWidth int, cursor bool, lo layout) string {
	t := d.cfg.Theme
	row := d.rows[d.view[v].row]
	line := d.lineAt(s, v)
	changed := row.Kind != diff.Equal && line >= 0
	sepStyle := t.NumLeft
	if s == right {
		sepStyle = t.NumRight
	}
	// The cursor shows on the active side.
	cursor = cursor && s == lo.active

	num := strings.Repeat(" ", numWidth)
	if line >= 0 {
		num = fmt.Sprintf("%*d", numWidth, line+1)
	}
	pending := d.pending != nil && d.pending.side == s && d.pending.line == line && line >= 0
	_, moved := d.movedTo(s, line)
	gutter := d.gutter(row, s, changed, pending, moved, num, sepStyle, cursor)

	contentWidth := max(width-numWidth-3, 0)
	if line < 0 {
		return gutter + t.Filler.Render(strings.Repeat("╱", contentWidth))
	}

	return gutter + d.content(s, row, line, contentWidth, cursor, moved, lo.xoff)
}

// gutter renders what precedes a line of side s in row: the marker, the line
// numbers nums and the separator in sepStyle. The marker is ● when pending
// holds, ◆ on anchors and - or + on changed lines, in the moved style for
// moved ones; changed lines also have their side's number style.
func (d *fileDiff) gutter(row diff.Row, s side, changed, pending, moved bool, nums string, sepStyle lipgloss.Style,
	cursor bool) string {
	t := d.cfg.Theme
	lineStyle, _, _, _ := d.sideStyles(s)
	if moved {
		lineStyle = d.movedStyle(s)
	}
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
	if cursor {
		// The cursor's line numbers stand out in the cursor style, in
		// their own color.
		numStyle = t.Cursor.Inherit(numStyle)
	}
	return marker + numStyle.Render(nums) + sepStyle.Render("│") + " "
}

// boldIf returns style, bold when on is set.
func boldIf(style lipgloss.Style, on bool) lipgloss.Style {
	if on {
		return style.Bold(true)
	}
	return style
}

// movedStyle returns the style of moved lines of side s.
func (d *fileDiff) movedStyle(s side) lipgloss.Style {
	if s == left {
		return d.cfg.Theme.MinusMoved
	}
	return d.cfg.Theme.PlusMoved
}

// content renders line of side s, shown in row, width columns wide with
// changes emphasized and scrolled by xoff columns. On the cursor line
// unchanged text is bold; a moved line is drawn in the moved style, without
// emphasis.
func (d *fileDiff) content(s side, row diff.Row, line, width int, cursor, moved bool, xoff int) string {
	t := d.cfg.Theme
	lineStyle, nonEmph, emph, indent := d.sideStyles(s)
	st := lineStyles{base: boldIf(t.Zero, cursor && row.Kind == diff.Equal), ws: t.Whitespace,
		showAll: d.cfg.Settings.ShowWhitespaces}
	var spans []diff.Span
	switch {
	case row.Kind == diff.Equal:
	case moved:
		st.base = d.movedStyle(s)
	case row.Kind == diff.Change:
		st.base, st.emph, st.space, spans = nonEmph, emph, indent, emphOf(row, s)
	default:
		st.base = lineStyle
	}
	content := highlight(d.lines[s][line], spans, st)
	content = ansi.Cut(content, xoff, xoff+width)
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
