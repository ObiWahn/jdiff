package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

// Mark is a jump mark: a line of the new side of a file.
type Mark struct {
	Path string `toml:"path"` // relative to the repository, absolute outside one
	Line int    `toml:"line"` // 1-based
	Text string `toml:"text"` // the line when the mark was set, to find it again
}

// LetterMark is a mark with its letter.
type LetterMark struct {
	Letter string
	Mark
}

// marksState holds the jump marks of marks.toml: a table per repository,
// keyed by its top directory, "" outside repositories.
type marksState struct {
	path string                     // marks.toml, empty for no file
	repo string                     // the current repository
	all  map[string]map[string]Mark // the marks of all repositories
}

// LoadMarks reads the jump marks of repository repo, its top directory or ""
// outside repositories, from marks.toml in the configuration directory. A
// file that cannot be parsed is reported by Warning and replaced by the next
// change of a mark.
func (c *Config) LoadMarks(repo string) {
	c.marks = marksState{repo: repo, all: map[string]map[string]Mark{}}
	if c.dir == "" {
		return
	}
	c.marks.path = filepath.Join(c.dir, "marks.toml")
	if _, err := toml.DecodeFile(c.marks.path, &c.marks.all); err != nil && !errors.Is(err, fs.ErrNotExist) {
		c.warnings = append(c.warnings, fmt.Sprintf("marks.toml: %v; marks are lost on the next change", err))
		c.marks.all = map[string]map[string]Mark{}
	}
}

// Repo returns the repository whose marks are used, "" outside one.
func (c *Config) Repo() string { return c.marks.repo }

// Mark returns the mark of letter in the current repository.
func (c *Config) Mark(letter string) (Mark, bool) {
	m, ok := c.marks.all[c.marks.repo][letter]
	return m, ok
}

// Marks returns the marks of the current repository, sorted by letter.
func (c *Config) Marks() []LetterMark {
	var out []LetterMark
	for letter, m := range c.marks.all[c.marks.repo] {
		out = append(out, LetterMark{letter, m})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Letter < out[j].Letter })
	return out
}

// SetMark sets the mark of letter in the current repository and saves the
// marks.
func (c *Config) SetMark(letter string, m Mark) error {
	if c.marks.all == nil {
		c.marks.all = map[string]map[string]Mark{}
	}
	if c.marks.all[c.marks.repo] == nil {
		c.marks.all[c.marks.repo] = map[string]Mark{}
	}
	c.marks.all[c.marks.repo][letter] = m
	return c.saveMarks()
}

// DeleteMark deletes the mark of letter in the current repository and saves
// the marks.
func (c *Config) DeleteMark(letter string) error {
	delete(c.marks.all[c.marks.repo], letter)
	if len(c.marks.all[c.marks.repo]) == 0 {
		delete(c.marks.all, c.marks.repo)
	}
	return c.saveMarks()
}

func (c *Config) saveMarks() error {
	if c.marks.path == "" {
		return nil
	}
	return writeTOML(c.marks.path, "# Written by jdiff: jump marks per repository (m<letter> sets, '<letter> jumps).\n", c.marks.all)
}
