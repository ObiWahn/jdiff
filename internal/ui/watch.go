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

// diskWatch watches the files of a view on disk: by the watcher App
// shares, which it gives the view before Init (shareWatcher) and waits on, or
// by one of the view's own, which the view waits on and closes. The files it
// does not watch, all without a watcher, the view checks every
// watch_interval (poll), ticking while such a check is pending.
type diskWatch struct {
	w       *watch.Watcher
	own     bool
	ticking bool
}

// share makes dw watch by w, which App shares; a pending check stays.
func (dw *diskWatch) share(w *watch.Watcher) { dw.w, dw.own = w, false }

// poll returns the next check, for owner, of the files at paths, the view's
// to check, unless one is pending, auto reload is off, there is no
// watch_interval or all of them are watched.
func (dw *diskWatch) poll(cfg *config.Config, owner tea.Model, paths []string) tea.Cmd {
	if dw.ticking || !cfg.AutoReload || cfg.Settings.WatchInterval <= 0 || !dw.unwatched(paths) {
		return nil
	}
	dw.ticking = true
	return watchTick(cfg, owner)
}

// ticked reports whether owner checks its files for msg: it is owner's check
// or anyone's, which is then no longer pending, and auto reload is on.
func (dw *diskWatch) ticked(cfg *config.Config, owner tea.Model, msg watchTickMsg) bool {
	if msg.owner != nil && msg.owner != owner {
		return false
	}
	dw.ticking = false
	return cfg.AutoReload
}

// unwatched reports whether a change of a file at paths is not reported.
func (dw *diskWatch) unwatched(paths []string) bool {
	return slices.ContainsFunc(paths, func(p string) bool { return !dw.watched(p) })
}

// set watches the files at paths in place of those before, starting the
// view's own watcher when it has none, there are files and inotify_max_dirs
// allows one. It returns the command waiting for the changes of a watcher
// just started, and why the system refused, or "".
func (dw *diskWatch) set(cfg *config.Config, paths []string) (cmd tea.Cmd, msg string) {
	if cfg.Settings.WatchInterval <= 0 {
		return nil, ""
	}
	if dw.w == nil {
		if cfg.Settings.InotifyMaxDirs <= 0 || len(paths) == 0 {
			return nil, ""
		}
		w, err := watch.New(cfg.Settings.InotifyMaxDirs)
		if err != nil {
			return nil, refused(cfg, err)
		}
		dw.w, dw.own, cmd = w, true, waitChanges(w)
	}
	if err := dw.w.SetFiles(paths); err != nil {
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
// watcher, nil for the shared one, which App waits on.
func (dw *diskWatch) next() tea.Cmd {
	if !dw.own || dw.w == nil {
		return nil
	}
	return waitChanges(dw.w)
}

// watched reports whether a change of the file at path is reported.
func (dw *diskWatch) watched(path string) bool { return dw.w != nil && dw.w.Watched(path) }

// touch counts the directories of the files at paths as used.
func (dw *diskWatch) touch(paths []string) {
	if dw.w != nil {
		dw.w.Touch(paths)
	}
}

// limit applies inotify_max_dirs, set in the settings popup, also to the
// shared watcher, before the view's next poll; App applies it too, see "Kept
// on purpose" in doc/development.md.
func (dw *diskWatch) limit(cfg *config.Config) {
	if dw.w != nil {
		dw.w.SetLimit(cfg.Settings.InotifyMaxDirs)
	}
}

// ended reports whether w, which stopped reporting, was the watcher, which
// is then dropped.
func (dw *diskWatch) ended(w *watch.Watcher) bool {
	if w == nil || w != dw.w {
		return false
	}
	dw.w = nil
	return true
}

// stop stops watching the files: the view's own watcher is closed, the
// shared one, which stays, watches them no more.
func (dw *diskWatch) stop() {
	switch {
	case dw.w == nil:
	case dw.own:
		dw.w.Close()
		dw.w = nil
	default:
		dw.w.SetFiles(nil)
	}
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
