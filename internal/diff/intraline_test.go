package diff

import (
	"slices"
	"testing"
)

func TestIntraline(t *testing.T) {
	tests := []struct {
		name   string
		a, b   string
		ea, eb []Span
	}{
		{"word", "foo(bar)", "foo(baz)", []Span{{4, 7}}, []Span{{4, 7}}},
		{"indent only", "  x()", "    x()", []Span{{0, 2}}, []Span{{0, 4}}},
		{"indent added", "x()", "\tx()", nil, []Span{{0, 1}}},
		{"indent and word", "\tret a", "\t\tret b", []Span{{0, 1}, {5, 6}}, []Span{{0, 2}, {6, 7}}},
		{"inserted word", "a c", "a b c", nil, []Span{{2, 4}}},
		{"unrelated", "hello world", "x := 42", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ea, eb := Intraline(tt.a, tt.b)
			if !slices.Equal(ea, tt.ea) || !slices.Equal(eb, tt.eb) {
				t.Errorf("Intraline(%q, %q) = %v, %v, want %v, %v", tt.a, tt.b, ea, eb, tt.ea, tt.eb)
			}
		})
	}
}
