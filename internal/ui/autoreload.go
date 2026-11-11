package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"jdiff/internal/gitsrc"
)

// autoReload is the state of auto reload of App: repoStatus returns the
// state of the repository, which is checked, while gitMode is set, every
// auto_reload_interval and at once when App's watcher reports a change of the
// files of repoPaths, pinned, or in the directories of workDirs. statusRuns
// counts the checks under way, statusAgain asks for one more after them; tick
// numbers the regular checks, of which only the last counts; gen tells this
// run of auto reload from earlier ones; ctx ends its git commands, cancel
// ends ctx.
type autoReload struct {
	repoStatus  func(ctx context.Context) (string, error)
	repoPaths   func() ([]string, error)
	workDirs    func(ignore func(rel string) bool) (map[string]time.Time, error)
	gitMode     bool
	tick        int
	pins        []string
	statusAgain bool
	lastStatus  string
	lastActive  time.Time
	statusRuns  int
	gen         int
	ctx         context.Context
	cancel      context.CancelFunc
}

// autoReloadMsg tells the view that auto reload went on or off: it watches
// its files on disk only while it is on.
type autoReloadMsg struct{ on bool }

// autoTickMsg asks App for the next check of auto reload number gen,
// the tick number tick of it; a later or ended one drops it, and so does a
// change reported, which starts the next tick anew.
type autoTickMsg struct{ gen, tick int }

// autoStatusMsg is the result of git status for auto reload number gen.
type autoStatusMsg struct {
	gen    int
	status string
	err    error
}

// toggleAutoReload starts or stops auto reload and returns the commands of
// its checks. On, the view watches its files on disk. Of git diff and git
// log, the repository is watched and checked by git status too, and what is
// shown loaded again at once, since the repository may have changed since
// the last load; of two files or directories, only the files shown are
// watched (gitMode off), and the ticks only end it after the timeout. When it
// cannot start, the view says why.
func (s *App) toggleAutoReload() tea.Cmd {
	if s.cfg.AutoReload {
		s.stopAutoReload()
		return nil
	}
	s.auto.gitMode = s.auto.repoStatus != nil && s.gitView()
	if s.auto.gitMode {
		if s.cfg.Settings.AutoReloadInterval <= 0 {
			return showAutoReloadError(errors.New("auto_reload_interval is 0"))
		}
		ctx, cancel := context.WithCancel(context.Background())
		status, err := s.auto.repoStatus(ctx)
		if err != nil {
			cancel()
			return showAutoReloadError(err)
		}
		s.auto.ctx, s.auto.cancel, s.auto.lastStatus = ctx, cancel, status
	}
	s.cfg.AutoReload, s.auto.lastActive = true, time.Now()
	s.auto.gen++
	var on, cmd tea.Cmd
	s.Model, on = s.Model.Update(autoReloadMsg{on: true})
	if s.auto.gitMode {
		s.Model, cmd = s.Model.Update(refreshMsg{quiet: true})
	}
	return batch(cmd, on, s.autoTick(), s.watchRepo())
}

// switchAutoReload toggles auto reload for its key and saves whether it is
// on, so that the next start starts it too: off when it could not start.
// It ends --auto-reload, and with it the run without a timeout.
func (s *App) switchAutoReload() tea.Cmd {
	s.cfg.Flags.AutoReload = false
	cmd := s.toggleAutoReload()
	s.cfg.Settings.AutoReload = s.cfg.AutoReload
	if w := saveState(s.cfg); w != "" {
		cmd = batch(cmd, func() tea.Msg { return showMsg(w) })
	}
	return cmd
}

// gitView reports whether the view shows git diff or git log, which auto
// reload checks by git status.
func (s *App) gitView() bool {
	_, diff := s.current()
	_, log := s.view.(*Log)
	return diff || log
}

// stopAutoReload ends auto reload: its git commands are killed, and none
// run for it from now on; nothing is watched.
func (s *App) stopAutoReload() {
	s.cfg.AutoReload = false
	s.auto.gen++
	if s.auto.cancel != nil {
		s.auto.cancel()
		s.auto.cancel = nil
	}
	if s.w != nil {
		s.w.Pin(nil)
		s.w.SetDirs(nil)
	}
	s.auto.pins, s.auto.statusAgain, s.auto.gitMode = nil, false, false
	s.Model, _ = s.Model.Update(autoReloadMsg{on: false})
}

// autoTick returns the command for the next regular check, nil without an
// interval.
func (s *App) autoTick() tea.Cmd {
	if s.cfg.Settings.AutoReloadInterval <= 0 {
		return nil
	}
	s.auto.tick++
	gen, tick := s.auto.gen, s.auto.tick
	every := time.Duration(s.cfg.Settings.AutoReloadInterval) * time.Second
	return tea.Tick(every, func(time.Time) tea.Msg { return autoTickMsg{gen, tick} })
}

// checkStatus returns the command running git status.
func (s *App) checkStatus() tea.Cmd {
	s.auto.statusRuns++
	gen, status, ctx := s.auto.gen, s.auto.repoStatus, s.auto.ctx
	return func() tea.Msg {
		st, err := status(ctx)
		return autoStatusMsg{gen, st, err}
	}
}

// onAutoTick ends auto reload after the timeout without activity, unless
// --auto-reload started it, else checks
// the status, unless git is still at it, as in a large repository, or there
// is no git status to check.
func (s *App) onAutoTick(msg autoTickMsg) tea.Cmd {
	if msg.gen != s.auto.gen || msg.tick != s.auto.tick || !s.cfg.AutoReload {
		return nil
	}
	// In minutes as float: a large timeout overflows a time.Duration.
	if m := s.cfg.Settings.AutoReloadTimeout; m > 0 && !s.cfg.Flags.AutoReload && time.Since(s.auto.lastActive).Minutes() >= float64(m) {
		s.stopAutoReload()
		return func() tea.Msg { return showMsg(fmt.Sprintf("auto reload off: %d min without keys or changes", m)) }
	}
	if s.auto.statusRuns > 0 || !s.auto.gitMode {
		return s.autoTick()
	}
	return tea.Batch(s.autoTick(), s.checkStatus())
}

// onAutoStatus loads what is shown again when the status differs from the last
// one, which counts as activity, and watches the repository anew, whose files
// to watch it may have changed. The directories of the files it names anew
// count as used. git status elsewhere, or git diff refreshing the index,
// makes the watcher report a change but leaves the status as it is. When git
// fails, auto reload ends. A change reported while git was at it is checked
// once more.
func (s *App) onAutoStatus(msg autoStatusMsg) tea.Cmd {
	s.auto.statusRuns--
	if msg.gen != s.auto.gen {
		return nil
	}
	if msg.err != nil {
		s.stopAutoReload()
		return showAutoReloadError(msg.err)
	}
	var again tea.Cmd
	if s.auto.statusAgain && s.auto.statusRuns == 0 {
		s.auto.statusAgain, again = false, s.checkStatus()
	}
	if msg.status == s.auto.lastStatus {
		return again
	}
	if s.w != nil {
		var paths []string
		for _, p := range gitsrc.StatusPaths(msg.status, s.auto.lastStatus) {
			if abs, err := filepath.Abs(p); err == nil { // relative to where git ran
				paths = append(paths, abs)
			}
		}
		s.w.Touch(paths)
	}
	s.auto.lastStatus, s.auto.lastActive = msg.status, time.Now()
	var cmd tea.Cmd
	s.Model, cmd = s.Model.Update(refreshMsg{quiet: true})
	return batch(cmd, s.watchRepo(), again)
}

// onRepoChanged checks the status at once when the watcher reported a change
// of the repository or of the working tree, unless only of files that
// watch_ignore matches; while git is at it, once more after. The next regular
// check comes a full interval later. Without git status, a change of the
// files shown counts as activity.
func (s *App) onRepoChanged(paths []string) tea.Cmd {
	if !s.cfg.AutoReload {
		return nil
	}
	if !s.auto.gitMode {
		s.auto.lastActive = time.Now()
		return nil
	}
	if !slices.ContainsFunc(paths, s.repoChange) {
		return nil
	}
	if s.auto.statusRuns > 0 {
		s.auto.statusAgain = true
		return s.autoTick()
	}
	return batch(s.checkStatus(), s.autoTick())
}

// repoChange reports whether a change of path counts for auto reload: of the
// pinned files of the repository, or in the working tree, but of those that
// watch_ignore matches.
func (s *App) repoChange(path string) bool {
	if slices.Contains(s.auto.pins, path) {
		return true
	}
	rel := path
	if top := s.cfg.Repo(); top != "" {
		r, err := filepath.Rel(top, path)
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return false
		}
		rel = r
	}
	return !s.cfg.WatchIgnored(filepath.ToSlash(rel))
}

// repoWatchMsg brings, for auto reload number gen, the files of the
// repository to watch and the directories of its working tree.
type repoWatchMsg struct {
	gen  int
	pins []string
	dirs map[string]time.Time
}

// watchRepo returns the command finding in the background what auto reload
// watches of git: the files of the repository, which a checkout changes, and
// the directories of the working tree, unless inotify_max_dirs is 0; nil
// without a watcher or git. When git fails,
// the watcher keeps what it watched: the regular checks still see all
// changes, only later.
func (s *App) watchRepo() tea.Cmd {
	if s.w == nil || s.auto.repoPaths == nil || !s.auto.gitMode {
		return nil
	}
	gen, paths, workDirs, ignored := s.auto.gen, s.auto.repoPaths, s.auto.workDirs, s.cfg.WatchIgnored
	// Without directories to watch, the working tree is not looked at.
	scan := workDirs != nil && s.cfg.Settings.InotifyMaxDirs > 0
	return func() tea.Msg {
		pins, err := paths()
		if err != nil {
			return nil
		}
		var dirs map[string]time.Time
		if scan {
			if dirs, err = workDirs(ignored); err != nil {
				return nil
			}
		}
		return repoWatchMsg{gen, pins, dirs}
	}
}

// onRepoWatch watches what msg brings, the files of the repository always,
// unless auto reload ended meanwhile.
func (s *App) onRepoWatch(msg repoWatchMsg) {
	if msg.gen != s.auto.gen || !s.cfg.AutoReload || s.w == nil {
		return
	}
	s.auto.pins = msg.pins
	s.w.Pin(msg.pins)
	s.w.SetDirs(msg.dirs)
}

// showAutoReloadError returns the command showing why auto reload is off.
func showAutoReloadError(err error) tea.Cmd {
	return func() tea.Msg { return showMsg("auto reload: " + firstLine(err)) }
}
