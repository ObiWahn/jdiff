package watch

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// symlink links link to target, skipping the test where the system does not
// let it, as Windows without the right.
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlink: %v", err)
	}
}

// fake is a backend in memory: tests say which directories are missing and
// send its events.
type fake struct {
	mu      sync.Mutex
	next    int
	dirs    map[int]string
	missing map[string]bool
	ch      chan event
	once    sync.Once
}

func newFake() *fake {
	return &fake{dirs: map[int]string{}, missing: map[string]bool{}, ch: make(chan event)}
}

func (f *fake) add(dir string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.missing[dir] {
		return 0, fmt.Errorf("fake: %s: %w", dir, fs.ErrNotExist)
	}
	f.next++
	f.dirs[f.next] = dir
	return f.next, nil
}

func (f *fake) remove(id int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.dirs, id)
}

func (f *fake) events() <-chan event { return f.ch }

func (f *fake) close() error {
	f.once.Do(func() { close(f.ch) })
	return nil
}

// watching returns the directories f watches, sorted.
func (f *fake) watching() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Sorted(maps.Values(f.dirs))
}

// id returns the id of the watch of dir, -1 for none.
func (f *fake) id(dir string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, d := range f.dirs {
		if d == dir {
			return id
		}
	}
	return -1
}

// gone returns the event of dir gone, whose watch it ends, as a backend does.
func (f *fake) gone(dir string) event {
	id := f.id(dir)
	f.remove(id)
	return event{id: id}
}

// newFakeWatcher returns a watcher of at most limit directories on a fake
// backend, closed when the test ends, whose clock moves a second at each use.
func newFakeWatcher(t *testing.T, limit int) (*Watcher, *fake) {
	t.Helper()
	f := newFake()
	w := newWatcher(f, limit)
	clock := time.Unix(1000, 0)
	w.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	t.Cleanup(func() { w.Close() })
	return w, f
}

// at returns the time of second s, before the fake clock's first use.
func at(s int) time.Time { return time.Unix(int64(s), 0) }

func check(t *testing.T, f *fake, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := f.watching(); !slices.Equal(got, want) {
		t.Errorf("watching %v, want %v", got, want)
	}
}

func TestLimitWatchesTheNewest(t *testing.T) {
	w, f := newFakeWatcher(t, 2)
	if err := w.SetDirs(map[string]time.Time{"/a": at(1), "/b": at(2), "/c": at(3)}); err != nil {
		t.Fatal(err)
	}
	check(t, f, "/b", "/c")
	if w.Watched("/a/x") || !w.Watched("/c/x") {
		t.Errorf("Watched /a/x %v, /c/x %v, want false, true", w.Watched("/a/x"), w.Watched("/c/x"))
	}
	// Touched, a comes in and the oldest left, b, goes.
	w.Touch([]string{"/a/x"})
	check(t, f, "/a", "/c")
	if !w.Watched("/a/x") {
		t.Error("/a/x not watched after Touch")
	}
	w.Touch([]string{"/b/x"})
	check(t, f, "/a", "/b")
	// A directory not wanted is not brought in.
	w.Touch([]string{"/other/x"})
	check(t, f, "/a", "/b")
}

// A change seen counts as use: the directory it is in stays when another
// one is touched.
func TestEventCountsAsUse(t *testing.T) {
	w, f := newFakeWatcher(t, 2)
	w.SetDirs(map[string]time.Time{"/a": at(1), "/b": at(2), "/c": at(3)})
	check(t, f, "/b", "/c")
	f.ch <- event{id: f.id("/b"), name: "x"}
	if got := <-w.Changes(); !slices.Equal(got, []string{"/b/x"}) {
		t.Errorf("reported %v, want [/b/x]", got)
	}
	w.Touch([]string{"/a/x"})
	check(t, f, "/a", "/b")
}

// Pinned directories are watched always, and outside the limit.
func TestPinnedStay(t *testing.T) {
	w, f := newFakeWatcher(t, 1)
	w.Pin([]string{"/git/HEAD", "/git/index"})
	w.SetDirs(map[string]time.Time{"/a": at(1), "/b": at(2)})
	check(t, f, "/b", "/git")
	for _, p := range []string{"/a/x", "/b/x", "/a/x"} {
		w.Touch([]string{p})
		if !slices.Contains(f.watching(), "/git") {
			t.Fatalf("after Touch %s the pinned directory is not watched: %v", p, f.watching())
		}
	}
	check(t, f, "/a", "/git")
	// Pinned files are reported by their names only.
	f.ch <- event{id: f.id("/git"), name: "objects"}
	f.ch <- event{id: f.id("/git"), name: "HEAD"}
	if got := <-w.Changes(); !slices.Equal(got, []string{"/git/HEAD"}) {
		t.Errorf("reported %v, want [/git/HEAD]", got)
	}
}

func TestSetLimit(t *testing.T) {
	w, f := newFakeWatcher(t, 1)
	w.Pin([]string{"/git/HEAD"})
	w.SetDirs(map[string]time.Time{"/a": at(1), "/b": at(2)})
	check(t, f, "/b", "/git")
	w.SetLimit(0)
	check(t, f, "/git")
	if !w.Watched("/git/HEAD") || w.Watched("/b/x") {
		t.Errorf("limit 0: pinned watched %v, other %v; want true, false", w.Watched("/git/HEAD"), w.Watched("/b/x"))
	}
	w.SetLimit(3)
	check(t, f, "/a", "/b", "/git")
	w.SetLimit(1)
	check(t, f, "/b", "/git")
	w.Pin(nil)
	check(t, f, "/b")
}

// A missing directory takes no place: the next one is watched instead.
func TestMissingTakesNoPlace(t *testing.T) {
	w, f := newFakeWatcher(t, 2)
	f.missing["/c"] = true
	if err := w.SetDirs(map[string]time.Time{"/a": at(1), "/b": at(2), "/c": at(3)}); err != nil {
		t.Errorf("a missing directory is an error: %v", err)
	}
	check(t, f, "/a", "/b")
}

func TestSetReplaces(t *testing.T) {
	w, f := newFakeWatcher(t, 10)
	w.SetFiles([]string{"/f/x", "/g/y"})
	w.SetDirs(map[string]time.Time{"/d": at(1)})
	check(t, f, "/d", "/f", "/g")
	w.SetFiles([]string{"/g/y"})
	check(t, f, "/d", "/g")
	w.SetDirs(nil)
	check(t, f, "/g")
	// A directory both sets want stays while one does.
	w.SetDirs(map[string]time.Time{"/g": at(1)})
	w.SetFiles(nil)
	check(t, f, "/g")
}

func TestReports(t *testing.T) {
	w, f := newFakeWatcher(t, 10)
	w.SetFiles([]string{"/f/x"})
	w.SetDirs(map[string]time.Time{"/d": at(1), "/f": at(1)})
	// Of a set file, its path; of a directory set, any entry, once.
	for _, c := range []struct {
		dir, name string
		want      []string
	}{
		{"/d", "new", []string{"/d/new"}},
		{"/f", "x", []string{"/f/x"}},
		{"/f", "other", []string{"/f/other"}},
	} {
		f.ch <- event{id: f.id(c.dir), name: c.name}
		if got := <-w.Changes(); !slices.Equal(got, c.want) {
			t.Errorf("%s/%s: reported %v, want %v", c.dir, c.name, got, c.want)
		}
	}
	w.SetDirs(map[string]time.Time{"/d": at(1)})
	f.ch <- event{id: f.id("/f"), name: "other"}
	f.ch <- event{id: f.id("/f"), name: "x"}
	if got := <-w.Changes(); !slices.Equal(got, []string{"/f/x"}) {
		t.Errorf("after SetDirs without /f: reported %v, want [/f/x]", got)
	}
	f.ch <- event{overflow: true}
	if got := <-w.Changes(); !slices.Equal(got, []string{"/d", "/f/x"}) {
		t.Errorf("overflow: reported %v, want all, [/d /f/x]", got)
	}
}

// A directory gone reports its paths. One at its path again is watched; a
// missing one is not, and its place goes to the next.
func TestDirectoryGone(t *testing.T) {
	w, f := newFakeWatcher(t, 2)
	w.SetFiles([]string{"/f/x"})
	w.SetDirs(map[string]time.Time{"/d": at(3), "/e": at(1)})
	w.Touch([]string{"/f/x"})
	check(t, f, "/d", "/f")
	f.ch <- f.gone("/f")
	if got := <-w.Changes(); !slices.Equal(got, []string{"/f/x"}) {
		t.Errorf("replaced: reported %v, want [/f/x]", got)
	}
	check(t, f, "/d", "/f")

	f.missing["/d"] = true
	f.ch <- f.gone("/d")
	if got := <-w.Changes(); !slices.Equal(got, []string{"/d"}) {
		t.Errorf("removed: reported %v, want [/d]", got)
	}
	check(t, f, "/e", "/f")
	if w.Watched("/d/x") {
		t.Error("/d/x watched while /d is missing")
	}
}

func TestWatchedFollowsSymlinks(t *testing.T) {
	// git difftool --dir-diff links the working tree files into a
	// temporary directory: both directories must be watched.
	tree, tmp := t.TempDir(), t.TempDir()
	target, link := filepath.Join(tree, "a.txt"), filepath.Join(tmp, "a.txt")
	write(t, target, "a")
	symlink(t, target, link)
	w, f := newFakeWatcher(t, 1)
	w.SetFiles([]string{link})
	if w.Watched(link) {
		t.Errorf("watched with one of two directories: %v", f.watching())
	}
	w.SetLimit(2)
	if !w.Watched(link) {
		t.Errorf("not watched with both: %v", f.watching())
	}
}
