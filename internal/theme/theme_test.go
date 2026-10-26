package theme

import (
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestParseStyle(t *testing.T) {
	named := map[string]string{"my-red": "#ff0000"}
	tests := []struct {
		in     string
		fg, bg color.Color
		bold   bool
		ul     bool
	}{
		{"red", lipgloss.Color("1"), lipgloss.NoColor{}, false, false},
		{"red bold", lipgloss.Color("1"), lipgloss.NoColor{}, true, false},
		{"bold bright-green", lipgloss.Color("10"), lipgloss.NoColor{}, true, false},
		{"brightblue", lipgloss.Color("12"), lipgloss.NoColor{}, false, false},
		{"normal red", lipgloss.NoColor{}, lipgloss.Color("1"), false, false},
		{`"#434C5E" ul`, lipgloss.Color("#434c5e"), lipgloss.NoColor{}, false, true},
		{"232 bold 184", lipgloss.Color("232"), lipgloss.Color("184"), true, false},
		{"syntax auto", lipgloss.NoColor{}, lipgloss.NoColor{}, false, false},
		{"white box bold", lipgloss.Color("7"), lipgloss.NoColor{}, true, false},
		{"my-red", lipgloss.Color("#ff0000"), lipgloss.NoColor{}, false, false},
		{"", lipgloss.NoColor{}, lipgloss.NoColor{}, false, false},
	}
	for _, tt := range tests {
		st, err := ParseStyle(tt.in, named)
		if err != nil {
			t.Errorf("ParseStyle(%q): %v", tt.in, err)
			continue
		}
		if st.GetForeground() != tt.fg || st.GetBackground() != tt.bg ||
			st.GetBold() != tt.bold || st.GetUnderline() != tt.ul {
			t.Errorf("ParseStyle(%q) = fg %v bg %v bold %v ul %v", tt.in,
				st.GetForeground(), st.GetBackground(), st.GetBold(), st.GetUnderline())
		}
	}
}

func TestParseStyleErrors(t *testing.T) {
	for _, in := range []string{"rd", "red green blue", "#12345", "256", "darkgreen"} {
		if _, err := ParseStyle(in, nil); err == nil {
			t.Errorf("ParseStyle(%q) succeeded", in)
		}
	}
}

func TestParseGitConfig(t *testing.T) {
	out := []byte("delta.features\nObiDesert\x00delta.ObiDesert.minus-style\nred\x00delta.empty\n\x00delta.features\nlater\x00")
	cfg := parseGitConfig(out)
	want := Config{"delta.features": "later", "delta.ObiDesert.minus-style": "red", "delta.empty": ""}
	if len(cfg) != len(want) {
		t.Fatalf("cfg = %v, want %v", cfg, want)
	}
	for k, v := range want {
		if cfg[k] != v {
			t.Errorf("cfg[%q] = %q, want %q", k, cfg[k], v)
		}
	}
}

func TestFeatures(t *testing.T) {
	cfg := Config{
		"delta.features":   "d e",
		"delta.a.features": "c b",
		"delta.d.features": "f",
	}
	tests := []struct {
		env, flag string
		want      []string
	}{
		{"", "", []string{"e", "d", "f"}},
		{"", "a", []string{"a", "b", "c"}},
		{"x y", "", []string{"y", "x"}},
		{"x", "a", []string{"x"}},
		{"+x", "", []string{"x", "e", "d", "f"}},
		{"+x", "a", []string{"a", "b", "c", "x"}},
	}
	for _, tt := range tests {
		if got := Features(cfg, tt.env, tt.flag); !slices.Equal(got, tt.want) {
			t.Errorf("Features(env %q, flag %q) = %v, want %v", tt.env, tt.flag, got, tt.want)
		}
	}
}

func TestGitThemePriority(t *testing.T) {
	cfg := Config{
		"delta.features":                   "low high",
		"delta.low.minus-style":            "yellow",
		"delta.high.minus-style":           "blue",
		"delta.low.plus-style":             "cyan",
		"delta.inline-hint-style":          "magenta",
		"delta.high.inline-hint-style":     "white",
		"delta.high.file-modified-label":   "[*]",
		"delta.high.minus-non-emph-style":  "bright-black",
		"jdiff.anchor-style":               "cyan",
		"delta.high.file-decoration-style": `"#434C5E" ul`,
	}
	th := Resolve(FromGit(cfg, Features(cfg, "", "")))
	checks := []struct {
		name string
		got  lipgloss.Style
		fg   color.Color
	}{
		{"minus (rightmost feature wins)", th.Minus, lipgloss.Color("4")},
		{"plus (from lower feature)", th.Plus, lipgloss.Color("6")},
		{"hint (main section wins)", th.Hint, lipgloss.Color("5")},
		{"minus non-emph", th.MinusNonEmph, lipgloss.Color("8")},
		{"plus non-emph defaults to plus", th.PlusNonEmph, lipgloss.Color("6")},
		{"anchor from [jdiff]", th.Anchor, lipgloss.Color("6")},
		{"decoration color", th.Decoration, lipgloss.Color("#434c5e")},
		{"filler defaults to decoration", th.Filler, lipgloss.Color("#434c5e")},
	}
	for _, c := range checks {
		if c.got.GetForeground() != c.fg {
			t.Errorf("%s: fg %v, want %v", c.name, c.got.GetForeground(), c.fg)
		}
	}
	if th.ModifiedLabel != "[*]" || th.AddedLabel != "added:" {
		t.Errorf("labels = %q %q", th.ModifiedLabel, th.AddedLabel)
	}
	if th.Title.GetUnderline() {
		t.Errorf("title underlined by file-decoration-style ul; jdiff draws no underlines")
	}
	if len(th.Warnings) != 0 {
		t.Errorf("warnings: %v", th.Warnings)
	}
}

func TestResolveIndentAndWarnings(t *testing.T) {
	th := Default()
	if th.MinusIndent.GetForeground() != lipgloss.Color("9") || th.PlusIndent.GetForeground() != lipgloss.Color("10") {
		t.Errorf("default indent styles = %v %v, want the emph colors",
			th.MinusIndent.GetForeground(), th.PlusIndent.GetForeground())
	}

	cfg := Config{
		"delta.plus-emph-style":    "bold normal 22",
		"delta.minus-style":        "notacolor",
		"jdiff.minus-indent-style": "normal bright-black",
	}
	th = Resolve(FromGit(cfg, nil))
	if th.PlusIndent.GetBackground() != lipgloss.Color("22") {
		t.Errorf("plus indent should follow plus-emph-style, got %v", th.PlusIndent.GetBackground())
	}
	if th.MinusIndent.GetBackground() != lipgloss.Color("8") {
		t.Errorf("minus indent = %v, want jdiff override", th.MinusIndent.GetBackground())
	}
	if th.Minus.GetForeground() != lipgloss.Color("1") || len(th.Warnings) != 1 {
		t.Errorf("invalid minus-style: fg %v warnings %v, want default and one warning",
			th.Minus.GetForeground(), th.Warnings)
	}
}

func TestResolveInvalidJdiffStylesKeepDefaults(t *testing.T) {
	th := Resolve(FromGit(Config{
		"jdiff.filler-style":       "notacolor",
		"jdiff.whitespace-style":   "notacolor",
		"jdiff.minus-indent-style": "notacolor",
		"jdiff.plus-indent-style":  "notacolor",
	}, nil))
	def := Default()
	if th.Filler.GetForeground() != def.Decoration.GetForeground() ||
		th.Whitespace.GetForeground() != def.Decoration.GetForeground() {
		t.Errorf("filler %v whitespace %v, want the decoration color %v",
			th.Filler.GetForeground(), th.Whitespace.GetForeground(), def.Decoration.GetForeground())
	}
	if th.MinusIndent.GetForeground() != def.MinusEmph.GetForeground() ||
		th.PlusIndent.GetForeground() != def.PlusEmph.GetForeground() {
		t.Errorf("indent styles %v %v, want the emph colors",
			th.MinusIndent.GetForeground(), th.PlusIndent.GetForeground())
	}
	if len(th.Warnings) != 4 {
		t.Errorf("warnings %v, want one per invalid style", th.Warnings)
	}
}

func TestMovedStyles(t *testing.T) {
	th := Default()
	if th.MinusMoved.GetForeground() != lipgloss.Color("5") || th.PlusMoved.GetForeground() != lipgloss.Color("6") {
		t.Errorf("moved styles %v %v, want magenta and cyan", th.MinusMoved.GetForeground(), th.PlusMoved.GetForeground())
	}
	th = Resolve(FromGit(Config{"jdiff.plus-moved-style": "bright-cyan bold"}, nil))
	if th.PlusMoved.GetForeground() != lipgloss.Color("14") || !th.PlusMoved.GetBold() {
		t.Errorf("jdiff.plus-moved-style: %v, want bright cyan bold", th.PlusMoved.GetForeground())
	}
}

func TestOverflowStyle(t *testing.T) {
	if fg := Default().Overflow.GetForeground(); fg != lipgloss.Color("4") {
		t.Errorf("cut line markers %v, want blue", fg)
	}
}

func TestWrapMarkerStyle(t *testing.T) {
	if fg := Default().WrapMarker.GetForeground(); fg != lipgloss.Color("4") {
		t.Errorf("wrap marker %v, want blue", fg)
	}
	th := Resolve(FromGit(Config{"jdiff.wrap-marker-style": "yellow"}, nil))
	if fg := th.WrapMarker.GetForeground(); fg != lipgloss.Color("3") {
		t.Errorf("jdiff.wrap-marker-style: %v, want yellow", fg)
	}
}

func TestHiddenCharStyle(t *testing.T) {
	if bg := Default().HiddenChar.GetBackground(); bg != lipgloss.Color("1") {
		t.Errorf("hidden character background %v, want red", bg)
	}
}

func TestTrailingWhitespaceStyle(t *testing.T) {
	// As git marks whitespace errors: a red background.
	if bg := Default().TrailingWhitespace.GetBackground(); bg != lipgloss.Color("1") {
		t.Errorf("trailing whitespace background %v, want red", bg)
	}
}

func TestMoreStyle(t *testing.T) {
	// Unset: the style of unchanged whitespace, also one set by the theme.
	if th := Default(); th.More.GetForeground() != th.Whitespace.GetForeground() {
		t.Errorf("list arrows %v, want the whitespace color %v", th.More.GetForeground(), th.Whitespace.GetForeground())
	}
	th := Resolve(FromGit(Config{"jdiff.whitespace-style": "yellow"}, nil))
	if th.More.GetForeground() != lipgloss.Color("3") {
		t.Errorf("list arrows %v with whitespace-style yellow, want yellow", th.More.GetForeground())
	}
	th = Resolve(FromGit(Config{"jdiff.whitespace-style": "yellow", "jdiff.more-style": "bright-blue bold"}, nil))
	if th.More.GetForeground() != lipgloss.Color("12") || !th.More.GetBold() {
		t.Errorf("jdiff.more-style: %v, want bright blue bold", th.More.GetForeground())
	}
}

// The styles of --readme: headings and code follow the styles they fall back
// to, also those a theme sets, unless set themselves; links are bright blue
// and bold, as browsers draw them.
func TestDocStyles(t *testing.T) {
	th := Default()
	for _, c := range []struct {
		name      string
		got, want lipgloss.Style
	}{
		{"heading", th.DocHeading, th.Branch},
		{"code", th.DocCode, th.Title},
	} {
		if c.got.GetForeground() != c.want.GetForeground() || c.got.GetBold() != c.want.GetBold() {
			t.Errorf("%s: %v, want the style it falls back to, %v", c.name, c.got.GetForeground(), c.want.GetForeground())
		}
	}
	if th.DocLink.GetForeground() != lipgloss.Color("12") || !th.DocLink.GetBold() {
		t.Errorf("link: %v bold %v, want bright blue bold", th.DocLink.GetForeground(), th.DocLink.GetBold())
	}
	th = Resolve(FromGit(Config{"jdiff.branch-style": "red", "delta.file-style": "green", "jdiff.commit-hash-style": "cyan"}, nil))
	if th.DocHeading.GetForeground() != lipgloss.Color("1") || th.DocCode.GetForeground() != lipgloss.Color("2") ||
		th.DocLink.GetForeground() != lipgloss.Color("12") {
		t.Errorf("unset, with the others set: %v %v %v, want red green bright blue",
			th.DocHeading.GetForeground(), th.DocCode.GetForeground(), th.DocLink.GetForeground())
	}
	th = Resolve(FromGit(Config{"jdiff.doc-heading-style": "magenta bold", "jdiff.doc-code-style": "bright-black",
		"jdiff.doc-link-style": "blue ul"}, nil))
	if th.DocHeading.GetForeground() != lipgloss.Color("5") || !th.DocHeading.GetBold() ||
		th.DocCode.GetForeground() != lipgloss.Color("8") || th.DocLink.GetForeground() != lipgloss.Color("4") {
		t.Errorf("set: %v %v %v, want magenta bold, bright black, blue",
			th.DocHeading.GetForeground(), th.DocCode.GetForeground(), th.DocLink.GetForeground())
	}
}

func TestSearchStyle(t *testing.T) {
	if st := Default().Search; st.GetForeground() != lipgloss.Color("0") || st.GetBackground() != lipgloss.Color("3") {
		t.Errorf("search matches %v on %v, want black on yellow", st.GetForeground(), st.GetBackground())
	}
	th := Resolve(FromGit(Config{"jdiff.search-style": "reverse bright-magenta"}, nil))
	if !th.Search.GetReverse() || th.Search.GetForeground() != lipgloss.Color("13") {
		t.Errorf("jdiff.search-style: %v", th.Search.GetForeground())
	}
}

func TestParseStyleAttributes(t *testing.T) {
	for _, c := range []struct {
		word string
		set  func(lipgloss.Style) bool
	}{
		{"bold", lipgloss.Style.GetBold},
		{"dim", lipgloss.Style.GetFaint},
		{"italic", lipgloss.Style.GetItalic},
		{"ul", lipgloss.Style.GetUnderline},
		{"underline", lipgloss.Style.GetUnderline},
		{"blink", lipgloss.Style.GetBlink},
		{"reverse", lipgloss.Style.GetReverse},
		{"strike", lipgloss.Style.GetStrikethrough},
	} {
		st, err := ParseStyle(c.word, nil)
		if err != nil {
			t.Errorf("ParseStyle(%q): %v", c.word, err)
			continue
		}
		if !c.set(st) {
			t.Errorf("ParseStyle(%q) does not set it", c.word)
		}
		if c.set(lipgloss.NewStyle()) {
			t.Errorf("%s is set without being asked for", c.word)
		}
	}
	st, err := ParseStyle("dim italic blink strike red", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !st.GetFaint() || !st.GetItalic() || !st.GetBlink() || !st.GetStrikethrough() || st.GetForeground() != lipgloss.Color("1") {
		t.Errorf("several attributes: %+v", st)
	}
}

func TestReadGitConfig(t *testing.T) {
	t.Chdir(t.TempDir()) // outside any repository
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	global := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	if err := os.WriteFile(global, []byte("[user]\n\tname = x0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// git fails when no entry matches.
	if cfg := ReadGitConfig(); len(cfg) != 0 {
		t.Errorf("no matching entries: %v", cfg)
	}
	if err := os.WriteFile(global, []byte("[user]\n\tname = x0\n[delta]\n\tfeatures = F0\n[delta \"F0\"]\n\tminus-style = red\n[jdiff]\n\tx1 = x2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := ReadGitConfig()
	want := Config{"delta.features": "F0", "delta.F0.minus-style": "red", "jdiff.x1": "x2"}
	if len(cfg) != len(want) {
		t.Fatalf("cfg = %v, want %v", cfg, want)
	}
	for k, v := range want {
		if cfg[k] != v {
			t.Errorf("cfg[%q] = %q, want %q", k, cfg[k], v)
		}
	}
	// Without git: the defaults.
	t.Setenv("PATH", "")
	if cfg := ReadGitConfig(); len(cfg) != 0 {
		t.Errorf("without git: %v", cfg)
	}
}
