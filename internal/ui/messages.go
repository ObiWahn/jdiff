package ui

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"jdiff/internal/config"
	"jdiff/internal/keys"
)

// switchFileMsg asks the file list to show the next (1) or previous (-1)
// file.
type switchFileMsg int

// switchCommitMsg asks the log to show the next (1) or previous (-1) commit
// in history.
type switchCommitMsg int

// backMsg asks the log to return to the commit list, or from its flow view
// to the file list of the commit; or the file list to its list when it
// shows the flow view.
type backMsg struct{}

// flowAtMsg asks the log to show its flow view on the file with this index
// of the commit whose files it shows: enter in that file list in flow mode.
type flowAtMsg int

// jumpMsg asks the file list to show the line of the other end of moved
// code in another file: end, in the file of the group with index end.file,
// at screen row screenRow, with message msg.
type jumpMsg struct {
	end       moveEnd
	screenRow int
	msg       string
}

// refreshMsg asks the top-level view to load what it shows again: run git
// diff or git log again, list the directories again or read the two files.
// With quiet, as auto reload asks, it does not say when nothing changed.
type refreshMsg struct{ quiet bool }

func refreshAll() tea.Msg { return refreshMsg{} }

// nothingChanged is the message after loading again the same.
const nothingChanged = "nothing changed"

// The messages of loading again: nothingToReload where nothing is shown that
// could be, reloading while it runs in the background.
const (
	nothingToReload = "nothing to load again"
	reloading       = "loading again…"
)

// The prompts of the keys of marks, until the letter is pressed.
const (
	setMarkPrompt = "set mark: press a letter"
	jumpPrompt    = "jump to mark: press its letter"
)

// relistState is the state of listing again in the background, which Diff
// (its files) and Log (its commits) must keep the same way: while a listing
// runs, another one is not started, but run once it is done, quietly only
// when all that asked for it meanwhile asked quietly.
type relistState struct {
	running, again, againQuiet bool
}

// start returns whether a listing with quiet starts now; while one runs, it
// notes the listing for after it instead.
func (r *relistState) start(quiet bool) bool {
	if r.running {
		r.againQuiet = quiet && (!r.again || r.againQuiet)
		r.again = true
		return false
	}
	r.running = true
	return true
}

// done ends the listing that ran. It returns whether another one was asked
// for meanwhile, and whether quietly.
func (r *relistState) done() (again, quiet bool) {
	r.running = false
	again, r.again = r.again, false
	return again, r.againQuiet
}

// askAutoReloadMsg asks App to toggle auto reload.
type askAutoReloadMsg struct{}

func askAutoReload() tea.Msg { return askAutoReloadMsg{} }

// showMsg asks the top-level view to show a message in its status bar.
type showMsg string

// refreshed returns the message after loading again: what was added,
// removed and changed, and the anchors dropped from changed files.
func refreshed(added, gone, changed, dropped int) string {
	var parts []string
	for _, c := range []struct {
		n    int
		what string
	}{{added, "new"}, {gone, "gone"}, {changed, "changed"}} {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c.n, c.what))
		}
	}
	switch {
	case dropped == 1:
		parts = append(parts, "1 anchor dropped")
	case dropped > 1:
		parts = append(parts, fmt.Sprintf("%d anchors dropped", dropped))
	}
	if len(parts) == 0 {
		return nothingChanged
	}
	return strings.Join(parts, ", ")
}

// askDiffMsg asks App for the arguments of git diff to show next.
type askDiffMsg struct{}

func askDiff() tea.Msg { return askDiffMsg{} }

// askSettingsMsg asks SettingsPopup to open.
type askSettingsMsg struct{}

func askSettings() tea.Msg { return askSettingsMsg{} }

// toggleSettingMsg asks SettingsPopup to toggle the option of action, with the
// popup closed.
type toggleSettingMsg struct{ action string }

// settingKey returns the command toggling the option of action, nil when no
// option has it or, outside the log's flow view (logFlow), one that only
// matters there.
func settingKey(km *keys.Map, action string, logFlow bool) tea.Cmd {
	options := settingWords(km, settingTable)
	i := slices.IndexFunc(options, func(o settingOption) bool { return o.key != "" && o.action == action })
	if i < 0 || options[i].logFlow && !logFlow {
		return nil
	}
	return func() tea.Msg { return toggleSettingMsg{action} }
}

// pageKey returns the command of key h that all pages hand on: loading again,
// auto reload, the popups of git diff and the settings, or the option of its
// action, see settingKey; nil when h is none of them. The pages hand them on,
// and not the popups themselves as the help and the marks: o is a mark letter
// too, which m and ' take before the page looks at the key.
func pageKey(km *keys.Map, h keys.Hit, logFlow bool) tea.Cmd {
	switch h.Action {
	case "reload":
		return refreshAll
	case "auto_reload":
		return askAutoReload
	case "git_diff":
		return askDiff
	case "settings":
		return askSettings
	}
	return settingKey(km, h.Action, logFlow)
}

// settingChangedMsg tells the page below SettingsPopup that a toggle or a
// number changed, so that it lays out what depends on it. The diff view does
// on any message. problem says why it is not saved, for the page to show when
// the popup is closed.
type settingChangedMsg struct{ action, problem string }

// toggleUntrackedMsg asks the top-level view to toggle the untracked files of
// its git diff, which only the file list of one has.
type toggleUntrackedMsg struct{}

func toggleUntracked() tea.Msg { return toggleUntrackedMsg{} }

// untrackedOnlyInDiff is the answer of the other views.
const untrackedOnlyInDiff = "untracked files: only in jdiff diff"

// flowOnlyOfSeveral is the answer of the diff view of two files.
const flowOnlyOfSeveral = "flow: only with a file list or the log"

// toggleFlowMsg asks the log or the file list to switch between the diff
// view of one file and the flow view of all files.
type toggleFlowMsg struct{}

func toggleFlow() tea.Msg { return toggleFlowMsg{} }

// firstLine returns the first line of err: git writes errors of several lines,
// where the status bar and notes have one.
func firstLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}

// saveState writes the toggles to state.toml, returning a message when that
// fails, else "".
func saveState(cfg *config.Config) string {
	if err := cfg.SaveState(); err != nil {
		return "state: " + err.Error()
	}
	return ""
}

// The direction in history is decided here only: historyDir turns a key
// into it, historyOrder.step into a step through a list of commits.

// historyDir returns where key h of history goes: 1 to the next commit, for
// the first key of its row, as N and n, -1 to the previous one, as P and p.
func historyDir(h keys.Hit) int { return direction(h.Index == 0) }

// historyOrder is the order of a list of commits, as git log lists them:
// the step from a commit to the next one in history, -1 when the newest
// comes first, as by default, 1 when the oldest does, with --reverse.
type historyOrder int

// orderOf returns the order of a list of commits, the oldest first or not.
func orderOf(oldestFirst bool) historyOrder {
	if oldestFirst {
		return 1
	}
	return -1
}

// step returns the step through the list to go in direction dir in history.
func (o historyOrder) step(dir int) int { return dir * int(o) }

// switchCommit returns the command asking the log for the commit key h goes
// to in history.
func switchCommit(h keys.Hit) tea.Cmd {
	dir := switchCommitMsg(historyDir(h))
	return func() tea.Msg { return dir }
}

// toggle flips setting, one of the toggles of cfg, and saves them, returning
// a message when that fails, else "".
func toggle(cfg *config.Config, setting *bool) string {
	*setting = !*setting
	return saveState(cfg)
}

// runeKey returns the key press typing the character s.
func runeKey(s string) tea.KeyPressMsg {
	r, _ := utf8.DecodeRuneInString(s)
	return tea.KeyPressMsg{Code: r, Text: s}
}

// motion returns how many rows the cursor moves on key h, page rows for a
// page and far to the first or last row, whether h pages, so that views of
// file content move the window along, and whether h moves the cursor at all.
func motion(h keys.Hit, page, far int) (delta int, paging, ok bool) {
	switch h.Action {
	case "move":
		return direction(h.Index == 0), false, true
	case "page":
		return direction(h.Index == 1) * page, true, true
	case "ends":
		return direction(h.Index == 1) * far, false, true
	}
	return 0, false, false
}

// diffKeyView is what the keys that work alike in the diff view and the flow
// view act on, see diffKey.
type diffKeyView interface {
	moveCursor(delta int)
	scroll(delta int)        // up and down, the window along
	scrollSideways(cols int) // the lines, not before their start
	switchPane()
	nextChange(dir int)
	findText(dir int)
	askLetter(action, msg string) // the prefix and its message, until the mark's letter
}

// diffKey handles the keys that must work alike in the diff view and the
// flow view, and reports whether h was one of them: motion, quit, pane,
// half_page, scroll, change, search, match, set_mark and jump. page is the
// paging step, far the move to the first or last row. Moving between files
// and commits, anchors, folds and moved lines differ, and stay with each
// view.
func diffKey(v diffKeyView, h keys.Hit, page, far int) (tea.Cmd, bool) {
	if delta, paging, ok := motion(h, page, far); ok {
		if paging {
			v.scroll(delta)
		} else {
			v.moveCursor(delta)
		}
		return nil, true
	}
	switch h.Action {
	case "quit":
		return tea.Quit, true
	case "pane":
		v.switchPane()
	case "half_page":
		v.scroll(halfPage(h, page))
	case "scroll":
		v.scrollSideways(direction(h.Index == 1) * scrollStep)
	case "change":
		v.nextChange(direction(h.Index == 0))
	case "search":
		return askSearch, true
	case "match":
		v.findText(direction(h.Index == 0))
	case "set_mark":
		v.askLetter(h.Action, setMarkPrompt)
	case "jump":
		v.askLetter(h.Action, jumpPrompt)
	default:
		return nil, false
	}
	return nil, true
}

// scrollStep is how many columns the scroll keys move the lines sideways,
// in the diff view and the flow view alike.
const scrollStep = 8

// halfPage returns how many rows half_page key h scrolls, half of page rows,
// the paging step, in the diff view, the flow view and the readme alike.
func halfPage(h keys.Hit, page int) int { return direction(h.Index == 1) * (page / 2) }

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
