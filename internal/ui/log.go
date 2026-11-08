package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	relist   CommitsFunc             // lists the commits again, nil when they cannot be
	counting bool                    // a count is running
	byHash   map[string]*commitEntry // the commits by hash
	minCount int                     // the least width of the count columns
	cols     logColumns              // see measure
	loadGen  int                     // counts the requests of requestCommit
	pending  *commitRequest          // the commit loading, nil for none
	// relisting is set while the commits are listed again in the
	// background, again when they are to be listed once more after that,
	// quietly with againQuiet (see relistCommits).
	relisting, again, againQuiet bool

	commits []*commitEntry
	order   historyOrder // of commits
	list
	open   *commitEntry // commit whose files are shown, nil for the list
	files  bool         // in flow mode: the file list of open, not the flow view
	flow   *Flow        // all commits one after the other
	msg    string
	prefix string // "'" while waiting for the letter of a mark
}

// NewLog returns the commit list for commits of branch, in the order of git
// log: newest first, or with oldestFirst (git log --reverse) oldest. With
// counts, their line counts are counted once they are shown; without, they
// come with the commits.
func NewLog(cfg *config.Config, branch string, commits []gitsrc.Commit, oldestFirst bool,
	load LoadFunc, counts CountFunc) *Log {
	l := &Log{cfg: cfg, branch: branch, load: load, counts: counts, byHash: map[string]*commitEntry{}, minCount: 1,
		order: orderOf(oldestFirst)}
	if counts != nil {
		l.minCount = countWidth
	}
	var parts []*flowPart
	for _, c := range commits {
		e := newCommitEntry(c, counts == nil)
		l.commits = append(l.commits, e)
		l.byHash[c.Hash] = e
		parts = append(parts, newCommitPart(e))
	}
	l.list = list{n: len(l.commits), preview: &cfg.Settings.Preview}
	l.measure()
	l.flow = newFlow(cfg, parts, func(p int) tea.Cmd {
		part := l.flow.parts[p]
		return l.loadCmd(part.commit, 0, part)
	})
	l.flow.order = l.order
	if len(commits) == 0 {
		l.msg = "no commits"
	}
	if w := cfg.Warning(); w != "" {
		l.msg = w
	}
	return l
}

// newCommitEntry returns the entry of c, counted when its line counts are
// known.
func newCommitEntry(c gitsrc.Commit, counted bool) *commitEntry {
	// Commits are only shown: their text is made printable once.
	c.Author, c.Subject = printable(c.Author), printable(c.Subject)
	body := strings.Split(c.Body, "\n")
	for i, line := range body {
		body[i] = printable(line)
	}
	c.Body = strings.Join(body, "\n")
	return &commitEntry{Commit: c, counted: counted}
}

// CommitsFunc lists the commits of the log again, with the branch.
type CommitsFunc func() (branch string, commits []gitsrc.Commit, err error)

// WithRelist sets how r lists the commits again and returns l.
func (l *Log) WithRelist(list CommitsFunc) *Log {
	l.relist = list
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
		l.setMsg("line counts: " + firstLine(m.err))
		return
	}
	for _, h := range m.hashes {
		if e := l.byHash[h]; e != nil {
			c := m.counts[h]
			e.Added, e.Removed, e.counted = c.Added, c.Removed, true
		}
	}
	l.measure()
}

func (l *Log) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
		l.scrollToCursor()
		if l.flowShown() {
			_, cmd := l.flow.Update(msg)
			return cmd
		}
	case settingChangedMsg:
		l.scrollToCursor() // the message box takes room from the list
		if msg.problem != "" && l.open == nil {
			l.msg = msg.problem
		}
	case backMsg:
		if l.flowShown() {
			return l.filesOfFlow()
		}
		l.open, l.files = nil, false
		return nil
	case flowAtMsg:
		return l.showFlow(int(msg), nil)
	case switchCommitMsg:
		return l.switchCommit(int(msg))
	case commitLoadedMsg:
		return l.commitLoaded(msg)
	case refreshMsg:
		return l.relistCommits(msg.quiet)
	case commitsListedMsg:
		if msg.log == l {
			return l.commitsListed(msg)
		}
		return nil
	case showMsg:
		l.setMsg(string(msg))
		return nil
	case toggleUntrackedMsg:
		l.setMsg(untrackedOnlyInDiff)
		return nil
	case toggleFlowMsg:
		cmd := l.toggleFlow()
		if w := saveState(l.cfg); w != "" {
			l.setMsg(w)
		}
		return cmd
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
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return l.key(k.String())
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
	h := keyFile.key(commitList, k)
	page := max(l.listHeight()-1, 1)
	if delta, _, ok := motion(h, page, len(l.commits)); ok {
		l.moveCursor(delta)
		return nil
	}
	switch h.action {
	case "quit":
		return tea.Quit
	case "open":
		if l.cfg.Settings.Flow {
			return l.showFlow(-1, nil)
		} else {
			return l.requestCommit(l.cursor, "")
		}
	case "flow":
		return toggleFlow
	case "history":
		// The next or previous commit in history, as N and P go from its files.
		dir := historyDir(h)
		if i := l.cursor + l.order.step(dir); i >= 0 && i < len(l.commits) {
			l.moveCursor(i - l.cursor)
		} else {
			l.msg = ends(dir > 0, "last commit", "first commit")
		}
	case "jump":
		l.prefix, l.msg = h.action, "jump to mark: press its letter"
	default:
		return pageKey(h, false)
	}
	return nil
}

// flowShown reports whether the flow view is shown.
func (l *Log) flowShown() bool { return l.open != nil && l.cfg.Settings.Flow && !l.files }

// showFlow shows the flow view on file of the commit under the cursor,
// where its diff view from leaves it, or on its bar for from nil and on the
// commit's bar for file -1 (see Flow.showFrom), and returns the command
// loading the commits on the screen. A list that loading again emptied has
// no commit to show.
func (l *Log) showFlow(file int, from *Model) tea.Cmd {
	if len(l.commits) == 0 {
		l.msg = "no commits"
		return nil
	}
	l.open, l.files = l.commits[l.cursor], false
	l.flow.width, l.flow.height = l.width, l.height
	l.flow.showFrom(l.cursor, file, from)
	return l.flow.nextLoad()
}

// toggleFlow switches between the flow view and the files of the commit
// under the cursor, keeping the cursor on its line and screen row, and
// returns the command loading the commits on the screen of the flow view.
func (l *Log) toggleFlow() tea.Cmd {
	st := &l.cfg.Settings
	if !st.Flow {
		st.Flow = true
		switch {
		case l.open == nil:
			return l.showFlow(-1, nil)
		case l.open.app.open == nil:
			return l.showFlow(l.open.app.cursor, nil)
		default:
			return l.showFlow(l.open.app.cursor, l.open.app.open.view)
		}
	}
	st.Flow = false
	if l.open == nil {
		return nil
	}
	if l.files { // the file list stays
		l.files = false
		return nil
	}
	p, file, line := l.flow.position()
	screenRow := l.flow.cursor - l.flow.top
	path := ""
	if e := l.commits[p]; e.app != nil && file >= 0 {
		path = e.app.files[file].Path
	}
	l.open = nil
	if l.commits[p].app == nil { // still loading: its files when they come
		return l.requestCommit(p, "")
	}
	if !l.openCommit(p, path) {
		return nil
	}
	app := l.open.app
	if app.open == nil && file >= 0 {
		app.cursor = file
		app.scrollToCursor()
	}
	if app.open != nil && app.open.Path == path {
		app.open.view.takeOver(l.flow, line, screenRow)
	}
	return nil
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
		l.msg = firstLine(err)
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

// switchCommit shows the next (dir > 0) or previous commit in history,
// keeping the open file when the other commit changed it too, and returns
// the command loading it, see requestCommit.
func (l *Log) switchCommit(dir int) tea.Cmd {
	if l.open == nil {
		return nil
	}
	app := l.open.app
	path := ""
	if app.open != nil {
		path = app.open.Path
	}
	i := l.cursor + l.order.step(dir)
	if i < 0 || i >= len(l.commits) {
		app.setMsg(ends(dir > 0, "last commit", "first commit"))
		return nil
	}
	if app.open != nil {
		app.open.updateStats()
	}
	return l.requestCommit(i, path)
}

// commitLoadedMsg brings the files of the commit with hash, or why they
// could not be loaded: for the request number gen of requestCommit or
// filesOfFlow, 0 for none, and for part of the flow view, nil for none.
type commitLoadedMsg struct {
	gen   int
	hash  string
	files []source.File
	err   error
	part  *flowPart
}

// loadCmd returns the command loading the files of e in the background, for
// request gen and the flow's part, see commitLoadedMsg. Only git runs there:
// the views are made when the files come, on the keys' side, which owns the
// settings.
func (l *Log) loadCmd(e *commitEntry, gen int, part *flowPart) tea.Cmd {
	c, load := e.Commit, l.load
	return func() tea.Msg {
		files, err := load(c)
		return commitLoadedMsg{gen, c.Hash, files, err, part}
	}
}

// commitRequest is the commit requestCommit loads: shown with path open
// when it arrives, if the view is still from, which it was asked from; with
// files, as filesOfFlow asks, its file list when the flow view still shows
// it. msg says that it loads.
type commitRequest struct {
	gen       int
	path, msg string
	from      *commitEntry
	files     bool
}

// request starts loading the files of e for req, dropping any request
// before, and returns the command; the status bar says that it loads.
func (l *Log) request(e *commitEntry, req commitRequest) tea.Cmd {
	l.loadGen++
	req.gen, req.msg = l.loadGen, "loading "+e.Short+"…"
	l.pending = &req
	l.setMsg(req.msg)
	return l.loadCmd(e, req.gen, nil)
}

// requestCommit shows commit i as openCommit does, at once when its files
// are loaded, else it returns the command loading them, which runs git
// without blocking the keys. The status bar says what loads meanwhile, and
// the view stays usable; moving to another view, or another request, drops
// the files when they come.
func (l *Log) requestCommit(i int, path string) tea.Cmd {
	if i < 0 || i >= len(l.commits) {
		return nil
	}
	e := l.commits[i]
	if e.app != nil {
		if !l.openCommit(i, path) && l.open != nil {
			l.open.app.setMsg(l.msg)
		}
		return nil
	}
	return l.request(e, commitRequest{path: path, from: l.open})
}

// commitLoaded takes the files msg brings: they become the commit's, unless
// it has some already, loaded for the list or the flow view meanwhile. The
// flow view, when it loaded them, shows them; a request shows the commit,
// when it is still the one asked for last and the view is the one it was
// asked from. It returns the command loading the next commit of the flow
// view.
func (l *Log) commitLoaded(msg commitLoadedMsg) tea.Cmd {
	e := l.byHash[msg.hash]
	if e != nil && e.app == nil && msg.err == nil {
		e.app = newApp(l.cfg, msg.files, &e.Commit)
	}
	var cmd tea.Cmd
	if msg.part != nil {
		cmd = l.flow.partLoaded(msg.part, msg.err)
	}
	req := l.pending
	if req == nil || msg.gen != req.gen {
		return cmd
	}
	l.pending = nil
	l.dropMsg(req.msg)
	switch {
	case e == nil:
	case req.files:
		if p, _, _ := l.flow.position(); l.flowShown() && l.commits[p] == e {
			cmd = batch(cmd, l.filesOfFlow())
		}
	case l.open != req.from || l.flowShown():
	case msg.err != nil:
		l.setMsg(firstLine(msg.err))
	default:
		l.openCommit(slices.Index(l.commits, e), req.path)
	}
	return cmd
}

// commitsListedMsg brings the commits log listed again, with the branch, or
// why it could not.
type commitsListedMsg struct {
	log     *Log
	branch  string
	commits []gitsrc.Commit
	err     error
	quiet   bool
}

// relistCommits returns the command listing the commits again (r) in the
// background, as App.relistFiles does the files: the commits stay usable
// meanwhile, and a listing asked for while one runs runs after it.
func (l *Log) relistCommits(quiet bool) tea.Cmd {
	if l.relist == nil {
		l.setMsg("nothing to load again")
		return nil
	}
	if l.relisting {
		l.againQuiet = quiet && (!l.again || l.againQuiet)
		l.again = true
		return nil
	}
	l.relisting = true
	if !quiet {
		l.setMsg("loading again…")
	}
	list := l.relist
	return func() tea.Msg {
		branch, commits, err := list()
		return commitsListedMsg{l, branch, commits, err, quiet}
	}
}

// commitsListed takes the commits listed again. New commits are added and
// gone ones dropped; a commit still there keeps its entry, and so its files
// with their anchors and folds: its content cannot change. The cursor stays
// on its commit; when the commit shown is gone, the list is shown. It returns
// the command listing them once more when that was asked for meanwhile.
func (l *Log) commitsListed(res commitsListedMsg) tea.Cmd {
	l.relisting = false
	var next tea.Cmd
	if l.again {
		l.again = false
		next = l.relistCommits(l.againQuiet)
	}
	branch, commits, quiet := res.branch, res.commits, res.quiet
	if res.err != nil {
		l.setMsg(firstLine(res.err))
		return next
	}
	parts := map[*commitEntry]*flowPart{}
	for _, p := range l.flow.parts {
		parts[p.commit] = p
	}
	var cur *commitEntry
	if l.cursor < len(l.commits) {
		cur = l.commits[l.cursor]
	}
	kept, oldParts, shown, open := l.byHash, l.flow.parts, l.flowShown(), l.open
	if shown {
		p, _, _ := l.flow.position()
		open = oldParts[p].commit
	}
	l.branch, l.commits, l.flow.parts, l.byHash = branch, nil, nil, map[string]*commitEntry{}
	added := 0
	for _, c := range commits {
		e, p := kept[c.Hash], (*flowPart)(nil)
		if e != nil {
			delete(kept, c.Hash)
			p = parts[e]
		} else {
			e = newCommitEntry(c, l.counts == nil)
			p = newCommitPart(e)
			added++
		}
		l.commits = append(l.commits, e)
		l.byHash[c.Hash] = e
		l.flow.parts = append(l.flow.parts, p)
	}
	l.list.n = len(l.commits)
	l.measure()
	if i := slices.Index(l.commits, cur); i >= 0 {
		l.cursor = i
	} else {
		l.cursor = max(min(l.cursor, len(l.commits)-1), 0)
	}
	msg := refreshed(added, len(kept), 0, 0)
	switch {
	case open != nil && kept[open.Hash] == open:
		msg = "commit " + open.Short + " is gone; " + msg
		l.open = nil
	case shown:
		l.flow.followParts(oldParts)
		l.flow.keepScreenRow(l.flow.relocate)
		next = batch(next, l.flow.nextLoad())
		l.cursor, _, _ = l.flow.position()
		l.open = l.commits[l.cursor]
	case open != nil:
		l.cursor = slices.Index(l.commits, open)
	}
	l.scrollToCursor()
	if !quiet || msg != nothingChanged {
		l.setMsg(msg)
	}
	return next
}

// filesOfFlow shows the file list of the commit under the flow view's
// cursor, on its file, as backspace does from a diff view. A commit of one
// file, or one that cannot be loaded, has no list to show: the commits are
// shown, on it. A commit still loading shows its list when its files come;
// it returns the command loading them.
func (l *Log) filesOfFlow() tea.Cmd {
	p, file, _ := l.flow.position()
	l.cursor = p
	l.scrollToCursor()
	e := l.commits[p]
	if e.app == nil && l.flow.parts[p].err == "" { // its list when it comes
		return l.request(e, commitRequest{files: true})
	}
	if e.app == nil || e.app.single() {
		l.open, l.files = nil, false
		return nil
	}
	app := e.app
	app.Update(tea.WindowSizeMsg{Width: l.width, Height: l.height})
	app.open = nil
	if file >= 0 {
		app.cursor = file
		app.scrollToCursor()
	}
	l.open, l.files = l.commits[p], true
	return nil
}

// dropMsg clears msg where setMsg may have put it, unless another message
// took its place.
func (l *Log) dropMsg(msg string) {
	if l.msg == msg {
		l.msg = ""
	}
	if l.flow.msg == msg {
		l.flow.msg = ""
	}
	for _, e := range l.commits {
		if a := e.app; a != nil {
			if a.msg == msg {
				a.msg = ""
			}
			if a.open != nil && a.open.view != nil && a.open.view.msg == msg {
				a.open.view.msg = ""
			}
		}
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

// logColumns are the widths the columns of the commit list need: the digits
// of the counts, the author, the subject, and hash and date together.
type logColumns struct{ added, removed, author, subject, fixed int }

// measure computes the widths of the columns, after the commits or their
// counts changed: measuring them on each frame takes too long in a long
// history.
func (l *Log) measure() {
	c := logColumns{added: l.minCount, removed: l.minCount}
	for _, e := range l.commits {
		c.added = max(c.added, len(fmt.Sprint(e.Added)))
		c.removed = max(c.removed, len(fmt.Sprint(e.Removed)))
		c.author = max(c.author, min(ansi.StringWidth(e.Author), maxAuthorWidth))
		c.subject = max(c.subject, ansi.StringWidth(e.Subject))
		c.fixed = max(c.fixed, ansi.StringWidth(e.Short)+ansi.StringWidth(e.Date))
	}
	l.cols = c
}

func (l *Log) View() tea.View { return tea.NewView(l.frame()) }

func (l *Log) frame() string {
	if l.flowShown() {
		return l.flow.frame()
	}
	if l.open != nil {
		return l.open.app.frame()
	}
	if l.width == 0 {
		return ""
	}
	t := l.cfg.Theme
	addW, remW, authW := l.cols.added, l.cols.removed, l.cols.author
	// The subject gets the room left by the other columns, so that author
	// and date stay aligned and visible.
	fixedW := l.cols.fixed + 1 + 2 + (addW + 1) + 1 + (remW + 1) + 2 + 2 + authW + 2
	subjW := max(min(l.cols.subject, l.width-fixedW), 1)

	head := " " + t.Branch.Render(l.branch) + " " +
		lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("(%d commits)", len(l.commits)))
	item := func(i int, st func(lipgloss.Style) lipgloss.Style) string {
		c := l.commits[i]
		author := pad(ansi.Truncate(c.Author, maxAuthorWidth, "…"), authW, lipgloss.NewStyle())
		subject := ansi.Truncate(c.Subject, subjW, "…")
		subject += strings.Repeat(" ", subjW-ansi.StringWidth(subject))
		return " " + st(t.CommitHash).Render(c.Short) + "  " + counts(t, st, c.Added, c.Removed, addW, remW) + "  " +
			st(lipgloss.NewStyle()).Render(subject) + "  " + st(t.Decoration).Render(author) + "  " +
			st(t.Decoration).Render(c.Date)
	}
	hints := statusKeys(l.cfg, barKeys{enter: "open", flow: true})
	return l.render(t, head, item, func(h int) string { return l.messageBox(l.commits[l.cursor].Commit, h) }, pageStatus(l.cfg, l.msg, hints))
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
