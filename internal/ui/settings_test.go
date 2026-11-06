package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
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
	for _, o := range settingOptions {
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
	app := func() (tea.Model, *config.Config) { a := newFlowApp(); return a, a.cfg }
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
		{"single diff", func() (tea.Model, *config.Config) { m := newMarkModel(); return m, m.cfg }, nil},
	}
}

func TestSettingsOpenOnAllPages(t *testing.T) {
	for _, page := range settingsPages() {
		for _, opener := range []string{"S", "f8"} {
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
	tomlKeys := map[string]string{"s": "side_by_side", "v": "preview", "w": "show_whitespaces", "W": "mark_whitespace",
		"t": "show_trailing", "e": "show_cr", "b": "bold_changes", "r": "wrap", "c": "full_messages", "C": "stay_in_commit"}
	for i, o := range settingOptions {
		if o.ask != nil {
			continue // the page toggles it: see TestUntracked...
		}
		dir := t.TempDir()
		cfg, err := config.Load(dir, nil, "", "")
		if err != nil {
			t.Fatal(err)
		}
		s := WithSettings(cfg, newTestModel(numbered("x", 10), numbered("y", 10)))
		s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		was := *o.setting(cfg)
		pressRoot(s, "S", o.key)
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
	a := newTestApp()
	s := WithSettings(a.cfg, a)
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	pressRoot(s, "S", "j", "j", "k")
	if s.cursor != 1 || a.cursor != 0 {
		t.Errorf("j j k: popup cursor %d, want 1; list cursor %d, want 0: keys must not reach the page", s.cursor, a.cursor)
	}
	pressRoot(s, "k", "k", "k")
	if s.cursor != 0 {
		t.Errorf("past the first option: cursor %d", s.cursor)
	}
	for range len(settingOptions) + 3 {
		pressRoot(s, "j")
	}
	if s.cursor != len(settingOptions)-1 {
		t.Errorf("past the last option: cursor %d", s.cursor)
	}
	pressRoot(s, "esc")
	for _, k := range []string{"S", "f8", "esc", "q"} {
		pressRoot(s, "S")
		pressRoot(s, k)
		if s.open {
			t.Errorf("%s does not close the settings", k)
		}
	}
	pressRoot(s, "S")
	if _, cmd := s.Update(keyMsg("ctrl+c")); cmd == nil {
		t.Errorf("ctrl+c does not quit in the settings")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c in the settings: %T, want a quit", cmd())
	}
}

// The letters of the options do nothing on the pages themselves.
func TestSettingsKeysLeftThePages(t *testing.T) {
	for _, page := range settingsPages() {
		m, cfg := page.make()
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		pressRoot(m, page.keys...)
		before := fmt.Sprint(optionValues(cfg))
		for _, o := range settingOptions {
			pressRoot(m, o.key)
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
	pressRoot(s, "f", "S", "C")
	if !strings.Contains(ansi.Strip(s.frame()), "staying in commit") {
		t.Errorf("C in the popup does not say so:\n%s", ansi.Strip(s.frame()))
	}
}

// S is a mark letter too: after m and ' it names the mark, not the popup.
func TestSettingsKeyAsMarkLetter(t *testing.T) {
	m := newMarkModel()
	s := WithSettings(m.cfg, m)
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	line := m.lineAt(right, m.cursor)
	pressRoot(s, "m", "S")
	if s.open {
		t.Errorf("m S opened the settings")
	}
	if mk, ok := m.cfg.Mark("S"); !ok || mk.Line != line+1 {
		t.Errorf("m S: mark %+v, set %v; want line %d", mk, ok, line+1)
	}
	pressRoot(s, "G", "'", "S")
	if s.open {
		t.Errorf("' S opened the settings")
	}
	if got := m.lineAt(right, m.cursor); got != line {
		t.Errorf("' S: on line %d, want %d", got, line)
	}
	if pressRoot(s, "S"); !s.open {
		t.Errorf("S after the marks does not open the settings")
	}
}

// The flow view lays out its rows anew on a toggle while the popup stays open
// over it.
func TestSettingsReflowInFlowBelowPopup(t *testing.T) {
	probe := newTestModel([]string{"a"}, []string{"b"})
	full := strings.Repeat("x", probe.contentWidth(80, false)+5)
	a := NewApp(config.Default(), []source.File{modified("a.go", []string{full, "old"}, []string{full, "new"})})
	a.cfg.Settings.CollapseUnchanged = false
	s := WithSettings(a.cfg, a)
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressRoot(s, "f", "S")
	before := len(a.flow.rows)
	pressRoot(s, "r")
	if len(a.flow.rows) <= before {
		t.Errorf("r in the popup: %d flow rows, %d before; want more for the wrapped lines", len(a.flow.rows), before)
	}
	if !strings.Contains(s.frame(), "settings") {
		t.Errorf("the popup closed on r:\n%s", s.frame())
	}
}

func TestSettingsPopupSweep(t *testing.T) {
	t.Parallel() // a sweep: its own config and views, nothing shared
	n := len(settingOptions)
	// Every height from the smallest drawn past "all fit" at n+4; a narrow
	// terminal keeps drawing fast, and 30 columns still show the key of a line.
	for height := minHeight; height <= n+6; height++ {
		t.Run(fmt.Sprint("height ", height), func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			s := WithSettings(cfg, newTestModel(numbered("x", 40), numbered("y", 40)))
			s.Update(tea.WindowSizeMsg{Width: 30, Height: height})
			pressRoot(s, "S")
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
				cursorOption := fmt.Sprintf("] %s ", settingOptions[s.cursor].key)
				if !strings.Contains(strings.Join(got, "\n"), cursorOption) {
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

// A key that does the same on several pages stands once in the help: what
// "Everywhere" lists is in no other section, and no section lists a key twice.
func TestHelpListsSharedKeysOnce(t *testing.T) {
	everywhere := map[string]bool{}
	section, seen := "", map[string]bool{}
	for _, b := range bindings {
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
