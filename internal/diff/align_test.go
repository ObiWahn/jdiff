package diff

import (
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// checkRows verifies the invariants every alignment must hold: each line of
// both files appears exactly once, in order, and Equal rows really are equal.
func checkRows(t *testing.T, a, b []string, rows []Row) {
	t.Helper()
	nextL, nextR := 0, 0
	for i, r := range rows {
		if r.Left >= 0 {
			if r.Left != nextL {
				t.Fatalf("row %d: left line %d, want %d", i, r.Left, nextL)
			}
			nextL++
		}
		if r.Right >= 0 {
			if r.Right != nextR {
				t.Fatalf("row %d: right line %d, want %d", i, r.Right, nextR)
			}
			nextR++
		}
		switch r.Kind {
		case Equal:
			if r.Left < 0 || r.Right < 0 || a[r.Left] != b[r.Right] {
				t.Fatalf("row %d: Equal row %+v is not equal", i, r)
			}
		case Change:
			if r.Left < 0 || r.Right < 0 || a[r.Left] == b[r.Right] {
				t.Fatalf("row %d: bad Change row %+v", i, r)
			}
		case Delete:
			if r.Left < 0 || r.Right >= 0 {
				t.Fatalf("row %d: bad Delete row %+v", i, r)
			}
		case Insert:
			if r.Left >= 0 || r.Right < 0 {
				t.Fatalf("row %d: bad Insert row %+v", i, r)
			}
		}
	}
	if nextL != len(a) || nextR != len(b) {
		t.Fatalf("rows cover %d/%d left and %d/%d right lines", nextL, len(a), nextR, len(b))
	}
}

func kinds(rows []Row) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteByte("=~-+"[r.Kind])
	}
	return b.String()
}

func TestAlignIdentical(t *testing.T) {
	a := []string{"a", "b", "c"}
	rows := Align(a, a, nil, Options{})
	checkRows(t, a, a, rows)
	if got := kinds(rows); got != "===" {
		t.Errorf("kinds = %q, want %q", got, "===")
	}
}

func TestAlignEmpty(t *testing.T) {
	b := []string{"x", "y"}
	checkRows(t, nil, b, Align(nil, b, nil, Options{}))
	checkRows(t, b, nil, Align(b, nil, nil, Options{}))
	checkRows(t, nil, nil, Align(nil, nil, nil, Options{}))
}

// Wrapping code in an if/else must show only the new lines as inserted and
// the wrapped lines as re-indented changes paired with their originals.
func TestAlignWrapInIf(t *testing.T) {
	a := []string{
		"func f() {",
		"\ta()",
		"\tb()",
		"}",
	}
	b := []string{
		"func f() {",
		"\tif x {",
		"\t\ta()",
		"\t\tb()",
		"\t} else {",
		"\t\tc()",
		"\t}",
		"}",
	}
	rows := Align(a, b, nil, Options{})
	checkRows(t, a, b, rows)
	if got, want := kinds(rows), "=+~~+++="; got != want {
		t.Fatalf("kinds = %q, want %q", got, want)
	}
	for _, r := range rows[2:4] {
		if a[r.Left] != strings.TrimPrefix(b[r.Right], "\t") {
			t.Errorf("row %+v pairs %q with %q", r, a[r.Left], b[r.Right])
		}
	}
	if got := rows[2].RightEmph; !slices.Equal(got, []Span{{0, 2}}) {
		t.Errorf("RightEmph = %v, want indentation only", got)
	}

	// Compared character by character, only the added tab is emphasized.
	rows = Align(a, b, nil, Options{SplitIndent: true})
	if l, r := rows[2].LeftEmph, rows[2].RightEmph; l != nil || !slices.Equal(r, []Span{{1, 2}}) {
		t.Errorf("split indent: emph = %v, %v, want none and the second tab", l, r)
	}
}

func TestStats(t *testing.T) {
	a := []string{"keep", "\told", "gone"}
	b := []string{"keep", "\t\told", "new", "more"}
	added, removed := Stats(Align(a, b, nil, Options{}))
	if added != 3 || removed != 2 {
		t.Errorf("Stats = +%d -%d, want +3 -2", added, removed)
	}
}

func TestAlignAnchorForcesPairing(t *testing.T) {
	a := []string{"foo", "bar"}
	b := []string{"bar", "foo"}
	rows := Align(a, b, []Anchor{{Left: 1, Right: 0}}, Options{})
	checkRows(t, a, b, rows)
	if got, want := kinds(rows), "-=+"; got != want {
		t.Fatalf("kinds = %q, want %q", got, want)
	}
	if !rows[1].Anchored || rows[0].Anchored || rows[2].Anchored {
		t.Errorf("anchored flags wrong: %+v", rows)
	}
}

func TestAlignAnchorUnrelatedLines(t *testing.T) {
	a := []string{"keep", "old one", "x", "tail"}
	b := []string{"keep", "y", "z", "new one", "tail"}
	rows := Align(a, b, []Anchor{{Left: 1, Right: 3}}, Options{})
	checkRows(t, a, b, rows)
	var anchored []Row
	for _, r := range rows {
		if r.Anchored {
			anchored = append(anchored, r)
		}
	}
	if len(anchored) != 1 || anchored[0].Left != 1 || anchored[0].Right != 3 || anchored[0].Kind != Change {
		t.Fatalf("anchored rows = %+v", anchored)
	}
}

func TestAlignManyAnchors(t *testing.T) {
	a := []string{"a", "b", "c", "d", "e"}
	b := []string{"e", "d", "c", "b", "a"}
	anchors := []Anchor{{0, 1}, {2, 2}, {4, 3}}
	rows := Align(a, b, anchors, Options{})
	checkRows(t, a, b, rows)
	n := 0
	for _, r := range rows {
		if r.Anchored {
			if (Anchor{r.Left, r.Right}) != anchors[n] {
				t.Errorf("anchor %d: row %+v", n, r)
			}
			n++
		}
	}
	if n != len(anchors) {
		t.Errorf("%d anchored rows, want %d", n, len(anchors))
	}
}

func TestAlignRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	words := []string{"a", "b", "c", "  a", "\tb", ""}
	gen := func() []string {
		out := make([]string, rng.Intn(30))
		for i := range out {
			out[i] = words[rng.Intn(len(words))]
		}
		return out
	}
	for range 500 {
		a, b := gen(), gen()
		var anchors []Anchor
		for l, r := 0, 0; len(a) > 0 && len(b) > 0; {
			l += rng.Intn(len(a))/3 + 1
			r += rng.Intn(len(b))/3 + 1
			if l >= len(a) || r >= len(b) {
				break
			}
			anchors = append(anchors, Anchor{l, r})
		}
		checkRows(t, a, b, Align(a, b, anchors, Options{SplitIndent: rng.Intn(2) == 0}))
	}
}

func TestConflicts(t *testing.T) {
	anchors := []Anchor{{10, 12}, {20, 25}}
	tests := []struct {
		n    Anchor
		want []Anchor
	}{
		{Anchor{15, 18}, nil},
		{Anchor{30, 30}, nil},
		{Anchor{5, 5}, nil},
		{Anchor{15, 5}, []Anchor{{10, 12}}},
		{Anchor{5, 30}, []Anchor{{10, 12}, {20, 25}}},
		{Anchor{10, 13}, []Anchor{{10, 12}}},
		{Anchor{15, 25}, []Anchor{{20, 25}}},
	}
	for _, tt := range tests {
		if got := Conflicts(anchors, tt.n); !slices.Equal(got, tt.want) {
			t.Errorf("Conflicts(%v) = %v, want %v", tt.n, got, tt.want)
		}
	}
}

// lcsLen is a reference LCS length by dynamic programming.
func lcsLen(a, b []string) int {
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	return dp[0][0]
}

func TestMyersIsLCS(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for range 1000 {
		gen := func() []string {
			out := make([]string, rng.Intn(20))
			for i := range out {
				out[i] = string(rune('a' + rng.Intn(4)))
			}
			return out
		}
		a, b := gen(), gen()
		pairs := myers(a, b)
		for i, p := range pairs {
			if a[p.a] != b[p.b] || (i > 0 && (p.a <= pairs[i-1].a || p.b <= pairs[i-1].b)) {
				t.Fatalf("myers(%q, %q): invalid pairs %v", a, b, pairs)
			}
		}
		if got, want := len(pairs), lcsLen(a, b); got != want {
			t.Fatalf("myers(%q, %q): %d pairs, want %d", a, b, got, want)
		}
	}
}

func TestPatienceUsesUniqueLines(t *testing.T) {
	// Patience must anchor on the unique "x" rather than the frequent "}".
	a := []string{"}", "}", "x", "}"}
	b := []string{"x", "}", "}", "}"}
	pairs := patience(a, b)
	if !slices.Contains(pairs, pair{2, 0}) {
		t.Errorf("patience pairs %v do not match the unique line", pairs)
	}
}
