package ui

import (
	"fmt"

	"jdiff/internal/diff"
)

// moveGroup is the files searched together for moved code: the files of one
// commit or of a comparison of two directories, or the one file of a
// comparison of two files. Never files of different commits.
type moveGroup struct {
	diffs  []*fileAlign
	names  [][2]string // the names of the sides of the files, for messages
	aligns []int       // the alignments the moves were found for
	moves  diff.Moves
}

// add adds a, whose sides are called names, to the group.
func (g *moveGroup) add(a *fileAlign, names [2]string) {
	a.group, a.groupIndex = g, len(g.diffs)
	g.diffs = append(g.diffs, a)
	g.names = append(g.names, names)
	g.aligns = nil
}

// ensure finds the moves again when a diff of the group was aligned anew,
// after an anchor was added or removed or the file was reloaded.
func (g *moveGroup) ensure() {
	fresh := len(g.aligns) == len(g.diffs)
	for i, a := range g.diffs {
		fresh = fresh && g.aligns[i] == a.aligns
	}
	if fresh {
		return
	}
	in := make([]diff.MoveInput, len(g.diffs))
	g.aligns = make([]int, len(g.diffs))
	for i, a := range g.diffs {
		in[i] = diff.MoveInput{Old: a.lines[left], New: a.lines[right], Rows: a.rows}
		g.aligns[i] = a.aligns
	}
	g.moves = diff.FindMoves(in)
}

// movedTo returns where line of side s of a moved: for a removed line the
// line it moved to, for an added line the line it came from.
func (a *fileAlign) movedTo(s side, line int) (diff.Ref, bool) {
	if a.group == nil || line < 0 {
		return diff.Ref{}, false
	}
	a.group.ensure()
	moves := a.group.moves.To
	if s == left {
		moves = a.group.moves.From
	}
	r, ok := moves[a.groupIndex][line]
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
func (a *fileAlign) otherEnd(at spot, sh shape) (end moveEnd, msg string, ok bool) {
	s := at.side
	if sh.unified {
		s = a.unifiedSide(at, sh)
	}
	line := a.lineAt(s, at.v)
	r, ok := a.movedTo(s, line)
	if !ok && !sh.unified {
		// Side by side the pane is for anchors: a moved line has no line
		// beside it, so the cursor may be in either pane.
		s = s.other()
		line = a.lineAt(s, at.v)
		r, ok = a.movedTo(s, line)
	}
	if !ok {
		return moveEnd{}, "not moved", false
	}
	how := "moved from"
	if s == right {
		how = "moved to"
	}
	return moveEnd{r.File, s.other(), r.Line}, fmt.Sprintf("%s %s:%d", how, a.group.names[a.groupIndex][s], line+1), true
}

// spotOf returns the cursor on the start of line of side s of a in the
// screen lines of sh (see firstSub).
func (a *fileAlign) spotOf(s side, line int, sh shape) spot {
	v := a.viewOf[a.rowOf[s][line]]
	return spot{v, a.firstSub(v, s, sh), s}
}
