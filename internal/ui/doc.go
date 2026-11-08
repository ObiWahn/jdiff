package ui

import (
	"cmp"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/keys"
)

// Doc shows the pages of jdiff's documentation, markdown rendered for the
// terminal: jdiff --readme. A cursor line moves through a page; enter follows
// the link it is on, the first unless h l or tab chose another, to another
// page or heading, backspace goes back. Only links that lead to a file of
// docs, and a heading there, or to the web are links. A file that is no
// markdown, reached by a link, shows as it is.
type Doc struct {
	cfg      *config.Config
	docs     fs.FS
	order    []string                  // the pages in docs, in the order of n and p
	page     string                    // the file of docs shown
	anchors  map[string]map[string]int // the headings of other pages, see anchorsOf
	rendered docPage
	forWidth int // the width rendered is for, 0 before the first
	top      int // the first line shown
	cursor   int // the line of the cursor
	link     int // the selected link, on the cursor's line; -1 for none
	back     []docPlace
	msg      string
	width    int
	height   int
}

// docPlace is where a link was followed from, and the link.
type docPlace struct {
	page              string
	top, cursor, link int
}

// NewDoc returns the documentation in docs, whose pages are order, on page
// order[page].
func NewDoc(cfg *config.Config, docs fs.FS, order []string, page int) *Doc {
	return &Doc{cfg: cfg, docs: docs, order: order, page: order[page], anchors: map[string]map[string]int{}, link: -1}
}

func (d *Doc) Init() tea.Cmd { return nil }

func (d *Doc) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width, d.height = msg.Width, msg.Height
		d.render()
	case tea.KeyPressMsg:
		return d, d.key(msg.String())
	case findMsg:
		d.findText(int(msg))
	}
	return d, nil
}

// textWidth is the width pages are rendered for: the screen but a column
// on each side, the left one for the cursor.
func (d *Doc) textWidth() int { return max(d.width-2, 1) }

// bodyHeight is the number of lines shown, between the title and the status
// bar.
func (d *Doc) bodyHeight() int { return max(d.height-2, 1) }

// render renders the page for the width of the screen, unless it is already.
func (d *Doc) render() {
	if d.forWidth == d.textWidth() {
		return
	}
	src, err := fs.ReadFile(d.docs, d.page)
	if err != nil {
		d.msg = err.Error()
	}
	d.forWidth = d.textWidth()
	if path.Ext(d.page) == ".md" {
		d.rendered = renderMarkdown(src, d.forWidth)
	} else {
		d.rendered = renderText(src, d.forWidth)
	}
	for i, l := range d.rendered.links {
		_, _, ok := d.resolve(l.target)
		d.rendered.links[i].broken = !ok
	}
	d.scrollTo(d.top)
	d.moveTo(d.cursor)
}

// show shows page from line top, the cursor on line cursor.
func (d *Doc) show(page string, top, cursor int) {
	d.page, d.forWidth = page, 0
	d.render()
	d.scrollTo(top)
	d.moveTo(cursor)
}

// scrollTo scrolls to line top, kept where lines fill the screen.
func (d *Doc) scrollTo(top int) {
	d.top = max(min(top, len(d.rendered.lines)-d.bodyHeight()), 0)
}

// moveTo moves the cursor to line i, kept in the page, and scrolls it into
// view; the first link on the line that resolves is selected.
func (d *Doc) moveTo(i int) {
	n := len(d.rendered.lines)
	d.cursor = max(min(i, n-1), 0)
	d.top = scrollWindow(d.cursor, d.top, d.bodyHeight(), n)
	d.link = -1
	if d.cursor < n {
		for _, s := range d.rendered.lines[d.cursor].segs {
			if d.live(s.link) {
				d.link = s.link
				break
			}
		}
	}
}

func (d *Doc) key(k string) tea.Cmd {
	d.msg = ""
	h := d.cfg.Keys.Key(keys.DocView, k)
	body := d.bodyHeight()
	if delta, paging, ok := motion(h, max(body-1, 1), len(d.rendered.lines)); ok {
		if paging {
			d.scrollTo(d.top + delta)
		}
		d.moveTo(d.cursor + delta)
		return nil
	}
	switch h.Action {
	case "quit":
		return tea.Quit
	case "half_page":
		delta := direction(h.Index == 1) * (body / 2)
		d.scrollTo(d.top + delta)
		d.moveTo(d.cursor + delta)
	case "link":
		d.nextLink(direction(h.Index == 0))
	case "line_link":
		d.lineLink(direction(h.Index == 1))
	case "follow":
		d.follow()
	case "back":
		if len(d.back) == 0 {
			d.msg = "no link followed to go back from"
			break
		}
		p := d.back[len(d.back)-1]
		d.back = d.back[:len(d.back)-1]
		d.show(p.page, p.top, p.cursor)
		d.link = p.link
	case "next_page":
		i := slices.Index(d.order, d.page)
		next := i + direction(h.Index == 0)
		switch {
		case i < 0:
			d.msg = d.page + " is no page of " + d.cfg.Keys.Text("{next_page}") + ": " + d.cfg.Keys.Text("{back}") + " goes back"
		case next < 0 || next >= len(d.order):
			d.msg = ends(h.Index == 0, "last page", "first page")
		default:
			d.show(d.order[next], 0, 0)
		}
	case "search":
		return askSearch
	case "match":
		d.findText(direction(h.Index == 0))
	}
	return nil
}

// live reports whether link i of the page is one: it resolves.
func (d *Doc) live(i int) bool { return i >= 0 && !d.rendered.links[i].broken }

// nextLink moves the cursor to the next link in direction dir and selects
// it: the one after the selected link, also on the same line, or the first
// below or last above the cursor.
func (d *Doc) nextLink(dir int) {
	links := d.rendered.links
	next := -1
	for i := range links {
		j := i
		if dir < 0 {
			j = len(links) - 1 - i
		}
		after := d.link >= 0 && (j-d.link)*dir > 0 || d.link < 0 && (links[j].line-d.cursor)*dir > 0
		if after && d.live(j) {
			next = j
			break
		}
	}
	if next < 0 {
		d.msg = ends(dir > 0, "no link further down", "no link further up")
		return
	}
	d.moveTo(links[next].line)
	d.link = next
}

// lineLink selects the next link on the cursor's line in direction dir,
// right or left.
func (d *Doc) lineLink(dir int) {
	var on []int
	for _, s := range d.rendered.lines[min(d.cursor, len(d.rendered.lines)-1)].segs {
		if d.live(s.link) && !slices.Contains(on, s.link) {
			on = append(on, s.link)
		}
	}
	i := slices.Index(on, d.link) + dir
	switch {
	case len(on) == 0:
		d.msg = "no link on this line"
	case i < 0 || i >= len(on):
		d.msg = ends(dir > 0, "no link further right", "no link further left")
	default:
		d.link = on[i]
	}
}

// follow follows the selected link to its page and heading, which shows at
// the top; for a web address the box of frame shows it already.
func (d *Doc) follow() {
	if d.link < 0 {
		d.msg = "no link on this line: " + d.cfg.Keys.Text("{link}") + " go to the next, previous"
		return
	}
	target := d.rendered.links[d.link].target
	page, anchor, _ := d.resolve(target)
	if page == "" {
		return
	}
	d.back = append(d.back, docPlace{d.page, d.top, d.cursor, d.link})
	d.show(page, 0, 0)
	if line := d.rendered.anchors[anchor]; anchor != "" {
		d.show(page, line, line)
	}
}

// resolve returns the file of docs and the heading that target, a link of
// the page shown, goes to, and whether both are there; for a web address
// the file is "".
func (d *Doc) resolve(target string) (page, anchor string, ok bool) {
	file, anchor, _ := strings.Cut(target, "#")
	if strings.Contains(file, ":") { // https:, mailto:
		return "", "", true
	}
	page = d.page
	if file != "" {
		page = path.Join(path.Dir(d.page), file)
		if _, err := fs.Stat(d.docs, page); err != nil {
			return page, anchor, false
		}
	}
	if anchor == "" {
		return page, "", true
	}
	_, ok = d.anchorsOf(page)[anchor]
	return page, anchor, ok
}

// anchorsOf returns the headings of page by the names links give them, with
// their lines for the width of the page shown when it is that page.
func (d *Doc) anchorsOf(page string) map[string]int {
	if page == d.page {
		return d.rendered.anchors
	}
	a, ok := d.anchors[page]
	if !ok {
		if src, err := fs.ReadFile(d.docs, page); err == nil && path.Ext(page) == ".md" {
			a = renderMarkdown(src, 80).anchors
		}
		d.anchors[page] = a
	}
	return a
}

// findText moves the cursor to the next line, in direction dir, that holds
// what config.Search searched for, past the cursor's own line.
func (d *Doc) findText(dir int) {
	q := d.cfg.Search.Query
	m := matcherOf(q)
	if m == nil {
		d.msg = nothingSearched
		return
	}
	var hits []int
	for i, l := range d.rendered.lines {
		if len(m.spans(l.plain())) > 0 {
			hits = append(hits, i)
		}
	}
	if len(hits) == 0 {
		d.msg = "no match for " + q.Text + " in this page"
		return
	}
	i, ok := nextHit(len(hits), dir, func(i int) int { return cmp.Compare(hits[i], d.cursor) })
	if !ok {
		d.msg = noFurther(q, dir, "")
		return
	}
	d.moveTo(hits[i])
	d.msg = searchMsg(q, i, len(hits))
}

func (d *Doc) View() tea.View { return tea.NewView(d.frame()) }

func (d *Doc) frame() string {
	if d.width == 0 {
		return ""
	}
	t := d.cfg.Theme
	lines := d.rendered.lines
	var b strings.Builder
	var where []string
	if i := slices.Index(d.order, d.page); i >= 0 {
		where = append(where, fmt.Sprintf("page %d/%d", i+1, len(d.order)))
	}
	if n := len(lines); n > d.bodyHeight() {
		where = append(where, fmt.Sprintf("%d%%", 100*min(d.top+d.bodyHeight(), n)/n))
	}
	b.WriteString(statusLine(t, d.width, t.Title.Render(" "+printable(d.page)), strings.Join(where, " · ")+" ") + "\n")
	m := matcherOf(d.cfg.Search.Query)
	for i := d.top; i < d.top+d.bodyHeight(); i++ {
		if i < len(lines) {
			margin := " "
			if i == d.cursor {
				margin = t.Cursor.Render(margin)
			}
			b.WriteString(margin + d.drawLine(lines[i], m))
		}
		b.WriteString("\n")
	}
	keys := statusKeys(d.cfg, barKeys{enter: "follow", back: "back", text: true})
	b.WriteString(statusLine(t, d.width, pageStatus(d.cfg, d.msg, keys), ""))
	return d.withWebAddress(b.String())
}

// withWebAddress returns frame with a box of the web address of the selected
// link below its line, or above it near the bottom, while the cursor is on
// it; links into the documentation need none.
func (d *Doc) withWebAddress(frame string) string {
	if d.link < 0 {
		return frame
	}
	target := d.rendered.links[d.link].target
	if page, _, _ := d.resolve(target); page != "" {
		return frame
	}
	row := 1 + d.cursor - d.top // in frame, below the title
	y := row + 1
	if y+minBoxHeight > 1+d.bodyHeight() {
		y = row - minBoxHeight
	}
	if y < 1 {
		return frame // no room
	}
	x := 1 // the cursor's column, then the line up to the link
	for _, s := range d.rendered.lines[d.cursor].segs {
		if s.link == d.link {
			break
		}
		x += ansi.StringWidth(s.text)
	}
	address := printable(target)
	w := min(ansi.StringWidth(address)+4, d.width)
	return overlayAt(frame, box(d.cfg.Theme, w, minBoxHeight, "", []string{address}), max(min(x, d.width-w), 0), y, w, d.width)
}

// drawLine draws l in the styles of its pieces, the selected link in the
// cursor style and what m finds in the search style.
func (d *Doc) drawLine(l docLine, m *matcher) string {
	var found []bool
	if m != nil {
		plain := l.plain()
		found = make([]bool, len(plain))
		for _, s := range m.spans(plain) {
			for j := s.Start; j < s.End; j++ {
				found[j] = true
			}
		}
	}
	var b strings.Builder
	off := 0
	for _, s := range l.segs {
		st := d.style(s.style)
		switch {
		case s.link >= 0 && s.link == d.link:
			st = d.cfg.Theme.Cursor
		case s.link >= 0 && !d.live(s.link):
			st = d.style(docPlain) // leads nowhere
		}
		// Runs of the piece in or out of a match.
		for start := 0; start < len(s.text); {
			end := start + 1
			in := found != nil && found[off+start]
			for end < len(s.text) && (found != nil && found[off+end]) == in {
				end++
			}
			if in {
				b.WriteString(d.cfg.Theme.Search.Render(s.text[start:end]))
			} else {
				b.WriteString(st.Render(s.text[start:end]))
			}
			start = end
		}
		off += len(s.text)
	}
	return b.String()
}

// style returns the style of the theme for a piece of a page.
func (d *Doc) style(s docStyle) lipgloss.Style {
	t := d.cfg.Theme
	switch s {
	case docBold:
		return lipgloss.NewStyle().Bold(true)
	case docItalic:
		return lipgloss.NewStyle().Italic(true)
	case docCode:
		return t.DocCode
	case docHeading:
		return t.DocHeading
	case docLink:
		return t.DocLink
	case docDecoration:
		return t.Decoration
	case docOverflow:
		return t.Overflow
	}
	return lipgloss.NewStyle()
}
