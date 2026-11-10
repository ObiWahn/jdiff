package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// docStyle is what a piece of a page is, which decides its style in the
// theme when it is drawn.
type docStyle int

const (
	docPlain docStyle = iota
	docBold
	docItalic
	docCode
	docHeading
	docLink
	docDecoration // bullets, rules, the lines of tables and quotes
	docOverflow   // » at the end of a cut line
)

// docSeg is a piece of a line of a page in one style: printable text, part
// of link number link of the page, -1 for none.
type docSeg struct {
	text  string
	style docStyle
	link  int
}

// docLine is a line of a page; anchor is the name of the heading it starts,
// as links to it write it after #.
type docLine struct {
	segs   []docSeg
	anchor string
}

// plain returns the text of l, without styles.
func (l docLine) plain() string {
	var b strings.Builder
	for _, s := range l.segs {
		b.WriteString(s.text)
	}
	return b.String()
}

// docPage is a markdown page rendered for a width: its lines, none wider,
// and the targets of its links with the line each starts on.
type docPage struct {
	lines   []docLine
	links   []pageLink
	anchors map[string]int // the line of each heading
}

// pageLink is a link of a page: where it goes, as written, and its first
// line; broken when it leads nowhere, which Readme decides.
type pageLink struct {
	target string
	line   int
	broken bool
}

var markdown = goldmark.New(goldmark.WithExtensions(extension.Table))

// renderMarkdown renders src for a screen width columns wide. Images are left
// out, so a line of badges is gone; a line still too wide, of code or a table,
// is cut with ».
func renderMarkdown(src []byte, width int) docPage {
	width = max(width, 1)
	r := &mdRenderer{src: src}
	lines := r.blocks(markdown.Parser().Parse(text.NewReader(src)), width, false)
	page := docPage{anchors: map[string]int{}}
	for i := range r.links {
		page.links = append(page.links, pageLink{target: r.links[i], line: -1})
	}
	for i, l := range lines {
		l.segs = cutSegs(l.segs, width)
		lines[i] = l
		if l.anchor != "" {
			page.anchors[l.anchor] = i
		}
		for _, s := range l.segs {
			if s.link >= 0 && page.links[s.link].line < 0 {
				page.links[s.link].line = i
			}
		}
	}
	page.lines = lines
	return page
}

// renderText renders a file that is no markdown for a screen width columns
// wide: its lines as code, the long ones wrapped at the last space that
// fits, past their indentation, else where the line ends; the further pieces
// indented.
func renderText(src []byte, width int) docPage {
	width = max(width, 1)
	page := docPage{anchors: map[string]int{}}
	text := strings.TrimSuffix(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	for _, line := range strings.Split(text, "\n") {
		rest, lead, w := printable(line), "", width
		add := func(s string) {
			page.lines = append(page.lines, docLine{segs: trimSegs([]docSeg{{lead, docPlain, -1}, {s, docCode, -1}})})
		}
		for ansi.StringWidth(rest) > w {
			piece := ansi.Truncate(rest, w, "")
			if i := strings.LastIndex(piece, " "); i > 0 && strings.TrimSpace(piece[:i]) != "" {
				piece = piece[:i+1]
			}
			add(piece)
			rest = rest[len(piece):]
			if lead == "" && width > 2 {
				lead, w = "  ", width-2
			}
		}
		add(rest)
	}
	return page
}

// mdRenderer renders the blocks of a markdown tree into lines.
type mdRenderer struct {
	src     []byte
	links   []string        // the targets of the links found so far
	anchors map[string]bool // the anchors given so far
	depth   int             // of nested lists
}

// blocks renders the children of n one below the other, with an empty line
// between them unless tight, as in the items of a tight list.
func (r *mdRenderer) blocks(n ast.Node, width int, tight bool) []docLine {
	var out []docLine
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		lines := r.block(c, width)
		if len(lines) == 0 {
			continue
		}
		if len(out) > 0 && !tight {
			out = append(out, docLine{})
		}
		out = append(out, lines...)
	}
	return out
}

// block renders the block n, width columns wide but for code.
func (r *mdRenderer) block(n ast.Node, width int) []docLine {
	switch n := n.(type) {
	case *ast.Heading:
		lines := wrapSegs(r.inline(n, docHeading), width)
		if len(lines) == 0 {
			return nil
		}
		lines[0].anchor = r.anchor(n)
		if n.Level <= 2 {
			w := 0
			for _, l := range lines {
				w = max(w, segsWidth(l.segs))
			}
			rule := "─"
			if n.Level == 1 {
				rule = "═"
			}
			lines = append(lines, docLine{segs: []docSeg{{strings.Repeat(rule, w), docDecoration, -1}}})
		}
		return lines
	case *ast.Paragraph, *ast.TextBlock:
		return wrapSegs(r.inline(n, docPlain), width)
	case *ast.List:
		return r.list(n, width)
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		var out []docLine
		lines := n.Lines()
		for i := range lines.Len() {
			seg := lines.At(i)
			line := printable(strings.TrimRight(string(seg.Value(r.src)), "\r\n"))
			out = append(out, docLine{segs: trimSegs([]docSeg{{"  ", docPlain, -1}, {line, docCode, -1}})})
		}
		return out
	case *ast.Blockquote:
		inner := r.blocks(n, max(width-2, 1), false)
		for i := range inner {
			inner[i].segs = append([]docSeg{{"│ ", docDecoration, -1}}, inner[i].segs...)
		}
		return inner
	case *ast.ThematicBreak:
		return []docLine{{segs: []docSeg{{strings.Repeat("─", width), docDecoration, -1}}}}
	case *east.Table:
		return r.table(n, width)
	}
	return nil // HTML
}

// bullets are the marks of the items of unordered lists, by depth.
var bullets = []string{"•", "◦", "▪"}

// list renders the items of n, each after its bullet or number, its further
// lines indented to its text.
func (r *mdRenderer) list(n *ast.List, width int) []docLine {
	var marks []string
	for c, i := n.FirstChild(), 0; c != nil; c, i = c.NextSibling(), i+1 {
		if n.IsOrdered() {
			marks = append(marks, fmt.Sprintf("%d.", n.Start+i))
		} else {
			marks = append(marks, bullets[min(r.depth, len(bullets)-1)])
		}
	}
	markW := 0
	for _, m := range marks {
		markW = max(markW, ansi.StringWidth(m))
	}
	indent := markW + 1
	r.depth++
	defer func() { r.depth-- }()
	var out []docLine
	for c, i := n.FirstChild(), 0; c != nil; c, i = c.NextSibling(), i+1 {
		if i > 0 && !n.IsTight {
			out = append(out, docLine{})
		}
		lines := r.blocks(c, max(width-indent, 1), n.IsTight)
		if len(lines) == 0 {
			lines = []docLine{{}}
		}
		for j, l := range lines {
			lead := strings.Repeat(" ", indent)
			style := docPlain
			if j == 0 {
				lead, style = fmt.Sprintf("%*s ", markW, marks[i]), docDecoration
			}
			if len(l.segs) == 0 && j > 0 {
				out = append(out, l) // an empty line stays empty
				continue
			}
			l.segs = append([]docSeg{{lead, style, -1}}, l.segs...)
			out = append(out, l)
		}
	}
	return out
}

// table renders t with its columns aligned between bars, the header bold
// and ruled off. When it does not fit, the widest column is narrowed and its
// cells wrapped, down to tableMinColumn; beyond that the lines are cut.
func (r *mdRenderer) table(t *east.Table, width int) []docLine {
	var rows [][][]docSeg
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		style := docPlain
		if _, ok := row.(*east.TableHeader); ok {
			style = docBold
		}
		var cells [][]docSeg
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, r.inline(cell, style))
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return nil
	}
	cols := len(t.Alignments)
	widths := make([]int, cols)
	for _, cells := range rows {
		for i, c := range cells {
			if i < cols {
				for _, l := range wrapSegs(c, 1<<20) {
					widths[i] = max(widths[i], segsWidth(l.segs))
				}
			}
		}
	}
	const gap = 3 // " │ "
	total := func() int {
		sum := gap * (cols - 1)
		for _, w := range widths {
			sum += w
		}
		return sum
	}
	for total() > width {
		widest := 0
		for i, w := range widths {
			if w > widths[widest] {
				widest = i
			}
		}
		if widths[widest] <= tableMinColumn {
			break
		}
		widths[widest]--
	}
	bar := docSeg{" │ ", docDecoration, -1}
	var out []docLine
	for ri, cells := range rows {
		wrapped := make([][]docLine, cols)
		height := 1
		for i := range cols {
			if i < len(cells) {
				wrapped[i] = wrapSegs(cells[i], widths[i])
			}
			height = max(height, len(wrapped[i]))
		}
		for j := range height {
			var segs []docSeg
			for i := range cols {
				if i > 0 {
					segs = append(segs, bar)
				}
				var cell []docSeg
				if j < len(wrapped[i]) {
					cell = wrapped[i][j].segs
				}
				segs = append(segs, alignSegs(cell, widths[i], t.Alignments[i])...)
			}
			out = append(out, docLine{segs: trimSegs(segs)})
		}
		if ri == 0 {
			var rule []string
			for _, w := range widths {
				rule = append(rule, strings.Repeat("─", w))
			}
			out = append(out, docLine{segs: []docSeg{{strings.Join(rule, "─┼─"), docDecoration, -1}}})
		}
	}
	return out
}

// tableMinColumn is the narrowest a column of a table is wrapped to.
const tableMinColumn = 10

// alignSegs pads segs to width as align says: right, centered or left.
func alignSegs(segs []docSeg, width int, align east.Alignment) []docSeg {
	pad := max(width-segsWidth(segs), 0)
	left := 0
	switch align {
	case east.AlignRight:
		left = pad
	case east.AlignCenter:
		left = pad / 2
	}
	out := []docSeg{{strings.Repeat(" ", left), docPlain, -1}}
	out = append(out, segs...)
	return append(out, docSeg{strings.Repeat(" ", pad-left), docPlain, -1})
}

// trimSegs drops the spaces at the end of a line.
func trimSegs(segs []docSeg) []docSeg {
	for len(segs) > 0 {
		last := &segs[len(segs)-1]
		last.text = strings.TrimRight(last.text, " ")
		if last.text != "" {
			break
		}
		segs = segs[:len(segs)-1]
	}
	return segs
}

// anchor returns the name links give heading h after #, as GitHub makes it:
// lower case, spaces as -, without punctuation, numbered when it repeats.
func (r *mdRenderer) anchor(h *ast.Heading) string {
	var b strings.Builder
	for _, s := range r.inlineQuiet(h) {
		for _, c := range strings.ToLower(s.text) {
			switch {
			case c == ' ':
				b.WriteRune('-')
			case c == '-' || c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c):
				b.WriteRune(c)
			}
		}
	}
	name := b.String()
	if r.anchors == nil {
		r.anchors = map[string]bool{}
	}
	for i := 1; r.anchors[name]; i++ {
		name = fmt.Sprintf("%s-%d", b.String(), i)
	}
	r.anchors[name] = true
	return name
}

// inlineQuiet returns the inline pieces of n without counting its links.
func (r *mdRenderer) inlineQuiet(n ast.Node) []docSeg {
	links := len(r.links)
	segs := r.inline(n, docPlain)
	r.links = r.links[:links]
	return segs
}

// inline returns the text of the inline children of n in style, as pieces;
// emphasis and code inside it take their own style, but in a heading or a
// link. A hard line break is a piece "\n".
func (r *mdRenderer) inline(n ast.Node, style docStyle) []docSeg {
	var out []docSeg
	r.inlineInto(n, style, -1, &out)
	return out
}

func (r *mdRenderer) inlineInto(n ast.Node, style docStyle, link int, out *[]docSeg) {
	inner := func(s docStyle) docStyle {
		if style == docHeading || style == docLink {
			return style
		}
		return s
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			*out = append(*out, docSeg{printable(string(c.Segment.Value(r.src))), style, link})
			switch {
			case c.HardLineBreak():
				*out = append(*out, docSeg{"\n", style, link})
			case c.SoftLineBreak():
				*out = append(*out, docSeg{" ", style, link})
			}
		case *ast.String:
			*out = append(*out, docSeg{printable(string(c.Value)), style, link})
		case *ast.CodeSpan:
			var b strings.Builder
			for t := c.FirstChild(); t != nil; t = t.NextSibling() {
				if t, ok := t.(*ast.Text); ok {
					b.Write(t.Segment.Value(r.src))
				}
			}
			*out = append(*out, docSeg{printable(b.String()), inner(docCode), link})
		case *ast.Emphasis:
			s := docItalic
			if c.Level >= 2 {
				s = docBold
			}
			r.inlineInto(c, inner(s), link, out)
		case *ast.Link:
			id, before := len(r.links), len(*out)
			r.links = append(r.links, string(c.Destination))
			r.inlineInto(c, docLink, id, out)
			if len(*out) == before { // only an image: a badge
				r.links = r.links[:id]
			}
		case *ast.AutoLink:
			id := len(r.links)
			r.links = append(r.links, string(c.URL(r.src)))
			*out = append(*out, docSeg{printable(string(c.Label(r.src))), docLink, id})
		case *ast.Image, *ast.RawHTML:
		default:
			r.inlineInto(c, style, link, out)
		}
	}
}

// segsWidth returns the width of segs in columns.
func segsWidth(segs []docSeg) int {
	w := 0
	for _, s := range segs {
		w += ansi.StringWidth(s.text)
	}
	return w
}

// wordToken is a word of inline text, which may be made of pieces in several
// styles, and the space before it, in the style of the text it was in.
type wordToken struct {
	space docSeg
	parts []docSeg
	brk   bool // a hard line break
}

// wrapSegs fills lines of width columns with the words of segs, breaking
// at spaces; a word wider than a line is broken where the line ends.
func wrapSegs(segs []docSeg, width int) []docLine {
	var words []wordToken
	cur := wordToken{}
	for _, s := range segs {
		if s.text == "\n" {
			words = append(words, cur, wordToken{brk: true})
			cur = wordToken{}
			continue
		}
		for i, part := range strings.Split(s.text, " ") {
			if i > 0 {
				words = append(words, cur)
				cur = wordToken{space: docSeg{" ", s.style, s.link}}
			}
			if part != "" {
				cur.parts = append(cur.parts, docSeg{part, s.style, s.link})
			}
		}
	}
	words = append(words, cur)

	var out []docLine
	var line []docSeg
	lineW := 0
	flush := func() {
		out = append(out, docLine{segs: line})
		line, lineW = nil, 0
	}
	for _, w := range words {
		if w.brk {
			flush()
			continue
		}
		if len(w.parts) == 0 {
			continue
		}
		ww := segsWidth(w.parts)
		switch {
		case lineW == 0:
		case lineW+1+ww <= width:
			line = append(line, w.space)
			lineW++
		default:
			flush()
		}
		for ww > width-lineW {
			// Only a word too long for a line of its own is broken.
			if lineW > 0 {
				flush()
				continue
			}
			line = sliceSegs(w.parts, 0, width)
			w.parts = sliceSegs(w.parts, width, ww)
			ww -= width
			flush()
		}
		line = append(line, w.parts...)
		lineW += ww
	}
	if len(line) > 0 {
		flush()
	}
	return out
}

// sliceSegs returns the columns from to to of segs.
func sliceSegs(segs []docSeg, from, to int) []docSeg {
	var out []docSeg
	col := 0
	for _, s := range segs {
		w := ansi.StringWidth(s.text)
		if col+w > from && col < to {
			s.text = ansi.Cut(s.text, max(from-col, 0), min(to-col, w))
			out = append(out, s)
		}
		col += w
	}
	return out
}

// cutSegs returns segs cut to width columns, the last of them », when they
// are wider.
func cutSegs(segs []docSeg, width int) []docSeg {
	if segsWidth(segs) <= width {
		return segs
	}
	return append(sliceSegs(segs, 0, width-1), docSeg{"»", docOverflow, -1})
}
