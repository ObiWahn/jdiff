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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/settings"
)

// searchKey opens the search bar; nextKey and prevKey search again forward
// and backward.
const searchKey, nextKey, prevKey = "/", ".", ","

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
		if !strings.ContainsFunc(src, unicode.IsUpper) {
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
	exact := strings.ContainsFunc(q, unicode.IsUpper)
	for i := 0; i+len(q) <= len(text); {
		part := text[i : i+len(q)]
		if exact && part == q || !exact && strings.EqualFold(part, q) {
			out = append(out, diff.Span{Start: i, End: i + len(q)})
			i += len(q)
			continue
		}
		_, n := utf8.DecodeRuneInString(text[i:])
		i += n
	}
	return out
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
// hits in order, which before reports to come before the cursor: forward
// the first that does not, backward the last that does; false past the
// last or before the first, as the search does not wrap around.
func nextHit(n, dir int, before func(i int) bool) (int, bool) {
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
// in the line of side s of d, in a pane width columns wide, scrolled by
// xoff: xoff when the match is in view, else in steps of 8 columns, as h
// and l scroll, the match a step from the left edge.
func (d *fileDiff) showMatch(m *matcher, s side, line, xoff, width int) int {
	spans := m.spans(d.lines[s][line])
	if len(spans) == 0 {
		return xoff
	}
	text := d.lines[s][line]
	from := ansi.StringWidth(printable(text[:spans[0].Start]))
	to := from + ansi.StringWidth(printable(text[spans[0].Start:spans[0].End]))
	if from >= xoff && to <= xoff+width-1 { // the last column may hold »
		return xoff
	}
	return max(from-8, 0) / 8 * 8
}

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
		m.msg = "nothing searched yet: / searches"
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
	// Forward, the cursor's own line counts as before it, backward as after
	// it, so that it is passed.
	before := func(i int) bool {
		if dir > 0 {
			return hits[i].order() <= pos.order()
		}
		return hits[i].order() < pos.order()
	}
	i, ok := nextHit(len(hits), dir, before)
	if !ok {
		m.msg = noFurther(q, dir, "")
		return
	}
	h := hits[i]
	line := lineOf(m.rows[h.row], h.side)
	m.jumpTo(m.reveal(h.side, line, m.shape()))
	if !m.cfg.Settings.Wrap {
		m.xoff = m.showMatch(mt, h.side, line, m.xoff, m.contentWidth(m.width, !m.cfg.Settings.SideBySide))
	}
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
		f.msg = "nothing searched yet: / searches"
		return
	}
	cur := 0 // the cursor's part
	if f.cursor < len(f.rows) {
		cur = f.rows[f.cursor].part
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
	compare := func(a, b flowHit) int {
		return cmp.Or(cmp.Compare(a.part, b.part), cmp.Compare(a.file, b.file), cmp.Compare(a.order(), b.order()))
	}
	before := func(i int) bool {
		if dir > 0 {
			return compare(hits[i], pos) <= 0
		}
		return compare(hits[i], pos) < 0
	}
	i, ok := nextHit(len(hits), dir, before)
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
	if !f.cfg.Settings.Wrap {
		f.xoff = d.showMatch(mt, h.side, line, f.xoff, d.contentWidth(f.width, !f.cfg.Settings.SideBySide))
	}
}

// SearchBar wraps the view: / opens a bar at the bottom to type what to
// search for, plain text or, switched with tab, a regular expression, in
// changed lines or, switched with shift+tab, in all; both are
// toggles, kept in state.toml. Up and down go through what was searched for
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
	case tea.KeyMsg:
		if s.input != nil {
			return s, s.key(msg)
		}
	}
	var cmd tea.Cmd
	s.Model, cmd = s.Model.Update(msg)
	return s, cmd
}

// key edits the query: enter searches it, esc, enter on nothing typed or
// backspace on it closes the bar; tab switches between plain text and
// regular expression, shift+tab between changed and all lines; up and
// down go through the history, each query in its mode.
func (s *SearchBar) key(k tea.KeyMsg) tea.Cmd {
	in, history, st := s.input, s.cfg.Search.History, &s.cfg.Settings
	s.err = ""
	show := func(q config.Query) { in.text, s.regex = q.Text, q.Regex }
	switch {
	case k.Type == tea.KeyCtrlC:
		return tea.Quit
	case in.cancels(k):
		s.input = nil
		return nil
	}
	switch k.Type {
	case tea.KeyEnter:
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
	case tea.KeyTab:
		s.regex = !s.regex
		st.SearchRegex = s.regex
		s.err = saveState(s.cfg)
	case tea.KeyShiftTab:
		s.err = toggle(s.cfg, &st.SearchAll)
	case tea.KeyUp:
		if s.hist == len(history) {
			s.draft = config.Query{Text: in.text, Regex: s.regex}
		}
		if s.hist > 0 {
			s.hist--
			show(history[s.hist])
		}
	case tea.KeyDown:
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

// View draws the bar over the view, above its status line, while it is
// open: the query, and why its regular expression does not compile, see
// inputBox.
func (s *SearchBar) View() string {
	base := s.Model.View()
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
	keys := []string{"enter find", "tab " + otherMode, "shift+tab " + otherScope, "esc cancel"}
	if len(s.cfg.Search.History) > 0 {
		keys = slices.Insert(keys, 1, "↑↓ history")
	}
	title := popupTitle(t, mode+" "+scope(s.cfg.Settings), "", strings.Join(keys, " · "))
	bar, w := inputBox(t, s.width, title, "/"+printable(s.input.text), s.err, false)
	// Above the status line, the last row.
	y := max(strings.Count(base, "\n")-strings.Count(bar, "\n"), 0)
	return overlayAt(base, bar, max((s.width-w)/2, 0), y, w, s.width)
}
