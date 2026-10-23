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
}

// ReadLines reads a text file into lines without their line terminators.
func ReadLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return SplitLines(data)
}

// SplitLines splits text into lines without their line terminators, or
// returns ErrBinary for data containing NUL bytes.
func SplitLines(data []byte) ([]string, error) {
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, ErrBinary
	}
	if len(data) == 0 {
		return nil, nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines, nil
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

	var out []File
	for _, p := range paths {
		f := File{
			Path:  p,
			InOld: slices.Contains(oldFiles, p),
			InNew: slices.Contains(newFiles, p),
		}
		if f.InOld {
			if f.Old, err = ReadLines(filepath.Join(oldDir, p)); err != nil && !errors.Is(err, ErrBinary) {
				return nil, err
			}
			f.Binary = f.Binary || errors.Is(err, ErrBinary)
		}
		if f.InNew {
			if f.New, err = ReadLines(filepath.Join(newDir, p)); err != nil && !errors.Is(err, ErrBinary) {
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
