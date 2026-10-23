package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/gitsrc"
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
	case e.OldPath != "":
		return th.RenamedLabel
	case !e.InOld:
		return th.AddedLabel
	case !e.InNew:
		return th.RemovedLabel
	}
	return th.ModifiedLabel
}

// name returns the path shown for the file, "old → new" for renames.
func (e *entry) name() string {
	if e.OldPath != "" {
		return e.OldPath + " → " + e.Path
	}
	return e.Path
}

// paneNames returns the names shown above the panes of the file's diff.
func (e *entry) paneNames() [2]string {
	oldPath := e.Path
	if e.OldPath != "" {
		oldPath = e.OldPath
	}
	return [2]string{"a/" + oldPath, "b/" + e.Path}
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
// of the selected file and returns to the list on backspace. A single file
// is opened directly.
type App struct {
	cfg    *config.Config
	inLog  bool           // part of the log: backspace and N/P go to the log
	commit *gitsrc.Commit // whose files are shown, nil outside the log
	files  []*entry
	cursor int
	top    int
	open   *entry // file whose diff view is shown, nil for the list
	flow   *Flow  // flow view outside the log, created on first use

	width, height int
	msg           string
}

// NewApp returns the file selector for files.
func NewApp(cfg *config.Config, files []source.File) *App { return newApp(cfg, files, nil) }

// newApp returns the file selector for files, as part of the log for the
// files of commit when it is set.
func newApp(cfg *config.Config, files []source.File, commit *gitsrc.Commit) *App {
	a := &App{cfg: cfg, inLog: commit != nil, commit: commit}
	for _, f := range files {
		e := &entry{File: f}
		e.updateStats()
		a.files = append(a.files, e)
	}
	if len(files) == 0 {
		a.msg = "no changed files"
	}
	if len(cfg.Theme.Warnings) > 0 {
		a.msg = "theme: " + strings.Join(cfg.Theme.Warnings, "; ")
	}
	if a.single() {
		a.openFile()
	}
	return a
}

// single reports whether there is exactly one file and it can be shown.
func (a *App) single() bool { return len(a.files) == 1 && !a.files[0].Binary }

// openPath opens the file with the given path and reports whether it could.
func (a *App) openPath(path string) bool {
	for i, e := range a.files {
		if e.Path == path && !e.Binary {
			a.cursor = i
			a.scrollToCursor()
			a.openFile()
			return true
		}
	}
	return false
}

// setMsg shows msg in the status bar of whatever is displayed.
func (a *App) setMsg(msg string) {
	if a.open != nil {
		a.open.view.msg = msg
	} else {
		a.msg = msg
	}
}

func (a *App) Init() tea.Cmd { return nil }

// flowShown reports whether the flow view is shown instead of a diff view.
// Inside the log, the log shows its own flow view.
func (a *App) flowShown() bool { return a.open != nil && a.cfg.Settings.Flow && !a.inLog }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.scrollToCursor()
	case switchFileMsg:
		a.switchFile(int(msg))
		return a, nil
	case toggleFlowMsg:
		a.toggleFlow()
		return a, nil
	case backMsg: // from the flow view
		a.open = nil
		return a, nil
	}
	if a.flowShown() {
		_, cmd := a.flow.Update(msg)
		if _, file, _ := a.flow.position(); file >= 0 {
			a.cursor = file
			a.scrollToCursor()
		}
		return a, cmd
	}
	if a.open != nil {
		if k, ok := msg.(tea.KeyMsg); ok && k.String() == "backspace" {
			a.open.updateStats()
			a.open = nil
			if a.inLog && a.single() {
				return a, func() tea.Msg { return backMsg{} }
			}
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
	case "up", "k", "p":
		a.moveCursor(-1)
	case "down", "j", "n":
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
	case "backspace":
		if a.inLog {
			return func() tea.Msg { return backMsg{} }
		}
	case "N", "P":
		if a.inLog {
			return switchCommit(k)
		}
	case "f":
		return toggleFlow
	case "v":
		a.cfg.Settings.Preview = !a.cfg.Settings.Preview
		a.scrollToCursor()
	}
	return nil
}

// viewOf returns the diff view of e, creating it on first use.
func (a *App) viewOf(e *entry) *Model {
	if e.view == nil {
		names := e.paneNames()
		e.view = New(a.cfg, names[left], names[right], e.Old, e.New)
		e.view.canBack = true
		e.view.commitNav = a.inLog
	}
	return e.view
}

func (a *App) openFile() {
	if a.cursor >= len(a.files) {
		return
	}
	if a.cfg.Settings.Flow && !a.inLog {
		a.showFlow(a.cursor, -1, 0)
		return
	}
	e := a.files[a.cursor]
	if e.Binary {
		a.msg = e.Path + " is a binary file"
		return
	}
	a.viewOf(e).Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.open = e
}

// showFlow shows the flow view on line of file (its bar for line -1) at
// screen row screenRow.
func (a *App) showFlow(file, line, screenRow int) {
	if a.flow == nil {
		a.flow = flowOfApp(a)
	}
	a.open = a.files[file]
	a.flow.width, a.flow.height = a.width, a.height
	a.flow.show(0, file, line, screenRow)
}

// toggleFlow switches between the diff view of the file under the cursor
// and the flow view, keeping the cursor on its line and screen row.
func (a *App) toggleFlow() {
	st := &a.cfg.Settings
	if !st.Flow {
		st.Flow = true
		switch {
		case len(a.files) == 0:
		case a.open == nil:
			a.showFlow(a.cursor, -1, 0)
		default:
			m := a.open.view
			a.showFlow(a.cursor, m.cursor, m.cursor-m.top)
			a.flow.active = m.active
		}
		return
	}
	st.Flow = false
	if a.open == nil {
		a.msg = "flow off"
		return
	}
	_, file, line := a.flow.position()
	screenRow := a.flow.cursor - a.flow.top
	a.open = nil
	if file < 0 {
		return
	}
	a.cursor = file
	a.scrollToCursor()
	if a.files[file].Binary {
		return
	}
	a.openFile()
	if m := a.open.view; line >= 0 {
		m.active = a.flow.active
		m.cursor = line
		m.top = line - screenRow
		m.scrollToCursor()
	}
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
		a.setMsg("last file")
	} else {
		a.setMsg("first file")
	}
}

func (a *App) moveCursor(delta int) {
	a.cursor = max(min(a.cursor+delta, len(a.files)-1), 0)
	a.scrollToCursor()
}

// listHeight is the height of the list below the heading and an empty line.
func (a *App) listHeight() int {
	list, _ := splitList(a.height, len(a.files), a.cfg.Settings.Preview)
	return list
}

// boxHeight is the height of the diff preview below the list, 0 when it is
// hidden.
func (a *App) boxHeight() int {
	_, box := splitList(a.height, len(a.files), a.cfg.Settings.Preview)
	return box
}

// preview renders the diff of e in a box height lines high, starting with
// the context of the first change.
func (a *App) preview(e *entry, height int) string {
	t := a.cfg.Theme
	title := lipgloss.NewStyle().Bold(true).Render(" " + e.name() + " ")
	if e.Binary {
		return box(t, a.width, height, title, []string{t.Hint.Render("binary file")})
	}
	m := a.viewOf(e)
	m.width = max(a.width-4, 0)
	start := 0
	for v := range m.view {
		if m.changeStart(v) {
			start = max(v-a.cfg.Settings.Context, 0)
			break
		}
	}
	var lines []string
	for v := start; v < len(m.view) && len(lines) < height-2; v++ {
		lines = append(lines, m.bodyRow(v, false))
	}
	return box(t, a.width, height, title, lines)
}

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
	if a.flowShown() {
		return a.flow.View()
	}
	if a.open != nil {
		return a.open.view.View()
	}
	if a.width == 0 {
		return ""
	}

	t := a.cfg.Theme
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
	bold := lipgloss.NewStyle().Bold(true)
	head := " " + bold.Render(fmt.Sprintf("%d files changed", len(a.files)))
	if c := a.commit; c != nil {
		head = " " + t.CommitHash.Render(c.Short) + " " + bold.Render(c.Subject)
	}
	head += "  " + t.Plus.Render(fmt.Sprintf("+%d", added)) + " " + t.Minus.Render(fmt.Sprintf("-%d", removed))
	b.WriteString(ansi.Truncate(head, a.width, "…") + "\n\n")

	for i := a.top; i < a.top+a.listHeight(); i++ {
		if i < len(a.files) {
			e := a.files[i]
			// The selected file is drawn bold.
			st := func(s lipgloss.Style) lipgloss.Style { return s.Bold(i == a.cursor) }
			counts := st(t.Plus).Render(count("+", e.added, addW+1)) + " " +
				st(t.Minus).Render(count("-", e.removed, remW+1))
			if e.Binary {
				counts = st(t.Filler).Render(fmt.Sprintf("%-*s", addW+remW+3, "binary"))
			}
			lbl := st(lipgloss.NewStyle()).Render(fmt.Sprintf(" %-*s ", lblW, e.label(t)))
			b.WriteString(ansi.Truncate(lbl+" "+counts+"  "+st(lipgloss.NewStyle()).Render(e.name()), a.width, "…"))
		}
		b.WriteString("\n")
	}
	if h := a.boxHeight(); h > 0 && a.cursor < len(a.files) {
		b.WriteString("\n" + a.preview(a.files[a.cursor], h))
	}

	status := a.msg
	if status == "" {
		status = "enter open · f flow · v preview · j/k move · q quit"
		if a.inLog {
			status = "enter open · f flow · v preview · j/k move · N/P commit · backspace commits · q quit"
		}
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
