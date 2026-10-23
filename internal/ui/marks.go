package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"jdiff/internal/config"
)

// markJumpMsg asks the file selector to show mark letter in another file of
// the diff, at screen row screenRow.
type markJumpMsg struct {
	letter    string
	screenRow int
}

// isMarkLetter reports whether k names a mark: a letter a-z or A-Z.
func isMarkLetter(k string) bool {
	if utf8.RuneCountInString(k) != 1 {
		return false
	}
	c := k[0]
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// repoPath returns the path of file in marks: relative to the repository
// repo when inside it, else absolute.
func repoPath(repo, file string) string {
	abs, err := filepath.Abs(file)
	if err != nil {
		return file
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	if repo != "" {
		if rel, err := filepath.Rel(repo, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return filepath.ToSlash(rel)
		}
	}
	return abs
}

// setMark sets mark letter on the new-file line of displayed row v of d and
// returns the message to show.
func setMark(cfg *config.Config, d *fileDiff, letter string, v int) string {
	line := d.lineAt(right, v)
	if line < 0 {
		return "no line of the new file here"
	}
	m := config.Mark{Path: d.markPath, Line: line + 1, Text: d.lines[right][line]}
	if err := cfg.SetMark(letter, m); err != nil {
		return "marks: " + err.Error()
	}
	return fmt.Sprintf("mark %s: %s:%d", letter, m.Path, m.Line)
}

// markLine returns the new-file line of mark m in d: its line when it still
// holds its text, else the nearest line that does, else its line number
// within the file, with a note. ok is false for an empty new file.
func (d *fileDiff) markLine(m config.Mark) (line int, note string, ok bool) {
	lines := d.lines[right]
	if len(lines) == 0 {
		return 0, "", false
	}
	want := m.Line - 1
	for dist := 0; dist < len(lines); dist++ {
		for _, i := range []int{want - dist, want + dist} {
			if i >= 0 && i < len(lines) && lines[i] == m.Text {
				return i, "", true
			}
		}
	}
	return max(min(want, len(lines)-1), 0), "line changed", true
}

// reveal returns the cursor on line of side s of d, unfolding the block of
// unchanged rows the line is in.
func (d *fileDiff) reveal(s side, line int) spot {
	row := d.rowOf[s][line]
	for _, b := range d.blocks {
		if row >= b.start && row < b.end && !d.blockOpen(b) {
			d.foldOpen[d.rows[b.start].Left] = true
			d.buildView()
			break
		}
	}
	return d.spotOf(s, line)
}

// markMsg returns the message for having jumped to mark letter.
func markMsg(letter, note string) string {
	if note != "" {
		return "mark " + letter + ": " + note
	}
	return "mark " + letter
}

// notInDiff returns the message for mark letter m outside the diff shown.
func notInDiff(letter string, m config.Mark) string {
	return fmt.Sprintf("mark %s: %s is not in this diff", letter, m.Path)
}
