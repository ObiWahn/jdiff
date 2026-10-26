package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/source"
)

// sp returns the span from start to end.
func sp(start, end int) diff.Span { return diff.Span{Start: start, End: end} }

func TestMatcherSpans(t *testing.T) {
	for _, tt := range []struct {
		q    config.Query
		text string
		want []diff.Span
	}{
		// Smart case: lower case finds any case, upper case only its own.
		{config.Query{Text: "foo"}, "Foo foo FOO", []diff.Span{sp(0, 3), sp(4, 7), sp(8, 11)}},
		{config.Query{Text: "Foo"}, "Foo foo FOO", []diff.Span{sp(0, 3)}},
		{config.Query{Text: "aa"}, "aaaaa", []diff.Span{sp(0, 2), sp(2, 4)}}, // no overlaps
		{config.Query{Text: "äpfel"}, "Äpfel äpfel", []diff.Span{sp(0, 6), sp(7, 13)}},
		{config.Query{Text: "x"}, "abc", nil},
		// Regular expressions, smart case too; empty matches are left out.
		{config.Query{Text: "f.o", Regex: true}, "FAO fbo", []diff.Span{sp(0, 3), sp(4, 7)}},
		{config.Query{Text: "F.o", Regex: true}, "FAO Fbo", []diff.Span{sp(4, 7)}},
		{config.Query{Text: "x*", Regex: true}, "axb", []diff.Span{sp(1, 2)}},
		{config.Query{Text: `\d+`, Regex: true}, "a12 b3", []diff.Span{sp(1, 3), sp(5, 6)}},
	} {
		m, err := compile(tt.q)
		if err != nil {
			t.Fatalf("%+v: %v", tt.q, err)
		}
		if got := m.spans(tt.text); !slices.Equal(got, tt.want) {
			t.Errorf("%+v in %q: %v, want %v", tt.q, tt.text, got, tt.want)
		}
	}
	if _, err := compile(config.Query{Text: "([", Regex: true}); err == nil {
		t.Error("a broken regular expression compiles")
	}
	if matcherOf(config.Query{Text: "([", Regex: true}) != nil || matcherOf(config.Query{}) != nil {
		t.Error("a matcher for a broken or empty query")
	}
}

// newSearchModel returns the diff view of 40 numbered lines, in which line
// 5 changed and line 30 is only in the old file, with the unchanged lines
// folded.
func newSearchModel() *Model {
	a, b := numbered("x", 40), numbered("x", 40)
	b[5] = "x5 changed"
	b = slices.Delete(b, 30, 31)
	m := newTestModel(a, b)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m
}

func TestModelFindText(t *testing.T) {
	m := newSearchModel()
	m.cfg.Settings.SearchAll = true // also in the unchanged lines
	press(m, ".")
	if m.msg != "nothing searched yet: / searches" {
		t.Errorf(". before a search: %q", m.msg)
	}
	m.cfg.Search.Add(config.Query{Text: "nothing like it"})
	press(m, ".")
	if !strings.HasPrefix(m.msg, "no match for nothing like it") {
		t.Errorf("no match: %q", m.msg)
	}
	// x3 is in both files, folded: . unfolds it, on the left line first.
	m.cfg.Search.Add(config.Query{Text: "x3"})
	at := func() (side, int) { return m.active, m.lineAt(m.active, m.cursor) }
	want := []struct {
		s    side
		line int
	}{{left, 3}, {right, 3}, {left, 30}, {left, 31}, {right, 30}, {left, 32}, {right, 31}}
	for i, w := range want {
		press(m, ".")
		if s, line := at(); s != w.s || line != w.line {
			t.Fatalf(". %d: on %v line %d, want %v line %d (%s)", i+1, s, line, w.s, w.line, m.msg)
		}
	}
	if !strings.Contains(m.msg, "match 7/") {
		t.Errorf("msg %q, want match 7 of all", m.msg)
	}
	// On to the last match, x39 on the right; there it stays: no wrapping.
	for range 20 {
		press(m, ".")
	}
	if s, line := at(); s != right || line != 38 || m.msg != "/x3: no match further down" {
		t.Errorf("past the last: on %v line %d, msg %q; want to stay on the right x39", s, line, m.msg)
	}
	press(m, ",")
	if s, line := at(); s != left || line != 39 {
		t.Errorf(", from the last: on %v line %d, want the left x39", s, line)
	}
	// Back to the first, and there it stays.
	for range 20 {
		press(m, ",")
	}
	if s, line := at(); s != left || line != 3 || m.msg != "/x3: no match further up" {
		t.Errorf("before the first: on %v line %d, msg %q; want to stay on the left x3", s, line, m.msg)
	}
}

func TestFindChangedOnly(t *testing.T) {
	m := newSearchModel() // x5 changed, x30 removed
	m.cfg.Search.Add(config.Query{Text: "x3"})
	// By default only in changed lines: the removed x30, then no further.
	press(m, ".")
	if m.active != left || m.lineAt(left, m.cursor) != 30 {
		t.Errorf(". in changed lines: on %v line %d, want the removed x30", m.active, m.lineAt(m.active, m.cursor))
	}
	press(m, ".")
	if m.msg != "/x3: no match further down" {
		t.Errorf(". past it: %q", m.msg)
	}
	m.cfg.Search.Add(config.Query{Text: "x2"})
	press(m, ",")
	if m.msg != "no match for x2 in changed lines" {
		t.Errorf("only in unchanged lines: %q", m.msg)
	}
	// Only changed lines are highlighted.
	ansiColors(t)
	m.cfg.Settings.CollapseUnchanged = false
	m.realign()
	m.cfg.Search.Add(config.Query{Text: "x"})
	match := m.cfg.Theme.Search.Render("x")
	match = match[:strings.Index(match, "x")]
	if !strings.Contains(renderedLine(t, m, right, 5), match+"x") || strings.Contains(renderedLine(t, m, right, 4), match+"x") {
		t.Errorf("highlights: changed %q, unchanged %q", renderedLine(t, m, right, 5), renderedLine(t, m, right, 4))
	}
	m.cfg.Settings.SearchAll = true
	if !strings.Contains(renderedLine(t, m, right, 4), match+"x") {
		t.Errorf("all lines: unchanged %q not highlighted", renderedLine(t, m, right, 4))
	}
}

func TestSearchBarToggles(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	m := newSearchModel()
	m.cfg = cfg
	s := WithSearch(cfg, m)
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	state := func() string {
		b, _ := os.ReadFile(filepath.Join(dir, "state.toml"))
		return string(b)
	}
	title := func() string {
		for _, r := range strings.Split(ansi.Strip(s.View()), "\n") {
			if i := strings.Index(r, "╭─ "); i >= 0 {
				r, _, _ = strings.Cut(r[i+len("╭─ "):], "╮") // the view goes on right of the popup
				return strings.TrimRight(strings.TrimLeft(r, " "), "─ ")
			}
		}
		return ""
	}
	pressRoot(s, "/")
	if got := title(); got != "search changed lines  enter find · tab all · shift+tab regex · esc cancel" {
		t.Errorf("title %q", got)
	}
	// tab: all lines, shift+tab: regex; each saved at once.
	pressRoot(s, "tab")
	if !cfg.Settings.SearchAll || !strings.Contains(state(), "search_all = true") ||
		title() != "search all lines  enter find · tab changed · shift+tab regex · esc cancel" {
		t.Errorf("shift+tab: all %v, title %q, state\n%s", cfg.Settings.SearchAll, title(), state())
	}
	pressRoot(s, "shift+tab")
	if !s.regex || !cfg.Settings.SearchRegex || !strings.Contains(state(), "search_regex = true") ||
		title() != "regex all lines  enter find · tab changed · shift+tab text · esc cancel" {
		t.Errorf("tab: regex %v, title %q, state\n%s", s.regex, title(), state())
	}
	pressRoot(s, "shift+tab", "tab")
	if s.regex || cfg.Settings.SearchRegex || cfg.Settings.SearchAll ||
		!strings.Contains(state(), "search_regex = false") || !strings.Contains(state(), "search_all = false") {
		t.Errorf("back: regex %v %v, all %v, state\n%s", s.regex, cfg.Settings.SearchRegex, cfg.Settings.SearchAll, state())
	}
	// backspace deletes, and on nothing typed closes the bar.
	pressRoot(s, "ab", "backspace")
	if s.input == nil || s.input.text != "a" {
		t.Fatalf("backspace: %v", s.input)
	}
	pressRoot(s, "backspace", "backspace")
	if s.input != nil || cfg.Search.Text != "" {
		t.Errorf("backspace on nothing typed: open %v, search %+v", s.input != nil, cfg.Search.Query)
	}
	// The bar opens in the mode saved; a query of the history brings its
	// own, which enter saves.
	pressRoot(s, "/", "shift+tab", "x[0-9]", "enter")
	pressRoot(s, "/", "shift+tab", "plain", "enter")
	if cfg.Settings.SearchRegex || !strings.Contains(state(), "search_regex = false") {
		t.Fatalf("plain search: regex %v", cfg.Settings.SearchRegex)
	}
	pressRoot(s, "/", "up", "up", "enter")
	if cfg.Search.Query != (config.Query{Text: "x[0-9]", Regex: true}) || !cfg.Settings.SearchRegex ||
		!strings.Contains(state(), "search_regex = true") {
		t.Errorf("from the history: search %+v, regex %v, state\n%s", cfg.Search.Query, cfg.Settings.SearchRegex, state())
	}
	pressRoot(s, "/")
	if !s.regex {
		t.Error("the bar does not open in the mode saved")
	}
}

func TestFindScrollsToMatch(t *testing.T) {
	long := strings.Repeat("a", 150) + "needle" + strings.Repeat("b", 50)
	m := newTestModel([]string{"x", long}, []string{"y", long})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	m.cfg.Settings.Wrap, m.cfg.Settings.SearchAll = false, true // the long line is unchanged
	m.cfg.Search.Add(config.Query{Text: "needle"})
	press(m, ".")
	w := m.contentWidth(m.width, !m.cfg.Settings.SideBySide)
	if m.xoff == 0 || m.xoff > 150 || m.xoff+w-1 < 156 || m.xoff%8 != 0 {
		t.Errorf("xoff %d in a pane of %d columns, want the needle at 150 to 156 in view", m.xoff, w)
	}
	// In view already: no scrolling.
	xoff := m.xoff
	press(m, ".")
	if m.xoff != xoff {
		t.Errorf("the next match, in view: xoff %d, want %d", m.xoff, xoff)
	}
	// Wrapped lines do not scroll.
	m.xoff, m.cfg.Settings.Wrap = 0, true
	press(m, ".")
	if m.xoff != 0 {
		t.Errorf("with wrapping: xoff %d", m.xoff)
	}
}

func TestFindHighlights(t *testing.T) {
	ansiColors(t)
	m := newSearchModel()
	m.cfg.Settings.CollapseUnchanged = false
	m.realign()
	match := m.cfg.Theme.Search.Render("changed")
	match = match[:strings.Index(match, "changed")] // its escape sequences
	row := func() string { return renderedLine(t, m, right, 5) }
	if strings.Contains(row(), match+"changed") {
		t.Fatal("highlighted before a search")
	}
	// The cache of the line's highlight follows the query.
	m.cfg.Search.Add(config.Query{Text: "changed"})
	if !strings.Contains(row(), match+"changed") {
		t.Errorf("not highlighted: %q", row())
	}
	m.cfg.Search.Add(config.Query{Text: "x5"})
	if r := row(); strings.Contains(r, match+"changed") || !strings.Contains(r, match+"x5") {
		t.Errorf("after another search: %q", r)
	}
}

func TestFlowFindText(t *testing.T) {
	l, _ := newTestLog() // c3: a.go 1→2, b.go x→y; c2: a.go 0→1; c1: c.go p→q r
	pressLog(l, "f")
	f := l.flow
	for p := range f.parts {
		if f.unloaded(p) {
			f.loadPart(p)
		}
	}
	where := func() string {
		p, file, line := f.position()
		if file < 0 || line < 0 {
			return fmt.Sprintf("part %d file %d", p, file)
		}
		d := f.parts[p].app.files[file].diff
		name := map[side]string{left: "left", right: "right"}[f.active]
		return fmt.Sprintf("%s %s %s %d", f.parts[p].commit.Hash, f.parts[p].app.files[file].Path, name, d.lineAt(f.active, line))
	}
	// "1" is in c3's a.go (old) and c2's a.go (new). The search stays in
	// the cursor's commit: c3's match, then no further one.
	l.cfg.Search.Add(config.Query{Text: "1"})
	pressLog(l, ".")
	if got := where(); got != "c3 a.go left 0" {
		t.Fatalf(". : at %q (%s)", got, f.msg)
	}
	pressLog(l, ".")
	if got := where(); got != "c3 a.go left 0" || f.msg != "/1: no match further down in this commit" {
		t.Errorf(". past c3's last match: at %q, msg %q", got, f.msg)
	}
	// P goes to the previous commit in history, c2; . finds its match.
	pressLog(l, "P", ".")
	if got := where(); got != "c2 a.go right 0" {
		t.Errorf("P . : at %q (%s)", got, f.msg)
	}
	// In c1, which has no 1.
	pressLog(l, "P", ".")
	if f.msg != "no match for 1 in changed lines of this commit" {
		t.Errorf("in c1: msg %q", f.msg)
	}
	// A regular expression: q or y, in c1's c.go.
	l.cfg.Search.Add(config.Query{Text: "^[qy]$", Regex: true})
	pressLog(l, ".")
	if got := where(); got != "c1 c.go right 0" || !strings.HasPrefix(f.msg, "regex /^[qy]$") {
		t.Errorf("regex: at %q, msg %q", got, f.msg)
	}
}

func TestFlowFindTextOutsideLog(t *testing.T) {
	// Without commits, the flow searches all files, without wrapping.
	a := NewApp(config.Default(), []source.File{
		{Path: "a.go", Old: []string{"one", "find"}, New: []string{"one", "other"}, InOld: true, InNew: true},
		{Path: "b.go", Old: []string{"two"}, New: []string{"two", "find"}, InOld: true, InNew: true},
	})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressApp(a, "f")
	a.cfg.Search.Add(config.Query{Text: "find"})
	for _, want := range []int{0, 1, 1} {
		pressApp(a, ".")
		if _, file, _ := a.flow.position(); file != want {
			t.Errorf(". : on file %d, want %d (%s)", file, want, a.flow.msg)
		}
	}
	if a.flow.msg != "/find: no match further down" {
		t.Errorf("msg %q", a.flow.msg)
	}
}

// newSearchBar returns the search bar around the help popup around m.
func newSearchBar(m tea.Model) *SearchBar {
	cfg := config.Default()
	if v, ok := m.(*Model); ok {
		cfg = v.cfg
	}
	s := WithSearch(cfg, WithHelp(cfg, m))
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return s
}

func TestSearchBar(t *testing.T) {
	m := newSearchModel()
	s := newSearchBar(m)
	status := func() string { v := ansi.Strip(s.View()); return v[strings.LastIndex(v, "\n")+1:] }
	before := status()
	pressRoot(s, "/")
	view := ansi.Strip(s.View())
	rows := strings.Split(view, "\n")
	// The bar above the status line, which stays.
	if s.input == nil || !strings.Contains(rows[len(rows)-4], "╭─ search changed lines  enter find · tab all · shift+tab regex · esc cancel") ||
		!strings.Contains(rows[len(rows)-3], "/█") || status() != before {
		t.Fatalf("/: bar\n%s", view)
	}
	// ? goes into the line, not to the help popup.
	pressRoot(s, "x3?", "backspace")
	if !strings.Contains(ansi.Strip(s.View()), "/x3█") {
		t.Errorf("typed:\n%s", ansi.Strip(s.View()))
	}
	pressRoot(s, "enter") // in changed lines: the removed x30
	if s.input != nil || s.cfg.Search.Query != (config.Query{Text: "x3"}) || m.lineAt(m.active, m.cursor) != 30 {
		t.Fatalf("enter: bar open %v, search %+v, cursor line %d", s.input != nil, s.cfg.Search.Query, m.lineAt(m.active, m.cursor))
	}
	// A broken regular expression keeps the bar open and says why.
	pressRoot(s, "/", "shift+tab", "([", "enter")
	if v := ansi.Strip(s.View()); s.input == nil || !strings.Contains(v, "╭─ regex") || !strings.Contains(v, "missing closing ]") {
		t.Errorf("broken regex:\n%s", v)
	}
	pressRoot(s, "backspace", "backspace", "x[12]", "enter")
	if s.cfg.Search.Query != (config.Query{Text: "x[12]", Regex: true}) {
		t.Errorf("regex search %+v", s.cfg.Search.Query)
	}
	// History: up goes back, with each query's mode, down forward to what was typed.
	pressRoot(s, "/", "shift+tab", "draft", "up")
	if s.input.text != "x[12]" || !s.regex {
		t.Errorf("up: %q regex %v", s.input.text, s.regex)
	}
	pressRoot(s, "up")
	if s.input.text != "x3" || s.regex {
		t.Errorf("up up: %q regex %v", s.input.text, s.regex)
	}
	pressRoot(s, "up") // no older one
	pressRoot(s, "down", "down")
	if s.input.text != "draft" || s.regex {
		t.Errorf("down down: %q regex %v, want the draft as typed", s.input.text, s.regex)
	}
	// esc and enter on nothing typed change nothing.
	pressRoot(s, "esc")
	pressRoot(s, "/", "enter")
	if s.input != nil || s.cfg.Search.Query != (config.Query{Text: "x[12]", Regex: true}) || len(s.cfg.Search.History) != 2 {
		t.Errorf("esc, empty enter: open %v, search %+v, history %v", s.input != nil, s.cfg.Search.Query, s.cfg.Search.History)
	}
	// The bar opens in the mode saved: text, as shift+tab switched to it before esc
	// above. Searching again moves a query to the end of the history, once.
	pressRoot(s, "/", "x3", "enter")
	if !slices.Equal(s.cfg.Search.History, []config.Query{{Text: "x[12]", Regex: true}, {Text: "x3"}}) {
		t.Errorf("history %v", s.cfg.Search.History)
	}
}

func TestSearchInAppReachesView(t *testing.T) {
	a := NewApp(config.Default(), []source.File{
		{Path: "a.go", Old: numbered("x", 20), New: numbered("y", 20), InOld: true, InNew: true},
		{Path: "b.go", Old: []string{"p"}, New: []string{"q"}, InOld: true, InNew: true},
	})
	s := WithSearch(a.cfg, a)
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	pressRoot(s, "enter", "/", "y7", "enter")
	if v := a.open.view; v.lineAt(v.active, v.cursor) != 7 || v.active != right {
		t.Errorf("search from the file list's diff view: on %v line %d", v.active, v.lineAt(v.active, v.cursor))
	}
}
