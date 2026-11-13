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
	"jdiff/internal/popup"
)

// Readme shows the pages of jdiff's documentation, markdown rendered for the
// terminal: jdiff --readme (keys of doc_view). A cursor line moves through a
// page; enter follows the link it is on, the first unless h l or tab chose
// another, to another page or heading, backspace goes back. Only links that lead to a file of
// docs, and a heading there, or to the web are links. A file that is no
// markdown, reached by a link, shows as it is.
type Readme struct {
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

// NewReadme returns the documentation in docs, whose pages are order, on page
// order[page].
func NewReadme(cfg *config.Config, docs fs.FS, order []string, page int) *Readme {
	return &Readme{cfg: cfg, docs: docs, order: order, page: order[page], anchors: map[string]map[string]int{}, link: -1}
}

func (r *Readme) Init() tea.Cmd { return nil }

func (r *Readme) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.width, r.height = msg.Width, msg.Height
		r.render()
	case tea.KeyPressMsg:
		return r, r.key(msg.String())
	case findMsg:
		r.findText(int(msg))
	}
	return r, nil
}

// textWidth is the width pages are rendered for: the screen but a column
// on each side, the left one for the cursor.
func (r *Readme) textWidth() int { return max(r.width-2, 1) }

// bodyHeight is the number of lines shown, between the title and the status
// bar.
func (r *Readme) bodyHeight() int { return max(r.height-2, 1) }

// render renders the page for the width of the screen, unless it is already.
func (r *Readme) render() {
	if r.forWidth == r.textWidth() {
		return
	}
	src, err := fs.ReadFile(r.docs, r.page)
	if err != nil {
		r.msg = err.Error()
	}
	r.forWidth = r.textWidth()
	if path.Ext(r.page) == ".md" {
		r.rendered = renderMarkdown(src, r.forWidth)
	} else {
		r.rendered = renderText(src, r.forWidth)
	}
	for i, l := range r.rendered.links {
		_, _, ok := r.resolve(l.target)
		r.rendered.links[i].broken = !ok
	}
	r.scrollTo(r.top)
	r.moveTo(r.cursor)
}

// show shows page from line top, the cursor on line cursor.
func (r *Readme) show(page string, top, cursor int) {
	r.page, r.forWidth = page, 0
	r.render()
	r.scrollTo(top)
	r.moveTo(cursor)
}

// scrollTo scrolls to line top, kept where lines fill the screen.
func (r *Readme) scrollTo(top int) {
	r.top = max(min(top, len(r.rendered.lines)-r.bodyHeight()), 0)
}

// moveTo moves the cursor to line i, kept in the page, and scrolls it into
// view; the first link on the line that resolves is selected.
func (r *Readme) moveTo(i int) {
	n := len(r.rendered.lines)
	r.cursor = max(min(i, n-1), 0)
	r.top = scrollWindow(r.cursor, r.top, r.bodyHeight(), n)
	r.link = -1
	if r.cursor < n {
		for _, s := range r.rendered.lines[r.cursor].segs {
			if r.live(s.link) {
				r.link = s.link
				break
			}
		}
	}
}

func (r *Readme) key(k string) tea.Cmd {
	r.msg = ""
	h := r.cfg.Keys.Key(keys.DocView, k)
	page := max(r.bodyHeight()-1, 1)
	if delta, paging, ok := motion(h, page, len(r.rendered.lines)); ok {
		if paging {
			r.scrollTo(r.top + delta)
		}
		r.moveTo(r.cursor + delta)
		return nil
	}
	switch h.Action {
	case "quit":
		return tea.Quit
	case "half_page":
		delta := halfPage(h, page)
		r.scrollTo(r.top + delta)
		r.moveTo(r.cursor + delta)
	case "link":
		r.nextLink(direction(h.I == 0))
	case "line_link":
		r.lineLink(direction(h.I == 1))
	case "follow":
		r.follow()
	case "back":
		if len(r.back) == 0 {
			r.msg = "no link followed to go back from"
			break
		}
		p := r.back[len(r.back)-1]
		r.back = r.back[:len(r.back)-1]
		r.show(p.page, p.top, p.cursor)
		r.link = p.link
	case "next_page":
		i := slices.Index(r.order, r.page)
		next := i + direction(h.I == 0)
		switch {
		case i < 0:
			r.msg = r.page + " is no page of " + r.cfg.Keys.Text("{next_page}") + ": " + r.cfg.Keys.Text("{back}") + " goes back"
		case next < 0 || next >= len(r.order):
			r.msg = ends(h.I == 0, "last page", "first page")
		default:
			r.show(r.order[next], 0, 0)
		}
	case "search":
		return askSearch
	case "match":
		r.findText(direction(h.I == 0))
	}
	return nil
}

// live reports whether link i of the page is one: it resolves.
func (r *Readme) live(i int) bool { return i >= 0 && !r.rendered.links[i].broken }

// nextLink moves the cursor to the next link in direction dir and selects
// it: the one after the selected link, also on the same line, or the first
// below or last above the cursor.
func (r *Readme) nextLink(dir int) {
	links := r.rendered.links
	next := -1
	for i := range links {
		j := i
		if dir < 0 {
			j = len(links) - 1 - i
		}
		after := r.link >= 0 && (j-r.link)*dir > 0 || r.link < 0 && (links[j].line-r.cursor)*dir > 0
		if after && r.live(j) {
			next = j
			break
		}
	}
	if next < 0 {
		r.msg = ends(dir > 0, "no link further down", "no link further up")
		return
	}
	r.moveTo(links[next].line)
	r.link = next
}

// lineLink selects the next link on the cursor's line in direction dir,
// right or left.
func (r *Readme) lineLink(dir int) {
	var on []int
	for _, s := range r.rendered.lines[min(r.cursor, len(r.rendered.lines)-1)].segs {
		if r.live(s.link) && !slices.Contains(on, s.link) {
			on = append(on, s.link)
		}
	}
	i := slices.Index(on, r.link) + dir
	switch {
	case len(on) == 0:
		r.msg = "no link on this line"
	case i < 0 || i >= len(on):
		r.msg = ends(dir > 0, "no link further right", "no link further left")
	default:
		r.link = on[i]
	}
}

// follow follows the selected link to its page and heading, which shows at
// the top; for a web address the box of frame shows it already.
func (r *Readme) follow() {
	if r.link < 0 {
		r.msg = "no link on this line: " + r.cfg.Keys.Text("{link}") + " go to the next, previous"
		return
	}
	target := r.rendered.links[r.link].target
	page, anchor, _ := r.resolve(target)
	if page == "" {
		return
	}
	r.back = append(r.back, docPlace{r.page, r.top, r.cursor, r.link})
	r.show(page, 0, 0)
	if line := r.rendered.anchors[anchor]; anchor != "" {
		r.show(page, line, line)
	}
}

// resolve returns the file of docs and the heading that target, a link of
// the page shown, goes to, and whether both are there; for a web address
// the file is "".
func (r *Readme) resolve(target string) (page, anchor string, ok bool) {
	file, anchor, _ := strings.Cut(target, "#")
	if strings.Contains(file, ":") { // https:, mailto:
		return "", "", true
	}
	page = r.page
	if file != "" {
		page = path.Join(path.Dir(r.page), file)
		if _, err := fs.Stat(r.docs, page); err != nil {
			return page, anchor, false
		}
	}
	if anchor == "" {
		return page, "", true
	}
	_, ok = r.anchorsOf(page)[anchor]
	return page, anchor, ok
}

// anchorsOf returns the headings of page by the names links give them, with
// their lines for the width of the page shown when it is that page.
func (r *Readme) anchorsOf(page string) map[string]int {
	if page == r.page {
		return r.rendered.anchors
	}
	a, ok := r.anchors[page]
	if !ok {
		if src, err := fs.ReadFile(r.docs, page); err == nil && path.Ext(page) == ".md" {
			a = renderMarkdown(src, 80).anchors
		}
		r.anchors[page] = a
	}
	return a
}

// findText moves the cursor to the next line, in direction dir, that holds
// what config.Search searched for, past the cursor's own line.
func (r *Readme) findText(dir int) {
	q := r.cfg.Search.Query
	m := matcherOf(q)
	if m == nil {
		r.msg = nothingSearched
		return
	}
	var hits []int
	for i, l := range r.rendered.lines {
		if len(m.spans(l.plain())) > 0 {
			hits = append(hits, i)
		}
	}
	if len(hits) == 0 {
		r.msg = noMatch(q, "this page")
		return
	}
	i, ok := nextHit(len(hits), dir, func(i int) int { return cmp.Compare(hits[i], r.cursor) })
	if !ok {
		r.msg = noFurther(q, dir, "")
		return
	}
	r.moveTo(hits[i])
	r.msg = searchMsg(q, i, len(hits))
}

func (r *Readme) View() tea.View { return tea.NewView(r.frame()) }

func (r *Readme) frame() string {
	if r.width == 0 {
		return ""
	}
	t := r.cfg.Theme
	lines := r.rendered.lines
	var b strings.Builder
	var where []string
	if i := slices.Index(r.order, r.page); i >= 0 {
		where = append(where, fmt.Sprintf("page %d/%d", i+1, len(r.order)))
	}
	if n := len(lines); n > r.bodyHeight() {
		where = append(where, fmt.Sprintf("%d%%", 100*min(r.top+r.bodyHeight(), n)/n))
	}
	b.WriteString(statusLine(t, r.width, t.Title.Render(" "+printable(r.page)), strings.Join(where, " · ")+" ") + "\n")
	m := matcherOf(r.cfg.Search.Query)
	for i := r.top; i < r.top+r.bodyHeight(); i++ {
		if i < len(lines) {
			margin := " "
			if i == r.cursor {
				margin = t.Cursor.Render(margin)
			}
			b.WriteString(margin + r.drawLine(lines[i], m))
		}
		b.WriteString("\n")
	}
	keys := statusKeys(r.cfg, barKeys{enter: "follow", back: "back", text: true})
	b.WriteString(pageBar(r.cfg, r.width, pageStatus(r.cfg, r.msg, keys), ""))
	return r.withWebAddress(b.String())
}

// withWebAddress returns frame with a box of the web address of the selected
// link below its line, or above it near the bottom, while the cursor is on
// it; links into the documentation need none.
func (r *Readme) withWebAddress(frame string) string {
	if r.link < 0 {
		return frame
	}
	target := r.rendered.links[r.link].target
	if page, _, _ := r.resolve(target); page != "" {
		return frame
	}
	row := 1 + r.cursor - r.top // in frame, below the title
	y := row + 1
	if y+minBoxHeight > 1+r.bodyHeight() {
		y = row - minBoxHeight
	}
	if y < 1 {
		return frame // no room
	}
	x := 1 // the cursor's column, then the line up to the link
	for _, s := range r.rendered.lines[r.cursor].segs {
		if s.link == r.link {
			break
		}
		x += ansi.StringWidth(s.text)
	}
	lines := []string{printable(target)}
	w := popupWidth(popup.Frame{}, lines, 0, r.width)
	return popupBox{popup.Frame{}, lines, w, minBoxHeight}.at(r.cfg.Theme, frame, max(min(x, r.width-w), 0), y, r.width)
}

// drawLine draws l in the styles of its pieces, the selected link in the
// cursor style and what m finds in the search style.
func (r *Readme) drawLine(l docLine, m *matcher) string {
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
		st := r.style(s.style)
		switch {
		case s.link >= 0 && s.link == r.link:
			st = r.cfg.Theme.Cursor
		case s.link >= 0 && !r.live(s.link):
			st = r.style(docPlain) // leads nowhere
		}
		// Runs of the piece in or out of a match.
		for start := 0; start < len(s.text); {
			end := start + 1
			in := found != nil && found[off+start]
			for end < len(s.text) && (found != nil && found[off+end]) == in {
				end++
			}
			if in {
				b.WriteString(r.cfg.Theme.Search.Render(s.text[start:end]))
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
func (r *Readme) style(s docStyle) lipgloss.Style {
	t := r.cfg.Theme
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
