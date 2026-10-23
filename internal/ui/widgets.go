package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/theme"
)

// paneTitles renders names above the panes of a view width columns wide.
// Side by side the name of the active pane is bold, the other one faint; in
// the normal layout, one column of both, both are bold.
func paneTitles(t *theme.Theme, names [2]string, active side, sideBySide bool, width int) string {
	w := (width - 1) / 2
	title := func(s side) string {
		style := lipgloss.NewStyle().Bold(true)
		if sideBySide && s != active {
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

// scrollWindow returns the first shown item of a window h items high over n
// items: top, moved as little as needed to show cursor.
func scrollWindow(cursor, top, h, n int) int {
	if cursor < top {
		top = cursor
	} else if cursor >= top+h {
		top = cursor - h + 1
	}
	return max(min(top, n-h), 0)
}

// list is the state of the commit and the file list: n items below a
// heading and an empty line, the box below them while preview is set, and the
// status bar.
type list struct {
	cursor, top   int
	n             int
	preview       *bool // the setting that shows the box
	width, height int
}

// listHeight is the height of the list below the heading and an empty line.
func (li *list) listHeight() int {
	h, _ := splitList(li.height, li.n, *li.preview)
	return h
}

// boxHeight is the height of the box below the list, 0 when it is hidden.
func (li *list) boxHeight() int {
	_, h := splitList(li.height, li.n, *li.preview)
	return h
}

func (li *list) moveCursor(delta int) {
	li.cursor = max(min(li.cursor+delta, li.n-1), 0)
	li.scrollToCursor()
}

func (li *list) scrollToCursor() {
	li.top = scrollWindow(li.cursor, li.top, li.listHeight(), li.n)
}

// render renders the list: head, the shown items as drawn by item, which
// renders the selected one bold through st, the box as drawn by box when it
// is shown, and status.
func (li *list) render(t *theme.Theme, head string, item func(i int, st func(lipgloss.Style) lipgloss.Style) string,
	box func(height int) string, status string) string {
	var b strings.Builder
	b.WriteString(ansi.Truncate(head, li.width, "…") + "\n\n")
	for i := li.top; i < li.top+li.listHeight(); i++ {
		if i < li.n {
			st := func(s lipgloss.Style) lipgloss.Style { return s.Bold(i == li.cursor) }
			b.WriteString(ansi.Truncate(item(i, st), li.width, "…"))
		}
		b.WriteString("\n")
	}
	if h := li.boxHeight(); h > 0 && li.cursor < li.n {
		b.WriteString("\n" + box(h))
	}
	b.WriteString(ansi.Truncate(t.Hint.Render(status), li.width, ""))
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

// counts renders the added and removed line counts in columns addW and remW
// digits wide, in st of the plus and minus styles.
func counts(t *theme.Theme, st func(lipgloss.Style) lipgloss.Style, added, removed, addW, remW int) string {
	return st(t.Plus).Render(count("+", added, addW+1)) + " " + st(t.Minus).Render(count("-", removed, remW+1))
}
