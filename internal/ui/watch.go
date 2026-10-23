package ui

import (
	"errors"
	"fmt"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jdiff/internal/config"
	"jdiff/internal/source"
)

// racyWindow is how long after its modification time a file is compared by
// content: file systems keep the time in coarse steps, so a change of the
// same size right after the last read may leave the stamp as it was.
const racyWindow = 2 * time.Second

// watchTickMsg asks the top-level view to check its watched files.
type watchTickMsg struct{}

// watchTick returns the command for the next check of the watched files,
// nil when watching is off.
func watchTick(cfg *config.Config) tea.Cmd {
	interval := cfg.Settings.WatchInterval
	if interval <= 0 {
		return nil
	}
	return tea.Tick(time.Duration(interval*float64(time.Second)), func(time.Time) tea.Msg { return watchTickMsg{} })
}

// refreshFile reloads the sides of f that changed on disk into f and into
// its diff d, unless nil, which view m, unless nil, follows. It returns the
// message to show, "" when nothing changed. A side changed when its stamp
// did, or, while its modification time is recent, its content. A side that
// cannot be read is checked again next time; a side that became binary keeps
// its content.
func refreshFile(f *source.File, d *fileDiff, m *Model) string {
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
			if m != nil {
				m.syncLayout()
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
