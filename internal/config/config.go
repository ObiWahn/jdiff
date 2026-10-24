// Package config bundles what every view needs to know: the theme and the
// user settings. All views share one Config.
//
// The settings come from ~/.config/jdiff: main.toml holds the settings and the
// name of the theme, state.toml, written by jdiff, the toggles as last set.
// Each themes/<name>.toml is a theme: style and label options as in delta's
// git config, and named colors in a [colors] table. A theme leaves unset
// options to jdiff's defaults; without a name, delta's git config is the
// theme.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/settings"
	"jdiff/internal/theme"
)

// Config is shared by pointer, so a setting changed in one view applies to
// all of them.
type Config struct {
	Theme    *theme.Theme
	Settings settings.Settings
	Themes   []theme.Spec // all themes found; Theme is the selected one

	warnings  []string // problems with the files
	dir       string   // the configuration directory, empty for none
	statePath string   // where SaveState writes, empty for no state file

	marks marksState // jump marks, see LoadMarks

	// Search is what / searched for, which . and , search again and the
	// views highlight; it is not saved.
	Search Search
}

// Query is a search of /: plain text, or with Regex a regular expression.
type Query struct {
	Text  string
	Regex bool
}

// Search is the state of /: the query searched for last, empty for none,
// and all searched for, oldest first.
type Search struct {
	Query
	History []Query
}

// Add makes q the query searched for, last in the history, once.
func (s *Search) Add(q Query) {
	s.Query = q
	s.History = append(slices.DeleteFunc(s.History, func(h Query) bool { return h == q }), q)
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

// Load reads the files in dir over the default settings and selects the
// theme. The themes are jdiff's defaults ("default"), delta's git config
// gitCfg with the features env (DELTA_FEATURES) and flag (--features) select
// ("delta"), and the theme files; main.toml selects one by name, else it is
// "delta". Missing files are skipped and unknown keys reported by Warning;
// files that cannot be parsed are an error, unless they are theme files not
// selected. An empty dir reads no files.
func Load(dir string, gitCfg theme.Config, env, flag string) (*Config, error) {
	c := &Config{Settings: settings.Default()}
	c.Themes = []theme.Spec{theme.Builtin(), theme.FromGit(gitCfg, theme.Features(gitCfg, env, flag))}
	name := ""
	if dir != "" {
		c.dir = dir
		c.statePath = filepath.Join(dir, "state.toml")
		m := mainFile{Settings: c.Settings}
		if err := c.decode(filepath.Join(dir, "main.toml"), &m); err != nil {
			return nil, err
		}
		c.Settings = m.Settings
		if iv := c.Settings.WatchInterval; iv < 0 {
			c.warnings = append(c.warnings, fmt.Sprintf("main.toml: watch_interval %v is negative, files are not watched", iv))
			c.Settings.WatchInterval = 0
		}
		if n := c.Settings.InotifyMaxDirs; n < 0 {
			c.warnings = append(c.warnings, fmt.Sprintf("main.toml: inotify_max_dirs %d is negative, inotify is off", n))
			c.Settings.InotifyMaxDirs = 0
		}
		// Lines are cut and wrapped by columns: the symbols take one.
		def := settings.Default()
		for _, sym := range []struct {
			key      string
			v        *string
			fallback string
		}{
			{"line_end", &c.Settings.LineEnd, def.LineEnd}, {"cr_end", &c.Settings.CREnd, def.CREnd},
			{"extends", &c.Settings.Extends, def.Extends}, {"precedes", &c.Settings.Precedes, def.Precedes},
		} {
			if ansi.StringWidth(*sym.v) != 1 {
				c.warnings = append(c.warnings, fmt.Sprintf("main.toml: %s %q is not one column wide, %q is used",
					sym.key, *sym.v, sym.fallback))
				*sym.v = sym.fallback
			}
		}
		// state.toml holds only the toggles.
		if err := c.decode(c.statePath, &c.Settings.Toggles); err != nil {
			return nil, err
		}
		name = m.Theme
		if err := c.readThemes(filepath.Join(dir, "themes"), name); err != nil {
			return nil, err
		}
	}
	c.Theme = theme.Resolve(c.selectTheme(name, dir))
	return c, nil
}

// readThemes adds the theme files in dir to the themes. A file that cannot
// be parsed is an error when it is the selected theme, else a warning, and
// unknown keys are reported for the selected theme only.
func (c *Config) readThemes(dir, selected string) error {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.toml"))
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".toml")
		if c.theme(name) != nil {
			c.warnings = append(c.warnings, fmt.Sprintf("%s: %q is a built-in theme, file ignored", filepath.Base(path), name))
			continue
		}
		sp, unknown, err := theme.FromFile(path)
		if err != nil {
			err = fmt.Errorf("%s: %w", path, err)
			if name == selected {
				return err
			}
			c.warnings = append(c.warnings, err.Error())
			continue
		}
		if name == selected {
			c.unknown(path, unknown)
		}
		c.Themes = append(c.Themes, sp)
	}
	return nil
}

// selectTheme returns the theme called name, "delta" for "", or with a
// warning "delta" when there is none.
func (c *Config) selectTheme(name, dir string) theme.Spec {
	if name == "" {
		name = "delta"
	}
	if sp := c.theme(name); sp != nil {
		return *sp
	}
	c.warnings = append(c.warnings, fmt.Sprintf("theme %q: no file %s", name, filepath.Join(dir, "themes", name+".toml")))
	return *c.theme("delta")
}

// theme returns the theme called name, or nil.
func (c *Config) theme(name string) *theme.Spec {
	for i := range c.Themes {
		if c.Themes[i].Name == name {
			return &c.Themes[i]
		}
	}
	return nil
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
	c.unknown(path, unknown)
	return nil
}

// unknown reports the unknown keys of the file path.
func (c *Config) unknown(path string, keys []string) {
	if len(keys) > 0 {
		sort.Strings(keys)
		c.warnings = append(c.warnings, fmt.Sprintf("%s: unknown %s", filepath.Base(path), strings.Join(keys, ", ")))
	}
}

// SaveState writes the toggled settings to state.toml, through a temporary
// file so that the file is never left half written.
func (c *Config) SaveState() error {
	if c.statePath == "" {
		return nil
	}
	return writeTOML(c.statePath, "# Written by jdiff: the toggles as last set, over main.toml.\n", c.Settings.Toggles)
}

// writeTOML writes header and v as TOML to path, through a temporary file
// so that the file is never left half written.
func writeTOML(path, header string, v any) error {
	var b bytes.Buffer
	b.WriteString(header)
	if err := toml.NewEncoder(&b).Encode(v); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Warning returns the problems with the theme and the files, or "".
func (c *Config) Warning() string {
	var parts []string
	if len(c.Theme.Warnings) > 0 {
		parts = append(parts, "theme: "+strings.Join(c.Theme.Warnings, "; "))
	}
	return strings.Join(append(parts, c.warnings...), "; ")
}
