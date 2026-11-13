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
	"jdiff/internal/keymap"
	"jdiff/internal/keys"
)

// settingOption is a line of the settings popup: the key that toggles it
// there and on the pages, the action of its row in keybindings.toml, what it
// does while on, from that row, and the toggle. With ask, the page below
// toggles it, when it can. With logFlow, its key works only in the log's flow
// view, where it shows; in the popup always. With number instead of the toggle,
// it is a number, asked for in a line; it has no key, the cursor goes to it in
// the popup. With patterns, it is ignored_hide, edited in a list of its own,
// see hidePopup. With keymap, it is the keys, in a list of their own, see
// keymapPopup; it has no key on the pages, in the popup keymapKey.
type settingOption struct {
	key, action, what string
	setting           func(*config.Config) *bool
	ask               tea.Cmd
	logFlow           bool
	number            func(*config.Config) *int
	patterns          bool
	keymap            bool
}

// settingTable lists the settings without their keys and words, which
// settingOptions takes from the keys.
var settingTable = []settingOption{
	{action: "layout", setting: func(c *config.Config) *bool { return &c.Settings.SideBySide }},
	{action: "flow", setting: func(c *config.Config) *bool { return &c.Settings.Flow }, ask: toggleFlow},
	{action: "untracked", setting: func(c *config.Config) *bool { return &c.Settings.Untracked }, ask: toggleUntracked},
	{action: "ignored", setting: func(c *config.Config) *bool { return &c.Settings.Ignored }, ask: toggleIgnored},
	{action: "excluded", setting: func(c *config.Config) *bool { return &c.Settings.Excluded }, ask: toggleExcluded},
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
	{action: "ignored_hide", patterns: true},
	{action: "keybindings", keymap: true},
}

// settingOptions returns the lines of the settings popup, with their keys and
// what they do from km, in its order.
func settingOptions(km *keys.Map) []settingOption {
	options, _ := orderSettings(km.FirstKeys(keys.Everywhere), settingWords(km, settingTable), nil)
	return options
}

// helpLines returns the lines of the help popup, from km, the settings in its
// order.
func helpLines(km *keys.Map) []binding {
	_, bs := orderSettings(km.FirstKeys(keys.Everywhere), settingWords(km, settingTable), helpBindings(km))
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
// the problems of its keys by themselves (keymap.Keymap.Problems).
func keyOrderProblems(text string) []string {
	p, err := keys.Parse(text)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	order := p.FirstKeys(keys.Everywhere)
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
	options []settingOption // of cfg.Keys, again after keymap changed them
	open    bool
	listPopup
	msg      string     // a problem saving the toggles
	input    *lineInput // while it asks for the number of the option, or a pattern of hide
	inputErr string     // why the number or pattern typed is none
	hide     hidePopup
	keymap   keymapPopup
}

// hidePopup is the list of the patterns of ignored_hide over the settings
// popup, which enter on its row opens: a adds a pattern and enter edits the
// selected one, typed in the line of the settings popup; d or del deletes
// it. A pattern is taken only when it is a regular expression, so that a
// typo changes nothing; each change is saved and lists the files anew.
type hidePopup struct {
	open bool
	listPopup
	edit int    // the pattern the line edits; len of the patterns for a new one
	msg  string // a problem saving them
}

// keymapPopup lists the rows of keybindings.toml with keys, page by page, over
// the settings popup, which K or enter on its line opens: enter takes the keys
// pressed next as the keys of the selected row, two for an action that goes
// two ways, a adds one to them, d or del gives its action on the page the
// built-in keys again. Each change is saved in the user's keybindings.toml
// and applies at once, unless the file would have a problem, as a key with
// two actions on one page: then it says so and nothing changes. D, after y
// to its question, moves the file to keybindings.toml.bak: all keys are the
// built-in ones.
type keymapPopup struct {
	open bool
	listPopup
	capture *keyCapture // while it takes the keys pressed
	reset   bool        // while it asks whether to move the file away
	msg     string      // why a change was not taken, or that the file was moved
}

// keyCapture is a change of the keys of line being pressed: want keys, in
// place of its keys or, with add, after them.
type keyCapture struct {
	line    keyLine
	add     bool
	want    int
	pressed []string
}

// keyLine is a line of the keymap popup: a row of a page, its index among the
// rows of its action there, and whether the user's keybindings.toml changed
// the keys of that action there.
type keyLine struct {
	page    string
	index   int
	row     keymap.Row
	changed bool
}

// keyLines returns the lines of the keymap popup of km: the rows with keys.
func keyLines(km *keys.Map) []keyLine {
	byAction := func(rows []keymap.Row) map[string][][]string {
		m := map[string][][]string{}
		for _, r := range rows {
			m[r.Action] = append(m[r.Action], r.Keys)
		}
		return m
	}
	var lines []keyLine
	for _, page := range km.Tables() {
		rows := km.Rows(page)
		mine, builtin := byAction(rows), byAction(keys.Builtin().Rows(page))
		index := map[string]int{}
		for _, r := range rows {
			i := index[r.Action]
			index[r.Action]++
			if len(r.Keys) > 0 {
				changed := !slices.EqualFunc(mine[r.Action], builtin[r.Action], slices.Equal)
				lines = append(lines, keyLine{page, i, r, changed})
			}
		}
	}
	return lines
}

// rowsOf returns the rows of action among rows.
func rowsOf(rows []keymap.Row, action string) []keymap.Row {
	var of []keymap.Row
	for _, r := range rows {
		if r.Action == action {
			of = append(of, r)
		}
	}
	return of
}

// WithSettings returns m with the settings popup.
func WithSettings(cfg *config.Config, m tea.Model) *SettingsPopup {
	return &SettingsPopup{Model: m, cfg: cfg, options: settingOptions(cfg.Keys)}
}

func (p *SettingsPopup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.hide.width, p.hide.height = msg.Width, msg.Height
		p.keymap.width, p.keymap.height = msg.Width, msg.Height
		p.move(0)
		p.moveHide(0)
		p.moveKeymap(0)
	case askSettingsMsg:
		p.open, p.cursor, p.top, p.msg = true, 0, 0, ""
		return p, nil
	case toggleSettingMsg:
		i := slices.IndexFunc(p.options, func(o settingOption) bool { return o.action == msg.action })
		return p, p.flip(p.options[i])
	case tea.KeyPressMsg:
		switch {
		case p.input != nil && p.hide.open:
			return p, p.editPattern(msg)
		case p.input != nil:
			return p, p.editNumber(msg)
		case p.hide.open:
			return p, p.hideKey(msg.String())
		case p.keymap.capture != nil:
			return p, p.captureKey(msg.String())
		case p.keymap.reset:
			return p, p.resetKey(msg.String())
		case p.keymap.open:
			return p, p.keymapKey(msg.String())
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
	case keymapKey:
		i := slices.IndexFunc(p.options, func(o settingOption) bool { return o.keymap })
		p.move(i - p.cursor)
		return p.flip(p.options[i])
	default:
		if i := slices.IndexFunc(p.options, func(o settingOption) bool { return o.key != "" && keyPress(o.key) == k }); i >= 0 {
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
	if o.patterns {
		p.hide.open, p.hide.cursor, p.hide.top, p.hide.msg = true, 0, 0, ""
		return nil
	}
	if o.keymap {
		p.keymap.open, p.keymap.cursor, p.keymap.top, p.keymap.msg = true, 0, 0, ""
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

func (p *SettingsPopup) moveHide(delta int) { p.hide.moveIn(delta, len(p.cfg.IgnoredHide)) }

// hideKey handles k in the list of ignored_hide: esc, q and the key of the
// settings go back to them.
func (p *SettingsPopup) hideKey(k string) tea.Cmd {
	if cmd, ok := popupNav(p.cfg.Keys, k, "settings", &p.hide.open, p.moveHide); ok {
		return cmd
	}
	list := p.cfg.IgnoredHide
	switch k {
	case "a":
		p.hide.edit, p.input, p.inputErr = len(list), &lineInput{}, ""
	case "enter":
		if p.hide.cursor < len(list) {
			p.hide.edit, p.input, p.inputErr = p.hide.cursor, &lineInput{text: list[p.hide.cursor]}, ""
		}
	case "d", "delete":
		if p.hide.cursor < len(list) {
			return p.setHide(slices.Delete(slices.Clone(list), p.hide.cursor, p.hide.cursor+1))
		}
	}
	return nil
}

// editPattern edits the pattern asked for: enter takes and saves it, unless
// it is empty, which would hide all, or no regular expression; esc and
// backspace on nothing typed cancel.
func (p *SettingsPopup) editPattern(k tea.KeyPressMsg) tea.Cmd {
	in := p.input
	switch {
	case k.String() == "ctrl+c":
		return tea.Quit
	case in.cancels(k):
		p.input = nil
	case k.String() == "enter":
		if in.text == "" {
			p.inputErr = "a regular expression; empty would hide all"
			return nil
		}
		if err := config.CheckPattern(in.text); err != nil {
			p.inputErr = "no regular expression: " + strings.TrimPrefix(firstLine(err), "error parsing regexp: ")
			return nil
		}
		list := slices.Clone(p.cfg.IgnoredHide)
		if p.hide.edit < len(list) {
			list[p.hide.edit] = in.text
		} else {
			list = append(list, in.text)
		}
		p.input, p.hide.cursor = nil, p.hide.edit
		return p.setHide(list)
	default:
		p.inputErr = ""
		in.edit(k)
	}
	return nil
}

// setHide makes list ignored_hide, saves it and tells the page below, which
// lists its files anew.
func (p *SettingsPopup) setHide(list []string) tea.Cmd {
	if err := p.cfg.SetIgnoredHide(list); err != nil { // checked when typed
		p.hide.msg = firstLine(err)
		return nil
	}
	p.hide.msg = saveState(p.cfg)
	p.moveHide(0)
	_, cmd := p.Model.Update(settingChangedMsg{action: "ignored_hide"})
	return cmd
}

// keymapKey opens the keymap popup in the settings popup, and only there.
const keymapKey = "K"

// optionKey returns the key of o as the settings popup shows it.
func optionKey(o settingOption) string {
	if o.keymap {
		return keymapKey
	}
	return keys.Spelling(o.key)
}

func (p *SettingsPopup) moveKeymap(delta int) { p.keymap.moveIn(delta, len(keyLines(p.cfg.Keys))) }

// keymapKey handles k in the list of the keys: esc, q and the key of the
// settings go back to them.
func (p *SettingsPopup) keymapKey(k string) tea.Cmd {
	if cmd, ok := popupNav(p.cfg.Keys, k, "settings", &p.keymap.open, p.moveKeymap); ok {
		return cmd
	}
	l := keyLines(p.cfg.Keys)[p.keymap.cursor]
	twoWay := p.cfg.Keys.TwoWay(l.row.Action)
	switch k {
	case "enter":
		p.keymap.capture, p.keymap.msg = &keyCapture{line: l, want: 1 + btoi(twoWay)}, ""
	case "a":
		if twoWay {
			p.keymap.msg = l.row.Action + " takes two keys, one each way: enter sets them"
			return nil
		}
		p.keymap.capture, p.keymap.msg = &keyCapture{line: l, add: true, want: 1}, ""
	case "d", "delete":
		if !l.changed {
			p.keymap.msg = l.row.Action + " in " + l.page + " has the built-in keys"
			return nil
		}
		p.setKeys(l.page, l.row.Action, nil)
	case "D":
		if !p.cfg.HasUserKeys() {
			p.keymap.msg = "no " + keys.FileName + ": the keys are the built-in ones"
			return nil
		}
		p.keymap.reset, p.keymap.msg = true, ""
	}
	return nil
}

// resetKey answers the question of D: y moves keybindings.toml away, n and
// esc keep it; other keys wait for an answer.
func (p *SettingsPopup) resetKey(k string) tea.Cmd {
	switch k {
	case "ctrl+c":
		return tea.Quit
	case "n", "esc":
		p.keymap.reset = false
	case "y":
		p.keymap.reset = false
		if err := p.cfg.ResetKeys(); err != nil {
			p.keymap.msg = firstLine(err)
			return nil
		}
		p.keymap.msg, p.options = keys.FileName+" is now "+keys.FileName+".bak: the built-in keys apply", settingOptions(p.cfg.Keys)
		p.moveKeymap(0)
	}
	return nil
}

// captureKey takes k as the next key of the capture, esc cancels it. With all
// keys pressed, the row of the capture takes them, unless one stands twice in
// the rows of its action.
func (p *SettingsPopup) captureKey(k string) tea.Cmd {
	c := p.keymap.capture
	if k == "esc" {
		p.keymap.capture = nil
		return nil
	}
	name := k
	if strings.HasSuffix(c.line.row.Keys[0], "<letter>") {
		name += "<letter>"
	}
	c.pressed = append(c.pressed, name)
	if len(c.pressed) < c.want {
		return nil
	}
	p.keymap.capture = nil
	var rows [][]string
	for _, r := range rowsOf(p.cfg.Keys.Rows(c.line.page), c.line.row.Action) {
		rows = append(rows, r.Keys)
	}
	if c.add {
		rows[c.line.index] = append(slices.Clone(rows[c.line.index]), c.pressed...)
	} else {
		rows[c.line.index] = c.pressed
	}
	var seen []string
	for _, ks := range rows {
		for _, name := range ks {
			if slices.Contains(seen, name) {
				p.keymap.msg = name + " stands twice in the keys of " + c.line.row.Action
				return nil
			}
			seen = append(seen, name)
		}
	}
	p.setKeys(c.line.page, c.line.row.Action, rows)
	return nil
}

// setKeys saves rows as the keys of action on page, which apply at once, or
// says why not.
func (p *SettingsPopup) setKeys(page, action string, rows [][]string) {
	if err := p.cfg.SetKeys(page, action, rows); err != nil {
		p.keymap.msg = firstLine(err)
		return
	}
	p.keymap.msg, p.options = "", settingOptions(p.cfg.Keys)
	p.moveKeymap(0)
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

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
		case o.patterns:
			states[i] = fmt.Sprintf("[%d]", len(p.cfg.IgnoredHide))
		case o.keymap:
			states[i] = fmt.Sprintf("[%d]", len(slices.DeleteFunc(keyLines(p.cfg.Keys), func(l keyLine) bool { return !l.changed })))
		case *o.setting(p.cfg):
			states[i] = "[x]"
		default:
			states[i] = "[ ]"
		}
		keyW = max(keyW, ansi.StringWidth(optionKey(o)))
		stateW = max(stateW, ansi.StringWidth(states[i]))
	}
	for i, o := range options {
		all[i] = fmt.Sprintf(" %-*s %-*s  %s ", stateW, states[i], keyW, optionKey(o), o.what)
	}
	view := p.draw(t, base, popupFrame{"settings", "enter toggle · K keys · esc close", p.msg}, all, "")
	name := strings.ReplaceAll(p.options[p.cursor].action, "_", " ")
	if p.hide.open {
		items := make([]string, len(p.cfg.IgnoredHide))
		for i, expr := range p.cfg.IgnoredHide {
			items[i] = " " + printable(expr) + " "
		}
		f := popupFrame{"ignored_hide", "a add · enter edit · d delete · esc back", p.hide.msg}
		view, name = p.hide.draw(t, view, f, items, "none: ignored files all show; a adds a pattern"), "ignored_hide pattern"
	}
	if p.keymap.open {
		view = p.keymapFrame(view)
	}
	if p.input == nil {
		return view
	}
	return inputPopup(t, p.width, popupFrame{name: name, hints: "enter set · esc cancel"}, printable(p.input.text), p.inputErr, true).over(t, view, p.width)
}

// keymapFrame draws the list of the keys over view, and the keys pressed
// while it takes them.
func (p *SettingsPopup) keymapFrame(view string) string {
	t := p.cfg.Theme
	f := popupFrame{"keybindings", "enter set · a add · d built-in · D all built-in · * changed · esc back", p.keymap.msg}
	view = p.keymap.draw(t, view, f, keymapItems(p.cfg.Keys, keyLines(p.cfg.Keys)), "")
	if p.keymap.reset {
		return p.keymapAsk(view, popupFrame{name: "built-in keys", hints: "y yes · n no"},
			"move "+keys.FileName+" to "+keys.FileName+".bak and use the built-in keys?")
	}
	c := p.keymap.capture
	if c == nil {
		return view
	}
	hint := "press the key · esc cancel"
	if c.want > 1 {
		hint = fmt.Sprintf("press key %d of %d, in the order of the help · esc cancel", len(c.pressed)+1, c.want)
	}
	f = popupFrame{name: c.line.row.Action + " in " + c.line.page, hints: hint}
	return inputPopup(t, p.width, f, strings.Join(keys.Shown(c.pressed), " "), "", true).over(t, view, p.width)
}

// keymapAsk draws over view what the list of the keys asks for: line in a
// popup in frame f, as wide as they need, up to the screen.
func (p *SettingsPopup) keymapAsk(view string, f popupFrame, line string) string {
	lines := []string{" " + line + " "}
	return popup{f, lines, popupWidth(f, lines, 0, p.width), len(lines) + 2}.over(p.cfg.Theme, view, p.width)
}

// keymapItems returns the items of the list of the keys of km for lines: a *
// for a changed row, its page, its keys and its help.
func keymapItems(km *keys.Map, lines []keyLine) []string {
	pageW, keysW := 0, 0
	for _, l := range lines {
		pageW = max(pageW, ansi.StringWidth(l.page))
		keysW = max(keysW, ansi.StringWidth(strings.Join(keys.Shown(l.row.Keys), " ")))
	}
	items := make([]string, len(lines))
	for i, l := range lines {
		mark := " "
		if l.changed {
			mark = "*"
		}
		items[i] = fmt.Sprintf(" %s %-*s  %-*s  %s ", mark, pageW, l.page, keysW, strings.Join(keys.Shown(l.row.Keys), " "), km.Text(l.row.Help))
	}
	return items
}
