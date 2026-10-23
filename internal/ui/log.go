package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/gitsrc"
	"jdiff/internal/source"
)

// LoadFunc returns the files a commit changed.
type LoadFunc func(gitsrc.Commit) ([]source.File, error)

// CountFunc returns the line counts of the commits with the given hashes.
type CountFunc func(hashes []string) (map[string]gitsrc.LineCounts, error)

// countsMsg brings the line counts of the commits hashes, or why they could
// not be counted.
type countsMsg struct {
	hashes []string
	counts map[string]gitsrc.LineCounts
	err    error
}

// countWidth is the width the count columns keep while counts arrive, so
// that they do not move with each batch.
const countWidth = 4

// commitEntry is one commit in the log. Its file selector is created on
// first open and kept, so anchors survive going back to the list.
type commitEntry struct {
	gitsrc.Commit
	counted bool // Added and Removed are known
	app     *App
}

// Log is the Bubble Tea model of the commit list. Enter shows the files of
// a commit; N/P step through commits from the deeper views.
type Log struct {
	cfg    *config.Config
	branch string // shown above the commit list
	load   LoadFunc
	// counts counts the lines of the commits shown, in the background; nil
	// when the commits came counted or counting failed.
	counts   CountFunc
	counting bool                    // a count is running
	byHash   map[string]*commitEntry // the commits by hash
	minCount int                     // the least width of the count columns

	commits []*commitEntry
	list
	open   *commitEntry // commit whose files are shown, nil for the list
	flow   *Flow        // all commits one after the other
	msg    string
	prefix string // "'" while waiting for the letter of a mark
}

// NewLog returns the commit list for commits, newest first, of branch. With
// counts, their line counts are counted once they are shown; without, they
// come with the commits.
func NewLog(cfg *config.Config, branch string, commits []gitsrc.Commit, load LoadFunc, counts CountFunc) *Log {
	l := &Log{cfg: cfg, branch: branch, load: load, counts: counts, byHash: map[string]*commitEntry{}, minCount: 1}
	if counts != nil {
		l.minCount = countWidth
	}
	var parts []*flowPart
	for _, c := range commits {
		// Commits are only shown: their text is made printable once.
		c.Author, c.Subject = printable(c.Author), printable(c.Subject)
		body := strings.Split(c.Body, "\n")
		for i, line := range body {
			body[i] = printable(line)
		}
		c.Body = strings.Join(body, "\n")
		e := &commitEntry{Commit: c, counted: counts == nil}
		l.commits = append(l.commits, e)
		l.byHash[c.Hash] = e
		parts = append(parts, newCommitPart(e))
	}
	l.list = list{n: len(l.commits), preview: &cfg.Settings.Preview}
	l.flow = newFlow(cfg, parts, l.app)
	if len(commits) == 0 {
		l.msg = "no commits"
	}
	if w := cfg.Warning(); w != "" {
		l.msg = w
	}
	return l
}

func (l *Log) Init() tea.Cmd { return nil }

func (l *Log) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(countsMsg); ok {
		l.addCounts(m)
	} else if cmd := l.update(msg); cmd != nil {
		return l, cmd
	}
	return l, l.countShown()
}

// countShown returns the command counting the lines of the commits on the
// list's screen and a screen beyond that are not counted yet, or nil when
// they are, a count is running or the list is not shown.
func (l *Log) countShown() tea.Cmd {
	if l.counts == nil || l.counting || l.open != nil || l.height == 0 {
		return nil
	}
	var hashes []string
	for i := l.top; i < min(l.top+2*l.listHeight(), len(l.commits)); i++ {
		if e := l.commits[i]; !e.counted {
			hashes = append(hashes, e.Hash)
		}
	}
	if len(hashes) == 0 {
		return nil
	}
	l.counting = true
	counts := l.counts
	return func() tea.Msg {
		c, err := counts(hashes)
		return countsMsg{hashes, c, err}
	}
}

// addCounts takes the line counts of m. After an error the counts are not
// tried again: the list stays without them.
func (l *Log) addCounts(m countsMsg) {
	l.counting = false
	if m.err != nil {
		l.counts = nil
		l.setMsg("line counts: " + m.err.Error())
		return
	}
	for _, h := range m.hashes {
		if e := l.byHash[h]; e != nil {
			c := m.counts[h]
			e.Added, e.Removed, e.counted = c.Added, c.Removed, true
		}
	}
}

func (l *Log) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
		l.scrollToCursor()
		if l.flowShown() {
			l.flow.Update(msg)
			return nil
		}
	case backMsg:
		l.open = nil
		return nil
	case switchCommitMsg:
		l.switchCommit(int(msg))
		return nil
	case toggleFlowMsg:
		l.toggleFlow()
		if w := saveState(l.cfg); w != "" {
			l.setMsg(w)
		}
		return nil
	}
	if l.flowShown() {
		_, cmd := l.flow.Update(msg)
		l.cursor, _, _ = l.flow.position()
		l.scrollToCursor()
		return cmd
	}
	if l.open != nil {
		_, cmd := l.open.app.Update(msg)
		return cmd
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		return pressKeys(k, l.key)
	}
	return nil
}

func (l *Log) key(k string) tea.Cmd {
	l.msg = ""
	if l.prefix != "" {
		// Marks are lines of files: the list has none to jump to.
		l.prefix = ""
		if isMarkLetter(k) {
			l.msg = "mark " + k + ": open a commit to jump to it"
		}
		return nil
	}
	page := max(l.listHeight()-1, 1)
	if delta, _, ok := motion(k, page, len(l.commits)); ok {
		l.moveCursor(delta)
		return nil
	}
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "enter":
		if l.cfg.Settings.Flow {
			l.showFlow(-1, nil)
		} else {
			l.openCommit(l.cursor, "")
		}
	case "f":
		return toggleFlow
	case "'":
		l.prefix, l.msg = k, "jump to mark: press its letter"
	case "v":
		l.msg = toggle(l.cfg, &l.cfg.Settings.Preview)
		l.scrollToCursor()
	}
	return nil
}

// flowShown reports whether the flow view is shown.
func (l *Log) flowShown() bool { return l.open != nil && l.cfg.Settings.Flow }

// showFlow shows the flow view on file of the commit under the cursor,
// where its diff view from leaves it, or on its bar for from nil and on the
// commit's bar for file -1 (see Flow.showFrom).
func (l *Log) showFlow(file int, from *Model) {
	l.open = l.commits[l.cursor]
	l.flow.width, l.flow.height = l.width, l.height
	l.flow.showFrom(l.cursor, file, from)
}

// toggleFlow switches between the flow view and the files of the commit
// under the cursor, keeping the cursor on its line and screen row.
func (l *Log) toggleFlow() {
	st := &l.cfg.Settings
	if !st.Flow {
		st.Flow = true
		switch {
		case l.open == nil:
			l.showFlow(-1, nil)
		case l.open.app.open == nil:
			l.showFlow(l.open.app.cursor, nil)
		default:
			l.showFlow(l.open.app.cursor, l.open.app.open.view)
		}
		return
	}
	st.Flow = false
	if l.open == nil {
		l.msg = "flow off"
		return
	}
	p, file, line := l.flow.position()
	screenRow := l.flow.cursor - l.flow.top
	path := ""
	if e := l.commits[p]; e.app != nil && file >= 0 {
		path = e.app.files[file].Path
	}
	l.open = nil
	if !l.openCommit(p, path) {
		return
	}
	app := l.open.app
	if app.open == nil && file >= 0 {
		app.cursor = file
		app.scrollToCursor()
	}
	if app.open != nil && app.open.Path == path {
		app.open.view.takeOver(l.flow, line, screenRow)
	}
}

// app returns the file selector of commit i, loading its files on first
// use.
func (l *Log) app(i int) (*App, error) {
	e := l.commits[i]
	if e.app == nil {
		files, err := l.load(e.Commit)
		if err != nil {
			return nil, err
		}
		e.app = newApp(l.cfg, files, &e.Commit)
	}
	return e.app, nil
}

// openCommit shows the files of commit i, opening path directly when the
// commit changed it, and the only file of a single-file commit.
func (l *Log) openCommit(i int, path string) bool {
	if i < 0 || i >= len(l.commits) {
		return false
	}
	e := l.commits[i]
	if _, err := l.app(i); err != nil {
		l.msg = err.Error()
		return false
	}
	e.app.Update(tea.WindowSizeMsg{Width: l.width, Height: l.height})
	switch {
	case path != "" && e.app.openPath(path):
	case e.app.single():
		e.app.cursor = 0
		e.app.openFile()
	default:
		e.app.open = nil
	}
	l.open = e
	l.cursor = i
	l.scrollToCursor()
	return true
}

// switchCommit shows the next (dir > 0, older) or previous commit, keeping
// the open file when the other commit changed it too.
func (l *Log) switchCommit(dir int) {
	if l.open == nil {
		return
	}
	app := l.open.app
	path := ""
	if app.open != nil {
		path = app.open.Path
	}
	i := l.cursor + dir
	if i < 0 || i >= len(l.commits) {
		app.setMsg(ends(dir > 0, "oldest commit", "newest commit"))
		return
	}
	if app.open != nil {
		app.open.updateStats()
	}
	if !l.openCommit(i, path) {
		app.setMsg(l.msg)
	}
}

// setMsg shows msg in the status bar of whatever is displayed.
func (l *Log) setMsg(msg string) {
	switch {
	case l.flowShown():
		l.flow.msg = msg
	case l.open != nil:
		l.open.app.setMsg(msg)
	default:
		l.msg = msg
	}
}

// maxAuthorWidth caps the author column.
const maxAuthorWidth = 16

func (l *Log) View() string {
	if l.flowShown() {
		return l.flow.View()
	}
	if l.open != nil {
		return l.open.app.View()
	}
	if l.width == 0 {
		return ""
	}
	t := l.cfg.Theme
	addW, remW, authW, subjW, fixedW := l.minCount, l.minCount, 0, 0, 0
	for _, c := range l.commits {
		addW = max(addW, len(fmt.Sprint(c.Added)))
		remW = max(remW, len(fmt.Sprint(c.Removed)))
		authW = max(authW, min(ansi.StringWidth(c.Author), maxAuthorWidth))
		subjW = max(subjW, ansi.StringWidth(c.Subject))
		fixedW = max(fixedW, ansi.StringWidth(c.Short)+ansi.StringWidth(c.Date))
	}
	// The subject gets the room left by the other columns, so that author
	// and date stay aligned and visible.
	fixedW += 1 + 2 + (addW + 1) + 1 + (remW + 1) + 2 + 2 + authW + 2
	subjW = max(min(subjW, l.width-fixedW), 1)

	head := " " + t.Branch.Render(l.branch) + " " +
		lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("(%d commits)", len(l.commits)))
	item := func(i int, st func(lipgloss.Style) lipgloss.Style) string {
		c := l.commits[i]
		author := fmt.Sprintf("%-*s", authW, ansi.Truncate(c.Author, maxAuthorWidth, "…"))
		subject := ansi.Truncate(c.Subject, subjW, "…")
		subject += strings.Repeat(" ", subjW-ansi.StringWidth(subject))
		return " " + st(t.CommitHash).Render(c.Short) + "  " + counts(t, st, c.Added, c.Removed, addW, remW) + "  " +
			st(lipgloss.NewStyle()).Render(subject) + "  " + st(t.Decoration).Render(author) + "  " +
			st(t.Decoration).Render(c.Date)
	}
	status := l.msg
	if status == "" {
		status = "enter open · f flow · v message · ? help · q quit"
	}
	return l.render(t, head, item, func(h int) string { return l.messageBox(l.commits[l.cursor].Commit, h) }, status)
}

// messageBox renders the full message of c in a box height lines high,
// with the hash in the top border. A last … line marks a cut message.
func (l *Log) messageBox(c gitsrc.Commit, height int) string {
	t := l.cfg.Theme
	lines := []string{t.Zero.Bold(true).Render(c.Subject)}
	if c.Body != "" {
		lines = append(lines, "")
		for line := range strings.SplitSeq(c.Body, "\n") {
			lines = append(lines, t.Zero.Render(line))
		}
	}
	if n := height - 2; len(lines) > n {
		lines = lines[:n]
		if n > 1 {
			lines[n-1] = t.Hint.Render("…")
		}
	}
	return box(t, l.width, height, t.CommitHash.Render(" "+c.Hash+" "), lines)
}
