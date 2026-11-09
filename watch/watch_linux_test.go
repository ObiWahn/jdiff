package watch

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// start returns a watcher of at most limit directories watching the files
// at paths, closed when the test ends.
func start(t *testing.T, paths []string, limit int) *Watcher {
	t.Helper()
	w, err := New(limit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	if err := w.SetFiles(paths); err != nil {
		t.Fatal(err)
	}
	return w
}

// next returns the next batch of w, failing after a second without one.
func next(t *testing.T, w *Watcher) []string {
	t.Helper()
	select {
	case paths := <-w.Changes():
		return paths
	case <-time.After(time.Second):
		t.Fatal("no change reported")
		return nil
	}
}

// quiet fails when w reports a change within a moment.
func quiet(t *testing.T, w *Watcher) {
	t.Helper()
	select {
	case paths := <-w.Changes():
		t.Errorf("reported %v, want nothing", paths)
	case <-time.After(4 * settle):
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestWatcherReportsWriteRenameAndDelete(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	write(t, a, "a")
	write(t, b, "b")
	w := start(t, []string{a, b}, 20)

	write(t, a, "a2")
	if got := next(t, w); !slices.Equal(got, []string{a}) {
		t.Errorf("write: %v, want %v", got, []string{a})
	}
	// Saved as editors often do: a new file renamed over the old one.
	tmp := filepath.Join(dir, ".b.txt.swp")
	write(t, tmp, "b2")
	if err := os.Rename(tmp, b); err != nil {
		t.Fatal(err)
	}
	if got := next(t, w); !slices.Equal(got, []string{b}) {
		t.Errorf("rename over: %v, want %v", got, []string{b})
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if got := next(t, w); !slices.Equal(got, []string{a}) {
		t.Errorf("delete: %v, want %v", got, []string{a})
	}
	// Recreated after the delete: still watched, by its directory.
	write(t, a, "a3")
	if got := next(t, w); !slices.Equal(got, []string{a}) {
		t.Errorf("recreate: %v, want %v", got, []string{a})
	}
}

func TestWatcherBatchesAndFilters(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	write(t, a, "a")
	write(t, b, "b")
	w := start(t, []string{a, b}, 20)

	// Other files of the directory and its subdirectories are not reported.
	write(t, filepath.Join(dir, "other.txt"), "x")
	mkdir(t, filepath.Join(dir, "sub"))
	write(t, filepath.Join(dir, "sub", "a.txt"), "x")
	quiet(t, w)

	// Changes right after each other come as one batch.
	write(t, a, "a2")
	write(t, b, "b2")
	write(t, a, "a3")
	if got := next(t, w); !slices.Equal(got, []string{a, b}) {
		t.Errorf("burst: %v, want one batch %v", got, []string{a, b})
	}
	quiet(t, w)
}

// A directory set reports any entry: new, renamed, removed; not those of its
// subdirectories.
func TestWatcherDirs(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, filepath.Join(dir, "sub"))
	w, err := New(20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	if err := w.SetDirs(map[string]time.Time{dir: {}}); err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	write(t, a, "a")
	if got := next(t, w); !slices.Equal(got, []string{a}) {
		t.Errorf("new: %v, want %v", got, []string{a})
	}
	if err := os.Rename(a, b); err != nil {
		t.Fatal(err)
	}
	if got := next(t, w); !slices.Equal(got, []string{a, b}) {
		t.Errorf("rename: %v, want %v", got, []string{a, b})
	}
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	if got := next(t, w); !slices.Equal(got, []string{b}) {
		t.Errorf("remove: %v, want %v", got, []string{b})
	}
	write(t, filepath.Join(dir, "sub", "c"), "c")
	quiet(t, w)
}

func TestWatcherFollowsSymlinks(t *testing.T) {
	// git difftool --dir-diff links the working tree files into a
	// temporary directory.
	tree, tmp := t.TempDir(), t.TempDir()
	target, link := filepath.Join(tree, "a.txt"), filepath.Join(tmp, "a.txt")
	write(t, target, "a")
	symlink(t, target, link)
	w := start(t, []string{link}, 20)
	write(t, target, "a2")
	if got := next(t, w); !slices.Equal(got, []string{link}) {
		t.Errorf("target written: %v, want the link %v", got, []string{link})
	}
}

// Beyond the limit, the directories of the files modified last are watched;
// Touch brings another one in, and changes there are reported.
func TestWatcherLimit(t *testing.T) {
	var paths []string
	for i := range 3 {
		p := filepath.Join(t.TempDir(), "a.txt")
		write(t, p, "a")
		at := time.Unix(int64(1000+i), 0)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	w := start(t, paths, 2)
	if w.Watched(paths[0]) || !w.Watched(paths[1]) || !w.Watched(paths[2]) {
		t.Fatalf("watched %v %v %v, want false true true", w.Watched(paths[0]), w.Watched(paths[1]), w.Watched(paths[2]))
	}
	write(t, paths[0], "a2")
	quiet(t, w)
	w.Touch(paths[:1])
	if !w.Watched(paths[0]) || w.Watched(paths[1]) {
		t.Errorf("after Touch watched %v %v, want true false", w.Watched(paths[0]), w.Watched(paths[1]))
	}
	write(t, paths[0], "a3")
	if got := next(t, w); !slices.Equal(got, paths[:1]) {
		t.Errorf("reported %v, want %v", got, paths[:1])
	}
	// A missing directory is left out.
	w.SetFiles([]string{paths[0], "/no/such/dir/c"})
	if w.Watched("/no/such/dir/c") {
		t.Error("missing directory watched")
	}
}

func TestWatcherClose(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	write(t, a, "a")
	w, err := New(1)
	if err != nil {
		t.Fatal(err)
	}
	w.SetFiles([]string{a})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-w.Changes():
		if ok {
			t.Error("a change reported after Close")
		}
	case <-time.After(time.Second):
		t.Error("Changes not closed after Close")
	}
	if w.Close() == nil {
		t.Error("second Close succeeded")
	}
}

// A watched directory that is removed or moved away, and not there again,
// cannot be watched any more: its files count as changed, and are no longer
// watched, which Watched tells; the others still are.
func TestWatcherDirectoryGone(t *testing.T) {
	for _, gone := range []struct {
		name string
		do   func(dir string) error
	}{
		{"removed", os.RemoveAll},
		{"moved", func(dir string) error { return os.Rename(dir, dir+".old") }},
	} {
		t.Run(gone.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "d")
			mkdir(t, dir)
			a, b := filepath.Join(dir, "a.txt"), filepath.Join(t.TempDir(), "b.txt")
			write(t, a, "a")
			write(t, b, "b")
			w := start(t, []string{a, b}, 20)
			if err := gone.do(dir); err != nil {
				t.Fatal(err)
			}
			if got := next(t, w); !slices.Contains(got, a) {
				t.Errorf("reported %v, want %s", got, a)
			}
			if w.Watched(a) || !w.Watched(b) {
				t.Errorf("watched %v %v, want false true", w.Watched(a), w.Watched(b))
			}
			write(t, b, "b2")
			if got := next(t, w); !slices.Equal(got, []string{b}) {
				t.Errorf("reported %v, want %s", got, b)
			}
		})
	}
}

// A watched directory replaced by another one at its path, as a checkout
// may, is watched again: changes in the new one are reported.
func TestWatcherDirectoryReplaced(t *testing.T) {
	root := t.TempDir()
	dir, other := filepath.Join(root, "d"), filepath.Join(root, "other")
	for _, d := range []string{dir, other} {
		mkdir(t, d)
		write(t, filepath.Join(d, "a.txt"), "a")
	}
	a := filepath.Join(dir, "a.txt")
	w := start(t, []string{a}, 20)
	// Swapped at once, so that the path is never missing.
	if err := unix.Renameat2(unix.AT_FDCWD, dir, unix.AT_FDCWD, other, unix.RENAME_EXCHANGE); err != nil {
		t.Skipf("no RENAME_EXCHANGE here: %v", err)
	}
	if got := next(t, w); !slices.Equal(got, []string{a}) {
		t.Fatalf("swap: reported %v, want %s", got, a)
	}
	write(t, a, "a2")
	if got := next(t, w); !slices.Equal(got, []string{a}) {
		t.Errorf("write in the new directory: reported %v, want %s", got, a)
	}
	write(t, filepath.Join(other, "a.txt"), "a3")
	quiet(t, w)
}
