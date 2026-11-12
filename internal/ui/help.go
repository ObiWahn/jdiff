package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/keys"
)

// binding is a key and what it does; a binding without key is a heading.
type binding struct{ key, what string }

// helpBindings returns the lines of the help popup of km, in the order of its
// help; helpLines puts the settings in their order.
func helpBindings(km *keys.Map) []binding {
	var bs []binding
	for _, h := range km.Help {
		switch {
		case h.Heading != "":
			bs = append(bs, binding{"", h.Heading})
		case h.Note != "":
			bs = append(bs, binding{"", "  " + km.Text(h.Note)})
		default:
			r := km.Row(h.Action)
			bs = append(bs, binding{strings.Join(r.Keys, " "), km.Text(r.Help)})
		}
	}
	return bs
}

// Help wraps the top-level model and shows the key bindings of all pages in
// a popup over it on ?.
type Help struct {
	tea.Model
	cfg           *config.Config
	lines         []binding // of cfg.Keys, again on each opening: the settings change them
	rows          []string  // the lines drawn, wrapped to the width while open
	boxWidth      int
	open          bool
	top           int // first shown row of the popup
	width, height int
}

// WithHelp returns m with the help popup.
func WithHelp(cfg *config.Config, m tea.Model) *Help {
	return &Help{Model: m, cfg: cfg, lines: helpLines(cfg.Keys)}
}

func (h *Help) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width, h.height = msg.Width, msg.Height
		if h.open { // else wrapped when opened: resizing stays cheap
			h.wrap()
			h.scroll(0)
		}
	case tea.KeyPressMsg:
		if cmd, ok := popupKeys(h.cfg.Keys, msg, h.open, "help", h.key); ok {
			return h, cmd
		}
	}
	_, cmd := h.Model.Update(msg)
	return h, cmd
}

// popupKeys passes the key press msg to key, when the popup is open or msg
// opens it, being a key of action opener, and reports whether it did; else
// msg is for the page below.
func popupKeys(km *keys.Map, msg tea.KeyPressMsg, open bool, opener string, key func(string) tea.Cmd) (tea.Cmd, bool) {
	k := msg.String()
	if !open && !isAction(km, k, opener) {
		return nil, false
	}
	return key(k), true
}

// key handles k in the popup, or opens it on ?.
func (h *Help) key(k string) tea.Cmd {
	if !h.open {
		h.open, h.top, h.lines = true, 0, helpLines(h.cfg.Keys)
		h.wrap()
		return nil
	}
	cmd, _ := popupNav(h.cfg.Keys, k, "help", &h.open, h.scroll)
	return cmd
}

// popupNav handles the keys that all popups share, so that they close and
// move alike: a key of opener, esc and q close the popup, ctrl+c quits, down
// and j move by one, up and k back. It reports whether k was one of them;
// the popup handles the others.
func popupNav(km *keys.Map, k, opener string, open *bool, move func(delta int)) (tea.Cmd, bool) {
	if isAction(km, k, opener) {
		*open = false
		return nil, true
	}
	switch k {
	case "esc", "q":
		*open = false
	case "ctrl+c":
		return tea.Quit, true
	case "down", "j":
		move(1)
	case "up", "k":
		move(-1)
	default:
		return nil, false
	}
	return nil, true
}

// boxHeight is the height of the popup.
func (h *Help) boxHeight() int { return popupHeight(len(h.rows), h.height) }

// scroll moves the shown lines by delta; from the top, it moves on by two
// at once, as the window never starts at the second line.
func (h *Help) scroll(delta int) {
	top := h.top + delta
	if top == 1 && delta > 0 {
		top = 2
	}
	h.top = windowAt(top, h.boxHeight()-2, len(h.rows)).first
}

// wrap draws the lines into rows as wide as the screen allows: what does not
// fit goes on in the rows below, under its first word, instead of being cut.
// The popup is as wide as its widest row.
func (h *Help) wrap() {
	t := h.cfg.Theme
	keyW := 0
	for _, b := range h.lines {
		keyW = max(keyW, ansi.StringWidth(b.key))
	}
	inner := h.width - 4 // border and padding
	h.rows = h.rows[:0]
	for _, b := range h.lines {
		switch {
		case b.key == "" && !strings.HasPrefix(b.what, " "):
			h.rows = append(h.rows, wrapRows("", 0, b.what, inner, t.Branch)...)
		case b.key == "":
			h.rows = append(h.rows, wrapRows(strings.Repeat(" ", keyW+2), keyW+2, strings.TrimSpace(b.what), inner, t.Hint)...)
		default:
			key := t.Key.Render(fmt.Sprintf("  %-*s", keyW, b.key)) + "  "
			h.rows = append(h.rows, wrapRows(key, keyW+4, b.what, inner, lipgloss.NewStyle())...)
		}
	}
	rowW := 0
	for _, r := range h.rows {
		rowW = max(rowW, ansi.StringWidth(r))
	}
	h.boxWidth = min(rowW+4, h.width)
}

// wrapRows returns the rows of text in style after prefix, which is indent
// columns wide, wrapped to width; the rows after the first are indented as
// far. Without room after the indent, text stays in one row.
func wrapRows(prefix string, indent int, text string, width int, style lipgloss.Style) []string {
	if width-indent < 1 {
		return []string{prefix + style.Render(text)}
	}
	parts := strings.Split(ansi.Wrap(text, width-indent, ""), "\n")
	rows := make([]string, len(parts))
	for i, p := range parts {
		if i > 0 {
			prefix = strings.Repeat(" ", indent)
		}
		rows[i] = prefix + style.Render(strings.TrimRight(p, " "))
	}
	return rows
}

func (h *Help) View() tea.View { return tea.NewView(h.frame()) }

func (h *Help) frame() string {
	base := h.Model.View().Content
	if !h.open || h.width == 0 {
		return base
	}
	t := h.cfg.Theme
	lines := windowAt(h.top, h.boxHeight()-2, len(h.rows)).rows(t, len(h.rows), func(i int) string { return h.rows[i] })
	title := t.CommitHash.Render(" keys ")
	if h.boxHeight()-2 < len(h.rows) {
		title += t.Hint.Render(fmt.Sprintf(" %d/%d ", h.top+1, len(h.rows)))
	}
	return popup{title, lines, h.boxWidth, h.boxHeight()}.over(t, base, h.width)
}

// overlay draws the popup pop, w columns wide, centered over base, which
// is width columns wide.
func overlay(base, pop string, w, width int) string {
	y := max((strings.Count(base, "\n")+1-strings.Count(strings.TrimSuffix(pop, "\n"), "\n")-1)/2, 0)
	return overlayAt(base, pop, max((width-w)/2, 0), y, w, width)
}

// overlayAt draws the popup pop, w columns wide, over base, which is width
// columns wide, from column x of row y on.
func overlayAt(base, pop string, x, y, w, width int) string {
	rows := strings.Split(base, "\n")
	popRows := strings.Split(strings.TrimSuffix(pop, "\n"), "\n")
	for i, p := range popRows {
		if y+i >= len(rows) {
			break
		}
		row := rows[y+i]
		left := ansi.Truncate(row, x, "")
		left += strings.Repeat(" ", x-ansi.StringWidth(left))
		rows[y+i] = left + p + ansi.Cut(row, x+w, width)
	}
	return strings.Join(rows, "\n")
}
