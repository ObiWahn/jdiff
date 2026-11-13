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
// screen with the overlay package. A Box is a frame whose keys come from a
// keymap (internal/keymap): the caller names the actions of each border.
package popup

import (
	"errors"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/keymap"
	"jdiff/internal/teakeys"

	"jdiff/internal/geom"
)

const (
	// FrameW is the width the frame adds to its content: the border and a
	// space on either side.
	FrameW = 4
	// FrameH is the height it adds: the top and bottom border; BlankTop and
	// BlankBottom add more (InnerSize, OuterSize).
	FrameH = 2
	// hintSep stands between two keys of a border.
	hintSep = " · "
	// minDash is the least border line beside a border text.
	minDash = 1
)

// Size is a width and a height: of a box, borders included, or of the area
// its content is painted in (geom.Size).
type Size = geom.Size

// ErrTooNarrow and ErrTooLow say which side of the content does not fit in
// the box RequestInnerSize gives.
var (
	ErrTooNarrow = errors.New("popup: the content is too wide for the box")
	ErrTooLow    = errors.New("popup: the content is too high for the box")
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
// given, so the caller makes it printable. BlankTop and BlankBottom are
// empty rows inside the border, above and below the content. BottomMsgRoom
// is the width the bottom border keeps for a message while none shows, so
// that a box sized without one (OuterSize, RequestInnerSize) neither widens
// nor cuts a message up to that width when it comes.
type Frame struct {
	Title                 string
	Top                   []key.Binding
	Bottom                []key.Binding
	Msg                   string
	Styles                Styles
	BlankTop, BlankBottom int
	BottomMsgRoom         int
}

// frameH is the height the frame adds to its content: the borders and the
// blank rows.
func (f Frame) frameH() int { return FrameH + max(f.BlankTop, 0) + max(f.BlankBottom, 0) }

// InnerSize returns the area a box of size box leaves its content, the area
// it is painted in, the room the content has (it may take less): InnerWidth
// × InnerHeight.
func (f Frame) InnerSize(box Size) Size { return Size{f.InnerWidth(box.W), f.InnerHeight(box.H)} }

// InnerWidth returns the columns a box width wide leaves its content: less
// the borders and the space beside each; at least 0.
func (f Frame) InnerWidth(width int) int { return max(width-FrameW, 0) }

// InnerHeight returns the rows a box height high leaves its content: less
// the borders and the blank rows; at least 0. A box too low for the blank
// rows and a row of content keeps the row: the blank rows give way, all of
// them at once.
func (f Frame) InnerHeight(height int) int {
	avail := max(height-FrameH, 0)
	top, bottom := f.blankRows(avail, min(avail, 1))
	return avail - top - bottom
}

// blankRows returns the blank rows kept above and below rows of content when
// the borders leave avail rows: all of them when the content leaves room for
// all, else none, so that a box never has a blank row on one side only.
func (f Frame) blankRows(avail, rows int) (top, bottom int) {
	top, bottom = max(f.BlankTop, 0), max(f.BlankBottom, 0)
	if avail-max(rows, 0) < top+bottom {
		return 0, 0
	}
	return top, bottom
}

// OuterSize returns the size of the box around content of size content:
// plus the borders, the space beside each and the blank rows, at least as
// wide as the border texts need (the title, the keys, the message).
func (f Frame) OuterSize(content Size) Size {
	return Size{max(max(content.W, 0)+FrameW, f.textsWidth()), max(content.H, 0) + f.frameH()}
}

// RequestInnerSize returns the box for content of size want, at most limit,
// a side of limit 0 or less meaning no limit on that side: inner is the area
// the content gets, outer the box, borders included. When it is too low, the
// blank rows give way first, all of them at once. When the content still
// does not fit, outer is the most possible, limit's width or height on the
// side that does not fit, inner what is left of it, and the error says which
// side: ErrTooNarrow, ErrTooLow, or both (errors.Join). Border texts wider
// than the limit are cut, as Render cuts them, and are no error.
func (f Frame) RequestInnerSize(want, limit Size) (inner, outer Size, err error) {
	outer = f.OuterSize(want)
	if limit.W > 0 {
		outer.W = max(min(outer.W, limit.W), FrameW)
	}
	if limit.H > 0 {
		outer.H = max(min(outer.H, limit.H), FrameH)
	}
	avail := outer.H - FrameH // the rows inside the borders, for content and blank rows
	var errs []error
	if limit.W > 0 && max(want.W, 0)+FrameW > limit.W {
		errs = append(errs, ErrTooNarrow)
	}
	if max(want.H, 0) > avail {
		errs = append(errs, ErrTooLow)
	}
	inner = Size{f.InnerSize(outer).W, min(max(want.H, 0), avail)}
	return inner, outer, errors.Join(errs...)
}

// Fit returns the box for body, as RequestInnerSize: its content as wide as
// the widest line of body and as high as its lines, at least least (a side
// 0: as the body), at most limit (a side 0: no limit).
func (f Frame) Fit(body string, least, limit Size) (inner, outer Size, err error) {
	want := Size{max(lipgloss.Width(body), least.W), max(strings.Count(body, "\n")+1, least.H)}
	return f.RequestInnerSize(want, limit)
}

// BorderRoom returns the room the borders of a box width wide leave their
// texts: the top border's title and keys, the bottom border's keys or
// message, each with a space on either side; the box less its corners and
// the least border line beside each. A text wider than that is cut with ….
//
// Note: meant for this package's own use (render, OuterSize); exported for
// now to see how callers would use it, and to become internal again.
func (f Frame) BorderRoom(width int) int { return max(width-2-2*minDash, 0) }

// textsWidth is the width the border texts need: the title and the top
// keys, or the bottom keys or message, at least BottomMsgRoom, each with the
// least border line beside it.
func (f Frame) textsWidth() int {
	bottom, _ := f.bottom()
	bottomW := ansi.StringWidth(bottomLabel(bottom))
	if f.BottomMsgRoom > 0 {
		bottomW = max(bottomW, f.BottomMsgRoom+2) // the message with a space on either side
	}
	return max(topWidth(f.Title, Hints(f.Top)), bottomW) + 2*minDash + 2
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

// RenderSize draws body in the frame, exactly box.W columns wide, the
// borders included: lines cut with "…" or padded with spaces. With box.H > 0
// it is exactly box.H rows high: the blank rows stay when the lines of body
// leave room for all of them, else all give way; the content's rows are cut
// or padded with empty ones. With box.H <= 0 it is as high as body and the
// blank rows. A width too small for the border texts cuts them as Render
// does; the frame is at least FrameW wide and as high as its borders.
func (f Frame) RenderSize(body string, box Size) string {
	lines := strings.Split(body, "\n")
	top, bottom := max(f.BlankTop, 0), max(f.BlankBottom, 0)
	if box.H > 0 {
		avail := max(box.H-FrameH, 0)
		top, bottom = f.blankRows(avail, min(len(lines), avail))
		rows := avail - top - bottom
		lines = lines[:min(len(lines), rows)]
		lines = append(lines, make([]string, rows-len(lines))...)
	}
	return f.render(lines, max(box.W, FrameW), top, bottom)
}

// render draws lines in the frame, width wide, between blankTop and
// blankBottom empty rows.
func (f Frame) render(lines []string, width, blankTop, blankBottom int) string {
	lines = append(append(make([]string, blankTop), lines...), make([]string, blankBottom)...)
	inner := width - FrameW
	line := width - 2 // the border line between the corners
	room := f.BorderRoom(width)
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

	for _, l := range lines {
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

// Box is a popup's frame whose keys come from a keymap: the actions of
// TopActions at the right of the top border, those of BottomActions at the
// right of the bottom one, looked up on Page as keymap.Key looks keys up
// (keymap.RowsOn), as they are in the keymap it has now. The frame's Top and
// Bottom are shown before them, for what is no key (a position, 3/10); its
// Msg, set by SetMsg, in place of the bottom keys.
type Box struct {
	Frame
	Page                      string
	TopActions, BottomActions []string
	// State gives the state of an action whose label shows it (a key that
	// reads "regex" or "plain"): the row's label of that state; nil: 0.
	State func(action string) int
	// TwoWay names actions drawn two-way on this box, besides those of the
	// keymap's Spec.TwoWay, which names them in all tables.
	TwoWay []string
	// FirstKey shows each action by its row's display or, without one, its
	// first key (↵ open), not all its keys (↵/o open); a two-way action stays
	// one pair (↑↓).
	FirstKey bool
	keys     *keymap.Keymap
}

// NewBox returns the box titled title with the actions top and bottom of
// page in km, in DefaultStyles.
func NewBox(title string, km *keymap.Keymap, page string, top, bottom []string) Box {
	return Box{Frame: Frame{Title: title, Styles: DefaultStyles()}, Page: page, TopActions: top, BottomActions: bottom, keys: km}
}

// SetMsg returns b with msg in place of the bottom keys, until SetMsg("");
// msg is drawn as given, so the caller makes it printable.
func (b Box) SetMsg(msg string) Box {
	b.Msg = msg
	return b
}

// Fit returns the box for body, at least least, at most limit, and the area
// the content gets in it, as Frame.Fit with the keys of its actions.
func (b Box) Fit(body string, least, limit Size) (inner, outer Size, err error) {
	return b.frame().Fit(body, least, limit)
}

// Render draws body in the box, exactly box.W wide and, with box.H > 0,
// box.H high, as Frame.RenderSize.
func (b Box) Render(body string, box Size) string {
	return b.frame().RenderSize(body, box)
}

// frame returns b's frame with the keys of its actions after its own.
func (b Box) frame() Frame {
	f := b.Frame
	f.Top = append(slices.Clone(f.Top), b.bindings(b.TopActions)...)
	f.Bottom = append(slices.Clone(f.Bottom), b.bindings(b.BottomActions)...)
	return f
}

// bindings returns the keys of actions on b's page: a two-way action (of
// the keymap's Spec.TwoWay or of b.TwoWay) once, with FirstKey the others
// by their display or first key,
// its forward keys with the help of both; an action without a row is left
// out.
func (b Box) bindings(actions []string) []key.Binding {
	var bs []key.Binding
	for _, a := range actions {
		rows := b.keys.RowsOn(b.Page, a)
		bind := teakeys.BindingOn(b.keys, b.Page, a, key.Binding{})
		twoWay := b.keys.TwoWay(a) || slices.Contains(b.TwoWay, a)
		if twoWay {
			bind, _ = teakeys.TwoWayOn(b.keys, b.Page, a, key.Binding{}, key.Binding{})
		}
		if b.FirstKey && !twoWay && len(rows) > 0 && rows[0].Display == "" && len(rows[0].Keys) > 0 {
			bind.SetHelp(teakeys.Display(rows[0].Keys[0]), bind.Help().Desc)
		}
		if b.State != nil && len(rows) > 0 {
			bind.SetHelp(bind.Help().Key, rows[0].LabelOf(b.State(a)))
		}
		bs = append(bs, bind)
	}
	return bs
}
