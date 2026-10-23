// Package theme builds jdiff's styles. A theme comes from a source, as a
// Spec: jdiff's defaults, delta's git config settings or a theme file;
// Resolve turns it into the styles, with the defaults for what it leaves
// unset.
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
	TrailingWhitespace              lipgloss.Style // whitespace at the end of lines when shown
	HiddenChar                      lipgloss.Style // ☠ for invisible and direction changing characters
	Overflow                        lipgloss.Style // « and » at the edges of cut lines
	MinusMoved, PlusMoved           lipgloss.Style // moved code: removed here, added there
	FileBar, CommitBar              lipgloss.Style // flow mode: start of a file, a commit
	CommitHash                      lipgloss.Style // commit hashes in the log
	Branch                          lipgloss.Style // branch above the commit list

	AddedLabel, RemovedLabel, ModifiedLabel, RenamedLabel string

	// Warnings lists settings that could not be used.
	Warnings []string
}

// Default returns the theme without any configuration.
func Default() *Theme { return Resolve(Builtin()) }

// Resolve builds the theme that sp defines. Options sp leaves unset or sets
// to an invalid value take their default; invalid values are reported in
// Warnings.
func Resolve(sp Spec) *Theme {
	def := Builtin()
	t := &Theme{}
	name := func(key string) string {
		switch {
		case sp.Kind == KindFile:
			return sp.Name + ".toml: " + key
		case isJdiffKey(key):
			return "jdiff." + key
		}
		return "delta." + key
	}
	val := func(v, d *string) string {
		if v != nil {
			return *v
		}
		return *d
	}
	warn := func(key string, err error) {
		// Options that default to another one may report its error again.
		if w := fmt.Sprintf("%s: %v", name(key), err); !slices.Contains(t.Warnings, w) {
			t.Warnings = append(t.Warnings, w)
		}
	}
	parse := func(key, s, fallback string) lipgloss.Style {
		st, err := ParseStyle(s, sp.Colors)
		if err != nil {
			warn(key, err)
			st, _ = ParseStyle(fallback, nil)
		}
		return st
	}
	// style returns option key with value v and default d.
	style := func(key string, v, d *string) lipgloss.Style {
		if v != nil {
			return parse(key, *v, *d)
		}
		return parse(key, *d, "")
	}
	// styleOr returns option key with value v, which defaults to option
	// fbKey with value fv and default fd.
	styleOr := func(key string, v *string, fbKey string, fv, fd *string) lipgloss.Style {
		if v != nil {
			return parse(key, *v, val(fv, fd))
		}
		return style(fbKey, fv, fd)
	}
	// styleOrStyle returns option key with value v, which defaults to the
	// style d, also when it is invalid.
	styleOrStyle := func(key string, v *string, d lipgloss.Style) lipgloss.Style {
		if v == nil {
			return d
		}
		st, err := ParseStyle(*v, sp.Colors)
		if err != nil {
			warn(key, err)
			return d
		}
		return st
	}

	t.Minus = style("minus-style", sp.MinusStyle, def.MinusStyle)
	t.MinusEmph = style("minus-emph-style", sp.MinusEmphStyle, def.MinusEmphStyle)
	t.Plus = style("plus-style", sp.PlusStyle, def.PlusStyle)
	t.PlusEmph = style("plus-emph-style", sp.PlusEmphStyle, def.PlusEmphStyle)
	t.MinusNonEmph = styleOr("minus-non-emph-style", sp.MinusNonEmphStyle, "minus-style", sp.MinusStyle, def.MinusStyle)
	t.PlusNonEmph = styleOr("plus-non-emph-style", sp.PlusNonEmphStyle, "plus-style", sp.PlusStyle, def.PlusStyle)
	t.Zero = style("zero-style", sp.ZeroStyle, def.ZeroStyle)

	t.NumMinus = style("line-numbers-minus-style", sp.LineNumbersMinusStyle, def.LineNumbersMinusStyle)
	t.NumPlus = style("line-numbers-plus-style", sp.LineNumbersPlusStyle, def.LineNumbersPlusStyle)
	t.NumZero = style("line-numbers-zero-style", sp.LineNumbersZeroStyle, def.LineNumbersZeroStyle)
	t.NumLeft = style("line-numbers-left-style", sp.LineNumbersLeftStyle, def.LineNumbersLeftStyle)
	t.NumRight = style("line-numbers-right-style", sp.LineNumbersRightStyle, def.LineNumbersRightStyle)

	// Only the color of the decoration is used: jdiff draws no underlines.
	decoration := style("file-decoration-style", sp.FileDecorationStyle, def.FileDecorationStyle)
	t.Title = style("file-style", sp.FileStyle, def.FileStyle)
	t.Decoration = lipgloss.NewStyle().Foreground(decoration.GetForeground())
	t.Hint = style("inline-hint-style", sp.InlineHintStyle, def.InlineHintStyle)

	t.AddedLabel = val(sp.FileAddedLabel, def.FileAddedLabel)
	t.RemovedLabel = val(sp.FileRemovedLabel, def.FileRemovedLabel)
	t.ModifiedLabel = val(sp.FileModifiedLabel, def.FileModifiedLabel)
	t.RenamedLabel = val(sp.FileRenamedLabel, def.FileRenamedLabel)

	t.Anchor = style("anchor-style", sp.AnchorStyle, def.AnchorStyle)
	t.Pending = style("pending-style", sp.PendingStyle, def.PendingStyle)
	t.Cursor = style("cursor-style", sp.CursorStyle, def.CursorStyle)
	t.FileBar = style("file-bar-style", sp.FileBarStyle, def.FileBarStyle)
	t.CommitBar = style("commit-bar-style", sp.CommitBarStyle, def.CommitBarStyle)
	t.CommitHash = style("commit-hash-style", sp.CommitHashStyle, def.CommitHashStyle)
	t.Branch = style("branch-style", sp.BranchStyle, def.BranchStyle)
	t.MinusMoved = style("minus-moved-style", sp.MinusMovedStyle, def.MinusMovedStyle)
	t.PlusMoved = style("plus-moved-style", sp.PlusMovedStyle, def.PlusMovedStyle)
	t.Filler = styleOrStyle("filler-style", sp.FillerStyle, t.Decoration)
	t.Whitespace = styleOrStyle("whitespace-style", sp.WhitespaceStyle, t.Decoration)
	t.TrailingWhitespace = style("trailing-whitespace-style", sp.TrailingWhitespaceStyle, def.TrailingWhitespaceStyle)
	t.HiddenChar = style("hidden-char-style", sp.HiddenCharStyle, def.HiddenCharStyle)
	t.Overflow = style("overflow-style", sp.OverflowStyle, def.OverflowStyle)
	// Changed whitespace is drawn as · and →, by default like changed words.
	t.MinusIndent = styleOrStyle("minus-indent-style", sp.MinusIndentStyle, t.MinusEmph)
	t.PlusIndent = styleOrStyle("plus-indent-style", sp.PlusIndentStyle, t.PlusEmph)
	return t
}
