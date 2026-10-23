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
	dir := switchCommitMsg(1)
	if k == "P" {
		dir = -1
	}
	return func() tea.Msg { return dir }
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
