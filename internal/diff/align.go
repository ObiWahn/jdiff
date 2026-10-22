// Package diff aligns two files line by line for side-by-side display.
//
// Lines are matched on their content with surrounding whitespace removed, so
// re-indented code still lines up with its original. User-chosen anchors force
// specific line pairs to line up and split the matching into independent
// sections.
package diff

import "strings"

// Kind classifies a display row.
type Kind int

const (
	Equal  Kind = iota // both lines identical
	Change             // both lines present but different
	Delete             // only the left line is present
	Insert             // only the right line is present
)

// Anchor forces line Left of the old file to line up with line Right of the
// new file. Both are 0-based.
type Anchor struct{ Left, Right int }

// Span is a byte range [Start, End) within a line.
type Span struct{ Start, End int }

// Row is one display row of the side-by-side view.
type Row struct {
	Kind        Kind
	Left, Right int  // 0-based line indices, -1 when absent
	Anchored    bool // the row is a user anchor
	// Emphasized parts of Change rows, as computed by Intraline.
	LeftEmph, RightEmph []Span
}

// Options control how changed lines are compared.
type Options struct {
	// SplitIndent compares indentation character by character, so only its
	// added or removed part is emphasized.
	SplitIndent bool
}

// Align computes the display rows for old lines a and new lines b. anchors
// must be sorted by Left and must not cross (see Conflicts).
func Align(a, b []string, anchors []Anchor, opt Options) []Row {
	al := aligner{a: a, b: b, ka: keys(a), kb: keys(b), opt: opt}
	pa, pb := 0, 0
	for _, an := range anchors {
		al.section(pa, an.Left, pb, an.Right)
		al.pair(an.Left, an.Right, true)
		pa, pb = an.Left+1, an.Right+1
	}
	al.section(pa, len(a), pb, len(b))
	return al.rows
}

// aligner accumulates the rows of one alignment.
type aligner struct {
	a, b, ka, kb []string
	opt          Options
	rows         []Row
}

// Conflicts returns the anchors that cannot coexist with n, because they
// share a line with it or would cross it.
func Conflicts(anchors []Anchor, n Anchor) []Anchor {
	var out []Anchor
	for _, an := range anchors {
		dl, dr := an.Left-n.Left, an.Right-n.Right
		if dl == 0 || dr == 0 || (dl < 0) != (dr < 0) {
			out = append(out, an)
		}
	}
	return out
}

// Stats counts the added and removed lines of an alignment. A changed line
// counts as both.
func Stats(rows []Row) (added, removed int) {
	for _, r := range rows {
		switch r.Kind {
		case Change:
			added, removed = added+1, removed+1
		case Insert:
			added++
		case Delete:
			removed++
		}
	}
	return added, removed
}

func keys(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimSpace(l)
	}
	return out
}

// section appends the rows for a[a0:a1] against b[b0:b1].
func (al *aligner) section(a0, a1, b0, b1 int) {
	i, j := a0, b0
	for _, p := range patience(al.ka[a0:a1], al.kb[b0:b1]) {
		al.gap(i, a0+p.a, j, b0+p.b)
		al.pair(a0+p.a, b0+p.b, false)
		i, j = a0+p.a+1, b0+p.b+1
	}
	al.gap(i, a1, j, b1)
}

// gap appends rows for unmatched lines. Lines are paired by position as
// changes; the remainder becomes plain deletions or insertions.
func (al *aligner) gap(i0, i1, j0, j1 int) {
	for i0 < i1 && j0 < j1 {
		al.pair(i0, j0, false)
		i0, j0 = i0+1, j0+1
	}
	for ; i0 < i1; i0++ {
		al.rows = append(al.rows, Row{Kind: Delete, Left: i0, Right: -1})
	}
	for ; j0 < j1; j0++ {
		al.rows = append(al.rows, Row{Kind: Insert, Left: -1, Right: j0})
	}
}

func (al *aligner) pair(l, r int, anchored bool) {
	row := Row{Kind: Equal, Left: l, Right: r, Anchored: anchored}
	if al.a[l] != al.b[r] {
		row.Kind = Change
		row.LeftEmph, row.RightEmph = Intraline(al.a[l], al.b[r], al.opt.SplitIndent)
	}
	al.rows = append(al.rows, row)
}
