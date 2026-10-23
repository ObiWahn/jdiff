package ui

import "jdiff/internal/diff"

// moveGroup is the files searched together for moved code: the files of one
// commit or of a comparison of two directories, or the one file of a
// comparison of two files. Never files of different commits.
type moveGroup struct {
	diffs  []*fileDiff
	names  []string // the files' names, for messages
	aligns []int    // the alignments the moves were found for
	moves  diff.Moves
}

// add adds d, called name, to the group.
func (g *moveGroup) add(d *fileDiff, name string) {
	d.group, d.groupIndex = g, len(g.diffs)
	g.diffs = append(g.diffs, d)
	g.names = append(g.names, name)
	g.aligns = nil
}

// ensure finds the moves again when a diff of the group was aligned anew,
// after an anchor was added or removed or the file was reloaded.
func (g *moveGroup) ensure() {
	fresh := len(g.aligns) == len(g.diffs)
	for i, d := range g.diffs {
		fresh = fresh && g.aligns[i] == d.aligns
	}
	if fresh {
		return
	}
	in := make([]diff.MoveInput, len(g.diffs))
	g.aligns = make([]int, len(g.diffs))
	for i, d := range g.diffs {
		in[i] = diff.MoveInput{Old: d.lines[left], New: d.lines[right], Rows: d.rows}
		g.aligns[i] = d.aligns
	}
	g.moves = diff.FindMoves(in)
}

// movedTo returns where line of side s of d moved: for a removed line the
// line it moved to, for an added line the line it came from.
func (d *fileDiff) movedTo(s side, line int) (diff.Ref, bool) {
	if d.group == nil || line < 0 {
		return diff.Ref{}, false
	}
	d.group.ensure()
	moves := d.group.moves.To
	if s == left {
		moves = d.group.moves.From
	}
	r, ok := moves[d.groupIndex][line]
	return r, ok
}
