package diff

import (
	"strings"
	"unicode"
)

// MoveInput is one file of the files searched for moved code, such as the
// files of one commit: its old and new lines and their alignment.
type MoveInput struct {
	Old, New []string
	Rows     []Row
}

// Ref is a line of one of the files: the index of the file and of the line.
type Ref struct{ File, Line int }

// Moves are the moved lines of the files, per file: From maps removed lines
// (indices into Old) to the lines they moved to, To maps added lines
// (indices into New) to the lines they came from.
type Moves struct {
	From, To []map[int]Ref
}

// minMovedAlnum is the number of letters and digits a block needs to count
// as moved, as in git, so that lone braces and the like are not marked.
const minMovedAlnum = 20

// FindMoves finds blocks of consecutive removed lines that were added again
// as consecutive lines, in the same file or another one. Lines are compared
// without surrounding whitespace, as Align matches them. Lines of an Equal
// row are neither removed nor added, and the two lines of a Change row are a
// change in place, not a move. Blocks are taken in file and line order, the
// longest at each line first; each line belongs to at most one block.
func FindMoves(files []MoveInput) Moves {
	type side struct {
		row  map[int]int // line -> row, for removed or added lines only
		used map[int]bool
	}
	removed := make([]side, len(files))
	added := make([]side, len(files))
	candidates := map[string][]Ref{} // added lines by their key
	// Added lines followed by another added line, by the keys of both: the
	// starts of blocks of two lines or more, so that a line found in many
	// places, like a blank one, is not tried against all of them.
	pairs := map[string][]Ref{}
	pairKey := func(lines []string, line int) string {
		return strings.TrimSpace(lines[line]) + "\n" + strings.TrimSpace(lines[line+1])
	}
	for i, f := range files {
		removed[i] = side{row: map[int]int{}, used: map[int]bool{}}
		added[i] = side{row: map[int]int{}, used: map[int]bool{}}
		for r, row := range f.Rows {
			if row.Kind == Equal {
				continue
			}
			if row.Left >= 0 {
				removed[i].row[row.Left] = r
			}
			if row.Right >= 0 {
				added[i].row[row.Right] = r
			}
		}
		for line := range f.New {
			if _, ok := added[i].row[line]; ok {
				key := strings.TrimSpace(f.New[line])
				candidates[key] = append(candidates[key], Ref{i, line})
				if _, ok := added[i].row[line+1]; ok {
					pk := pairKey(f.New, line)
					pairs[pk] = append(pairs[pk], Ref{i, line})
				}
			}
		}
	}

	// matches reports whether removed line l of file i can move to added
	// line to.
	matches := func(i, l int, to Ref) bool {
		rr, ok := removed[i].row[l]
		if !ok || removed[i].used[l] {
			return false
		}
		ar, ok := added[to.File].row[to.Line]
		if !ok || added[to.File].used[to.Line] || (to.File == i && ar == rr) {
			return false
		}
		return strings.TrimSpace(files[i].Old[l]) == strings.TrimSpace(files[to.File].New[to.Line])
	}

	moves := Moves{From: make([]map[int]Ref, len(files)), To: make([]map[int]Ref, len(files))}
	for i := range files {
		moves.From[i], moves.To[i] = map[int]Ref{}, map[int]Ref{}
	}
	// longest returns the longest block starting at removed line l of file
	// i among the added lines cands, the first one on a tie, and its length.
	longest := func(i, l int, cands []Ref) (best Ref, n int) {
		for _, c := range cands {
			k := 0
			for matches(i, l+k, Ref{c.File, c.Line + k}) {
				k++
			}
			if k > n {
				best, n = c, k
			}
		}
		return best, n
	}
	for i, f := range files {
		for l := 0; l < len(f.Old); l++ {
			// Blocks of two lines or more start among the pairs, in the
			// order of the candidates. A single line counts only with
			// enough letters and digits of its own, rarely found in many
			// places: only then are all candidates tried.
			var best Ref
			n := 0
			if l+1 < len(f.Old) {
				best, n = longest(i, l, pairs[pairKey(f.Old, l)])
			}
			if n < 2 && alnum(f.Old[l:l+1]) >= minMovedAlnum {
				best, n = longest(i, l, candidates[strings.TrimSpace(f.Old[l])])
			}
			if n == 0 || alnum(f.Old[l:l+n]) < minMovedAlnum {
				continue
			}
			for k := range n {
				to := Ref{best.File, best.Line + k}
				removed[i].used[l+k], added[to.File].used[to.Line] = true, true
				moves.From[i][l+k] = to
				moves.To[to.File][to.Line] = Ref{i, l + k}
			}
			l += n - 1
		}
	}
	return moves
}

// alnum counts the letters and digits in lines.
func alnum(lines []string) int {
	n := 0
	for _, l := range lines {
		for _, r := range l {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				n++
			}
		}
	}
	return n
}
