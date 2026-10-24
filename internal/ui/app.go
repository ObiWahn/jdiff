package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/gitsrc"
	"jdiff/internal/notify"
	"jdiff/internal/source"
	"jdiff/internal/theme"
)

// entry is one file in the selector. Its diff is created with the selector,
// since moved code is searched in all files together, and its diff view on
// first open; both are kept, so anchors survive going back to the list.
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

// refresh reloads the sides of e that changed on disk, see refreshFile. Its
// diff view and after, unless nil, follow each reload of its diff.
func (e *entry) refresh(after func(*fileDiff)) string {
	if e.Binary {
		return ""
	}
	msg := refreshFile(&e.File, e.diff, func() {
		if e.view != nil {
			e.view.syncLayout()
		}
		if after != nil {
			after(e.diff)
		}
	})
	if msg != "" {
		e.updateStats()
	}
	return msg
}

// update takes f, the file listed again, into e, reloading the sides of
// its diff whose content changed. Its diff view and after, unless nil,
// follow each reload, as in refresh. It returns the anchors dropped and
// whether the content changed.
func (e *entry) update(f source.File, after func(*fileDiff)) (dropped int, changed bool) {
	was := [2][]string{e.Old, e.New}
	e.File = f
	if e.Binary {
		return 0, false
	}
	e.diff.names = e.paneNames()
	for s, lines := range [2][]string{f.Old, f.New} {
		if slices.Equal(was[s], lines) {
			continue
		}
		changed = true
		dropped += e.diff.reload(side(s), lines)
		if e.view != nil {
			e.view.syncLayout()
		}
		if after != nil {
			after(e.diff)
		}
	}
	return dropped, changed
}

func (e *entry) updateStats() {
	if e.Binary {
		return
	}
	if e.diff != nil {
		e.added, e.removed = diff.Stats(e.diff.rows)
		return
	}
	e.added, e.removed = diff.Stats(diff.Align(e.Old, e.New, nil, diff.Options{}))
}

// App is the Bubble Tea model of the file selector. It shows the diff view
// of the selected file and returns to the list on backspace. A single file
// is opened directly.
type App struct {
	cfg    *config.Config
	commit *gitsrc.Commit // whose files are shown, nil outside the log
	files  []*entry
	relist ListFunc // lists the files again, nil when they cannot be
	// gitDiff runs git diff with arguments, nil outside jdiff diff; diffArgs
	// are those of the files shown.
	gitDiff  DiffFunc
	diffArgs []string
	// notify reports changes to the files on disk; nil while they are
	// checked every watch_interval, ticking while such a check is pending.
	notify  *notify.Watcher
	ticking bool
	list
	open   *entry // file whose diff view is shown, nil for the list
	flow   *Flow  // flow view outside the log, created on first use
	msg    string
	prefix string // "'" while waiting for the letter of a mark
}

// NewApp returns the file selector for files.
func NewApp(cfg *config.Config, files []source.File) *App { return newApp(cfg, files, nil) }

// ListFunc lists the files of a comparison again.
type ListFunc func() ([]source.File, error)

// DiffFunc returns the files git diff compares with args.
type DiffFunc func(args []string) ([]source.File, error)

// WithGitDiff makes a the files of git diff with args, which R runs again,
// and returns a.
func (a *App) WithGitDiff(args []string, gitDiff DiffFunc) *App {
	a.gitDiff, a.diffArgs = gitDiff, args
	a.relist = func() ([]source.File, error) { return a.gitDiff(a.diffArgs) }
	return a
}

// WithRelist sets how R lists the files again and returns a.
func (a *App) WithRelist(list ListFunc) *App {
	a.relist = list
	return a
}

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

// stop stops watching the files: the view is replaced.
func (a *App) stop() {
	if a.notify != nil {
		a.notify.Close()
	}
}

// diffCommand returns the git diff command line of args.
func diffCommand(args []string) string {
	return strings.TrimSpace("git diff " + strings.Join(args, " "))
}

// inLog reports whether a is part of the log: backspace and N/P go to the
// log.
func (a *App) inLog() bool { return a.commit != nil }

// single reports whether there is exactly one file and it can be shown.
func (a *App) single() bool { return len(a.files) == 1 && !a.files[0].Binary }

// openPath opens the file with the given path and reports whether it could.
func (a *App) openPath(path string) bool {
	i := a.fileByPath(path)
	if i >= 0 {
		a.openIndex(i)
	}
	return i >= 0
}

// fileByPath returns the index of the file with the given path that can be
// shown, or -1.
func (a *App) fileByPath(path string) int {
	return slices.IndexFunc(a.files, func(e *entry) bool { return e.Path == path && !e.Binary })
}

// fileByGroup returns the index of the file at index i of the files searched
// for moved code, or -1.
func (a *App) fileByGroup(i int) int {
	return slices.IndexFunc(a.files, func(e *entry) bool { return e.diff != nil && e.diff.groupIndex == i })
}

// openIndex opens file i, after updating the counts of the file it leaves.
func (a *App) openIndex(i int) {
	if a.open != nil {
		a.open.updateStats()
	}
	a.cursor = i
	a.scrollToCursor()
	a.openFile()
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
	return a.watch()
}

// watch starts watching the files on disk anew, by inotify when it can, and
// returns the command waiting for their changes. A pending check of polling
// is not started twice.
func (a *App) watch() tea.Cmd {
	if a.notify != nil {
		a.notify.Close()
	}
	var paths []string
	for _, e := range a.files {
		paths = append(paths, diskPaths(e.File)...)
	}
	w, cmd, msg := startWatching(a.cfg, a, paths)
	a.notify = w
	if msg != "" {
		a.setMsg(msg)
	}
	if w == nil && cmd != nil {
		if a.ticking {
			return nil
		}
		a.ticking = true
	}
	return cmd
}

// refresh reloads the files i that changed on disk for which which holds.
func (a *App) refresh(which func(i int) bool) {
	var msgs []string
	reload := func(after func(*fileDiff)) {
		for i := range a.files {
			if !which(i) {
				continue
			}
			if msg := a.files[i].refresh(after); msg != "" {
				msgs = append(msgs, msg)
			}
		}
	}
	if a.flowShown() {
		a.flow.refresh(reload)
	} else {
		reload(nil)
	}
	if len(msgs) > 0 {
		a.setMsg(strings.Join(msgs, "; "))
	}
}

// relistFiles lists the files again (R). New files are added and gone ones
// dropped; a file still there keeps its entry, and so its diff with anchors
// and folds, reloaded where its content changed. A file that became binary
// or stopped being binary counts as gone and new. The cursor stays on its
// file; when the file shown is gone, the list is shown. The files are
// watched anew; it returns the command waiting for their changes.
func (a *App) relistFiles() tea.Cmd {
	if a.relist == nil {
		a.setMsg("nothing to load again")
		return nil
	}
	files, err := a.relist()
	if err != nil {
		a.setMsg(err.Error())
		return nil
	}
	kept := map[string]*entry{}
	for _, e := range a.files {
		kept[e.Path] = e
	}
	var cur *entry
	if a.cursor < len(a.files) {
		cur = a.files[a.cursor]
	}
	added, changed, dropped := 0, 0, 0
	update := func(after func(*fileDiff)) {
		old := a.files
		a.files = nil
		// The files are searched for moved code together.
		group := &moveGroup{}
		for _, f := range files {
			e := kept[f.Path]
			if e == nil || e.Binary != f.Binary {
				e = &entry{File: f}
				added++
			} else {
				delete(kept, f.Path)
				n, ok := e.update(f, after)
				if ok {
					changed++
				}
				dropped += n
			}
			if !e.Binary {
				group.add(a.diffOf(e), e.Path)
			}
			e.updateStats()
			a.files = append(a.files, e)
		}
		if a.flow != nil {
			a.flow.followFiles(0, old, a.files)
		}
	}
	shown := a.flowShown()
	if shown {
		a.flow.refresh(update)
	} else {
		update(nil)
	}
	a.list.n = len(a.files)
	if i := slices.Index(a.files, cur); i >= 0 {
		a.cursor = i
	} else {
		a.cursor = max(min(a.cursor, len(a.files)-1), 0)
	}
	msg := refreshed(added, len(kept), changed, dropped)
	switch {
	case len(a.files) == 0:
		a.open = nil
	case shown:
		if _, file, _ := a.flow.position(); file >= 0 {
			a.cursor = file
		}
		a.open = a.files[a.cursor]
	case a.open != nil && !slices.Contains(a.files, a.open):
		msg = a.open.Path + " is gone; " + msg
		a.open = nil
	}
	a.scrollToCursor()
	a.setMsg(msg)
	if a.inLog() {
		return nil
	}
	return a.watch()
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
		if msg.owner != nil && msg.owner != a {
			return a, nil
		}
		a.ticking = false
		if a.notify != nil {
			return a, nil // inotify took over
		}
		// Polling checks the current file and its neighbors.
		a.refresh(func(i int) bool { return i >= a.cursor-1 && i <= a.cursor+1 })
		a.ticking = true
		return a, watchTick(a.cfg, a)
	case changedMsg:
		if msg.w != a.notify {
			return a, nil
		}
		changed := func(i int) bool {
			return slices.ContainsFunc(diskPaths(a.files[i].File), func(p string) bool { return slices.Contains(msg.paths, p) })
		}
		a.refresh(changed)
		return a, waitChanges(a.notify)
	case jumpMsg:
		a.jump(msg)
		return a, nil
	case markJumpMsg:
		a.gotoMark(msg.letter, msg.screenRow)
		return a, nil
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
	case refreshMsg:
		return a, a.relistFiles()
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
	if a.prefix != "" {
		a.prefix = ""
		if isMarkLetter(k) {
			a.gotoMark(k, (a.height-2)/2)
		}
		return nil
	}
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
		if a.inLog() && a.cfg.Settings.Flow && a.cursor < len(a.files) {
			file := a.cursor
			return func() tea.Msg { return flowAtMsg(file) }
		}
		a.openFile()
	case "backspace":
		if a.inLog() {
			return func() tea.Msg { return backMsg{} }
		}
	case "N", "P":
		if a.inLog() {
			return switchCommit(k)
		}
	case "R", "f5":
		return refreshAll
	case diffKey:
		return askDiff
	case "f":
		return toggleFlow
	case "'":
		a.prefix, a.msg = k, "jump to mark: press its letter"
	case "v":
		a.msg = toggle(a.cfg, &a.cfg.Settings.Preview)
		a.scrollToCursor()
	case "s":
		a.msg = toggle(a.cfg, &a.cfg.Settings.SideBySide)
	case "w":
		a.msg = toggle(a.cfg, &a.cfg.Settings.ShowWhitespaces)
	case "W":
		a.msg = toggle(a.cfg, &a.cfg.Settings.MarkWhitespace)
	case "t":
		a.msg = toggle(a.cfg, &a.cfg.Settings.ShowTrailing)
	case "e":
		a.msg = toggle(a.cfg, &a.cfg.Settings.ShowCR)
	case "b":
		a.msg = toggle(a.cfg, &a.cfg.Settings.BoldChanges)
	case "r":
		a.msg = toggle(a.cfg, &a.cfg.Settings.Wrap)
	}
	return nil
}

// diffOf returns the diff of e, creating it on first use.
func (a *App) diffOf(e *entry) *fileDiff {
	if e.diff == nil {
		e.diff = newFileDiff(a.cfg, e.paneNames(), [2][]string{e.Old, e.New})
		e.diff.markPath = e.Path
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
		a.showFlow(a.cursor, nil)
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

// showFlow shows the flow view on file, where its diff view from leaves
// it, or on its bar for from nil (see Flow.showFrom).
func (a *App) showFlow(file int, from *Model) {
	if a.flow == nil {
		a.flow = flowOfApp(a)
	}
	a.open = a.files[file]
	a.flow.width, a.flow.height = a.width, a.height
	a.flow.showFrom(0, file, from)
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
			a.showFlow(a.cursor, nil)
		default:
			a.showFlow(a.cursor, a.open.view)
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

// gotoMark opens the file of mark letter, when it is in the diff, with the
// cursor on its line at screen row screenRow.
func (a *App) gotoMark(letter string, screenRow int) {
	mk, ok := a.cfg.Mark(letter)
	if !ok {
		a.setMsg("no mark " + letter)
		return
	}
	i := a.fileByPath(mk.Path)
	if i < 0 {
		a.setMsg(notInDiff(letter, mk))
		return
	}
	a.openIndex(i)
	if a.flowShown() {
		a.flow.gotoMark(letter)
		return
	}
	if at, ok := a.open.view.markSpot(letter, mk); ok {
		a.open.view.show(at, screenRow)
	}
}

// jump opens the file of j's end of moved code with the cursor on its line.
func (a *App) jump(j jumpMsg) {
	i := a.fileByGroup(j.end.file)
	if i < 0 {
		return
	}
	a.openIndex(i)
	a.open.view.show(a.open.diff.spotOf(j.end.side, j.end.line, a.open.view.shape()), j.screenRow)
	a.open.view.msg = j.msg
}

// switchFile opens the next (dir > 0) or previous file that can be shown.
func (a *App) switchFile(dir int) {
	for i := a.cursor + dir; i >= 0 && i < len(a.files); i += dir {
		if !a.files[i].Binary {
			a.openIndex(i)
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
	if len(d.view) == 0 { // empty on both sides
		return box(t, a.width, height, title, nil)
	}
	// The preview is scrolled like the file's diff view.
	lo := layout{width: max(a.width-4, 0)}
	lo.shape = newShape(a.cfg, lo.width)
	if e.view != nil {
		lo.xoff, lo.active = e.view.xoff, e.view.active
	}
	screen, at := d.screenLines(lo.shape)
	from := 0
	for i := range screen {
		if d.blockStart(screen, i) {
			// The rows of context before the block, else its first line.
			from = min(at[sline{max(screen[i].v-a.cfg.Settings.Context, 0), 0}], i)
			break
		}
	}
	var lines []string
	for i := from; i < len(screen) && len(lines) < height-2; i++ {
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
	if a.gitDiff != nil {
		head += "  " + t.Hint.Render(diffCommand(a.diffArgs))
	}

	item := func(i int, st func(lipgloss.Style) lipgloss.Style) string {
		e := a.files[i]
		cnt := counts(t, st, e.added, e.removed, addW, remW)
		if e.Binary {
			cnt = st(t.Filler).Render(fmt.Sprintf("%-*s", addW+remW+3, "binary"))
		}
		lbl := st(lipgloss.NewStyle()).Render(fmt.Sprintf(" %-*s ", lblW, e.label(t)))
		return lbl + " " + cnt + "  " + st(lipgloss.NewStyle()).Render(e.name())
	}
	hints := "enter open · f flow · v preview · d git diff · ? help · q quit"
	if a.inLog() {
		hints = "enter open · backspace commits · N/P commit · f flow · v preview · d git diff · ? help · q quit"
	}
	return a.render(t, head, item, func(h int) string { return a.preview(a.files[a.cursor], h) }, statusText(t, a.msg, hints))
}
