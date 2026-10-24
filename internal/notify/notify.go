// Package notify reports changes to files as the kernel sees them, instead
// of checking them over and over. It watches the directories the files are
// in, not the files: editors often save by writing a new file and renaming
// it over the old one, which a watch on the old file would not see. Only
// these directories are watched, not their subdirectories.
//
// Only Linux (inotify) is supported; elsewhere New fails with ErrUnsupported
// and the caller checks the files itself.
package notify

import (
	"errors"
	"path/filepath"
	"slices"
)

var (
	// ErrUnsupported is returned where the kernel cannot report changes.
	ErrUnsupported = errors.New("not supported on this system")
	// ErrTooManyDirs is returned when the files are in more directories
	// than allowed.
	ErrTooManyDirs = errors.New("files in too many directories")
)

// watched maps each path the kernel names to the paths it was given as: a
// path stands for itself and, when it is a symlink, for its target, which
// is what git difftool --dir-diff puts in place of working tree files.
func watched(paths []string) map[string][]string {
	names := map[string][]string{}
	for _, p := range paths {
		p = filepath.Clean(p)
		names[p] = append(names[p], p)
		if target, err := filepath.EvalSymlinks(p); err == nil && target != p {
			names[target] = append(names[target], p)
		}
	}
	return names
}

// dirs returns the directories of the paths of names, sorted.
func dirs(names map[string][]string) []string {
	var out []string
	for p := range names {
		out = append(out, filepath.Dir(p))
	}
	slices.Sort(out)
	return slices.Compact(out)
}
