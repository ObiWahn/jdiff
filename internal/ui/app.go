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

// entry is one file in the selector. Its diff is created on first use and
// its diff view on first open; both are kept, so anchors survive going back
// to the list.
type entry struct {
	source.File
	added, removed int
	diff           *fileDiff
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
	return [2]string{"a/" + e.PathInOld(), "b/" + e.Path}
}

// refresh reloads the sides of e that changed on disk, see refreshFile.
func (e *entry) refresh() string {
	if e.Binary {
		return ""
	}
	msg := refreshFile(&e.File, e.diff, e.view)
	if msg != "" {
		e.updateStats()
	}
	return msg
}

func (e *entry) updateStats() {
	if e.Binary {
		return
	}
	rows := diff.Align(e.Old, e.New, nil, diff.Options{})
	if e.diff != nil {
		rows = e.diff.rows
	}
	e.added, e.removed = diff.Stats(rows)
}

// App is the Bubble Tea model of the file selector. It shows the diff view
// of the selected file and returns to the list on backspace. A single file
// is opened directly.
type App struct {
	cfg    *config.Config
	commit *gitsrc.Commit // whose files are shown, nil outside the log
	files  []*entry
	list
	open *entry // file whose diff view is shown, nil for the list
	flow *Flow  // flow view outside the log, created on first use
	msg  string
}

// NewApp returns the file selector for files.
func NewApp(cfg *config.Config, files []source.File) *App { return newApp(cfg, files, nil) }

// newApp returns the file selector for files, as part of the log for the
// files of commit when it is set.
func newApp(cfg *config.Config, files []source.File, commit *gitsrc.Commit) *App {
	a := &App{cfg: cfg, commit: commit}
	// The files are searched for moved code together.
	group := &moveGroup{}
	for _, f := range files {
		e := &entry{File: f}
		if !e.Binary {
			group.add(a.diffOf(e), e.Path)
		}
		e.updateStats()
		a.files = append(a.files, e)
	}
	a.list = list{n: len(a.files), preview: &cfg.Settings.Preview}
	if len(files) == 0 {
		a.msg = "no changed files"
	}
	if w := cfg.Warning(); w != "" {
		a.msg = w
	}
	if a.single() {
		a.openFile()
	}
	return a
}

// inLog reports whether a is part of the log: backspace and N/P go to the
// log.
func (a *App) inLog() bool { return a.commit != nil }

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
	switch {
	case a.flowShown():
		a.flow.msg = msg
	case a.open != nil:
		a.open.view.msg = msg
	default:
		a.msg = msg
	}
}

// Init starts watching the files, outside the log.
func (a *App) Init() tea.Cmd {
	if a.inLog() {
		return nil
	}
	return watchTick(a.cfg)
}

// refresh reloads the files around the cursor that changed on disk: the
// current one and its neighbors.
func (a *App) refresh() {
	var msgs []string
	reload := func() {
		for i := max(a.cursor-1, 0); i <= min(a.cursor+1, len(a.files)-1); i++ {
			if msg := a.files[i].refresh(); msg != "" {
				msgs = append(msgs, msg)
			}
		}
	}
	if a.flowShown() {
		a.flow.refresh(reload)
	} else {
		reload()
	}
	if len(msgs) > 0 {
		a.setMsg(strings.Join(msgs, "; "))
	}
}

// flowShown reports whether the flow view is shown instead of a diff view.
// Inside the log, the log shows its own flow view.
func (a *App) flowShown() bool { return a.open != nil && a.cfg.Settings.Flow && !a.inLog() }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.scrollToCursor()
	case watchTickMsg:
		a.refresh()
		return a, watchTick(a.cfg)
	case switchFileMsg:
		a.switchFile(int(msg))
		return a, nil
	case toggleFlowMsg:
		a.toggleFlow()
		if w := saveState(a.cfg); w != "" {
			a.setMsg(w)
		}
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
			if a.inLog() && a.single() {
				return a, func() tea.Msg { return backMsg{} }
			}
			return a, nil
		}
		_, cmd := a.open.view.Update(msg)
		return a, cmd
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		return a, pressKeys(k, a.key)
	}
	return a, nil
}

func (a *App) key(k string) tea.Cmd {
	a.msg = ""
	page := max(a.listHeight()-1, 1)
	if delta, _, ok := motion(k, page, len(a.files)); ok {
		a.moveCursor(delta)
		return nil
	}
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "p":
		a.moveCursor(-1)
	case "n":
		a.moveCursor(1)
	case "enter":
		a.openFile()
	case "backspace":
		if a.inLog() {
			return func() tea.Msg { return backMsg{} }
		}
	case "N", "P":
		if a.inLog() {
			return switchCommit(k)
		}
	case "f":
		return toggleFlow
	case "v":
		a.msg = toggle(a.cfg, &a.cfg.Settings.Preview)
		a.scrollToCursor()
	case "s":
		a.msg = toggle(a.cfg, &a.cfg.Settings.SideBySide)
	case "w":
		a.msg = toggle(a.cfg, &a.cfg.Settings.ShowWhitespaces)
	}
	return nil
}

// diffOf returns the diff of e, creating it on first use.
func (a *App) diffOf(e *entry) *fileDiff {
	if e.diff == nil {
		e.diff = newFileDiff(a.cfg, e.paneNames(), [2][]string{e.Old, e.New})
	}
	return e.diff
}

// viewOf returns the diff view of e, creating it on first use.
func (a *App) viewOf(e *entry) *Model {
	if e.view == nil {
		e.view = newModel(a.diffOf(e))
		e.view.canBack = true
		e.view.commitNav = a.inLog()
	}
	return e.view
}

func (a *App) openFile() {
	if a.cursor >= len(a.files) {
		return
	}
	if a.cfg.Settings.Flow && !a.inLog() {
		a.showFlow(a.cursor, -1, 0, 0)
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

// showFlow shows the flow view on line sub of diff row line of file (its
// bar for line -1) at screen row screenRow.
func (a *App) showFlow(file, line, sub, screenRow int) {
	if a.flow == nil {
		a.flow = flowOfApp(a)
	}
	a.open = a.files[file]
	a.flow.width, a.flow.height = a.width, a.height
	a.flow.show(0, file, line, sub, screenRow)
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
			a.showFlow(a.cursor, -1, 0, 0)
		default:
			m := a.open.view
			m.syncLayout()
			a.showFlow(a.cursor, m.cursor, m.sub, m.pos()-m.top)
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
	a.open.view.takeOver(a.flow, line, screenRow)
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
	a.setMsg(ends(dir > 0, "last file", "first file"))
}

// preview renders the diff of e in a box height lines high, starting with
// the context of the first change.
func (a *App) preview(e *entry, height int) string {
	t := a.cfg.Theme
	title := lipgloss.NewStyle().Bold(true).Render(" " + e.name() + " ")
	if e.Binary {
		return box(t, a.width, height, title, []string{t.Hint.Render("binary file")})
	}
	d := a.diffOf(e)
	start := 0
	for v := range d.view {
		if d.changeStart(v) {
			start = max(v-a.cfg.Settings.Context, 0)
			break
		}
	}
	if len(d.view) == 0 { // empty on both sides
		return box(t, a.width, height, title, nil)
	}
	// The preview is scrolled like the file's diff view.
	lo := layout{width: max(a.width-4, 0), unified: d.unified()}
	if e.view != nil {
		lo.xoff, lo.active = e.view.xoff, e.view.active
	}
	screen, screenOf := d.screenLines(lo.unified)
	var lines []string
	for i := screenOf[start]; i < len(screen) && len(lines) < height-2; i++ {
		lines = append(lines, d.render(screen[i], false, lo))
	}
	return box(t, a.width, height, title, lines)
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

	// Styles are rendered side by side: lipgloss underlining breaks nested
	// escape sequences.
	bold := lipgloss.NewStyle().Bold(true)
	head := " " + bold.Render(fmt.Sprintf("%d files changed", len(a.files)))
	if c := a.commit; c != nil {
		head = " " + t.CommitHash.Render(c.Short) + " " + bold.Render(c.Subject)
	}
	head += "  " + t.Plus.Render(fmt.Sprintf("+%d", added)) + " " + t.Minus.Render(fmt.Sprintf("-%d", removed))

	item := func(i int, st func(lipgloss.Style) lipgloss.Style) string {
		e := a.files[i]
		cnt := counts(t, st, e.added, e.removed, addW, remW)
		if e.Binary {
			cnt = st(t.Filler).Render(fmt.Sprintf("%-*s", addW+remW+3, "binary"))
		}
		lbl := st(lipgloss.NewStyle()).Render(fmt.Sprintf(" %-*s ", lblW, e.label(t)))
		return lbl + " " + cnt + "  " + st(lipgloss.NewStyle()).Render(e.name())
	}
	status := a.msg
	if status == "" {
		status = "enter open · f flow · v preview · ? help · q quit"
		if a.inLog() {
			status = "enter open · N/P commit · backspace commits · ? help · q quit"
		}
	}
	return a.render(t, head, item, func(h int) string { return a.preview(a.files[a.cursor], h) }, status)
}
