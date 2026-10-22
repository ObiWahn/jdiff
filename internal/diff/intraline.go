package diff

import (
	"unicode"
	"unicode/utf8"
)

// maxLineDistance is the share of changed content above which two paired
// lines count as unrelated and get no emphasis (delta uses the same default).
const maxLineDistance = 0.6

type token struct {
	text       string
	start, end int
}

// Intraline returns the parts of a and b that differ, at word granularity.
// Leading whitespace is its own token, so an indentation change emphasizes
// just the indentation. Both results are nil when the lines are too different
// for emphasis to be useful.
func Intraline(a, b string) (ea, eb []Span) {
	ta, tb := tokenize(a), tokenize(b)
	ma := make([]bool, len(ta))
	mb := make([]bool, len(tb))
	texts := func(ts []token) []string {
		out := make([]string, len(ts))
		for i, t := range ts {
			out[i] = t.text
		}
		return out
	}
	for _, p := range myers(texts(ta), texts(tb)) {
		ma[p.a], mb[p.b] = true, true
	}

	changedA, totalA := contentBytes(ta, ma)
	changedB, totalB := contentBytes(tb, mb)
	if total := totalA + totalB; total > 0 &&
		float64(changedA+changedB)/float64(total) > maxLineDistance {
		return nil, nil
	}
	return spans(ta, ma), spans(tb, mb)
}

// contentBytes counts the changed and total bytes, leaving out indentation so
// that re-indented lines do not look unrelated.
func contentBytes(ts []token, matched []bool) (changed, total int) {
	for i, t := range ts {
		if i == 0 && isSpace(t.text) {
			continue
		}
		total += len(t.text)
		if !matched[i] {
			changed += len(t.text)
		}
	}
	return changed, total
}

func spans(ts []token, matched []bool) []Span {
	var out []Span
	for i, t := range ts {
		if matched[i] {
			continue
		}
		if n := len(out); n > 0 && out[n-1].End == t.start {
			out[n-1].End = t.end
		} else {
			out = append(out, Span{t.start, t.end})
		}
	}
	return out
}

// tokenize splits s into runs of word characters, runs of whitespace and
// single other characters.
func tokenize(s string) []token {
	var out []token
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		j := i + n
		if class := runeClass(r); class != 0 {
			for j < len(s) {
				r2, n2 := utf8.DecodeRuneInString(s[j:])
				if runeClass(r2) != class {
					break
				}
				j += n2
			}
		}
		out = append(out, token{s[i:j], i, j})
		i = j
	}
	return out
}

// runeClass returns 1 for word characters, 2 for whitespace and 0 for
// characters that form a token on their own.
func runeClass(r rune) int {
	switch {
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return 1
	case unicode.IsSpace(r):
		return 2
	}
	return 0
}

func isSpace(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return runeClass(r) == 2
}
