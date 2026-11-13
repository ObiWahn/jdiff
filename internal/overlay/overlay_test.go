package overlay

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Center draws the popup in the middle of the base; the base shows around it.
func TestCenter(t *testing.T) {
	base := strings.Repeat(strings.Repeat(".", 10)+"\n", 4) + strings.Repeat(".", 10)
	got := Center(base, "AB\nCD", 10, 5)
	want := "..........\n....AB....\n....CD....\n..........\n.........."
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A popup larger than the base is clipped to it.
func TestCenterClips(t *testing.T) {
	got := Center("...\n...", "ABCDE\nFGHIJ\nKLMNO", 3, 2)
	for _, l := range strings.Split(got, "\n") {
		if len([]rune(l)) > 3 {
			t.Errorf("line %q wider than the base", l)
		}
	}
	if n := strings.Count(got, "\n") + 1; n != 2 {
		t.Errorf("%d lines, want 2", n)
	}
}

// slowAt is At as it was before it saved measures: every cut line and row
// measured again. At must draw exactly what it draws.
func slowAt(base, popup string, x, y, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := strings.Split(base, "\n")
	out := make([]string, height)
	copy(out, rows)
	placed := make([]bool, height) // place cuts these rows itself
	lines := strings.Split(popup, "\n")
	pw := 0
	for _, l := range lines {
		pw = max(pw, ansi.StringWidth(l))
	}
	for i, l := range lines {
		r := y + i
		if r < 0 || r >= height {
			continue
		}
		l += strings.Repeat(" ", pw-ansi.StringWidth(l))
		px := x
		if px < 0 {
			l, px = ansi.TruncateLeft(l, -px, ""), 0
		}
		if px >= width {
			continue
		}
		out[r], placed[r] = slowPlace(out[r], ansi.Truncate(l, width-px, ""), px, width), true
	}
	for i := range out {
		if !placed[i] {
			out[i] = ansi.Truncate(out[i], width, "")
		}
	}
	return strings.Join(out, "\n")
}

// place returns row, at most width wide, with line drawn over it from column
// x on; where line cuts a wide character of row in half, a space takes its
// place.
func slowPlace(row, line string, x, width int) string {
	rowW := ansi.StringWidth(row)
	left := ansi.Truncate(row, x, "")
	left += strings.Repeat(" ", x-ansi.StringWidth(left))
	end := x + ansi.StringWidth(line)
	right := ""
	if want := min(rowW, width) - end; want > 0 {
		right = ansi.Cut(row, end, width)
		if ansi.StringWidth(right) > want { // ansi keeps a wide character cut in half
			right = " " + ansi.Cut(row, end+1, width)
		}
		right = strings.Repeat(" ", max(want-ansi.StringWidth(right), 0)) + right
	}
	return left + line + right
}

// parts are what the random views are made of: narrow, wide and combined
// characters, emoji, colors and resets.
var parts = []string{"a", "b", " ", "界", "😀", "e\u0301", "\x1b[31m", "\x1b[1;44m", "\x1b[0m"}

// randomBlock returns rows lines of up to maxW parts each.
func randomBlock(r *rand.Rand, rows, maxW int) string {
	ls := make([]string, rows)
	for i := range ls {
		var b strings.Builder
		for range r.IntN(maxW + 1) {
			b.WriteString(parts[r.IntN(len(parts))])
		}
		ls[i] = b.String()
	}
	return strings.Join(ls, "\n")
}

// At draws byte for byte what slowAt draws, over random views: wide
// characters at every edge, popups off the view at each side, short popup
// lines, base rows wider than the view, fewer or more rows than its height.
func TestAtSameAsSlow(t *testing.T) {
	n := 30000
	if testing.Short() {
		n = 3000
	}
	r := rand.New(rand.NewPCG(1, 2))
	for i := range n {
		w, h := r.IntN(14)+1, r.IntN(8)+1
		base := randomBlock(r, r.IntN(h+3), w+5)
		pop := randomBlock(r, r.IntN(h+2)+1, w+3)
		x, y := r.IntN(w+6)-4, r.IntN(h+4)-3
		if got, want := At(base, pop, x, y, w, h), slowAt(base, pop, x, y, w, h); got != want {
			t.Fatalf("case %d: At(%q, %q, %d, %d, %d, %d)\n got %q\nwant %q", i, base, pop, x, y, w, h, got, want)
		}
	}
}

// coloredView returns a view of width × height cells, every ten of them
// colored.
func coloredView(width, height int) string {
	rows := make([]string, height)
	for r := range rows {
		var b strings.Builder
		for c := 0; c < width; c += 10 {
			fmt.Fprintf(&b, "\x1b[3%dm%-9s\x1b[0m ", c%7+1, fmt.Sprintf("r%02dc%03d", r, c))
		}
		rows[r] = b.String()
	}
	return strings.Join(rows, "\n")
}

// A 100x40 popup over a colored 200x60 screen, drawn on every key.
func BenchmarkAt(b *testing.B) {
	base, pop := coloredView(200, 60), coloredView(100, 40)
	for b.Loop() {
		At(base, pop, 50, 10, 200, 60)
	}
}

func BenchmarkSlowAt(b *testing.B) {
	base, pop := coloredView(200, 60), coloredView(100, 40)
	for b.Loop() {
		slowAt(base, pop, 50, 10, 200, 60)
	}
}
