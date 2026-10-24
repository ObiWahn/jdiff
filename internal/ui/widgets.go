package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/theme"
)

// paneTitles renders names above the panes of a view width columns wide.
// Side by side the name of the active pane is bold, the other one faint; in
// the normal layout, one column of both, both are bold.
func paneTitles(t *theme.Theme, names [2]string, active side, sideBySide bool, width int) string {
	w := (width - 1) / 2
	title := func(s side) string {
		style := lipgloss.NewStyle().Bold(true)
		if sideBySide && s != active {
			style = lipgloss.NewStyle().Faint(true)
		}
		return pad(style.Render(ansi.Truncate(" "+names[s], w, "…")), w, lipgloss.NewStyle())
	}
	return title(left) + t.Decoration.Render("│") + title(right)
}

// statusLine renders the status bar with left and right aligned parts; left
// is shortened when both do not fit.
func statusLine(t *theme.Theme, width int, left, right string) string {
	left = ansi.Truncate(left, max(width-ansi.StringWidth(right), 0), "…")
	gap := max(width-ansi.StringWidth(left)-ansi.StringWidth(right), 0)
	return ansi.Truncate(t.Hint.Render(left+strings.Repeat(" ", gap)+right), width, "")
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
// high, with title in the top border. Lines that do not fit are cut.
func box(t *theme.Theme, width, height int, title string, lines []string) string {
	border := t.Decoration
	inner := max(width-4, 0) // border and a space on each side
	title = ansi.Truncate(title, inner, "")
	var b strings.Builder
	b.WriteString(border.Render("╭─") + title +
		border.Render(strings.Repeat("─", max(width-3-ansi.StringWidth(title), 0))+"╮") + "\n")
	for i := range height - 2 {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], inner, "…")
		}
		b.WriteString(border.Render("│") + " " + pad(line, inner, lipgloss.NewStyle()) + " " + border.Render("│") + "\n")
	}
	b.WriteString(border.Render("╰"+strings.Repeat("─", max(width-2, 0))+"╯") + "\n")
	return b.String()
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
	width, height int
}

// listHeight is the height of the list below the heading and an empty line.
func (li *list) listHeight() int {
	h, _ := splitList(li.height, li.n, *li.preview)
	return h
}

// boxHeight is the height of the box below the list, 0 when it is hidden.
func (li *list) boxHeight() int {
	_, h := splitList(li.height, li.n, *li.preview)
	return h
}

func (li *list) moveCursor(delta int) {
	li.cursor = max(min(li.cursor+delta, li.n-1), 0)
	li.scrollToCursor()
}

func (li *list) scrollToCursor() {
	li.top = scrollWindow(li.cursor, li.top, li.listHeight(), li.n)
}

// render renders the list: head, the shown items as drawn by item, which
// renders the selected one bold through st, the box as drawn by box when it
// is shown, and status.
func (li *list) render(t *theme.Theme, head string, item func(i int, st func(lipgloss.Style) lipgloss.Style) string,
	box func(height int) string, status string) string {
	var b strings.Builder
	b.WriteString(ansi.Truncate(head, li.width, "…") + "\n\n")
	for i := li.top; i < li.top+li.listHeight(); i++ {
		if i < li.n {
			st := func(s lipgloss.Style) lipgloss.Style { return s.Bold(i == li.cursor) }
			b.WriteString(ansi.Truncate(item(i, st), li.width, "…"))
		}
		b.WriteString("\n")
	}
	if h := li.boxHeight(); h > 0 && li.cursor < li.n {
		b.WriteString("\n" + box(h))
	}
	b.WriteString(ansi.Truncate(t.Hint.Render(status), li.width, ""))
	return b.String()
}

// DiffSwitch wraps the top-level view, whatever it shows: d asks for the
// arguments of git diff in a popup, whose files then take the view's place,
// as with jdiff diff. When git fails, the popup stays open and says why.
type DiffSwitch struct {
	tea.Model           // the view in its wrappers
	view      tea.Model // the view itself
	wrap      func(tea.Model) tea.Model
	cfg       *config.Config
	gitDiff   DiffFunc
	input     *lineInput // while it asks
	err       string     // why git failed, until the next key
	width     int
	height    int
}

// WithDiffSwitch returns the switch for view, which wrap puts in its
// wrappers, as it does the views that replace it; gitDiff runs git diff.
func WithDiffSwitch(cfg *config.Config, view tea.Model, wrap func(tea.Model) tea.Model, gitDiff DiffFunc) *DiffSwitch {
	return &DiffSwitch{Model: wrap(view), view: view, wrap: wrap, cfg: cfg, gitDiff: gitDiff}
}

// current returns the arguments of the git diff shown, and whether the view
// shows one.
func (s *DiffSwitch) current() ([]string, bool) {
	if a, ok := s.view.(*App); ok && a.gitDiff != nil {
		return a.diffArgs, true
	}
	return nil, false
}

func (s *DiffSwitch) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
	case askDiffMsg:
		s.input, s.err = &lineInput{}, ""
		return s, nil
	case tea.KeyMsg:
		if s.input != nil {
			s.err = ""
			return s, s.key(msg)
		}
	}
	var cmd tea.Cmd
	s.Model, cmd = s.Model.Update(msg)
	return s, cmd
}

// key edits the arguments asked for; all text typed goes into the line. Up
// takes the arguments of the git diff shown to edit. Enter shows the diff of
// the line; with nothing typed it closes the popup, as esc does.
func (s *DiffSwitch) key(k tea.KeyMsg) tea.Cmd {
	in := s.input
	switch k.Type {
	case tea.KeyCtrlC:
		return tea.Quit
	case tea.KeyEsc:
		s.input = nil
	case tea.KeyEnter:
		args := strings.Fields(in.text)
		if len(args) == 0 {
			s.input = nil
			return nil
		}
		return s.show(args)
	case tea.KeyUp:
		if args, ok := s.current(); ok {
			in.text = strings.Join(args, " ")
		}
	default:
		in.edit(k)
	}
	return nil
}

// show replaces the view by the files of git diff with args, from scratch,
// closes the popup and returns the command starting the new view. When git
// fails, the view and the popup stay, which shows the first line of git's
// message.
func (s *DiffSwitch) show(args []string) tea.Cmd {
	files, err := s.gitDiff(args)
	if err != nil {
		s.err, _, _ = strings.Cut(err.Error(), "\n")
		return nil
	}
	s.input = nil
	if v, ok := s.view.(interface{ stop() }); ok {
		v.stop()
	}
	a := NewApp(s.cfg, files).WithGitDiff(args, s.gitDiff)
	if len(files) > 0 {
		a.setMsg(fmt.Sprintf("%s: %d files", diffCommand(args), len(files)))
	}
	s.view, s.Model = a, s.wrap(a)
	s.Model.Update(tea.WindowSizeMsg{Width: s.width, Height: s.height})
	return s.Model.Init()
}

// minInputWidth is the least width of the line in the popup of d.
const minInputWidth = 40

// View draws the popup over the view while it asks: the line typed,
// centered, and why git failed. The popup grows with the line, up to the
// width of the screen; beyond, the line's start is cut so that its end
// stays in view.
func (s *DiffSwitch) View() string {
	base := s.Model.View()
	if s.input == nil || s.width == 0 {
		return base
	}
	t := s.cfg.Theme
	keys := " enter show · esc cancel "
	if _, ok := s.current(); ok {
		keys = " enter show · ↑ current · esc cancel "
	}
	title := t.CommitHash.Render(" git diff ") + t.Hint.Render(keys)
	text := printable(s.input.text) + "█"
	inner := max(minInputWidth, ansi.StringWidth(title), ansi.StringWidth(text))
	inner = min(inner, s.width-4) // borders and a space on each side
	if over := ansi.StringWidth(text) - inner; over > 0 {
		text = cutLeft(text, over)
	}
	lines := []string{"", strings.Repeat(" ", (inner-ansi.StringWidth(text))/2) + text, ""}
	if s.err != "" {
		lines = append(lines, t.Minus.Render(s.err))
	}
	return overlay(base, box(t, inner+4, len(lines)+2, title, lines), inner+4, s.width)
}

// lineInput is a line of text being typed, with the cursor at its end.
type lineInput struct{ text string }

// edit applies k to the text: typed and pasted text is added, backspace
// deletes a character, ctrl+w a word and ctrl+u the line.
func (in *lineInput) edit(k tea.KeyMsg) {
	switch k.Type {
	case tea.KeyRunes:
		in.text += string(k.Runes)
	case tea.KeySpace:
		in.text += " "
	case tea.KeyBackspace:
		if r := []rune(in.text); len(r) > 0 {
			in.text = string(r[:len(r)-1])
		}
	case tea.KeyCtrlW:
		in.text = strings.TrimRight(in.text, " ")
		in.text = in.text[:strings.LastIndex(in.text, " ")+1]
	case tea.KeyCtrlU:
		in.text = ""
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

// minWidth and minHeight are the smallest terminal the views are drawn in.
// Side by side the gutters of both panes take 7 columns and twice the digits
// of the line numbers, 19 for files of up to 999999 lines; the lists need a
// heading, an empty line, an item and the status bar.
const minWidth, minHeight = 20, 4

// SizeGuard wraps the top-level model. In a terminal smaller than minWidth
// by minHeight it shows that in a centered box instead of the page and
// ignores keys but ctrl+c, until the terminal is large enough again; the
// page still learns the size and gets all other messages.
type SizeGuard struct {
	tea.Model
	cfg           *config.Config
	width, height int
}

// WithSizeGuard returns m guarded against too small terminals.
func WithSizeGuard(cfg *config.Config, m tea.Model) *SizeGuard { return &SizeGuard{Model: m, cfg: cfg} }

func (g *SizeGuard) tooSmall() bool { return g.width < minWidth || g.height < minHeight }

func (g *SizeGuard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		g.width, g.height = msg.Width, msg.Height
	case tea.KeyMsg:
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

func (g *SizeGuard) View() string {
	if g.width == 0 || !g.tooSmall() {
		return g.Model.View()
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
