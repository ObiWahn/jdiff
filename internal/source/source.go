// Package source loads the files to compare from disk.
package source

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ErrBinary is returned for files that contain NUL bytes.
var ErrBinary = errors.New("binary file")

// File is one file of a comparison.
type File struct {
	Path     string // relative to both sides, slash separated
	OldPath  string // the old path of a renamed file, else empty
	Old, New []string
	// InOld and InNew report whether the file exists on that side.
	InOld, InNew bool
	Binary       bool
	// OldFile and NewFile are the paths of the sides on disk, empty for
	// files from git; OldStamp and NewStamp identify the versions read.
	OldFile, NewFile   string
	OldStamp, NewStamp Stamp
}

// Stamp identifies a version of a file on disk.
type Stamp struct {
	ModTime time.Time
	Size    int64
}

// StampOf returns the stamp of the file at path, following symlinks.
func StampOf(path string) (Stamp, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Stamp{}, err
	}
	return Stamp{info.ModTime(), info.Size()}, nil
}

// PathInOld returns the path of the file on the old side.
func (f File) PathInOld() string {
	if f.OldPath != "" {
		return f.OldPath
	}
	return f.Path
}

// ReadLines reads a text file into lines without their newlines; the \r of
// a \r\n line end is kept, so that changed line ends show.
func ReadLines(path string) ([]string, error) {
	lines, _, err := Read(path)
	return lines, err
}

// Read reads a text file like ReadLines and returns the stamp of the version
// read. The stamp is taken first, so a change while reading shows later.
func Read(path string) ([]string, Stamp, error) {
	st, err := StampOf(path)
	if err != nil {
		return nil, Stamp{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, Stamp{}, err
	}
	lines, err := SplitLines(data)
	return lines, st, err
}

// SplitLines splits text into lines without their newlines, keeping the \r
// of \r\n line ends, or returns ErrBinary for data containing NUL bytes.
func SplitLines(data []byte) ([]string, error) {
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, ErrBinary
	}
	if len(data) == 0 {
		return nil, nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"), nil
}

// LoadDirs compares the regular files of two directory trees, as handed over
// by git difftool --dir-diff. Files with identical content are left out.
// Symlinks are followed, since git links work tree files into the new tree.
func LoadDirs(oldDir, newDir string) ([]File, error) {
	oldFiles, err := listFiles(oldDir)
	if err != nil {
		return nil, err
	}
	newFiles, err := listFiles(newDir)
	if err != nil {
		return nil, err
	}
	paths := append(slices.Clone(oldFiles), newFiles...)
	slices.Sort(paths)
	paths = slices.Compact(paths)
	inOld, inNew := pathSet(oldFiles), pathSet(newFiles)

	var out []File
	for _, p := range paths {
		f := File{Path: p, InOld: inOld[p], InNew: inNew[p]}
		if f.InOld {
			f.OldFile = filepath.Join(oldDir, p)
			if f.Old, f.OldStamp, err = Read(f.OldFile); err != nil && !errors.Is(err, ErrBinary) {
				return nil, err
			}
			f.Binary = f.Binary || errors.Is(err, ErrBinary)
		}
		if f.InNew {
			f.NewFile = filepath.Join(newDir, p)
			if f.New, f.NewStamp, err = Read(f.NewFile); err != nil && !errors.Is(err, ErrBinary) {
				return nil, err
			}
			f.Binary = f.Binary || errors.Is(err, ErrBinary)
		}
		if !f.Binary && f.InOld == f.InNew && slices.Equal(f.Old, f.New) {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

func pathSet(paths []string) map[string]bool {
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[p] = true
	}
	return set
}

// listFiles returns the sorted slash-separated paths of all regular files
// below dir, following symlinks to files.
func listFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}
