package ui

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/settings"
)

// askSearchMsg asks SearchBar for the query to search for.
type askSearchMsg struct{}

func askSearch() tea.Msg { return askSearchMsg{} }

// findMsg asks the view shown to search for the query of config.Search from
// its cursor on, forward (1) or backward (-1).
type findMsg int

// matcher finds a query in lines. Case is ignored unless the query holds
// an upper case letter, also in a regular expression.
type matcher struct {
	q  config.Query
	re *regexp.Regexp // for a regular expression
}

// compile returns the matcher of q, or why its regular expression does not
// compile.
func compile(q config.Query) (*matcher, error) {
	m := &matcher{q: q}
	if q.Regex {
		src := q.Text
		if !hasUpper(src, true) {
			src = "(?i)" + src
		}
		re, err := regexp.Compile(src)
		if err != nil {
			return nil, err
		}
		m.re = re
	}
	return m, nil
}

// compiled is the matcher used last: lines are drawn one at a time, all
// with the same query.
var compiled struct {
	sync.Mutex
	q config.Query
	m *matcher
}

// matcherOf returns the matcher of q, nil when q is empty or its regular
// expression does not compile.
func matcherOf(q config.Query) *matcher {
	if q.Text == "" {
		return nil
	}
	compiled.Lock()
	defer compiled.Unlock()
	if compiled.m == nil || compiled.q != q {
		compiled.q = q
		compiled.m, _ = compile(q)
	}
	return compiled.m
}

// spans returns where m's query is found in text, without overlaps; empty
// matches of a regular expression are left out.
func (m *matcher) spans(text string) []diff.Span {
	var out []diff.Span
	if m.re != nil {
		for _, loc := range m.re.FindAllStringIndex(text, -1) {
			if loc[1] > loc[0] {
				out = append(out, diff.Span{Start: loc[0], End: loc[1]})
			}
		}
		return out
	}
	q := m.q.Text
	exact := hasUpper(q, false)
	for i := 0; i < len(text); {
		end := -1
		switch {
		case exact && strings.HasPrefix(text[i:], q):
			end = i + len(q)
		case !exact:
			if n := foldPrefix(text[i:], q); n >= 0 {
				end = i + n
			}
		}
		if end > i {
			out = append(out, diff.Span{Start: i, End: end})
			i = end
			continue
		}
		_, n := utf8.DecodeRuneInString(text[i:])
		i += n
	}
	return out
}

// foldPrefix returns the length in bytes of the start of s that is q with
// case ignored, rune by rune, or -1. The lengths may differ, as of K and the
// Kelvin sign.
func foldPrefix(s, q string) int {
	j := 0
	for _, qr := range q {
		if j >= len(s) {
			return -1
		}
		r, n := utf8.DecodeRuneInString(s[j:])
		if r != qr && !strings.EqualFold(string(r), string(qr)) {
			return -1
		}
		j += n
	}
	return j
}

// hasUpper reports whether query holds an upper case letter, which makes the
// search match case. In a regular expression the letters of escapes, as \S,
// \pL, \p{Lu} or \x4A, do not count.
func hasUpper(query string, regex bool) bool {
	if !regex {
		return strings.ContainsFunc(query, unicode.IsUpper)
	}
	for i := 0; i < len(query); {
		r, n := utf8.DecodeRuneInString(query[i:])
		i += n
		switch {
		case r == '\\' && i < len(query):
			e, m := utf8.DecodeRuneInString(query[i:])
			i += m
			if e == 'p' || e == 'P' || e == 'x' {
				switch {
				case i < len(query) && query[i] == '{':
					if j := strings.IndexByte(query[i:], '}'); j >= 0 {
						i += j + 1
					}
				case e == 'x':
					i = min(i+2, len(query))
				case i < len(query):
					_, m := utf8.DecodeRuneInString(query[i:])
					i += m
				}
			}
		case unicode.IsUpper(r):
			return true
		}
	}
	return false
}

// hit is a line found by /: side's line of diff row row.
type hit struct {
	row  int
	side side
}

// order is where h stands among the lines of its diff: by row, the left
// line first.
func (h hit) order() int { return 2*h.row + int(h.side) }

// hits returns the lines of d that m finds something in, in order: only
// the changed ones unless all is set.
func (d *fileDiff) hits(m *matcher, all bool) []hit {
	var out []hit
	for i, r := range d.rows {
		if !all && r.Kind == diff.Equal {
			continue
		}
		for _, s := range []side{left, right} {
			if line := lineOf(r, s); line >= 0 && m.spans(d.lines[s][line]) != nil {
				out = append(out, hit{i, s})
			}
		}
	}
	return out
}

// nextHit returns the index of the hit to go to in direction dir among n
// hits in order, which compare compares to the cursor: forward the first
// after it, backward the last before it, so that the cursor's own line is
// passed; false past the last or before the first, as the search does not
// wrap around.
func nextHit(n, dir int, compare func(i int) int) (int, bool) {
	before := func(i int) bool {
		if dir > 0 {
			return compare(i) <= 0
		}
		return compare(i) < 0
	}
	after := 0 // the first hit not before the cursor
	for after < n && before(after) {
		after++
	}
	if dir > 0 {
		return after, after < n
	}
	return after - 1, after > 0
}

// showMatch returns the horizontal scroll that shows the first match of m
// in the line of side s of d, in a view viewWidth columns wide, scrolled by
// xoff: xoff when lines are wrapped or the match is in view, else in steps of
// 8 columns, as h and l scroll, the match a step from the left edge.
func (d *fileDiff) showMatch(m *matcher, s side, line, xoff, viewWidth int) int {
	spans := m.spans(d.lines[s][line])
	if d.cfg.Settings.Wrap || len(spans) == 0 {
		return xoff
	}
	width := d.contentWidth(viewWidth, !d.cfg.Settings.SideBySide)
	text := d.lines[s][line]
	from := ansi.StringWidth(printable(text[:spans[0].Start]))
	to := from + ansi.StringWidth(printable(text[spans[0].Start:spans[0].End]))
	if from >= xoff && to <= xoff+width-1 { // the last column may hold »
		return xoff
	}
	return max(from-8, 0) / 8 * 8
}

// nothingSearched is the message of . and , before the first search.
const nothingSearched = "nothing searched yet: / searches"

// searchMsg returns the status message of match i of n for q.
func searchMsg(q config.Query, i, n int) string {
	return queryName(q) + fmt.Sprintf(": match %d/%d · . next · , previous", i+1, n)
}

// queryName returns q as the status line names it.
func queryName(q config.Query) string {
	if q.Regex {
		return "regex /" + q.Text
	}
	return "/" + q.Text
}

// noFurther returns the status message when no match of q lies further in
// direction dir: the cursor stays.
func noFurther(q config.Query, dir int, where string) string {
	return queryName(q) + ": " + ends(dir > 0, "no match further down", "no match further up") + strings.Replace(where, " of ", " in ", 1)
}

// scope returns where / searches, as messages name it.
func scope(st settings.Settings) string {
	if st.SearchAll {
		return "all lines"
	}
	return "changed lines"
}

// findText moves the cursor to the next line, in direction dir, that holds
// what config.Search searched for, past the cursor's own line; past the
// last or first match it stays. Without wrapping lines, it scrolls the
// match into view.
func (m *Model) findText(dir int) {
	q := m.cfg.Search.Query
	mt := matcherOf(q)
	if mt == nil {
		m.msg = nothingSearched
		return
	}
	hits := m.hits(mt, m.cfg.Settings.SearchAll)
	if len(hits) == 0 {
		m.msg = "no match for " + q.Text + " in " + scope(m.cfg.Settings)
		return
	}
	m.syncLayout()
	pos := hit{0, m.active}
	if m.cursor < len(m.view) {
		pos.row = m.view[m.cursor].row
	}
	i, ok := nextHit(len(hits), dir, func(i int) int { return cmp.Compare(hits[i].order(), pos.order()) })
	if !ok {
		m.msg = noFurther(q, dir, "")
		return
	}
	h := hits[i]
	line := lineOf(m.rows[h.row], h.side)
	m.jumpTo(m.reveal(h.side, line, m.shape()))
	m.xoff = m.showMatch(mt, h.side, line, m.xoff, m.width)
	m.msg = searchMsg(q, i, len(hits))
}

// flowHit is a line found by / in the flow view: hit in file of part.
type flowHit struct {
	part, file int
	hit
}

// findText moves the cursor to the next line, in direction dir, that holds
// what config.Search searched for, past the cursor's own line, in all
// files; in the log in those of the cursor's commit, N and P go to the
// others. Past the last or first match it stays. Without wrapping lines, it
// scrolls the match into view.
func (f *Flow) findText(dir int) {
	q := f.cfg.Search.Query
	mt := matcherOf(q)
	if mt == nil {
		f.msg = nothingSearched
		return
	}
	cur := 0 // the cursor's part
	if f.cursor < len(f.rows) {
		cur = f.rows[f.cursor].part
	}
	if f.inLog() && f.unloaded(cur) {
		f.await(cur, func() { f.findText(dir) })
		return
	}
	where := ""
	if f.inLog() {
		where = " of this commit"
	}
	var hits []flowHit
	for p, part := range f.parts {
		if part.app == nil || f.inLog() && p != cur {
			continue
		}
		for i, e := range part.app.files {
			if e.Binary {
				continue
			}
			for _, h := range part.app.diffOf(e).hits(mt, f.cfg.Settings.SearchAll) {
				hits = append(hits, flowHit{p, i, h})
			}
		}
	}
	if len(hits) == 0 {
		f.msg = "no match for " + q.Text + " in " + scope(f.cfg.Settings) + where
		return
	}
	// The cursor's position: a commit's or file's own rows come before the
	// lines of its files.
	pos := flowHit{hit: hit{-1, f.active}}
	if f.cursor < len(f.rows) {
		r := f.rows[f.cursor]
		pos.part, pos.file = r.part, r.file
		if r.kind == flowDiff {
			pos.row = f.parts[r.part].app.files[r.file].diff.view[r.line].row
		}
	}
	i, ok := nextHit(len(hits), dir, func(i int) int {
		a := hits[i]
		return cmp.Or(cmp.Compare(a.part, pos.part), cmp.Compare(a.file, pos.file), cmp.Compare(a.order(), pos.order()))
	})
	if !ok {
		f.msg = noFurther(q, dir, where)
		return
	}
	h := hits[i]
	d := f.parts[h.part].app.files[h.file].diff
	line := lineOf(d.rows[h.row], h.side)
	at := d.reveal(h.side, line, f.built)
	f.relocate() // a fold may have opened: the cursor keeps its row
	f.jumpTo(f.findDiff(h.part, h.file, at.v, at.sub))
	f.active, f.msg = h.side, searchMsg(q, i, len(hits))
	f.xoff = d.showMatch(mt, h.side, line, f.xoff, f.width)
}

// SearchBar wraps the view: / opens a bar at the bottom to type what to
// search for, in changed lines or, switched with tab, in all, plain text
// or, switched with shift+tab, a regular expression; both are toggles,
// kept in state.toml. Up and down go through what was searched for
// before, enter searches from the cursor on, as . does. What was searched
// for is kept in config.Search.
type SearchBar struct {
	tea.Model
	cfg   *config.Config
	input *lineInput // while the bar is open
	regex bool       // the text is a regular expression, as last searched or toggled
	// hist is the entry of config.Search.History shown, its length for the
	// text typed, draft, kept while going through the history.
	hist  int
	draft config.Query
	err   string // why the regular expression does not compile, or the state is not saved

	width, height int
}

// WithSearch returns m with the search bar.
func WithSearch(cfg *config.Config, m tea.Model) *SearchBar { return &SearchBar{Model: m, cfg: cfg} }

func (s *SearchBar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
	case askSearchMsg:
		s.input, s.regex, s.err = &lineInput{}, s.cfg.Settings.SearchRegex, ""
		s.hist = len(s.cfg.Search.History)
		return s, nil
	case tea.KeyPressMsg:
		if s.input != nil {
			return s, s.key(msg)
		}
	case tea.PasteMsg:
		if s.input != nil {
			s.err = ""
			s.input.paste(msg.Content)
			return s, nil
		}
	}
	var cmd tea.Cmd
	s.Model, cmd = s.Model.Update(msg)
	return s, cmd
}

// key edits the query: enter searches it, esc, enter on nothing typed or
// backspace on it closes the bar; tab switches between changed and all
// lines, shift+tab between plain text and regular expression; up and
// down go through the history, each query in its mode.
func (s *SearchBar) key(k tea.KeyPressMsg) tea.Cmd {
	in, history, st := s.input, s.cfg.Search.History, &s.cfg.Settings
	s.err = ""
	show := func(q config.Query) { in.text, s.regex = q.Text, q.Regex }
	switch {
	case k.String() == "ctrl+c":
		return tea.Quit
	case in.cancels(k):
		s.input = nil
		return nil
	}
	switch k.String() {
	case "enter":
		q := config.Query{Text: in.text, Regex: s.regex}
		if q.Text == "" {
			s.input = nil
			return nil
		}
		if _, err := compile(q); err != nil {
			s.err = err.Error()
			return nil
		}
		s.input = nil
		s.cfg.Search.Add(q)
		if st.SearchRegex != q.Regex { // from the history
			st.SearchRegex = q.Regex
			s.err = saveState(s.cfg)
		}
		return func() tea.Msg { return findMsg(1) }
	case "tab":
		s.err = toggle(s.cfg, &st.SearchAll)
	case "shift+tab":
		s.regex = !s.regex
		st.SearchRegex = s.regex
		s.err = saveState(s.cfg)
	case "up":
		if s.hist == len(history) {
			s.draft = config.Query{Text: in.text, Regex: s.regex}
		}
		if s.hist > 0 {
			s.hist--
			show(history[s.hist])
		}
	case "down":
		if s.hist < len(history) {
			s.hist++
			if s.hist == len(history) {
				show(s.draft)
			} else {
				show(history[s.hist])
			}
		}
	default:
		in.edit(k)
	}
	return nil
}

// frame draws the bar over the view, above its status line, while it is
// open: the query, and why its regular expression does not compile, see
// inputBox.
func (s *SearchBar) View() tea.View { return tea.NewView(s.frame()) }

func (s *SearchBar) frame() string {
	base := s.Model.View().Content
	if s.input == nil || s.width == 0 {
		return base
	}
	t := s.cfg.Theme
	// The title says how the bar searches and what tab and shift+tab switch
	// to.
	mode, otherMode := "search", "regex"
	if s.regex {
		mode, otherMode = "regex", "text"
	}
	otherScope := "all"
	if s.cfg.Settings.SearchAll {
		otherScope = "changed"
	}
	keys := []string{"enter find", "tab " + otherScope, "shift+tab " + otherMode, "esc cancel"}
	if len(s.cfg.Search.History) > 0 {
		keys = slices.Insert(keys, 1, "↑↓ history")
	}
	title := popupTitle(t, mode+" "+scope(s.cfg.Settings), "", strings.Join(keys, " · "))
	bar, w := inputBox(t, s.width, title, "/"+printable(s.input.text), s.err, false)
	// Above the status line, the last row.
	y := max(strings.Count(base, "\n")-strings.Count(bar, "\n"), 0)
	return overlayAt(base, bar, max((s.width-w)/2, 0), y, w, s.width)
}
