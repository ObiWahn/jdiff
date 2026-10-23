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
	c, err := Load(t.TempDir(), nil, nil)
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
	c, err := Load(dir, nil, nil)
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
	c, err := Load(dir, nil, nil)
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
	_, err := Load(dir, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "main.toml") || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("err = %v, want main.toml and line 2", err)
	}
}

func TestThemeOverGitConfig(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.toml":          `theme = "desert"`,
		"themes/desert.toml": "minus-style = \"yellow\"\nanchor-style = \"cyan\"\n",
	})
	git := theme.Config{"delta.minus-style": "blue", "delta.plus-style": "magenta"}
	c, err := Load(dir, git, nil)
	if err != nil {
		t.Fatal(err)
	}
	th := c.Theme
	if th.Minus.GetForeground() != lipgloss.Color("3") || th.Plus.GetForeground() != lipgloss.Color("5") ||
		th.Anchor.GetForeground() != lipgloss.Color("6") {
		t.Errorf("minus %v plus %v anchor %v, want the theme's yellow, git's magenta, the theme's cyan",
			th.Minus.GetForeground(), th.Plus.GetForeground(), th.Anchor.GetForeground())
	}
	if git["delta.minus-style"] != "blue" {
		t.Errorf("Load changed the git config passed in")
	}
}

func TestMissingTheme(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.toml": `theme = "nope"`})
	c, err := Load(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if w := c.Warning(); !strings.Contains(w, `theme "nope": no file`) {
		t.Errorf("warning %q", w)
	}
}

func TestSaveState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "jdiff") // created by SaveState
	c, err := Load(dir, nil, nil)
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
	c, err = Load(dir, nil, nil)
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
