package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/keys"
)

func TestHelpPopup(t *testing.T) {
	d := newTestDiff()
	h := WithHelp(config.Default(), d)
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 80}) // room for all bindings
	pressRoot(h, "?")
	view := ansi.Strip(h.frame())
	for _, want := range []string{"╭─ keys ─", "Everywhere", "Commit list and file list", "Diff and flow view",
		"ctrl+space", "next, previous commit"} {
		if !strings.Contains(view, want) {
			t.Errorf("help lacks %q:\n%s", want, view)
		}
	}
	if lines := strings.Split(view, "\n"); len(lines) != 80 {
		t.Errorf("help view has %d lines, want the screen's 80", len(lines))
	}
	// Keys do not reach the page while the popup is open.
	pressRoot(h, "j", "enter")
	if d.cursor != 0 || d.open != nil {
		t.Errorf("keys reached the file list: cursor %d open %v", d.cursor, d.open)
	}
	pressRoot(h, "esc")
	if h.open || strings.Contains(ansi.Strip(h.frame()), "╭─ keys") {
		t.Errorf("esc did not close the help")
	}
	pressRoot(h, "j")
	if d.cursor != 1 {
		t.Errorf("j after closing: cursor %d, want 1", d.cursor)
	}
}

func TestHelpScrolls(t *testing.T) {
	h := WithHelp(config.Default(), newTestDiff())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	pressRoot(h, "?")
	if !strings.Contains(ansi.Strip(h.frame()), " 1/") {
		t.Fatalf("small help lacks its position:\n%s", ansi.Strip(h.frame()))
	}
	// From the top, j moves by two: line 1 would only stand behind an arrow.
	pressRoot(h, "j")
	if h.top != 2 || !strings.Contains(ansi.Strip(h.frame()), " 3/") {
		t.Errorf("j: top %d", h.top)
	}
	pressRoot(h, "j")
	if h.top != 3 {
		t.Errorf("j j: top %d", h.top)
	}
	pressRoot(h, "end", "k", "k", "k")
	if h.top != 0 {
		t.Errorf("k past the top: top %d", h.top)
	}
}

func TestSizeGuardWhenTooSmall(t *testing.T) {
	d := newTestDiff()
	g := WithSizeGuard(d.cfg, d)
	g.Update(tea.WindowSizeMsg{Width: 15, Height: 3})
	view := g.frame()
	if v := ansi.Strip(view); !strings.Contains(v, "too small") || !strings.Contains(v, "15x3, need 20x4") {
		t.Errorf("15x3: no notice:\n%s", ansi.Strip(view))
	}
	if line := rawText(view, 15); line != "" || len(strings.Split(view, "\n")) > 3 {
		t.Errorf("15x3: the notice does not fit:\n%s", ansi.Strip(view))
	}
	// Keys are ignored, but ctrl+c quits.
	pressRoot(g, "j", "enter", "?")
	if d.cursor != 0 || d.open != nil {
		t.Errorf("keys reached the page: cursor %d, open %v", d.cursor, d.open)
	}
	if _, cmd := g.Update(keyMsg("ctrl+c")); cmd == nil {
		t.Error("ctrl+c does not quit while the terminal is too small")
	}
	// Large enough: the page again, sized while it was hidden.
	g.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if d.width != 80 || strings.Contains(ansi.Strip(g.frame()), "too small") {
		t.Errorf("80x20: page width %d:\n%s", d.width, ansi.Strip(g.frame()))
	}
	pressRoot(g, "j")
	if d.cursor != 1 {
		t.Errorf("80x20: j moved the cursor to %d, want 1", d.cursor)
	}
}

// TestNoViewOverflows draws every view in terminals of many sizes, small
// ones included, and checks that no line is wider and no screen taller than
// the terminal, which would make the terminal wrap or scroll.
func TestNoViewOverflows(t *testing.T) {
	long := func() tea.Model {
		a, b := numbered("a fairly long line of code number ", 40), numbered("a fairly long line of code number ", 40)
		b[4] = "changed and longer than before"
		return newTestFileDiff(a, b)
	}
	big := func() tea.Model { // 10000 lines: line numbers of 5 digits
		b := numbered("x", 10000)
		b[5000] = "changed"
		return newTestFileDiff(numbered("x", 10000), b)
	}
	views := []struct {
		name string
		make func() tea.Model
		keys []string
	}{
		{"diff", long, nil},
		{"diff normal", long, []string{"o", "s", "esc"}},
		{"diff wrapped", long, []string{"o", "w", "esc"}},
		{"diff normal wrapped", long, []string{"o", "s", "w", "esc", "j", "j", "j", "j", "j"}},
		{"big diff", big, []string{"]"}},
		{"big diff normal", big, []string{"o", "s", "esc", "]"}},
		{"file list", func() tea.Model { return newTestDiff() }, nil},
		{"flow", func() tea.Model { return newFlowDiff() }, []string{"f"}},
		{"flow wrapped", func() tea.Model { return newFlowDiff() }, []string{"f", "o", "w", "esc"}},
		{"commit list", func() tea.Model { l, _ := newTestLog(); return l }, nil},
		{"log flow", func() tea.Model { l, _ := newTestLog(); return l }, []string{"f", "enter"}},
		{"help", func() tea.Model { return newTestDiff() }, []string{"?"}},
		{"marks", func() tea.Model { return newMarkFileDiff() }, []string{"m", "a", "\\"}},
		{"settings", func() tea.Model { return newTestDiff() }, []string{"o"}},
	}
	sizes := []int{1, 2, 3, 4, 5, 8, 12}
	widths := []int{1, 5, 10, 15, 19, 20, 21, 25, 30, 41, 60}
	for _, v := range views {
		m := v.make()
		cfg := config.Default()
		switch p := m.(type) {
		case *FileDiff:
			cfg = p.cfg
		case *Diff:
			cfg = p.cfg
		case *Log:
			cfg = p.cfg
		}
		g := WithSizeGuard(cfg, WithHelp(cfg, WithSettings(cfg, WithMarks(cfg, m))))
		g.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
		pressRoot(g, v.keys...)
		// One view is resized through all sizes, as a terminal is.
		for _, w := range widths {
			for _, h := range sizes {
				g.Update(tea.WindowSizeMsg{Width: w, Height: h})
				view := g.frame()
				if n := len(strings.Split(view, "\n")); n > h {
					t.Errorf("%s at %dx%d: %d lines", v.name, w, h, n)
				}
				if line := rawText(view, w); line != "" {
					t.Errorf("%s at %dx%d: line too wide: %q", v.name, w, h, line)
				}
			}
		}
	}
}

func TestHelpArrowsSweep(t *testing.T) {
	t.Parallel() // a sweep: its own config and views, nothing shared
	// At 80 columns long lines wrap, so rows are more than lines.
	probe := WithHelp(config.Default(), newTestDiff())
	probe.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressRoot(probe, "?")
	n := len(probe.rows)
	if n <= len(probe.lines) {
		t.Fatalf("80 columns: %d rows of %d lines, none wrapped", n, len(probe.lines))
	}
	// The box has height-4 rows. Each of these sizes must stay: one and two
	// rows (no arrows), three and four (the smallest with arrows), some,
	// n-2, n-1 (an arrow hides two rows), n (all fit exactly) and n+1.
	for _, rows := range []int{1, 2, 3, 4, 12, n - 2, n - 1, n, n + 1} {
		t.Run(fmt.Sprint("rows ", rows), func(t *testing.T) {
			t.Parallel()
			height := rows + 4
			h := WithHelp(config.Default(), newTestDiff())
			h.Update(tea.WindowSizeMsg{Width: 80, Height: height})
			pressRoot(h, "?")
			rows = min(rows, n) // room for more: the box stays at the lines' height
			if h.boxHeight()-2 != rows {
				t.Fatalf("height %d: %d rows in the box, want %d", height, h.boxHeight()-2, rows)
			}
			keys := append(slices.Repeat([]string{"j"}, n+2), slices.Repeat([]string{"k"}, n+2)...)
			for step, k := range append([]string{""}, keys...) {
				if k != "" {
					pressRoot(h, k)
				}
				got := popupRows(h.frame())
				fail := func(why string) {
					t.Fatalf("height %d, step %d, top %d: %s\n%q", height, step, h.top, why, got)
				}
				if len(got) != rows {
					fail("not a full popup")
				}
				if h.top == 1 && rows >= 3 && n > rows {
					fail("starts at line 1, behind an arrow")
				}
				var up, down int
				hasUp := strings.HasPrefix(got[0], "  ↑ ")
				hasDown := strings.HasPrefix(got[len(got)-1], "  ↓ ")
				if hasUp {
					fmt.Sscanf(got[0], "  ↑ %d more", &up)
				}
				if hasDown {
					fmt.Sscanf(got[len(got)-1], "  ↓ %d more", &down)
				}
				shown := rows
				if hasUp {
					shown--
				}
				if hasDown {
					shown--
				}
				switch {
				case (n <= rows || rows < 3) && (hasUp || hasDown):
					fail("arrows though all lines fit, or in fewer than three rows")
				case rows < 3:
				case hasUp && (up != h.top || up < 2):
					fail("the arrow above is wrong")
				case !hasUp && h.top != 0:
					fail("lines hidden above without an arrow")
				case hasDown && (down != n-h.top-shown || down < 2):
					fail("the arrow below is wrong")
				case !hasDown && n > rows && h.top+shown != n:
					fail("lines hidden below without an arrow")
				}
			}
		})
	}
}

// TestHelpWrapsSweep checks at each width that the help shows all its words,
// wrapping what does not fit instead of cutting it. The widths go from the
// least with a column left for the words, keyW+9, up to one past all fit; 80
// and 100 must stay, where lines were cut before. The box is as wide as its
// widest row, with border and padding.
func TestHelpWrapsSweep(t *testing.T) {
	t.Parallel()
	probe := WithHelp(config.Default(), newTestDiff())
	probe.Update(tea.WindowSizeMsg{Width: 1000, Height: 20})
	pressRoot(probe, "?")
	fits, keyW := probe.boxWidth, 0
	for _, b := range probe.lines {
		keyW = max(keyW, ansi.StringWidth(shownKeys(b.key)))
	}
	noSpace := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for width := keyW + 9; width <= fits+1; width++ {
		t.Run(fmt.Sprint("width ", width), func(t *testing.T) {
			t.Parallel()
			h := WithHelp(config.Default(), newTestDiff())
			h.Update(tea.WindowSizeMsg{Width: width, Height: 20})
			pressRoot(h, "?")
			h.Update(tea.WindowSizeMsg{Width: width, Height: len(h.rows) + 4}) // all rows fit
			got := popupRows(h.frame())
			if len(got) != len(h.rows) {
				t.Fatalf("%d rows shown of %d", len(got), len(h.rows))
			}
			if wrapped := len(h.rows) > len(h.lines); wrapped != (width < fits) {
				t.Errorf("%d rows of %d lines in a box %d wide, all fit from %d", len(h.rows), len(h.lines), h.boxWidth, fits)
			}
			rowW := 0
			for _, r := range got {
				rowW = max(rowW, ansi.StringWidth(r))
			}
			if h.boxWidth != rowW+4 {
				t.Errorf("box %d wide for rows up to %d wide", h.boxWidth, rowW)
			}
			text := noSpace(strings.Join(got, " "))
			if strings.Contains(text, "…") {
				t.Errorf("a line is cut:\n%s", strings.Join(got, "\n"))
			}
			for _, b := range h.lines {
				if want := noSpace(shownKeys(b.key) + b.what); !strings.Contains(text, want) {
					t.Errorf("lacks %q %q:\n%s", shownKeys(b.key), b.what, strings.Join(got, "\n"))
				}
			}
		})
	}
}

// The help names the key of every setting.
func TestHelpListsSettings(t *testing.T) {
	for _, o := range settingOptions(keys.Builtin()) {
		if !slices.ContainsFunc(helpLines(keys.Builtin()), func(b binding) bool { return b.key == o.key }) {
			t.Errorf("the help has no line for %s: %s", o.key, o.what)
		}
	}
}
