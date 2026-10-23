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
	if err := c.SaveState(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "state.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, "flow = true") || strings.Contains(s, "context") {
		t.Errorf("state.toml:\n%s", s)
	}
	c, err = Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Settings.Flow || c.Settings.Preview || c.Settings.Context != 3 {
		t.Errorf("reloaded %+v, want flow on, preview off, context default", c.Settings)
	}
	if Default().SaveState() != nil {
		t.Errorf("SaveState without a directory should do nothing")
	}
}

func TestDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if dir, err := Dir(); err != nil || dir != "/xdg/jdiff" {
		t.Errorf("Dir() = %q, %v with XDG_CONFIG_HOME", dir, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/u")
	if dir, err := Dir(); err != nil || dir != "/home/u/.config/jdiff" {
		t.Errorf("Dir() = %q, %v without XDG_CONFIG_HOME", dir, err)
	}
}
