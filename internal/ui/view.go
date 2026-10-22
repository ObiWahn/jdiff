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

// Colors follow the ObiDesert delta theme: changes in the terminal's red and
// green without background, emphasized parts bold in the bright variants.
// Whitespace has no foreground to color, so changed indentation gets a dark
// background instead.
var (
	minusStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	minusEmphStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	minusSpace     = lipgloss.NewStyle().Background(lipgloss.Color("52"))
	plusStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	plusEmphStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	plusSpace      = lipgloss.NewStyle().Background(lipgloss.Color("22"))
	plainStyle     = lipgloss.NewStyle()

	decorationStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#434C5E"))
	fillerStyle     = decorationStyle
	cursorStyle     = lipgloss.NewStyle().Reverse(true).Bold(true)
	anchorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	pendingStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)

	titleStyle       = lipgloss.NewStyle().Faint(true)
	activeTitleStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	statusStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
)

// sideStyles returns the change, emphasis and changed-whitespace styles of s.
func sideStyles(s side) (base, emph, space lipgloss.Style) {
	if s == left {
		return minusStyle, minusEmphStyle, minusSpace
	}
	return plusStyle, plusEmphStyle, plusSpace
}

func (m *Model) help() string {
	h := "tab pane · enter mark/anchor · n/N change · h/l scroll · esc cancel · "
	if m.canBack {
		h += "backspace files · "
	}
	return h + "q quit"
}

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	paneWidth := (m.width - 1) / 2
	numWidth := len(strconv.Itoa(max(len(m.lines[left]), len(m.lines[right]), 1)))
	sep := decorationStyle.Render("│")

	var b strings.Builder
	b.WriteString(m.title(left, paneWidth) + sep + m.title(right, paneWidth) + "\n")
	for i := m.top; i < m.top+m.bodyHeight(); i++ {
		if i < len(m.rows) {
			b.WriteString(m.cell(left, i, paneWidth, numWidth) + sep + m.cell(right, i, paneWidth, numWidth))
		}
		b.WriteString("\n")
	}
	b.WriteString(m.status())
	return b.String()
}

func (m *Model) title(s side, width int) string {
	style := titleStyle
	if s == m.active {
		style = activeTitleStyle
	}
	return pad(style.Render(ansi.Truncate(" "+m.names[s], width, "…")), width, plainStyle)
}

func (m *Model) status() string {
	left := m.msg
	if left == "" {
		left = m.help()
	}
	right := fmt.Sprintf(" anchors %d · row %d/%d", len(m.anchors), m.cursor+1, len(m.rows))
	left = ansi.Truncate(left, max(m.width-ansi.StringWidth(right), 0), "…")
	gap := max(m.width-ansi.StringWidth(left)-ansi.StringWidth(right), 0)
	return ansi.Truncate(statusStyle.Render(left+strings.Repeat(" ", gap)+right), m.width, "")
}

// cell renders side s of row r: marker, line number, separator and the line
// content.
func (m *Model) cell(s side, r, width, numWidth int) string {
	row := m.rows[r]
	line := m.lineAt(s, r)
	changed := row.Kind != diff.Equal && line >= 0
	base, emph, space := sideStyles(s)

	marker := " "
	switch {
	case m.pending != nil && m.pending.side == s && m.pending.line == line && line >= 0:
		marker = pendingStyle.Render("●")
	case row.Anchored:
		marker = anchorStyle.Render("◆")
	case changed && s == left:
		marker = base.Render("-")
	case changed:
		marker = base.Render("+")
	}

	num := strings.Repeat(" ", numWidth)
	if line >= 0 {
		num = fmt.Sprintf("%*d", numWidth, line+1)
	}
	switch {
	case r == m.cursor && s == m.active:
		num = cursorStyle.Render(num)
	case changed:
		num = base.Render(num)
	}
	gutter := marker + num + base.Render("│") + " "

	contentWidth := max(width-numWidth-3, 0)
	if line < 0 {
		return gutter + fillerStyle.Render(strings.Repeat("╱", contentWidth))
	}

	var spans []diff.Span
	switch {
	case !changed:
		base, emph, space = plainStyle, plainStyle, plainStyle
	case s == left:
		spans = row.LeftEmph
	default:
		spans = row.RightEmph
	}
	content := highlight(m.lines[s][line], spans, base, emph, space)
	content = ansi.Cut(content, m.xoff, m.xoff+contentWidth)
	return gutter + pad(content, contentWidth, plainStyle)
}

// highlight renders text in base style with the given spans in emph style,
// whitespace inside spans in space style. Tabs are expanded and control
// characters made visible.
func highlight(text string, spans []diff.Span, base, emph, space lipgloss.Style) string {
	var b strings.Builder
	col := 0
	render := func(part string, style lipgloss.Style) {
		if part == "" {
			return
		}
		var p strings.Builder
		for _, r := range part {
			switch {
			case r == '\t':
				n := tabWidth - col%tabWidth
				p.WriteString(strings.Repeat(" ", n))
				col += n
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
	// renderEmph splits an emphasized part into whitespace and other runs.
	renderEmph := func(part string) {
		for part != "" {
			r, _ := utf8.DecodeRuneInString(part)
			ws := unicode.IsSpace(r)
			i := strings.IndexFunc(part, func(r rune) bool { return unicode.IsSpace(r) != ws })
			if i < 0 {
				i = len(part)
			}
			if ws {
				render(part[:i], space)
			} else {
				render(part[:i], emph)
			}
			part = part[i:]
		}
	}
	pos := 0
	for _, sp := range spans {
		render(text[pos:sp.Start], base)
		renderEmph(text[sp.Start:sp.End])
		pos = sp.End
	}
	render(text[pos:], base)
	return b.String()
}

// pad fills s with spaces in style up to width columns.
func pad(s string, width int, style lipgloss.Style) string {
	if n := width - ansi.StringWidth(s); n > 0 {
		return s + style.Render(strings.Repeat(" ", n))
	}
	return s
}
