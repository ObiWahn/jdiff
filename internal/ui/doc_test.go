package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
)

// plainLines returns the text of the lines of p.
func plainLines(p docPage) []string {
	var out []string
	for _, l := range p.lines {
		out = append(out, l.plain())
	}
	return out
}

func TestRenderMarkdown(t *testing.T) {
	for _, c := range []struct {
		name  string
		src   string
		width int
		want  []string
	}{
		{"heading 1 ruled", "# x0 x1", 20, []string{"x0 x1", "═════"}},
		{"heading 2 ruled", "## x0", 20, []string{"x0", "──"}},
		{"heading 3 alone", "### x0", 20, []string{"x0"}},
		{"blocks apart", "x0\n\nx1", 20, []string{"x0", "", "x1"}},
		{"lines joined", "x0\nx1", 20, []string{"x0 x1"}},
		{"wrapped at spaces", "x0 x1 x2 x3 x4", 8, []string{"x0 x1 x2", "x3 x4"}},
		{"exactly full", "x0 x1", 5, []string{"x0 x1"}},
		{"long word broken", "x0 x123456789", 5, []string{"x0", "x1234", "56789"}},
		{"hard break", "x0  \nx1", 20, []string{"x0", "x1"}},
		{"tight list", "- x0\n- x1", 20, []string{"• x0", "• x1"}},
		{"loose list", "- x0\n\n- x1", 20, []string{"• x0", "", "• x1"}},
		{"nested list", "- x0\n  - x1\n    - x2\n      - x3\n- x4", 20,
			[]string{"• x0", "  ◦ x1", "    ▪ x2", "      ▪ x3", "• x4"}},
		{"list item wrapped", "- x0 x1 x2", 6, []string{"• x0", "  x1", "  x2"}},
		{"numbers aligned", "9. x0\n10. x1", 20, []string{" 9. x0", "10. x1"}},
		{"code kept", "```\nx0  x1\n\nx2\n```", 20, []string{"  x0  x1", "", "  x2"}},
		{"code cut", "    x0123456789", 8, []string{"  x0123»"}},
		{"quote", "> x0 x1", 5, []string{"│ x0", "│ x1"}},
		{"rule", "x0\n\n---", 4, []string{"x0", "", "────"}},
		{"emphasis and code", "*x0* **x1** `x2 x3`", 20, []string{"x0 x1 x2 x3"}},
		{"badge left out", "[![x0](x1.svg)](x2)\n\n# x3", 20, []string{"x3", "══"}},
		{"html left out", "<div>x0</div>\n\nx1", 20, []string{"x1"}},
		{"table", "| x0 | x1 |\n|---|--:|\n| x22 | x3 |", 20,
			[]string{"x0  │ x1", "────┼───", "x22 │ x3"}},
		// The widest column wraps, down to tableMinColumn.
		{"table wrapped", "| x0 | x1 |\n|---|---|\n| x2 | x3 x4 x5 x6 x7 |", 17,
			[]string{"x0 │ x1", "───┼─────────────", "x2 │ x3 x4 x5 x6", "   │ x7"}},
		// Words wider than tableMinColumn are broken, then the lines cut.
		{"table cut", "| x0 | x1 |\n|---|---|\n| x2345678901 | x3456789012 |", 16,
			[]string{"x0         │ x1", "───────────┼───»", "x234567890 │ x3»", "1          │ 2"}},
	} {
		got := plainLines(renderMarkdown([]byte(c.src), c.width))
		if !slices.Equal(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// Pieces keep their styles, also where lines wrap: the space inside a link
// is part of it.
func TestRenderMarkdownStyles(t *testing.T) {
	p := renderMarkdown([]byte("x0 **x1** *x2* `x3` [x4 x5](x6.md)"), 10)
	var got []string
	for _, l := range p.lines {
		var segs []string
		for _, s := range l.segs {
			segs = append(segs, fmt.Sprintf("%q/%d/%d", s.text, s.style, s.link))
		}
		got = append(got, strings.Join(segs, " "))
	}
	want := []string{
		`"x0"/0/-1 " "/0/-1 "x1"/1/-1 " "/0/-1 "x2"/2/-1`,
		`"x3"/3/-1 " "/0/-1 "x4"/5/0 " "/5/0 "x5"/5/0`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
}

// Links know their targets and first lines; headings their names as links
// write them, numbered when one repeats.
func TestRenderMarkdownLinks(t *testing.T) {
	src := "# X0, x1-x2_x3 x4.toml\n\nx5 [x6\nx7](x8.md#x9) <https://x10.example>\n\n## X0, x1-x2_x3 x4.toml"
	p := renderMarkdown([]byte(src), 10)
	// The heading takes three lines and its rule.
	if want := []pageLink{{"x8.md#x9", 5}, {"https://x10.example", 6}}; !slices.Equal(p.links, want) {
		t.Errorf("links %v, want %v", p.links, want)
	}
	if want := map[string]int{"x0-x1-x2_x3-x4toml": 0, "x0-x1-x2_x3-x4toml-1": 9}; fmt.Sprint(p.anchors) != fmt.Sprint(want) {
		t.Errorf("anchors %v, want %v\n%q", p.anchors, want, plainLines(p))
	}
}

// docTestPages are the pages of newTestDoc: a.md, then doc/b.md, which links
// back. a.md has the lines x0 … x29 before its links, b.md the headings x0 …
// x9 with ten lines each.
var docTestPages = []string{"a.md", "doc/b.md"}

func newTestDoc(t *testing.T, width, height int) *Doc {
	t.Helper()
	var a, b strings.Builder
	for i := range 30 {
		fmt.Fprintf(&a, "x%d\n\n", i)
	}
	a.WriteString("[b](doc/b.md) [b x5](doc/b.md#x5) [x30](https://x30.example) [x31](x31.md) [b x99](doc/b.md#x99)\n")
	for i := range 10 {
		fmt.Fprintf(&b, "### x%d\n\n", i)
		for j := range 10 {
			fmt.Fprintf(&b, "y%d%d\n\n", i, j)
		}
	}
	b.WriteString("[a](../a.md)\n")
	docs := fstest.MapFS{"a.md": {Data: []byte(a.String())}, "doc/b.md": {Data: []byte(b.String())}}
	d := NewDoc(config.Default(), docs, docTestPages, 0)
	d.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return d
}

// selected returns the target of the selected link, "" for none.
func (d *Doc) selected() string {
	if d.link < 0 {
		return ""
	}
	return d.rendered.links[d.link].target
}

// a.md has its links on line 60, after x0 … x29 with empty lines between.
func TestDocLinks(t *testing.T) {
	d := newTestDoc(t, 80, 12) // 10 lines shown
	pressRoot(d, "enter")
	if d.msg != "no link on this line: tab shift+tab go to the next, previous" {
		t.Errorf("enter without a link: %q", d.msg)
	}
	// tab goes to the first link below, then to the next on the same line.
	pressRoot(d, "tab")
	if d.cursor != 60 || d.selected() != "doc/b.md" || d.top != 51 || d.msg != "doc/b.md" {
		t.Fatalf("tab: cursor %d, link %q, top %d, msg %q", d.cursor, d.selected(), d.top, d.msg)
	}
	pressRoot(d, "tab", "enter")
	if d.page != 1 || d.top != d.rendered.anchors["x5"] || d.cursor != d.top || d.rendered.lines[d.top].plain() != "x5" {
		t.Errorf("enter on b x5: page %d, top %d, cursor %d", d.page, d.top, d.cursor)
	}
	// Back where it was, on the first link of the line.
	pressRoot(d, "backspace")
	if d.page != 0 || d.cursor != 60 || d.top != 51 || d.selected() != "doc/b.md" {
		t.Errorf("backspace: page %d, cursor %d, top %d, link %q", d.page, d.cursor, d.top, d.selected())
	}
	pressRoot(d, "backspace")
	if d.msg != "no link followed to go back from" {
		t.Errorf("backspace again: %q", d.msg)
	}
	for _, c := range []struct{ tabs, msg string }{
		{"tab tab tab", "https://x30.example"},
		{"tab tab tab tab", "no page x31.md"},
		{"tab tab tab tab tab", "no heading #x99 in doc/b.md"},
	} {
		d := newTestDoc(t, 80, 12)
		pressRoot(d, strings.Fields(c.tabs)...)
		pressRoot(d, "enter")
		if d.msg != c.msg {
			t.Errorf("%s enter: %q, want %q", c.tabs, d.msg, c.msg)
		}
	}
	// The cursor on the line of links selects the first and says where it goes.
	pressRoot(d, "end")
	if d.selected() != "doc/b.md" || d.msg != "doc/b.md" {
		t.Errorf("end: link %q, msg %q", d.selected(), d.msg)
	}
	pressRoot(d, "tab", "tab", "tab", "tab", "tab")
	if d.selected() != "doc/b.md#x99" || d.msg != "no link further down" {
		t.Errorf("past the last: %q, %q", d.selected(), d.msg)
	}
	pressRoot(d, "shift+tab")
	if d.selected() != "x31.md" {
		t.Errorf("shift+tab: %q", d.selected())
	}
	// h l go through the links of the line only.
	pressRoot(d, "l", "l")
	if d.selected() != "doc/b.md#x99" || d.msg != "no link further right" {
		t.Errorf("l on the last: %q, %q", d.selected(), d.msg)
	}
	pressRoot(d, "h", "left", "right")
	if d.selected() != "x31.md" || d.msg != "x31.md" {
		t.Errorf("h left right: %q, %q", d.selected(), d.msg)
	}
	pressRoot(d, "home", "h", "h", "h", "h", "h")
	if d.selected() != "" || d.msg != "no link on this line" {
		t.Errorf("h without links: %q, %q", d.selected(), d.msg)
	}
	pressRoot(d, "end", "h")
	if d.selected() != "doc/b.md" || d.msg != "no link further left" {
		t.Errorf("h on the first: %q, %q", d.selected(), d.msg)
	}
	pressRoot(d, "home", "shift+tab")
	if d.selected() != "" || d.msg != "no link further up" {
		t.Errorf("shift+tab at the top: %q, %q", d.selected(), d.msg)
	}
	// A link back from doc/b.md, relative to it.
	pressRoot(d, "n", "end", "enter")
	if d.page != 0 || d.top != 0 || d.cursor != 0 {
		t.Errorf("../a.md: page %d, top %d, cursor %d", d.page, d.top, d.cursor)
	}
}

// a.md has 61 lines, 10 shown: the cursor moves, and paging moves the screen
// and the cursor along.
func TestDocPagesAndScroll(t *testing.T) {
	d := newTestDoc(t, 80, 12)
	for _, c := range []struct {
		keys        string
		cursor, top int
	}{
		{"j", 1, 0}, {"k", 0, 0}, {"k", 0, 0}, {"pgdown", 9, 9}, {"ctrl+d", 14, 14}, {"ctrl+u", 9, 9},
		{"pgup", 0, 0}, {"end", 60, 51}, {"j", 60, 51}, {"home", 0, 0},
	} {
		pressRoot(d, c.keys)
		if d.cursor != c.cursor || d.top != c.top {
			t.Errorf("%s: cursor %d, top %d; want %d, %d", c.keys, d.cursor, d.top, c.cursor, c.top)
		}
	}
	if rows := strings.Split(d.frame(), "\n"); !strings.HasPrefix(rows[1], d.cfg.Theme.Cursor.Render(" ")+"x0") {
		t.Errorf("the cursor's line is not marked: %q", rows[1])
	}
	pressRoot(d, "p")
	if d.page != 0 || d.msg != "first page" {
		t.Errorf("p on the first: page %d, %q", d.page, d.msg)
	}
	pressRoot(d, "j", "n")
	if d.page != 1 || d.top != 0 || d.cursor != 0 || !strings.HasPrefix(ansi.Strip(d.frame()), " doc/b.md") {
		t.Errorf("n: page %d, top %d, cursor %d", d.page, d.top, d.cursor)
	}
	pressRoot(d, "n")
	if d.page != 1 || d.msg != "last page" {
		t.Errorf("n on the last: page %d, %q", d.page, d.msg)
	}
}

func TestDocSearch(t *testing.T) {
	d := newTestDoc(t, 80, 12)
	d.Update(findMsg(1))
	if d.msg != nothingSearched {
		t.Errorf("before a search: %q", d.msg)
	}
	d.cfg.Search.Add(config.Query{Text: "x2"}) // x2, x20 … x29
	d.Update(findMsg(1))
	if d.cursor != 4 || d.msg != "/x2: match 1/11 · . next · , previous" {
		t.Errorf("x2: cursor %d, %q", d.cursor, d.msg)
	}
	pressRoot(d, ".")
	if d.cursor != 40 || d.top != 31 {
		t.Errorf(". : cursor %d, top %d", d.cursor, d.top)
	}
	pressRoot(d, ",", ",")
	if d.cursor != 4 || d.msg != "/x2: no match further up" {
		t.Errorf(", ,: cursor %d, %q", d.cursor, d.msg)
	}
	if !strings.Contains(d.frame(), d.cfg.Theme.Search.Render("x2")) {
		t.Error("the match is not drawn in the search style")
	}
	d.cfg.Search.Add(config.Query{Text: "x77"})
	pressRoot(d, ".")
	if d.msg != "no match for x77 in this page" {
		t.Errorf("x77: %q", d.msg)
	}
}

// The status bar shows only the keys that work on a page of text, in the
// order of all status bars.
func TestDocStatusBar(t *testing.T) {
	d := newTestDoc(t, 80, 12)
	lines := strings.Split(ansi.Strip(d.frame()), "\n")
	if got := lines[len(lines)-1]; strings.TrimSpace(got) != "enter follow · backspace back · ? help · q quit" {
		t.Errorf("status bar %q", got)
	}
}

// docFiles returns the pages of jdiff's documentation.
func docFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "doc", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range append(files, filepath.Join("..", "..", "README.md")) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(f)] = b
	}
	return out
}

// All pages of the documentation at every width from minWidth to 200: no
// line wider than the text, no link lost. Then the frames of every height
// from minHeight to 40, in the narrowest terminal, one column wider and at
// 80: as high and no wider than the screen.
func TestDocSweep(t *testing.T) {
	files := docFiles(t)
	for name, src := range files {
		links := len(renderMarkdown(src, 1000).links)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for w := minWidth - 2; w <= 200; w++ {
				p := renderMarkdown(src, w)
				for i, l := range p.lines {
					if lw := segsWidth(l.segs); lw > w {
						t.Fatalf("width %d: line %d is %d wide: %q", w, i, lw, l.plain())
					}
				}
				if len(p.links) != links || slices.ContainsFunc(p.links, func(l pageLink) bool { return l.line < 0 }) {
					t.Fatalf("width %d: links %v, want %d", w, p.links, links)
				}
			}
		})
	}
	pages := slices.Sorted(func(yield func(string) bool) {
		for name := range files {
			if !yield(name) {
				return
			}
		}
	})
	docs := fstest.MapFS{}
	for name, src := range files {
		docs[name] = &fstest.MapFile{Data: src}
	}
	for _, w := range []int{minWidth, minWidth + 1, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			t.Parallel()
			for page := range pages {
				for h := minHeight; h <= 40; h++ {
					d := NewDoc(config.Default(), docs, pages, page)
					d.Update(tea.WindowSizeMsg{Width: w, Height: h})
					pressRoot(d, "pgdown", "tab")
					rows := strings.Split(d.frame(), "\n")
					if len(rows) != h {
						t.Fatalf("%s %dx%d: %d rows", pages[page], w, h, len(rows))
					}
					for i, r := range rows {
						if rw := ansi.StringWidth(r); rw > w {
							t.Fatalf("%s %dx%d: row %d is %d wide", pages[page], w, h, i, rw)
						}
					}
				}
			}
		})
	}
}
