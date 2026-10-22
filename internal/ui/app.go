package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/diff"
	"jdiff/internal/settings"
	"jdiff/internal/source"
	"jdiff/internal/theme"
)

// entry is one file in the selector. Its diff view is created on first open
// and kept, so anchors survive going back to the list.
type entry struct {
	source.File
	added, removed int
	view           *Model
}

// label returns the status label, using delta's file labels.
func (e *entry) label(th *theme.Theme) string {
	switch {
	case !e.InOld:
		return th.AddedLabel
	case !e.InNew:
		return th.RemovedLabel
	}
	return th.ModifiedLabel
}

func (e *entry) updateStats() {
	if e.Binary {
		return
	}
	rows := diff.Align(e.Old, e.New, nil, diff.Options{})
	if e.view != nil {
		rows = e.view.rows
	}
	e.added, e.removed = diff.Stats(rows)
}

// App is the Bubble Tea model of the file selector. It shows the diff view
// of the selected file and returns to the list on backspace.
type App struct {
	theme    *theme.Theme
	settings settings.Settings
	files    []*entry
	cursor   int
	top      int
	open     *entry // file whose diff view is shown, nil for the list

	width, height int
	msg           string
}

// NewApp returns the file selector for files.
func NewApp(th *theme.Theme, st settings.Settings, files []source.File) *App {
	a := &App{theme: th, settings: st}
	for _, f := range files {
		e := &entry{File: f}
		e.updateStats()
		a.files = append(a.files, e)
	}
	if len(files) == 0 {
		a.msg = "no changed files"
	}
	if len(th.Warnings) > 0 {
		a.msg = "theme: " + strings.Join(th.Warnings, "; ")
	}
	return a
}

func (a *App) Init() tea.Cmd { return nil }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		a.width, a.height = sz.Width, sz.Height
		a.scrollToCursor()
	}
	if dir, ok := msg.(switchFileMsg); ok {
		a.switchFile(int(dir))
		return a, nil
	}
	if a.open != nil {
		if k, ok := msg.(tea.KeyMsg); ok && k.String() == "backspace" {
			a.open.updateStats()
			a.open = nil
			return a, nil
		}
		_, cmd := a.open.view.Update(msg)
		return a, cmd
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		for _, name := range keyNames(k) {
			if cmd := a.key(name); cmd != nil {
				return a, cmd
			}
		}
	}
	return a, nil
}

func (a *App) key(k string) tea.Cmd {
	a.msg = ""
	page := max(a.listHeight()-1, 1)
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "up", "k":
		a.moveCursor(-1)
	case "down", "j":
		a.moveCursor(1)
	case "pgup", "ctrl+b":
		a.moveCursor(-page)
	case "pgdown", "ctrl+f", " ":
		a.moveCursor(page)
	case "home", "g":
		a.moveCursor(-len(a.files))
	case "end", "G":
		a.moveCursor(len(a.files))
	case "enter":
		a.openFile()
	}
	return nil
}

func (a *App) openFile() {
	if a.cursor >= len(a.files) {
		return
	}
	e := a.files[a.cursor]
	if e.Binary {
		a.msg = e.Path + " is a binary file"
		return
	}
	if e.view == nil {
		e.view = New(a.theme, a.settings, "a/"+e.Path, "b/"+e.Path, e.Old, e.New)
		e.view.canBack = true
	}
	e.view.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.open = e
}

// switchFile opens the next (dir > 0) or previous file that can be shown.
func (a *App) switchFile(dir int) {
	for i := a.cursor + dir; i >= 0 && i < len(a.files); i += dir {
		if !a.files[i].Binary {
			a.open.updateStats()
			a.cursor = i
			a.scrollToCursor()
			a.openFile()
			return
		}
	}
	if dir > 0 {
		a.open.view.msg = "last file"
	} else {
		a.open.view.msg = "first file"
	}
}

func (a *App) moveCursor(delta int) {
	a.cursor = max(min(a.cursor+delta, len(a.files)-1), 0)
	a.scrollToCursor()
}

func (a *App) listHeight() int { return max(a.height-2, 1) }

func (a *App) scrollToCursor() {
	h := a.listHeight()
	if a.cursor < a.top {
		a.top = a.cursor
	} else if a.cursor >= a.top+h {
		a.top = a.cursor - h + 1
	}
	a.top = max(min(a.top, len(a.files)-h), 0)
}

func (a *App) View() string {
	if a.open != nil {
		return a.open.view.View()
	}
	if a.width == 0 {
		return ""
	}

	t := a.theme
	var added, removed int
	addW, remW, lblW := 1, 1, 0
	for _, e := range a.files {
		added, removed = added+e.added, removed+e.removed
		addW = max(addW, len(fmt.Sprint(e.added)))
		remW = max(remW, len(fmt.Sprint(e.removed)))
		lblW = max(lblW, ansi.StringWidth(e.label(t)))
	}

	var b strings.Builder
	// Styles are rendered side by side: lipgloss underlining breaks nested
	// escape sequences.
	head := t.Title.Render(fmt.Sprintf(" %d files changed", len(a.files))) + ", " +
		t.Plus.Render(fmt.Sprintf("+%d", added)) + ", " + t.Minus.Render(fmt.Sprintf("-%d", removed))
	b.WriteString(ansi.Truncate(head, a.width, "…") + "\n")

	for i := a.top; i < a.top+a.listHeight(); i++ {
		if i < len(a.files) {
			e := a.files[i]
			counts := t.Plus.Render(count("+", e.added, addW+1)) + " " +
				t.Minus.Render(count("-", e.removed, remW+1))
			if e.Binary {
				counts = t.Filler.Render(fmt.Sprintf("%-*s", addW+remW+3, "binary"))
			}
			lbl := fmt.Sprintf(" %-*s ", lblW, e.label(t))
			if i == a.cursor {
				lbl = t.Cursor.Render(lbl)
			}
			b.WriteString(ansi.Truncate(lbl+" "+counts+"  "+e.Path, a.width, "…"))
		}
		b.WriteString("\n")
	}

	status := a.msg
	if status == "" {
		status = "enter open · backspace back to list · j/k move · q quit"
	}
	b.WriteString(ansi.Truncate(t.Hint.Render(status), a.width, ""))
	return b.String()
}

// count formats n with sign right aligned to width, or blanks when n is 0.
func count(sign string, n, width int) string {
	if n == 0 {
		return strings.Repeat(" ", width)
	}
	return fmt.Sprintf("%*s", width, sign+fmt.Sprint(n))
}
