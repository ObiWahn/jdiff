// Package settings holds jdiff's user settings.
package settings

// Settings are the user-configurable options. They are fixed defaults for
// now and meant to be read from a config file later.
type Settings struct {
	// Context is the number of unchanged lines kept visible around changes
	// while unchanged code is collapsed.
	Context int
	// CollapseUnchanged (collapse_unchanged) starts the diff view with
	// unchanged code collapsed.
	CollapseUnchanged bool
	// ShowWhitespaces (show_whitespaces) draws all whitespace as · and →,
	// not only changed whitespace.
	ShowWhitespaces bool
	// OnlyChangedWhitespaces (only_changed_whitespaces) compares indentation
	// character by character, so only its added or removed part is marked.
	OnlyChangedWhitespaces bool
	// Flow (flow) shows all files one after the other instead of one file
	// at a time; f toggles it.
	Flow bool
}

// Default returns the default settings.
func Default() Settings {
	return Settings{
		Context:                3,
		CollapseUnchanged:      true,
		ShowWhitespaces:        false,
		OnlyChangedWhitespaces: true,
	}
}
