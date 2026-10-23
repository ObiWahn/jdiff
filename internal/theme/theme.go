// Package theme builds jdiff's styles from delta's git config settings.
//
// jdiff renders in 16 color mode: every color is an ANSI color, so the
// terminal palette decides the actual colors. Hex and 256 colors in a theme
// are mapped to the nearest ANSI color.
package theme

import (
	"fmt"
	"slices"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds all styles of the jdiff views.
type Theme struct {
	Minus, MinusNonEmph, MinusEmph, MinusIndent lipgloss.Style
	Plus, PlusNonEmph, PlusEmph, PlusIndent     lipgloss.Style
	Zero                                        lipgloss.Style

	NumMinus, NumPlus, NumZero lipgloss.Style
	NumLeft, NumRight          lipgloss.Style // separators after the numbers

	Title      lipgloss.Style // file names
	Decoration lipgloss.Style // pane separator
	Hint       lipgloss.Style // status bar

	Anchor, Pending, Cursor, Filler lipgloss.Style
	Whitespace                      lipgloss.Style // unchanged whitespace when shown
	FileBar, CommitBar              lipgloss.Style // flow mode: start of a file, a commit
	CommitHash                      lipgloss.Style // commit hashes in the log
	Branch                          lipgloss.Style // branch above the commit list

	AddedLabel, RemovedLabel, ModifiedLabel, RenamedLabel string

	// Warnings lists settings that could not be used.
	Warnings []string
}

// Defaults for 16 color mode. Unlike delta's defaults they use foreground
// colors only, since ANSI backgrounds are too loud for whole lines.
var deltaDefaults = map[string]string{
	"minus-style":              "red",
	"minus-emph-style":         "bright-red bold",
	"plus-style":               "green",
	"plus-emph-style":          "bright-green bold",
	"zero-style":               "normal",
	"line-numbers-minus-style": "red",
	"line-numbers-plus-style":  "green",
	"line-numbers-zero-style":  "normal",
	"line-numbers-left-style":  "blue",
	"line-numbers-right-style": "blue",
	"file-style":               "blue",
	"file-decoration-style":    "blue ul",
	"inline-hint-style":        "blue",
	"file-added-label":         "added:",
	"file-removed-label":       "removed:",
	"file-modified-label":      "",
	"file-renamed-label":       "renamed:",
}

var jdiffDefaults = map[string]string{
	"anchor-style":  "yellow bold",
	"pending-style": "magenta bold",
	"cursor-style":  "reverse bold",
	// Bars with a background, so file and commit borders stand out.
	"file-bar-style":    "bold bright-white blue",
	"commit-bar-style":  "bold black yellow",
	"commit-hash-style": "yellow",
	"branch-style":      "yellow bold",
}

// jdiffKeys are the style keys of the [jdiff] git config section; all
// others are delta's.
var jdiffKeys = []string{"filler-style", "whitespace-style", "minus-indent-style", "plus-indent-style"}

// IsJdiffKey reports whether key is a [jdiff] key rather than a delta one.
func IsJdiffKey(key string) bool {
	_, ok := jdiffDefaults[key]
	return ok || slices.Contains(jdiffKeys, key)
}

// Default returns the theme without any configuration.
func Default() *Theme { return Load(Config{}, nil) }

// Load builds the theme from cfg with the given features (see Features).
func Load(cfg Config, features []string) *Theme {
	t := &Theme{}
	str := func(key string) string {
		if v, ok := lookup(cfg, features, key); ok {
			return v
		}
		return deltaDefaults[key]
	}
	warn := func(name string, err error) {
		// Options that default to another one may report its error again.
		if w := fmt.Sprintf("%s: %v", name, err); !slices.Contains(t.Warnings, w) {
			t.Warnings = append(t.Warnings, w)
		}
	}
	parse := func(name, s, fallback string) lipgloss.Style {
		st, err := ParseStyle(s, cfg)
		if err != nil {
			warn(name, err)
			st, _ = ParseStyle(fallback, nil)
		}
		return st
	}
	delta := func(key string) lipgloss.Style {
		return parse("delta."+key, str(key), deltaDefaults[key])
	}
	// deltaOr returns a delta option that defaults to another option.
	deltaOr := func(key, fallbackKey string) lipgloss.Style {
		if v, ok := lookup(cfg, features, key); ok {
			return parse("delta."+key, v, str(fallbackKey))
		}
		return delta(fallbackKey)
	}
	jdiff := func(key, fallback string) lipgloss.Style {
		if v, ok := cfg["jdiff."+key]; ok {
			return parse("jdiff."+key, v, fallback)
		}
		return parse("jdiff."+key, fallback, "")
	}
	// jdiffOr returns a jdiff option that defaults to another style, also
	// when it is invalid.
	jdiffOr := func(key string, def lipgloss.Style) lipgloss.Style {
		v, ok := cfg["jdiff."+key]
		if !ok {
			return def
		}
		st, err := ParseStyle(v, cfg)
		if err != nil {
			warn("jdiff."+key, err)
			return def
		}
		return st
	}

	t.Minus, t.MinusEmph = delta("minus-style"), delta("minus-emph-style")
	t.Plus, t.PlusEmph = delta("plus-style"), delta("plus-emph-style")
	t.MinusNonEmph = deltaOr("minus-non-emph-style", "minus-style")
	t.PlusNonEmph = deltaOr("plus-non-emph-style", "plus-style")
	t.Zero = delta("zero-style")

	t.NumMinus = delta("line-numbers-minus-style")
	t.NumPlus = delta("line-numbers-plus-style")
	t.NumZero = delta("line-numbers-zero-style")
	t.NumLeft = delta("line-numbers-left-style")
	t.NumRight = delta("line-numbers-right-style")

	// Only the color of the decoration is used: jdiff draws no underlines.
	decoration := delta("file-decoration-style")
	t.Title = delta("file-style")
	t.Decoration = lipgloss.NewStyle().Foreground(decoration.GetForeground())
	t.Hint = delta("inline-hint-style")

	t.AddedLabel = str("file-added-label")
	t.RemovedLabel = str("file-removed-label")
	t.ModifiedLabel = str("file-modified-label")
	t.RenamedLabel = str("file-renamed-label")

	t.Anchor = jdiff("anchor-style", jdiffDefaults["anchor-style"])
	t.Pending = jdiff("pending-style", jdiffDefaults["pending-style"])
	t.Cursor = jdiff("cursor-style", jdiffDefaults["cursor-style"])
	t.FileBar = jdiff("file-bar-style", jdiffDefaults["file-bar-style"])
	t.CommitBar = jdiff("commit-bar-style", jdiffDefaults["commit-bar-style"])
	t.CommitHash = jdiff("commit-hash-style", jdiffDefaults["commit-hash-style"])
	t.Branch = jdiff("branch-style", jdiffDefaults["branch-style"])
	t.Filler = jdiffOr("filler-style", t.Decoration)
	t.Whitespace = jdiffOr("whitespace-style", t.Decoration)
	// Changed whitespace is drawn as · and →, by default like changed words.
	t.MinusIndent = jdiffOr("minus-indent-style", t.MinusEmph)
	t.PlusIndent = jdiffOr("plus-indent-style", t.PlusEmph)
	return t
}
