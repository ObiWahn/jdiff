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
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/keys"
	"jdiff/internal/settings"
	"jdiff/internal/theme"
)

// Config is shared by pointer, so a setting changed in one view applies to
// all of them.
type Config struct {
	Theme    *theme.Theme
	Keys     *keys.Map // the keys of all pages, built-in and the user's; SetKeys replaces them
	Settings settings.Settings
	Themes   []theme.Spec // all themes found; Theme is the selected one

	warnings  []string // problems with the files
	dir       string   // the configuration directory, empty for none
	statePath string   // where SaveState writes, empty for no state file

	marks marksState // jump marks, see LoadMarks

	watchIgnore []*regexp.Regexp // watch_ignore compiled, see WatchIgnored
	ignoredHide []*regexp.Regexp // ignored_hide compiled, see IgnoredHidden
	// IgnoredHide is ignored_hide as read, which SaveState writes.
	IgnoredHide []string

	// AutoReload is whether what is shown is loaded again when the repository
	// changes, now; R toggles it. It ends by itself, see
	// Settings.AutoReloadTimeout, and starts on with Settings.AutoReload.
	AutoReload bool

	// Flags are the toggles the command line turned on, for the run only:
	// see SetFlags. A key switching one ends its flag, so that it is saved
	// again.
	Flags Flags
	// saved is what SaveState writes for untracked, ignored and excluded
	// while their flags are set: the values of the files.
	saved settings.Toggles

	// Search is what / searched for, which . and , search again and the
	// views highlight; it is not saved.
	Search Search

	// Version is the version of jdiff the pages show at the right end of
	// their status bars, as --version prints it; "" shows none, as in the
	// tests.
	Version string
}

// Flags are what the command line turns on for the run only, without
// changing state.toml.
type Flags struct {
	// Untracked (--untracked) shows the untracked files; SaveState keeps the
	// value of the files.
	Untracked bool
	// Ignored (--ignored) and Excluded (--excluded) show the ignored files
	// of .gitignore and of .git/info/exclude, with the untracked ones; as
	// Untracked, SaveState keeps the values of the files.
	Ignored, Excluded bool
	// AutoReload (--auto-reload) starts auto reload, which then does not end
	// after Settings.AutoReloadTimeout; Settings.AutoReload stays as saved.
	AutoReload bool
}

// SetFlags turns on what f asks for, for the run only.
func (c *Config) SetFlags(f Flags) {
	c.Flags, c.saved = f, c.Settings.Toggles
	c.Settings.Untracked = c.Settings.Untracked || f.Untracked
	c.Settings.Ignored = c.Settings.Ignored || f.Ignored
	c.Settings.Excluded = c.Settings.Excluded || f.Excluded
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
	c := &Config{Theme: theme.Default(), Keys: keys.Builtin(), Settings: settings.Default()}
	c.watchIgnore = c.compilePatterns("watch_ignore", DefaultWatchIgnore)
	c.IgnoredHide, c.ignoredHide = slices.Clone(DefaultIgnoredHide), defaultIgnoredHide()
	return c
}

// defaultIgnoredHide is DefaultIgnoredHide compiled, once: Default runs
// often, in the tests for each view, and a compiled expression may be shared.
var defaultIgnoredHide = sync.OnceValue(func() []*regexp.Regexp {
	var res []*regexp.Regexp
	for _, expr := range DefaultIgnoredHide {
		res = append(res, regexp.MustCompile(expr))
	}
	return res
})

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

// minInterval and maxInterval bound watch_interval and auto_reload_interval,
// in seconds.
const (
	minInterval = 0.1
	maxInterval = 86400
)

// mainFile is the content of main.toml. WatchIgnore and IgnoredHide are not
// among the settings, which views compare as a whole.
type mainFile struct {
	settings.Settings
	Theme string `toml:"theme"`
	// IgnoredHide (ignored_hide) are regular expressions of paths as those
	// of WatchIgnore: an ignored or excluded file that one matches, or a
	// directory above it, is not shown. As a toggle, state.toml keeps it,
	// see stateFile. See IgnoredHidden.
	IgnoredHide []string `toml:"ignored_hide"`
	// WatchIgnore (watch_ignore) are regular expressions of paths from the
	// top of the working tree, with / and directories with a / at the end:
	// while auto reload watches the working tree, a change of a file that
	// one matches does not run git status, and a directory that one matches
	// is not watched, nor those below it. See WatchIgnored.
	WatchIgnore []string `toml:"watch_ignore"`
}

// stateFile is the content of state.toml: the toggles and ignored_hide.
// SaveState writes ignored_hide itself, one per line: the encoder would put
// them on one.
type stateFile struct {
	settings.Toggles
	IgnoredHide []string `toml:"ignored_hide"`
}

// DefaultIgnoredHide is ignored_hide by default: directories of tools, at
// any depth, that hold many files nobody edits.
var DefaultIgnoredHide = []string{
	`(^|/)node_modules/$`, `(^|/)bower_components/$`, `(^|/)\.venv/$`, `(^|/)__pycache__/$`,
	`(^|/)\.mypy_cache/$`, `(^|/)\.pytest_cache/$`, `(^|/)\.ruff_cache/$`, `(^|/)\.tox/$`, `(^|/)\.gradle/$`,
	`(^|/)\.terraform/$`, `(^|/)\.next/$`, `(^|/)\.nuxt/$`, `(^|/)\.cache/$`, `(^|/)\.direnv/$`,
}

// DefaultWatchIgnore is watch_ignore by default: editors' files beside the
// one saved, Vim's swap files, backups and the file it tries writing (4913),
// Emacs' locks and autosaves.
var DefaultWatchIgnore = []string{`\.sw[a-px]$`, `~$`, `(^|/)\.#`, `(^|/)#[^/]*#$`, `(^|/)4913$`, `\.tmp$`}

// Load reads the files in dir over the default settings and selects the
// theme. The themes are jdiff's defaults ("default"), delta's git config
// gitCfg with the features env (DELTA_FEATURES) and flag (--features) select
// ("delta"), and the theme files; main.toml selects one by name, else it is
// "delta". Missing files are skipped and unknown keys reported by Warning;
// files that cannot be parsed are an error, unless they are theme files not
// selected. An empty dir reads no files.
func Load(dir string, gitCfg theme.Config, env, flag string) (*Config, error) {
	c := &Config{Keys: keys.Builtin(), Settings: settings.Default(), IgnoredHide: slices.Clone(DefaultIgnoredHide)}
	c.Themes = []theme.Spec{theme.Builtin(), theme.FromGit(gitCfg, theme.Features(gitCfg, env, flag))}
	name, ignore := "", DefaultWatchIgnore
	if dir != "" {
		c.dir = dir
		c.statePath = filepath.Join(dir, "state.toml")
		m := mainFile{Settings: c.Settings, WatchIgnore: slices.Clone(DefaultWatchIgnore), IgnoredHide: c.IgnoredHide}
		if err := c.decode(filepath.Join(dir, "main.toml"), &m); err != nil {
			return nil, err
		}
		c.Settings, ignore, c.IgnoredHide = m.Settings, m.WatchIgnore, m.IgnoredHide
		c.checkIntervals()
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
		st := stateFile{Toggles: c.Settings.Toggles, IgnoredHide: c.IgnoredHide}
		if err := c.decode(c.statePath, &st); err != nil {
			return nil, err
		}
		c.Settings.Toggles, c.IgnoredHide = st.Toggles, st.IgnoredHide
		// inotify_max_dirs is a toggle: it may come from main.toml or
		// state.toml, so the warning names neither.
		if n := c.Settings.InotifyMaxDirs; n < 0 {
			c.warnings = append(c.warnings, fmt.Sprintf("inotify_max_dirs %d is negative, it is off", n))
			c.Settings.InotifyMaxDirs = 0
		}
		var problems []string
		if c.Keys, problems = keys.Load(dir); len(problems) > 0 {
			c.warnings = append(c.warnings, keys.FileName+": "+strings.Join(problems, "; ")+"; the built-in keys are used")
		}
		name = m.Theme
		if err := c.readThemes(filepath.Join(dir, "themes"), name); err != nil {
			return nil, err
		}
	}
	c.Theme = theme.Resolve(c.selectTheme(name, dir))
	c.watchIgnore = c.compilePatterns("watch_ignore", ignore)
	// A toggle: from main.toml or state.toml, so the warning names main.toml
	// for both, as for watch_ignore.
	if slices.Equal(c.IgnoredHide, DefaultIgnoredHide) {
		c.ignoredHide = defaultIgnoredHide()
	} else {
		c.ignoredHide = c.compilePatterns("ignored_hide", c.IgnoredHide)
	}
	return c, nil
}

// checkIntervals clamps watch_interval and auto_reload_interval of main.toml
// to what works, with a warning for each.
func (c *Config) checkIntervals() {
	if iv := c.Settings.WatchInterval; iv < 0 {
		c.warnings = append(c.warnings, fmt.Sprintf("main.toml: watch_interval %v is negative, files are not watched", iv))
		c.Settings.WatchInterval = 0
	}
	// Shorter checks keep a core busy; longer ones overflow the
	// durations they become.
	if iv := c.Settings.WatchInterval; iv > 0 && iv < minInterval {
		c.warnings = append(c.warnings, fmt.Sprintf("main.toml: watch_interval %v is below %v, %v is used", iv, minInterval, minInterval))
		c.Settings.WatchInterval = minInterval
	}
	if iv := c.Settings.WatchInterval; iv > maxInterval {
		c.warnings = append(c.warnings, fmt.Sprintf("main.toml: watch_interval %v is above %d, %d is used", iv, maxInterval, maxInterval))
		c.Settings.WatchInterval = maxInterval
	}
	if iv := c.Settings.AutoReloadInterval; iv > maxInterval {
		c.warnings = append(c.warnings, fmt.Sprintf("main.toml: auto_reload_interval %d is above %d, %d is used", iv, maxInterval, maxInterval))
		c.Settings.AutoReloadInterval = maxInterval
	}
}

// compilePatterns compiles exprs, the list key of main.toml; one that is no
// regular expression is left out with a warning, so that a typo never stops
// jdiff.
func (c *Config) compilePatterns(key string, exprs []string) []*regexp.Regexp {
	var res []*regexp.Regexp
	for _, expr := range exprs {
		re, err := regexp.Compile(expr)
		if err != nil {
			c.warnings = append(c.warnings, fmt.Sprintf("main.toml: %s %q is not a regular expression, left out: %v", key, expr, err))
			continue
		}
		res = append(res, re)
	}
	return res
}

// WatchIgnored reports whether a pattern of watch_ignore matches rel, a path
// from the top of the working tree with /, of a directory with a / at the
// end.
func (c *Config) WatchIgnored(rel string) bool {
	return slices.ContainsFunc(c.watchIgnore, func(re *regexp.Regexp) bool { return re.MatchString(rel) })
}

// IgnoredHidden reports whether a pattern of ignored_hide matches rel, a
// path as for WatchIgnored. SetIgnoredHide changes them: git commands in the
// background take IgnoredHider instead.
func (c *Config) IgnoredHidden(rel string) bool { return c.IgnoredHider()(rel) }

// IgnoredHider returns IgnoredHidden of the patterns of now, which
// SetIgnoredHide leaves as they are: it replaces them, so that git commands
// in the background may go on with these.
func (c *Config) IgnoredHider() func(rel string) bool {
	res := c.ignoredHide
	return func(rel string) bool {
		return slices.ContainsFunc(res, func(re *regexp.Regexp) bool { return re.MatchString(rel) })
	}
}

// CheckPattern returns why expr cannot be a pattern of ignored_hide, as it
// is no regular expression, or nil.
func CheckPattern(expr string) error {
	_, err := regexp.Compile(expr)
	return err
}

// SetIgnoredHide makes list ignored_hide, as the settings popup edits it,
// unless one of them is no regular expression: then nothing changes. It
// does not save it, see SaveState.
func (c *Config) SetIgnoredHide(list []string) error {
	var res []*regexp.Regexp
	for _, expr := range list {
		re, err := regexp.Compile(expr)
		if err != nil {
			return fmt.Errorf("%q: %w", expr, err)
		}
		res = append(res, re)
	}
	c.IgnoredHide, c.ignoredHide = slices.Clone(list), res
	return nil
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
		slices.Sort(keys)
		c.warnings = append(c.warnings, fmt.Sprintf("%s: unknown %s", filepath.Base(path), strings.Join(keys, ", ")))
	}
}

// SaveState writes the toggled settings to state.toml, through a temporary
// file so that the file is never left half written.
func (c *Config) SaveState() error {
	if c.statePath == "" {
		return nil
	}
	t := c.Settings.Toggles
	if c.Flags.Untracked {
		t.Untracked = c.saved.Untracked
	}
	if c.Flags.Ignored {
		t.Ignored = c.saved.Ignored
	}
	if c.Flags.Excluded {
		t.Excluded = c.saved.Excluded
	}
	tail, err := tomlList("ignored_hide", c.IgnoredHide)
	if err != nil {
		return err
	}
	return writeTOML(c.statePath, "# Written by jdiff: the toggles as last set, over main.toml.\n", t, tail)
}

// tomlList returns key = list as TOML, one string per line, to be read and
// edited by hand: literal strings where they can be, as regular expressions
// need no doubled backslashes there; others as the encoder quotes them.
func tomlList(key string, list []string) (string, error) {
	if len(list) == 0 {
		return key + " = []\n", nil
	}
	var b strings.Builder
	b.WriteString(key + " = [\n")
	for _, s := range list {
		if !strings.ContainsAny(s, "'\n\r") && !strings.ContainsFunc(s, unicode.IsControl) {
			b.WriteString("    '" + s + "',\n")
			continue
		}
		v, err := toml.Marshal(map[string]string{"v": s})
		if err != nil {
			return "", err
		}
		b.WriteString("    " + strings.TrimSuffix(strings.TrimPrefix(string(v), "v = "), "\n") + ",\n")
	}
	b.WriteString("]\n")
	return b.String(), nil
}

// writeTOML writes header, v as TOML and tail to path, see writeFile.
func writeTOML(path, header string, v any, tail string) error {
	var b bytes.Buffer
	b.WriteString(header)
	if err := toml.NewEncoder(&b).Encode(v); err != nil {
		return err
	}
	b.WriteString(tail)
	return writeFile(path, b.Bytes())
}

// writeFile writes data to path, through a temporary file so that the file is
// never left half written. Each write has a temporary file of its own: jdiffs
// running at the same time must not rename each other's.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	err = errors.Join(err, tmp.Close(), os.Chmod(tmp.Name(), 0o644))
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// SetKeys makes rows, the keys of each row, the rows of action on page in the
// user's keybindings.toml, nil the built-in ones, saves it and makes its keys
// the keys at once. A file that would have a problem, as a key with two
// actions on one page, is not saved and changes nothing: the error says what
// is wrong. The rest of the file stays as written, see keys.SetUserRows.
func (c *Config) SetKeys(page, action string, rows [][]string) error {
	if c.dir == "" {
		return errors.New("no configuration directory for " + keys.FileName)
	}
	path := filepath.Join(c.dir, keys.FileName)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	text, err := keys.SetUserRows(string(data), page, action, rows)
	if err != nil {
		return err
	}
	km, problems := keys.Builtin().ApplyUserKeys(text)
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	if err := writeFile(path, []byte(text)); err != nil {
		return err
	}
	c.Keys = &km
	return nil
}

// HasUserKeys reports whether there is a user's keybindings.toml, which
// ResetKeys would move away.
func (c *Config) HasUserKeys() bool {
	if c.dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(c.dir, keys.FileName))
	return err == nil
}

// ResetKeys moves the user's keybindings.toml to keybindings.toml.bak, over an
// older one, so that what was written by hand is kept, and makes the built-in
// keys the keys at once.
func (c *Config) ResetKeys() error {
	if c.dir == "" {
		return errors.New("no configuration directory for " + keys.FileName)
	}
	path := filepath.Join(c.dir, keys.FileName)
	if err := os.Rename(path, path+".bak"); err != nil {
		return err
	}
	c.Keys = keys.Builtin()
	return nil
}

// Dir returns the configuration directory the files were read from, "" for
// none.
func (c *Config) Dir() string { return c.dir }

// Warning returns the problems with the theme and the files, or "".
func (c *Config) Warning() string {
	var parts []string
	if len(c.Theme.Warnings) > 0 {
		parts = append(parts, "theme: "+strings.Join(c.Theme.Warnings, "; "))
	}
	return strings.Join(append(parts, c.warnings...), "; ")
}
