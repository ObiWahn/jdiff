package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
)

// binding is a key and what it does; a binding without key is a heading.
type binding struct{ key, what string }

// bindings lists the keys of all pages.
var bindings = []binding{
	{"", "Commit list"},
	{"j k", "move"},
	{"pgup pgdn", "page up, down (also ctrl+b ctrl+f)"},
	{"g G", "first, last commit"},
	{"enter", "show the files of the commit"},
	{"f", "flow view of all commits"},
	{"v", "show or hide the message box"},
	{"", "File list"},
	{"j k  n p", "move"},
	{"pgup pgdn", "page up, down (also ctrl+b ctrl+f)"},
	{"g G", "first, last file"},
	{"enter", "show the diff of the file"},
	{"'<letter>", "open the file of a jump mark"},
	{"N P", "next, previous commit"},
	{"f", "flow view of all files"},
	{"v", "show or hide the diff preview"},
	{"s", "side by side or normal preview"},
	{"w", "show all whitespace or only changed"},
	{"backspace", "back to the commits"},
	{"", "Diff view"},
	{"j k", "move"},
	{"pgup pgdn", "page up, down (also ctrl+b ctrl+f)"},
	{"ctrl+u ctrl+d", "half page up, down"},
	{"g G", "top, bottom"},
	{"h l", "scroll left, right"},
	{"] [", "next, previous change"},
	{"n p", "next, previous file"},
	{"N P", "next, previous commit"},
	{"space", "fold or unfold unchanged code"},
	{"ctrl+space", "fold or unfold all of the file"},
	{"tab", "switch pane"},
	{"o", "other end of moved code (magenta: moved away, cyan: moved here)"},
	{"m<letter>", "set a jump mark on the line of the new file"},
	{"'<letter>", "jump to a mark, when its file is in this diff"},
	{"enter", "mark a line, then its partner: anchor"},
	{"", "  enter on an anchored line removes the anchor"},
	{"", "  normal layout: unchanged lines take the side opposite the mark"},
	{"esc", "cancel the mark"},
	{"s", "side by side or normal layout"},
	{"w", "show all whitespace or only changed"},
	{"f", "flow view"},
	{"backspace", "back to the list"},
	{"", "Flow view"},
	{"", "  the keys of the diff view, and:"},
	{"n p", "next, previous file bar"},
	{"N P", "next, previous commit bar"},
	{"c", "full or short commit messages"},
	{"C", "stay in the current commit (N P still switch)"},
	{"f", "single file view"},
	{"", "Everywhere"},
	{"", "  c C f s v w are saved in ~/.config/jdiff/state.toml"},
	{"?", "this help (? esc q close, j k scroll)"},
	{"q ctrl+c", "quit"},
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
	case tea.KeyMsg:
		keys := keyNames(msg)
		if !h.open && !slices.Contains(keys, "?") {
			break
		}
		if len(keys) == 1 {
			return h, h.key(keys[0], msg)
		}
		// A burst of runes opening or closing the popup: each key goes where
		// it would have gone pressed alone.
		for _, k := range keys {
			if cmd := h.key(k, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}); cmd != nil {
				return h, cmd
			}
		}
		return h, nil
	}
	_, cmd := h.Model.Update(msg)
	return h, cmd
}

// key handles k, pressed as msg, in the popup, or passes msg to the page.
func (h *Help) key(k string, msg tea.KeyMsg) tea.Cmd {
	if h.open {
		switch k {
		case "?", "esc", "q":
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
	if k == "?" {
		h.open, h.top = true, 0
		return nil
	}
	_, cmd := h.Model.Update(msg)
	return cmd
}

// boxHeight is the height of the popup: all lines when they fit, else the
// screen but a line above and below.
func (h *Help) boxHeight() int { return min(len(bindings)+2, max(h.height-2, minBoxHeight)) }

func (h *Help) scroll(delta int) {
	h.top = max(min(h.top+delta, len(bindings)-(h.boxHeight()-2)), 0)
}

func (h *Help) View() string {
	base := h.Model.View()
	if !h.open || h.width == 0 {
		return base
	}
	t := h.cfg.Theme
	keyW, whatW := 0, 0
	for _, b := range bindings {
		keyW = max(keyW, ansi.StringWidth(b.key))
		whatW = max(whatW, ansi.StringWidth(b.what))
	}
	var lines []string
	for _, b := range bindings[h.top:] {
		switch {
		case b.key == "" && !strings.HasPrefix(b.what, " "):
			lines = append(lines, t.Branch.Render(b.what))
		case b.key == "":
			lines = append(lines, strings.Repeat(" ", keyW+2)+t.Hint.Render(strings.TrimSpace(b.what)))
		default:
			key := lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("  %-*s", keyW, b.key))
			lines = append(lines, key+"  "+b.what)
		}
	}
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
	rows := strings.Split(base, "\n")
	popRows := strings.Split(strings.TrimSuffix(pop, "\n"), "\n")
	y := max((len(rows)-len(popRows))/2, 0)
	x := max((width-w)/2, 0)
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
