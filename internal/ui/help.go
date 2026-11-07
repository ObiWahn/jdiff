package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
)

// binding is a key and what it does; a binding without key is a heading.
type binding struct{ key, what string }

// bindings lists the lines of the help popup, from keybindings.toml.
var bindings = keyFile.bindings()

// bindings returns the lines of the help popup.
func (p keyPages) bindings() []binding {
	var bs []binding
	for _, h := range p.Help {
		switch {
		case h.Heading != "":
			bs = append(bs, binding{"", h.Heading})
		case h.Note != "":
			bs = append(bs, binding{"", "  " + h.Note})
		default:
			r := p.row(h.Action)
			bs = append(bs, binding{strings.Join(r.Keys, " "), r.Help})
		}
	}
	return bs
}

// Help wraps the top-level model and shows the key bindings of all pages in
// a popup over it on ?.
type Help struct {
	tea.Model
	cfg           *config.Config
	open          bool
	top           int // first shown line of the popup
	width, height int
}

// WithHelp returns m with the help popup.
func WithHelp(cfg *config.Config, m tea.Model) *Help { return &Help{Model: m, cfg: cfg} }

func (h *Help) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width, h.height = msg.Width, msg.Height
		h.scroll(0)
	case tea.KeyPressMsg:
		if cmd, ok := popupKeys(msg, h.open, "help", h.key); ok {
			return h, cmd
		}
	}
	_, cmd := h.Model.Update(msg)
	return h, cmd
}

// popupKeys passes the key press msg to key, when the popup is open or msg
// opens it, being a key of action opener, and reports whether it did; else
// msg is for the page below.
func popupKeys(msg tea.KeyPressMsg, open bool, opener string, key func(string) tea.Cmd) (tea.Cmd, bool) {
	k := msg.String()
	if !open && !isAction(k, opener) {
		return nil, false
	}
	return key(k), true
}

// key handles k in the popup, or opens it on ?.
func (h *Help) key(k string) tea.Cmd {
	if !h.open {
		h.open, h.top = true, 0
		return nil
	}
	if isAction(k, "help") {
		h.open = false
		return nil
	}
	switch k {
	case "esc", "q":
		h.open = false
	case "ctrl+c":
		return tea.Quit
	case "down", "j":
		h.scroll(1)
	case "up", "k":
		h.scroll(-1)
	}
	return nil
}

// boxHeight is the height of the popup.
func (h *Help) boxHeight() int { return popupHeight(len(bindings), h.height) }

// scroll moves the shown lines by delta; from the top, it moves on by two
// at once, as the window never starts at the second line.
func (h *Help) scroll(delta int) {
	top := h.top + delta
	if top == 1 && delta > 0 {
		top = 2
	}
	h.top = windowAt(top, h.boxHeight()-2, len(bindings)).first
}

func (h *Help) View() tea.View { return tea.NewView(h.frame()) }

func (h *Help) frame() string {
	base := h.Model.View().Content
	if !h.open || h.width == 0 {
		return base
	}
	t := h.cfg.Theme
	keyW, whatW := 0, 0
	for _, b := range bindings {
		keyW = max(keyW, ansi.StringWidth(b.key))
		whatW = max(whatW, ansi.StringWidth(b.what))
	}
	line := func(i int) string {
		switch b := bindings[i]; {
		case b.key == "" && !strings.HasPrefix(b.what, " "):
			return t.Branch.Render(b.what)
		case b.key == "":
			return strings.Repeat(" ", keyW+2) + t.Hint.Render(strings.TrimSpace(b.what))
		default:
			return lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("  %-*s", keyW, b.key)) + "  " + b.what
		}
	}
	lines := windowAt(h.top, h.boxHeight()-2, len(bindings)).rows(t, len(bindings), line)
	w := min(keyW+whatW+8, h.width) // the key indent, gap, border and padding
	title := t.CommitHash.Render(" keys ")
	if h.boxHeight()-2 < len(bindings) {
		title += t.Hint.Render(fmt.Sprintf(" %d/%d ", h.top+1, len(bindings)))
	}
	return overlay(base, box(t, w, h.boxHeight(), title, lines), w, h.width)
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
