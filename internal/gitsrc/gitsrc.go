// Package gitsrc reads commits and their changed files from git.
package gitsrc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
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
	Added, Removed int    // line counts as reported by git
}

// Repo runs git in a directory; the empty Dir means the current directory.
type Repo struct{ Dir string }

func (r Repo) git(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
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

// Log returns the commits git log selects with args, newest first. Merges
// are counted against their first parent, as Files compares them.
// Records start with RS, fields are separated by US and the body, which
// may span lines, ends with GS before the numstat lines.
func (r Repo) Log(args []string) ([]Commit, error) {
	out, err := r.git(append([]string{"log",
		"--format=%x1e%H%x1f%h%x1f%an%x1f%ad%x1f%P%x1f%s%x1f%b%x1d",
		"--date=short", "--numstat", "--diff-merges=first-parent"}, args...)...)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for rec := range strings.SplitSeq(string(out), "\x1e") {
		if rec == "" {
			continue
		}
		header, stat, _ := strings.Cut(rec, "\x1d")
		f := strings.Split(header, "\x1f")
		if len(f) != 7 {
			return nil, fmt.Errorf("git log: unexpected record %q", header)
		}
		c := Commit{Hash: f[0], Short: f[1], Author: f[2], Date: f[3], Subject: f[5],
			Body: strings.TrimSpace(f[6])}
		c.Parent, _, _ = strings.Cut(f[4], " ")
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
		commits = append(commits, c)
	}
	return commits, nil
}

// Branch returns the name of the checked out branch, or "detached HEAD".
func (r Repo) Branch() string {
	out, err := r.git("symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return "detached HEAD"
	}
	return strings.TrimSpace(string(out))
}

// Files returns the files c changed against its first parent, or against
// the empty tree for a root commit, with their contents.
func (r Repo) Files(c Commit) ([]source.File, error) {
	base := c.Parent
	if base == "" {
		base = emptyTree
	}
	out, err := r.git("diff", "--no-ext-diff", "--name-status", "-z", "-M", base, c.Hash)
	if err != nil {
		return nil, err
	}

	var files []source.File
	var specs []string // blob requests for git cat-file, two per file
	tokens := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	for i := 0; i+1 < len(tokens); {
		status := tokens[i]
		f := source.File{Path: tokens[i+1], InOld: true, InNew: true}
		i += 2
		switch status[0] {
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
		oldPath := f.Path
		if f.OldPath != "" {
			oldPath = f.OldPath
		}
		specs = append(specs, base+":"+oldPath, c.Hash+":"+f.Path)
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
