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
// token, and runs of whitespace next to the same unchanged words keep what
// they have in common, so only the added or removed part is emphasized. Both
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
	// keep holds the bytes at the start and the end of an unmatched token
	// that are the same as on the other side.
	ka, kb := make([]kept, len(ta)), make([]kept, len(tb))
	if splitIndent {
		prev := pair{-1, -1}
		for _, p := range append(pairs, pair{len(ta), len(tb)}) {
			keepWhitespace(ta[prev.a+1:p.a], tb[prev.b+1:p.b], ka[prev.a+1:p.a], kb[prev.b+1:p.b])
			prev = p
		}
	}

	changedA, totalA := contentBytes(ta, ma, ka)
	changedB, totalB := contentBytes(tb, mb, kb)
	if total := totalA + totalB; total > 0 &&
		float64(changedA+changedB)/float64(total) > maxLineDistance {
		return nil, nil
	}
	return spans(ta, ma, ka), spans(tb, mb, kb)
}

// kept is the unchanged start and end of a token, in bytes.
type kept struct{ start, end int }

// keepWhitespace finds what the unmatched tokens a and b between the same
// two matches have in common in whitespace, into ka and kb: a whitespace
// run that starts both keeps their common start, one that ends both their
// common end, as words inserted into padding take its place. A run that is
// all of one side pairs with the run of the other that has more in common.
func keepWhitespace(a, b []token, ka, kb []kept) {
	if len(a) == 0 || len(b) == 0 {
		return
	}
	common := func(x, y string, fromEnd bool) int {
		n := 0
		for n < len(x) && n < len(y) {
			i, j := n, n
			if fromEnd {
				i, j = len(x)-1-n, len(y)-1-n
			}
			if x[i] != y[j] {
				break
			}
			n++
		}
		return n
	}
	first, last := 0, 0
	if isSpace(a[0].text) && isSpace(b[0].text) {
		first = common(a[0].text, b[0].text, false)
	}
	la, lb := len(a)-1, len(b)-1
	if isSpace(a[la].text) && isSpace(b[lb].text) {
		last = common(a[la].text, b[lb].text, true)
	}
	switch {
	case la > 0 && lb > 0: // distinct first and last runs: both
		ka[0].start, kb[0].start = first, first
		ka[la].end, kb[lb].end = last, last
	case last > first:
		ka[la].end, kb[lb].end = last, last
	default:
		ka[0].start, kb[0].start = first, first
	}
}

// contentBytes counts the changed and total bytes, leaving out indentation so
// that re-indented lines do not look unrelated. keep is the unchanged start
// and end of each token, see keepWhitespace.
func contentBytes(ts []token, matched []bool, keep []kept) (changed, total int) {
	indent := true
	for i, t := range ts {
		if indent = indent && isSpace(t.text); indent {
			continue
		}
		total += len(t.text)
		if !matched[i] {
			changed += max(len(t.text)-keep[i].start-keep[i].end, 0)
		}
	}
	return changed, total
}

// spans returns the parts of the tokens ts that changed: the unmatched ones
// without their unchanged start and end keep.
func spans(ts []token, matched []bool, keep []kept) []Span {
	var out []Span
	for i, t := range ts {
		start, end := t.start+keep[i].start, t.end-keep[i].end
		if matched[i] || start >= end {
			continue
		}
		if n := len(out); n > 0 && out[n-1].End == start {
			out[n-1].End = end
		} else {
			out = append(out, Span{start, end})
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
