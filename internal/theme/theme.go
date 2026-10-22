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

	AddedLabel, RemovedLabel, ModifiedLabel string

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
}

var jdiffDefaults = map[string]string{
	"anchor-style":  "yellow bold",
	"pending-style": "magenta bold",
	"cursor-style":  "reverse bold",
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
	parse := func(name, s, fallback string) lipgloss.Style {
		st, err := ParseStyle(s, cfg)
		if err != nil {
			// Options that default to another one may report its error again.
			if w := fmt.Sprintf("%s: %v", name, err); !slices.Contains(t.Warnings, w) {
				t.Warnings = append(t.Warnings, w)
			}
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

	decoration := delta("file-decoration-style")
	t.Title = delta("file-style").Underline(decoration.GetUnderline())
	t.Decoration = lipgloss.NewStyle().Foreground(decoration.GetForeground())
	t.Hint = delta("inline-hint-style")

	t.AddedLabel = str("file-added-label")
	t.RemovedLabel = str("file-removed-label")
	t.ModifiedLabel = str("file-modified-label")

	t.Anchor = jdiff("anchor-style", jdiffDefaults["anchor-style"])
	t.Pending = jdiff("pending-style", jdiffDefaults["pending-style"])
	t.Cursor = jdiff("cursor-style", jdiffDefaults["cursor-style"])
	t.Filler, t.Whitespace = t.Decoration, t.Decoration
	if _, ok := cfg["jdiff.filler-style"]; ok {
		t.Filler = jdiff("filler-style", "")
	}
	if _, ok := cfg["jdiff.whitespace-style"]; ok {
		t.Whitespace = jdiff("whitespace-style", "")
	}
	// Changed whitespace is drawn as · and →, by default like changed words.
	t.MinusIndent, t.PlusIndent = t.MinusEmph, t.PlusEmph
	if _, ok := cfg["jdiff.minus-indent-style"]; ok {
		t.MinusIndent = jdiff("minus-indent-style", "")
	}
	if _, ok := cfg["jdiff.plus-indent-style"]; ok {
		t.PlusIndent = jdiff("plus-indent-style", "")
	}
	return t
}
