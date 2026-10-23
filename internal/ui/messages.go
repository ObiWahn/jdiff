package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"jdiff/internal/config"
)

// switchFileMsg asks the file selector to show the next (1) or previous (-1)
// file.
type switchFileMsg int

// switchCommitMsg asks the log to show the next (1, older) or previous (-1)
// commit.
type switchCommitMsg int

// backMsg asks the log to return to the commit list, or the file selector to
// its list when it shows the flow view.
type backMsg struct{}

// jumpMsg asks the file selector to show the line of the other end of moved
// code in another file: end, in the file of the group with index end.file,
// at screen row screenRow, with message msg.
type jumpMsg struct {
	end       moveEnd
	screenRow int
	msg       string
}

// toggleFlowMsg asks the log or the file selector to switch between the diff
// view of one file and the flow view of all files.
type toggleFlowMsg struct{}

func toggleFlow() tea.Msg { return toggleFlowMsg{} }

// saveState writes the toggles to state.toml, returning a message when that
// fails, else "".
func saveState(cfg *config.Config) string {
	if err := cfg.SaveState(); err != nil {
		return "state: " + err.Error()
	}
	return ""
}

// switchCommit returns the command asking the log for the next (N) or
// previous (P) commit.
func switchCommit(k string) tea.Cmd {
	dir := switchCommitMsg(direction(k == "N"))
	return func() tea.Msg { return dir }
}

// toggle flips setting, one of the toggles of cfg, and saves them, returning
// a message when that fails, else "".
func toggle(cfg *config.Config, setting *bool) string {
	*setting = !*setting
	return saveState(cfg)
}

// keyNames returns the key presses of msg. Bubble Tea delivers runes read
// together (held keys, fast typing over ssh) as one message; they are split so
// that each counts.
func keyNames(msg tea.KeyMsg) []string {
	if msg.Type != tea.KeyRunes || msg.Paste || msg.Alt || len(msg.Runes) < 2 {
		return []string{msg.String()}
	}
	out := make([]string, len(msg.Runes))
	for i, r := range msg.Runes {
		out[i] = string(r)
	}
	return out
}

// pressKeys passes the key presses of msg to key until it returns a command.
func pressKeys(msg tea.KeyMsg, key func(string) tea.Cmd) tea.Cmd {
	for _, k := range keyNames(msg) {
		if cmd := key(k); cmd != nil {
			return cmd
		}
	}
	return nil
}

// motion returns how many rows the cursor moves on k, page rows for a page
// and far to the first or last row, whether k pages, so that views of file
// content move the window along, and whether k moves the cursor at all.
func motion(k string, page, far int) (delta int, paging, ok bool) {
	switch k {
	case "up", "k":
		return -1, false, true
	case "down", "j":
		return 1, false, true
	case "pgup", "ctrl+b":
		return -page, true, true
	case "pgdown", "ctrl+f":
		return page, true, true
	case "home", "g":
		return -far, false, true
	case "end", "G":
		return far, false, true
	}
	return 0, false, false
}

func direction(forward bool) int {
	if forward {
		return 1
	}
	return -1
}

// ends returns the message for having reached the last or first item.
func ends(forward bool, last, first string) string {
	if forward {
		return last
	}
	return first
}
