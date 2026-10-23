package theme

import (
	"path/filepath"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
)

// Kind is where a theme comes from.
type Kind int

const (
	KindBuiltin Kind = iota // jdiff's defaults
	KindGit                 // delta's and jdiff's git config sections
	KindFile                // a theme file
)

// Spec is a theme as its source defines it: the values as written, nil for
// options the source leaves unset. Resolve turns it into a Theme.
//
// The TOML keys are the option names; in the git config they are keys of
// the [delta] section, or of [jdiff] where the git tag says so.
type Spec struct {
	Name string `toml:"-"`
	Kind Kind   `toml:"-"`

	MinusStyle            *string `toml:"minus-style"`
	MinusEmphStyle        *string `toml:"minus-emph-style"`
	MinusNonEmphStyle     *string `toml:"minus-non-emph-style"` // unset: minus-style
	PlusStyle             *string `toml:"plus-style"`
	PlusEmphStyle         *string `toml:"plus-emph-style"`
	PlusNonEmphStyle      *string `toml:"plus-non-emph-style"` // unset: plus-style
	ZeroStyle             *string `toml:"zero-style"`
	LineNumbersMinusStyle *string `toml:"line-numbers-minus-style"`
	LineNumbersPlusStyle  *string `toml:"line-numbers-plus-style"`
	LineNumbersZeroStyle  *string `toml:"line-numbers-zero-style"`
	LineNumbersLeftStyle  *string `toml:"line-numbers-left-style"`
	LineNumbersRightStyle *string `toml:"line-numbers-right-style"`
	FileStyle             *string `toml:"file-style"`
	FileDecorationStyle   *string `toml:"file-decoration-style"` // only its color is used
	InlineHintStyle       *string `toml:"inline-hint-style"`
	FileAddedLabel        *string `toml:"file-added-label"`
	FileRemovedLabel      *string `toml:"file-removed-label"`
	FileModifiedLabel     *string `toml:"file-modified-label"`
	FileRenamedLabel      *string `toml:"file-renamed-label"`

	AnchorStyle      *string `toml:"anchor-style" git:"jdiff"`
	PendingStyle     *string `toml:"pending-style" git:"jdiff"`
	CursorStyle      *string `toml:"cursor-style" git:"jdiff"`
	FileBarStyle     *string `toml:"file-bar-style" git:"jdiff"`
	CommitBarStyle   *string `toml:"commit-bar-style" git:"jdiff"`
	CommitHashStyle  *string `toml:"commit-hash-style" git:"jdiff"`
	BranchStyle      *string `toml:"branch-style" git:"jdiff"`
	FillerStyle      *string `toml:"filler-style" git:"jdiff"`       // unset: file-decoration-style's color
	WhitespaceStyle  *string `toml:"whitespace-style" git:"jdiff"`   // unset: file-decoration-style's color
	MinusIndentStyle *string `toml:"minus-indent-style" git:"jdiff"` // unset: minus-emph-style
	PlusIndentStyle  *string `toml:"plus-indent-style" git:"jdiff"`  // unset: plus-emph-style

	// Colors are named colors that styles can use like color names.
	Colors map[string]string `toml:"colors"`
}

// options calls f with the key, git config section and field of each option
// of s.
func (s *Spec) options(f func(key, section string, field **string)) {
	v := reflect.ValueOf(s).Elem()
	for i := range v.NumField() {
		key := v.Type().Field(i).Tag.Get("toml")
		if key == "-" || key == "colors" {
			continue
		}
		section := v.Type().Field(i).Tag.Get("git")
		if section == "" {
			section = "delta"
		}
		f(key, section, v.Field(i).Addr().Interface().(**string))
	}
}

// isJdiffKey reports whether key is a [jdiff] key rather than a delta one.
func isJdiffKey(key string) bool {
	jdiff := false
	(&Spec{}).options(func(k, section string, _ **string) {
		jdiff = jdiff || (k == key && section == "jdiff")
	})
	return jdiff
}

// Builtin returns jdiff's default theme. Its colors are made for 16 color
// mode: unlike delta's defaults they are foreground colors only, since ANSI
// backgrounds are too loud for whole lines.
func Builtin() Spec {
	str := func(s string) *string { return &s }
	return Spec{
		Name: "default",
		Kind: KindBuiltin,

		MinusStyle:            str("red"),
		MinusEmphStyle:        str("bright-red bold"),
		PlusStyle:             str("green"),
		PlusEmphStyle:         str("bright-green bold"),
		ZeroStyle:             str("normal"),
		LineNumbersMinusStyle: str("red"),
		LineNumbersPlusStyle:  str("green"),
		LineNumbersZeroStyle:  str("normal"),
		LineNumbersLeftStyle:  str("blue"),
		LineNumbersRightStyle: str("blue"),
		FileStyle:             str("blue"),
		FileDecorationStyle:   str("blue ul"),
		InlineHintStyle:       str("blue"),
		FileAddedLabel:        str("added:"),
		FileRemovedLabel:      str("removed:"),
		FileModifiedLabel:     str(""),
		FileRenamedLabel:      str("renamed:"),

		AnchorStyle:  str("yellow bold"),
		PendingStyle: str("magenta bold"),
		CursorStyle:  str("reverse bold"),
		// Bars with a background, so file and commit borders stand out.
		FileBarStyle:    str("bold bright-white blue"),
		CommitBarStyle:  str("bold black yellow"),
		CommitHashStyle: str("yellow"),
		BranchStyle:     str("yellow bold"),
	}
}

// FromGit returns the theme that delta's and jdiff's git config sections in
// cfg define with the given features (see Features): an option in [delta]
// wins over the features in their order, jdiff's options are read from
// [jdiff]. As in delta, any key of [delta] can name a color.
func FromGit(cfg Config, features []string) Spec {
	s := Spec{Name: "delta", Kind: KindGit, Colors: map[string]string{}}
	s.options(func(key, section string, field **string) {
		if section == "jdiff" {
			if v, ok := cfg["jdiff."+key]; ok {
				*field = &v
			}
		} else if v, ok := lookup(cfg, features, key); ok {
			*field = &v
		}
	})
	for k, v := range cfg {
		if name, ok := strings.CutPrefix(k, "delta."); ok && !strings.Contains(name, ".") {
			s.Colors[name] = v
		}
	}
	return s
}

// FromFile reads the theme file path, a TOML file of options and a [colors]
// table, and names the theme after the file. It also returns the keys of the
// file that are no options.
func FromFile(path string) (Spec, []string, error) {
	s := Spec{Name: strings.TrimSuffix(filepath.Base(path), ".toml"), Kind: KindFile}
	md, err := toml.DecodeFile(path, &s)
	if err != nil {
		return Spec{}, nil, err
	}
	var unknown []string
	for _, k := range md.Undecoded() {
		unknown = append(unknown, k.String())
	}
	// Color words are matched lowercased, as git lowercases its keys.
	colors := make(map[string]string, len(s.Colors))
	for name, c := range s.Colors {
		colors[strings.ToLower(name)] = c
	}
	s.Colors = colors
	return s, unknown, nil
}
