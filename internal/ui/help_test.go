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
