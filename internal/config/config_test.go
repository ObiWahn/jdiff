package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"jdiff/internal/settings"
	"jdiff/internal/theme"
)

// writeFiles writes name → content below a new directory and returns it.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadWithoutFiles(t *testing.T) {
	c, err := Load(t.TempDir(), nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Settings != settings.Default() || c.Warning() != "" {
		t.Errorf("settings %+v, warning %q", c.Settings, c.Warning())
	}
}

func TestStateOverridesMain(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.toml":  "context = 5\nflow = true\nside_by_side = false\nshow_whitespaces = false\n",
		"state.toml": "flow = false\nshow_whitespaces = true\n",
	})
	c, err := Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	s := c.Settings
	if s.Context != 5 || s.Flow || s.SideBySide || !s.ShowWhitespaces || !s.Preview {
		t.Errorf("settings %+v: want context 5 and side_by_side off from main, flow off and "+
			"show_whitespaces on from state, preview on by default", s)
	}
}

func TestUnknownKeys(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.toml":  "contxt = 5\n",
		"state.toml": "context = 9\n", // not a toggle
	})
	c, err := Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	w := c.Warning()
	if c.Settings.Context != 3 || !strings.Contains(w, "main.toml: unknown contxt") ||
		!strings.Contains(w, "state.toml: unknown context") {
		t.Errorf("context %d, warning %q", c.Settings.Context, w)
	}
}

func TestParseError(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.toml": "context = 3\nflow = \n"})
	_, err := Load(dir, nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "main.toml") || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("err = %v, want main.toml and line 2", err)
	}
}

func TestThemeFileOverDefaults(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.toml":          `theme = "desert"`,
		"themes/desert.toml": "minus-style = \"yellow\"\nanchor-style = \"cyan\"\n",
	})
	git := theme.Config{"delta.minus-style": "blue", "delta.plus-style": "magenta"}
	c, err := Load(dir, git, "", "")
	if err != nil {
		t.Fatal(err)
	}
	// The git config is a theme of its own: the theme file falls back to
	// the defaults.
	th := c.Theme
	if th.Minus.GetForeground() != lipgloss.Color("3") || th.Plus.GetForeground() != lipgloss.Color("2") ||
		th.Anchor.GetForeground() != lipgloss.Color("6") {
		t.Errorf("minus %v plus %v anchor %v, want the theme's yellow, the default green, the theme's cyan",
			th.Minus.GetForeground(), th.Plus.GetForeground(), th.Anchor.GetForeground())
	}
}

func TestThemeSelection(t *testing.T) {
	git := theme.Config{"delta.minus-style": "blue", "delta.features": "dark", "delta.dark.plus-style": "magenta",
		"delta.light.plus-style": "cyan"}
	for _, tt := range []struct {
		main, env, flag string
		minus, plus     lipgloss.Color
	}{
		{"", "", "", "4", "5"},                      // no name: delta's git config
		{`theme = "delta"`, "", "", "4", "5"},       // by name
		{`theme = "default"`, "", "", "1", "2"},     // jdiff's defaults
		{"", "light", "", "4", "6"},                 // DELTA_FEATURES replaces delta.features
		{"", "", "light", "4", "6"},                 // so does --features
		{`theme = "desert"`, "light", "", "3", "2"}, // features only apply to delta's theme
	} {
		dir := writeFiles(t, map[string]string{"main.toml": tt.main, "themes/desert.toml": `minus-style = "yellow"`})
		c, err := Load(dir, git, tt.env, tt.flag)
		if err != nil {
			t.Fatal(err)
		}
		if fg := c.Theme.Minus.GetForeground(); fg != tt.minus {
			t.Errorf("%q env %q flag %q: minus %v, want %v", tt.main, tt.env, tt.flag, fg, tt.minus)
		}
		if fg := c.Theme.Plus.GetForeground(); fg != tt.plus {
			t.Errorf("%q env %q flag %q: plus %v, want %v", tt.main, tt.env, tt.flag, fg, tt.plus)
		}
	}
}

func TestThemeFileColorsAndWarnings(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.toml": `theme = "desert"`,
		"themes/desert.toml": "minus-style = \"Sand bold\"\nplus-style = \"nope\"\nminus-styel = \"red\"\n" +
			"features = \"dark\"\n[colors]\nSand = \"#c2b280\"\n",
	})
	c, err := Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if fg := c.Theme.Minus.GetForeground(); fg != lipgloss.Color("#c2b280") || !c.Theme.Minus.GetBold() {
		t.Errorf("minus %v, want the named color sand, bold", fg)
	}
	w := c.Warning()
	for _, want := range []string{`desert.toml: plus-style: style "nope"`, "desert.toml: unknown features, minus-styel"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning %q lacks %q", w, want)
		}
	}
	if fg := c.Theme.Plus.GetForeground(); fg != lipgloss.Color("2") {
		t.Errorf("invalid plus-style: %v, want the default green", fg)
	}
}

func TestThemeFiles(t *testing.T) {
	files := map[string]string{
		"themes/desert.toml": `minus-style = "yellow"`,
		"themes/broken.toml": "minus-style = ",
		"themes/delta.toml":  `minus-style = "cyan"`,
		"themes/other.toml":  "bogus = 1\n",
	}
	c, err := Load(writeFiles(t, files), nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, sp := range c.Themes {
		names = append(names, sp.Name)
	}
	if got := strings.Join(names, " "); got != "default delta desert other" {
		t.Errorf("themes %q, want default delta desert other", got)
	}
	w := c.Warning()
	if !strings.Contains(w, "broken.toml") || !strings.Contains(w, `delta.toml: "delta" is a built-in theme`) ||
		strings.Contains(w, "bogus") {
		t.Errorf("warning %q: want the broken file and the reserved name, not other.toml's unknown key", w)
	}
	if fg := c.Theme.Minus.GetForeground(); fg != lipgloss.Color("1") {
		t.Errorf("minus %v, want delta's (here the default) red, not delta.toml's", fg)
	}

	files["main.toml"] = `theme = "broken"`
	if _, err := Load(writeFiles(t, files), nil, "", ""); err == nil || !strings.Contains(err.Error(), "broken.toml") {
		t.Errorf("selected broken theme: err %v, want broken.toml", err)
	}
}

func TestMissingTheme(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.toml": `theme = "nope"`})
	c, err := Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if w := c.Warning(); !strings.Contains(w, `theme "nope": no file`) {
		t.Errorf("warning %q", w)
	}
}

func TestSaveState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "jdiff") // created by SaveState
	c, err := Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	c.Settings.Flow, c.Settings.Preview, c.Settings.Context = true, false, 7
	c.Settings.FullMessages = false
	if err := c.SaveState(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "state.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, "flow = true") || !strings.Contains(s, "full_messages = false") ||
		strings.Contains(s, "context") {
		t.Errorf("state.toml:\n%s", s)
	}
	c, err = Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Settings.Flow || c.Settings.Preview || c.Settings.FullMessages || c.Settings.Context != 3 {
		t.Errorf("reloaded %+v, want flow on, preview off, short messages, context default", c.Settings)
	}
	if Default().SaveState() != nil {
		t.Errorf("SaveState without a directory should do nothing")
	}
}

func TestDir(t *testing.T) {
	// Absolute paths of the system: /xdg is not absolute on Windows.
	xdg, home := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if dir, err := Dir(); err != nil || dir != filepath.Join(xdg, "jdiff") {
		t.Errorf("Dir() = %q, %v with XDG_CONFIG_HOME", dir, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // the home of os.UserHomeDir on Windows
	if dir, err := Dir(); err != nil || dir != filepath.Join(home, ".config", "jdiff") {
		t.Errorf("Dir() = %q, %v without XDG_CONFIG_HOME", dir, err)
	}
}

func TestWatchInterval(t *testing.T) {
	for _, tt := range []struct {
		main    string
		want    float64
		warning string
	}{
		{"", 1, ""},
		{"watch_interval = 0.25", 0.25, ""},
		{"watch_interval = 0", 0, ""},
		{"watch_interval = -2", 0, "watch_interval -2 is negative"},
	} {
		c, err := Load(writeFiles(t, map[string]string{"main.toml": tt.main}), nil, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if w := c.Warning(); c.Settings.WatchInterval != tt.want || (tt.warning == "") != (w == "") ||
			!strings.Contains(w, tt.warning) {
			t.Errorf("%q: interval %v, warning %q; want %v and %q", tt.main, c.Settings.WatchInterval, w, tt.want, tt.warning)
		}
	}
}

func TestInotifyMaxDirs(t *testing.T) {
	for _, tt := range []struct {
		main    string
		want    int
		warning string
	}{
		{"", 20, ""},
		{"inotify_max_dirs = 100", 100, ""},
		{"inotify_max_dirs = 0", 0, ""},
		{"inotify_max_dirs = -1", 0, "inotify_max_dirs -1 is negative"},
	} {
		c, err := Load(writeFiles(t, map[string]string{"main.toml": tt.main}), nil, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if w := c.Warning(); c.Settings.InotifyMaxDirs != tt.want || (tt.warning == "") != (w == "") ||
			!strings.Contains(w, tt.warning) {
			t.Errorf("%q: max dirs %v, warning %q; want %v and %q", tt.main, c.Settings.InotifyMaxDirs, w, tt.want, tt.warning)
		}
	}
}

func TestLineEnd(t *testing.T) {
	for _, tt := range []struct {
		main    string
		want    string
		warning string
	}{
		{"", "¶", ""},
		{`line_end = "¬"`, "¬", ""},
		{`line_end = "$"`, "$", ""},
		{`line_end = "ab"`, "¶", `line_end "ab" is not one column wide`},
		{`line_end = ""`, "¶", `line_end "" is not one column wide`},
	} {
		c, err := Load(writeFiles(t, map[string]string{"main.toml": tt.main}), nil, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if w := c.Warning(); c.Settings.LineEnd != tt.want || (tt.warning == "") != (w == "") ||
			!strings.Contains(w, tt.warning) {
			t.Errorf("%q: line end %q, warning %q; want %q and %q", tt.main, c.Settings.LineEnd, w, tt.want, tt.warning)
		}
	}
}

func TestCREnd(t *testing.T) {
	for _, tt := range []struct {
		main    string
		want    string
		warning string
	}{
		{"", "␍", ""},
		{`cr_end = "←"`, "←", ""},
		{`cr_end = "^M"`, "␍", `cr_end "^M" is not one column wide`},
	} {
		c, err := Load(writeFiles(t, map[string]string{"main.toml": tt.main}), nil, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if w := c.Warning(); c.Settings.CREnd != tt.want || (tt.warning == "") != (w == "") ||
			!strings.Contains(w, tt.warning) {
			t.Errorf("%q: cr end %q, warning %q; want %q and %q", tt.main, c.Settings.CREnd, w, tt.want, tt.warning)
		}
	}
}

func TestCutMarkers(t *testing.T) {
	for _, tt := range []struct {
		main              string
		extends, precedes string
		warning           string
	}{
		{"", "»", "«", ""},
		{"extends = \">\"\nprecedes = \"<\"", ">", "<", ""},
		{`extends = ">>"`, "»", "«", `extends ">>" is not one column wide`},
		{`precedes = ""`, "»", "«", `precedes "" is not one column wide`},
	} {
		c, err := Load(writeFiles(t, map[string]string{"main.toml": tt.main}), nil, "", "")
		if err != nil {
			t.Fatal(err)
		}
		st := c.Settings
		if w := c.Warning(); st.Extends != tt.extends || st.Precedes != tt.precedes || (tt.warning == "") != (w == "") ||
			!strings.Contains(w, tt.warning) {
			t.Errorf("%q: extends %q precedes %q, warning %q", tt.main, st.Extends, st.Precedes, w)
		}
	}
}

func TestMarks(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	c.LoadMarks("/src/one")
	if err := c.SetMark("b", Mark{Path: "main.go", Line: 33, Text: "func main() {"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetMark("a", Mark{Path: "ui/flow.go", Line: 120, Text: "func (f *Flow) jump() {"}); err != nil {
		t.Fatal(err)
	}
	c.LoadMarks("/src/two")
	if err := c.SetMark("a", Mark{Path: "other.go", Line: 1, Text: "package other"}); err != nil {
		t.Fatal(err)
	}

	// A new session in the first repository sees its marks only.
	c, _ = Load(dir, nil, "", "")
	c.LoadMarks("/src/one")
	got := c.Marks()
	if len(got) != 2 || got[0].Letter != "a" || got[0].Path != "ui/flow.go" || got[1].Line != 33 {
		t.Fatalf("marks of /src/one: %+v", got)
	}
	if err := c.DeleteMark("a"); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(dir, nil, "", "")
	c.LoadMarks("/src/one")
	if _, ok := c.Mark("a"); ok || len(c.Marks()) != 1 {
		t.Errorf("after deleting a: %+v", c.Marks())
	}
	c.LoadMarks("/src/two")
	if m, ok := c.Mark("a"); !ok || m.Path != "other.go" {
		t.Errorf("mark a of /src/two: %+v %v, want it untouched", m, ok)
	}
}

func TestMarksBrokenFileAndNoDir(t *testing.T) {
	dir := writeFiles(t, map[string]string{"marks.toml": "[broken"})
	c, err := Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	c.LoadMarks("")
	if !strings.Contains(c.Warning(), "marks.toml") || len(c.Marks()) != 0 {
		t.Errorf("broken marks.toml: warning %q, marks %+v", c.Warning(), c.Marks())
	}
	// The next change replaces the broken file.
	if err := c.SetMark("x", Mark{Path: "/abs/f.txt", Line: 2, Text: "two"}); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(dir, nil, "", "")
	c.LoadMarks("")
	if m, ok := c.Mark("x"); !ok || m.Path != "/abs/f.txt" {
		t.Errorf("after replacing the broken file: %+v %v", m, ok)
	}
	// Without a configuration directory, marks live in memory only.
	d := Default()
	if err := d.SetMark("y", Mark{Path: "p", Line: 1}); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Mark("y"); !ok {
		t.Error("mark without a configuration directory is lost")
	}
}
