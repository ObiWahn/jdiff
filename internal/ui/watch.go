package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
