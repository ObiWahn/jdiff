// Package settings holds jdiff's user settings.
package settings

// Settings are the user-configurable options, read from main.toml and,
// for the toggles, state.toml (see package config); the TOML keys are given
// in brackets.
type Settings struct {
	// Context (context) is the number of unchanged lines kept visible around changes
	// while unchanged code is collapsed.
	Context int `toml:"context"`
	// CollapseUnchanged (collapse_unchanged) starts the diff view with
	// unchanged code collapsed.
	CollapseUnchanged bool `toml:"collapse_unchanged"`
	// OnlyChangedWhitespaces (only_changed_whitespaces) compares indentation
	// character by character, so only its added or removed part is marked.
	OnlyChangedWhitespaces bool `toml:"only_changed_whitespaces"`
	Toggles
}

// Toggles are the settings switched by keys, which state.toml keeps as last
// set; their TOML keys are on the same level as the other settings.
type Toggles struct {
	// Flow (flow) shows all files one after the other instead of one file
	// at a time; f toggles it.
	Flow bool `toml:"flow"`
	// SideBySide (side_by_side) shows the old and new file next to each
	// other; off, they are shown in one column like git diff. s toggles it.
	SideBySide bool `toml:"side_by_side"`
	// Preview (preview) shows a box below the commit and file lists with
	// the selected commit's message or file's diff; v toggles it.
	Preview bool `toml:"preview"`
	// ShowWhitespaces (show_whitespaces) draws all whitespace as · and →,
	// not only changed whitespace; w toggles it.
	ShowWhitespaces bool `toml:"show_whitespaces"`
}

// Default returns the default settings.
func Default() Settings {
	return Settings{
		Context:                3,
		CollapseUnchanged:      true,
		OnlyChangedWhitespaces: true,
		Toggles: Toggles{
			SideBySide:      true,
			Preview:         true,
			ShowWhitespaces: false,
		},
	}
}
