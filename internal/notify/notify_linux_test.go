package notify

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// start returns a watcher of paths, closed when the test ends.
func start(t *testing.T, paths []string, maxDirs int) *Watcher {
	t.Helper()
	w, err := New(paths, maxDirs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
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
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
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

func TestWatcherFollowsSymlinks(t *testing.T) {
	// git difftool --dir-diff links the working tree files into a
	// temporary directory.
	tree, tmp := t.TempDir(), t.TempDir()
	target, link := filepath.Join(tree, "a.txt"), filepath.Join(tmp, "a.txt")
	write(t, target, "a")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	w := start(t, []string{link}, 20)
	write(t, target, "a2")
	if got := next(t, w); !slices.Equal(got, []string{link}) {
		t.Errorf("target written: %v, want the link %v", got, []string{link})
	}
}

func TestWatcherLimitsDirs(t *testing.T) {
	var paths []string
	for range 3 {
		paths = append(paths, filepath.Join(t.TempDir(), "a.txt"))
	}
	if _, err := New(paths, 2); !errors.Is(err, ErrTooManyDirs) {
		t.Errorf("3 directories, at most 2: err %v, want ErrTooManyDirs", err)
	}
	// Files of one directory take one; a missing directory is left out.
	dir := t.TempDir()
	start(t, []string{filepath.Join(dir, "a"), filepath.Join(dir, "b"), "/no/such/dir/c"}, 2)
}

func TestWatcherClose(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	write(t, a, "a")
	w, err := New([]string{a}, 1)
	if err != nil {
		t.Fatal(err)
	}
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
