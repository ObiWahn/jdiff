package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
// a commit; n/p step through commits from the deeper views.
type Log struct {
	cfg  *config.Config
	load LoadFunc

	commits []*commitEntry
	cursor  int
	top     int
	open    *commitEntry // commit whose files are shown, nil for the list
	flow    *Flow        // all commits one after the other

	width, height int
	msg           string
}

// NewLog returns the commit list for commits, newest first.
func NewLog(cfg *config.Config, commits []gitsrc.Commit, load LoadFunc) *Log {
	l := &Log{cfg: cfg, load: load}
	var parts []*flowPart
	for _, c := range commits {
		e := &commitEntry{Commit: c}
		l.commits = append(l.commits, e)
		parts = append(parts, &flowPart{commit: e, header: commitHeader(c)})
	}
	l.flow = newFlow(cfg, parts, l.app)
	if len(commits) == 0 {
		l.msg = "no commits"
	}
	if len(cfg.Theme.Warnings) > 0 {
		l.msg = "theme: " + strings.Join(cfg.Theme.Warnings, "; ")
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
		for _, name := range keyNames(k) {
			if cmd := l.key(name); cmd != nil {
				return l, cmd
			}
		}
	}
	return l, nil
}

func (l *Log) key(k string) tea.Cmd {
	l.msg = ""
	page := max(l.listHeight()-1, 1)
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "up", "k":
		l.moveCursor(-1)
	case "down", "j":
		l.moveCursor(1)
	case "pgup", "ctrl+b":
		l.moveCursor(-page)
	case "pgdown", "ctrl+f", " ":
		l.moveCursor(page)
	case "home", "g":
		l.moveCursor(-len(l.commits))
	case "end", "G":
		l.moveCursor(len(l.commits))
	case "enter":
		if l.cfg.Settings.Flow {
			l.showFlow(-1, -1, 0)
		} else {
			l.openCommit(l.cursor, "")
		}
	case "f":
		return toggleFlow
	}
	return nil
}

// flowShown reports whether the flow view is shown.
func (l *Log) flowShown() bool { return l.open != nil && l.cfg.Settings.Flow }

// showFlow shows the flow view on line of file of the commit under the
// cursor (see Flow.show) at screen row screenRow.
func (l *Log) showFlow(file, line, screenRow int) {
	l.open = l.commits[l.cursor]
	l.flow.width, l.flow.height = l.width, l.height
	l.flow.show(l.cursor, file, line, screenRow)
}

// toggleFlow switches between the flow view and the files of the commit
// under the cursor, keeping the cursor on its line and screen row.
func (l *Log) toggleFlow() {
	st := &l.cfg.Settings
	if !st.Flow {
		st.Flow = true
		switch {
		case l.open == nil:
			l.showFlow(-1, -1, 0)
		case l.open.app.open == nil:
			l.showFlow(l.open.app.cursor, -1, 0)
		default:
			app := l.open.app
			m := app.open.view
			l.showFlow(app.cursor, m.cursor, m.cursor-m.top)
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
		m.cursor = line
		m.top = line - screenRow
		m.scrollToCursor()
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
		e.app = newApp(l.cfg, files, true)
		e.app.title = e.Short + " " + e.Subject + ": "
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
		if dir > 0 {
			app.setMsg("oldest commit")
		} else {
			app.setMsg("newest commit")
		}
		return
	}
	if app.open != nil {
		app.open.updateStats()
	}
	if !l.openCommit(i, path) {
		app.setMsg(l.msg)
	}
}

func (l *Log) moveCursor(delta int) {
	l.cursor = max(min(l.cursor+delta, len(l.commits)-1), 0)
	l.scrollToCursor()
}

func (l *Log) listHeight() int { return max(l.height-2, 1) }

func (l *Log) scrollToCursor() {
	h := l.listHeight()
	if l.cursor < l.top {
		l.top = l.cursor
	} else if l.cursor >= l.top+h {
		l.top = l.cursor - h + 1
	}
	l.top = max(min(l.top, len(l.commits)-h), 0)
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
	addW, remW, authW := 1, 1, 0
	for _, c := range l.commits {
		addW = max(addW, len(fmt.Sprint(c.Added)))
		remW = max(remW, len(fmt.Sprint(c.Removed)))
		authW = max(authW, min(ansi.StringWidth(c.Author), maxAuthorWidth))
	}

	var b strings.Builder
	b.WriteString(ansi.Truncate(t.Title.Render(fmt.Sprintf(" %d commits", len(l.commits))), l.width, "…") + "\n")
	for i := l.top; i < l.top+l.listHeight(); i++ {
		if i < len(l.commits) {
			c := l.commits[i]
			hash := " " + c.Short + " "
			if i == l.cursor {
				hash = t.Cursor.Render(hash)
			}
			counts := t.Plus.Render(count("+", c.Added, addW+1)) + " " +
				t.Minus.Render(count("-", c.Removed, remW+1))
			author := fmt.Sprintf("%-*s", authW, ansi.Truncate(c.Author, maxAuthorWidth, "…"))
			line := hash + " " + t.Decoration.Render(c.Date) + " " + counts + "  " +
				t.Decoration.Render(author) + "  " + c.Subject
			b.WriteString(ansi.Truncate(line, l.width, "…"))
		}
		b.WriteString("\n")
	}
	status := l.msg
	if status == "" {
		status = "enter open · f flow · j/k move · q quit"
	}
	b.WriteString(ansi.Truncate(t.Hint.Render(status), l.width, ""))
	return b.String()
}
