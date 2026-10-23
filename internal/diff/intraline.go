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
// just the indentation; with splitIndent each indentation character is a
// token, and a run of whitespace in place of another one in the line keeps
// their common start, so only the added or removed part is emphasized. Both
// results are nil when the lines are too different for emphasis to be
// useful.
func Intraline(a, b string, splitIndent bool) (ea, eb []Span) {
	ta, tb := tokenize(a, splitIndent), tokenize(b, splitIndent)
	ma := make([]bool, len(ta))
	mb := make([]bool, len(tb))
	texts := func(ts []token) []string {
		out := make([]string, len(ts))
		for i, t := range ts {
			out[i] = t.text
		}
		return out
	}
	pairs := myers(texts(ta), texts(tb))
	for _, p := range pairs {
		ma[p.a], mb[p.b] = true, true
	}
	// same holds the bytes at the start of an unmatched token that are the
	// same as on the other side.
	sa, sb := make([]int, len(ta)), make([]int, len(tb))
	if splitIndent {
		prev := pair{-1, -1}
		for _, p := range append(pairs, pair{len(ta), len(tb)}) {
			// One whitespace token on each side between two matches.
			if i, j := prev.a+1, prev.b+1; p.a == i+1 && p.b == j+1 && isSpace(ta[i].text) && isSpace(tb[j].text) {
				n := 0
				for n < len(ta[i].text) && n < len(tb[j].text) && ta[i].text[n] == tb[j].text[n] {
					n++
				}
				sa[i], sb[j] = n, n
			}
			prev = p
		}
	}

	changedA, totalA := contentBytes(ta, ma, sa)
	changedB, totalB := contentBytes(tb, mb, sb)
	if total := totalA + totalB; total > 0 &&
		float64(changedA+changedB)/float64(total) > maxLineDistance {
		return nil, nil
	}
	return spans(ta, ma, sa), spans(tb, mb, sb)
}

// contentBytes counts the changed and total bytes, leaving out indentation so
// that re-indented lines do not look unrelated. same is the unchanged start
// of each token, see Intraline.
func contentBytes(ts []token, matched []bool, same []int) (changed, total int) {
	indent := true
	for i, t := range ts {
		if indent = indent && isSpace(t.text); indent {
			continue
		}
		total += len(t.text)
		if !matched[i] {
			changed += len(t.text) - same[i]
		}
	}
	return changed, total
}

// spans returns the parts of the tokens ts that changed: the unmatched ones
// past their unchanged start same.
func spans(ts []token, matched []bool, same []int) []Span {
	var out []Span
	for i, t := range ts {
		start := t.start + same[i]
		if matched[i] || start == t.end {
			continue
		}
		if n := len(out); n > 0 && out[n-1].End == start {
			out[n-1].End = t.end
		} else {
			out = append(out, Span{start, t.end})
		}
	}
	return out
}

// tokenize splits s into runs of word characters, runs of whitespace and
// single other characters. With splitIndent, each character of the leading
// whitespace is a token of its own.
func tokenize(s string, splitIndent bool) []token {
	var out []token
	indent := splitIndent
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		j := i + n
		class := runeClass(r)
		if indent = indent && class == classSpace; indent {
			out = append(out, token{s[i:j], i, j})
			i = j
			continue
		}
		if class != classSingle {
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

// class is the kind of character a token is made of.
type class int

const (
	classSingle class = iota // characters that form a token on their own
	classWord
	classSpace
)

func runeClass(r rune) class {
	switch {
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return classWord
	case unicode.IsSpace(r):
		return classSpace
	}
	return classSingle
}

func isSpace(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return runeClass(r) == classSpace
}
