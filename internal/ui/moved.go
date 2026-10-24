package ui

import (
	"fmt"

	"jdiff/internal/diff"
)

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

// moveEnd is the other end of a moved line: line of side side of the file
// at index file of the group.
type moveEnd struct {
	file int
	side side
	line int
}

// otherEnd returns the other end of the moved line under the cursor at, in
// the screen lines of sh, and the message to show there, which says where
// the cursor came from: side by side the line of the active pane, else the
// one beside it. ok is false when neither moved.
func (d *fileDiff) otherEnd(at spot, sh shape) (end moveEnd, msg string, ok bool) {
	s := at.side
	if sh.unified {
		s = d.unifiedSide(at, sh)
	}
	line := d.lineAt(s, at.v)
	r, ok := d.movedTo(s, line)
	if !ok && !sh.unified {
		// Side by side the pane is for anchors: a moved line has no line
		// beside it, so the cursor may be in either pane.
		s = s.other()
		line = d.lineAt(s, at.v)
		r, ok = d.movedTo(s, line)
	}
	if !ok {
		return moveEnd{}, "not moved", false
	}
	how := "moved from"
	if s == right {
		how = "moved to"
	}
	return moveEnd{r.File, s.other(), r.Line}, fmt.Sprintf("%s %s:%d", how, d.group.names[d.groupIndex], line+1), true
}

// spotOf returns the cursor on the start of line of side s of d in the
// screen lines of sh (see firstSub).
func (d *fileDiff) spotOf(s side, line int, sh shape) spot {
	v := d.viewOf[d.rowOf[s][line]]
	return spot{v, d.firstSub(v, s, sh), s}
}
