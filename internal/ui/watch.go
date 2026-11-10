package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"jdiff/internal/config"
	"jdiff/internal/gitsrc"
	"jdiff/internal/source"
	"jdiff/watch"
)

// racyWindow is how long after its modification time a file is compared by
// content: file systems keep the time in coarse steps, so a change of the
// same size right after the last read may leave the stamp as it was.
const racyWindow = 2 * time.Second

// watchTickMsg asks the top-level view to check its watched files. owner is
// the view that asked for it; another view, which took its place, drops it.
// Without owner, any view takes it.
type watchTickMsg struct{ owner tea.Model }

// changedMsg reports files on disk that changed, by their paths cleaned,
// from inotify watcher w.
type changedMsg struct {
	w     *watch.Watcher
	paths []string
}

// watchEndedMsg reports that inotify watcher w stopped reporting changes: it
// was closed, or a directory could not be watched any more. The view whose
// watcher it still is checks its files itself from now on.
type watchEndedMsg struct{ w *watch.Watcher }

// waitChanges returns the command waiting for the next changes w reports.
func waitChanges(w *watch.Watcher) tea.Cmd {
	return func() tea.Msg {
		paths, ok := <-w.Changes()
		if !ok {
			return watchEndedMsg{w}
		}
		return changedMsg{w, paths}
	}
}

// diskWatch watches the files of a view on disk: by the watcher DiffSwitch
// shares, which it gives the view before Init (shareWatcher) and waits on, or
// by one of the view's own, which the view waits on and closes. The files it
// does not watch, all without a watcher, the view checks every
// watch_interval.
type diskWatch struct {
	w   *watch.Watcher
	own bool
}

// set watches the files at paths in place of those before, starting the
// view's own watcher when it has none, there are files and inotify_max_dirs
// allows one. It
// returns the command waiting for the changes of a watcher just started, and
// why the system refused, or "".
func (d *diskWatch) set(cfg *config.Config, paths []string) (cmd tea.Cmd, msg string) {
	if cfg.Settings.WatchInterval <= 0 {
		return nil, ""
	}
	if d.w == nil {
		if cfg.Settings.InotifyMaxDirs <= 0 || len(paths) == 0 {
			return nil, ""
		}
		w, err := watch.New(cfg.Settings.InotifyMaxDirs)
		if err != nil {
			return nil, refused(cfg, err)
		}
		d.w, d.own, cmd = w, true, waitChanges(w)
	}
	if err := d.w.SetFiles(paths); err != nil {
		msg = refused(cfg, err)
	}
	return cmd, msg
}

// refused returns what to say when the system refused to watch, nothing
// where it cannot.
func refused(cfg *config.Config, err error) string {
	if errors.Is(err, watch.ErrUnsupported) {
		return ""
	}
	return err.Error() + "; checking files every " + strconv.FormatFloat(cfg.Settings.WatchInterval, 'g', -1, 64) + "s"
}

// next returns the command waiting for the next changes of the view's own
// watcher, nil for the shared one, which DiffSwitch waits on.
func (d *diskWatch) next() tea.Cmd {
	if !d.own || d.w == nil {
		return nil
	}
	return waitChanges(d.w)
}

// watched reports whether a change of the file at path is reported.
func (d *diskWatch) watched(path string) bool { return d.w != nil && d.w.Watched(path) }

// touch counts the directories of the files at paths as used.
func (d *diskWatch) touch(paths []string) {
	if d.w != nil {
		d.w.Touch(paths)
	}
}

// limit applies inotify_max_dirs, set in the settings popup.
func (d *diskWatch) limit(cfg *config.Config) {
	if d.w != nil {
		d.w.SetLimit(cfg.Settings.InotifyMaxDirs)
	}
}

// ended reports whether w, which stopped reporting, was the watcher, which
// is then dropped.
func (d *diskWatch) ended(w *watch.Watcher) bool {
	if w == nil || w != d.w {
		return false
	}
	d.w = nil
	return true
}

// stop stops watching the files: the view's own watcher is closed, the
// shared one, which stays, watches them no more.
func (d *diskWatch) stop() {
	switch {
	case d.w == nil:
	case d.own:
		d.w.Close()
		d.w = nil
	default:
		d.w.SetFiles(nil)
	}
}

// autoReloadMsg tells the view that auto reload went on or off: it watches
// its files on disk only while it is on.
type autoReloadMsg struct{ on bool }

// autoTickMsg asks DiffSwitch for the next check of auto reload number gen,
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
func (s *DiffSwitch) toggleAutoReload() tea.Cmd {
	if s.cfg.AutoReload {
		s.stopAutoReload()
		return nil
	}
	s.gitMode = s.repoStatus != nil && s.gitView()
	if s.gitMode {
		if s.cfg.Settings.AutoReloadInterval <= 0 {
			return showAutoReloadError(errors.New("auto_reload_interval is 0"))
		}
		ctx, cancel := context.WithCancel(context.Background())
		status, err := s.repoStatus(ctx)
		if err != nil {
			cancel()
			return showAutoReloadError(err)
		}
		s.autoCtx, s.autoCancel, s.lastStatus = ctx, cancel, status
	}
	s.cfg.AutoReload, s.lastActive = true, time.Now()
	s.autoGen++
	var on, cmd tea.Cmd
	s.Model, on = s.Model.Update(autoReloadMsg{on: true})
	if s.gitMode {
		s.Model, cmd = s.Model.Update(refreshMsg{quiet: true})
	}
	return batch(cmd, on, s.autoTick(), s.watchRepo())
}

// gitView reports whether the view shows git diff or git log, which auto
// reload checks by git status.
func (s *DiffSwitch) gitView() bool {
	_, diff := s.current()
	_, log := s.view.(*Log)
	return diff || log
}

// stopAutoReload ends auto reload: its git commands are killed, and none
// run for it from now on; nothing is watched.
func (s *DiffSwitch) stopAutoReload() {
	s.cfg.AutoReload = false
	s.autoGen++
	if s.autoCancel != nil {
		s.autoCancel()
		s.autoCancel = nil
	}
	if s.w != nil {
		s.w.Pin(nil)
		s.w.SetDirs(nil)
	}
	s.pins, s.statusAgain, s.gitMode = nil, false, false
	s.Model, _ = s.Model.Update(autoReloadMsg{on: false})
}

// autoTick returns the command for the next regular check, nil without an
// interval.
func (s *DiffSwitch) autoTick() tea.Cmd {
	if s.cfg.Settings.AutoReloadInterval <= 0 {
		return nil
	}
	s.tick++
	gen, tick := s.autoGen, s.tick
	every := time.Duration(s.cfg.Settings.AutoReloadInterval) * time.Second
	return tea.Tick(every, func(time.Time) tea.Msg { return autoTickMsg{gen, tick} })
}

// checkStatus returns the command running git status.
func (s *DiffSwitch) checkStatus() tea.Cmd {
	s.statusRuns++
	gen, status, ctx := s.autoGen, s.repoStatus, s.autoCtx
	return func() tea.Msg {
		st, err := status(ctx)
		return autoStatusMsg{gen, st, err}
	}
}

// onAutoTick ends auto reload after the timeout without activity, else checks
// the status, unless git is still at it, as in a large repository, or there
// is no git status to check.
func (s *DiffSwitch) onAutoTick(msg autoTickMsg) tea.Cmd {
	if msg.gen != s.autoGen || msg.tick != s.tick || !s.cfg.AutoReload {
		return nil
	}
	// In minutes as float: a large timeout overflows a time.Duration.
	if m := s.cfg.Settings.AutoReloadTimeout; m > 0 && time.Since(s.lastActive).Minutes() >= float64(m) {
		s.stopAutoReload()
		return func() tea.Msg { return showMsg(fmt.Sprintf("auto reload off: %d min without keys or changes", m)) }
	}
	if s.statusRuns > 0 || !s.gitMode {
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
func (s *DiffSwitch) onAutoStatus(msg autoStatusMsg) tea.Cmd {
	s.statusRuns--
	if msg.gen != s.autoGen {
		return nil
	}
	if msg.err != nil {
		s.stopAutoReload()
		return showAutoReloadError(msg.err)
	}
	var again tea.Cmd
	if s.statusAgain && s.statusRuns == 0 {
		s.statusAgain, again = false, s.checkStatus()
	}
	if msg.status == s.lastStatus {
		return again
	}
	if s.w != nil {
		var paths []string
		for _, p := range gitsrc.StatusPaths(msg.status, s.lastStatus) {
			if abs, err := filepath.Abs(p); err == nil { // relative to where git ran
				paths = append(paths, abs)
			}
		}
		s.w.Touch(paths)
	}
	s.lastStatus, s.lastActive = msg.status, time.Now()
	var cmd tea.Cmd
	s.Model, cmd = s.Model.Update(refreshMsg{quiet: true})
	return batch(cmd, s.watchRepo(), again)
}

// onRepoChanged checks the status at once when the watcher reported a change
// of the repository or of the working tree, unless only of files that
// watch_ignore matches; while git is at it, once more after. The next regular
// check comes a full interval later. Without git status, a change of the
// files shown counts as activity.
func (s *DiffSwitch) onRepoChanged(paths []string) tea.Cmd {
	if !s.cfg.AutoReload {
		return nil
	}
	if !s.gitMode {
		s.lastActive = time.Now()
		return nil
	}
	if !slices.ContainsFunc(paths, s.repoChange) {
		return nil
	}
	if s.statusRuns > 0 {
		s.statusAgain = true
		return s.autoTick()
	}
	return batch(s.checkStatus(), s.autoTick())
}

// repoChange reports whether a change of path counts for auto reload: of the
// pinned files of the repository, or in the working tree, but of those that
// watch_ignore matches.
func (s *DiffSwitch) repoChange(path string) bool {
	if slices.Contains(s.pins, path) {
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
func (s *DiffSwitch) watchRepo() tea.Cmd {
	if s.w == nil || s.repoPaths == nil || !s.gitMode {
		return nil
	}
	gen, paths, workDirs, ignored := s.autoGen, s.repoPaths, s.workDirs, s.cfg.WatchIgnored
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
func (s *DiffSwitch) onRepoWatch(msg repoWatchMsg) {
	if msg.gen != s.autoGen || !s.cfg.AutoReload || s.w == nil {
		return
	}
	s.pins = msg.pins
	s.w.Pin(msg.pins)
	s.w.SetDirs(msg.dirs)
}

// showAutoReloadError returns the command showing why auto reload is off.
func showAutoReloadError(err error) tea.Cmd {
	return func() tea.Msg { return showMsg("auto reload: " + firstLine(err)) }
}

// diskPaths returns the paths of the sides of f on disk, cleaned as
// changedMsg reports them.
func diskPaths(f source.File) []string {
	var out []string
	for _, p := range []string{f.OldFile, f.NewFile} {
		if p != "" {
			out = append(out, filepath.Clean(p))
		}
	}
	return out
}

// watchTick returns the command for the next check of the watched files,
// nil when watching is off.
func watchTick(cfg *config.Config, owner tea.Model) tea.Cmd {
	interval := cfg.Settings.WatchInterval
	if interval <= 0 {
		return nil
	}
	return tea.Tick(time.Duration(interval*float64(time.Second)), func(time.Time) tea.Msg { return watchTickMsg{owner} })
}

// stale reports whether the file at path may differ from what was read with
// stamp: its stamp changed or, while its modification time is recent, its
// content may have. A file that cannot be read is not.
func stale(path string, stamp source.Stamp) bool {
	st, err := source.StampOf(path)
	return err == nil && (st != stamp || time.Since(st.ModTime) < racyWindow)
}

// refreshFile reloads the sides of f that changed on disk into f and into
// its diff a, unless nil, calling after, unless nil, after each reload of a
// so that views follow their cursor line before the next one. It returns the
// message to show, "" when nothing changed. A side changed when its stamp
// did, or, while its modification time is recent, its content. A side that
// cannot be read is checked again next time; a side that became binary keeps
// its content.
func refreshFile(f *source.File, a *fileAlign, after func()) string {
	type sideFile struct {
		path  string
		stamp *source.Stamp
		lines *[]string
	}
	sides := [2]sideFile{{f.OldFile, &f.OldStamp, &f.Old}, {f.NewFile, &f.NewStamp, &f.New}}
	reloaded, binary, dropped := false, false, 0
	for s, sf := range sides {
		if sf.path == "" {
			continue
		}
		if !stale(sf.path, *sf.stamp) {
			continue
		}
		lines, st, err := source.Read(sf.path)
		if errors.Is(err, source.ErrBinary) {
			*sf.stamp, binary = st, true
			continue
		}
		if err != nil {
			continue
		}
		if slices.Equal(lines, *sf.lines) {
			*sf.stamp = st
			continue
		}
		*sf.stamp, *sf.lines, reloaded = st, lines, true
		if a != nil {
			dropped += a.reload(side(s), lines)
			if after != nil {
				after()
			}
		}
	}
	switch {
	case binary:
		return f.Path + " is binary now, not reloaded"
	case !reloaded:
		return ""
	case dropped == 1:
		return f.Path + " reloaded, 1 anchor dropped"
	case dropped > 1:
		return fmt.Sprintf("%s reloaded, %d anchors dropped", f.Path, dropped)
	}
	return f.Path + " reloaded"
}
