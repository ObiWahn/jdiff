// Package overlay draws a popup over a rendered view: the view shows around
// the popup, which is clipped to the view's size.
package overlay

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Center returns base (width × height cells) with popup drawn over its
// middle.
func Center(base, popup string, width, height int) string {
	x := max(0, (width-lipgloss.Width(popup))/2)
	y := max(0, (height-lipgloss.Height(popup))/2)
	return At(base, popup, x, y, width, height)
}

// At returns base (width × height cells) with popup drawn at column x, row y:
// height rows of base, each cut to width, those base lacks empty. The popup
// is a rectangle as wide as its widest line, shorter lines filled with
// spaces, clipped to the view; a wide character of base that it cuts in half
// becomes a space, so that the columns stay. The rows are cut as strings,
// keeping the escape sequences of base and popup: several times faster than
// composing cells (lipgloss' Canvas), which matters for a full screen drawn
// on every key.
func At(base, popup string, x, y, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := strings.Split(base, "\n")
	out := make([]string, height)
	copy(out, rows)
	placed := make([]bool, height) // place cuts these rows itself
	lines := strings.Split(popup, "\n")
	widths := make([]int, len(lines)) // each line measured once
	pw := 0
	for i, l := range lines {
		widths[i] = ansi.StringWidth(l)
		pw = max(pw, widths[i])
	}
	for i, l := range lines {
		r := y + i
		if r < 0 || r >= height {
			continue
		}
		l += strings.Repeat(" ", pw-widths[i])
		px, lw := x, pw
		if px < 0 { // a wide character cut in half goes, so measure again
			l, px = ansi.TruncateLeft(l, -px, ""), 0
			lw = ansi.StringWidth(l)
		}
		if px >= width {
			continue
		}
		if px+lw > width {
			l = ansi.Truncate(l, width-px, "")
			lw = ansi.StringWidth(l)
		}
		out[r], placed[r] = place(out[r], l, px, lw, width), true
	}
	for i := range out {
		if !placed[i] {
			out[i] = ansi.Truncate(out[i], width, "")
		}
	}
	return strings.Join(out, "\n")
}

// place returns row, at most width wide, with line, lw columns wide, drawn
// over it from column x on; where line cuts a wide character of row in half,
// a space takes its place.
func place(row, line string, x, lw, width int) string {
	rowW := ansi.StringWidth(row)
	left := ansi.Truncate(row, x, "")
	left += strings.Repeat(" ", x-ansi.StringWidth(left))
	end := x + lw
	right := ""
	if want := min(rowW, width) - end; want > 0 {
		right = ansi.Cut(row, end, width)
		rw := ansi.StringWidth(right)
		if rw > want { // ansi keeps a wide character cut in half
			right = " " + ansi.Cut(row, end+1, width)
			rw = ansi.StringWidth(right)
		}
		right = strings.Repeat(" ", max(want-rw, 0)) + right
	}
	return left + line + right
}
