package ui

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/gitsrc"
	"jdiff/internal/keymap"
	"jdiff/internal/keys"
	"jdiff/internal/source"
)

// setOption presses keys in the settings popup put around m, as a user toggles
// the options of a page.
func setOption(m tea.Model, cfg *config.Config, keys ...string) {
	s := WithSettings(cfg, m)
	s.Update(askSettingsMsg{})
	pressRoot(s, keys...)
}

// optionValues returns the values of all options.
func optionValues(cfg *config.Config) []bool {
	var out []bool
	for _, o := range settingOptions(keys.Builtin()) {
		if o.setting == nil {
			continue // a number: see TestSettingsNumber; the patterns: TestSettingsIgnoredHide
		}
		out = append(out, *o.setting(cfg))
	}
	return out
}

// settingsPages returns a view of every page, with the keys that lead to it.
func settingsPages() []struct {
	name string
	make func() (tea.Model, *config.Config)
	keys []string
} {
	app := func() (tea.Model, *config.Config) { d := newFlowDiff(); return d, d.cfg }
	log := func() (tea.Model, *config.Config) { l, _ := newTestLog(); return l, l.cfg }
	return []struct {
		name string
		make func() (tea.Model, *config.Config)
		keys []string
	}{
		{"commit list", log, nil},
		{"files of a commit", log, []string{"enter"}},
		{"file list", app, nil},
		{"diff", app, []string{"enter"}},
		{"flow", app, []string{"f"}},
		{"log flow", log, []string{"f"}},
		{"single diff", func() (tea.Model, *config.Config) { m := newMarkFileDiff(); return m, m.cfg }, nil},
	}
}

func TestSettingsOpenOnAllPages(t *testing.T) {
	for _, page := range settingsPages() {
		for _, opener := range []string{"o", "f8"} {
			m, cfg := page.make()
			s := WithSettings(cfg, m)
			s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			pressRoot(s, page.keys...)
			pressRoot(s, opener)
			if !s.open {
				t.Errorf("%s: %s does not open the settings", page.name, opener)
			}
		}
	}
}

func TestSettingsOptions(t *testing.T) {
	tomlKeys := map[string]string{"s": "side_by_side", "v": "preview", "W": "show_whitespaces", "ctrl+w": "mark_whitespace",
		"t": "show_trailing", "e": "show_cr", "b": "bold_changes", "w": "wrap", "c": "full_messages", "C": "stay_in_commit"}
	for i, o := range settingOptions(keys.Builtin()) {
		if o.ask != nil || o.number != nil || o.patterns || o.keymap {
			continue // the page toggles it: see TestUntracked...; a number: see TestSettingsNumber; the patterns: TestSettingsIgnoredHide; the keys: TestSettingsKeymap
		}
		dir := t.TempDir()
		cfg, err := config.Load(dir, nil, "", "")
		if err != nil {
			t.Fatal(err)
		}
		s := WithSettings(cfg, newTestFileDiff(numbered("x", 10), numbered("y", 10)))
		s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		was := *o.setting(cfg)
		pressRoot(s, "o", o.key)
		if *o.setting(cfg) == was || !s.open || s.cursor != i {
			t.Errorf("%s: value %v (was %v), open %v, cursor %d; want it flipped, open, on %d", o.key, *o.setting(cfg), was, s.open, s.cursor, i)
		}
		data, err := os.ReadFile(filepath.Join(dir, "state.toml"))
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf("%s = %v", tomlKeys[o.key], !was); !strings.Contains(string(data), want) {
			t.Errorf("%s: state.toml lacks %q:\n%s", o.key, want, data)
		}
		// Back with enter, then again with space, on the selected option.
		pressRoot(s, "enter")
		if *o.setting(cfg) != was {
			t.Errorf("%s: enter does not flip it back", o.key)
		}
		pressRoot(s, "space")
		if *o.setting(cfg) == was {
			t.Errorf("%s: space does not flip it", o.key)
		}
	}
}

func TestSettingsMoveAndClose(t *testing.T) {
	d := newTestDiff()
	s := WithSettings(d.cfg, d)
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	pressRoot(s, "o", "j", "j", "k")
	if s.cursor != 1 || d.cursor != 0 {
		t.Errorf("j j k: popup cursor %d, want 1; list cursor %d, want 0: keys must not reach the page", s.cursor, d.cursor)
	}
	pressRoot(s, "k", "k", "k")
	if s.cursor != 0 {
		t.Errorf("past the first option: cursor %d", s.cursor)
	}
	for range len(settingOptions(keys.Builtin())) + 3 {
		pressRoot(s, "j")
	}
	if s.cursor != len(settingOptions(keys.Builtin()))-1 {
		t.Errorf("past the last option: cursor %d", s.cursor)
	}
	pressRoot(s, "esc")
	for _, k := range []string{"o", "f8", "esc", "q"} {
		pressRoot(s, "o")
		pressRoot(s, k)
		if s.open {
			t.Errorf("%s does not close the settings", k)
		}
	}
	pressRoot(s, "o")
	if _, cmd := s.Update(keyMsg("ctrl+c")); cmd == nil {
		t.Errorf("ctrl+c does not quit in the settings")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c in the settings: %T, want a quit", cmd())
	}
}

// r, F5 and ctrl+r load again on every page, and R does not.
func TestReloadKeysOnAllPages(t *testing.T) {
	for _, page := range settingsPages() {
		shown := func(k string) bool {
			m, cfg := page.make()
			s := WithSettings(cfg, m)
			s.Update(tea.WindowSizeMsg{Width: 200, Height: 24})
			pressRoot(s, page.keys...)
			pressRoot(s, k)
			return strings.Contains(ansi.Strip(s.frame()), "nothing to load again")
		}
		for _, k := range []string{"r", "f5", "ctrl+r"} {
			if !shown(k) {
				t.Errorf("%s: %s does not load again", page.name, k)
			}
		}
		if shown("R") {
			t.Errorf("%s: R loads again", page.name)
		}
	}
}

// The letters of the options do nothing on the pages themselves.
func TestSettingsKeysLeftThePages(t *testing.T) {
	for _, page := range settingsPages() {
		m, cfg := page.make()
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		pressRoot(m, page.keys...)
		before := fmt.Sprint(optionValues(cfg))
		for _, o := range settingOptions(keys.Builtin()) {
			if o.key != "f" { // the pages' own key: see TestSettingsFlow
				pressRoot(m, o.key)
			}
		}
		if got := fmt.Sprint(optionValues(cfg)); got != before {
			t.Errorf("%s: the letters changed the options: %s, was %s", page.name, got, before)
		}
	}
}

// C says so in the status bar, below the popup, as the only option with a
// message of its own.
func TestSettingsStayInCommitMessage(t *testing.T) {
	l, _ := newTestLog()
	s := WithSettings(l.cfg, l)
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	pressRoot(s, "f", "o", "C")
	if !strings.Contains(ansi.Strip(s.frame()), "staying in commit") {
		t.Errorf("C in the popup does not say so:\n%s", ansi.Strip(s.frame()))
	}
}

// o is a mark letter too: after m and ' it names the mark, not the popup.
func TestSettingsKeyAsMarkLetter(t *testing.T) {
	m := newMarkFileDiff()
	s := WithSettings(m.cfg, m)
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	line := m.lineAt(right, m.cursor)
	pressRoot(s, "m", "o")
	if s.open {
		t.Errorf("m o opened the settings")
	}
	if mk, ok := m.cfg.Mark("o"); !ok || mk.Line != line+1 {
		t.Errorf("m o: mark %+v, set %v; want line %d", mk, ok, line+1)
	}
	pressRoot(s, "end", "'", "o")
	if s.open {
		t.Errorf("' o opened the settings")
	}
	if got := m.lineAt(right, m.cursor); got != line {
		t.Errorf("' o: on line %d, want %d", got, line)
	}
	if pressRoot(s, "o"); !s.open {
		t.Errorf("o after the marks does not open the settings")
	}
}

// The flow view lays out its rows anew on a toggle while the popup stays open
// over it.
func TestSettingsWrapInFlowBelowPopup(t *testing.T) {
	probe := newTestFileDiff([]string{"a"}, []string{"b"})
	full := strings.Repeat("x", probe.contentWidth(80, false)+5)
	d := NewDiff(config.Default(), []source.File{modified("a.go", []string{full, "old"}, []string{full, "new"})})
	d.cfg.Settings.CollapseUnchanged = false
	s := WithSettings(d.cfg, d)
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressRoot(s, "f", "o")
	before := len(d.flow.rows)
	pressRoot(s, "w")
	if len(d.flow.rows) <= before {
		t.Errorf("w in the popup: %d flow rows, %d before; want more for the wrapped lines", len(d.flow.rows), before)
	}
	if !strings.Contains(s.frame(), "settings") {
		t.Errorf("the popup closed on w:\n%s", s.frame())
	}
}

func TestSettingsPopupSweep(t *testing.T) {
	t.Parallel() // a sweep: its own config and views, nothing shared
	n := len(settingOptions(keys.Builtin()))
	// Every height from the smallest drawn past "all fit" at n+4; a narrow
	// terminal keeps drawing fast, and 30 columns still show the key of a line.
	for height := minHeight; height <= n+6; height++ {
		t.Run(fmt.Sprint("height ", height), func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			s := WithSettings(cfg, newTestFileDiff(numbered("x", 40), numbered("y", 40)))
			s.Update(tea.WindowSizeMsg{Width: 30, Height: height})
			pressRoot(s, "o")
			rows := max(min(n, height-4), 1)
			check := func(step int) {
				t.Helper()
				view := s.frame()
				if got := len(strings.Split(view, "\n")); got > height {
					t.Fatalf("step %d: %d lines", step, got)
				}
				if line := rawText(view, 30); line != "" {
					t.Fatalf("step %d: line too wide: %q", step, line)
				}
				got := popupRows(view)
				if len(got) != rows {
					t.Fatalf("step %d: %d rows, want %d: %q", step, len(got), rows, got)
				}
				// The states are padded to the widest, a number.
				cursorOption := regexp.MustCompile(`\] +` + regexp.QuoteMeta(settingOptions(keys.Builtin())[s.cursor].key) + ` `)
				if !cursorOption.MatchString(strings.Join(got, "\n")) {
					t.Fatalf("step %d: option %d out of view: %q", step, s.cursor, got)
				}
			}
			check(0)
			for step := 1; step < 2*n; step++ {
				k := "j"
				if step >= n {
					k = "k"
				}
				pressRoot(s, k)
				check(step)
			}
		})
	}
}

// openIgnoredHide returns the settings popup over the file list of a git
// diff with the untracked and ignored files shown, its list of ignored_hide
// open; relists counts the times the files are listed anew.
func openIgnoredHide(t *testing.T, width, height int) (s *SettingsPopup, dir string, relists *int) {
	t.Helper()
	dir = t.TempDir()
	cfg, err := config.Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	d := newUntrackedDiff()
	d.cfg = cfg
	cfg.Settings.Untracked, cfg.Settings.Ignored = true, true
	list, n := d.gitDiff, 0
	d.gitDiff = func(ctx context.Context, args []string, extra gitsrc.Extra) ([]source.File, error) {
		n++
		return list(ctx, args, extra)
	}
	s = WithSettings(cfg, d)
	s.Update(tea.WindowSizeMsg{Width: width, Height: height})
	pressRoot(s, "o")
	for range slices.IndexFunc(s.options, func(o settingOption) bool { return o.patterns }) {
		pressRoot(s, "j")
	}
	pressRoot(s, "enter")
	if !s.hide.open {
		t.Fatal("enter on ignored_hide: its list did not open")
	}
	return s, dir, &n
}

// The list of ignored_hide: a adds a pattern, enter edits one, d deletes it;
// each change is saved and lists the files anew. What is no regular
// expression, or empty, stays asked for and changes nothing. esc goes back
// to the settings.
func TestSettingsIgnoredHide(t *testing.T) {
	s, dir, relists := openIgnoredHide(t, 100, 40)
	cfg := s.cfg
	n := len(config.DefaultIgnoredHide)
	if view := ansi.Strip(s.frame()); !strings.Contains(view, "ignored_hide") || !strings.Contains(view, "(^|/)node_modules/$") {
		t.Errorf("the list does not show:\n%s", view)
	}
	saved := func() string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, "state.toml"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	pressRoot(s, "a")
	s.Update(tea.PasteMsg{Content: "^x0/$"})
	pressRoot(s, "enter")
	if s.input != nil || len(cfg.IgnoredHide) != n+1 || cfg.IgnoredHide[n] != "^x0/$" || s.hide.cursor != n || *relists != 1 {
		t.Fatalf("a ^x0/$: input %v, %q, cursor %d, %d relists", s.input, cfg.IgnoredHide, s.hide.cursor, *relists)
	}
	if !strings.Contains(saved(), "    '^x0/$',\n]") || !cfg.IgnoredHidden("x0/") {
		t.Errorf("^x0/$ not saved or not used:\n%s", saved())
	}

	for _, bad := range []struct{ text, err string }{{"(", "no regular expression: missing closing ): `(`"}, {"", "empty would hide all"}} {
		pressRoot(s, "a")
		s.Update(tea.PasteMsg{Content: bad.text})
		pressRoot(s, "enter")
		if s.input == nil || !strings.Contains(s.inputErr, bad.err) || len(cfg.IgnoredHide) != n+1 || *relists != 1 {
			t.Errorf("%q: input %v, err %q, %d patterns, %d relists", bad.text, s.input, s.inputErr, len(cfg.IgnoredHide), *relists)
		}
		if view := ansi.Strip(s.frame()); !strings.Contains(view, bad.err) {
			t.Errorf("%q: the error does not show:\n%s", bad.text, view)
		}
		pressRoot(s, "esc")
		if s.input != nil || !s.hide.open {
			t.Errorf("%q: esc: input %v, list open %v", bad.text, s.input, s.hide.open)
		}
	}

	pressRoot(s, "enter") // on ^x0/$
	if s.input == nil || s.input.text != "^x0/$" {
		t.Fatalf("enter: input %+v, want ^x0/$ to edit", s.input)
	}
	pressRoot(s, "ctrl+u")
	s.Update(tea.PasteMsg{Content: "^x1/$"})
	pressRoot(s, "enter")
	if len(cfg.IgnoredHide) != n+1 || cfg.IgnoredHide[n] != "^x1/$" || cfg.IgnoredHidden("x0/") || *relists != 2 {
		t.Errorf("edited: %q, %d relists", cfg.IgnoredHide, *relists)
	}

	pressRoot(s, "d")
	if len(cfg.IgnoredHide) != n || s.hide.cursor != n-1 || *relists != 3 || strings.Contains(saved(), "x1") {
		t.Errorf("d: %d patterns, cursor %d, %d relists:\n%s", len(cfg.IgnoredHide), s.hide.cursor, *relists, saved())
	}
	for range n {
		pressRoot(s, "delete")
	}
	if len(cfg.IgnoredHide) != 0 || !strings.Contains(saved(), "ignored_hide = []") || cfg.IgnoredHidden("node_modules/") {
		t.Errorf("all deleted: %q:\n%s", cfg.IgnoredHide, saved())
	}
	if view := ansi.Strip(s.frame()); !strings.Contains(view, "none: ignored files all show") {
		t.Errorf("no hint without patterns:\n%s", view)
	}
	pressRoot(s, "d", "enter") // nothing to delete or edit
	if s.input != nil || *relists != 3+n {
		t.Errorf("d, enter on nothing: input %v, %d relists", s.input, *relists)
	}

	pressRoot(s, "esc")
	if s.hide.open || !s.open {
		t.Errorf("esc: list open %v, settings open %v; want back in the settings", s.hide.open, s.open)
	}
	if view := ansi.Strip(s.frame()); !strings.Contains(view, "[0]") {
		t.Errorf("the settings do not show [0] patterns:\n%s", view)
	}
}

func TestSettingsIgnoredHideSweep(t *testing.T) {
	t.Parallel() // a sweep: its own config and views, nothing shared
	const n = 30 // more than the settings, so that the list's box stands out on top
	index := func(r string) int {
		var k int
		fmt.Sscanf(strings.TrimSpace(r), "^x%d", &k)
		return k
	}
	// Every height from the smallest drawn past "all fit" at n+4; a narrow
	// terminal keeps drawing fast.
	for height := minHeight; height <= n+6; height++ {
		t.Run(fmt.Sprint("height ", height), func(t *testing.T) {
			t.Parallel()
			s, _, _ := openIgnoredHide(t, 40, height)
			var list []string
			for i := range n {
				list = append(list, fmt.Sprintf("^x%02d", i))
			}
			if err := s.cfg.SetIgnoredHide(list); err != nil {
				t.Fatal(err)
			}
			what := fmt.Sprintf("ignored_hide, height %d", height)
			h := popupHeight(n, height) - 2
			check := func(k string, shown []int) []int {
				t.Helper()
				view := s.frame()
				if got := len(strings.Split(view, "\n")); got > height {
					t.Fatalf("%s, %s: %d lines", what, k, got)
				}
				if line := rawText(view, 40); line != "" {
					t.Fatalf("%s, %s: line too wide: %q", what, k, line)
				}
				return checkShown(t, what+", "+k, popupRows(view), h, n, s.hide.cursor, shown, index)
			}
			shown := check("", nil)
			for _, k := range append(slices.Repeat([]string{"j"}, n), slices.Repeat([]string{"k"}, n)...) {
				pressRoot(s, k)
				shown = check(k, shown)
			}
		})
	}
}

// openKeymap returns the settings popup over a diff, width × height, with the
// list of the keys open, and the configuration directory, in which
// keybindings.toml holds text.
func openKeymap(t *testing.T, width, height int, text string) (*SettingsPopup, string) {
	t.Helper()
	dir := t.TempDir()
	if text != "" {
		if err := os.WriteFile(filepath.Join(dir, keys.FileName), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	s := WithSettings(cfg, NewFileDiff(cfg, "old", "new", numbered("x", 10), numbered("y", 10)))
	s.Update(tea.WindowSizeMsg{Width: width, Height: height})
	pressRoot(s, "o", "K")
	if !s.keymap.open {
		t.Fatal("K in the settings: the keys did not open")
	}
	return s, dir
}

// keymapTo moves the cursor of the list of the keys to the row of action on
// page with key.
func keymapTo(t *testing.T, s *SettingsPopup, page, action, key string) {
	t.Helper()
	i := slices.IndexFunc(keyLines(s.cfg.Keys), func(l keyLine) bool {
		return l.page == page && l.row.Action == action && slices.Contains(l.row.Keys, key)
	})
	if i < 0 {
		t.Fatalf("no row of %s with %s in %s", action, key, page)
	}
	s.keymap.cursor = i
	s.moveKeymap(0)
}

func TestSettingsKeymap(t *testing.T) {
	const mine = "# mine\ndiff_view = [\n    { action = \"fold\",   # z\n      keys = [\"z\"] },\n]\n"
	s, dir := openKeymap(t, 120, 40, mine)
	cfg := s.cfg
	saved := func() string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, keys.FileName))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	view := func() string { return ansi.Strip(s.frame()) }
	if v := view(); !strings.Contains(v, "keybindings") || !strings.Contains(v, "* diff_view") {
		t.Errorf("the list does not show, or not fold as changed:\n%s", v)
	}

	keymapTo(t, s, "diff_view", "pane", "tab")
	pressRoot(s, "enter")
	if s.keymap.capture == nil || !strings.Contains(view(), "pane in diff_view") {
		t.Fatalf("enter: no capture:\n%s", view())
	}
	pressRoot(s, "x")
	if s.keymap.capture != nil || cfg.Keys.Key(keys.DiffView, "x").Action != "pane" || cfg.Keys.Key(keys.DiffView, "tab").Action != "" {
		t.Errorf("x: capture %v, x %+v, tab %+v", s.keymap.capture, cfg.Keys.Key(keys.DiffView, "x"), cfg.Keys.Key(keys.DiffView, "tab"))
	}
	if want := mine + "diff_view = [\n"; strings.HasPrefix(saved(), want) || !strings.HasPrefix(saved(), "# mine\ndiff_view = [\n    { action = \"fold\",   # z\n      keys = [\"z\"] },\n    { action = \"pane\", keys = [\"x\"] },\n]\n") {
		t.Errorf("x not saved, or the file not kept:\n%s", saved())
	}

	pressRoot(s, "enter", "j") // j moves
	if !strings.Contains(s.keymap.msg, "key j has two actions in diff_view: move, pane") || cfg.Keys.Key(keys.DiffView, "x").Action != "pane" {
		t.Errorf("a conflict: msg %q, x %+v", s.keymap.msg, cfg.Keys.Key(keys.DiffView, "x"))
	}
	if !strings.Contains(view(), "key j has two actions") {
		t.Errorf("the conflict does not show:\n%s", view())
	}
	pressRoot(s, "a", "y")
	if r := rowsOf(cfg.Keys.Rows("diff_view"), "pane"); len(r) != 1 || !slices.Equal(r[0].Keys, []string{"x", "y"}) || s.keymap.msg != "" {
		t.Errorf("a y: %+v, msg %q", r, s.keymap.msg)
	}
	pressRoot(s, "a", "x")
	if !strings.Contains(s.keymap.msg, "x stands twice") {
		t.Errorf("a x: msg %q", s.keymap.msg)
	}
	pressRoot(s, "enter", "esc")
	if s.keymap.capture != nil || !s.keymap.open || cfg.Keys.Key(keys.DiffView, "x").Action != "pane" {
		t.Errorf("esc: capture %v, open %v; want it cancelled, nothing changed", s.keymap.capture, s.keymap.open)
	}

	// Two ways: two keys, in the order of the row.
	keymapTo(t, s, "diff_view", "change", "]")
	pressRoot(s, "enter", "f8")
	if v := view(); s.keymap.capture == nil || !strings.Contains(v, "key 2 of 2") || !strings.Contains(v, " F8█ ") {
		t.Fatalf("f8: want the second key asked for:\n%s", v)
	}
	pressRoot(s, "ctrl+g")
	if r := rowsOf(cfg.Keys.Rows("diff_view"), "change"); len(r) != 1 || !slices.Equal(r[0].Keys, []string{"f8", "ctrl+g"}) {
		t.Errorf("f8 ctrl+g: %+v, msg %q", r, s.keymap.msg)
	}
	if !strings.Contains(saved(), `{ action = "change", keys = ["f8", "ctrl+g"] }`) {
		t.Errorf("not saved as Bubble Tea names the keys:\n%s", saved())
	}
	pressRoot(s, "a")
	if !strings.Contains(s.keymap.msg, "change takes two keys") || s.keymap.capture != nil {
		t.Errorf("a on two ways: msg %q", s.keymap.msg)
	}

	keymapTo(t, s, "diff_view", "set_mark", "m<letter>")
	pressRoot(s, "enter", "M")
	if r := cfg.Keys.Row("set_mark"); !slices.Equal(r.Keys, []string{"M<letter>"}) {
		t.Errorf("M on set_mark: %q", r.Keys)
	}

	// Back to the built-in keys; the rest of the file stays.
	keymapTo(t, s, "diff_view", "pane", "x")
	pressRoot(s, "d")
	if cfg.Keys.Key(keys.DiffView, "tab").Action != "pane" || strings.Contains(saved(), "pane") || !strings.HasPrefix(saved(), mine[:len(mine)-2]) {
		t.Errorf("d: tab %+v:\n%s", cfg.Keys.Key(keys.DiffView, "tab"), saved())
	}
	pressRoot(s, "delete")
	if !strings.Contains(s.keymap.msg, "has the built-in keys") {
		t.Errorf("del on a built-in row: msg %q", s.keymap.msg)
	}

	// The keys apply at once: on the page, in the settings and in the help.
	keymapTo(t, s, "everywhere", "wrap", "w")
	pressRoot(s, "enter", "Z")
	if v := view(); !regexp.MustCompile(`\* everywhere +Z`).MatchString(v) { // the page column is as wide as the longest table
		t.Errorf("Z: not marked as changed:\n%s", v)
	}
	pressRoot(s, "esc")
	if s.keymap.open || !s.open {
		t.Fatalf("esc: keys open %v, settings open %v; want back in the settings", s.keymap.open, s.open)
	}
	if v := view(); !strings.Contains(v, "Z       wrap long lines") || !strings.Contains(v, "[4]   K       the keys") {
		t.Errorf("the settings do not show Z, or not 4 rows changed:\n%s", v)
	}
	pressRoot(s, "esc")
	wrap := cfg.Settings.Wrap
	pressRoot(s, "Z")
	if cfg.Settings.Wrap == wrap {
		t.Errorf("Z on the page does not toggle wrap")
	}
	h := WithHelp(cfg, s)
	h.Update(tea.WindowSizeMsg{Width: 120, Height: 100})
	pressRoot(h, "?")
	if v := ansi.Strip(h.frame()); !strings.Contains(v, "Z              wrap long lines") {
		t.Errorf("the help does not show Z:\n%s", v)
	}
}

// D asks, and y moves keybindings.toml to keybindings.toml.bak: the built-in
// keys apply at once; n and esc keep it.
func TestSettingsKeymapReset(t *testing.T) {
	const mine = "# mine\neverywhere = [{ action = \"wrap\", keys = [\"Z\"] }]\n"
	s, dir := openKeymap(t, 120, 40, mine)
	cfg := s.cfg
	path := filepath.Join(dir, keys.FileName)
	for _, no := range []string{"n", "esc"} {
		pressRoot(s, "D")
		if v := ansi.Strip(s.frame()); !s.keymap.reset || !strings.Contains(v, "keybindings.toml.bak and use the built-in keys?") {
			t.Fatalf("D: no question:\n%s", v)
		}
		pressRoot(s, "j", no) // j is no answer
		if s.keymap.reset || !cfg.HasUserKeys() || cfg.Keys.Row("wrap").Keys[0] != "Z" || s.keymap.cursor != 0 {
			t.Errorf("D j %s: asking %v, file %v, wrap %q, cursor %d; want it kept", no, s.keymap.reset, cfg.HasUserKeys(), cfg.Keys.Row("wrap").Keys, s.keymap.cursor)
		}
	}
	pressRoot(s, "D", "y")
	bak, err := os.ReadFile(path + ".bak")
	if err != nil || string(bak) != mine || cfg.HasUserKeys() {
		t.Errorf("D y: .bak %q, %v, file there %v", bak, err, cfg.HasUserKeys())
	}
	if cfg.Keys != keys.Builtin() || !strings.Contains(s.keymap.msg, "keybindings.toml is now keybindings.toml.bak") {
		t.Errorf("D y: built-in keys %v, msg %q", cfg.Keys == keys.Builtin(), s.keymap.msg)
	}
	if v := ansi.Strip(s.frame()); strings.Contains(v, "│  * ") || !strings.Contains(v, " keybindings.toml is now keybindings.toml.bak: the built-in keys apply ─╯") {
		t.Errorf("D y: a row still marked changed, or no message in the bottom border:\n%s", v)
	}
	pressRoot(s, "D")
	if s.keymap.reset || !strings.Contains(s.keymap.msg, "no keybindings.toml") {
		t.Errorf("D without a file: asking %v, msg %q", s.keymap.reset, s.keymap.msg)
	}
	pressRoot(s, "esc", "esc", "w")
	if !cfg.Settings.Wrap {
		t.Errorf("w on the page does not toggle wrap after the reset")
	}
}

// The list of the keys scrolls as the other lists; its lines are found by
// their start, which a narrow terminal leaves, cut: the page and the keys.
func TestSettingsKeymapSweep(t *testing.T) {
	t.Parallel() // a sweep: its own config and views, nothing shared
	n := len(keyLines(keys.Builtin()))
	// Every height from the smallest drawn up to 40, and around "all fit" at
	// n+4.
	var heights []int
	for height := minHeight; height <= 40; height++ {
		heights = append(heights, height)
	}
	for height := n + 2; height <= n+6; height++ {
		heights = append(heights, height)
	}
	for _, height := range heights {
		t.Run(fmt.Sprint("height ", height), func(t *testing.T) {
			t.Parallel()
			s, _ := openKeymap(t, 40, height, "")
			items := keymapItems(s.cfg.Keys, keyLines(s.cfg.Keys))
			index := func(r string) int {
				r, found := strings.TrimSuffix(r, "…"), -1
				for i, item := range items {
					if strings.HasPrefix(item, r) {
						if found >= 0 {
							return -1 // two lines start alike
						}
						found = i
					}
				}
				return found
			}
			what := fmt.Sprintf("keys, height %d", height)
			h := popupHeight(n, height) - 2
			check := func(k string, shown []int) []int {
				t.Helper()
				view := s.frame()
				if got := len(strings.Split(view, "\n")); got > height {
					t.Fatalf("%s, %s: %d lines", what, k, got)
				}
				if line := rawText(view, 40); line != "" {
					t.Fatalf("%s, %s: line too wide: %q", what, k, line)
				}
				return checkShown(t, what+", "+k, popupRows(view), h, n, s.keymap.cursor, shown, index)
			}
			shown := check("", nil)
			for _, k := range append(slices.Repeat([]string{"j"}, n), slices.Repeat([]string{"k"}, n)...) {
				pressRoot(s, k)
				shown = check(k, shown)
			}
		})
	}
}

// A key that does the same on several pages stands once in the help: what
// "Everywhere" lists is in no other section, and no section lists a key twice.
func TestHelpListsSharedKeysOnce(t *testing.T) {
	everywhere := map[string]bool{}
	section, seen := "", map[string]bool{}
	for _, b := range helpLines(keys.Builtin()) {
		if b.key == "" {
			if !strings.HasPrefix(b.what, " ") {
				section, seen = b.what, map[string]bool{}
			}
			continue
		}
		if section == "Everywhere" {
			everywhere[b.key] = true
		} else if everywhere[b.key] {
			t.Errorf("%s: %q is listed under Everywhere too", section, b.key)
		}
		if seen[b.key] {
			t.Errorf("%s: %q is listed twice", section, b.key)
		}
		seen[b.key] = true
	}
}

// The letters of the settings work on the pages, without the popup.
func TestSettingKeysOnAllPages(t *testing.T) {
	for _, page := range settingsPages() {
		for _, o := range settingOptions(keys.Builtin()) {
			if o.ask != nil || o.logFlow || o.number != nil || o.patterns || o.keymap {
				continue // see TestUntrackedKeyOnPages, TestLogFlowKeysOnlyThere, TestSettingsNumber, TestSettingsKeymap
			}
			m, cfg := page.make()
			s := WithSettings(cfg, m)
			s.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
			pressRoot(s, page.keys...)
			before := *o.setting(cfg)
			pressRoot(s, o.key)
			if *o.setting(cfg) == before || s.open {
				t.Errorf("%s: %s: %v, popup %v; want it toggled from %v, no popup", page.name, o.key, *o.setting(cfg), s.open, before)
			}
			pressRoot(s, o.key)
			if *o.setting(cfg) != before {
				t.Errorf("%s: %s twice: %v, want %v", page.name, o.key, *o.setting(cfg), before)
			}
		}
	}
}

func TestLogFlowKeysOnlyThere(t *testing.T) {
	for _, page := range settingsPages() {
		for _, o := range settingOptions(keys.Builtin()) {
			if !o.logFlow {
				continue
			}
			m, cfg := page.make()
			s := WithSettings(cfg, m)
			s.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
			pressRoot(s, page.keys...)
			before := *o.setting(cfg)
			pressRoot(s, o.key)
			if want := page.name == "log flow"; (*o.setting(cfg) != before) != want {
				t.Errorf("%s: %s toggles: %v, want %v", page.name, o.key, *o.setting(cfg) != before, want)
			}
		}
	}
}

func TestLogFlowStayInCommitKeyTells(t *testing.T) {
	l, _ := newTestLog()
	s := WithSettings(l.cfg, l)
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressRoot(s, "f", "C")
	if !l.cfg.Settings.StayInCommit || l.flow.msg != "staying in commit c3" {
		t.Errorf("C: %v, msg %q", l.cfg.Settings.StayInCommit, l.flow.msg)
	}
}

func TestUntrackedKeyOnPages(t *testing.T) {
	d := newUntrackedDiff()
	s := WithSettings(d.cfg, d)
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressRoot(s, "u")
	if !d.cfg.Settings.Untracked || len(d.files) != 3 || d.files[2].Path != "x0" {
		t.Errorf("u in a git diff: %v, %d files", d.cfg.Settings.Untracked, len(d.files))
	}
	pressRoot(s, "enter", "u")
	if d.cfg.Settings.Untracked || len(d.files) != 2 {
		t.Errorf("u in its diff view: %v, %d files", d.cfg.Settings.Untracked, len(d.files))
	}

	m := newMarkFileDiff()
	s = WithSettings(m.cfg, m)
	pressRoot(s, "u")
	if m.cfg.Settings.Untracked || m.msg != addedOnlyInDiff(source.AddedUntracked) {
		t.Errorf("u on two files: %v, msg %q", m.cfg.Settings.Untracked, m.msg)
	}
}

// After m and ' the letters are mark letters, in a line of text they are text.
func TestSettingKeysGiveWay(t *testing.T) {
	m := newMarkFileDiff()
	cfg := m.cfg
	before := cfg.Settings.SideBySide
	s := WithSearch(cfg, WithSettings(cfg, m))
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressRoot(s, "ctrl+space", "m", "s")
	if _, ok := cfg.Mark("s"); !ok || cfg.Settings.SideBySide != before {
		t.Errorf("m s: mark %v, side by side %v", ok, cfg.Settings.SideBySide)
	}
	pressRoot(s, "'", "s")
	if m.msg != "mark s" || cfg.Settings.SideBySide != before {
		t.Errorf("' s: msg %q, side by side %v", m.msg, cfg.Settings.SideBySide)
	}
	pressRoot(s, "/", "s")
	if s.input == nil || s.input.text != "s" || cfg.Settings.SideBySide != before {
		t.Errorf("/ s: input %+v, side by side %v", s.input, cfg.Settings.SideBySide)
	}
}

func TestSettingKeySaves(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	s := WithSettings(cfg, NewFileDiff(cfg, "old", "new", []string{"x0"}, []string{"x1"}))
	pressRoot(s, "W")
	data, err := os.ReadFile(filepath.Join(dir, "state.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "show_whitespaces = true") {
		t.Errorf("state.toml:\n%s", data)
	}
}

// A problem saving shows on the page, where the key was pressed.
func TestSettingKeyTellsWhyNotSaved(t *testing.T) {
	newCfg := func(t *testing.T) *config.Config {
		dir := t.TempDir()
		cfg, err := config.Load(dir, nil, "", "")
		if err != nil {
			t.Fatal(err)
		}
		// A directory in the place of state.toml.
		if err := os.MkdirAll(filepath.Join(dir, "state.toml", "x0"), 0o755); err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	files := []source.File{modified("f00", []string{"x0"}, []string{"x1"}), modified("f01", []string{"x2"}, []string{"x3"})}
	press := func(m tea.Model, cfg *config.Config, keys ...string) {
		s := WithSettings(cfg, m)
		s.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
		pressRoot(s, keys...)
	}
	t.Run("diff view", func(t *testing.T) {
		cfg := newCfg(t)
		m := NewFileDiff(cfg, "old", "new", files[0].Old, files[0].New)
		press(m, cfg, "w")
		if !strings.HasPrefix(m.msg, "state: ") {
			t.Errorf("msg %q", m.msg)
		}
	})
	t.Run("file list", func(t *testing.T) {
		cfg := newCfg(t)
		d := NewDiff(cfg, files)
		press(d, cfg, "w")
		if !strings.HasPrefix(d.msg, "state: ") {
			t.Errorf("msg %q", d.msg)
		}
	})
	t.Run("flow", func(t *testing.T) {
		cfg := newCfg(t)
		cfg.Settings.Flow = true
		d := NewDiff(cfg, files)
		press(d, cfg, "enter", "w")
		if !d.flowShown() || !strings.HasPrefix(d.flow.msg, "state: ") {
			t.Errorf("flow %v, msg %q", d.flowShown(), d.flow.msg)
		}
	})
	t.Run("commit list", func(t *testing.T) {
		l, _ := newTestLog()
		cfg := newCfg(t)
		l.cfg = cfg
		press(l, cfg, "w")
		if !strings.HasPrefix(l.msg, "state: ") {
			t.Errorf("msg %q", l.msg)
		}
	})
	// With the popup open it shows there, and not on the page below.
	t.Run("popup", func(t *testing.T) {
		cfg := newCfg(t)
		m := NewFileDiff(cfg, "old", "new", files[0].Old, files[0].New)
		s := WithSettings(cfg, m)
		s.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
		pressRoot(s, "o", "w")
		if !strings.HasPrefix(s.msg, "state: ") || m.msg != "" {
			t.Errorf("popup msg %q, page msg %q", s.msg, m.msg)
		}
	})
}

// Pasted text changes the number typed, as a key does: why it was none is gone.
func TestSettingsNumberPasteClearsError(t *testing.T) {
	s := WithSettings(config.Default(), newTestFileDiff(numbered("x", 30), numbered("y", 30)))
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	pressRoot(s, "o")
	dirs := slices.IndexFunc(settingOptions(keys.Builtin()), func(o settingOption) bool { return o.action == "inotify_max_dirs" })
	for range dirs {
		pressRoot(s, "j")
	}
	pressRoot(s, "enter")
	if view := ansi.Strip(s.frame()); !strings.Contains(view, "inotify max dirs") || strings.Contains(view, "auto reload timeout") {
		t.Errorf("the line of inotify_max_dirs is not named after it:\n%s", view)
	}
	pressRoot(s, "ctrl+u", "x", "enter")
	if s.inputErr == "" {
		t.Fatal("x was taken for a number")
	}
	s.Update(tea.PasteMsg{Content: "1"})
	if s.inputErr != "" || s.input.text != "x1" {
		t.Errorf("pasted after the failure: err %q, line %q", s.inputErr, s.input.text)
	}
}

// The last option has no key: enter on it asks for the minutes in a line:
// enter sets and saves them, what is no number stays asked for, esc keeps
// them. The columns stay aligned.
func TestSettingsNumber(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.AutoReloadTimeout != 15 {
		t.Errorf("default timeout %d, want 15", cfg.Settings.AutoReloadTimeout)
	}
	s := WithSettings(cfg, newTestFileDiff(numbered("x", 30), numbered("y", 30)))
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	// a is free, on the page and in the popup.
	pressRoot(s, "a")
	if s.input != nil || s.open {
		t.Errorf("a on the page: input %+v, popup %v", s.input, s.open)
	}
	pressRoot(s, "o", "a")
	if s.input != nil {
		t.Errorf("a in the popup: input %+v", s.input)
	}
	minutes := slices.IndexFunc(settingOptions(keys.Builtin()), func(o settingOption) bool { return o.action == "auto_reload_timeout" })
	for range minutes {
		pressRoot(s, "j")
	}
	pressRoot(s, "enter")
	if s.input == nil || s.input.text != "15" {
		t.Fatalf("enter on the minutes: input %+v, want them to edit", s.input)
	}
	if view := ansi.Strip(s.frame()); !strings.Contains(view, "auto reload timeout") {
		t.Errorf("the line is not shown:\n%s", view)
	}
	pressRoot(s, "ctrl+u", "x", "enter")
	if s.input == nil || !strings.Contains(ansi.Strip(s.frame()), "a whole number") || cfg.Settings.AutoReloadTimeout != 15 {
		t.Errorf("x: input %+v, timeout %d; want it asked for again", s.input, cfg.Settings.AutoReloadTimeout)
	}
	pressRoot(s, "esc")
	if s.input != nil || !s.open || cfg.Settings.AutoReloadTimeout != 15 {
		t.Errorf("esc: input %+v, open %v, timeout %d", s.input, s.open, cfg.Settings.AutoReloadTimeout)
	}
	pressRoot(s, "enter", "ctrl+u", "1", "2", "0", "enter")
	if s.input != nil || !s.open || cfg.Settings.AutoReloadTimeout != 120 {
		t.Errorf("120: input %+v, open %v, timeout %d", s.input, s.open, cfg.Settings.AutoReloadTimeout)
	}
	data, err := os.ReadFile(filepath.Join(dir, "state.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "auto_reload_timeout = 120") {
		t.Errorf("state.toml lacks the timeout:\n%s", data)
	}
	var cols []int
	for _, line := range strings.Split(ansi.Strip(s.frame()), "\n") {
		for _, what := range []string{"mark changed whitespace", "minutes without keys", "side by side layout"} {
			if i := strings.Index(line, what); i >= 0 {
				cols = append(cols, ansi.StringWidth(line[:i]))
			}
		}
	}
	if len(cols) != 3 || cols[0] != cols[1] || cols[1] != cols[2] {
		t.Errorf("columns of the options: %v, want 3 the same", cols)
	}
}

// f in the popup switches the page below to the flow view and back, as on the
// pages, and saves it; two files have none, which it says.
func TestSettingsFlow(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	d := NewDiff(cfg, []source.File{modified("f00", []string{"x0"}, []string{"x1"}), modified("f01", []string{"x2"}, []string{"x3"})})
	s := WithSettings(cfg, d)
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	pressRoot(s, "o", "f")
	if !cfg.Settings.Flow || !d.flowShown() || !s.open {
		t.Errorf("f: flow %v, shown %v, popup %v", cfg.Settings.Flow, d.flowShown(), s.open)
	}
	data, err := os.ReadFile(filepath.Join(dir, "state.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "flow = true") {
		t.Errorf("state.toml:\n%s", data)
	}
	if !regexp.MustCompile(`\[x\] +f `).MatchString(ansi.Strip(s.frame())) {
		t.Errorf("f is not ticked:\n%s", ansi.Strip(s.frame()))
	}
	pressRoot(s, "f")
	if cfg.Settings.Flow || d.flowShown() || !s.open {
		t.Errorf("f again: flow %v, shown %v, popup %v", cfg.Settings.Flow, d.flowShown(), s.open)
	}

	m := newMarkFileDiff()
	s = WithSettings(m.cfg, m)
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	pressRoot(s, "o", "f")
	if m.cfg.Settings.Flow || m.msg != flowOnlyOfSeveral {
		t.Errorf("f on two files: flow %v, msg %q", m.cfg.Settings.Flow, m.msg)
	}
}

// The settings follow the order of the rows of everywhere, those not in them
// stay behind and all else stays where it is.
func TestOrderSettings(t *testing.T) {
	type o = settingOption
	order := []string{`\`, "b", "f", "s", "o"}
	options, bs := orderSettings(order,
		[]o{{key: "s"}, {key: "f"}, {key: "t"}, {key: "b"}, {key: "c", logFlow: true}, {what: "x0"}},
		[]binding{{"", "Everywhere"}, {"j k", "x1"}, {"s", "x2"}, {"f", "x3"}, {"b", "x4"}, {"o F8", "x5"}, {"", "Flow"}, {"c", "x6"}})
	var keys []string
	for _, opt := range options {
		keys = append(keys, opt.key)
	}
	if want := []string{"b", "f", "s", "t", "c", ""}; !slices.Equal(keys, want) {
		t.Errorf("options: %q, want %q", keys, want)
	}
	var whats []string
	for _, b := range bs {
		whats = append(whats, b.what)
	}
	if want := []string{"Everywhere", "x1", "x4", "x3", "x2", "x5", "Flow", "x6"}; !slices.Equal(whats, want) {
		t.Errorf("bindings: %q, want %q", whats, want)
	}
	// Without a table, all stays.
	before := []o{{key: "s"}, {key: "f"}}
	if got, _ := orderSettings(nil, before, nil); got[0].key != "s" || got[1].key != "f" {
		t.Errorf("no table: %+v", got)
	}
}

// The settings and the help follow the order and the keys of the user's file.
func TestSettingsOfUserKeys(t *testing.T) {
	cfg := config.Default()
	p, problems := cfg.Keys.WithUser(`everywhere = [
    { action = "wrap", keys = ["alt+w"] },
    { action = "layout", keys = ["S"] },
]`)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	cfg.Keys = &keys.Map{Keymap: p}
	options := settingOptions(cfg.Keys)
	if i := slices.IndexFunc(options, func(o settingOption) bool { return o.action == "layout" }); options[i].key != "S" {
		t.Errorf("layout: key %q", options[i].key)
	}
	if !slices.ContainsFunc(helpLines(cfg.Keys), func(b binding) bool { return b.key == "alt+w" }) {
		t.Errorf("the help has not alt+w: %q", helpLines(cfg.Keys))
	}
}

func TestKeyOrderProblems(t *testing.T) {
	// A row of its own for each key, so that taking one out leaves the setting.
	var rows string
	keyRow := map[string]string{}
	for i, o := range settingOptions(keys.Builtin()) {
		if o.key != "" && !o.logFlow {
			keyRow[o.key] = fmt.Sprintf("{ action = \"k%d\", keys = [%q] },\n", i, o.key)
			rows += keyRow[o.key]
		}
	}
	for _, o := range settingOptions(keys.Builtin()) {
		if o.action != "" {
			rows += fmt.Sprintf("{ action = %q, setting = \"x\" },\n", o.action)
		}
	}
	table := func(rows string) string { return "everywhere = [\n" + rows + "]\n" }
	if got := keyOrderProblems(table(rows)); len(got) != 0 {
		t.Errorf("all there: %q", got)
	}
	for _, c := range []struct{ text, want string }{
		{"everywhere = [", "toml:"},
		{"x = 1\n", "unknown table x"},
		{"diff_view = []\n", "no rows"},
		{table(rows + keyRow["s"]), "key s stands twice"},
		{table(rows + `{ action = "x0", keys = ["alt+x"] },` + "\n"), "key alt+x is not in the help"},
		{table(strings.Replace(rows, keyRow["t"], "", 1)), "setting t is not in the table"},
		{table(rows) + `help = [{ action = "x0" }]`, "action x0 has no help"},
		{table(rows) + `diff_view = [{ action = "a", keys = ["x"] }, { action = "b", keys = ["x"] }]`, "key x has two actions in diff_view"},
		{table(strings.Replace(rows, `{ action = "wrap", setting = "x" },`+"\n", "", 1)), "setting wrap has no words"},
	} {
		got := keyOrderProblems(c.text)
		if len(got) != 1 || !strings.Contains(got[0], c.want) {
			t.Errorf("%q: problems %q, want %q", c.want, got, c.want)
		}
	}
}

// keybindings.toml decides the order of the settings: it must fit them.
func TestKeybindingsFile(t *testing.T) {
	if problems := keyOrderProblems(keys.BuiltinText); len(problems) != 0 {
		t.Errorf("keybindings.toml: %q", problems)
	}
}

// The help's words show the keys of their placeholders.
func TestHelpLinesText(t *testing.T) {
	if bs := helpLines(keys.Builtin()); !slices.Contains(bs, binding{"?", "this help (? esc q close, j k scroll)"}) {
		t.Errorf("the help's line of ? is not expanded: %q", bs)
	}
}

// The status bars, with the words of keybindings.toml.
func TestStatusKeysWords(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.Flow, cfg.Settings.SideBySide = false, true
	rest := " · d git diff · o settings · ? help · q quit"
	for _, c := range []struct {
		k    barKeys
		want string
	}{
		{barKeys{enter: "open", flow: true}, "enter open · f flow · s unified" + rest},
		{barKeys{enter: "open", back: "to_commits", commits: true, flow: true}, "enter open · backspace commits · N/P commit · f flow · s unified" + rest},
		{barKeys{enter: "anchor"}, "enter mark/anchor · s unified" + rest},
		{barKeys{enter: "anchor", back: "to_files", flow: true}, "enter mark/anchor · backspace files · f flow · s unified" + rest},
	} {
		if got := statusKeys(cfg, c.k); got != c.want {
			t.Errorf("%+v:\n got %q\nwant %q", c.k, got, c.want)
		}
	}
	cfg.Settings.Flow, cfg.Settings.SideBySide = true, false
	if got, want := statusKeys(cfg, barKeys{enter: "open", flow: true}), "enter open · f file · s split"+rest; got != want {
		t.Errorf("switched:\n got %q\nwant %q", got, want)
	}
}

// doc/keybindings.md lists the keys of keybindings.toml, page by page, in the
// same order.
func TestKeybindingsDoc(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "doc", "keybindings.md"))
	if err != nil {
		t.Fatal(err)
	}
	backticked := regexp.MustCompile("`([^`]+)`")
	inDoc := map[string][]string{}
	page := ""
	for _, line := range strings.Split(string(doc), "\n") {
		if strings.HasPrefix(line, "#") {
			page = strings.TrimSpace(strings.TrimLeft(line, "# "))
		} else if strings.HasPrefix(line, "| ") {
			for _, m := range backticked.FindAllStringSubmatch(strings.Split(line, "|")[1], -1) {
				inDoc[page] = append(inDoc[page], m[1])
			}
		}
	}
	p, err := keys.Parse(keys.BuiltinText)
	if err != nil {
		t.Fatal(err)
	}
	pages := []struct {
		heading string
		rows    []keymap.Row
	}{
		{"Commit list (`jdiff log`)", p.Rows(keys.CommitList)},
		{"File list", p.Rows(keys.FileList)},
		{"Diff view", p.Rows(keys.DiffView)},
		{"Flow view", p.Rows(keys.FlowView)},
		{"README (`jdiff --readme`)", p.Rows(keys.DocView)},
		{"Everywhere", p.Rows(keys.Everywhere)},
		{"All popups (`popup`)", p.Rows(keys.Popup)},
		{"Jump marks (`marks_popup`)", p.Rows(keys.MarksPopup)},
		{"Settings (`settings_popup`)", p.Rows(keys.SettingsPopup)},
		{"Patterns of ignored_hide (`ignored_hide`)", p.Rows(keys.IgnoredHide)},
		{"Keys (`keys_popup`)", p.Rows(keys.KeysPopup)},
		{"A key pressed for a row (`key_capture`)", p.Rows(keys.KeyCapture)},
		{"Questions (`confirm`)", p.Rows(keys.Confirm)},
		{"Input lines (`input_line`)", p.Rows(keys.InputLine)},
		{"Another git diff (`git_diff_line`)", p.Rows(keys.GitDiffLine)},
		{"Search (`search_line`)", p.Rows(keys.SearchLine)},
		{"While the terminal is too small (`too_small`)", p.Rows(keys.TooSmall)},
	}
	for _, c := range pages {
		var want []string
		for _, r := range c.rows {
			want = append(want, keys.Shown(r.Keys)...)
		}
		if !slices.Equal(inDoc[c.heading], want) {
			t.Errorf("%s: doc %q, keybindings.toml %q", c.heading, inDoc[c.heading], want)
		}
	}
	if len(inDoc) != len(pages) {
		t.Errorf("the doc has keys on %d pages, keybindings.toml %d: %q", len(inDoc), len(pages), slices.Sorted(maps.Keys(inDoc)))
	}
}
