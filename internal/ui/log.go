package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/gitsrc"
	"jdiff/internal/settings"
	"jdiff/internal/source"
	"jdiff/internal/theme"
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
	theme    *theme.Theme
	settings settings.Settings
	load     LoadFunc

	commits []*commitEntry
	cursor  int
	top     int
	open    *commitEntry // commit whose files are shown, nil for the list

	width, height int
	msg           string
}

// NewLog returns the commit list for commits, newest first.
func NewLog(th *theme.Theme, st settings.Settings, commits []gitsrc.Commit, load LoadFunc) *Log {
	l := &Log{theme: th, settings: st, load: load}
	for _, c := range commits {
		l.commits = append(l.commits, &commitEntry{Commit: c})
	}
	if len(commits) == 0 {
		l.msg = "no commits"
	}
	if len(th.Warnings) > 0 {
		l.msg = "theme: " + strings.Join(th.Warnings, "; ")
	}
	return l
}

func (l *Log) Init() tea.Cmd { return nil }

func (l *Log) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
		l.scrollToCursor()
	case backMsg:
		l.open = nil
		return l, nil
	case switchCommitMsg:
		l.switchCommit(int(msg))
		return l, nil
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
		l.openCommit(l.cursor, "")
	}
	return nil
}

// openCommit shows the files of commit i, opening path directly when the
// commit changed it, and the only file of a single-file commit.
func (l *Log) openCommit(i int, path string) bool {
	if i < 0 || i >= len(l.commits) {
		return false
	}
	e := l.commits[i]
	if e.app == nil {
		files, err := l.load(e.Commit)
		if err != nil {
			l.msg = err.Error()
			return false
		}
		e.app = NewApp(l.theme, l.settings, files)
		e.app.inLog = true
		e.app.title = e.Short + " " + e.Subject + ": "
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
	if l.open != nil {
		return l.open.app.View()
	}
	if l.width == 0 {
		return ""
	}
	t := l.theme
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
		status = "enter open · j/k move · q quit"
	}
	b.WriteString(ansi.Truncate(t.Hint.Render(status), l.width, ""))
	return b.String()
}
