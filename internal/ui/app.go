package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"jdiff/internal/config"
	"jdiff/internal/source"
	"jdiff/watch"
)

// App is the top-level model of jdiff: it wraps the page shown, whatever it
// is, and keeps what all pages share: the watcher, auto reload, and git diff
// switched to by d, which asks for the arguments of git diff in a popup,
// whose files then take the page's place, as with jdiff diff. When git
// fails, the popup stays open and says why.
type App struct {
	tea.Model           // the view in its wrappers
	view      tea.Model // the view itself
	wrap      func(tea.Model) tea.Model
	cfg       *config.Config
	// w is the watcher the views share, nil where the system has none;
	// limit is the inotify_max_dirs it has.
	w      *watch.Watcher
	limit  int
	auto   autoReload
	popup  diffPopup
	width  int
	height int
}

// diffPopup is the popup of git diff of App: run lists the diff of the
// arguments asked for in input, which is set while it asks; err is why git
// failed, until the next key. running is set while git diff runs for the
// request number gen of show; cancel ends it.
type diffPopup struct {
	run     DiffFunc
	input   *lineInput
	err     string
	running bool
	gen     int
	cancel  context.CancelFunc
}

// NewApp returns the app showing view in the search bar, the help, the
// settings and the marks popups, as it does the views that replace it;
// gitDiff runs git diff.
func NewApp(cfg *config.Config, view tea.Model, gitDiff DiffFunc) *App {
	return newApp(cfg, view, func(m tea.Model) tea.Model {
		return WithSearch(cfg, WithHelp(cfg, WithSettings(cfg, WithMarks(cfg, m))))
	}, gitDiff)
}

// newApp returns the app showing view, which wrap puts in its wrappers, as
// it does the views that replace it. The tests choose the wrappers.
func newApp(cfg *config.Config, view tea.Model, wrap func(tea.Model) tea.Model, gitDiff DiffFunc) *App {
	return &App{Model: wrap(view), view: view, wrap: wrap, cfg: cfg, popup: diffPopup{run: gitDiff}}
}

// WithAutoReload sets how auto reload gets the state of the repository, the
// files to watch for changes of it and the directories of its working tree,
// and returns s.
func (s *App) WithAutoReload(status func(ctx context.Context) (string, error), paths func() ([]string, error),
	dirs func(ignore func(rel string) bool) (map[string]time.Time, error)) *App {
	s.auto.repoStatus, s.auto.repoPaths, s.auto.workDirs = status, paths, dirs
	return s
}

// Init starts the watcher the views share, unless watching is off, and
// gives it to the view, before it starts.
func (s *App) Init() tea.Cmd {
	var wait tea.Cmd
	if s.cfg.Settings.WatchInterval > 0 {
		if w, err := watch.New(s.cfg.Settings.InotifyMaxDirs); err == nil {
			s.w, s.limit, wait = w, s.cfg.Settings.InotifyMaxDirs, waitChanges(w)
			s.share()
		}
	}
	return batch(s.Model.Init(), wait)
}

// share gives the watcher to the view, if it watches files.
func (s *App) share() {
	if v, ok := s.view.(interface{ shareWatcher(*watch.Watcher) }); ok && s.w != nil {
		v.shareWatcher(s.w)
	}
}

// current returns the arguments of the git diff shown, and whether the view
// shows one.
func (s *App) current() ([]string, bool) {
	if d, ok := s.view.(*Diff); ok && d.gitDiff != nil {
		return d.diffArgs, true
	}
	return nil, false
}

func (s *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
	case askDiffMsg:
		s.popup.input, s.popup.err = &lineInput{}, ""
		s.endDiff()
		return s, nil
	case diffListedMsg:
		return s, s.showDiff(msg)
	case askAutoReloadMsg:
		return s, s.toggleAutoReload()
	case autoTickMsg:
		return s, s.onAutoTick(msg)
	case autoStatusMsg:
		return s, s.onAutoStatus(msg)
	case changedMsg:
		if s.w != nil && msg.w == s.w {
			check := s.onRepoChanged(msg.paths)
			var cmd tea.Cmd
			s.Model, cmd = s.Model.Update(msg)
			return s, batch(cmd, check, waitChanges(s.w))
		}
	case watchEndedMsg:
		if s.w != nil && msg.w == s.w {
			s.w = nil // the regular checks still see all changes; the view checks its files
		}
	case repoWatchMsg:
		s.onRepoWatch(msg)
		return s, nil
	case tea.KeyPressMsg:
		s.auto.lastActive = time.Now()
		if s.popup.input != nil {
			s.popup.err = ""
			return s, s.key(msg)
		}
	case tea.PasteMsg:
		if s.popup.input != nil {
			s.popup.err = ""
			s.popup.input.paste(msg.Content)
			return s, nil
		}
	}
	var cmd tea.Cmd
	s.Model, cmd = s.Model.Update(msg)
	// Set in the settings popup; Diff and FileDiff applied it already
	// (diskWatch.limit), not the views without files on disk. See "Kept on
	// purpose" in doc/development.md.
	if s.w != nil && s.limit != s.cfg.Settings.InotifyMaxDirs {
		s.limit = s.cfg.Settings.InotifyMaxDirs
		s.w.SetLimit(s.limit)
	}
	return s, cmd
}

// key edits the arguments asked for; all text typed goes into the line. Up
// takes the arguments of the git diff shown to edit. Enter shows the diff of
// the line; with nothing typed it closes the popup, as esc and backspace on
// it do.
func (s *App) key(k tea.KeyPressMsg) tea.Cmd {
	in := s.popup.input
	switch {
	case k.String() == "ctrl+c":
		return tea.Quit
	case in.cancels(k):
		s.popup.input = nil
		s.endDiff()
		return nil
	}
	switch k.String() {
	case "enter":
		args, err := splitArgs(in.text)
		if err != nil {
			s.popup.err = err.Error()
			return nil
		}
		if len(args) == 0 {
			s.popup.input = nil
			return nil
		}
		return s.show(args)
	case "up":
		if args, ok := s.current(); ok {
			in.text = joinArgs(args)
		}
	default:
		in.edit(k)
	}
	return nil
}

// splitArgs splits text into arguments at spaces, as a shell does for
// quotes: 'single' and "double" quotes keep spaces, a backslash outside
// single quotes the character after it.
func splitArgs(text string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg, quote, escaped := false, rune(0), false
	for _, r := range text {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inArg = true, true
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote, inArg = r, true
		case r == ' ':
			if inArg {
				args, inArg = append(args, cur.String()), false
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("missing closing %c", quote)
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

// joinArgs returns args as splitArgs reads them back: quoted where they hold
// spaces, quotes or backslashes, or are empty.
func joinArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = a
		if a == "" || strings.ContainsAny(a, " '\"\\") {
			quoted[i] = "'" + strings.ReplaceAll(a, "'", `'"'"'`) + "'"
		}
	}
	return strings.Join(quoted, " ")
}

// endDiff ends the git diff of the popup, if one runs: its files are no
// longer wanted.
func (s *App) endDiff() {
	s.popup.running = false
	if s.popup.cancel != nil {
		s.popup.cancel()
		s.popup.cancel = nil
	}
}

// stop ends all git commands of s and its view: the program ends.
func (s *App) stop() {
	s.endDiff()
	s.stopAutoReload()
	if v, ok := s.view.(interface{ stop() }); ok {
		v.stop()
	}
	if s.w != nil {
		s.w.Close()
	}
}

// diffListedMsg brings the files of git diff with args, run for the request
// number gen of show, or why git failed.
type diffListedMsg struct {
	gen   int
	args  []string
	files []source.File
	err   error
}

// show returns the command running git diff with args in the background,
// for showDiff. The popup stays open meanwhile and says so; esc closes it
// and drops the files, as another request does.
func (s *App) show(args []string) tea.Cmd {
	s.endDiff()
	s.popup.gen++
	s.popup.running = true
	ctx, cancel := context.WithCancel(context.Background())
	s.popup.cancel = cancel
	gen, untracked, gitDiff := s.popup.gen, s.cfg.Settings.Untracked, s.popup.run
	return func() tea.Msg {
		files, err := gitDiff(ctx, args, untracked)
		return diffListedMsg{gen, args, files, err}
	}
}

// showDiff replaces the view by the files msg brings, from scratch, closes
// the popup and returns the command starting the new view; when the popup was
// closed or asked again meanwhile, it drops them. When git failed, the view
// and the popup stay, which shows the first line of git's message.
func (s *App) showDiff(msg diffListedMsg) tea.Cmd {
	if msg.gen != s.popup.gen || !s.popup.running { // closed or opened again meanwhile
		return nil
	}
	s.endDiff()
	args, files := msg.args, msg.files
	if msg.err != nil {
		s.popup.err = firstLine(msg.err)
		return nil
	}
	s.popup.input = nil
	if v, ok := s.view.(interface{ stop() }); ok {
		v.stop()
	}
	d := NewDiff(s.cfg, files).WithGitDiff(args, s.popup.run)
	if len(files) > 0 {
		d.setMsg(fmt.Sprintf("%s: %d files", diffCommand(args), len(files)))
	}
	s.view, s.Model = d, s.wrap(d)
	s.share()
	s.Model.Update(tea.WindowSizeMsg{Width: s.width, Height: s.height})
	cmd := s.Model.Init()
	if s.cfg.AutoReload && !s.auto.gitMode { // now git diff: checked by git status
		s.stopAutoReload()
		cmd = batch(cmd, s.toggleAutoReload())
	}
	return cmd
}

// frame draws the popup over the view while it asks: the line typed,
// centered, and why git failed. The popup grows with the line, up to the
// width of the screen; beyond, the line's start is cut so that its end
// stays in view.
func (s *App) View() tea.View { return tea.NewView(s.frame()) }

func (s *App) frame() string {
	base := s.Model.View().Content
	if s.popup.input == nil || s.width == 0 {
		return base
	}
	t := s.cfg.Theme
	keys := "enter show · esc cancel"
	if _, ok := s.current(); ok {
		keys = "enter show · ↑ current · esc cancel"
	}
	msg := ""
	if s.popup.running {
		msg = "running git diff…"
	}
	pop, w := inputBox(t, s.width, popupTitle(t, "git diff", msg, keys), printable(s.popup.input.text), s.popup.err, true)
	return overlay(base, pop, w, s.width)
}
