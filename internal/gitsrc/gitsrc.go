// Package gitsrc reads commits and their changed files from git.
package gitsrc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"jdiff/internal/source"
)

// emptyTree is the id of git's empty tree, the base of root commits.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Commit is one commit of the log.
type Commit struct {
	Hash, Short    string
	Author, Date   string // Date: of the author, local, as 2006-01-02 15:04
	Subject        string
	Body           string // the message after the subject, trimmed
	Parent         string // first parent, empty for a root commit
	Added, Removed int    // line counts, which Log leaves 0: see Counts
}

// LineCounts are the lines a commit added and removed.
type LineCounts struct{ Added, Removed int }

// Repo runs git in a directory; the empty Dir means the current directory.
type Repo struct {
	Dir string
	ctx context.Context // ends the git commands, see WithContext
}

// WithContext returns r running its git commands with ctx: when it is done,
// they are killed and the methods return its error. Without, they run to
// their end.
func (r Repo) WithContext(ctx context.Context) Repo {
	r.ctx = ctx
	return r
}

// running counts the git commands that run, see Wait.
var running sync.WaitGroup

// Wait waits until the git commands that run have ended, at most d, and
// reports whether they have. Killing the commands of a cancelled context
// takes a moment, which a program that ends must wait for: else they run
// on without it.
func Wait(d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// command returns the command running git with args in r.Dir.
func (r Repo) command(args ...string) *exec.Cmd {
	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Dir
	return cmd
}

func (r Repo) git(args ...string) ([]byte, error) { return r.run(nil, nil, args...) }

// run runs git with env added to its environment and stdin as its input.
func (r Repo) run(env []string, stdin []byte, args ...string) ([]byte, error) {
	cmd := r.command(args...)
	cmd.Env = append(cmd.Environ(), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	running.Add(1)
	out, err := cmd.Output()
	running.Done()
	if err != nil {
		return nil, r.gitError(args[0], &stderr, err)
	}
	return out, nil
}

// gitError returns the error of git name that failed with err: the context's
// error when it was cancelled, as what git says then is of no interest, else
// what git wrote to stderr, else err.
func (r Repo) gitError(name string, stderr *bytes.Buffer, err error) error {
	if r.ctx != nil && r.ctx.Err() != nil {
		return r.ctx.Err()
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return fmt.Errorf("git %s: %s", name, msg)
	}
	return fmt.Errorf("git %s: %w", name, err)
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

// logFormatFlags and diffFormatFlags are the flags of git log and git diff
// that change the output jdiff reads, alone or with =value.
var (
	logFormatFlags  = []string{"--oneline", "--pretty", "--format", "--graph", "--show-signature"}
	diffFormatFlags = []string{"-p", "-u", "--patch", "-s", "--no-patch", "--stat", "--numstat", "--shortstat",
		"--dirstat", "--name-only", "--name-status", "--summary", "--compact-summary", "--check",
		"--patch-with-raw", "--patch-with-stat", "--output"}
)

// checkFormat returns an error naming the first argument before a -- that
// is one of flags: they change the output of git command, which jdiff reads.
func checkFormat(command string, args, flags []string) error {
	for _, a := range args {
		if a == "--" {
			break
		}
		name, _, _ := strings.Cut(a, "=")
		if slices.Contains(flags, name) {
			return fmt.Errorf("git %s: %s changes the output jdiff reads, leave it out", command, name)
		}
	}
	return nil
}

// Log returns the commits git log selects with args, in its order, without
// their line counts: counting diffs every commit, which takes seconds in a
// long history, so Counts does it for the commits shown. Records start with
// RS, fields are separated by US and the body, which may span lines, ends
// with GS. Signatures are not shown, even with log.showSignature set: their
// output would come between the records.
func (r Repo) Log(args []string) ([]Commit, error) {
	if err := checkFormat("log", args, logFormatFlags); err != nil {
		return nil, err
	}
	out, err := r.git(append([]string{"log", "--no-show-signature",
		"--format=%x1e%H%x1f%h%x1f%an%x1f%ad%x1f%P%x1f%s%x1f%b%x1d",
		"--date=format-local:%Y-%m-%d %H:%M"}, args...)...)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for rec := range strings.SplitSeq(string(out), "\x1e") {
		if rec == "" {
			continue
		}
		header, _, _ := strings.Cut(rec, "\x1d")
		fields := strings.Split(header, "\x1f")
		if len(fields) != 7 {
			return nil, fmt.Errorf("git log: unexpected record %q", header)
		}
		c := Commit{Hash: fields[0], Short: fields[1], Author: fields[2], Date: fields[3],
			Subject: fields[5], Body: strings.TrimSpace(fields[6])}
		c.Parent, _, _ = strings.Cut(fields[4], " ")
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
	out, err := r.git(append([]string{"log", "--no-walk=unsorted", "--no-show-signature", "--format=%x1e%H", "--numstat", "--no-relative",
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
	return r.gitPaths(names...)
}

// gitPaths returns where git keeps the files names, also in a worktree:
// absolute and cleaned, as git gives them relative to where it ran.
func (r Repo) gitPaths(names ...string) ([]string, error) {
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
			p = filepath.Join(r.Dir, p) // relative to where git ran
		}
		paths[i] = filepath.Clean(p) // git writes / also on Windows
	}
	return paths, nil
}

// vcsDirs are where version control systems keep their own data, which is
// not worked on: WorkDirs leaves them out, and what is below them. Fossil
// keeps its data in files.
var vcsDirs = []string{".git", ".svn", ".hg", ".bzr", "CVS", "_darcs", ".jj", ".pijul", ".sl"}

// WorkDirs returns the directories of the working tree that hold its files,
// tracked or untracked and not ignored, with the time each was modified, as
// when a file there is created, deleted or renamed, as editors save; the top
// level always. A directory takes one lstat, not each of its files, so that
// this stays cheap in large repositories. A submodule and a nested
// repository count as entries of the directory they are in, which is not
// entered. Left out are the directories of vcsDirs, and those for which
// ignore holds of their path from the top with / and a / at the end, with
// the directories below them. With the ignored or excluded files of extra,
// the directories of those it does not hide count too.
func (r Repo) WorkDirs(ignore func(rel string) bool, extra Extra) (map[string]time.Time, error) {
	top, err := r.TopLevel()
	if err != nil {
		return nil, err
	}
	// All files, also from a subdirectory, by their paths from the top.
	out, err := r.git("ls-files", "-z", "--cached", "--others", "--exclude-standard", "--full-name", "--", ":/")
	if err != nil {
		return nil, err
	}
	dirs := map[string]time.Time{top: {}}
	left := map[string]bool{} // directories from the top, whether left out
	var leftOut func(rel string) bool
	leftOut = func(rel string) bool {
		if rel == "." {
			return false
		}
		if l, ok := left[rel]; ok {
			return l
		}
		l := slices.Contains(vcsDirs, path.Base(rel)) || ignore(rel+"/") || leftOut(path.Dir(rel))
		left[rel] = l
		return l
	}
	rels := strings.Split(string(out), "\x00")
	if extra.Ignored || extra.Excluded {
		added, err := Repo{Dir: top, ctx: r.ctx}.extraFiles(Extra{Ignored: extra.Ignored, Excluded: extra.Excluded, Hide: extra.Hide})
		if err != nil {
			return nil, err
		}
		rels = slices.AppendSeq(rels, maps.Keys(added))
	}
	for _, rel := range rels {
		// A nested repository comes as its directory with a /.
		rel = strings.TrimSuffix(rel, "/")
		if rel == "" || leftOut(path.Dir(rel)) {
			continue
		}
		dirs[filepath.Dir(filepath.Join(top, filepath.FromSlash(rel)))] = time.Time{}
	}
	for dir := range dirs {
		if info, err := os.Lstat(dir); err == nil {
			dirs[dir] = info.ModTime()
		}
	}
	return dirs, nil
}

// Status returns the state of the repository: the commit of HEAD, the branch,
// staged, changed and untracked files, and with the ignored or excluded
// files of extra the ignored ones too, but those extra.Hide hides. It does
// not write the index.
func (r Repo) Status(extra Extra) (string, error) {
	args := []string{"status", "--porcelain=v2", "--branch", "--untracked-files=all"}
	if !extra.Ignored && !extra.Excluded {
		out, err := r.run([]string{"GIT_OPTIONAL_LOCKS=0"}, nil, args...)
		return string(out), err
	}
	out, err := r.run([]string{"GIT_OPTIONAL_LOCKS=0"}, nil, append(args, "--ignored")...)
	if err != nil {
		return "", err
	}
	// The paths of git status are relative to where it ran.
	top, err := r.TopLevel()
	if err != nil {
		return "", err
	}
	dir, err := filepath.Abs(r.Dir)
	if err != nil {
		return "", err
	}
	hidden := hiddenPaths(extra.Hide)
	var b strings.Builder
	for _, line := range strings.SplitAfter(string(out), "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "! "); ok {
			if rel, err := filepath.Rel(top, filepath.Join(dir, filepath.FromSlash(p))); err == nil && hidden(filepath.ToSlash(rel)) {
				continue
			}
		}
		b.WriteString(line)
	}
	return b.String(), nil
}

// StatusPaths returns the paths of the files that the lines of status, from
// Status, name and those of old do not: changed, renamed (the new path),
// unmerged, untracked or ignored. They are relative to where git ran. Paths
// git quotes, with special characters, are left out.
func StatusPaths(status, old string) []string {
	had := map[string]bool{}
	for _, line := range strings.Split(old, "\n") {
		had[line] = true
	}
	var paths []string
	for _, line := range strings.Split(status, "\n") {
		if had[line] || len(line) < 2 {
			continue
		}
		// The fields before the path, which may hold spaces: see git status
		// --porcelain=v2.
		var p string
		switch line[0] {
		case '1':
			p = field(line, 8)
		case '2':
			p, _, _ = strings.Cut(field(line, 9), "\t")
		case 'u':
			p = field(line, 10)
		case '?', '!':
			p = line[2:]
		}
		if p != "" && !strings.HasPrefix(p, `"`) {
			paths = append(paths, filepath.FromSlash(p))
		}
	}
	return paths
}

// field returns the rest of line after n fields separated by single spaces,
// "" when it has fewer.
func field(line string, n int) string {
	parts := strings.SplitN(line, " ", n+1)
	if len(parts) <= n {
		return ""
	}
	return parts[n]
}

// Files returns the files c changed against its first parent, or against
// the empty tree for a root commit, with their contents: what Diff returns
// for the two commits, but without asking for the top of the working tree,
// as two commits have no side in it and a bare repository has none.
func (r Repo) Files(c Commit) ([]source.File, error) {
	base := c.Parent
	if base == "" {
		base = emptyTree
	}
	return r.diffFiles(nil, "", []string{base, c.Hash})
}

// rawEntry is a file in the output of git diff --raw -z, without its content,
// and the ids of its sides.
type rawEntry struct {
	file         source.File
	oldID, newID string
}

// parseRaw parses the output of git diff --raw -z. Unmerged files are left
// out.
func parseRaw(out []byte) ([]rawEntry, error) {
	var entries []rawEntry
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
		entries = append(entries, rawEntry{f, meta[2], meta[3]})
	}
	return entries, nil
}

// IsRevision reports whether git knows arg as a revision, as git diff tells
// revisions from paths before --.
func (r Repo) IsRevision(arg string) bool {
	_, err := r.git("rev-parse", "--verify", "--quiet", arg)
	return err == nil
}

// Diff returns the files git diff compares with args, the arguments of git
// diff: the working tree against the index without any, --cached for the
// index against HEAD, commits and paths. Sides in the working tree, which
// git names by the zero id, are read from disk (see readWorking) and, but
// symlinks, set OldFile or NewFile, so that they can be watched. Unmerged
// files are left out.
func (r Repo) Diff(args []string) ([]source.File, error) { return r.diff(nil, args) }

// Extra are the files DiffUntracked adds to what git diff compares: the
// untracked files that nothing ignores, and of the ignored ones those that
// the .gitignore files of the working tree ignore and those that
// .git/info/exclude does; the global excludes file is left out. Hide leaves
// ignored and excluded files out by their path from the top with /, and so
// does it of a directory above them, with a / at the end; nil hides none. It
// is called in the background.
type Extra struct {
	Untracked, Ignored, Excluded bool
	Hide                         func(rel string) bool
}

// DiffUntracked returns what Diff does, and the files of extra as added
// where git diff compares with the working tree, each with what it was
// added as; a file that both a .gitignore file and .git/info/exclude ignore
// counts as ignored. They are marked with git add -N in a copy of the index,
// which is gone on return: the real one stays as it is. Directories, as
// nested repositories, and files that cannot be read, as broken symlinks,
// are left out.
func (r Repo) DiffUntracked(args []string, extra Extra) ([]source.File, error) {
	top, err := r.TopLevel()
	if err != nil {
		return nil, err
	}
	added, err := Repo{Dir: top, ctx: r.ctx}.extraFiles(extra)
	if err != nil {
		return nil, err
	}
	if len(added) == 0 {
		return r.Diff(args)
	}
	paths := slices.Sorted(maps.Keys(added))
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
	// One file may be named like a pathspec, say with a * in it. -f adds
	// the ignored ones too.
	list := []byte(strings.Join(paths, "\x00"))
	if _, err := (Repo{Dir: top, ctx: r.ctx}).run(append(env, "GIT_LITERAL_PATHSPECS=1"), list,
		"add", "-N", "-f", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
		return nil, err
	}
	files, err := r.diff(env, args)
	for i := range files {
		files[i].Added = added[files[i].Path]
	}
	return files, err
}

// extraFiles returns the files of e by their paths from r.Dir, which must be
// the top of the working tree, with what each is added as.
func (r Repo) extraFiles(e Extra) (map[string]source.Added, error) {
	added := map[string]source.Added{}
	hidden := hiddenPaths(e.Hide)
	list := func(kind source.Added, args ...string) error {
		out, err := r.git(append([]string{"ls-files", "--others", "-z"}, args...)...)
		if err != nil {
			return err
		}
		for _, p := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
			if p == "" || added[p] != source.NotAdded || (kind != source.AddedUntracked && hidden(p)) ||
				!readableFile(filepath.Join(r.Dir, filepath.FromSlash(p))) {
				continue
			}
			added[p] = kind
		}
		return nil
	}
	if e.Untracked {
		if err := list(source.AddedUntracked, "--exclude-standard"); err != nil {
			return nil, err
		}
	}
	if e.Ignored {
		if err := list(source.AddedIgnored, "--ignored", "--exclude-per-directory=.gitignore"); err != nil {
			return nil, err
		}
	}
	if e.Excluded {
		paths, err := r.gitPaths("info/exclude")
		if err != nil {
			return nil, err
		}
		// Without the file nothing is excluded; git would fail on it.
		if info, err := os.Stat(paths[0]); err == nil && info.Mode().IsRegular() {
			if err := list(source.AddedExcluded, "--ignored", "--exclude-from="+paths[0]); err != nil {
				return nil, err
			}
		}
	}
	return added, nil
}

// hiddenPaths returns whether hide holds of a path from the top with /, or
// of a directory above it with a / at the end; nil hides nothing. The
// directories are looked up once each.
func hiddenPaths(hide func(rel string) bool) func(rel string) bool {
	if hide == nil {
		return func(string) bool { return false }
	}
	dirs := map[string]bool{}
	var dirHidden func(dir string) bool
	dirHidden = func(dir string) bool {
		if dir == "." {
			return false
		}
		if h, ok := dirs[dir]; ok {
			return h
		}
		h := hide(dir+"/") || dirHidden(path.Dir(dir))
		dirs[dir] = h
		return h
	}
	return func(rel string) bool { return hide(rel) || dirHidden(path.Dir(rel)) }
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
// repository without an index yet leaves nothing to copy. The copy keeps the
// time the index was modified: git compares the content of the files changed
// in the same second as the index was written, as their times and sizes may
// match, and a later time of the copy would hide such changes. The time is
// taken before the content, so that a write in between leaves it older,
// which only makes git compare more.
func (r Repo) copyIndex(dir string) (string, error) {
	paths, err := r.gitPaths("index")
	if err != nil {
		return "", err
	}
	from := paths[0]
	to := filepath.Join(dir, "index")
	info, err := os.Stat(from)
	if errors.Is(err, fs.ErrNotExist) {
		return to, nil
	}
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(from)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(to, data, 0o600); err != nil {
		return "", err
	}
	return to, os.Chtimes(to, info.ModTime(), info.ModTime())
}

// diff is Diff with env added to the environment of git diff.
func (r Repo) diff(env, args []string) ([]source.File, error) {
	if err := checkFormat("diff", args, diffFormatFlags); err != nil {
		return nil, err
	}
	top, err := r.TopLevel()
	if err != nil {
		return nil, err
	}
	return r.diffFiles(env, top, args)
}

// diffFiles returns the files git diff compares with args, with env added to
// its environment. Sides git names by the zero id are read from the working
// tree at top; Files passes none, as two commits have no such sides. Blobs
// are requested by object id, not by "rev:path": git cat-file reads one
// request per line, and paths may hold newlines.
func (r Repo) diffFiles(env []string, top string, args []string) ([]source.File, error) {
	// Paths relative to the top, also with diff.relative set.
	out, err := r.run(env, nil, append([]string{"diff", "--no-ext-diff", "--no-relative", "--raw", "-z", "--no-abbrev", "-M"},
		args...)...)
	if err != nil {
		return nil, err
	}

	entries, err := parseRaw(out)
	if err != nil {
		return nil, err
	}

	// A side of a file, in the order of the files and old before new: a
	// blob, or the working copy for the zero id.
	type fileSide struct {
		blob int // index into specs, -1 for the working copy
		path string
	}
	var files []source.File
	var sides []fileSide
	var specs []string
	for _, e := range entries {
		f := e.file
		files = append(files, f)
		for _, want := range []struct {
			in   bool
			id   string
			path string
		}{{f.InOld, e.oldID, f.PathInOld()}, {f.InNew, e.newID, f.Path}} {
			if !want.in {
				continue
			}
			s := fileSide{blob: -1, path: want.path}
			if strings.Trim(want.id, "0") != "" {
				s.blob = len(specs)
				specs = append(specs, want.id)
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
			side := sides[n]
			n++
			var content []string
			binary := false
			if side.blob >= 0 {
				content, binary = lines(blobs[side.blob])
			} else {
				path := filepath.Join(top, filepath.FromSlash(side.path))
				w := readWorking(path)
				content, binary = w.lines, w.binary
				if w.watch {
					if old {
						f.OldFile, f.OldStamp = path, w.stamp
					} else {
						f.NewFile, f.NewStamp = path, w.stamp
					}
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

// workingSide is a side of a file read from the working tree, see
// readWorking.
type workingSide struct {
	lines  []string
	stamp  source.Stamp
	binary bool
	watch  bool // reloaded when it changes on disk
}

// readWorking reads the side of a file in the working tree at path as git
// compares it, so that one file cannot fail the whole list: a symlink by
// the path it points to, as git stores it, also one to a directory, and not
// watched, since watching reads what it points to; a directory, as of a
// submodule, and a file that cannot be read as binary, not watched; binary
// content as binary, watched; a file gone since git listed it as empty,
// watched so that it shows when it is back.
func readWorking(path string) workingSide {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return workingSide{watch: true}
	case err != nil:
		return workingSide{binary: true}
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return workingSide{binary: true}
		}
		return workingSide{lines: []string{target}}
	}
	l, stamp, err := source.Read(path)
	switch {
	case errors.Is(err, source.ErrBinary):
		return workingSide{stamp: stamp, binary: true, watch: true}
	case err != nil:
		return workingSide{binary: true}
	}
	return workingSide{lines: l, stamp: stamp, watch: true}
}

// lines splits a blob, reporting missing blobs (e.g. submodules) and
// binary content as binary.
func lines(b blob) ([]string, bool) {
	if b.missing {
		return nil, true
	}
	l, err := source.SplitLines(b.data)
	return l, errors.Is(err, source.ErrBinary)
}

type blob struct {
	data    []byte
	missing bool
}

// catFiles reads the objects named by specs, their ids, with a single git
// cat-file --batch process.
func (r Repo) catFiles(specs []string) ([]blob, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	cmd := r.command("cat-file", "--batch")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
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
	running.Add(1)
	defer running.Done()
	go func() {
		w := bufio.NewWriter(stdin)
		for _, s := range specs {
			fmt.Fprintln(w, s)
		}
		w.Flush()
		stdin.Close()
	}()

	// fail stops git, which may be blocked writing output no longer read.
	// When the context was cancelled, git was killed and err is only the
	// end of its output: the context's error is returned, as by run.
	fail := func(err error) ([]blob, error) {
		cmd.Process.Kill()
		cmd.Wait()
		if r.ctx != nil && r.ctx.Err() != nil {
			return nil, r.ctx.Err()
		}
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
		fields := strings.Fields(header)
		if len(fields) != 3 {
			blobs[i].missing = true
			continue
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			return fail(fmt.Errorf("git cat-file: bad header %q", header))
		}
		data := make([]byte, size+1) // content and trailing newline
		if _, err := io.ReadFull(rd, data); err != nil {
			return fail(fmt.Errorf("git cat-file: %w", err))
		}
		blobs[i] = blob{data: data[:size], missing: fields[1] != "blob"}
	}
	if err := cmd.Wait(); err != nil {
		return nil, r.gitError("cat-file", &stderr, err)
	}
	return blobs, nil
}
