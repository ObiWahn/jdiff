// Package watch reports changes to files as the system sees them, instead
// of checking them over and over. It watches the directories the files are
// in, not the files: editors often save by writing a new file and renaming
// it over the old one, which a watch on the old file would not see, and only
// a directory sees a file appear. Only these directories are watched, not
// their subdirectories.
//
// A Watcher watches the directories of pinned files always, and of the others
// at most a number. When more are wanted, the most recently used ones are
// watched, see Watched, and the callers check the others themselves. A
// directory is used when something in it changes and when Touch says so;
// before either, at the time it or its newest file was modified.
//
// What a system offers is its backend. Only Linux (inotify) has one; elsewhere
// New fails with ErrUnsupported and the callers check the files themselves.
// Another system needs only a backend of its own (backend_<os>.go).
package watch

import (
	"cmp"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// ErrUnsupported is returned where the system cannot report changes.
var ErrUnsupported = errors.New("not supported on this system")

// settle is how long changes are collected after the first one, so that
// the events of one save come as one batch.
const settle = 50 * time.Millisecond

// backend watches single directories, without their subdirectories. Its
// methods may be called from any goroutine; add and remove must not wait for
// the events to be taken.
type backend interface {
	// add starts watching dir and returns the id of its events. A missing
	// dir is an error matching fs.ErrNotExist.
	add(dir string) (int, error)
	// remove stops watching id: no event of it comes after.
	remove(id int)
	// events returns the events, closed after close or when the system
	// stops reporting.
	events() <-chan event
	close() error
}

// event is a change of the entry name of the directory watched as id. With
// name "", the directory itself is gone or moved away, and its watch ended.
// With overflow, the system lost events; id is not set.
type event struct {
	id       int
	name     string
	overflow bool
}

// Watcher reports changes to files and directories. Its methods may be called
// from any goroutine.
type Watcher struct {
	b backend

	mu     sync.Mutex
	limit  int                  // the most directories watched
	files  map[string][]string  // SetFiles, see watched
	pinned map[string][]string  // Pin, see watched
	whole  map[string]bool      // SetDirs: all their entries count
	used   map[string]time.Time // the directories wanted, but pinned ones, and when they were used
	ids    map[int]string       // watch id -> directory
	dirIDs map[string]int       // watched directory -> watch id
	now    func() time.Time     // the time of a use, time.Now but in tests

	raw     chan string
	changes chan []string
	done    chan struct{}
	once    sync.Once
}

// New returns a watcher of at most limit directories, watching none yet. It
// fails when the system cannot report changes.
func New(limit int) (*Watcher, error) {
	b, err := newBackend()
	if err != nil {
		return nil, err
	}
	return newWatcher(b, limit), nil
}

// newWatcher returns a watcher of at most limit directories with b.
func newWatcher(b backend, limit int) *Watcher {
	w := &Watcher{b: b, limit: limit, used: map[string]time.Time{}, ids: map[int]string{}, dirIDs: map[string]int{}, now: time.Now,
		raw: make(chan string), changes: make(chan []string), done: make(chan struct{})}
	go w.read()
	go w.batch()
	return w
}

// SetFiles watches the files at paths, in place of those set before. A
// change of one is reported by its path as given; other entries of their
// directories are not reported. It returns the first error of the system,
// other than for a missing directory, which is left out; the directories
// that could be watched are.
func (w *Watcher) SetFiles(paths []string) error {
	names := watched(paths)
	modified := newest(names)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.files = names
	w.want(modified)
	return w.apply()
}

// Pin watches the files at paths as SetFiles does, in place of those pinned
// before, but always: their directories do not count towards the limit.
func (w *Watcher) Pin(paths []string) error {
	names := watched(paths)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pinned = names
	return w.apply()
}

// SetDirs watches the directories of dirs, in place of those set before, with
// the time each was last modified. A change of any entry is reported by the
// directory joined with its name. It returns errors as SetFiles does.
func (w *Watcher) SetDirs(dirs map[string]time.Time) error {
	cleaned := map[string]time.Time{}
	for d, t := range dirs {
		cleaned[filepath.Clean(d)] = t
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.whole = map[string]bool{}
	for d := range cleaned {
		w.whole[d] = true
	}
	w.want(cleaned)
	return w.apply()
}

// Touch counts the directories of the files at paths, those of symlink
// targets too, as used now: the most recent ones are watched.
func (w *Watcher) Touch(paths []string) error {
	ds := dirs(watched(paths))
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	for _, d := range ds {
		if _, ok := w.used[d]; ok {
			w.used[d] = now
		}
	}
	return w.apply()
}

// SetLimit sets the most directories watched besides those of pinned files;
// with 0 only those are.
func (w *Watcher) SetLimit(limit int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.limit = limit
	return w.apply()
}

// Watched reports whether a change of the file at path is reported now: its
// directory is watched, and so is that of its target when it is a symlink.
func (w *Watcher) Watched(path string) bool {
	names := watched([]string{path})
	w.mu.Lock()
	defer w.mu.Unlock()
	for name := range names {
		if _, ok := w.dirIDs[filepath.Dir(name)]; !ok {
			return false
		}
	}
	return true
}

// Changes returns the channel of changed paths, in sorted batches. It is
// closed when the watcher is, and when the system stops reporting.
func (w *Watcher) Changes() <-chan []string { return w.changes }

// Close stops watching.
func (w *Watcher) Close() error {
	err := os.ErrClosed
	w.once.Do(func() {
		close(w.done)
		err = w.b.close()
	})
	return err
}

// want adds the directories of modified, at the time given, to those wanted
// that are not yet.
func (w *Watcher) want(modified map[string]time.Time) {
	for d, t := range modified {
		if _, ok := w.used[d]; !ok {
			w.used[d] = t
		}
	}
}

// apply watches the pinned directories and the most recently used others, as
// many as the limit allows, and no others: those no longer wanted are dropped.
// A missing directory takes no place. It returns the first error of the
// system other than for a missing directory. w.mu is held.
func (w *Watcher) apply() error {
	wanted := map[string]bool{}
	for d := range w.whole {
		wanted[d] = true
	}
	for _, d := range dirs(w.files) {
		wanted[d] = true
	}
	maps.DeleteFunc(w.used, func(d string, _ time.Time) bool { return !wanted[d] })
	pins := dirs(w.pinned)
	order := slices.Clone(pins)
	others := slices.Collect(maps.Keys(w.used))
	slices.SortFunc(others, func(a, b string) int {
		if c := w.used[b].Compare(w.used[a]); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	for _, d := range others {
		if !slices.Contains(order, d) {
			order = append(order, d)
		}
	}
	// The watches beyond the limit go first, so that the system's limit
	// is not passed on the way.
	keep := order[:min(len(order), len(pins)+max(w.limit, 0))]
	for d, id := range w.dirIDs {
		if !slices.Contains(keep, d) {
			w.b.remove(id)
			delete(w.ids, id)
			delete(w.dirIDs, d)
		}
	}
	var first error
	n := 0 // the others watched
	for _, d := range order {
		pinned := slices.Contains(pins, d)
		if !pinned && n >= w.limit {
			break
		}
		if _, ok := w.dirIDs[d]; ok {
			if !pinned {
				n++
			}
			continue
		}
		id, err := w.b.add(d)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) && first == nil {
				first = err
			}
			continue
		}
		w.ids[id], w.dirIDs[d] = d, id
		if !pinned {
			n++
		}
	}
	return first
}

// read hands the paths of the backend's events to batch, until the watcher
// is closed or the backend ends.
func (w *Watcher) read() {
	defer close(w.raw)
	for ev := range w.b.events() {
		for _, p := range w.paths(ev) {
			select {
			case w.raw <- p:
			case <-w.done:
				return
			}
		}
	}
}

// paths returns the paths ev reports. A change counts as use of its
// directory. When events were lost, all paths count as changed. When a
// watched directory is gone or moved away, as when a checkout removes and
// makes it again, its paths count as changed, and the directory at its path
// is watched again when there is one: else it is not watched, which Watched
// tells.
func (w *Watcher) paths(ev event) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ev.overflow {
		var out []string
		for _, names := range []map[string][]string{w.files, w.pinned} {
			for _, ps := range names {
				out = append(out, ps...)
			}
		}
		return append(out, slices.Collect(maps.Keys(w.whole))...)
	}
	dir, ok := w.ids[ev.id]
	if !ok {
		return nil // removed meanwhile
	}
	if ev.name == "" {
		delete(w.ids, ev.id)
		delete(w.dirIDs, dir)
		var out []string
		for _, names := range []map[string][]string{w.files, w.pinned} {
			for p, ps := range names {
				if filepath.Dir(p) == dir {
					out = append(out, ps...)
				}
			}
		}
		if w.whole[dir] {
			out = append(out, dir)
		}
		w.apply() // what it cannot watch, Watched tells
		return out
	}
	if _, ok := w.used[dir]; ok {
		w.used[dir] = w.now()
	}
	p := filepath.Join(dir, ev.name)
	out := append(slices.Clone(w.files[p]), w.pinned[p]...)
	if w.whole[dir] {
		out = append(out, p)
	}
	return out
}

// batch collects the paths from raw for settle after the first one and
// sends them on as one batch, until raw is closed; the batch collected
// then is still sent.
func (w *Watcher) batch() {
	defer close(w.changes)
	for open := true; open; {
		p, ok := <-w.raw
		if !ok {
			return
		}
		set := map[string]bool{p: true}
		timer := time.NewTimer(settle)
	collect:
		for {
			select {
			case p, ok := <-w.raw:
				if !ok {
					timer.Stop()
					open = false
					break collect
				}
				set[p] = true
			case <-timer.C:
				break collect
			}
		}
		select {
		case w.changes <- slices.Sorted(maps.Keys(set)):
		case <-w.done:
			return
		}
	}
}

// watched maps each path the system names to the paths it was given as: a
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

// newest returns the directories of the paths of names with the time their
// newest file there was modified; a file that cannot be read counts as never.
func newest(names map[string][]string) map[string]time.Time {
	out := map[string]time.Time{}
	for p := range names {
		d := filepath.Dir(p)
		t := out[d]
		if info, err := os.Stat(p); err == nil && info.ModTime().After(t) {
			t = info.ModTime()
		}
		out[d] = t
	}
	return out
}
