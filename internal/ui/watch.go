package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"jdiff/internal/config"
	"jdiff/internal/notify"
	"jdiff/internal/source"
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
	w     *notify.Watcher
	paths []string
}

// waitChanges returns the command waiting for the next changes w reports.
func waitChanges(w *notify.Watcher) tea.Cmd {
	return func() tea.Msg {
		paths, ok := <-w.Changes()
		if !ok {
			return nil
		}
		return changedMsg{w, paths}
	}
}

// startWatching starts watching the files at paths and returns the
// watcher and the command waiting for its changes. When inotify is off, not
// supported or the files are in more than inotify_max_dirs directories, the
// watcher is nil and the command the first check of polling; when the
// kernel refuses inotify, msg says why. With watching off or no paths, it
// returns nothing.
func startWatching(cfg *config.Config, owner tea.Model, paths []string) (w *notify.Watcher, cmd tea.Cmd, msg string) {
	if cfg.Settings.WatchInterval <= 0 || len(paths) == 0 {
		return nil, nil, ""
	}
	if cfg.Settings.InotifyMaxDirs > 0 {
		w, err := notify.New(paths, cfg.Settings.InotifyMaxDirs)
		switch {
		case err == nil:
			return w, waitChanges(w), ""
		case !errors.Is(err, notify.ErrUnsupported) && !errors.Is(err, notify.ErrTooManyDirs):
			msg = err.Error() + "; checking files every " + strconv.FormatFloat(cfg.Settings.WatchInterval, 'g', -1, 64) + "s"
		}
	}
	return nil, watchTick(cfg, owner), msg
}

// autoTickMsg asks DiffSwitch for the next check of auto reload number gen;
// a later or ended one drops it.
type autoTickMsg struct{ gen int }

// autoStatusMsg is the result of git status for auto reload number gen.
type autoStatusMsg struct {
	gen    int
	status string
	err    error
}

// toggleAutoReload starts or stops auto reload and returns the commands of
// its checks. Starting also loads again, since the repository may have changed
// since the last load. When it cannot start, the view says why.
func (s *DiffSwitch) toggleAutoReload() tea.Cmd {
	if s.cfg.AutoReload {
		s.stopAutoReload()
		return nil
	}
	if s.repoStatus == nil {
		return showAutoReloadError(errors.New("nothing to watch"))
	}
	if s.cfg.Settings.AutoReloadInterval <= 0 {
		return showAutoReloadError(errors.New("auto_reload_interval is 0"))
	}
	status, err := s.repoStatus()
	if err != nil {
		return showAutoReloadError(err)
	}
	s.cfg.AutoReload, s.lastStatus, s.lastActive = true, status, time.Now()
	s.autoGen++
	s.watchRepo()
	var cmd tea.Cmd
	s.Model, cmd = s.Model.Update(refreshMsg{quiet: true})
	return tea.Batch(cmd, s.autoTick(), s.waitRepo())
}

// stopAutoReload ends auto reload: no git commands run for it from now on.
func (s *DiffSwitch) stopAutoReload() {
	s.cfg.AutoReload = false
	s.autoGen++
	if s.repoWatch != nil {
		s.repoWatch.Close()
		s.repoWatch = nil
	}
}

// autoTick returns the command for the next regular check.
func (s *DiffSwitch) autoTick() tea.Cmd {
	gen := s.autoGen
	every := time.Duration(s.cfg.Settings.AutoReloadInterval) * time.Second
	return tea.Tick(every, func(time.Time) tea.Msg { return autoTickMsg{gen} })
}

// checkStatus returns the command running git status.
func (s *DiffSwitch) checkStatus() tea.Cmd {
	s.statusRuns++
	gen, status := s.autoGen, s.repoStatus
	return func() tea.Msg {
		st, err := status()
		return autoStatusMsg{gen, st, err}
	}
}

// onAutoTick ends auto reload after the timeout without activity, else checks
// the status, unless git is still at it, as in a large repository.
func (s *DiffSwitch) onAutoTick(msg autoTickMsg) tea.Cmd {
	if msg.gen != s.autoGen {
		return nil
	}
	if m := s.cfg.Settings.AutoReloadTimeout; m > 0 && time.Since(s.lastActive) >= time.Duration(m)*time.Minute {
		s.stopAutoReload()
		return func() tea.Msg { return showMsg(fmt.Sprintf("auto reload off: %d min without keys or changes", m)) }
	}
	if s.statusRuns > 0 {
		return s.autoTick()
	}
	return tea.Batch(s.autoTick(), s.checkStatus())
}

// onAutoStatus loads what is shown again when the status differs from the last
// one, which counts as activity. git status elsewhere, or git diff refreshing
// the index, makes the watcher report a change but leaves the status as it is.
// When git fails, auto reload ends.
func (s *DiffSwitch) onAutoStatus(msg autoStatusMsg) tea.Cmd {
	s.statusRuns--
	if msg.gen != s.autoGen {
		return nil
	}
	if msg.err != nil {
		s.stopAutoReload()
		return showAutoReloadError(msg.err)
	}
	if msg.status == s.lastStatus {
		return nil
	}
	s.lastStatus, s.lastActive = msg.status, time.Now()
	var cmd tea.Cmd
	s.Model, cmd = s.Model.Update(refreshMsg{quiet: true})
	return cmd
}

// onRepoChanged checks the status at once after the watcher reported a change.
// The files are watched anew, since a checkout changes the branch's ref.
func (s *DiffSwitch) onRepoChanged() tea.Cmd {
	s.watchRepo()
	return tea.Batch(s.checkStatus(), s.waitRepo())
}

// watchRepo starts watching the repository's files, in place of the watcher
// before, which stays when that fails: the regular checks still see all
// changes, only later. A symlink counts with the directory of its target.
func (s *DiffSwitch) watchRepo() {
	if s.repoPaths == nil {
		return
	}
	paths, err := s.repoPaths()
	if err != nil {
		return
	}
	w, err := notify.New(paths, 2*len(paths))
	if err != nil {
		return
	}
	if s.repoWatch != nil {
		s.repoWatch.Close()
	}
	s.repoWatch = w
}

// waitRepo returns the command waiting for the watcher's next change, nil
// without one.
func (s *DiffSwitch) waitRepo() tea.Cmd {
	if s.repoWatch == nil {
		return nil
	}
	return waitChanges(s.repoWatch)
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

// refreshFile reloads the sides of f that changed on disk into f and into
// its diff d, unless nil, calling after, unless nil, after each reload of d
// so that views follow their cursor line before the next one. It returns the
// message to show, "" when nothing changed. A side changed when its stamp
// did, or, while its modification time is recent, its content. A side that
// cannot be read is checked again next time; a side that became binary keeps
// its content.
func refreshFile(f *source.File, d *fileDiff, after func()) string {
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
		if st, err := source.StampOf(sf.path); err != nil || (st == *sf.stamp && time.Since(st.ModTime) >= racyWindow) {
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
		if d != nil {
			dropped += d.reload(side(s), lines)
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
