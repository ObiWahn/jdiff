package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/gitsrc"
	"jdiff/internal/keys"
	"jdiff/internal/source"
	"jdiff/internal/theme"
	"jdiff/watch"
)

// fileEntry is one file in the list. Its alignment is created with the
// list, since moved code is searched in all files together, and its diff
// view on first open; both are kept, so anchors survive going back to the
// list.
type fileEntry struct {
	source.File
	added, removed int
	align          *fileAlign
	view           *FileDiff
}

// label returns the status label, using delta's file labels.
func (e *fileEntry) label(th *theme.Theme) string {
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

// name returns the path shown for the file, "old → new" for renames,
// printable: paths may hold control characters.
func (e *fileEntry) name() string {
	if e.OldPath != "" {
		return printable(e.OldPath) + " → " + printable(e.Path)
	}
	return printable(e.Path)
}

// paneNames returns the names shown above the panes of the file's diff.
func (e *fileEntry) paneNames() [2]string {
	return [2]string{"a/" + e.PathInOld(), "b/" + e.Path}
}

// refresh reloads the sides of e that changed on disk, see refreshFile. Its
// diff view and after, unless nil, follow each reload of its diff.
func (e *fileEntry) refresh(after func(*fileAlign)) string {
	if e.Binary {
		return ""
	}
	msg := refreshFile(&e.File, e.align, func() { e.followReload(after) })
	if msg != "" {
		e.updateStats()
	}
	return msg
}

// update takes f, the file listed again, into e, reloading the sides of
// its diff whose content changed. Its diff view and after, unless nil,
// follow each reload, as in refresh. It returns the anchors dropped and
// whether the content changed.
func (e *fileEntry) update(f source.File, after func(*fileAlign)) (dropped int, changed bool) {
	was := [2][]string{e.Old, e.New}
	e.File = f
	if e.Binary {
		return 0, false
	}
	e.align.names = e.paneNames()
	for s, lines := range [2][]string{f.Old, f.New} {
		if slices.Equal(was[s], lines) {
			continue
		}
		changed = true
		dropped += e.align.reload(side(s), lines)
		e.followReload(after)
	}
	return dropped, changed
}

// followReload lets its diff view and after, unless nil, follow a reload of
// its diff.
func (e *fileEntry) followReload(after func(*fileAlign)) {
	if e.view != nil {
		e.view.syncLayout()
	}
	if after != nil {
		after(e.align)
	}
}

func (e *fileEntry) updateStats() {
	if e.Binary {
		return
	}
	if e.align != nil {
		e.added, e.removed = diff.Stats(e.align.rows)
		return
	}
	e.added, e.removed = diff.Stats(diff.Align(e.Old, e.New, nil))
}

// fileListRows is the most rows the file list takes while the preview shows,
// so that on a tall screen the preview gets the rest.
const fileListRows = 15

// Diff is the Bubble Tea model of the file list (keys of file_list), of jdiff
// diff, two directories and a commit of the log. It shows the diff view
// of the selected file and returns to the list on backspace. A single file
// has its list too, as many do.
type Diff struct {
	cfg       *config.Config
	commit    *gitsrc.Commit // whose files are shown, nil outside the log
	files     []*fileEntry
	relist    ListFunc    // lists the files again, nil when they cannot be
	relisting relistState // of the files, see relistFiles
	// ctx ends the git commands of the view, cancel ends ctx, see stop.
	ctx    context.Context
	cancel context.CancelFunc
	// gitDiff runs git diff with arguments, nil outside jdiff diff; diffArgs
	// are those of the files shown.
	gitDiff  DiffFunc
	diffArgs []string
	// disk watches the files on disk while auto reload is on, from watch on;
	// those near the cursor that it does not watch are checked every
	// watch_interval. touched is the file the cursor was on last.
	disk     diskWatch
	watching bool
	touched  *fileEntry
	list
	// previewTop is the first screen line of the preview scrolled by pgup
	// and pgdn, while previewFor is the file under the cursor; moving the
	// cursor drops it.
	previewFor *fileEntry
	previewTop int
	open       *fileEntry // file whose diff view is shown, nil for the list
	flow       *Flow      // flow view outside the log, created on first use
	msg        string
	prefix     string // the action, "jump", while waiting for the letter of a mark
}

// NewDiff returns the file list for files.
func NewDiff(cfg *config.Config, files []source.File) *Diff { return newDiff(cfg, files, nil) }

// ListFunc lists the files of a comparison again. It runs in the background,
// and ends when ctx is done.
type ListFunc func(ctx context.Context) ([]source.File, error)

// DiffFunc returns the files git diff compares with args, with the
// untracked files as added when untracked is set. It runs in the background:
// it must not read the settings, which the keys change meanwhile; it ends
// when ctx is done.
type DiffFunc func(ctx context.Context, args []string, untracked bool) ([]source.File, error)

// WithGitDiff makes d the files of git diff with args, which r runs again,
// and returns d.
func (d *Diff) WithGitDiff(args []string, gitDiff DiffFunc) *Diff {
	d.gitDiff, d.diffArgs = gitDiff, args
	return d
}

// WithRelist sets how r lists the files again and returns d.
func (d *Diff) WithRelist(list ListFunc) *Diff {
	d.relist = list
	return d
}

// newDiff returns the file list for files, as part of the log for the
// files of commit when it is set.
func newDiff(cfg *config.Config, files []source.File, commit *gitsrc.Commit) *Diff {
	d := &Diff{cfg: cfg, commit: commit}
	d.ctx, d.cancel = context.WithCancel(context.Background())
	// The files are searched for moved code together.
	group := &moveGroup{}
	for _, f := range files {
		d.addEntry(group, &fileEntry{File: f})
	}
	d.list = list{n: len(d.files), preview: &cfg.Settings.Preview, maxRows: fileListRows}
	if len(files) == 0 {
		d.msg = "no changed files"
	}
	if w := cfg.Warning(); w != "" {
		d.msg = w
	}
	return d
}

// addEntry adds e to the files and its diff, unless binary, to group, the
// files searched for moved code together.
func (d *Diff) addEntry(group *moveGroup, e *fileEntry) {
	if !e.Binary {
		group.add(d.diffOf(e), [2]string{e.PathInOld(), e.Path})
	}
	e.updateStats()
	d.files = append(d.files, e)
}

// stop stops watching the files and ends the git commands of the view: it
// is replaced, or the program ends.
func (d *Diff) stop() {
	d.cancel()
	d.disk.stop()
}

// shareWatcher makes d watch its files by w, which App shares.
func (d *Diff) shareWatcher(w *watch.Watcher) { d.disk.share(w) }

// diffCommand returns the git diff command line of args, quoted as the
// popup of git diff reads them.
func diffCommand(args []string) string {
	return strings.TrimSpace("git diff " + joinArgs(args))
}

// inLog reports whether a is part of the log: backspace and N/P go to the
// log.
func (d *Diff) inLog() bool { return d.commit != nil }

// openPath opens the file with the given path and reports whether it could.
func (d *Diff) openPath(path string) bool {
	i := d.fileByPath(path)
	if i >= 0 {
		d.openIndex(i)
	}
	return i >= 0
}

// fileByPath returns the index of the file with the given path that can be
// shown, or -1.
func (d *Diff) fileByPath(path string) int {
	return slices.IndexFunc(d.files, func(e *fileEntry) bool { return e.Path == path && !e.Binary })
}

// fileByGroup returns the index of the file at index i of the files searched
// for moved code, or -1.
func (d *Diff) fileByGroup(i int) int {
	return slices.IndexFunc(d.files, func(e *fileEntry) bool { return e.align != nil && e.align.groupIndex == i })
}

// openIndex opens file i, after updating the counts of the file it leaves.
func (d *Diff) openIndex(i int) {
	if d.open != nil {
		d.open.updateStats()
	}
	d.cursor = i
	d.scrollToCursor()
	d.openFile()
}

// setMsg shows msg in the status bar of whatever is displayed.
func (d *Diff) setMsg(msg string) {
	switch {
	case d.flowShown():
		d.flow.msg = msg
	case d.open != nil:
		d.open.view.msg = msg
	default:
		d.msg = msg
	}
}

// Init starts watching the files, outside the log.
func (d *Diff) Init() tea.Cmd {
	if d.inLog() {
		return nil
	}
	return d.watch()
}

// watch watches the files on disk, in place of those before, while auto
// reload is on, and returns the command waiting for their changes and the
// check of those not watched; off, it watches none.
func (d *Diff) watch() tea.Cmd {
	if !d.cfg.AutoReload {
		d.disk.stop()
		d.watching = false
		return nil
	}
	var paths []string
	for _, e := range d.files {
		paths = append(paths, diskPaths(e.File)...)
	}
	cmd, msg := d.disk.set(d.cfg, paths)
	if msg != "" {
		d.setMsg(msg)
	}
	d.watching = true
	return batch(cmd, d.poll())
}

// poll returns the next check of the files near the cursor, the current one
// and its neighbors, that are not watched; nil while one is pending, or when
// none is to be checked.
func (d *Diff) poll() tea.Cmd {
	var paths []string
	for _, i := range d.nearCursor() {
		paths = append(paths, diskPaths(d.files[i].File)...)
	}
	return d.disk.poll(d.cfg, d, paths)
}

// nearCursor returns the indices of the current file and its neighbors.
func (d *Diff) nearCursor() []int {
	var out []int
	if cur := d.current(); cur >= 0 {
		for i := max(cur-1, 0); i <= min(cur+1, len(d.files)-1); i++ {
			out = append(out, i)
		}
	}
	return out
}

// unwatched reports whether a side of file i on disk is not watched.
func (d *Diff) unwatched(i int) bool {
	return d.disk.unwatched(diskPaths(d.files[i].File))
}

// current returns the index of the file the cursor is on: in the flow view,
// the diff view or the list; -1 for none.
func (d *Diff) current() int {
	if d.flowShown() {
		_, file, _ := d.flow.position()
		return file
	}
	if d.open != nil {
		return slices.Index(d.files, d.open)
	}
	if d.cursor < len(d.files) {
		return d.cursor
	}
	return -1
}

// followCursor counts the directories of the file the cursor is on as used
// when it is on another one than before, and reloads it when it changed on
// disk, as it may while its directory was not watched. It returns the check
// of the files near the cursor not watched.
func (d *Diff) followCursor() tea.Cmd {
	i := d.current()
	if !d.watching || !d.cfg.AutoReload || i < 0 || d.files[i] == d.touched {
		return nil
	}
	d.touched = d.files[i]
	d.disk.touch(diskPaths(d.touched.File))
	f := d.touched.File
	if f.OldFile != "" && stale(f.OldFile, f.OldStamp) || f.NewFile != "" && stale(f.NewFile, f.NewStamp) {
		d.refresh(func(j int) bool { return j == i })
	}
	return d.poll()
}

// refresh reloads the files i that changed on disk for which which holds.
func (d *Diff) refresh(which func(i int) bool) {
	var msgs []string
	reload := func(after func(*fileAlign)) {
		for i := range d.files {
			if !which(i) {
				continue
			}
			if msg := d.files[i].refresh(after); msg != "" {
				msgs = append(msgs, msg)
			}
		}
	}
	if d.flowShown() {
		d.flow.refresh(reload)
	} else {
		reload(nil)
	}
	if len(msgs) > 0 {
		d.setMsg(strings.Join(msgs, "; "))
	}
}

// filesListedMsg brings the files d listed again, or why it could not.
type filesListedMsg struct {
	diff  *Diff
	files []source.File
	err   error
	quiet bool
}

// relistFiles returns the command listing the files again (r) in the
// background: git diff, or the two directories. The files stay usable
// meanwhile. While a listing runs, another one is not started, but run once
// it is done, since what to list may have changed, as the untracked files.
// With quiet, as auto reload asks, nothing is said unless something changed.
func (d *Diff) relistFiles(quiet bool) tea.Cmd {
	list := d.relist
	if d.gitDiff != nil {
		args, untracked, gitDiff := d.diffArgs, d.cfg.Settings.Untracked, d.gitDiff
		list = func(ctx context.Context) ([]source.File, error) { return gitDiff(ctx, args, untracked) }
	}
	if list == nil {
		d.setMsg(nothingToReload)
		return nil
	}
	if !d.relisting.start(quiet) {
		return nil
	}
	if !quiet {
		d.setMsg(reloading)
	}
	ctx := d.ctx
	return func() tea.Msg {
		files, err := list(ctx)
		return filesListedMsg{d, files, err, quiet}
	}
}

// filesListed takes the files listed again. New files are added and gone
// ones dropped; a file still there keeps its entry, and so its diff with
// anchors and folds, reloaded where its content changed. A file that became
// binary or stopped being binary counts as gone and new. The cursor stays on
// its file; when the file shown is gone, the list is shown. The files are
// watched anew; it returns the command waiting for their changes, and the
// one listing them once more when that was asked for meanwhile.
func (d *Diff) filesListed(res filesListedMsg) tea.Cmd {
	var next tea.Cmd
	if again, quiet := d.relisting.done(); again {
		next = d.relistFiles(quiet)
	}
	files, quiet := res.files, res.quiet
	if res.err != nil {
		d.setMsg(firstLine(res.err))
		return next
	}
	kept := map[string]*fileEntry{}
	for _, e := range d.files {
		kept[e.Path] = e
	}
	var cur *fileEntry
	if d.cursor < len(d.files) {
		cur = d.files[d.cursor]
	}
	added, changed, dropped := 0, 0, 0
	update := func(after func(*fileAlign)) {
		old := d.files
		d.files = nil
		// The files are searched for moved code together.
		group := &moveGroup{}
		for _, f := range files {
			e := kept[f.Path]
			if e == nil || e.Binary != f.Binary {
				e = &fileEntry{File: f}
				added++
			} else {
				delete(kept, f.Path)
				n, ok := e.update(f, after)
				if ok {
					changed++
				}
				dropped += n
			}
			d.addEntry(group, e)
		}
		if d.flow != nil {
			d.flow.followFiles(0, old, d.files)
		}
	}
	shown := d.flowShown()
	if shown {
		d.flow.refresh(update)
	} else {
		update(nil)
	}
	d.list.n = len(d.files)
	if i := slices.Index(d.files, cur); i >= 0 {
		d.cursor = i
	} else {
		d.cursor = max(min(d.cursor, len(d.files)-1), 0)
	}
	msg := refreshed(added, len(kept), changed, dropped)
	switch {
	case len(d.files) == 0:
		d.open = nil
	case shown:
		if _, file, _ := d.flow.position(); file >= 0 {
			d.cursor = file
		}
		d.open = d.files[d.cursor]
	case d.open != nil && !slices.Contains(d.files, d.open):
		msg = d.open.Path + " is gone; " + msg
		d.open = nil
	}
	d.scrollToCursor()
	if !quiet || msg != nothingChanged {
		d.setMsg(msg)
	}
	if d.inLog() {
		return next
	}
	return batch(d.watch(), next)
}

// batch returns the commands that are not nil, as one: tea.Batch of them
// when there are several, so that a single command stays plain, as the
// tests run them.
func batch(cmds ...tea.Cmd) tea.Cmd {
	cmds = slices.DeleteFunc(cmds, func(c tea.Cmd) bool { return c == nil })
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

// flowShown reports whether the flow view is shown instead of a diff view.
// Inside the log, the log shows its own flow view.
func (d *Diff) flowShown() bool { return d.open != nil && d.cfg.Settings.Flow && !d.inLog() }

// Update handles msg, then follows the cursor, see followCursor.
func (d *Diff) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := d.update(msg)
	return m, batch(cmd, d.followCursor())
}

func (d *Diff) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width, d.height = msg.Width, msg.Height
		d.scrollToCursor()
	case settingChangedMsg:
		d.scrollToCursor() // the preview takes room from the list
		if msg.problem != "" && d.open == nil {
			d.msg = msg.problem
		}
		if msg.action == "inotify_max_dirs" {
			d.disk.limit(d.cfg)
			return d, d.poll()
		}
	case autoReloadMsg:
		if d.inLog() {
			return d, nil
		}
		if msg.on { // changed while off
			d.touched = nil
			d.refresh(func(int) bool { return true })
		}
		return d, d.watch()
	case watchTickMsg:
		if !d.disk.ticked(d.cfg, d, msg) {
			return d, nil
		}
		near := d.nearCursor()
		d.refresh(func(i int) bool { return slices.Contains(near, i) && d.unwatched(i) })
		return d, d.poll()
	case changedMsg:
		if msg.w == nil || msg.w != d.disk.w {
			return d, nil
		}
		changed := func(i int) bool {
			return slices.ContainsFunc(diskPaths(d.files[i].File), func(p string) bool { return slices.Contains(msg.paths, p) })
		}
		d.refresh(changed)
		return d, batch(d.disk.next(), d.poll())
	case watchEndedMsg:
		if !d.disk.ended(msg.w) {
			return d, nil
		}
		return d, d.poll()
	case jumpMsg:
		d.jump(msg)
		return d, nil
	case markJumpMsg:
		d.gotoMark(msg.letter, msg.screenRow)
		return d, nil
	case switchFileMsg:
		d.switchFile(int(msg))
		return d, nil
	case toggleFlowMsg:
		d.toggleFlow()
		if w := saveState(d.cfg); w != "" {
			d.setMsg(w)
		}
		return d, nil
	case backMsg: // from the flow view
		d.open = nil
		return d, nil
	case refreshMsg:
		return d, d.relistFiles(msg.quiet)
	case filesListedMsg:
		if msg.diff != d {
			return d, nil
		}
		return d, d.filesListed(msg)
	case showMsg:
		d.setMsg(string(msg))
		return d, nil
	case toggleUntrackedMsg:
		if d.gitDiff == nil {
			d.setMsg(untrackedOnlyInDiff)
			return d, nil
		}
		d.cfg.Settings.Untracked = !d.cfg.Settings.Untracked // not saved
		return d, d.relistFiles(false)
	}
	if d.flowShown() {
		_, cmd := d.flow.Update(msg)
		if _, file, _ := d.flow.position(); file >= 0 {
			d.cursor = file
			d.scrollToCursor()
		}
		return d, cmd
	}
	if d.open != nil {
		if k, ok := msg.(tea.KeyPressMsg); ok && d.cfg.Keys.Key(keys.DiffView, k.String()).Action == "to_files" {
			d.open.updateStats()
			d.open = nil
			return d, nil
		}
		_, cmd := d.open.view.Update(msg)
		return d, cmd
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return d, d.key(k.String())
	}
	return d, nil
}

func (d *Diff) key(k string) tea.Cmd {
	d.msg = ""
	cursor := d.cursor
	defer func() {
		if d.cursor != cursor {
			d.previewFor = nil // another file shows its preview from the start
		}
	}()
	if d.prefix != "" {
		d.prefix = ""
		if isMarkLetter(k) {
			d.gotoMark(k, (d.height-2)/2)
		}
		return nil
	}
	h := d.cfg.Keys.Key(keys.FileList, k)
	page := max(d.listHeight()-1, 1)
	if delta, paging, ok := motion(h, page, len(d.files)); ok {
		if h := d.boxHeight(); paging && h > 0 && d.cursor < len(d.files) {
			// The preview scrolls, by its height less a line, as the list pages.
			d.scrollPreview(direction(delta > 0) * max(h-3, 1))
			return nil
		}
		d.moveCursor(delta)
		return nil
	}
	switch h.Action {
	case "quit":
		return tea.Quit
	case "open":
		if d.inLog() && d.cfg.Settings.Flow && d.cursor < len(d.files) {
			file := d.cursor
			return func() tea.Msg { return flowAtMsg(file) }
		}
		d.openFile()
	case "to_commits":
		if d.inLog() {
			return func() tea.Msg { return backMsg{} }
		}
	case "history":
		if d.inLog() {
			return switchCommit(h)
		}
	case "flow":
		return toggleFlow
	case "jump":
		d.prefix, d.msg = h.Action, jumpPrompt
	default:
		return pageKey(d.cfg.Keys, h, false)
	}
	return nil
}

// diffOf returns the diff of e, creating it on first use.
func (d *Diff) diffOf(e *fileEntry) *fileAlign {
	if e.align == nil {
		e.align = newFileAlign(d.cfg, e.paneNames(), [2][]string{e.Old, e.New})
		e.align.markPath = e.Path
	}
	return e.align
}

// viewOf returns the diff view of e, creating it on first use.
func (d *Diff) viewOf(e *fileEntry) *FileDiff {
	if e.view == nil {
		e.view = newFileDiff(d.diffOf(e))
		e.view.canBack = true
		e.view.commitNav = d.inLog()
	}
	return e.view
}

func (d *Diff) openFile() {
	if d.cursor >= len(d.files) {
		return
	}
	if d.cfg.Settings.Flow && !d.inLog() {
		d.showFlow(d.cursor, nil)
		return
	}
	e := d.files[d.cursor]
	if e.Binary {
		d.msg = e.Path + " is a binary file"
		return
	}
	d.viewOf(e).Update(tea.WindowSizeMsg{Width: d.width, Height: d.height})
	d.open = e
}

// showFlow shows the flow view on file, where its diff view from leaves
// it, or on its bar for from nil (see Flow.showFrom).
func (d *Diff) showFlow(file int, from *FileDiff) {
	if d.flow == nil {
		d.flow = flowOfDiff(d)
	}
	d.open = d.files[file]
	d.flow.width, d.flow.height = d.width, d.height
	d.flow.showFrom(0, file, from)
}

// toggleFlow switches between the diff view of the file under the cursor
// and the flow view, keeping the cursor on its line and screen row.
func (d *Diff) toggleFlow() {
	st := &d.cfg.Settings
	if !st.Flow {
		st.Flow = true
		switch {
		case len(d.files) == 0:
		case d.open == nil:
			d.showFlow(d.cursor, nil)
		default:
			d.showFlow(d.cursor, d.open.view)
		}
		return
	}
	st.Flow = false
	if d.open == nil {
		return
	}
	_, file, line := d.flow.position()
	screenRow := d.flow.cursor - d.flow.top
	d.open = nil
	if file < 0 {
		return
	}
	d.cursor = file
	d.scrollToCursor()
	if d.files[file].Binary {
		return
	}
	d.openFile()
	d.open.view.takeOver(d.flow, line, screenRow)
}

// gotoMark opens the file of mark letter, when it is in the diff, with the
// cursor on its line at screen row screenRow.
func (d *Diff) gotoMark(letter string, screenRow int) {
	mk, ok := d.cfg.Mark(letter)
	if !ok {
		d.setMsg("no mark " + letter)
		return
	}
	i := d.fileByPath(mk.Path)
	if i < 0 {
		d.setMsg(notInDiff(letter, mk))
		return
	}
	d.openIndex(i)
	if d.flowShown() {
		d.flow.gotoMark(letter)
		return
	}
	if at, ok := d.open.view.markSpot(letter, mk); ok {
		d.open.view.show(at, screenRow)
	}
}

// jump opens the file of j's end of moved code with the cursor on its line.
func (d *Diff) jump(j jumpMsg) {
	i := d.fileByGroup(j.end.file)
	if i < 0 {
		return
	}
	d.openIndex(i)
	d.open.view.show(d.open.align.spotOf(j.end.side, j.end.line, d.open.view.shape()), j.screenRow)
	d.open.view.msg = j.msg
}

// switchFile opens the next (dir > 0) or previous file that can be shown.
func (d *Diff) switchFile(dir int) {
	for i := d.cursor + dir; i >= 0 && i < len(d.files); i += dir {
		if !d.files[i].Binary {
			d.openIndex(i)
			return
		}
	}
	d.setMsg(ends(dir > 0, "last file", "first file"))
}

// scrollPreview scrolls the preview of the file under the cursor by delta
// lines: up to its first line, down until its last one is the last shown.
func (d *Diff) scrollPreview(delta int) {
	e := d.files[d.cursor]
	if e.Binary {
		return
	}
	screen, at := d.diffOf(e).screenLines(d.previewLayout(e).shape)
	from := d.previewStart(e, screen, at)
	if delta > 0 {
		from = min(from+delta, max(len(screen)-(d.boxHeight()-2), from))
	} else {
		from = max(from+delta, 0)
	}
	d.previewFor, d.previewTop = e, from
}

// previewLayout returns the layout of the preview of e: as wide as the
// box's inside, scrolled like the file's diff view.
func (d *Diff) previewLayout(e *fileEntry) layout {
	lo := layout{width: max(d.width-4, 0)}
	lo.shape = newShape(d.cfg, lo.width)
	if e.view != nil {
		lo.xoff, lo.active = e.view.xoff, e.view.active
	}
	return lo
}

// previewStart returns the first of the screen lines of e's diff, indexed by
// at, that its preview shows: where pgup and pgdn scrolled it, else the
// context of the first change.
func (d *Diff) previewStart(e *fileEntry, screen []sline, at map[sline]int) int {
	if d.previewFor == e {
		return max(min(d.previewTop, len(screen)-1), 0)
	}
	a := d.diffOf(e)
	for i := range screen {
		if a.blockStart(screen, i) {
			// The rows of context before the block, else its first line.
			return min(at[sline{max(screen[i].v-d.cfg.Settings.Context, 0), 0}], i)
		}
	}
	return 0
}

// preview renders the diff of e in a box height lines high, starting with
// the context of the first change, or where pgup and pgdn scrolled it.
func (d *Diff) preview(e *fileEntry, height int) string {
	t := d.cfg.Theme
	title := lipgloss.NewStyle().Bold(true).Render(" " + e.name() + " ")
	if e.Binary {
		return box(t, d.width, height, title, []string{t.Hint.Render("binary file")})
	}
	a := d.diffOf(e)
	if len(a.view) == 0 { // empty on both sides
		return box(t, d.width, height, title, nil)
	}
	lo := d.previewLayout(e)
	screen, at := a.screenLines(lo.shape)
	from := d.previewStart(e, screen, at)
	var lines []string
	for i := from; i < len(screen) && len(lines) < height-2; i++ {
		lines = append(lines, a.render(screen[i], false, lo))
	}
	return box(t, d.width, height, title, lines)
}

func (d *Diff) View() tea.View { return tea.NewView(d.frame()) }

func (d *Diff) frame() string {
	if d.flowShown() {
		return d.flow.frame()
	}
	if d.open != nil {
		return d.open.view.frame()
	}
	if d.width == 0 {
		return ""
	}

	t := d.cfg.Theme
	var added, removed int
	addW, remW, lblW := 1, 1, 0
	for _, e := range d.files {
		added, removed = added+e.added, removed+e.removed
		addW = max(addW, len(fmt.Sprint(e.added)))
		remW = max(remW, len(fmt.Sprint(e.removed)))
		lblW = max(lblW, ansi.StringWidth(e.label(t)))
	}

	// Styles are rendered side by side: lipgloss underlining breaks nested
	// escape sequences.
	bold := lipgloss.NewStyle().Bold(true)
	changed := fmt.Sprintf("%d files changed", len(d.files))
	if len(d.files) == 1 {
		changed = "1 file changed"
	}
	head := " " + bold.Render(changed)
	if c := d.commit; c != nil {
		head = " " + t.CommitHash.Render(c.Short) + " " + bold.Render(c.Subject)
	}
	head += "  " + t.Plus.Render(fmt.Sprintf("+%d", added)) + " " + t.Minus.Render(fmt.Sprintf("-%d", removed))
	if c := d.commit; c != nil {
		head += "  " + t.Decoration.Render(c.Author)
	}
	if d.gitDiff != nil {
		cmd := diffCommand(d.diffArgs)
		if d.cfg.Settings.Untracked {
			cmd += " + untracked"
		}
		head += "  " + t.Hint.Render(printable(cmd))
	}

	item := func(i int, st func(lipgloss.Style) lipgloss.Style) string {
		e := d.files[i]
		cnt := counts(t, st, e.added, e.removed, addW, remW)
		if e.Binary {
			cnt = st(t.Filler).Render(fmt.Sprintf("%-*s", addW+remW+3, "binary"))
		}
		lbl := st(lipgloss.NewStyle()).Render(" " + pad(e.label(t), lblW, lipgloss.NewStyle()) + " ")
		return lbl + " " + cnt + "  " + st(lipgloss.NewStyle()).Render(e.name())
	}
	k := barKeys{enter: "open", commits: d.inLog(), flow: true}
	if d.inLog() {
		k.back = "to_commits"
	}
	hints := statusKeys(d.cfg, k)
	return d.render(t, head, item, func(h int) string { return d.preview(d.files[d.cursor], h) }, pageStatus(d.cfg, d.msg, hints))
}
