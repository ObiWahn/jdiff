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

	"jdiff/internal/config"
)

// Doc shows the pages of jdiff's documentation, markdown rendered for the
// terminal: jdiff --readme. A cursor line moves through a page; enter follows
// the link it is on, the first unless h l or tab chose another, to another
// page or heading, backspace goes back.
type Doc struct {
	cfg      *config.Config
	docs     fs.FS
	paths    []string // the pages in docs, in the order of n and p
	page     int
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

// docPlace is where a link was followed from.
type docPlace struct{ page, top, cursor int }

// NewDoc returns the documentation in docs, the pages paths, on page page.
func NewDoc(cfg *config.Config, docs fs.FS, paths []string, page int) *Doc {
	return &Doc{cfg: cfg, docs: docs, paths: paths, page: page, link: -1}
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
	src, err := fs.ReadFile(d.docs, d.paths[d.page])
	if err != nil {
		d.msg = err.Error()
	}
	d.rendered, d.forWidth = renderMarkdown(src, d.textWidth()), d.textWidth()
	d.scrollTo(d.top)
	d.moveTo(d.cursor)
}

// show shows page page from line top, the cursor on line cursor.
func (d *Doc) show(page, top, cursor int) {
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
// view; the first link on the line is selected, and the status bar says
// where it goes.
func (d *Doc) moveTo(i int) {
	n := len(d.rendered.lines)
	d.cursor = max(min(i, n-1), 0)
	d.top = scrollWindow(d.cursor, d.top, d.bodyHeight(), n)
	d.link = -1
	if d.cursor < n {
		for _, s := range d.rendered.lines[d.cursor].segs {
			if s.link >= 0 {
				d.link = s.link
				d.msg = d.rendered.links[s.link].target
				break
			}
		}
	}
}

func (d *Doc) key(k string) tea.Cmd {
	d.msg = ""
	h := keyFile.key(docView, k)
	body := d.bodyHeight()
	if delta, paging, ok := motion(h, max(body-1, 1), len(d.rendered.lines)); ok {
		if paging {
			d.scrollTo(d.top + delta)
		}
		d.moveTo(d.cursor + delta)
		return nil
	}
	switch h.action {
	case "quit":
		return tea.Quit
	case "half_page":
		delta := direction(h.i == 1) * (body / 2)
		d.scrollTo(d.top + delta)
		d.moveTo(d.cursor + delta)
	case "link":
		d.nextLink(direction(h.i == 0))
	case "line_link":
		d.lineLink(direction(h.i == 1))
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
	case "next_page":
		next := d.page + direction(h.i == 0)
		if next < 0 || next >= len(d.paths) {
			d.msg = ends(h.i == 0, "last page", "first page")
			break
		}
		d.show(next, 0, 0)
	case "search":
		return askSearch
	case "match":
		d.findText(direction(h.i == 0))
	}
	return nil
}

// nextLink moves the cursor to the next link in direction dir and selects
// it: the one after the selected link, also on the same line, or the first
// below or last above the cursor.
func (d *Doc) nextLink(dir int) {
	links := d.rendered.links
	next := d.link + dir
	if d.link < 0 {
		next = slices.IndexFunc(links, func(l pageLink) bool { return l.line > d.cursor })
		if dir < 0 {
			next = len(links) - 1
			if after := slices.IndexFunc(links, func(l pageLink) bool { return l.line > d.cursor }); after >= 0 {
				next = after - 1
			}
		}
	}
	if next < 0 || next >= len(links) {
		d.msg = ends(dir > 0, "no link further down", "no link further up")
		return
	}
	d.moveTo(links[next].line)
	d.link, d.msg = next, links[next].target
}

// lineLink selects the next link on the cursor's line in direction dir,
// right or left.
func (d *Doc) lineLink(dir int) {
	var on []int
	for _, s := range d.rendered.lines[min(d.cursor, len(d.rendered.lines)-1)].segs {
		if s.link >= 0 && !slices.Contains(on, s.link) {
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
		d.link, d.msg = on[i], d.rendered.links[on[i]].target
	}
}

// follow follows the selected link: to its page and heading, which shows
// at the top, or for a web address it shows where it goes.
func (d *Doc) follow() {
	if d.link < 0 {
		d.msg = "no link on this line: " + keyFile.keyText("{link}") + " go to the next, previous"
		return
	}
	target := d.rendered.links[d.link].target
	file, anchor, _ := strings.Cut(target, "#")
	if strings.Contains(file, ":") { // https:, mailto:
		d.msg = target
		return
	}
	page := d.page
	if file != "" {
		page = slices.Index(d.paths, path.Join(path.Dir(d.paths[d.page]), file))
		if page < 0 {
			d.msg = "no page " + file
			return
		}
	}
	d.back = append(d.back, docPlace{d.page, d.top, d.cursor})
	d.show(page, 0, 0)
	if anchor == "" {
		return
	}
	if line, ok := d.rendered.anchors[anchor]; ok {
		d.show(page, line, line)
	} else {
		d.msg = "no heading #" + anchor + " in " + d.paths[page]
	}
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
	where := fmt.Sprintf("page %d/%d", d.page+1, len(d.paths))
	if n := len(lines); n > d.bodyHeight() {
		where += fmt.Sprintf(" · %d%%", 100*min(d.top+d.bodyHeight(), n)/n)
	}
	b.WriteString(statusLine(t, d.width, t.Title.Render(" "+printable(d.paths[d.page])), where+" ") + "\n")
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
	return b.String()
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
		if s.link >= 0 && s.link == d.link {
			st = d.cfg.Theme.Cursor
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
