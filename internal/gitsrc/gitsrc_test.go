package gitsrc

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
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
	dir := t.TempDir()
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
	if root.Added != 7 || root.Removed != 0 {
		t.Errorf("root counts +%d -%d, want +7 -0", root.Added, root.Removed)
	}
	if second.Added != 2 || second.Removed != 1 {
		t.Errorf("second counts +%d -%d, want +2 -1 (binary not counted)", second.Added, second.Removed)
	}
	if merge.Added != 1 || merge.Removed != 0 {
		t.Errorf("merge counts +%d -%d, want +1 -0 against the first parent", merge.Added, merge.Removed)
	}
	if second.Body != "first body line\nsecond body line" || root.Body != "" {
		t.Errorf("bodies: second %q root %q", second.Body, root.Body)
	}
	if root.Short == "" || root.Author != "T" || root.Date == "" {
		t.Errorf("root = %+v", root)
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
	dir := t.TempDir()
	fake := "#!/usr/bin/env bash\necho 'x blob notasize'\nexec yes\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
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
	want, err := filepath.EvalSymlinks(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if top, err := (Repo{Dir: filepath.Join(r.Dir, "sub")}).TopLevel(); err != nil || top != want {
		t.Errorf("TopLevel from sub = %q, %v; want %q", top, err, want)
	}
	if _, err := (Repo{Dir: t.TempDir()}).TopLevel(); err == nil {
		t.Error("TopLevel outside a repository succeeded")
	}
}
