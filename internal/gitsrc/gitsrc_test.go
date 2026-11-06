package gitsrc

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"jdiff/internal/source"
)

// newRepo creates a repository with this history, oldest first:
//
//	root:   add a.txt (2 lines) and b.txt
//	second: change a.txt, rename b.txt to c.txt, add bin.dat
//	merge:  merge a branch that added d.txt
func newRepo(t *testing.T) Repo {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// The path git reports: without symlinks, as /var on macOS, and on
	// Windows with long names, not 8.3 short ones.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	write("a.txt", "one\ntwo\n")
	write("b.txt", "keep\nthese\nlines\nfor\nrename\n")
	run("add", ".")
	run("commit", "-qm", "root")
	write("a.txt", "one\n2\nthree\n")
	run("mv", "b.txt", "c.txt")
	write("bin.dat", "\x00\x01")
	run("add", ".")
	run("commit", "-qm", "second", "-m", "first body line\nsecond body line")
	run("checkout", "-qb", "side", "HEAD~1")
	write("d.txt", "side\n")
	run("add", ".")
	run("commit", "-qm", "side")
	run("checkout", "-q", "main")
	run("merge", "-q", "--no-ff", "-m", "merge side", "side")
	return Repo{Dir: dir}
}

func TestLog(t *testing.T) {
	r := newRepo(t)
	commits, err := r.Log([]string{"--first-parent"})
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, c := range commits {
		subjects = append(subjects, c.Subject)
	}
	if want := []string{"merge side", "second", "root"}; !slices.Equal(subjects, want) {
		t.Fatalf("subjects = %v, want %v", subjects, want)
	}
	merge, second, root := commits[0], commits[1], commits[2]
	if root.Parent != "" || second.Parent != root.Hash || merge.Parent != second.Hash {
		t.Errorf("parents: root %q second %q merge %q", root.Parent, second.Parent, merge.Parent)
	}
	if root.Added != 0 || second.Added != 0 {
		t.Errorf("Log counted lines: root +%d, second +%d; Counts does", root.Added, second.Added)
	}
	if second.Body != "first body line\nsecond body line" || root.Body != "" {
		t.Errorf("bodies: second %q root %q", second.Body, root.Body)
	}
	if root.Short == "" || root.Author != "T" || root.Date == "" {
		t.Errorf("root = %+v", root)
	}
}

func TestReversed(t *testing.T) {
	r := newRepo(t)
	for _, args := range [][]string{
		{"--first-parent"},
		{"--first-parent", "--reverse"},
		{"--reverse", "--first-parent", "--reverse"},
		{"--reverse", "--first-parent", "--", "a.txt"},
	} {
		commits, err := r.Log(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		oldestFirst := commits[0].Subject == "root"
		if got := Reversed(args); got != oldestFirst {
			t.Errorf("Reversed(%v) = %v, git log lists %q first", args, got, commits[0].Subject)
		}
	}
	// After --, --reverse is a path.
	if Reversed([]string{"--", "--reverse"}) {
		t.Error("Reversed counts --reverse after --")
	}
}

func TestCounts(t *testing.T) {
	r := newRepo(t)
	commits, err := r.Log([]string{"--first-parent"})
	if err != nil {
		t.Fatal(err)
	}
	merge, second, root := commits[0], commits[1], commits[2]
	// Asked in another order than the log's: each gets its own counts.
	counts, err := r.Counts([]string{root.Hash, merge.Hash, second.Hash})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, hash     string
		added, removed int
		why            string
	}{
		{"root", root.Hash, 7, 0, "against the empty tree"},
		{"second", second.Hash, 2, 1, "binary not counted"},
		{"merge", merge.Hash, 1, 0, "against the first parent"},
	} {
		got, ok := counts[c.hash]
		if !ok || got.Added != c.added || got.Removed != c.removed {
			t.Errorf("%s: %+v (found %v), want +%d -%d (%s)", c.name, got, ok, c.added, c.removed, c.why)
		}
	}
	if len(counts) != 3 {
		t.Errorf("%d counts for 3 commits: %v", len(counts), counts)
	}
	if counts, err := r.Counts(nil); err != nil || len(counts) != 0 {
		t.Errorf("no commits: %v, %v", counts, err)
	}
	if _, err := r.Counts([]string{"no-such-commit"}); err == nil {
		t.Error("Counts of an unknown commit succeeded")
	}
}

// gitIn runs git with args in dir, failing the test on an error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDiff(t *testing.T) {
	r := newRepo(t)
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(r.Dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	byPath := func(files []source.File) map[string]source.File {
		out := map[string]source.File{}
		for _, f := range files {
			out[f.Path] = f
		}
		return out
	}
	diff := func(args ...string) map[string]source.File {
		t.Helper()
		files, err := r.Diff(args)
		if err != nil {
			t.Fatal(err)
		}
		return byPath(files)
	}

	// Nothing changed: no files, no error.
	if files := diff(); len(files) != 0 {
		t.Errorf("clean tree: %v", files)
	}
	// Staged: a.txt changed and c.txt renamed to e.txt; then a.txt changed
	// once more in the working tree, and d.txt removed from it.
	write("a.txt", "one\nstaged\n")
	gitIn(t, r.Dir, "add", "a.txt")
	gitIn(t, r.Dir, "mv", "c.txt", "e.txt")
	write("a.txt", "one\nstaged\nworking\n")
	if err := os.Remove(filepath.Join(r.Dir, "d.txt")); err != nil {
		t.Fatal(err)
	}

	// Working tree against the index.
	files := diff()
	a := files["a.txt"]
	if !slices.Equal(a.Old, []string{"one", "staged"}) || !slices.Equal(a.New, []string{"one", "staged", "working"}) {
		t.Errorf("unstaged a.txt = %+v", a)
	}
	if a.OldFile != "" || a.NewFile != filepath.Join(r.Dir, "a.txt") || a.NewStamp.Size == 0 {
		t.Errorf("unstaged a.txt: old file %q, new file %q, stamp %+v; want the working copy, watched", a.OldFile, a.NewFile, a.NewStamp)
	}
	if d := files["d.txt"]; !d.InOld || d.InNew || !slices.Equal(d.Old, []string{"side"}) {
		t.Errorf("deleted d.txt = %+v", d)
	}
	if _, ok := files["e.txt"]; ok || len(files) != 2 {
		t.Errorf("unstaged files %v, want a.txt and d.txt only", files)
	}

	// The index against HEAD.
	files = diff("--cached")
	if a := files["a.txt"]; !slices.Equal(a.Old, []string{"one", "2", "three"}) || !slices.Equal(a.New, []string{"one", "staged"}) ||
		a.NewFile != "" {
		t.Errorf("staged a.txt = %+v", a)
	}
	if e := files["e.txt"]; e.OldPath != "c.txt" || !slices.Equal(e.Old, e.New) || len(e.New) != 5 {
		t.Errorf("staged e.txt = %+v, want the rename from c.txt", e)
	}

	// The working tree against HEAD, a path, and reversed.
	if a := diff("HEAD")["a.txt"]; !slices.Equal(a.Old, []string{"one", "2", "three"}) || len(a.New) != 3 || a.NewFile == "" {
		t.Errorf("a.txt against HEAD = %+v", a)
	}
	if files := diff("--", "a.txt"); len(files) != 1 {
		t.Errorf("path a.txt: %v", files)
	}
	if a := diff("-R")["a.txt"]; a.OldFile == "" || a.NewFile != "" || len(a.Old) != 3 {
		t.Errorf("reversed a.txt = %+v, want the working copy as the old side", a)
	}

	if _, err := r.Diff([]string{"no-such-rev"}); err == nil {
		t.Error("Diff with a bad revision succeeded")
	}
}

func TestFiles(t *testing.T) {
	r := newRepo(t)
	commits, err := r.Log([]string{"--first-parent"})
	if err != nil {
		t.Fatal(err)
	}
	merge, second, root := commits[0], commits[1], commits[2]

	files, err := r.Files(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "a.txt" || files[0].InOld || !slices.Equal(files[0].New, []string{"one", "two"}) {
		t.Errorf("root files = %+v", files)
	}

	files, err = r.Files(second)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]int{}
	for i, f := range files {
		byPath[f.Path] = i
	}
	a := files[byPath["a.txt"]]
	if !slices.Equal(a.Old, []string{"one", "two"}) || !slices.Equal(a.New, []string{"one", "2", "three"}) {
		t.Errorf("a.txt = %+v", a)
	}
	c := files[byPath["c.txt"]]
	if c.OldPath != "b.txt" || !c.InOld || !c.InNew || !slices.Equal(c.Old, c.New) || len(c.New) != 5 {
		t.Errorf("c.txt = %+v, want rename from b.txt with content", c)
	}
	if bin := files[byPath["bin.dat"]]; !bin.Binary || bin.InOld {
		t.Errorf("bin.dat = %+v", bin)
	}

	files, err = r.Files(merge)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "d.txt" || !slices.Equal(files[0].New, []string{"side"}) {
		t.Errorf("merge files = %+v, want d.txt against the first parent", files)
	}
}

func TestBranch(t *testing.T) {
	r := newRepo(t)
	if b := r.Branch(); b != "main" {
		t.Errorf("Branch() = %q, want main", b)
	}
	cmd := exec.Command("git", "checkout", "-q", "--detach")
	cmd.Dir = r.Dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout --detach: %v\n%s", err, out)
	}
	if b := r.Branch(); b != "detached HEAD" {
		t.Errorf("Branch() detached = %q", b)
	}
}

func TestLogErrors(t *testing.T) {
	r := newRepo(t)
	if _, err := r.Log([]string{"no-such-rev"}); err == nil {
		t.Error("Log with a bad revision succeeded")
	}
}

// TestCatFilesErrorStopsGit checks that a bad reply from git cat-file is
// reported even while git keeps writing output that is no longer read.
func TestCatFilesErrorStopsGit(t *testing.T) {
	// A fake git that answers with a bad header and then writes on and on.
	// It is a Go program, so that it runs as git also on Windows, where a
	// script would not.
	dir := t.TempDir()
	fake := `package main

import "os"

func main() {
	os.Stdout.WriteString("x blob notasize\n")
	for {
		if _, err := os.Stdout.WriteString("y\n"); err != nil {
			return
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(fake), 0o644); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "git"+exeSuffix()), "main.go")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the fake git: %v\n%s", err, out)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	done := make(chan error, 1)
	go func() {
		_, err := Repo{}.catFiles([]string{"HEAD:a.txt"})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("want an error for the bad header")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("catFiles hangs after a bad header")
	}
}

func TestTopLevel(t *testing.T) {
	r := newRepo(t)
	if err := os.MkdirAll(filepath.Join(r.Dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if top, err := (Repo{Dir: filepath.Join(r.Dir, "sub")}).TopLevel(); err != nil || top != r.Dir {
		t.Errorf("TopLevel from sub = %q, %v; want %q", top, err, r.Dir)
	}
	if _, err := (Repo{Dir: t.TempDir()}).TopLevel(); err == nil {
		t.Error("TopLevel outside a repository succeeded")
	}
}

// exeSuffix returns the suffix of executables: .exe on Windows.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// A newline in a path must not split the blob requests to git cat-file:
// "rev:a\nHEAD:b.txt" would read as two, the second b.txt's content.
func TestFilesNewlineInPath(t *testing.T) {
	r := newRepo(t)
	if err := os.WriteFile(filepath.Join(r.Dir, "a\nHEAD:b.txt"), []byte("own\n"), 0o644); err != nil {
		t.Skipf("file name with a newline: %v", err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "newline"}} {
		cmd := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = r.Dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	commits, err := r.Log([]string{"-1"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := r.Files(commits[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "a\nHEAD:b.txt" || files[0].InOld || !slices.Equal(files[0].New, []string{"own"}) {
		t.Errorf("files = %+v, want the new file with its own content", files)
	}
}

func TestDiffUntracked(t *testing.T) {
	r := newRepo(t)
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(r.Dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	paths := func(files []source.File) []string {
		var out []string
		for _, f := range files {
			out = append(out, f.Path)
		}
		slices.Sort(out)
		return out
	}
	diff := func(args ...string) []source.File {
		t.Helper()
		files, err := r.DiffUntracked(args)
		if err != nil {
			t.Fatalf("DiffUntracked %v: %v", args, err)
		}
		return files
	}

	// Nothing untracked: as Diff, and no index made.
	if files := diff(); len(files) != 0 {
		t.Errorf("clean tree: %v", paths(files))
	}

	write("a.txt", "changed\n")
	write("x0", "new 0\n")
	write("sub/x1", "new 1\n")
	write(".gitignore", "*.log\n")
	write("i.log", "ignored\n")
	want := []string{".gitignore", "a.txt", "sub/x1", "x0"}
	// A pathspec, if taken for one, would also add the broken link below.
	// Windows forbids * in file names.
	if runtime.GOOS != "windows" {
		write("broken*", "new 2\n")
		want = []string{".gitignore", "a.txt", "broken*", "sub/x1", "x0"}
	}
	// Left out: they cannot be shown, and must not fail the others.
	if err := os.Symlink("nowhere", filepath.Join(r.Dir, "broken")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(r.Dir, "nest"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, filepath.Join(r.Dir, "nest"), "init", "-q") // no commit: git add fails on it
	if err := os.Mkdir(filepath.Join(r.Dir, "nest1"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, filepath.Join(r.Dir, "nest1"), "init", "-q")
	write("nest1/n", "n\n")
	gitIn(t, filepath.Join(r.Dir, "nest1"), "add", ".")
	gitIn(t, filepath.Join(r.Dir, "nest1"), "commit", "-qm", "n")

	index := filepath.Join(r.Dir, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{nil, {"HEAD"}} {
		files := diff(args...)
		if got := paths(files); !slices.Equal(got, want) {
			t.Errorf("%v: files %v, want %v", args, got, want)
		}
		for _, f := range files {
			switch f.Path {
			case "a.txt":
				if !f.InOld || !slices.Equal(f.New, []string{"changed"}) {
					t.Errorf("%v: a.txt = %+v", args, f)
				}
			case "sub/x1":
				if f.InOld || !f.InNew || !slices.Equal(f.New, []string{"new 1"}) ||
					f.NewFile != filepath.Join(r.Dir, "sub", "x1") || f.NewStamp.Size == 0 {
					t.Errorf("%v: sub/x1 = %+v, want added and watched", args, f)
				}
			}
		}
	}

	// Without the working tree on a side, or the index, no new files.
	for _, args := range [][]string{{"--cached"}, {"HEAD~1", "HEAD"}} {
		wantFiles, err := r.Diff(args)
		if err != nil {
			t.Fatal(err)
		}
		if got := paths(diff(args...)); !slices.Equal(got, paths(wantFiles)) {
			t.Errorf("%v: files %v, want as without: %v", args, got, paths(wantFiles))
		}
	}

	// Paths limit them, and reversed they are the old side.
	if got := paths(diff("HEAD", "--", "sub")); !slices.Equal(got, []string{"sub/x1"}) {
		t.Errorf("path sub: %v", got)
	}
	for _, f := range diff("-R") {
		if f.Path == "x0" && (f.InNew || f.OldFile == "" || !slices.Equal(f.Old, []string{"new 0"})) {
			t.Errorf("reversed x0 = %+v, want the working copy as the old side", f)
		}
	}

	// From a subdirectory all files are found, with paths from the top.
	sub := Repo{Dir: filepath.Join(r.Dir, "sub")}
	files, err := sub.DiffUntracked([]string{"HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(files); !slices.Equal(got, want) {
		t.Errorf("from sub: %v, want %v", got, want)
	}

	// The real index stays, the files are still untracked.
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the index changed")
	}
	out, err := r.git("status", "--porcelain", "--", "x0")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "?? x0\n" {
		t.Errorf("status of x0 = %q", out)
	}
	if plain, err := r.Diff([]string{"HEAD"}); err != nil || !slices.Equal(paths(plain), []string{"a.txt"}) {
		t.Errorf("Diff still: %v, %v", paths(plain), err)
	}
}

// A repository without commits has no index to copy.
func TestDiffUntrackedNoCommits(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q")
	r := Repo{Dir: dir}
	if files, err := r.DiffUntracked(nil); err != nil || len(files) != 0 {
		t.Errorf("no files: %v, %v", files, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x0"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := r.DiffUntracked(nil)
	if err != nil || len(files) != 1 || files[0].Path != "x0" || files[0].InOld {
		t.Errorf("x0: %+v, %v", files, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "index")); err == nil {
		t.Error("an index appeared in the repository")
	}
}

func TestFilesDeletedFile(t *testing.T) {
	r := newRepo(t)
	gitIn(t, r.Dir, "rm", "-q", "a.txt")
	gitIn(t, r.Dir, "commit", "-qm", "delete")
	commits, err := r.Log([]string{"-1"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := r.Files(commits[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" || !files[0].InOld || files[0].InNew ||
		!slices.Equal(files[0].Old, []string{"one", "2", "three"}) || files[0].New != nil {
		t.Errorf("files = %+v, want a.txt deleted, with its lines", files)
	}
}

// A submodule has no blob to read: it is listed like a binary file.
func TestFilesSubmodule(t *testing.T) {
	r := newRepo(t)
	const commit = "1111111111111111111111111111111111111111"
	gitIn(t, r.Dir, "update-index", "--add", "--cacheinfo", "160000,"+commit+",sub")
	gitIn(t, r.Dir, "commit", "-qm", "add a submodule")
	commits, err := r.Log([]string{"-1"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := r.Files(commits[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "sub" || !files[0].Binary || files[0].InOld || !files[0].InNew {
		t.Errorf("files = %+v, want sub, added, as binary", files)
	}
}

func TestDiffBinaryInWorkingTree(t *testing.T) {
	r := newRepo(t)
	path := filepath.Join(r.Dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\x00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := r.Diff(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" || !files[0].Binary || files[0].NewFile != path {
		t.Errorf("files = %+v, want a.txt as binary, its working copy watched", files)
	}
}

// A file with conflicts is listed once, with what the working tree holds, and
// not again for its entry without contents, of status U.
func TestDiffUnmerged(t *testing.T) {
	r := newRepo(t)
	path := filepath.Join(r.Dir, "a.txt")
	gitIn(t, r.Dir, "checkout", "-qb", "other", "HEAD~1")
	if err := os.WriteFile(path, []byte("one\nother\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, r.Dir, "commit", "-qam", "other")
	gitIn(t, r.Dir, "checkout", "-q", "main")
	if err := os.WriteFile(path, []byte("one\nmain\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, r.Dir, "commit", "-qam", "main")
	cmd := exec.Command("git", "-c", "user.name=T", "-c", "user.email=t@example.com", "merge", "other")
	cmd.Dir = r.Dir
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the merge has no conflict:\n%s", out)
	}
	files, err := r.Diff(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" || files[0].NewFile != path || !slices.Contains(files[0].New, "=======") {
		t.Errorf("files = %+v, want a.txt once, with the conflict markers", files)
	}
}

func TestWatchPaths(t *testing.T) {
	r := newRepo(t)
	git := filepath.Join(r.Dir, ".git")
	want := []string{filepath.Join(git, "HEAD"), filepath.Join(git, "index"), filepath.Join(git, "packed-refs"),
		filepath.Join(git, "refs", "heads", "main")}
	got, err := r.WatchPaths()
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("WatchPaths() = %q, %v; want %q", got, err, want)
	}
	// Detached, there is no ref to watch.
	gitIn(t, r.Dir, "checkout", "-q", "--detach")
	if got, err = r.WatchPaths(); err != nil || !slices.Equal(got, want[:3]) {
		t.Errorf("detached: %q, %v; want %q", got, err, want[:3])
	}
	gitIn(t, r.Dir, "checkout", "-q", "main")
	// A worktree has its own HEAD and index, and the refs of the repository.
	dir := filepath.Join(t.TempDir(), "wt")
	gitIn(t, r.Dir, "worktree", "add", "-q", "-b", "wt", dir)
	got, err = Repo{Dir: dir}.WatchPaths()
	if err != nil || len(got) != 4 {
		t.Fatalf("worktree: %q, %v", got, err)
	}
	if !slices.Contains(got, filepath.Join(git, "refs", "heads", "wt")) || !slices.Contains(got, filepath.Join(git, "worktrees", "wt", "HEAD")) {
		t.Errorf("worktree: %q", got)
	}
	if _, err := (Repo{Dir: t.TempDir()}).WatchPaths(); err == nil {
		t.Error("outside a repository: no error")
	}
}

// Status differs after a new file, also in a new directory, an edit, git add
// and a commit, and does not write the index.
func TestStatus(t *testing.T) {
	r := newRepo(t)
	seen := map[string]bool{}
	check := func(what string) {
		t.Helper()
		s, err := r.Status()
		if err != nil {
			t.Fatal(err)
		}
		if seen[s] {
			t.Errorf("%s: status seen before:\n%s", what, s)
		}
		seen[s] = true
	}
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(r.Dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	check("start")
	write("new/x0.txt", "x0\n")
	check("new file")
	write("new/x1.txt", "x1\n")
	check("another new file in the new directory")
	write("a.txt", "edited\n")
	check("edit")
	gitIn(t, r.Dir, "add", "a.txt")
	check("add")
	gitIn(t, r.Dir, "commit", "-q", "-m", "next")
	check("commit")

	// The index stays as it is, though a file's times changed.
	index := filepath.Join(r.Dir, ".git", "index")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(r.Dir, "a.txt"), future, future); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Status(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("Status wrote the index")
	}
	if _, err := (Repo{Dir: t.TempDir()}).Status(); err == nil {
		t.Error("outside a repository: no error")
	}
}
