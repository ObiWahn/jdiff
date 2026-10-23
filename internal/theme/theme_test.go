package theme

import (
	"slices"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestParseStyle(t *testing.T) {
	cfg := Config{"delta.my-red": "#ff0000"}
	tests := []struct {
		in     string
		fg, bg lipgloss.TerminalColor
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
		st, err := ParseStyle(tt.in, cfg)
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

func TestLoadPriority(t *testing.T) {
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
	th := Load(cfg, Features(cfg, "", ""))
	checks := []struct {
		name string
		got  lipgloss.Style
		fg   lipgloss.TerminalColor
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

func TestLoadIndentAndWarnings(t *testing.T) {
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
	th = Load(cfg, nil)
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
