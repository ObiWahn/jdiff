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

// Align computes the display rows for old lines a and new lines b. anchors
// must be sorted by Left and must not cross (see Conflicts).
func Align(a, b []string, anchors []Anchor) []Row {
	ka, kb := keys(a), keys(b)
	var rows []Row
	pa, pb := 0, 0
	for _, an := range anchors {
		rows = alignSection(rows, a, b, ka, kb, pa, an.Left, pb, an.Right)
		rows = append(rows, pairRow(a, b, an.Left, an.Right, true))
		pa, pb = an.Left+1, an.Right+1
	}
	return alignSection(rows, a, b, ka, kb, pa, len(a), pb, len(b))
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

// alignSection appends the rows for a[a0:a1] against b[b0:b1].
func alignSection(rows []Row, a, b, ka, kb []string, a0, a1, b0, b1 int) []Row {
	i, j := a0, b0
	for _, p := range patience(ka[a0:a1], kb[b0:b1]) {
		rows = gapRows(rows, a, b, i, a0+p.a, j, b0+p.b)
		rows = append(rows, pairRow(a, b, a0+p.a, b0+p.b, false))
		i, j = a0+p.a+1, b0+p.b+1
	}
	return gapRows(rows, a, b, i, a1, j, b1)
}

// gapRows appends rows for unmatched lines. Lines are paired by position as
// changes; the remainder becomes plain deletions or insertions.
func gapRows(rows []Row, a, b []string, i0, i1, j0, j1 int) []Row {
	for i0 < i1 && j0 < j1 {
		rows = append(rows, pairRow(a, b, i0, j0, false))
		i0, j0 = i0+1, j0+1
	}
	for ; i0 < i1; i0++ {
		rows = append(rows, Row{Kind: Delete, Left: i0, Right: -1})
	}
	for ; j0 < j1; j0++ {
		rows = append(rows, Row{Kind: Insert, Left: -1, Right: j0})
	}
	return rows
}

func pairRow(a, b []string, l, r int, anchored bool) Row {
	row := Row{Kind: Equal, Left: l, Right: r, Anchored: anchored}
	if a[l] != b[r] {
		row.Kind = Change
		row.LeftEmph, row.RightEmph = Intraline(a[l], b[r])
	}
	return row
}
