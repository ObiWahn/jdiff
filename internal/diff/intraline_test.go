package diff

import (
	"slices"
	"testing"
)

func TestIntraline(t *testing.T) {
	tests := []struct {
		name   string
		a, b   string
		split  bool
		ea, eb []Span
	}{
		{"word", "foo(bar)", "foo(baz)", false, []Span{{4, 7}}, []Span{{4, 7}}},
		{"indent only", "  x()", "    x()", false, []Span{{0, 2}}, []Span{{0, 4}}},
		{"indent added", "x()", "\tx()", false, nil, []Span{{0, 1}}},
		{"indent and word", "\tret a", "\t\tret b", false, []Span{{0, 1}, {5, 6}}, []Span{{0, 2}, {6, 7}}},
		{"inserted word", "a c", "a b c", false, nil, []Span{{2, 4}}},
		{"unrelated", "hello world", "x := 42", false, nil, nil},
		{"split: indent only", "  x()", "    x()", true, nil, []Span{{2, 4}}},
		{"split: indent removed", "\t\tx", "\tx", true, []Span{{1, 2}}, nil},
		{"split: indent and word", "\tret a", "\t\tret b", true, []Span{{5, 6}}, []Span{{1, 2}, {6, 7}}},
		{"split: tabs to spaces", "\tx", "    x", true, []Span{{0, 1}}, []Span{{0, 4}}},
		{"split: inner spaces stay words", "a  b", "a b", true, []Span{{1, 3}}, []Span{{1, 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ea, eb := Intraline(tt.a, tt.b, tt.split)
			if !slices.Equal(ea, tt.ea) || !slices.Equal(eb, tt.eb) {
				t.Errorf("Intraline(%q, %q) = %v, %v, want %v, %v", tt.a, tt.b, ea, eb, tt.ea, tt.eb)
			}
		})
	}
}
