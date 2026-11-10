package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/keys"
)

// settingOption is a line of the settings popup: the key that toggles it
// there and on the pages, the action of its row in keybindings.toml, what it
// does while on, from that row, and the toggle. With ask, the page below
// toggles it, when it can. With logFlow, its key works only in the log's flow
// view, where it shows; in the popup always. With number instead of the toggle,
// it is a number, asked for in a line; it has no key, the cursor goes to it in
// the popup.
type settingOption struct {
	key, action, what string
	setting           func(*config.Config) *bool
	ask               tea.Cmd
	logFlow           bool
	number            func(*config.Config) *int
}

// settingTable lists the settings without their keys and words, which
// settingOptions takes from the keys.
var settingTable = []settingOption{
	{action: "layout", setting: func(c *config.Config) *bool { return &c.Settings.SideBySide }},
	{action: "flow", setting: func(c *config.Config) *bool { return &c.Settings.Flow }, ask: toggleFlow},
	{action: "untracked", setting: func(c *config.Config) *bool { return &c.Settings.Untracked }, ask: toggleUntracked},
	{action: "wrap", setting: func(c *config.Config) *bool { return &c.Settings.Wrap }},
	{action: "whitespace", setting: func(c *config.Config) *bool { return &c.Settings.ShowWhitespaces }},
	{action: "mark_whitespace", setting: func(c *config.Config) *bool { return &c.Settings.MarkWhitespace }},
	{action: "trailing", setting: func(c *config.Config) *bool { return &c.Settings.ShowTrailing }},
	{action: "line_ends", setting: func(c *config.Config) *bool { return &c.Settings.ShowCR }},
	{action: "bold", setting: func(c *config.Config) *bool { return &c.Settings.BoldChanges }},
	{action: "preview", setting: func(c *config.Config) *bool { return &c.Settings.Preview }},
	{action: "full_messages", setting: func(c *config.Config) *bool { return &c.Settings.FullMessages }, logFlow: true},
	{action: "stay_in_commit", setting: func(c *config.Config) *bool { return &c.Settings.StayInCommit }, logFlow: true},
	{action: "auto_reload_timeout", number: func(c *config.Config) *int { return &c.Settings.AutoReloadTimeout }},
	{action: "inotify_max_dirs", number: func(c *config.Config) *int { return &c.Settings.InotifyMaxDirs }},
}

// settingOptions returns the lines of the settings popup, with their keys and
// what they do from km, in its order.
func settingOptions(km *keys.Map) []settingOption {
	options, _ := orderSettings(km.Order(), settingWords(km, settingTable), nil)
	return options
}

// helpLines returns the lines of the help popup, from km, the settings in its
// order.
func helpLines(km *keys.Map) []binding {
	_, bs := orderSettings(km.Order(), settingWords(km, settingTable), helpBindings(km))
	return bs
}

// settingWords returns options with their keys and what they do from km.
func settingWords(km *keys.Map, options []settingOption) []settingOption {
	options = slices.Clone(options)
	for i := range options {
		r := km.Row(options[i].action)
		if len(r.Keys) > 0 {
			options[i].key = r.Keys[0]
		}
		options[i].what = km.Text(r.Setting)
	}
	return options
}

// isAction reports whether key k does action on all pages of km: the popups
// over them open and close on such keys.
func isAction(km *keys.Map, k, action string) bool {
	return km.Key(keys.Everywhere, k).Action == action
}

// orderSettings returns options and bindings with the settings in the order of
// the keys order. Options not in it, as c and C, which keybindings.toml lists
// with the flow view, stay behind, as they are; the other bindings stay where
// they are.
func orderSettings(order []string, options []settingOption, bs []binding) ([]settingOption, []binding) {
	rank := func(k string) int {
		if i := slices.Index(order, k); i >= 0 {
			return i
		}
		return len(order)
	}
	options = slices.Clone(options)
	slices.SortStableFunc(options, func(a, b settingOption) int { return cmp.Compare(rank(a.key), rank(b.key)) })

	isSetting := func(b binding) bool {
		return b.key != "" && slices.Contains(order, b.key) && slices.ContainsFunc(options, func(o settingOption) bool { return o.key == b.key })
	}
	bs = slices.Clone(bs)
	var at []int
	var settings []binding
	for i, b := range bs {
		if isSetting(b) {
			at, settings = append(at, i), append(settings, b)
		}
	}
	slices.SortStableFunc(settings, func(a, b binding) int { return cmp.Compare(rank(a.key), rank(b.key)) })
	for i, b := range settings {
		bs[at[i]] = b
	}
	return options, bs
}

// keyOrderProblems returns what is wrong with text, a keybindings.toml, for the
// settings and the help of the built-in keys: a row of all pages with a key
// that does not exist or stands twice, a setting missing or without words, and
// the problems of its keys by themselves (keys.Map.Problems).
func keyOrderProblems(text string) []string {
	p, err := keys.Parse(text)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	order := p.Order()
	if len(order) == 0 {
		return []string{"no rows in everywhere"}
	}
	options, bindings := settingOptions(keys.Builtin()), helpLines(keys.Builtin())
	for i, k := range order {
		if slices.Contains(order[:i], k) {
			problems = append(problems, "key "+k+" stands twice")
		}
		if !slices.ContainsFunc(bindings, func(b binding) bool { return slices.Contains(strings.Fields(b.key), k) }) {
			problems = append(problems, "key "+k+" is not in the help")
		}
	}
	for _, o := range options {
		if o.key != "" && !o.logFlow && !slices.Contains(order, o.key) {
			problems = append(problems, "setting "+o.key+" is not in the table")
		}
	}
	for _, o := range options {
		if p.Row(o.action).Setting == "" {
			problems = append(problems, "setting "+o.action+" has no words")
		}
	}
	return append(problems, p.Problems()...)
}

// SettingsPopup wraps the top-level model and shows the display toggles in a
// popup over it on askSettingsMsg. It stays open after a toggle, so that
// several can be set while the page below shows them.
type SettingsPopup struct {
	tea.Model
	cfg     *config.Config
	options []settingOption // of cfg.Keys, which do not change
	open    bool
	listPopup
	msg      string     // a problem saving the toggles
	input    *lineInput // while it asks for the number of the option
	inputErr string     // why the number typed is none
}

// WithSettings returns m with the settings popup.
func WithSettings(cfg *config.Config, m tea.Model) *SettingsPopup {
	return &SettingsPopup{Model: m, cfg: cfg, options: settingOptions(cfg.Keys)}
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
		i := slices.IndexFunc(p.options, func(o settingOption) bool { return o.action == msg.action })
		return p, p.flip(p.options[i])
	case tea.KeyPressMsg:
		switch {
		case p.input != nil:
			return p, p.editNumber(msg)
		case p.open:
			return p, p.key(msg.String())
		}
	case tea.PasteMsg:
		if p.input != nil {
			p.inputErr = ""
			p.input.paste(msg.Content)
			return p, nil
		}
	}
	_, cmd := p.Model.Update(msg)
	return p, cmd
}

// key handles k in the open popup.
func (p *SettingsPopup) key(k string) tea.Cmd {
	if cmd, ok := popupNav(p.cfg.Keys, k, "settings", &p.open, p.move); ok {
		return cmd
	}
	switch k {
	case "enter", "space":
		return p.flip(p.options[p.cursor])
	default:
		if i := slices.IndexFunc(p.options, func(o settingOption) bool { return o.key != "" && keys.Name(o.key) == k }); i >= 0 {
			p.move(i - p.cursor)
			return p.flip(p.options[i])
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
	_, cmd := p.Model.Update(settingChangedMsg{o.action, problem})
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
		o := p.options[p.cursor]
		*o.number(p.cfg) = n
		p.input, p.msg = nil, saveState(p.cfg)
		_, cmd := p.Model.Update(settingChangedMsg{action: o.action})
		return cmd
	default:
		p.inputErr = ""
		in.edit(k)
	}
	return nil
}

func (p *SettingsPopup) move(delta int) { p.moveIn(delta, len(p.options)) }

func (p *SettingsPopup) View() tea.View { return tea.NewView(p.frame()) }

func (p *SettingsPopup) frame() string {
	base := p.Model.View().Content
	if !p.open || p.width == 0 {
		return base
	}
	t := p.cfg.Theme
	options := p.options
	n := len(options)
	all := make([]string, n)
	states := make([]string, n)
	keyW, stateW := 0, 0
	for i, o := range options {
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
	for i, o := range options {
		all[i] = fmt.Sprintf(" %-*s %-*s  %s ", stateW, states[i], keyW, o.key, o.what)
	}
	title := popupTitle(t, "settings", p.msg, "enter toggle · esc close")
	view := p.draw(t, base, title, all, "")
	if p.input == nil {
		return view
	}
	pop, popW := inputBox(t, p.width, popupTitle(t, "auto reload timeout", "", "enter set · esc cancel"), printable(p.input.text), p.inputErr, true)
	return overlay(view, pop, popW, p.width)
}
