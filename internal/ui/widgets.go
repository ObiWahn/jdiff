package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/keys"
	"jdiff/internal/theme"
)

// paneTitles renders names above the panes of a view width columns wide,
// at the end of the left one the lines removed, of the right one those added,
// none when 0 or when the pane is too narrow. Side by side the name of the
// active pane is bold, the other one faint; in the normal layout, one column
// of both, both are bold.
func paneTitles(t *theme.Theme, names [2]string, removed, added int, active side, sideBySide bool, width int) string {
	w := (width - 1) / 2
	title := func(s side) string {
		style := t.Heading
		if sideBySide && s != active {
			style = t.InactiveHeading
		}
		n, sign, st := removed, "-", t.Minus
		if s == right {
			n, sign, st = added, "+", t.Plus
		}
		tail := ""
		if n > 0 {
			tail = sign + fmt.Sprint(n)
		}
		// The count takes a space before and after it, and leaves the name
		// two columns.
		if tail == "" || len(tail)+4 > w {
			return pad(style.Render(ansi.Truncate(" "+printable(names[s]), w, "…")), w, lipgloss.NewStyle())
		}
		nameW := w - len(tail) - 2
		name := pad(style.Render(ansi.Truncate(" "+printable(names[s]), nameW, "…")), nameW, lipgloss.NewStyle())
		return name + " " + st.Render(tail) + " "
	}
	return title(left) + t.Decoration.Render("│") + title(right)
}

// sideCells renders the line above the names of a git diff or of two
// directories, in two cells split as paneTitles splits the names: in each
// its label in the commit hash style, then its tail right aligned. Each
// cell is cut on the right where it does not fit, so that the labels show
// longest.
func sideCells(t *theme.Theme, labels, tails [2]string, width int) string {
	w := (width - 1) / 2
	cell := func(s side) string {
		label := t.CommitHash.Render(printable(labels[s]))
		gap := max(w-2-ansi.StringWidth(label)-ansi.StringWidth(tails[s]), 2)
		c := " " + label + strings.Repeat(" ", gap) + tails[s] + " "
		return pad(ansi.Truncate(c, w, "…"), w, lipgloss.NewStyle())
	}
	return cell(left) + t.Decoration.Render("│") + cell(right)
}

// statusLine renders the status bar with left, already styled, and right,
// in the hint style, aligned to its edges; left is shortened when both do
// not fit.
func statusLine(t *theme.Theme, width int, left, right string) string {
	left = ansi.Truncate(left, max(width-ansi.StringWidth(right), 0), "…")
	gap := max(width-ansi.StringWidth(left)-ansi.StringWidth(right), 0)
	return ansi.Truncate(left+t.Hint.Render(strings.Repeat(" ", gap)+right), width, "")
}

// statusText returns the left part of a status bar: msg, or without one
// the hints, each a key and what it does, separated by " · ", in the hint
// style. msg may name files or quote git, so it is drawn printable.
func statusText(t *theme.Theme, msg, hints string) string {
	if msg != "" {
		return t.Hint.Render(printable(msg))
	}
	return t.Hint.Render(hints)
}

// pageBar renders the status bar of a page, width columns wide: left, and
// at the right end right, as the anchors, and after it the version of jdiff
// (Config.Version) in its style and a space. The version shows only where it
// fits beside both, so that the keys and the anchors keep their room. It is
// rendered beside the line, not in it: lipgloss breaks nested escape
// sequences.
func pageBar(cfg *config.Config, width int, left, right string) string {
	if v := cfg.Version; v != "" {
		tail := " " + v + " "
		with := right
		if right != "" {
			with += " ·"
		}
		if tw := ansi.StringWidth(tail); ansi.StringWidth(left)+ansi.StringWidth(with)+tw <= width {
			return statusLine(cfg.Theme, width-tw, left, with) + cfg.Theme.Version.Render(tail)
		}
	}
	return statusLine(cfg.Theme, width, left, right)
}

// listBar returns the status bar of a list page, the commits or the files:
// as pageBar with the version; without one left alone, which list.render
// draws as it is.
func listBar(cfg *config.Config, width int, left string) string {
	if cfg.Version == "" {
		return left
	}
	return pageBar(cfg, width, left, "")
}

// pageStatus returns the left part of the status bar of a page, see
// statusText; while auto reload is on, its name comes first.
func pageStatus(cfg *config.Config, msg, hints string) string {
	t := cfg.Theme
	if cfg.AutoReload {
		return t.CommitHash.Render(" auto reload ") + t.Hint.Render(" ") + statusText(t, msg, hints) + t.Hint.Render(" ")
	}
	return statusText(t, msg, hints)
}

// barKeys says what differs between the status bars of the pages.
type barKeys struct {
	enter   string // the action of enter
	back    string // the action of backspace, "" when it does nothing
	commits bool   // N and P switch commits
	flow    bool   // f switches between the flow and the single file view
	text    bool   // a page of text, --readme: no layout, git diff or settings
}

// barState returns the index of an action's label while it is on or off.
func barState(on bool) int {
	if on {
		return 0
	}
	return 1
}

// statusKeys returns the keys of the status bar of a page, the same on all of
// them, in one order, so that a key that switches what is shown changes only
// its own label: f and s name what they switch to. All other keys are in the
// help. keybindings.toml gives the keys and the words.
func statusKeys(cfg *config.Config, k barKeys) string {
	var shown []string
	add := func(row keys.Row, keyNames string, state int) {
		if l := row.LabelIn(state); l != "" {
			shown = append(shown, keyNames+" "+l)
		}
	}
	first := func(action string, state int) {
		if r := cfg.Keys.Row(action); len(r.Keys) > 0 {
			add(r, r.Keys[0], state)
		}
	}
	first(k.enter, 0)
	if k.back != "" {
		first(k.back, 0)
	}
	if k.commits {
		r := cfg.Keys.Row("history")
		add(r, strings.Join(r.Keys, "/"), 0)
	}
	if k.flow {
		first("flow", barState(cfg.Settings.Flow))
	}
	actions := []string{"git_diff", "settings", "help", "quit"}
	if k.text {
		actions = actions[2:]
	} else {
		first("layout", barState(cfg.Settings.SideBySide))
	}
	for _, action := range actions {
		first(action, 0)
	}
	return strings.Join(shown, " · ")
}

// minBoxHeight fits the borders and one line.
const minBoxHeight = 3

// splitList divides a screen height high between a heading, an empty line,
// a list of n items, the box below it when shown and the status bar. The
// list takes at most half of the room and the box, after an empty line, the
// rest; box is 0 when hidden, for an empty list or when the screen is too
// small for it.
func splitList(height, n int, showBox bool) (list, box int) {
	room := max(height-3, 1)
	if !showBox || n == 0 {
		return room, 0
	}
	list = max(min(n, room/2), 1)
	box = room - list - 1
	if box < minBoxHeight {
		return room, 0
	}
	return list, box
}

// box renders lines in a rounded box width columns wide and height lines
// high, with title in the top border. Lines that do not fit are cut. The
// boxes of files and commits use it; popups draw their borders through
// popupFrame.
func box(t *theme.Theme, width, height int, title string, lines []string) string {
	border := t.Decoration
	title = ansi.Truncate(title, max(width-4, 0), "")
	return border.Render("╭─") + title + border.Render(strings.Repeat("─", max(width-3-ansi.StringWidth(title), 0))+"╮") + "\n" +
		boxRows(t, width, height, lines) +
		border.Render("╰"+strings.Repeat("─", max(width-2, 0))+"╯") + "\n"
}

// boxRows renders the rows between the borders of a box width columns wide
// and height lines high: lines, cut with … where they do not fit, padded
// with empty rows.
func boxRows(t *theme.Theme, width, height int, lines []string) string {
	border := t.Decoration
	inner := max(width-4, 0) // border and a space on each side
	var b strings.Builder
	for i := range height - 2 {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], inner, "…")
		}
		b.WriteString(border.Render("│") + " " + pad(line, inner, lipgloss.NewStyle()) + " " + border.Render("│") + "\n")
	}
	return b.String()
}

// popupHeight is the height of a popup of n lines, at least one, on a screen
// height lines high: all lines when they fit, else the screen but a line above
// and below.
func popupHeight(n, height int) int { return min(max(n, 1)+2, max(height-2, minBoxHeight)) }

// emptyListWidth is the least width of the line of a listPopup without items.
const emptyListWidth = 40

// listPopup is the state of a popup that lists items with a cursor, so that
// such popups move and look the same.
type listPopup struct {
	cursor, top   int // the selected item, the first one shown
	width, height int
}

// moveIn moves the cursor by delta among n items.
func (p *listPopup) moveIn(delta, n int) {
	p.cursor = max(min(p.cursor+delta, n-1), 0)
	p.top = windowAround(p.cursor, p.top, popupHeight(n, p.height)-2, n).first
}

// draw draws the popup over base, in frame f: the items all in a box, the
// selected one in the cursor style, or empty as a hint when there are none.
// It is as wide as the items and the frame need, up to the width of the
// screen.
func (p *listPopup) draw(t *theme.Theme, base string, f popupFrame, all []string, empty string) string {
	n := len(all)
	height := popupHeight(n, p.height)
	lines := windowAround(p.cursor, p.top, height-2, n).rows(t, n, func(i int) string {
		if i == p.cursor {
			return t.Cursor.Render(all[i])
		}
		return all[i]
	})
	width := popupWidth(f, all, 0, p.width)
	if n == 0 {
		lines, width = []string{t.Hint.Render(empty)}, popupWidth(f, nil, emptyListWidth, p.width)
	}
	return popup{f, lines, width, height}.over(t, base, p.width)
}

// popup is a box drawn over the screen: its frame, its lines, and its width
// and height, borders included. All popups are one, so that they look alike.
type popup struct {
	frame popupFrame
	lines []string
	w, h  int
}

// popupFrame is what a popup shows in its borders: name at the left of the
// top border and hints (its keys, or where it stands) at the right end, at least a line between them;
// msg (what went wrong or happens) at the right of the bottom border. What
// does not fit is cut with …, the hints before the name. msg may name files
// or quote git, so it is drawn printable.
type popupFrame struct {
	name, hints, msg string
}

// minDash is the least border line beside a text in a popup's border.
const minDash = 1

// topWidth returns the width of the top border's texts, " name " and
// " hints ", with the least line between them; 0 without either.
func (f popupFrame) topWidth() int {
	w := 0
	if f.name != "" {
		w += ansi.StringWidth(f.name) + 2
	}
	if f.hints != "" {
		w += ansi.StringWidth(f.hints) + 2
	}
	if f.name != "" && f.hints != "" {
		w += minDash
	}
	return w
}

// textWidth returns the width the texts of both borders need inside the
// least lines beside them.
func (f popupFrame) textWidth() int {
	if msg := printable(f.msg); msg != "" {
		return max(f.topWidth(), ansi.StringWidth(msg)+2)
	}
	return f.topWidth()
}

// fitTop returns f with the hints, then the name, cut until the top border's
// texts are at most room wide.
func (f popupFrame) fitTop(room int) popupFrame {
	if f.topWidth() <= room {
		return f
	}
	if f.hints != "" {
		hintsRoom := room - 2 // " hints "
		if f.name != "" {
			hintsRoom -= ansi.StringWidth(f.name) + 2 + minDash
		}
		if hintsRoom > 1 {
			f.hints = ansi.Truncate(f.hints, hintsRoom, "…")
			return f
		}
	}
	f.name, f.hints = ansi.Truncate(f.name, max(room-2, 0), "…"), ""
	return f
}

// borders returns the top and the bottom border of a popup width columns
// wide, each ending in a line break.
func (f popupFrame) borders(t *theme.Theme, width int) (top, bottom string) {
	b := t.Decoration
	line := max(width-2, 0) // between the corners
	room := max(line-2*minDash, 0)
	f = f.fitTop(room)
	rest := max(room-f.topWidth(), 0)
	top = b.Render("╭" + strings.Repeat("─", minDash))
	if f.name != "" {
		top += " " + t.CommitHash.Render(f.name) + " "
	}
	if f.hints == "" {
		top += b.Render(strings.Repeat("─", rest+minDash)+"╮") + "\n"
	} else {
		gap := rest
		if f.name != "" {
			gap += minDash
		}
		if gap > 0 {
			top += b.Render(strings.Repeat("─", gap))
		}
		top += " " + t.Hint.Render(f.hints) + " " + b.Render(strings.Repeat("─", minDash)+"╮") + "\n"
	}
	msg := printable(f.msg)
	if ansi.StringWidth(msg)+2 > room {
		msg = ansi.Truncate(msg, max(room-2, 0), "…")
	}
	if msg == "" {
		return top, b.Render("╰"+strings.Repeat("─", line)+"╯") + "\n"
	}
	return top, b.Render("╰"+strings.Repeat("─", max(line-minDash-ansi.StringWidth(msg)-2, 0))) +
		" " + t.PopupMessage.Render(msg) + " " + b.Render(strings.Repeat("─", minDash)+"╯") + "\n"
}

// popupWidth returns the width of a popup in frame f with lines: as wide as
// they and the texts of its borders need, at least least columns inside, up
// to width. A line wider than that is cut by boxRows.
func popupWidth(f popupFrame, lines []string, least, width int) int {
	inner := max(least, f.textWidth())
	for _, l := range lines {
		inner = max(inner, ansi.StringWidth(l))
	}
	return min(inner+4, width) // borders and a space on each side
}

// render returns p drawn: its borders and its lines between them.
func (p popup) render(t *theme.Theme) string {
	top, bottom := p.frame.borders(t, p.w)
	return top + boxRows(t, p.w, p.h, p.lines) + bottom
}

// over draws p centered over base, which is width columns wide.
func (p popup) over(t *theme.Theme, base string, width int) string {
	return overlay(base, p.render(t), p.w, width)
}

// at draws p over base, which is width columns wide, from column x of row y on.
func (p popup) at(t *theme.Theme, base string, x, y, width int) string {
	return overlayAt(base, p.render(t), x, y, p.w, width)
}

// scrollWindow returns the first shown item of a window h items high over n
// items: top, moved as little as needed to show cursor.
func scrollWindow(cursor, top, h, n int) int {
	if cursor < top {
		top = cursor
	} else if cursor >= top+h {
		top = cursor - h + 1
	}
	return max(min(top, n-h), 0)
}

// jumpTop returns the first shown item after the cursor jumped from item
// from to item to, with the window h items high over n items at top: it
// stays when to is in it, moves as little as needed when from and to fit in
// it together, so that both stay in view, and else keeps the cursor's
// screen row, so that the eye stays where it was.
func jumpTop(from, to, top, h, n int) int {
	if d := to - from; d < h && -d < h {
		return scrollWindow(to, top, h, n)
	}
	return scrollWindow(to, top+to-from, h, n)
}

// list is the state of the commit and the file list: n items below a
// heading and an empty line, the box below them while preview is set, and the
// status bar.
type list struct {
	cursor, top   int
	n             int
	preview       *bool // the setting that shows the box
	maxRows       int   // the most rows of the list while the box shows, 0 for no limit
	width, height int
}

// rows returns how many rows the list would take with the box: all items, up
// to maxRows.
func (li *list) rows() int {
	if li.maxRows > 0 {
		return min(li.n, li.maxRows)
	}
	return li.n
}

// listHeight is the height of the list below the heading and an empty line.
func (li *list) listHeight() int {
	h, _ := splitList(li.height, li.rows(), *li.preview)
	return h
}

// boxHeight is the height of the box below the list, 0 when it is hidden.
func (li *list) boxHeight() int {
	_, h := splitList(li.height, li.rows(), *li.preview)
	return h
}

func (li *list) moveCursor(delta int) {
	li.cursor = max(min(li.cursor+delta, li.n-1), 0)
	li.scrollToCursor()
}

func (li *list) scrollToCursor() {
	li.top = windowAround(li.cursor, li.top, li.listHeight(), li.n).first
}

// window is the part of n lines that a view h rows high shows: the lines
// from first to end, after a row with an arrow for the lines hidden above
// when up, and before one for those hidden below when down. Arrows take
// rows when the lines do not fit in three rows or more; an arrow hides at
// least two lines, a single one is shown instead.
type window struct {
	first, end int
	up, down   bool
}

// windowAt returns the window of h rows over n lines from top, moved into
// the lines. It never starts at 1, which would show the lines it shows from
// 0, with an arrow in place of the first.
func windowAt(top, h, n int) window {
	if n <= h || h < 3 {
		top = max(min(top, n-h), 0)
		return window{top, min(top+h, n), false, false}
	}
	// Up to the end of the lines, the window holds an arrow and h-1 lines.
	top = max(min(top, n-h+1), 0)
	if top == 1 {
		top = 0
	}
	end := shownEnd(top, h, n)
	return window{top, end, top > 0, end < n}
}

// windowAround returns the window of h rows over n lines from top, moved as
// little as needed to show the line cursor.
func windowAround(cursor, top, h, n int) window {
	if n <= h || h < 3 {
		return windowAt(scrollWindow(cursor, top, h, n), h, n)
	}
	w := windowAt(min(top, cursor), h, n)
	for cursor >= w.end {
		next := w.first + 1
		if next == 1 {
			next = 2
		}
		w = windowAt(next, h, n)
	}
	return w
}

// shownEnd returns the end of the lines shown from top in a window h rows
// high over n lines, after the rows of the arrows.
func shownEnd(top, h, n int) int {
	end := top + h
	if top > 0 {
		end-- // the arrow above
	}
	if end < n {
		end-- // the arrow below
	}
	return min(end, n)
}

// rows returns the rows of w over n lines: line(i) for the lines shown and
// the arrows with the number of lines they hide, drawn by moreRow.
func (w window) rows(t *theme.Theme, n int, line func(i int) string) []string {
	var out []string
	if w.up {
		out = append(out, moreRow(t, "↑", w.first))
	}
	for i := w.first; i < w.end; i++ {
		out = append(out, line(i))
	}
	if w.down {
		out = append(out, moreRow(t, "↓", n-w.end))
	}
	return out
}

// moreRow renders the row of an arrow for hidden lines of a list or popup.
func moreRow(t *theme.Theme, arrow string, hidden int) string {
	return t.More.Render(fmt.Sprintf("  %s %d more", arrow, hidden))
}

// render renders the list: head, the shown items as drawn by item, which
// renders the selected one bold through st, the box as drawn by box when it
// is shown, and status, styled, see statusText.
func (li *list) render(t *theme.Theme, head string, item func(i int, st func(lipgloss.Style) lipgloss.Style) string,
	box func(height int) string, status string) string {
	var b strings.Builder
	b.WriteString(ansi.Truncate(head, li.width, "…") + "\n\n")
	h := li.listHeight()
	w := windowAround(li.cursor, li.top, h, li.n)
	rows := w.rows(t, li.n, func(i int) string {
		return item(i, func(s lipgloss.Style) lipgloss.Style { return s.Bold(i == li.cursor) })
	})
	for i := range h {
		if i < len(rows) {
			b.WriteString(ansi.Truncate(rows[i], li.width, "…"))
		}
		b.WriteString("\n")
	}
	if h := li.boxHeight(); h > 0 && li.cursor < li.n {
		b.WriteString("\n" + box(h))
	}
	b.WriteString(ansi.Truncate(status, li.width, ""))
	return b.String()
}

// minInputWidth is the least width of the line in the popups of inputPopup.
const minInputWidth = 40

// inputPopup returns the popup holding a line typed, in frame f: text with
// the cursor at its end, centered between empty lines with centered, and err
// below when set. It is at least minInputWidth wide and grows with them up
// to width, beyond which the start of text is cut, so that its end stays
// in view.
func inputPopup(t *theme.Theme, width int, f popupFrame, text, err string, centered bool) popup {
	text += "█"
	w := popupWidth(f, []string{text, err}, minInputWidth, width)
	if over := ansi.StringWidth(text) - (w - 4); over > 0 {
		text = cutLeft(text, over)
	}
	lines := []string{text}
	if centered {
		lines = []string{"", strings.Repeat(" ", (w-4-ansi.StringWidth(text))/2) + text, ""}
	}
	if err != "" {
		lines = append(lines, t.Minus.Render(err))
	}
	return popup{f, lines, w, len(lines) + 2}
}

// lineInput is a line of text being typed, with the cursor at its end.
type lineInput struct{ text string }

// cancels reports whether k closes the input without using it: esc, or
// backspace on nothing typed.
func (in *lineInput) cancels(k tea.KeyPressMsg) bool {
	return k.String() == "esc" || k.String() == "backspace" && in.text == ""
}

// paste adds pasted text, which comes as a tea.PasteMsg: on one line, line
// breaks and tabs as spaces, without characters that are not visible, which
// could reach the terminal.
func (in *lineInput) paste(text string) {
	in.text += strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case hiddenChar(r):
			return -1
		}
		return r
	}, text)
}

// edit applies k to the text: typed text is added, backspace deletes a
// character, ctrl+w a word and ctrl+u the line. Pasted text comes as a
// tea.PasteMsg, which the owner adds with paste.
func (in *lineInput) edit(k tea.KeyPressMsg) {
	switch k.String() {
	case "backspace":
		if r := []rune(in.text); len(r) > 0 {
			in.text = string(r[:len(r)-1])
		}
	case "ctrl+w":
		in.text = strings.TrimRight(in.text, " ")
		in.text = in.text[:strings.LastIndex(in.text, " ")+1]
	case "ctrl+u":
		in.text = ""
	default:
		in.text += k.Text
	}
}

// pad fills s with spaces in style up to width columns.
func pad(s string, width int, style lipgloss.Style) string {
	if n := width - ansi.StringWidth(s); n > 0 {
		return s + style.Render(strings.Repeat(" ", n))
	}
	return s
}

// count formats n with sign right aligned to width, or blanks when n is 0.
func count(sign string, n, width int) string {
	if n == 0 {
		return strings.Repeat(" ", width)
	}
	return fmt.Sprintf("%*s", width, sign+fmt.Sprint(n))
}

// counts renders the added and removed line counts in columns addW and remW
// digits wide, in st of the plus and minus styles.
func counts(t *theme.Theme, st func(lipgloss.Style) lipgloss.Style, added, removed, addW, remW int) string {
	return st(t.Plus).Render(count("+", added, addW+1)) + " " + st(t.Minus).Render(count("-", removed, remW+1))
}

// totals renders the lines added and removed of a git diff or a commit.
func totals(t *theme.Theme, added, removed int) string {
	return t.Plus.Render(fmt.Sprintf("+%d", added)) + " " + t.Minus.Render(fmt.Sprintf("-%d", removed))
}

// minWidth and minHeight are the smallest terminal the views are drawn in.
// Side by side the gutters of both panes take 7 columns and twice the digits
// of the line numbers, 19 for files of up to 999999 lines; the lists need a
// heading, an empty line, an item and the status bar.
const minWidth, minHeight = 20, 4

// SizeGuard wraps the top-level model and puts it on the alternate screen.
// In a terminal smaller than minWidth by minHeight it shows that in a
// centered box instead of the page and ignores keys but ctrl+c, until the
// terminal is large enough again; the page still learns the size and gets
// all other messages.
type SizeGuard struct {
	tea.Model
	cfg           *config.Config
	width, height int
}

// WithSizeGuard returns m guarded against too small terminals.
func WithSizeGuard(cfg *config.Config, m tea.Model) *SizeGuard { return &SizeGuard{Model: m, cfg: cfg} }

// Stop ends the git commands that run in the background, and the watching
// of files, once the program ended: its git commands would else run on.
func (g *SizeGuard) Stop() {
	if m, ok := g.Model.(interface{ stop() }); ok {
		m.stop()
	}
}

func (g *SizeGuard) tooSmall() bool { return g.width < minWidth || g.height < minHeight }

func (g *SizeGuard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		g.width, g.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		if g.tooSmall() {
			if msg.String() == "ctrl+c" {
				return g, tea.Quit
			}
			return g, nil
		}
	}
	_, cmd := g.Model.Update(msg)
	return g, cmd
}

func (g *SizeGuard) View() tea.View {
	v := tea.NewView(g.frame())
	v.AltScreen = true
	return v
}

func (g *SizeGuard) frame() string {
	if g.width == 0 || !g.tooSmall() {
		return g.Model.View().Content
	}
	first := "terminal too small"
	if ansi.StringWidth(first) > g.width {
		first = "too small"
	}
	lines := []string{first, fmt.Sprintf("%dx%d, need %dx%d", g.width, g.height, minWidth, minHeight)}
	w := 0
	for _, l := range lines {
		w = max(w, ansi.StringWidth(l)+4) // border and a space on each side
	}
	notice := strings.Join(lines, "\n")
	if w <= g.width && len(lines)+2 <= g.height {
		notice = strings.TrimSuffix(box(g.cfg.Theme, w, len(lines)+2, "", lines), "\n")
	}
	// Without room for all of it, it is cut.
	rows := strings.Split(lipgloss.Place(g.width, g.height, lipgloss.Center, lipgloss.Center, notice), "\n")
	rows = rows[:min(len(rows), g.height)]
	for i, r := range rows {
		rows[i] = ansi.Truncate(r, g.width, "")
	}
	return strings.Join(rows, "\n")
}
