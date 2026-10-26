package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/diff"
)

const tabWidth = 4

// lineStyles are the styles of one rendered line.
type lineStyles struct {
	base  lipgloss.Style // text outside emphasized spans
	emph  lipgloss.Style // changed words
	space lipgloss.Style // changed whitespace, drawn as · and →
	ws    lipgloss.Style // other whitespace, drawn as · and → when showAll
	// showAll draws all whitespace visibly, not only changed whitespace.
	showAll bool
	// plainSpace draws changed whitespace like unchanged whitespace.
	plainSpace bool
	// showTrailing draws whitespace at the end of the line visibly in
	// trailing; with showAll the end of the line is drawn as lineEnd.
	trailing     lipgloss.Style
	showTrailing bool
	lineEnd      string
	// cr is drawn for the \r of a \r\n line end where it changed, and with
	// showCR on all lines.
	cr     string
	showCR bool
	// hidden is the style of ☠, drawn for invisible characters.
	hidden lipgloss.Style
	// matches are the parts of the line found by /, drawn in match over all
	// other styles.
	matches []diff.Span
	match   lipgloss.Style
}

// painter draws text in a style as lipgloss renders it, without the cost of
// a lipgloss render for each run of a line: the escape sequences come from
// rendering a sample once. Underlined and struck through styles are
// rendered by lipgloss, which styles their spaces apart.
type painter struct {
	style       lipgloss.Style
	open, close string
	slow        bool
}

func newPainter(st lipgloss.Style) painter {
	p := painter{style: st, slow: st.GetUnderline() || st.GetStrikethrough()}
	if !p.slow {
		out := st.Render("x")
		i := strings.Index(out, "x")
		p.open, p.close = out[:i], out[i+1:]
	}
	return p
}

func (p painter) paint(s string) string {
	if p.slow {
		return p.style.Render(s)
	}
	return p.open + s + p.close
}

// hiddenSymbol stands for characters that are not visible text, a space or
// a tab: control and format characters, among them those that change the
// direction of text and could reorder what is shown, and unusual spaces.
const hiddenSymbol = "☠"

// hiddenChar reports whether r is drawn as hiddenSymbol.
func hiddenChar(r rune) bool {
	if r < utf8.RuneSelf { // ASCII: only control characters
		return r != '\t' && (r < 0x20 || r == 0x7f)
	}
	return r != '\t' && (unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) && r != ' ')
}

// sideStyles returns the styles of side s for whole-line changes, the
// unchanged parts of changed lines, changed words and changed whitespace.
func (d *fileDiff) sideStyles(s side) (line, nonEmph, emph, indent lipgloss.Style) {
	t := d.cfg.Theme
	if s == left {
		return t.Minus, t.MinusNonEmph, t.MinusEmph, t.MinusIndent
	}
	return t.Plus, t.PlusNonEmph, t.PlusEmph, t.PlusIndent
}

// help returns the keys of the status bar, in the order of all pages:
// backspace, n/p and f when shown from a file list, N/P in the log.
func (m *Model) help() string {
	keys := []string{"enter mark/anchor"}
	if m.canBack {
		keys = append(keys, "backspace files")
	}
	keys = append(keys, "space fold", "]/[ change", "/ search")
	if m.canBack {
		keys = append(keys, "n/p file")
	}
	if m.commitNav {
		keys = append(keys, "N/P commit")
	}
	if m.canBack {
		keys = append(keys, "f flow")
	}
	keys = append(keys, "d git diff")
	return strings.Join(append(keys, "? help", "q quit"), " · ")
}

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.header() + "\n")
	m.syncLayout()
	pos := m.pos()
	for i := m.top; i < m.top+m.bodyHeight(); i++ {
		if i < len(m.screen) {
			b.WriteString(m.screenLine(m.screen[i], i == pos))
		}
		b.WriteString("\n")
	}
	b.WriteString(m.status())
	return b.String()
}

func (d *fileDiff) numWidth() int {
	return len(strconv.Itoa(max(len(d.lines[left]), len(d.lines[right]), 1)))
}

// header renders the file names above both panes.
func (m *Model) header() string {
	return paneTitles(m.cfg.Theme, m.names, m.active, m.cfg.Settings.SideBySide, m.width)
}

// screenLine renders screen line s in the current layout, marked as the
// cursor line when cursor is set.
func (m *Model) screenLine(s sline, cursor bool) string {
	return m.render(s, cursor, layout{m.width, m.xoff, m.active, m.screenShape})
}

// render renders screen line s in layout lo, marked as the cursor line when
// cursor is set.
func (d *fileDiff) render(s sline, cursor bool, lo layout) string {
	switch {
	case s.gap():
		return ""
	case lo.unified:
		return d.unifiedLine(s.v, s.sub, cursor, lo)
	}
	return d.bodyRow(s.v, s.sub, cursor, lo)
}

// startCol returns the first column of a line shown in a pane width columns
// wide: of its piece when lines are wrapped, else the scroll offset.
func startCol(piece, width int, lo layout) int {
	if lo.wrap > 0 {
		return piece * width
	}
	return lo.xoff
}

// unifiedLine renders line sub of displayed row v in the normal layout:
// the gutter with old and new line number, and the content. Unchanged
// lines show both numbers, changed ones the number of their side, further
// pieces of a wrapped line none.
func (d *fileDiff) unifiedLine(v, sub int, cursor bool, lo layout) string {
	if d.view[v].hidden > 0 {
		return d.fold(v, cursor, lo.width)
	}
	t := d.cfg.Theme
	row := d.rows[d.view[v].row]
	s, piece := d.unifiedPiece(v, sub, lo.shape)
	nw := d.numWidth()
	changed := row.Kind != diff.Equal
	number := func(line int, of side) string {
		if line < 0 || (changed && of != s) || piece > 0 {
			return strings.Repeat(" ", nw)
		}
		return fmt.Sprintf("%*d", nw, line+1)
	}
	nums := number(row.Left, left) + " ⋮ " + number(row.Right, right) + " "
	pending := piece == 0 && d.isPending(row) && (row.Kind == diff.Equal || d.pending.side == s)
	line := lineOf(row, s)
	_, moved := d.movedTo(s, line)
	gutter := d.gutter(row, s, changed, pending, moved, piece > 0, nums, t.Decoration, cursor, cursor)
	width := d.contentWidth(lo.width, true)
	return gutter + d.content(s, row, line, width, cursor, moved, startCol(piece, width, lo), lo.wrap == 0)
}

// bodyRow renders line sub of displayed row v side by side, marked as the
// cursor row when cursor is set.
func (d *fileDiff) bodyRow(v, sub int, cursor bool, lo layout) string {
	if d.view[v].hidden > 0 {
		return d.fold(v, cursor, lo.width)
	}
	w, nw := lo.width/2, d.numWidth()
	return d.cell(left, v, sub, w, nw, cursor, lo) + d.cell(right, v, sub, w, nw, cursor, lo)
}

func (m *Model) status() string {
	left := statusText(m.cfg.Theme, m.msg, m.help())
	row := 0
	if m.cursor < len(m.view) {
		row = m.view[m.cursor].row + 1
	}
	right := fmt.Sprintf(" row %d/%d · anchors %d", row, len(m.rows), len(m.anchors))
	return statusLine(m.cfg.Theme, m.width, left, right)
}

// fold renders displayed row v, a fold of unchanged rows, across both panes
// of a view width columns wide.
func (d *fileDiff) fold(v int, cursor bool, width int) string {
	label := fmt.Sprintf(" ⋯ %d unchanged lines ⋯ ", d.view[v].hidden)
	if cursor {
		label = d.cfg.Theme.Cursor.Render(label)
	}
	side := max(width-ansi.StringWidth(label), 0)
	line := d.cfg.Theme.Decoration.Render(strings.Repeat("─", side/2)) + label +
		d.cfg.Theme.Decoration.Render(strings.Repeat("─", side-side/2))
	return ansi.Truncate(line, width, "")
}

// cell renders piece of side s of displayed row v: the gutter with the line
// number, and the line content; further pieces of a wrapped line have no
// number, and a side without that piece is empty. On the cursor row both
// sides are bold, and the line number of the active side is in the cursor
// style.
func (d *fileDiff) cell(s side, v, piece, width, numWidth int, cursor bool, lo layout) string {
	t := d.cfg.Theme
	row := d.rows[d.view[v].row]
	line := d.lineAt(s, v)
	changed := row.Kind != diff.Equal && line >= 0
	sepStyle := t.NumLeft
	if s == right {
		sepStyle = t.NumRight
	}

	contentWidth := max(width-numWidth-4, 0)
	num := strings.Repeat(" ", numWidth)
	if line >= 0 && piece >= d.pieces(s, line, lo.shape) {
		// The other side's line takes more pieces.
		return sepStyle.Render("│") + strings.Repeat(" ", 1+numWidth) + sepStyle.Render("│") +
			strings.Repeat(" ", 1+contentWidth)
	}
	if line >= 0 && piece == 0 {
		num = fmt.Sprintf("%*d", numWidth, line+1)
	}
	pending := piece == 0 && d.pending != nil && d.pending.side == s && d.pending.line == line && line >= 0
	_, moved := d.movedTo(s, line)
	gutter := d.gutter(row, s, changed, pending, moved, piece > 0 && line >= 0, num, sepStyle, cursor,
		cursor && s == lo.active)

	if line < 0 {
		return gutter + t.Filler.Render(strings.Repeat("╱", contentWidth))
	}

	return gutter + d.content(s, row, line, contentWidth, cursor, moved, startCol(piece, contentWidth, lo), lo.wrap == 0)
}

// gutter renders what precedes a line of side s in row: a bar, the mark, the
// line numbers nums, a bar, both bars in sepStyle, and the sign. The mark is
// ● when pending holds and ◆ on anchors; the sign is ↪ on further pieces of
// a wrapped line (cont), in the wrap marker style, and - or + on changed
// lines, in the moved style for moved ones; changed lines also have their
// side's number style. On the cursor line marks, signs and numbers are bold,
// with active the numbers are in the cursor style.
func (d *fileDiff) gutter(row diff.Row, s side, changed, pending, moved, cont bool, nums string,
	sepStyle lipgloss.Style, cursor, active bool) string {
	t := d.cfg.Theme
	lineStyle, _, _, _ := d.sideStyles(s)
	if moved {
		lineStyle = d.movedStyle(s)
	}
	numStyle := t.NumZero
	mark, sign := " ", " "
	switch {
	case cont: // no mark: further pieces have no number either
	case pending:
		mark = t.Pending.Render("●")
	case row.Anchored:
		mark = boldIf(t.Anchor, cursor).Render("◆")
	}
	switch {
	case cont:
		sign = boldIf(t.WrapMarker, cursor).Render("↪")
	case changed && s == left:
		sign = boldIf(lineStyle, cursor).Render("-")
	case changed:
		sign = boldIf(lineStyle, cursor).Render("+")
	}
	if changed {
		numStyle = t.NumPlus
		if s == left {
			numStyle = t.NumMinus
		}
	}
	if active {
		// The cursor's line numbers stand out in the cursor style, in
		// their own color.
		numStyle = t.Cursor.Inherit(numStyle)
	} else {
		numStyle = boldIf(numStyle, cursor)
	}
	return sepStyle.Render("│") + mark + numStyle.Render(nums) + sepStyle.Render("│") + sign
}

// boldIf returns style, bold when on is set.
func boldIf(style lipgloss.Style, on bool) lipgloss.Style {
	if on {
		return style.Bold(true)
	}
	return style
}

// movedStyle returns the style of moved lines of side s.
func (d *fileDiff) movedStyle(s side) lipgloss.Style {
	if s == left {
		return d.cfg.Theme.MinusMoved
	}
	return d.cfg.Theme.PlusMoved
}

// content renders line of side s, shown in row, width columns wide with
// changes emphasized, from column from on. On the cursor line unchanged text
// is bold; a moved line is drawn in the moved style, without emphasis.
// With cut, a line that goes on past the pane ends in Extends, one with
// text scrolled out to the left starts with Precedes.
func (d *fileDiff) content(s side, row diff.Row, line, width int, cursor, moved bool, from int, cut bool) string {
	t := d.cfg.Theme
	lineStyle, nonEmph, emph, indent := d.sideStyles(s)
	st := lineStyles{base: boldIf(t.Zero, cursor && row.Kind == diff.Equal), ws: t.Whitespace,
		showAll: d.cfg.Settings.ShowWhitespaces, plainSpace: !d.cfg.Settings.MarkWhitespace,
		trailing: t.TrailingWhitespace, showTrailing: d.cfg.Settings.ShowTrailing, lineEnd: d.cfg.Settings.LineEnd,
		cr: d.cfg.Settings.CREnd, showCR: d.cfg.Settings.ShowCR, hidden: t.HiddenChar}
	var spans []diff.Span
	switch {
	case row.Kind == diff.Equal:
	case moved:
		st.base = d.movedStyle(s)
	case row.Kind == diff.Change:
		st.base, st.emph, st.space, spans = nonEmph, emph, indent, emphOf(row, s)
		if !d.cfg.Settings.BoldChanges {
			st.emph, st.space = st.emph.UnsetBold(), st.space.UnsetBold()
		}
	default:
		st.base = lineStyle
	}
	if m := matcherOf(d.cfg.Search.Query); m != nil && (d.cfg.Settings.SearchAll || row.Kind != diff.Equal) {
		st.matches, st.match = m.spans(d.lines[s][line]), t.Search
	}
	// The pieces of a wrapped line are drawn from one highlight of it.
	key := highlightKey{line, d.aligns, cursor, moved, d.cfg.Search.Query, d.cfg.Settings, t}
	if c := &d.highlighted[s]; !c.ok || c.key != key {
		*c = highlightCache{key, highlight(d.lines[s][line], spans, st), true}
	}
	content := d.highlighted[s].out
	plain := lipgloss.NewStyle()
	if !cut { // a piece of a wrapped line
		return pad(ansi.Cut(content, from, from+width), width, plain)
	}
	// fit returns s as w columns.
	fit := func(s string, w int) string { return pad(ansi.Truncate(s, w, ""), w, plain) }
	full := ansi.StringWidth(content)
	content = fit(cutLeft(content, from), width)
	// The edges say where the line goes on.
	if width >= 2 {
		mark := d.cfg.Theme.Overflow
		if full > from+width {
			content = fit(content, width-1) + mark.Render(d.cfg.Settings.Extends)
		}
		if from > 0 && full > 0 {
			content = mark.Render(d.cfg.Settings.Precedes) + fit(cutLeft(content, 1), width-1)
		}
	}
	return content
}

// cutLeft returns s without its first n columns. A wide character they cut
// through leaves a space, so that what follows keeps its column.
func cutLeft(s string, n int) string {
	if n <= 0 {
		return s
	}
	r := ansi.TruncateLeft(s, n, "")
	if ansi.StringWidth(r) > ansi.StringWidth(s)-n { // kept the cut character
		r = " " + ansi.TruncateLeft(s, n+1, "")
	}
	return r
}

// highlight renders text with the given spans emphasized. Changed
// whitespace (inside spans) is drawn visibly, · for a space and → for a tab,
// when it is indentation, holds a tab or is more than one space: a single
// space between changed words is no news. With showTrailing whitespace at
// the end of the line is drawn visibly in its style, with showAll all
// whitespace, and the end of the line as lineEnd. The \r of a \r\n line
// end is drawn as cr where it changed or with showCR, else not at all. Tabs
// are expanded and hidden characters drawn as ☠ in their style.
func highlight(text string, spans []diff.Span, st lineStyles) string {
	cr, changedCR := strings.HasSuffix(text, "\r"), false
	if cr {
		text = text[:len(text)-1]
	}
	var b strings.Builder
	col := 0
	pBase, pEmph, pSpace, pWS := newPainter(st.base), newPainter(st.emph), newPainter(st.space), newPainter(st.ws)
	pTrailing, pHidden, pMatch := newPainter(st.trailing), newPainter(st.hidden), newPainter(st.match)
	render := func(part string, p painter, visible bool) {
		if part == "" {
			return
		}
		var e strings.Builder
		expand(&e, part, &col, visible)
		b.WriteString(p.paint(e.String()))
	}
	lead := true // in the line's indentation
	at := 0      // where the part rendered next starts in text
	trailFrom := len(strings.TrimRightFunc(text, unicode.IsSpace))
	// class is the kind of run r belongs to: hidden, whitespace or text.
	class := func(r rune) int {
		switch {
		case hiddenChar(r):
			return 2
		case unicode.IsSpace(r):
			return 1
		}
		return 0
	}
	// renderPart splits a part into hidden, whitespace and other runs.
	renderPart := func(part string, emph, match bool) {
		for part != "" {
			r, _ := utf8.DecodeRuneInString(part)
			c := class(r)
			ws := c == 1
			i := strings.IndexFunc(part, func(r rune) bool { return class(r) != c })
			if i < 0 {
				i = len(part)
			}
			run := part[:i]
			if c == 2 {
				lead = false
				at += i
				render(run, pHidden, false)
				part = part[i:]
				continue
			}
			mark := lead || strings.ContainsRune(run, '\t') || utf8.RuneCountInString(run) > 1
			lead = lead && ws
			trailing := ws && at >= trailFrom
			at += i
			switch {
			case match: // found by /: over all styles, whitespace visible as around
				render(run, pMatch, ws && (st.showAll || trailing && st.showTrailing))
			case trailing && st.showTrailing:
				render(run, pTrailing, true)
			case ws && emph && !st.plainSpace && mark:
				render(part[:i], pSpace, true)
			case ws && st.showAll:
				render(part[:i], pWS, true)
			case ws:
				render(part[:i], pBase, false)
			case emph:
				render(part[:i], pEmph, false)
			default:
				render(part[:i], pBase, false)
			}
			part = part[i:]
		}
	}
	// The line is drawn in parts between the edges of the spans and the
	// matches, each emphasized or found by / or not.
	cuts := []int{0, len(text)}
	for _, sp := range spans {
		changedCR = changedCR || sp.End > len(text)
		cuts = append(cuts, min(sp.Start, len(text)), min(sp.End, len(text)))
	}
	for _, sp := range st.matches {
		cuts = append(cuts, min(sp.Start, len(text)), min(sp.End, len(text)))
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	in := func(spans []diff.Span, at int) bool {
		return slices.ContainsFunc(spans, func(sp diff.Span) bool { return at >= sp.Start && at < sp.End })
	}
	for i := 0; i+1 < len(cuts); i++ {
		from, to := cuts[i], cuts[i+1]
		renderPart(text[from:to], in(spans, from), in(st.matches, from))
	}
	if cr && (changedCR || st.showCR) {
		style := st.ws
		if changedCR {
			style = st.emph
		}
		b.WriteString(style.Render(st.cr))
	}
	if st.showAll {
		b.WriteString(st.ws.Render(st.lineEnd))
	}
	return b.String()
}

// expand writes text, which starts at column *col, to b as drawn: tabs
// expanded to the next tab stop and hidden characters as ☠; with visible
// set, spaces are drawn as · and tabs start with →. *col advances.
func expand(b *strings.Builder, text string, col *int, visible bool) {
	for _, r := range text {
		switch {
		case r == '\t':
			n := tabWidth - *col%tabWidth
			if visible {
				b.WriteString("→" + strings.Repeat(" ", n-1))
			} else {
				b.WriteString(strings.Repeat(" ", n))
			}
			*col += n
		case r == ' ' && visible:
			b.WriteRune('·')
			*col++
		case hiddenChar(r):
			b.WriteString(hiddenSymbol)
			*col++
		default:
			b.WriteRune(r)
			*col += ansi.StringWidth(string(r))
		}
	}
}

// printable returns line as drawn, see expand, so that its width is known:
// text from files, git or marks may hold tabs and control characters.
func printable(line string) string {
	var b strings.Builder
	col := 0
	expand(&b, line, &col, false)
	return b.String()
}
