// Package gitsrc reads commits and their changed files from git.
package gitsrc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"jdiff/internal/source"
)

// emptyTree is the id of git's empty tree, the base of root commits.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Commit is one commit of the log.
type Commit struct {
	Hash, Short    string
	Author, Date   string
	Subject        string
	Body           string // the message after the subject, trimmed
	Parent         string // first parent, empty for a root commit
	Added, Removed int    // line counts, which Log leaves 0: see Counts
}

// LineCounts are the lines a commit added and removed.
type LineCounts struct{ Added, Removed int }

// Repo runs git in a directory; the empty Dir means the current directory.
type Repo struct{ Dir string }

func (r Repo) git(args ...string) ([]byte, error) { return r.run(nil, nil, args...) }

// run runs git with env added to its environment and stdin as its input.
func (r Repo) run(env []string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = append(cmd.Environ(), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("git %s: %s", args[0], msg)
		}
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}

// Reversed reports whether git log lists the commits of args oldest first:
// each --reverse before a -- flips the order.
func Reversed(args []string) bool {
	rev := false
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "--reverse" {
			rev = !rev
		}
	}
	return rev
}

// Log returns the commits git log selects with args, in its order, without
// their line counts: counting diffs every commit, which takes seconds in a
// long history, so Counts does it for the commits shown. Records start with
// RS, fields are separated by US and the body, which may span lines, ends
// with GS.
func (r Repo) Log(args []string) ([]Commit, error) {
	out, err := r.git(append([]string{"log",
		"--format=%x1e%H%x1f%h%x1f%an%x1f%ad%x1f%P%x1f%s%x1f%b%x1d",
		"--date=short"}, args...)...)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for rec := range strings.SplitSeq(string(out), "\x1e") {
		if rec == "" {
			continue
		}
		header, _, _ := strings.Cut(rec, "\x1d")
		f := strings.Split(header, "\x1f")
		if len(f) != 7 {
			return nil, fmt.Errorf("git log: unexpected record %q", header)
		}
		c := Commit{Hash: f[0], Short: f[1], Author: f[2], Date: f[3], Subject: f[5],
			Body: strings.TrimSpace(f[6])}
		c.Parent, _, _ = strings.Cut(f[4], " ")
		commits = append(commits, c)
	}
	return commits, nil
}

// Counts returns the lines the commits hashes added and removed, by hash.
// Merges are counted against their first parent, as Files compares them,
// root commits against the empty tree; binary files are not counted.
func (r Repo) Counts(hashes []string) (map[string]LineCounts, error) {
	counts := map[string]LineCounts{}
	if len(hashes) == 0 {
		return counts, nil
	}
	out, err := r.git(append([]string{"log", "--no-walk=unsorted", "--format=%x1e%H", "--numstat",
		"--diff-merges=first-parent", "--end-of-options"}, hashes...)...)
	if err != nil {
		return nil, err
	}
	for rec := range strings.SplitSeq(string(out), "\x1e") {
		hash, stat, _ := strings.Cut(rec, "\n")
		if hash == "" {
			continue
		}
		var c LineCounts
		for line := range strings.SplitSeq(stat, "\n") {
			// "added<TAB>removed<TAB>path"; binary files show "-".
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) < 3 {
				continue
			}
			added, err1 := strconv.Atoi(parts[0])
			removed, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil {
				c.Added += added
				c.Removed += removed
			}
		}
		counts[hash] = c
	}
	return counts, nil
}

// TopLevel returns the top directory of the repository, or an error outside
// one. git writes it with slashes also on Windows; it is returned in the
// system's form, so that it compares with other paths.
func (r Repo) TopLevel() (string, error) {
	out, err := r.git("rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}

// Branch returns the name of the checked out branch, or "detached HEAD".
func (r Repo) Branch() string {
	out, err := r.git("symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return "detached HEAD"
	}
	return strings.TrimSpace(string(out))
}

// WatchPaths returns the files that change when the repository does: HEAD, the
// index, the packed refs and the ref of the checked out branch. They are where
// git has them, also in a worktree.
func (r Repo) WatchPaths() ([]string, error) {
	names := []string{"HEAD", "index", "packed-refs"}
	if ref, err := r.git("symbolic-ref", "-q", "HEAD"); err == nil {
		names = append(names, strings.TrimSpace(string(ref)))
	}
	args := []string{"rev-parse"}
	for _, n := range names {
		args = append(args, "--git-path", n)
	}
	out, err := r.git(args...)
	if err != nil {
		return nil, err
	}
	paths := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i, p := range paths {
		if !filepath.IsAbs(p) {
			paths[i] = filepath.Join(r.Dir, p) // relative to where git ran
		}
	}
	return paths, nil
}

// Status returns the state of the repository: the commit of HEAD, the branch,
// staged, changed and untracked files. It does not write the index.
func (r Repo) Status() (string, error) {
	out, err := r.run([]string{"GIT_OPTIONAL_LOCKS=0"}, nil, "status", "--porcelain=v2", "--branch", "--untracked-files=all")
	return string(out), err
}

// Files returns the files c changed against its first parent, or against
// the empty tree for a root commit, with their contents.
func (r Repo) Files(c Commit) ([]source.File, error) {
	base := c.Parent
	if base == "" {
		base = emptyTree
	}
	out, err := r.git("diff", "--no-ext-diff", "--raw", "-z", "--no-abbrev", "-M", base, c.Hash)
	if err != nil {
		return nil, err
	}

	// Blobs are requested by object id, not by "rev:path": git cat-file
	// reads one request per line, and paths may hold newlines.
	var files []source.File
	var specs []string // blob requests for git cat-file, two per file
	tokens := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	for i := 0; i+1 < len(tokens); {
		// ":oldmode newmode oldid newid status", then the path or paths.
		meta := strings.Fields(strings.TrimPrefix(tokens[i], ":"))
		if len(meta) != 5 {
			return nil, fmt.Errorf("git diff: unexpected entry %q", tokens[i])
		}
		f := source.File{Path: tokens[i+1], InOld: true, InNew: true}
		i += 2
		switch meta[4][0] {
		case 'A':
			f.InOld = false
		case 'D':
			f.InNew = false
		case 'R', 'C':
			if i >= len(tokens) {
				return nil, fmt.Errorf("git diff: truncated rename of %s", f.Path)
			}
			f.OldPath, f.Path = f.Path, tokens[i]
			i++
		}
		specs = append(specs, meta[2], meta[3])
		files = append(files, f)
	}

	blobs, err := r.catFiles(specs)
	if err != nil {
		return nil, err
	}
	for i := range files {
		f := &files[i]
		if f.InOld {
			f.Old, f.Binary = lines(blobs[2*i], f.Binary)
		}
		if f.InNew {
			f.New, f.Binary = lines(blobs[2*i+1], f.Binary)
		}
	}
	return files, nil
}

// Diff returns the files git diff compares with args, the arguments of git
// diff: the working tree against the index without any, --cached for the
// index against HEAD, commits and paths. Sides in the working tree, which
// git names by the zero id, are read from disk and set OldFile or NewFile,
// so that they can be watched. Unmerged files are left out.
func (r Repo) Diff(args []string) ([]source.File, error) { return r.diff(nil, args) }

// DiffUntracked returns what Diff does, and the untracked files as added
// where git diff compares with the working tree. They are marked with git add
// -N in a copy of the index, which is gone on return: the real one stays as
// it is. Directories, as nested repositories, and files that cannot be read,
// as broken symlinks, are left out.
func (r Repo) DiffUntracked(args []string) ([]source.File, error) {
	top, err := r.TopLevel()
	if err != nil {
		return nil, err
	}
	paths, err := Repo{Dir: top}.untracked()
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return r.Diff(args)
	}
	dir, err := os.MkdirTemp("", "jdiff-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	index, err := r.copyIndex(dir)
	if err != nil {
		return nil, err
	}
	env := []string{"GIT_INDEX_FILE=" + index}
	// One file may be named like a pathspec, say with a * in it.
	list := []byte(strings.Join(paths, "\x00"))
	if _, err := (Repo{Dir: top}).run(append(env, "GIT_LITERAL_PATHSPECS=1"), list,
		"add", "-N", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
		return nil, err
	}
	return r.diff(env, args)
}

// untracked returns the paths of the untracked files that are not ignored,
// relative to r.Dir, which must be the top of the working tree.
func (r Repo) untracked() ([]string, error) {
	out, err := r.git("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if p != "" && readableFile(filepath.Join(r.Dir, filepath.FromSlash(p))) {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// readableFile reports whether path is, or links to, a regular file that can
// be opened.
func readableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// copyIndex copies the index into dir and returns the path of the copy. A
// repository without an index yet leaves nothing to copy.
func (r Repo) copyIndex(dir string) (string, error) {
	out, err := r.git("rev-parse", "--git-path", "index")
	if err != nil {
		return "", err
	}
	from := strings.TrimSpace(string(out))
	if !filepath.IsAbs(from) {
		from = filepath.Join(r.Dir, from)
	}
	to := filepath.Join(dir, "index")
	data, err := os.ReadFile(from)
	if errors.Is(err, fs.ErrNotExist) {
		return to, nil
	}
	if err != nil {
		return "", err
	}
	return to, os.WriteFile(to, data, 0o600)
}

// diff is Diff with env added to the environment of git diff.
func (r Repo) diff(env, args []string) ([]source.File, error) {
	top, err := r.TopLevel()
	if err != nil {
		return nil, err
	}
	// Paths relative to the top, also with diff.relative set.
	out, err := r.run(env, nil, append([]string{"diff", "--no-ext-diff", "--no-relative", "--raw", "-z", "--no-abbrev", "-M"},
		args...)...)
	if err != nil {
		return nil, err
	}

	// A side of a file, in the order of the files and old before new: a
	// blob, or the working copy for the zero id.
	type sideOf struct {
		blob int // index into specs, -1 for the working copy
		path string
	}
	var files []source.File
	var sides []sideOf
	var specs []string
	tokens := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	for i := 0; i+1 < len(tokens); {
		// ":oldmode newmode oldid newid status", then the path or paths.
		meta := strings.Fields(strings.TrimPrefix(tokens[i], ":"))
		if len(meta) != 5 {
			return nil, fmt.Errorf("git diff: unexpected entry %q", tokens[i])
		}
		f := source.File{Path: tokens[i+1], InOld: true, InNew: true}
		i += 2
		switch meta[4][0] {
		case 'A':
			f.InOld = false
		case 'D':
			f.InNew = false
		case 'R', 'C':
			if i >= len(tokens) {
				return nil, fmt.Errorf("git diff: truncated rename of %s", f.Path)
			}
			f.OldPath, f.Path = f.Path, tokens[i]
			i++
		case 'U':
			continue
		}
		files = append(files, f)
		for _, sd := range []struct {
			in   bool
			id   string
			path string
		}{{f.InOld, meta[2], f.PathInOld()}, {f.InNew, meta[3], f.Path}} {
			if !sd.in {
				continue
			}
			s := sideOf{blob: -1, path: sd.path}
			if strings.Trim(sd.id, "0") != "" {
				s.blob = len(specs)
				specs = append(specs, sd.id)
			}
			sides = append(sides, s)
		}
	}

	blobs, err := r.catFiles(specs)
	if err != nil {
		return nil, err
	}
	n := 0
	for i := range files {
		f := &files[i]
		for _, old := range []bool{true, false} {
			if (old && !f.InOld) || (!old && !f.InNew) {
				continue
			}
			sd := sides[n]
			n++
			var content []string
			binary := false
			if sd.blob >= 0 {
				content, binary = lines(blobs[sd.blob], false)
			} else {
				path := filepath.Join(top, filepath.FromSlash(sd.path))
				l, stamp, err := source.Read(path)
				switch {
				case errors.Is(err, source.ErrBinary):
					binary = true
				case err != nil:
					return nil, err
				}
				content = l
				if old {
					f.OldFile, f.OldStamp = path, stamp
				} else {
					f.NewFile, f.NewStamp = path, stamp
				}
			}
			f.Binary = f.Binary || binary
			if old {
				f.Old = content
			} else {
				f.New = content
			}
		}
	}
	return files, nil
}

// lines splits a blob, reporting missing blobs (e.g. submodules) and
// binary content as binary.
func lines(b blob, binary bool) ([]string, bool) {
	if b.missing {
		return nil, true
	}
	l, err := source.SplitLines(b.data)
	return l, binary || errors.Is(err, source.ErrBinary)
}

type blob struct {
	data    []byte
	missing bool
}

// catFiles reads the objects named by specs ("rev:path") with a single
// git cat-file --batch process.
func (r Repo) catFiles(specs []string) ([]blob, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	cmd := exec.Command("git", "cat-file", "--batch")
	cmd.Dir = r.Dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		w := bufio.NewWriter(stdin)
		for _, s := range specs {
			fmt.Fprintln(w, s)
		}
		w.Flush()
		stdin.Close()
	}()

	// fail stops git, which may be blocked writing output no longer read.
	fail := func(err error) ([]blob, error) {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, err
	}

	rd := bufio.NewReader(stdout)
	blobs := make([]blob, len(specs))
	for i := range specs {
		header, err := rd.ReadString('\n')
		if err != nil {
			return fail(fmt.Errorf("git cat-file: %w", err))
		}
		// "<oid> <type> <size>" or "<object> missing"
		f := strings.Fields(header)
		if len(f) != 3 {
			blobs[i].missing = true
			continue
		}
		size, err := strconv.Atoi(f[2])
		if err != nil {
			return fail(fmt.Errorf("git cat-file: bad header %q", header))
		}
		data := make([]byte, size+1) // content and trailing newline
		if _, err := io.ReadFull(rd, data); err != nil {
			return fail(fmt.Errorf("git cat-file: %w", err))
		}
		blobs[i] = blob{data: data[:size], missing: f[1] != "blob"}
	}
	return blobs, cmd.Wait()
}
