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

// commitEntry is one commit in the log. Its file selector is created on
// first open and kept, so anchors survive going back to the list.
type commitEntry struct {
	gitsrc.Commit
	app *App
}

// Log is the Bubble Tea model of the commit list. Enter shows the files of
// a commit; N/P step through commits from the deeper views.
type Log struct {
	cfg    *config.Config
	branch string // shown above the commit list
	load   LoadFunc

	commits []*commitEntry
	list
	open *commitEntry // commit whose files are shown, nil for the list
	flow *Flow        // all commits one after the other
	msg  string
}

// NewLog returns the commit list for commits, newest first, of branch.
func NewLog(cfg *config.Config, branch string, commits []gitsrc.Commit, load LoadFunc) *Log {
	l := &Log{cfg: cfg, branch: branch, load: load}
	var parts []*flowPart
	for _, c := range commits {
		e := &commitEntry{Commit: c}
		l.commits = append(l.commits, e)
		parts = append(parts, &flowPart{commit: e, header: commitHeader(c)})
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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
		l.scrollToCursor()
		if l.flowShown() {
			l.flow.Update(msg)
			return l, nil
		}
	case backMsg:
		l.open = nil
		return l, nil
	case switchCommitMsg:
		l.switchCommit(int(msg))
		return l, nil
	case toggleFlowMsg:
		l.toggleFlow()
		if w := saveState(l.cfg); w != "" {
			l.setMsg(w)
		}
		return l, nil
	}
	if l.flowShown() {
		_, cmd := l.flow.Update(msg)
		l.cursor, _, _ = l.flow.position()
		l.scrollToCursor()
		return l, cmd
	}
	if l.open != nil {
		_, cmd := l.open.app.Update(msg)
		return l, cmd
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		return l, pressKeys(k, l.key)
	}
	return l, nil
}

func (l *Log) key(k string) tea.Cmd {
	l.msg = ""
	page := max(l.listHeight()-1, 1)
	if delta, ok := motion(k, page, len(l.commits)); ok {
		l.moveCursor(delta)
		return nil
	}
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "enter":
		if l.cfg.Settings.Flow {
			l.showFlow(-1, -1, 0, 0)
		} else {
			l.openCommit(l.cursor, "")
		}
	case "f":
		return toggleFlow
	case "v":
		l.msg = toggle(l.cfg, &l.cfg.Settings.Preview)
		l.scrollToCursor()
	}
	return nil
}

// flowShown reports whether the flow view is shown.
func (l *Log) flowShown() bool { return l.open != nil && l.cfg.Settings.Flow }

// showFlow shows the flow view on line sub of diff row line of file of the
// commit under the cursor (see Flow.show) at screen row screenRow.
func (l *Log) showFlow(file, line, sub, screenRow int) {
	l.open = l.commits[l.cursor]
	l.flow.width, l.flow.height = l.width, l.height
	l.flow.show(l.cursor, file, line, sub, screenRow)
}

// toggleFlow switches between the flow view and the files of the commit
// under the cursor, keeping the cursor on its line and screen row.
func (l *Log) toggleFlow() {
	st := &l.cfg.Settings
	if !st.Flow {
		st.Flow = true
		switch {
		case l.open == nil:
			l.showFlow(-1, -1, 0, 0)
		case l.open.app.open == nil:
			l.showFlow(l.open.app.cursor, -1, 0, 0)
		default:
			app := l.open.app
			m := app.open.view
			m.syncLayout()
			l.showFlow(app.cursor, m.cursor, m.sub, m.pos()-m.top)
			l.flow.active = m.active
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
	if app.open != nil && line >= 0 && app.open.Path == path {
		m := app.open.view
		m.active = l.flow.active
		m.place(line, l.flow.cursorSub(), screenRow)
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
	addW, remW, authW, subjW, fixedW := 1, 1, 0, 0, 0
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
