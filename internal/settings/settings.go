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
	// that are not watched (InotifyMaxDirs) are checked for changes and
	// reloaded while auto reload is on: both files of a comparison, or the
	// current file of the file list and its neighbors. 0 turns the checks
	// off, and watching too.
	WatchInterval float64 `toml:"watch_interval"`
	// AutoReloadInterval (auto_reload_interval) is how often, in seconds, auto
	// reload runs git status, which sees new files, and all changes where
	// Linux does not report them at once.
	AutoReloadInterval int `toml:"auto_reload_interval"`
	// LineEnd (line_end) is drawn at the end of lines while all whitespace
	// is shown; it must be one column wide.
	LineEnd string `toml:"line_end"`
	// CREnd (cr_end) is drawn for the \r of a \r\n line end where it
	// changed, and on all lines while ShowCR is set; one column wide.
	CREnd string `toml:"cr_end"`
	// Extends (extends) and Precedes (precedes) mark lines cut at the edge
	// of their pane while lines are not wrapped: Extends in the last column
	// when the line goes on, Precedes in the first when text is scrolled out
	// to the left; one column wide each.
	Extends  string `toml:"extends"`
	Precedes string `toml:"precedes"`
	// Untracked (untracked) also shows the untracked files in jdiff diff,
	// as added, where it compares with the working tree. The key of action
	// untracked toggles it and --untracked turns it on, for the run only: it
	// is the one toggle state.toml does not keep.
	Untracked bool `toml:"untracked"`
	Toggles
}

// Toggles are the settings switched by keys, which state.toml keeps as last
// set; their TOML keys are on the same level as the other settings. The keys
// are those of the action named with each, in keybindings.toml of package ui,
// which the user may change.
type Toggles struct {
	// Flow (flow) shows all files one after the other instead of one file
	// at a time; action flow.
	Flow bool `toml:"flow"`
	// SideBySide (side_by_side) shows the old and new file next to each
	// other; off, they are shown in one column like git diff; action layout.
	SideBySide bool `toml:"side_by_side"`
	// Preview (preview) shows a box below the commit and file lists with
	// the selected commit's message or file's diff; action preview.
	Preview bool `toml:"preview"`
	// ShowWhitespaces (show_whitespaces) draws all whitespace as · and →,
	// not only changed whitespace, and the end of lines as LineEnd; action
	// whitespace.
	ShowWhitespaces bool `toml:"show_whitespaces"`
	// MarkWhitespace (mark_whitespace) draws changed whitespace as · and →
	// in the style of changed words: indentation, tabs and runs of spaces,
	// not a single space between changed words; off, all changed whitespace
	// looks like unchanged whitespace; action mark_whitespace.
	MarkWhitespace bool `toml:"mark_whitespace"`
	// ShowTrailing (show_trailing) draws whitespace at the end of lines as ·
	// and → in the trailing whitespace style; action trailing.
	ShowTrailing bool `toml:"show_trailing"`
	// ShowCR (show_cr) draws the \r of all \r\n line ends as CREnd, not
	// only of changed ones; action line_ends.
	ShowCR bool `toml:"show_cr"`
	// Wrap (wrap) wraps lines longer than their pane onto further screen
	// lines; off, they are cut and scrolled sideways; action wrap.
	Wrap bool `toml:"wrap"`
	// BoldChanges (bold_changes) draws changed words and whitespace bold
	// where the theme says so; off, without bold, which terminals often
	// draw brighter; action bold.
	BoldChanges bool `toml:"bold_changes"`
	// FullMessages (full_messages) shows the whole commit message in the
	// flow view of the log; off, only the subject; action full_messages.
	FullMessages bool `toml:"full_messages"`
	// StayInCommit (stay_in_commit) keeps the cursor of the flow view of the
	// log in the current commit: moves out of it stop with a warning, the keys
	// of action history still switch commits; action stay_in_commit.
	StayInCommit bool `toml:"stay_in_commit"`
	// SearchRegex (search_regex) searches, action search, for a regular expression;
	// off, for plain text. shift+tab in the search bar toggles it.
	SearchRegex bool `toml:"search_regex"`
	// SearchAll (search_all) searches, action search, in all lines; off, only in
	// changed ones, added, removed and moved. tab in the search bar toggles
	// it.
	SearchAll bool `toml:"search_all"`
	// AutoReloadTimeout (auto_reload_timeout) is after how many minutes
	// without a key pressed or a change of the repository auto reload ends,
	// and with it its git commands; 0 is never. The settings popup sets it.
	AutoReloadTimeout int `toml:"auto_reload_timeout"`
	// InotifyMaxDirs (inotify_max_dirs) is the most directories whose
	// changes the system reports (on Linux, inotify) while auto reload is on,
	// the most recently used ones: the directories of the files on disk, not
	// their subdirectories, and those of the working tree; HEAD and the index
	// are watched besides. Files in the others are checked every
	// WatchInterval. 0 watches only HEAD and the index. The settings popup
	// sets it.
	InotifyMaxDirs int `toml:"inotify_max_dirs"`
}

// Default returns the default settings.
func Default() Settings {
	return Settings{
		Context:                3,
		CollapseUnchanged:      true,
		OnlyChangedWhitespaces: true,
		WatchInterval:          1,
		AutoReloadInterval:     10,
		LineEnd:                "¶",
		CREnd:                  "␍",
		Extends:                "»",
		Precedes:               "«",
		Toggles: Toggles{
			SideBySide:      true,
			Preview:         true,
			ShowWhitespaces: false,
			MarkWhitespace:  true,
			BoldChanges:     true,
			FullMessages:    true,

			AutoReloadTimeout: 5,
			InotifyMaxDirs:    100,
		},
	}
}
