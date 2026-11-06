package ui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
)

// settingOption is a line of the settings popup: the key that toggles it
// there and on the pages, what it does while on, and the toggle. With ask,
// the page below toggles it, when it can. With logFlow, its key works only in
// the log's flow view, where it shows; in the popup always.
type settingOption struct {
	key, what string
	setting   func(*config.Config) *bool
	ask       tea.Cmd
	logFlow   bool
}

var settingOptions = []settingOption{
	{key: "s", what: "side by side layout", setting: func(c *config.Config) *bool { return &c.Settings.SideBySide }},
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
}

// SettingsPopup wraps the top-level model and shows the display toggles in a
// popup over it on askSettingsMsg. It stays open after a toggle, so that
// several can be set while the page below shows them.
type SettingsPopup struct {
	tea.Model
	cfg           *config.Config
	open          bool
	cursor, top   int    // the selected option, the first one shown
	msg           string // a problem saving the toggles
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
		if p.open {
			return p, p.key(msg.String())
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
	keyW := 0
	for _, o := range settingOptions {
		keyW = max(keyW, ansi.StringWidth(o.key))
	}
	widest := 0
	for i, o := range settingOptions {
		state := "[ ]"
		if *o.setting(p.cfg) {
			state = "[x]"
		}
		all[i] = fmt.Sprintf(" %s %-*s  %s ", state, keyW, o.key, o.what)
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
	return overlay(base, box(t, width, p.boxHeight(), title, lines), width, p.width)
}
