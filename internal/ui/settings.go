package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
)

// settingOption is a line of the settings popup: the key that toggles it
// there and on the pages, what it does while on, and the toggle. With ask,
// the page below toggles it, when it can. With logFlow, its key works only in
// the log's flow view, where it shows; in the popup always. With number instead
// of the toggle, it is a number, asked for in a line; it has no key, the cursor
// goes to it in the popup.
type settingOption struct {
	key, what string
	setting   func(*config.Config) *bool
	ask       tea.Cmd
	logFlow   bool
	number    func(*config.Config) *int
}

var settingOptions = []settingOption{
	{key: "s", what: "side by side layout", setting: func(c *config.Config) *bool { return &c.Settings.SideBySide }},
	{key: "f", what: "flow view of all files or commits", setting: func(c *config.Config) *bool { return &c.Settings.Flow }, ask: toggleFlow},
	{key: "v", what: "show the message box, the diff preview", setting: func(c *config.Config) *bool { return &c.Settings.Preview }},
	{key: "W", what: "show all whitespace and line ends", setting: func(c *config.Config) *bool { return &c.Settings.ShowWhitespaces }},
	{key: "ctrl+w", what: "mark changed whitespace", setting: func(c *config.Config) *bool { return &c.Settings.MarkWhitespace }},
	{key: "t", what: "show trailing whitespace", setting: func(c *config.Config) *bool { return &c.Settings.ShowTrailing }},
	{key: "e", what: "show all \\r\\n line ends (␍)", setting: func(c *config.Config) *bool { return &c.Settings.ShowCR }},
	{key: "b", what: "bold changed words", setting: func(c *config.Config) *bool { return &c.Settings.BoldChanges }},
	{key: "w", what: "wrap long lines", setting: func(c *config.Config) *bool { return &c.Settings.Wrap }},
	{key: "c", what: "full commit messages, in the log's flow view", setting: func(c *config.Config) *bool { return &c.Settings.FullMessages }, logFlow: true},
	{key: "C", what: "stay in the current commit, there (N P still switch)", setting: func(c *config.Config) *bool { return &c.Settings.StayInCommit }, logFlow: true},
	{key: untrackedKey, what: "also show untracked files, in a git diff", setting: func(c *config.Config) *bool { return &c.Settings.Untracked }, ask: toggleUntracked},
	{what: "minutes without keys or changes until auto reload ends; 0 never", number: func(c *config.Config) *int { return &c.Settings.AutoReloadTimeout }},
}

// SettingsPopup wraps the top-level model and shows the display toggles in a
// popup over it on askSettingsMsg. It stays open after a toggle, so that
// several can be set while the page below shows them.
type SettingsPopup struct {
	tea.Model
	cfg           *config.Config
	open          bool
	cursor, top   int        // the selected option, the first one shown
	msg           string     // a problem saving the toggles
	input         *lineInput // while it asks for the number of the option
	inputErr      string     // why the number typed is none
	width, height int
}

// WithSettings returns m with the settings popup.
func WithSettings(cfg *config.Config, m tea.Model) *SettingsPopup {
	return &SettingsPopup{Model: m, cfg: cfg}
}

func (p *SettingsPopup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.move(0)
	case askSettingsMsg:
		p.open, p.cursor, p.top, p.msg = true, 0, 0, ""
		return p, nil
	case toggleSettingMsg:
		i := slices.IndexFunc(settingOptions, func(o settingOption) bool { return o.key == msg.key })
		return p, p.flip(settingOptions[i])
	case tea.KeyPressMsg:
		switch {
		case p.input != nil:
			return p, p.editNumber(msg)
		case p.open:
			return p, p.key(msg.String())
		}
	case tea.PasteMsg:
		if p.input != nil {
			p.input.text += msg.Content
			return p, nil
		}
	}
	_, cmd := p.Model.Update(msg)
	return p, cmd
}

// key handles k in the open popup.
func (p *SettingsPopup) key(k string) tea.Cmd {
	switch k {
	case settingsKey, settingsFKey, "esc", "q":
		p.open = false
	case "ctrl+c":
		return tea.Quit
	case "down", "j":
		p.move(1)
	case "up", "k":
		p.move(-1)
	case "enter", "space":
		return p.flip(settingOptions[p.cursor])
	default:
		if i := slices.IndexFunc(settingOptions, func(o settingOption) bool { return o.key == k }); i >= 0 {
			p.move(i - p.cursor)
			return p.flip(settingOptions[i])
		}
	}
	return nil
}

// flip toggles option o and tells the page below; a problem saving it shows
// in the popup while that is open, else on the page.
func (p *SettingsPopup) flip(o settingOption) tea.Cmd {
	if o.number != nil {
		p.input, p.inputErr = &lineInput{text: strconv.Itoa(*o.number(p.cfg))}, ""
		return nil
	}
	if o.ask != nil {
		return o.ask
	}
	problem := toggle(p.cfg, o.setting(p.cfg))
	if p.open {
		p.msg, problem = problem, ""
	}
	_, cmd := p.Model.Update(settingChangedMsg{o.key, problem})
	return cmd
}

// editNumber edits the number asked for: enter sets and saves it, unless it is
// none, esc and backspace on nothing typed cancel.
func (p *SettingsPopup) editNumber(k tea.KeyPressMsg) tea.Cmd {
	in := p.input
	switch {
	case k.String() == "ctrl+c":
		return tea.Quit
	case in.cancels(k):
		p.input = nil
	case k.String() == "enter":
		n, err := strconv.Atoi(strings.TrimSpace(in.text))
		if err != nil || n < 0 {
			p.inputErr = "a whole number, 0 or more"
			return nil
		}
		*settingOptions[p.cursor].number(p.cfg) = n
		p.input, p.msg = nil, saveState(p.cfg)
	default:
		p.inputErr = ""
		in.edit(k)
	}
	return nil
}

// boxHeight is the height of the popup: all options when they fit, else the
// screen but a line above and below.
func (p *SettingsPopup) boxHeight() int {
	return min(len(settingOptions)+2, max(p.height-2, minBoxHeight))
}

func (p *SettingsPopup) move(delta int) {
	n := len(settingOptions)
	p.cursor = max(min(p.cursor+delta, n-1), 0)
	p.top = windowAround(p.cursor, p.top, p.boxHeight()-2, n).first
}

func (p *SettingsPopup) View() tea.View { return tea.NewView(p.frame()) }

func (p *SettingsPopup) frame() string {
	base := p.Model.View().Content
	if !p.open || p.width == 0 {
		return base
	}
	t := p.cfg.Theme
	n := len(settingOptions)
	all := make([]string, n)
	states := make([]string, n)
	keyW, stateW := 0, 0
	for i, o := range settingOptions {
		switch {
		case o.number != nil:
			states[i] = fmt.Sprintf("[%d]", *o.number(p.cfg))
		case *o.setting(p.cfg):
			states[i] = "[x]"
		default:
			states[i] = "[ ]"
		}
		keyW = max(keyW, ansi.StringWidth(o.key))
		stateW = max(stateW, ansi.StringWidth(states[i]))
	}
	widest := 0
	for i, o := range settingOptions {
		all[i] = fmt.Sprintf(" %-*s %-*s  %s ", stateW, states[i], keyW, o.key, o.what)
		widest = max(widest, ansi.StringWidth(all[i]))
	}
	w := windowAround(p.cursor, p.top, p.boxHeight()-2, n)
	lines := w.rows(t, n, func(i int) string {
		if i == p.cursor {
			return t.Cursor.Render(all[i])
		}
		return all[i]
	})
	title := popupTitle(t, "settings", p.msg, "enter toggle · esc close")
	width := min(max(widest+4, ansi.StringWidth(title)+4), p.width)
	view := overlay(base, box(t, width, p.boxHeight(), title, lines), width, p.width)
	if p.input == nil {
		return view
	}
	pop, popW := inputBox(t, p.width, popupTitle(t, "auto reload timeout", "", "enter set · esc cancel"), printable(p.input.text), p.inputErr, true)
	return overlay(view, pop, popW, p.width)
}
