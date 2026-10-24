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
	{"N P  n p", "next, previous commit in history"},
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
	{"N P", "next, previous commit in history (in the log)"},
	{"f", "flow view of all files"},
	{"v", "show or hide the diff preview"},
	{"s", "side by side or normal preview"},
	{"w", "show all whitespace and line ends or only changed"},
	{"W", "mark changed whitespace or not"},
	{"t", "show trailing whitespace or not"},
	{"e", "show all \\r\\n line ends (␍) or only changed"},
	{"r", "reflow: wrap long lines or cut them"},
	{"b", "bold or plain changed words"},
	{"backspace", "back to the commits (in the log)"},
	{"", "Diff view"},
	{"j k", "move"},
	{"pgup pgdn", "page up, down (also ctrl+b ctrl+f)"},
	{"ctrl+u ctrl+d", "half page up, down"},
	{"g G", "top, bottom"},
	{"h l", "scroll left, right, unless lines are wrapped"},
	{"] [", "next, previous change"},
	{"n p", "next, previous file"},
	{"N P", "next, previous commit in history (in the log)"},
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
	{"/", "search: enter find, tab text or regex, shift+tab changed or all lines"},
	{"", "  ↑ ↓ earlier searches, esc or backspace on nothing typed cancel"},
	{". ,", "next, previous match; in the flow view in all files of the commit"},
	{"s", "side by side or normal layout"},
	{"w", "show all whitespace and line ends or only changed"},
	{"W", "mark changed whitespace or not"},
	{"t", "show trailing whitespace or not"},
	{"e", "show all \\r\\n line ends (␍) or only changed"},
	{"r", "reflow: wrap long lines or cut them"},
	{"b", "bold or plain changed words"},
	{"f", "flow view"},
	{"backspace", "back to the file list, also from the flow view"},
	{"", "Flow view"},
	{"", "  the keys of the diff view, and:"},
	{"n p", "next, previous file bar"},
	{"N P", "next, previous commit bar in history"},
	{"c", "full or short commit messages"},
	{"C", "stay in the current commit (N P still switch)"},
	{"f", "single file view"},
	{"", "Everywhere"},
	{"\\", "jump marks: enter jumps, d or del deletes"},
	{"R F5", "load again: git diff, git log, the directories or the files"},
	{"d", "popup: show git diff ARGS instead; ↑ the current ARGS, enter show, esc cancel"},
	{"", "  b c C e f r s t v w W, and tab shift+tab of /, are saved in ~/.config/jdiff/state.toml"},
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
		if cmd, ok := popupKeys(msg, h.open, "?", h.key); ok {
			return h, cmd
		}
	}
	_, cmd := h.Model.Update(msg)
	return h, cmd
}

// popupKeys passes the key presses of msg to key, when the popup is open or
// they open it with opener, and reports whether it did; else msg is for the
// page below. A burst of runes is split, so each key goes where it would
// have gone pressed alone.
func popupKeys(msg tea.KeyMsg, open bool, opener string, key func(string, tea.KeyMsg) tea.Cmd) (tea.Cmd, bool) {
	keys := keyNames(msg)
	if !open && !slices.Contains(keys, opener) {
		return nil, false
	}
	if len(keys) == 1 {
		return key(keys[0], msg), true
	}
	for _, k := range keys {
		if cmd := key(k, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}); cmd != nil {
			return cmd, true
		}
	}
	return nil, true
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

// scroll moves the shown lines by delta; from the top, it moves on by two
// at once, as the window never starts at the second line.
func (h *Help) scroll(delta int) {
	top := h.top + delta
	if top == 1 && delta > 0 {
		top = 2
	}
	h.top = windowAt(top, h.boxHeight()-2, len(bindings)).first
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
