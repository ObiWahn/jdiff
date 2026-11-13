// Package popup draws the frame of a popup around its content: its name at
// the left and keys at the right of the top border, more keys in the bottom
// border, as
//
//	╭─ Tags ─ ↵ toggle · q close ─╮
//	│ ▶ urgent↑                   │
//	│   billing↓                  │
//	╰─── ctrl+↑↓ move · / search ─╯
//
// A message (Frame.Msg) takes the place of the bottom keys. It only draws:
// the keys it shows are those the caller handles. Draw the result over the
// screen with the overlay package.
package popup

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	// FrameW is the width the frame adds to its content: the border and a
	// space on either side.
	FrameW = 4
	// FrameH is the height it adds: the top and bottom border.
	FrameH = 2
	// hintSep stands between two keys of a border.
	hintSep = " · "
	// minDash is the least border line beside a border text.
	minDash = 1
)

// Styles colour the frame; the content keeps its own colours.
type Styles struct {
	Border lipgloss.Style // the border lines (only their foreground is used)
	Title  lipgloss.Style // the popup's name
	Hint   lipgloss.Style // the keys in the borders
	Msg    lipgloss.Style // the message in the bottom border
}

// DefaultStyles are a blue border and keys, a yellow name;
// a message is bold red.
func DefaultStyles() Styles {
	return Styles{
		Border: lipgloss.NewStyle().Foreground(lipgloss.Blue),
		Title:  lipgloss.NewStyle().Foreground(lipgloss.Yellow),
		Hint:   lipgloss.NewStyle().Foreground(lipgloss.Blue),
		Msg:    lipgloss.NewStyle().Foreground(lipgloss.Red).Bold(true),
	}
}

// Frame is a popup's frame. Top and Bottom are the keys shown at the right
// of the top border, the title at its left, and at the right of the bottom
// border; disabled
// bindings and those without help are left out. Msg, when set, shows in
// place of the bottom keys (a conflict, "saving…"); it is drawn as
// given, so the caller makes it printable.
type Frame struct {
	Title  string
	Top    []key.Binding
	Bottom []key.Binding
	Msg    string
	Styles Styles
}

// bottom returns the bottom border's text and its style: Msg, or without
// one the bottom keys.
func (f Frame) bottom() (string, lipgloss.Style) {
	if f.Msg != "" {
		return f.Msg, f.Styles.Msg
	}
	return Hints(f.Bottom), f.Styles.Hint
}

// Hints returns the keys of bs as a border shows them: "key what", joined by
// " · "; a key without words stands alone, as a position (3/10) does.
func Hints(bs []key.Binding) string {
	var parts []string
	for _, b := range bs {
		h := b.Help()
		if !b.Enabled() || h.Key == "" {
			continue
		}
		if h.Desc == "" {
			parts = append(parts, h.Key)
			continue
		}
		parts = append(parts, h.Key+" "+h.Desc)
	}
	return strings.Join(parts, hintSep)
}

// Width returns the width the frame takes around body, at most maxWidth:
// as wide as the widest line of body or a border text needs, the message
// too.
func (f Frame) Width(body string, maxWidth int) int {
	bottom, _ := f.bottom()
	w := max(topWidth(f.Title, Hints(f.Top)), ansi.StringWidth(bottomLabel(bottom))) + 2*minDash + 2
	for _, line := range strings.Split(body, "\n") {
		w = max(w, ansi.StringWidth(line)+FrameW)
	}
	return max(min(w, maxWidth), FrameW)
}

// topWidth is the width of the top border's texts, " title " and " keys ",
// with the least border line between them; 0 without either.
func topWidth(title, hints string) int {
	w := 0
	if title != "" {
		w += ansi.StringWidth(title) + 2
	}
	if hints != "" {
		w += ansi.StringWidth(hints) + 2
	}
	if title != "" && hints != "" {
		w += minDash
	}
	return w
}

// bottomLabel is the bottom border's text: " text ", or "" without one.
func bottomLabel(text string) string {
	if text == "" {
		return ""
	}
	return " " + text + " "
}

// fitTop cuts the keys, then the title, until the top border's texts are
// at most room wide.
func fitTop(title, hints string, room int) (string, string) {
	if topWidth(title, hints) <= room {
		return title, hints
	}
	if hints != "" {
		keysRoom := room - topWidth(title, "") - minDash - 2 // " keys " after the title's line
		if title == "" {
			keysRoom = room - 2
		}
		if keysRoom > 1 {
			return title, ansi.Truncate(hints, keysRoom, "…")
		}
	}
	return ansi.Truncate(title, max(room-2, 0), "…"), ""
}

// Render draws body in the frame, at most maxWidth wide (Width); lines and
// border texts that do not fit are cut with "…".
func (f Frame) Render(body string, maxWidth int) string {
	width := f.Width(body, maxWidth)
	inner := width - FrameW
	line := width - 2 // the border line between the corners
	room := max(line-2*minDash, 0)
	b := f.Styles.Border
	var out strings.Builder

	// Top: ╭─ title ─── keys ─╮, the free line between them.
	title, hints := fitTop(f.Title, Hints(f.Top), room)
	rest := max(room-topWidth(title, hints), 0)
	out.WriteString(b.Render("╭" + strings.Repeat("─", minDash)))
	if title != "" {
		out.WriteString(" " + f.Styles.Title.Render(title) + " ")
	}
	if hints == "" {
		out.WriteString(b.Render(strings.Repeat("─", rest+minDash)+"╮") + "\n")
	} else {
		gap := rest
		if title != "" {
			gap += minDash
		}
		if gap > 0 {
			out.WriteString(b.Render(strings.Repeat("─", gap)))
		}
		out.WriteString(" " + f.Styles.Hint.Render(hints) + " " + b.Render(strings.Repeat("─", minDash)+"╮") + "\n")
	}

	for _, l := range strings.Split(body, "\n") {
		l = ansi.Truncate(l, inner, "…")
		out.WriteString(b.Render("│") + " " + l + strings.Repeat(" ", inner-ansi.StringWidth(l)) + " " + b.Render("│") + "\n")
	}

	// Bottom: ╰─── keys ─╯, or the message in place of the keys.
	bottom, style := f.bottom()
	if ansi.StringWidth(bottomLabel(bottom)) > room {
		bottom = ansi.Truncate(bottom, max(room-2, 0), "…")
	}
	if bottom == "" {
		out.WriteString(b.Render("╰" + strings.Repeat("─", line) + "╯"))
		return out.String()
	}
	out.WriteString(b.Render("╰"+strings.Repeat("─", max(line-minDash-ansi.StringWidth(bottomLabel(bottom)), 0))) +
		" " + style.Render(bottom) + " " + b.Render(strings.Repeat("─", minDash)+"╯"))
	return out.String()
}
