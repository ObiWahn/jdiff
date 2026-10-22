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
	t := m.theme
	if s == left {
		return t.Minus, t.MinusNonEmph, t.MinusEmph, t.MinusIndent
	}
	return t.Plus, t.PlusNonEmph, t.PlusEmph, t.PlusIndent
}

func (m *Model) help() string {
	h := "tab pane · enter mark/anchor · space fold · ]/[ change · h/l scroll · esc cancel · "
	if m.canBack {
		h += "n/p file · backspace files · "
	}
	return h + "q quit"
}

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	paneWidth := (m.width - 1) / 2
	numWidth := len(strconv.Itoa(max(len(m.lines[left]), len(m.lines[right]), 1)))
	sep := m.theme.Decoration.Render("│")

	var b strings.Builder
	b.WriteString(m.title(left, paneWidth) + sep + m.title(right, paneWidth) + "\n")
	for i := m.top; i < m.top+m.bodyHeight(); i++ {
		switch {
		case i >= len(m.view):
		case m.view[i].hidden > 0:
			b.WriteString(m.fold(i))
		default:
			b.WriteString(m.cell(left, i, paneWidth, numWidth) + sep + m.cell(right, i, paneWidth, numWidth))
		}
		b.WriteString("\n")
	}
	b.WriteString(m.status())
	return b.String()
}

func (m *Model) title(s side, width int) string {
	style := m.theme.Title
	if s != m.active {
		style = style.Faint(true).Underline(false)
	}
	return pad(style.Render(ansi.Truncate(" "+m.names[s], width, "…")), width, lipgloss.NewStyle())
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
	left = ansi.Truncate(left, max(m.width-ansi.StringWidth(right), 0), "…")
	gap := max(m.width-ansi.StringWidth(left)-ansi.StringWidth(right), 0)
	return ansi.Truncate(m.theme.Hint.Render(left+strings.Repeat(" ", gap)+right), m.width, "")
}

// fold renders displayed row v, a fold of unchanged rows, across both panes.
func (m *Model) fold(v int) string {
	label := fmt.Sprintf(" ⋯ %d unchanged lines ⋯ ", m.view[v].hidden)
	if v == m.cursor {
		label = m.theme.Cursor.Render(label)
	}
	side := max(m.width-ansi.StringWidth(label), 0)
	line := m.theme.Decoration.Render(strings.Repeat("─", side/2)) + label +
		m.theme.Decoration.Render(strings.Repeat("─", side-side/2))
	return ansi.Truncate(line, m.width, "")
}

// cell renders side s of displayed row v: marker, line number, separator and
// the line content.
func (m *Model) cell(s side, v, width, numWidth int) string {
	t := m.theme
	row := m.rows[m.view[v].row]
	line := m.lineAt(s, v)
	changed := row.Kind != diff.Equal && line >= 0
	lineStyle, nonEmph, emph, indent := m.sideStyles(s)
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
	case v == m.cursor && s == m.active:
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

	st := lineStyles{base: t.Zero, ws: t.Whitespace, showAll: m.settings.ShowWhitespaces}
	var spans []diff.Span
	switch {
	case !changed:
	case row.Kind != diff.Change:
		st.base = lineStyle
	case s == left:
		st.base, st.emph, st.space, spans = nonEmph, emph, indent, row.LeftEmph
	default:
		st.base, st.emph, st.space, spans = nonEmph, emph, indent, row.RightEmph
	}
	content := highlight(m.lines[s][line], spans, st)
	content = ansi.Cut(content, m.xoff, m.xoff+contentWidth)
	return gutter + pad(content, contentWidth, lipgloss.NewStyle())
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

// pad fills s with spaces in style up to width columns.
func pad(s string, width int, style lipgloss.Style) string {
	if n := width - ansi.StringWidth(s); n > 0 {
		return s + style.Render(strings.Repeat(" ", n))
	}
	return s
}
