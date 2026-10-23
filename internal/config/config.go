// Package config bundles what every view needs to know: the theme and the
// user settings. All views share one Config.
//
// The settings come from ~/.config/jdiff: main.toml holds the settings and the
// name of the theme, themes/<name>.toml its styles, which override delta's
// git config, and state.toml, written by jdiff, the toggles as last set.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"jdiff/internal/settings"
	"jdiff/internal/theme"
)

// Config is shared by pointer, so a setting changed in one view applies to
// all of them.
type Config struct {
	Theme    *theme.Theme
	Settings settings.Settings

	warnings  []string // problems with the files
	statePath string   // where SaveState writes, empty for no state file
}

// Default returns the default theme and settings, without files.
func Default() *Config {
	return &Config{Theme: theme.Default(), Settings: settings.Default()}
}

// Dir returns jdiff's configuration directory: $XDG_CONFIG_HOME/jdiff, by
// default ~/.config/jdiff, on all systems.
func Dir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "jdiff"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "jdiff"), nil
}

// mainFile is the content of main.toml.
type mainFile struct {
	settings.Settings
	Theme string `toml:"theme"`
}

// stateFile is the content of state.toml: the settings toggled by keys.
type stateFile struct {
	Flow            bool `toml:"flow"`
	SideBySide      bool `toml:"side_by_side"`
	Preview         bool `toml:"preview"`
	ShowWhitespaces bool `toml:"show_whitespaces"`
}

// Load reads the files in dir over the default settings, and builds the
// theme from delta's git config gitCfg with features and the theme file.
// Missing files are skipped and unknown keys reported by Warning; files
// that cannot be parsed are an error. An empty dir reads no files.
func Load(dir string, gitCfg theme.Config, features []string) (*Config, error) {
	c := &Config{Settings: settings.Default()}
	if dir == "" {
		c.Theme = theme.Load(gitCfg, features)
		return c, nil
	}
	c.statePath = filepath.Join(dir, "state.toml")

	m := mainFile{Settings: c.Settings}
	if err := c.decode(filepath.Join(dir, "main.toml"), &m); err != nil {
		return nil, err
	}
	c.Settings = m.Settings

	st := c.state()
	if err := c.decode(c.statePath, &st); err != nil {
		return nil, err
	}
	c.Settings.Flow, c.Settings.SideBySide = st.Flow, st.SideBySide
	c.Settings.Preview, c.Settings.ShowWhitespaces = st.Preview, st.ShowWhitespaces

	if m.Theme != "" {
		styles := map[string]string{}
		path := filepath.Join(dir, "themes", m.Theme+".toml")
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			c.warnings = append(c.warnings, fmt.Sprintf("theme %q: no file %s", m.Theme, path))
		} else if err := c.decode(path, &styles); err != nil {
			return nil, err
		}
		// The theme's keys win over all of delta's sections.
		gitCfg = maps.Clone(gitCfg)
		if gitCfg == nil {
			gitCfg = theme.Config{}
		}
		for k, v := range styles {
			section := "delta."
			if theme.IsJdiffKey(k) {
				section = "jdiff."
			}
			gitCfg[section+k] = v
		}
	}
	c.Theme = theme.Load(gitCfg, features)
	return c, nil
}

// decode reads the TOML file path into v, if it exists, and records its
// unknown keys.
func (c *Config) decode(path string, v any) error {
	md, err := toml.DecodeFile(path, v)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("%s: %w", path, err)
	}
	var unknown []string
	for _, k := range md.Undecoded() {
		unknown = append(unknown, k.String())
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		c.warnings = append(c.warnings, fmt.Sprintf("%s: unknown %s", filepath.Base(path), strings.Join(unknown, ", ")))
	}
	return nil
}

func (c *Config) state() stateFile {
	s := c.Settings
	return stateFile{Flow: s.Flow, SideBySide: s.SideBySide, Preview: s.Preview, ShowWhitespaces: s.ShowWhitespaces}
}

// SaveState writes the toggled settings to state.toml, through a temporary
// file so that the file is never left half written.
func (c *Config) SaveState() error {
	if c.statePath == "" {
		return nil
	}
	var b bytes.Buffer
	b.WriteString("# Written by jdiff: the toggles as last set, over main.toml.\n")
	if err := toml.NewEncoder(&b).Encode(c.state()); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.statePath), 0o755); err != nil {
		return err
	}
	tmp := c.statePath + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.statePath)
}

// Warning returns the problems with the theme and the files, or "".
func (c *Config) Warning() string {
	var parts []string
	if len(c.Theme.Warnings) > 0 {
		parts = append(parts, "theme: "+strings.Join(c.Theme.Warnings, "; "))
	}
	return strings.Join(append(parts, c.warnings...), "; ")
}
