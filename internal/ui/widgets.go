package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/theme"
)

// paneTitles renders names above the panes of a view width columns wide,
// the name of the active pane in bold, the other one faint.
func paneTitles(t *theme.Theme, names [2]string, active side, width int) string {
	w := (width - 1) / 2
	title := func(s side) string {
		style := lipgloss.NewStyle().Bold(true)
		if s != active {
			style = lipgloss.NewStyle().Faint(true)
		}
		return pad(style.Render(ansi.Truncate(" "+names[s], w, "…")), w, lipgloss.NewStyle())
	}
	return title(left) + t.Decoration.Render("│") + title(right)
}

// statusLine renders the status bar with left and right aligned parts; left
// is shortened when both do not fit.
func statusLine(t *theme.Theme, width int, left, right string) string {
	left = ansi.Truncate(left, max(width-ansi.StringWidth(right), 0), "…")
	gap := max(width-ansi.StringWidth(left)-ansi.StringWidth(right), 0)
	return ansi.Truncate(t.Hint.Render(left+strings.Repeat(" ", gap)+right), width, "")
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

// count formats n with sign right aligned to width, or blanks when n is 0.
func count(sign string, n, width int) string {
	if n == 0 {
		return strings.Repeat(" ", width)
	}
	return fmt.Sprintf("%*s", width, sign+fmt.Sprint(n))
}
