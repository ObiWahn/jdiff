package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
)

func TestHelpPopup(t *testing.T) {
	a := newTestApp()
	h := WithHelp(config.Default(), a)
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 60})
	pressRoot(h, "?")
	view := ansi.Strip(h.View())
	for _, want := range []string{"╭─ keys ─", "Commit list", "File list", "Diff view", "Flow view",
		"ctrl+space", "next, previous commit"} {
		if !strings.Contains(view, want) {
			t.Errorf("help lacks %q:\n%s", want, view)
		}
	}
	if lines := strings.Split(view, "\n"); len(lines) != 60 {
		t.Errorf("help view has %d lines, want the screen's 60", len(lines))
	}
	// Keys do not reach the page while the popup is open.
	pressRoot(h, "j", "enter")
	if a.cursor != 0 || a.open != nil {
		t.Errorf("keys reached the file list: cursor %d open %v", a.cursor, a.open)
	}
	pressRoot(h, "esc")
	if h.open || strings.Contains(ansi.Strip(h.View()), "╭─ keys") {
		t.Errorf("esc did not close the help")
	}
	pressRoot(h, "j")
	if a.cursor != 1 {
		t.Errorf("j after closing: cursor %d, want 1", a.cursor)
	}
}

func TestHelpScrolls(t *testing.T) {
	h := WithHelp(config.Default(), newTestApp())
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	pressRoot(h, "?")
	if !strings.Contains(ansi.Strip(h.View()), " 1/") {
		t.Fatalf("small help lacks its position:\n%s", ansi.Strip(h.View()))
	}
	pressRoot(h, "j", "j")
	if h.top != 2 || !strings.Contains(ansi.Strip(h.View()), " 3/") {
		t.Errorf("j j: top %d", h.top)
	}
	pressRoot(h, "G", "k", "k", "k")
	if h.top != 0 {
		t.Errorf("k past the top: top %d", h.top)
	}
}

func TestHelpInKeyBurst(t *testing.T) {
	a := newTestApp()
	h := WithHelp(config.Default(), a)
	h.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	// Runes read together arrive as one message.
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j?j")})
	if a.cursor != 1 || !h.open || h.top != 1 {
		t.Errorf("j?j: cursor %d, help open %v top %d; want 1, true, 1", a.cursor, h.open, h.top)
	}
	h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?j")})
	if h.open || a.cursor != 2 {
		t.Errorf("?j: help open %v cursor %d; want false, 2", h.open, a.cursor)
	}
}

func TestOverlayKeepsSides(t *testing.T) {
	got := overlay("abcdefgh\nijklmnop\nqrstuvwx", "XY\n", 2, 8)
	if want := "abcdefgh\nijkXYnop\nqrstuvwx"; got != want {
		t.Errorf("overlay = %q, want %q", got, want)
	}
}

func TestSizeGuardWhenTooSmall(t *testing.T) {
	a := newTestApp()
	g := WithSizeGuard(a.cfg, a)
	g.Update(tea.WindowSizeMsg{Width: 15, Height: 3})
	view := g.View()
	if v := ansi.Strip(view); !strings.Contains(v, "too small") || !strings.Contains(v, "15x3, need 20x4") {
		t.Errorf("15x3: no notice:\n%s", ansi.Strip(view))
	}
	if line := rawText(view, 15); line != "" || len(strings.Split(view, "\n")) > 3 {
		t.Errorf("15x3: the notice does not fit:\n%s", ansi.Strip(view))
	}
	// Keys are ignored, but ctrl+c quits.
	pressRoot(g, "j", "enter", "?")
	if a.cursor != 0 || a.open != nil {
		t.Errorf("keys reached the page: cursor %d, open %v", a.cursor, a.open)
	}
	if _, cmd := g.Update(keyMsg("ctrl+c")); cmd == nil {
		t.Error("ctrl+c does not quit while the terminal is too small")
	}
	// Large enough: the page again, sized while it was hidden.
	g.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if a.width != 80 || strings.Contains(ansi.Strip(g.View()), "too small") {
		t.Errorf("80x20: page width %d:\n%s", a.width, ansi.Strip(g.View()))
	}
	pressRoot(g, "j")
	if a.cursor != 1 {
		t.Errorf("80x20: j moved the cursor to %d, want 1", a.cursor)
	}
}

// TestNoViewOverflows draws every view in terminals of many sizes, small
// ones included, and checks that no line is wider and no screen taller than
// the terminal, which would make the terminal wrap or scroll.
func TestNoViewOverflows(t *testing.T) {
	long := func() tea.Model {
		a, b := numbered("a fairly long line of code number ", 40), numbered("a fairly long line of code number ", 40)
		b[4] = "changed and longer than before"
		return newTestModel(a, b)
	}
	big := func() tea.Model { // 10000 lines: line numbers of 5 digits
		b := numbered("x", 10000)
		b[5000] = "changed"
		return newTestModel(numbered("x", 10000), b)
	}
	views := []struct {
		name string
		make func() tea.Model
		keys []string
	}{
		{"diff", long, nil},
		{"diff normal", long, []string{"s"}},
		{"diff wrapped", long, []string{"r"}},
		{"diff normal wrapped", long, []string{"s", "r", "j", "j", "j", "j", "j"}},
		{"big diff", big, []string{"]"}},
		{"big diff normal", big, []string{"s", "]"}},
		{"file list", func() tea.Model { return newTestApp() }, nil},
		{"flow", func() tea.Model { return newFlowApp() }, []string{"f"}},
		{"flow wrapped", func() tea.Model { return newFlowApp() }, []string{"f", "r"}},
		{"commit list", func() tea.Model { l, _ := newTestLog(); return l }, nil},
		{"log flow", func() tea.Model { l, _ := newTestLog(); return l }, []string{"f", "enter"}},
		{"help", func() tea.Model { return newTestApp() }, []string{"?"}},
		{"marks", func() tea.Model { return newMarkModel() }, []string{"m", "a", "\\"}},
	}
	sizes := []int{1, 2, 3, 4, 5, 8, 12}
	widths := []int{1, 5, 10, 15, 19, 20, 21, 25, 30, 41, 60}
	for _, v := range views {
		m := v.make()
		cfg := config.Default()
		switch p := m.(type) {
		case *Model:
			cfg = p.cfg
		case *App:
			cfg = p.cfg
		case *Log:
			cfg = p.cfg
		}
		g := WithSizeGuard(cfg, WithHelp(cfg, WithMarks(cfg, m)))
		g.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
		pressRoot(g, v.keys...)
		// One view is resized through all sizes, as a terminal is.
		for _, w := range widths {
			for _, h := range sizes {
				g.Update(tea.WindowSizeMsg{Width: w, Height: h})
				view := g.View()
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
