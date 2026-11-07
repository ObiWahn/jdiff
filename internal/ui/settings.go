package ui

import (
	"cmp"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
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

var settingOptions = settingWords([]settingOption{
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
})

// settingWords returns options with their keys and what they do from
// keybindings.toml.
func settingWords(options []settingOption) []settingOption {
	for i := range options {
		r := keyFile.row(options[i].action)
		if len(r.Keys) > 0 {
			options[i].key = r.Keys[0]
		}
		options[i].what = keyFile.keyText(r.Setting)
	}
	return options
}

// keybindingsFile binds the keys of all pages to actions, gives their words and
// decides the order of the settings.
//
//go:embed keybindings.toml
var keybindingsFile string

// keyRow is a row of keybindings.toml.
type keyRow struct {
	Action  string
	Keys    []string
	Label   []string
	Help    string
	Setting string
}

// helpRow is a line of the help popup in keybindings.toml.
type helpRow struct{ Heading, Note, Action string }

// keyPages holds keybindings.toml: the rows of each page and the help's lines.
type keyPages struct {
	CommitList []keyRow  `toml:"commit_list"`
	FileList   []keyRow  `toml:"file_list"`
	DiffView   []keyRow  `toml:"diff_view"`
	FlowView   []keyRow  `toml:"flow_view"`
	Everywhere []keyRow  `toml:"everywhere"`
	Help       []helpRow `toml:"help"`
}

func readKeys(text string) (keyPages, error) {
	var p keyPages
	_, err := toml.Decode(text, &p)
	return p, err
}

// keyFile is keybindingsFile, read. It is part of the program, so that failing
// to read it is a bug, which TestKeybindingsFile finds.
var keyFile = func() keyPages {
	p, err := readKeys(keybindingsFile)
	if err != nil {
		panic(err)
	}
	return p
}()

// row returns the row that gives the keys and words of action; see
// keybindings.toml for the order. Without one, it has no keys.
func (p keyPages) row(action string) keyRow {
	if rows := p.rowsOf(action); len(rows) > 0 {
		return rows[0]
	}
	return keyRow{}
}

// rowsOf returns the rows of action in the first page that has it, in the
// order of row.
func (p keyPages) rowsOf(action string) []keyRow {
	for _, rows := range [][]keyRow{p.Everywhere, p.DiffView, p.FlowView, p.CommitList, p.FileList} {
		var of []keyRow
		for _, r := range rows {
			if r.Action == action {
				of = append(of, r)
			}
		}
		if len(of) > 0 {
			return of
		}
	}
	return nil
}

// placeholder matches {action} and {action:n} in the words of keybindings.toml.
var placeholder = regexp.MustCompile(`\{([a-z_]+)(?::([0-9]+))?\}`)

// keyText returns s with each placeholder replaced by the keys of its row, ""
// for a row that does not exist.
func (p keyPages) keyText(s string) string {
	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		sub := placeholder.FindStringSubmatch(m)
		n, _ := strconv.Atoi(sub[2])
		if rows := p.rowsOf(sub[1]); n < len(rows) {
			return strings.Join(rows[n].Keys, " ")
		}
		return ""
	})
}

// twoWay lists the actions whose rows take two keys, one for each way.
var twoWay = []string{"move", "page", "half_page", "ends", "scroll", "change", "next_file", "history", "match"}

// userRow is a row of the user's keybindings.toml, which changes only keys.
type userRow struct {
	Action string
	Keys   []string
}

// userKeys returns p with the keys of text, the user's keybindings.toml, and
// what is wrong with it. The rows of an action on a page replace all its rows
// there, keeping their words; a file with a problem changes nothing, so that
// a mistake cannot take away the keys to quit or get help.
func (p keyPages) userKeys(text string) (keyPages, []string) {
	var u struct {
		CommitList []userRow `toml:"commit_list"`
		FileList   []userRow `toml:"file_list"`
		DiffView   []userRow `toml:"diff_view"`
		FlowView   []userRow `toml:"flow_view"`
		Everywhere []userRow `toml:"everywhere"`
	}
	md, err := toml.Decode(text, &u)
	if err != nil {
		return p, []string{err.Error()}
	}
	var problems []string
	for _, k := range md.Undecoded() {
		problems = append(problems, "unknown "+k.String())
	}
	merged := p
	for _, t := range []struct {
		name string
		rows *[]keyRow
		user []userRow
	}{
		{"everywhere", &merged.Everywhere, u.Everywhere}, {"diff_view", &merged.DiffView, u.DiffView},
		{"flow_view", &merged.FlowView, u.FlowView}, {"commit_list", &merged.CommitList, u.CommitList},
		{"file_list", &merged.FileList, u.FileList},
	} {
		for _, r := range t.user {
			switch {
			case !slices.ContainsFunc(*t.rows, func(b keyRow) bool { return b.Action == r.Action }):
				problems = append(problems, "no action "+r.Action+" in "+t.name)
			case slices.Contains(twoWay, r.Action) && len(r.Keys) != 2:
				problems = append(problems, r.Action+" in "+t.name+" takes two keys, one each way")
			}
		}
		*t.rows = replaceKeys(*t.rows, t.user)
	}
	problems = append(problems, merged.conflicts()...)
	if len(problems) > 0 {
		return p, problems
	}
	return merged, nil
}

// replaceKeys returns rows with the rows of each action of user in place of
// its rows: the first where the first was, with the words of the row it
// replaces; rows of user beyond them have none.
func replaceKeys(rows []keyRow, user []userRow) []keyRow {
	var out []keyRow
	done := map[string]bool{}
	for _, r := range rows {
		if !slices.ContainsFunc(user, func(u userRow) bool { return u.Action == r.Action }) {
			out = append(out, r)
			continue
		}
		if done[r.Action] {
			continue
		}
		done[r.Action] = true
		var old []keyRow
		for _, o := range rows {
			if o.Action == r.Action {
				old = append(old, o)
			}
		}
		i := 0
		for _, u := range user {
			if u.Action != r.Action {
				continue
			}
			row := keyRow{Action: u.Action}
			if i < len(old) {
				row = old[i]
			}
			row.Keys = u.Keys
			out = append(out, row)
			i++
		}
	}
	return out
}

// KeysFile is the user's keybindings.toml in the configuration directory.
const KeysFile = "keybindings.toml"

// LoadKeys reads the user's keybindings.toml from the configuration directory
// of cfg, if there is one, over the built-in keys; a problem with it is a
// warning of cfg, and the built-in keys stay. Call it before the first page is
// made, which shows the warnings, and before OrderSettings.
func LoadKeys(cfg *config.Config) {
	if cfg.Dir() == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(cfg.Dir(), KeysFile))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	p, problems := keyFile, []string(nil)
	if err != nil {
		problems = []string{err.Error()}
	} else {
		p, problems = keyFile.userKeys(string(data))
	}
	if len(problems) > 0 {
		cfg.AddWarning(KeysFile + ": " + strings.Join(problems, "; ") + "; the built-in keys are used")
		return
	}
	keyFile = p
	settingOptions = settingWords(settingOptions)
	bindings = keyFile.bindings()
}

// keyPage is a page whose keys are looked up in keybindings.toml.
type keyPage int

const (
	commitList keyPage = iota
	fileList
	diffView
	flowView
	everywhere // the popups over all pages
)

// keyHit is what a key does: the action of its row, "" for none, and its place
// in the row, which gives the way of actions that go two ways (see
// keybindings.toml).
type keyHit struct {
	action string
	i      int
}

// tables returns the rows the keys of page pg are looked up in, in order.
func (p keyPages) tables(pg keyPage) [][]keyRow {
	switch pg {
	case commitList:
		return [][]keyRow{p.CommitList, p.Everywhere}
	case fileList:
		return [][]keyRow{p.FileList, p.Everywhere}
	case diffView:
		return [][]keyRow{p.DiffView, p.Everywhere}
	case flowView:
		return [][]keyRow{p.FlowView, p.DiffView, p.Everywhere}
	}
	return [][]keyRow{p.Everywhere}
}

// key returns what key k, as Bubble Tea names it, does on page pg.
func (p keyPages) key(pg keyPage, k string) keyHit {
	for _, rows := range p.tables(pg) {
		for _, r := range rows {
			for i, name := range r.Keys {
				if keyName(name) == k {
					return keyHit{r.Action, i}
				}
			}
		}
	}
	return keyHit{}
}

// isAction reports whether key k does action on all pages: the popups over
// them open and close on such keys.
func isAction(k, action string) bool { return keyFile.key(everywhere, k).action == action }

// keyName returns Bubble Tea's name of the key written as name in
// keybindings.toml, which writes pgdn and F5 as the help shows them, and
// <letter> for the letter that follows.
func keyName(name string) string {
	name = strings.TrimSuffix(name, "<letter>")
	if name == "pgdn" {
		return "pgdown"
	}
	if n, ok := strings.CutPrefix(name, "F"); ok && n != "" && strings.Trim(n, "0123456789") == "" {
		return "f" + n
	}
	return name
}

// label returns the word of the status bar in state, "" when there is none.
func (r keyRow) label(state int) string {
	if state < len(r.Label) {
		return r.Label[state]
	}
	return ""
}

// order returns the first key of each row of everywhere, which decides the
// order of the settings.
func (p keyPages) order() []string {
	var keys []string
	for _, r := range p.Everywhere {
		if len(r.Keys) > 0 {
			keys = append(keys, r.Keys[0])
		}
	}
	return keys
}

// orderSettings returns options and bindings with the settings in the order of
// keys. Options not in keys, as c and C, which keybindings.toml lists with the
// flow view, stay behind, as they are; the other bindings stay where they are.
func orderSettings(keys []string, options []settingOption, bs []binding) ([]settingOption, []binding) {
	rank := func(k string) int {
		if i := slices.Index(keys, k); i >= 0 {
			return i
		}
		return len(keys)
	}
	options = slices.Clone(options)
	slices.SortStableFunc(options, func(a, b settingOption) int { return cmp.Compare(rank(a.key), rank(b.key)) })

	isSetting := func(b binding) bool {
		return b.key != "" && slices.Contains(keys, b.key) && slices.ContainsFunc(options, func(o settingOption) bool { return o.key == b.key })
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

// OrderSettings puts the settings of the popup and the help in the order of
// keybindings.toml. Call it before the first view is drawn.
func OrderSettings() {
	settingOptions, bindings = orderSettings(keyFile.order(), settingOptions, bindings)
}

// keyOrderProblems returns what is wrong with text, a keybindings.toml: a row of
// all pages with a key that does not exist or stands twice, a setting missing or
// without words, a line of the help with an action that has no help, a key with
// two actions on one page, a placeholder without its row.
func keyOrderProblems(text string) []string {
	p, err := readKeys(text)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	keys := p.order()
	if len(keys) == 0 {
		return []string{"no rows in everywhere"}
	}
	for i, k := range keys {
		if slices.Contains(keys[:i], k) {
			problems = append(problems, "key "+k+" stands twice")
		}
		if !slices.ContainsFunc(bindings, func(b binding) bool { return slices.Contains(strings.Fields(b.key), k) }) {
			problems = append(problems, "key "+k+" is not in the help")
		}
	}
	for _, o := range settingOptions {
		if o.key != "" && !o.logFlow && !slices.Contains(keys, o.key) {
			problems = append(problems, "setting "+o.key+" is not in the table")
		}
	}
	for _, o := range settingOptions {
		if p.row(o.action).Setting == "" {
			problems = append(problems, "setting "+o.action+" has no words")
		}
	}
	problems = append(problems, p.conflicts()...)
	for _, r := range p.Help {
		if r.Action != "" && p.row(r.Action).Help == "" {
			problems = append(problems, "the help's action "+r.Action+" has no help")
		}
	}
	var words []string
	for _, rows := range [][]keyRow{p.Everywhere, p.DiffView, p.FlowView, p.CommitList, p.FileList} {
		for _, r := range rows {
			words = append(words, r.Help, r.Setting)
		}
	}
	for _, r := range p.Help {
		words = append(words, r.Note)
	}
	for _, w := range words {
		for _, m := range placeholder.FindAllStringSubmatch(w, -1) {
			n, _ := strconv.Atoi(m[2])
			if n >= len(p.rowsOf(m[1])) {
				problems = append(problems, "placeholder "+m[0]+" has no row")
			}
		}
	}
	return problems
}

// conflicts returns the keys with two actions on one page.
func (p keyPages) conflicts() []string {
	var problems []string
	for _, t := range []struct {
		name string
		rows []keyRow
	}{{"everywhere", p.Everywhere}, {"diff_view", p.DiffView}, {"flow_view", p.FlowView}, {"commit_list", p.CommitList}, {"file_list", p.FileList}} {
		actions := map[string]string{}
		for _, r := range t.rows {
			for _, name := range r.Keys {
				if a, ok := actions[keyName(name)]; ok && a != r.Action {
					problems = append(problems, "key "+name+" has two actions in "+t.name+": "+a+", "+r.Action)
				}
				actions[keyName(name)] = r.Action
			}
		}
	}
	return problems
}

// SettingsPopup wraps the top-level model and shows the display toggles in a
// popup over it on askSettingsMsg. It stays open after a toggle, so that
// several can be set while the page below shows them.
type SettingsPopup struct {
	tea.Model
	cfg  *config.Config
	open bool
	listPopup
	msg      string     // a problem saving the toggles
	input    *lineInput // while it asks for the number of the option
	inputErr string     // why the number typed is none
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
		i := slices.IndexFunc(settingOptions, func(o settingOption) bool { return o.action == msg.action })
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
			p.inputErr = ""
			p.input.text += msg.Content
			return p, nil
		}
	}
	_, cmd := p.Model.Update(msg)
	return p, cmd
}

// key handles k in the open popup.
func (p *SettingsPopup) key(k string) tea.Cmd {
	if isAction(k, "settings") {
		p.open = false
		return nil
	}
	switch k {
	case "esc", "q":
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
		if i := slices.IndexFunc(settingOptions, func(o settingOption) bool { return o.key != "" && keyName(o.key) == k }); i >= 0 {
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
		*settingOptions[p.cursor].number(p.cfg) = n
		p.input, p.msg = nil, saveState(p.cfg)
	default:
		p.inputErr = ""
		in.edit(k)
	}
	return nil
}

func (p *SettingsPopup) move(delta int) { p.moveIn(delta, len(settingOptions)) }

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
	for i, o := range settingOptions {
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
