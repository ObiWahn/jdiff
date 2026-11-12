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

	"charm.land/lipgloss/v2"
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
	AddedFile                       lipgloss.Style // U I X: untracked, ignored, excluded files in the file list
	TrailingWhitespace              lipgloss.Style // whitespace at the end of lines when shown
	HiddenChar                      lipgloss.Style // ☠ for invisible and direction changing characters
	Overflow                        lipgloss.Style // « and » at the edges of cut lines
	More                            lipgloss.Style // ↑ and ↓ for items of lists out of view
	Search                          lipgloss.Style // text found by /
	MinusMoved, PlusMoved           lipgloss.Style // moved code: removed here, added there
	FileBar, CommitBar              lipgloss.Style // flow mode: start of a file, a commit
	CommitHash                      lipgloss.Style // commit hashes in the log
	Branch                          lipgloss.Style // branch above the commit list
	DocHeading, DocCode, DocLink    lipgloss.Style // --readme: headings, code, links
	Heading                         lipgloss.Style // headings of lists and boxes, the active pane's title
	InactiveHeading                 lipgloss.Style // the inactive pane's title, side by side
	Key                             lipgloss.Style // the keys in the help

	AddedLabel, RemovedLabel, ModifiedLabel, RenamedLabel string

	// Warnings lists settings that could not be used.
	Warnings []string
}

// Default returns the theme without any configuration.
func Default() *Theme { return Resolve(Builtin()) }

// Resolve builds the theme that sp defines. Options sp leaves unset or sets
// to an invalid value take their default; invalid values are reported in
// Warnings. It lists the options one per line, in the order of Theme, on
// purpose: a new option is one more line, the helpers are those of resolver.
func Resolve(sp Spec) *Theme {
	def := Builtin()
	t := &Theme{}
	r := &resolver{sp: sp, t: t}

	t.Minus = r.style("minus-style", sp.MinusStyle, def.MinusStyle)
	t.MinusEmph = r.style("minus-emph-style", sp.MinusEmphStyle, def.MinusEmphStyle)
	t.Plus = r.style("plus-style", sp.PlusStyle, def.PlusStyle)
	t.PlusEmph = r.style("plus-emph-style", sp.PlusEmphStyle, def.PlusEmphStyle)
	t.MinusNonEmph = r.styleOr("minus-non-emph-style", sp.MinusNonEmphStyle, "minus-style", sp.MinusStyle, def.MinusStyle)
	t.PlusNonEmph = r.styleOr("plus-non-emph-style", sp.PlusNonEmphStyle, "plus-style", sp.PlusStyle, def.PlusStyle)
	t.Zero = r.style("zero-style", sp.ZeroStyle, def.ZeroStyle)

	t.NumMinus = r.style("line-numbers-minus-style", sp.LineNumbersMinusStyle, def.LineNumbersMinusStyle)
	t.NumPlus = r.style("line-numbers-plus-style", sp.LineNumbersPlusStyle, def.LineNumbersPlusStyle)
	t.NumZero = r.style("line-numbers-zero-style", sp.LineNumbersZeroStyle, def.LineNumbersZeroStyle)
	t.NumLeft = r.style("line-numbers-left-style", sp.LineNumbersLeftStyle, def.LineNumbersLeftStyle)
	t.NumRight = r.style("line-numbers-right-style", sp.LineNumbersRightStyle, def.LineNumbersRightStyle)

	// Only the color of the decoration is used: jdiff draws no underlines.
	decoration := r.style("file-decoration-style", sp.FileDecorationStyle, def.FileDecorationStyle)
	t.Title = r.style("file-style", sp.FileStyle, def.FileStyle)
	t.Decoration = lipgloss.NewStyle().Foreground(decoration.GetForeground())
	t.Hint = r.style("inline-hint-style", sp.InlineHintStyle, def.InlineHintStyle)

	t.AddedLabel = val(sp.FileAddedLabel, def.FileAddedLabel)
	t.RemovedLabel = val(sp.FileRemovedLabel, def.FileRemovedLabel)
	t.ModifiedLabel = val(sp.FileModifiedLabel, def.FileModifiedLabel)
	t.RenamedLabel = val(sp.FileRenamedLabel, def.FileRenamedLabel)

	t.Anchor = r.style("anchor-style", sp.AnchorStyle, def.AnchorStyle)
	t.Pending = r.style("pending-style", sp.PendingStyle, def.PendingStyle)
	t.Cursor = r.style("cursor-style", sp.CursorStyle, def.CursorStyle)
	t.FileBar = r.style("file-bar-style", sp.FileBarStyle, def.FileBarStyle)
	t.CommitBar = r.style("commit-bar-style", sp.CommitBarStyle, def.CommitBarStyle)
	t.CommitHash = r.style("commit-hash-style", sp.CommitHashStyle, def.CommitHashStyle)
	t.Branch = r.style("branch-style", sp.BranchStyle, def.BranchStyle)
	t.MinusMoved = r.style("minus-moved-style", sp.MinusMovedStyle, def.MinusMovedStyle)
	t.PlusMoved = r.style("plus-moved-style", sp.PlusMovedStyle, def.PlusMovedStyle)
	t.Filler = r.styleOrStyle("filler-style", sp.FillerStyle, t.Decoration)
	t.Whitespace = r.styleOrStyle("whitespace-style", sp.WhitespaceStyle, t.Decoration)
	t.AddedFile = r.styleOrStyle("added-file-style", sp.AddedFileStyle, t.Filler)
	t.TrailingWhitespace = r.style("trailing-whitespace-style", sp.TrailingWhitespaceStyle, def.TrailingWhitespaceStyle)
	t.HiddenChar = r.style("hidden-char-style", sp.HiddenCharStyle, def.HiddenCharStyle)
	t.Overflow = r.style("overflow-style", sp.OverflowStyle, def.OverflowStyle)
	t.More = r.styleOrStyle("more-style", sp.MoreStyle, t.Whitespace)
	t.Search = r.style("search-style", sp.SearchStyle, def.SearchStyle)
	// Changed whitespace is drawn as · and →, by default like changed words.
	t.MinusIndent = r.styleOrStyle("minus-indent-style", sp.MinusIndentStyle, t.MinusEmph)
	t.PlusIndent = r.styleOrStyle("plus-indent-style", sp.PlusIndentStyle, t.PlusEmph)
	t.DocHeading = r.styleOrStyle("doc-heading-style", sp.DocHeadingStyle, t.Branch)
	t.DocCode = r.styleOrStyle("doc-code-style", sp.DocCodeStyle, t.Title)
	t.DocLink = r.style("doc-link-style", sp.DocLinkStyle, def.DocLinkStyle)
	t.Heading = r.style("heading-style", sp.HeadingStyle, def.HeadingStyle)
	t.InactiveHeading = r.style("inactive-heading-style", sp.InactiveHeadingStyle, def.InactiveHeadingStyle)
	t.Key = r.style("key-style", sp.KeyStyle, def.KeyStyle)
	return t
}

// resolver resolves the options of sp into t, reporting invalid values in
// t.Warnings: the helpers of Resolve, which lists the options one per line.
type resolver struct {
	sp Spec
	t  *Theme
}

// name returns how a warning names option key.
func (r *resolver) name(key string) string {
	switch {
	case r.sp.Kind == KindFile:
		return r.sp.Name + ".toml: " + key
	case isJdiffKey(key):
		return "jdiff." + key
	}
	return "delta." + key
}

// val returns v, or the default d when v is unset.
func val(v, d *string) string {
	if v != nil {
		return *v
	}
	return *d
}

// warn reports err of option key once.
func (r *resolver) warn(key string, err error) {
	// Options that default to another one may report its error again.
	if w := fmt.Sprintf("%s: %v", r.name(key), err); !slices.Contains(r.t.Warnings, w) {
		r.t.Warnings = append(r.t.Warnings, w)
	}
}

// parse returns the style s of option key, fallback when s is invalid.
func (r *resolver) parse(key, s, fallback string) lipgloss.Style {
	st, err := ParseStyle(s, r.sp.Colors)
	if err != nil {
		r.warn(key, err)
		st, _ = ParseStyle(fallback, nil)
	}
	return st
}

// style returns option key with value v and default d.
func (r *resolver) style(key string, v, d *string) lipgloss.Style {
	if v != nil {
		return r.parse(key, *v, *d)
	}
	return r.parse(key, *d, "")
}

// styleOr returns option key with value v, which defaults to option fbKey
// with value fv and default fd.
func (r *resolver) styleOr(key string, v *string, fbKey string, fv, fd *string) lipgloss.Style {
	if v != nil {
		return r.parse(key, *v, val(fv, fd))
	}
	return r.style(fbKey, fv, fd)
}

// styleOrStyle returns option key with value v, which defaults to the style
// d, also when it is invalid.
func (r *resolver) styleOrStyle(key string, v *string, d lipgloss.Style) lipgloss.Style {
	if v == nil {
		return d
	}
	st, err := ParseStyle(*v, r.sp.Colors)
	if err != nil {
		r.warn(key, err)
		return d
	}
	return st
}
