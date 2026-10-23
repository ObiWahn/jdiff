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
	// character by character, and a run of whitespace in place of another
	// one by its length, so only the added or removed part is marked.
	OnlyChangedWhitespaces bool `toml:"only_changed_whitespaces"`
	// WatchInterval (watch_interval) is how often, in seconds, files on disk
	// are checked for changes and reloaded: both files of a comparison, or
	// the current file of the file list and its neighbors. 0 turns it off.
	WatchInterval float64 `toml:"watch_interval"`
	// LineEnd (line_end) is drawn at the end of lines while all whitespace
	// is shown; it must be one column wide.
	LineEnd string `toml:"line_end"`
	// CREnd (cr_end) is drawn for the \r of a \r\n line end where it
	// changed, and on all lines while ShowCR is set; one column wide.
	CREnd string `toml:"cr_end"`
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
	// not only changed whitespace, and the end of lines as LineEnd; w
	// toggles it.
	ShowWhitespaces bool `toml:"show_whitespaces"`
	// MarkWhitespace (mark_whitespace) draws changed whitespace as · and →
	// in the style of changed words: indentation, tabs and runs of spaces,
	// not a single space between changed words; off, all changed whitespace
	// looks like unchanged whitespace. W toggles it.
	MarkWhitespace bool `toml:"mark_whitespace"`
	// ShowTrailing (show_trailing) draws whitespace at the end of lines as ·
	// and → in the trailing whitespace style. t toggles it.
	ShowTrailing bool `toml:"show_trailing"`
	// ShowCR (show_cr) draws the \r of all \r\n line ends as CREnd, not
	// only of changed ones. e toggles it.
	ShowCR bool `toml:"show_cr"`
	// Wrap (wrap) wraps lines longer than their pane onto further screen
	// lines; off, they are cut and h and l scroll them. r (reflow) toggles
	// it.
	Wrap bool `toml:"wrap"`
	// BoldChanges (bold_changes) draws changed words and whitespace bold
	// where the theme says so; off, without bold, which terminals often
	// draw brighter. b toggles it.
	BoldChanges bool `toml:"bold_changes"`
	// FullMessages (full_messages) shows the whole commit message in the
	// flow view of the log; off, only the subject. c toggles it.
	FullMessages bool `toml:"full_messages"`
	// StayInCommit (stay_in_commit) keeps the cursor of the flow view of the
	// log in the current commit: moves out of it stop with a warning, N and
	// P still switch commits. C toggles it.
	StayInCommit bool `toml:"stay_in_commit"`
}

// Default returns the default settings.
func Default() Settings {
	return Settings{
		Context:                3,
		CollapseUnchanged:      true,
		OnlyChangedWhitespaces: true,
		WatchInterval:          1,
		LineEnd:                "¶",
		CREnd:                  "␍",
		Toggles: Toggles{
			SideBySide:      true,
			Preview:         true,
			ShowWhitespaces: false,
			MarkWhitespace:  true,
			BoldChanges:     true,
			FullMessages:    true,
		},
	}
}
