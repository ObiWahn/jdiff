// Package config bundles what every view needs to know: the theme and the
// user settings. All views share one Config.
package config

import (
	"jdiff/internal/settings"
	"jdiff/internal/theme"
)

// Config is shared by pointer, so a setting changed in one view applies to
// all of them.
type Config struct {
	Theme    *theme.Theme
	Settings settings.Settings
}

// Default returns the default theme and settings.
func Default() *Config {
	return &Config{Theme: theme.Default(), Settings: settings.Default()}
}
