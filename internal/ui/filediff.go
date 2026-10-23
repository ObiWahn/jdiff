package ui

import (
	"fmt"
	"slices"
	"strings"

	"jdiff/internal/config"
	"jdiff/internal/diff"
)

type side int

const (
	left side = iota
	right
)

func (s side) other() side { return 1 - s }

// lineOf returns the line of side s shown in r, or -1.
func lineOf(r diff.Row, s side) int {
	if s == left {
		return r.Left
	}
	return r.Right
}

// emphOf returns the emphasized parts of the line of side s in r.
func emphOf(r diff.Row, s side) []diff.Span {
	if s == left {
		return r.LeftEmph
	}
	return r.RightEmph
}

// anchorLine returns the line of side s that an anchors.
func anchorLine(an diff.Anchor, s side) int {
	if s == left {
		return an.Left
	}
	return an.Right
}

// mark is a line chosen as the first half of a new anchor.
type mark struct {
	side side
	line int
}

// vrow is a displayed row: the diff row row, or when hidden > 0 a fold
// standing for the unchanged rows [row, row+hidden). block is the index of
// the foldable block the row belongs to, or -1.
type vrow struct{ row, hidden, block int }

// sline is a screen line of a diff: line sub of displayed row v. Side by
// side every displayed row is one line; in the normal layout a changed row
// shows its removed line, then its added line.
type sline struct{ v, sub int }

// block is a foldable run of unchanged rows [start, end).
type block struct{ start, end int }

// spot is a cursor in a diff: on line sub of displayed row v, in the pane of
// side side.
type spot struct {
	v, sub int
	side   side
}

// layout is how a view draws a diff: width columns wide, scrolled by xoff
// columns, with the cursor in the pane of side active, in the normal layout
// when unified.
type layout struct {
	width, xoff int
	active      side
	unified     bool
}

// fileDiff is the diff of one file as the user anchored and folded it. The
// single file view, the flow view and the preview all show the same
// fileDiff, each with its own cursor.
type fileDiff struct {
	cfg   *config.Config
	names [2]string
	lines [2][]string

	anchors []diff.Anchor // sorted by Left, never crossing
	rows    []diff.Row
	rowOf   [2][]int // line index -> row index, per side

	blocks   []block      // foldable blocks of unchanged rows
	foldOpen map[int]bool // open state by the block's first left line
	view     []vrow       // displayed rows
	viewOf   []int        // row index -> view index
	changes  int          // counts the rebuilds of view

	pending *mark        // first half of an anchor being created
	replace *diff.Anchor // anchor waiting for confirmation to replace conflicts

	// The last reload: it replaced the lines of side reloadSide, moved maps
	// their old indices to the new ones, -1 for lines gone. reloads counts
	// the reloads.
	reloads    int
	reloadSide side
	moved      []int

	aligns     int        // counts the alignments
	group      *moveGroup // the files searched for moved code with this one
	groupIndex int        // the index of this file in group
}

func newFileDiff(cfg *config.Config, names [2]string, lines [2][]string) *fileDiff {
	d := &fileDiff{cfg: cfg, names: names, lines: lines, foldOpen: map[int]bool{}}
	d.align()
	return d
}

// reload replaces the lines of side s by lines, the file's new content on
// disk. Lines that are unchanged keep their anchors and folds; anchors on
// lines that are gone are dropped and counted. A pending mark or replace
// prompt is cancelled. Views follow their cursor line through moved, so
// they must catch up before the next reload.
func (d *fileDiff) reload(s side, lines []string) (dropped int) {
	moved := lineMap(d.lines[s], lines)
	var anchors []diff.Anchor
	for _, an := range d.anchors {
		n := moved[anchorLine(an, s)]
		if n < 0 {
			dropped++
			continue
		}
		if s == left {
			an.Left = n
		} else {
			an.Right = n
		}
		anchors = append(anchors, an)
	}
	d.anchors = anchors
	if s == left {
		open := map[int]bool{}
		for line, o := range d.foldOpen {
			if n := moved[line]; n >= 0 {
				open[n] = o
			}
		}
		d.foldOpen = open
	}
	d.pending, d.replace = nil, nil
	d.lines[s] = lines
	d.reloads, d.reloadSide, d.moved = d.reloads+1, s, moved
	d.align()
	return dropped
}

// follow returns the displayed row that now shows a line of r, a row before
// the diff changed: its left line, else its right one, through the last
// reload when reloaded is set. It returns -1 when neither line is left.
func (d *fileDiff) follow(r diff.Row, reloaded bool) int {
	for _, s := range []side{left, right} {
		line := lineOf(r, s)
		if line >= 0 && reloaded && s == d.reloadSide {
			line = d.moved[line]
		}
		if line >= 0 {
			return d.viewOf[d.rowOf[s][line]]
		}
	}
	return -1
}

// lineMap maps the indices of old to those of the same lines in new, -1
// for lines that are not in new. It keeps the order of the lines.
func lineMap(old, new []string) []int {
	moved := make([]int, len(old))
	for i := range moved {
		moved[i] = -1
	}
	for _, r := range diff.Align(old, new, nil, diff.Options{}) {
		if r.Kind == diff.Equal {
			moved[r.Left] = r.Right
		}
	}
	return moved
}

// act runs the anchor and fold key k (enter, esc, space or ctrl+space) at
// the cursor at, in the normal layout when unified. It returns the cursor
// afterwards and the message to show, "" for none.
func (d *fileDiff) act(k string, at spot, unified bool) (spot, string) {
	if d.replace != nil {
		an := *d.replace
		d.replace = nil
		switch k {
		case "enter":
			for _, c := range diff.Conflicts(d.anchors, an) {
				d.removeAnchor(c)
			}
			return d.addAnchor(an, at.side)
		case "esc":
			return at, "replace cancelled"
		}
	}
	switch k {
	case "esc":
		if d.pending != nil {
			d.pending = nil
			return at, "mark cancelled"
		}
	case "enter":
		return d.enter(at, unified)
	case " ":
		return d.toggleNearestBlock(at)
	case "ctrl+@": // ctrl+space
		return d.toggleAllBlocks(at)
	}
	return at, ""
}

// enter implements the anchor workflow: mark a line, then pick its partner
// in the other pane. Enter on an anchored line removes that anchor. In the
// normal layout the line under the cursor decides the side.
func (d *fileDiff) enter(at spot, unified bool) (spot, string) {
	if unified {
		at.side = d.unifiedSide(at)
	}
	line := d.lineAt(at.side, at.v)
	if line < 0 {
		if at.v < len(d.view) && d.view[at.v].hidden > 0 {
			return at, "unchanged lines folded, space expands"
		}
		return at, "no line here"
	}
	if an, ok := d.anchorAt(at.side, line); ok {
		d.removeAnchor(an)
		d.align()
		return spot{d.viewOf[d.rowOf[at.side][line]], int(at.side), at.side}, "removed anchor " + anchorName(an)
	}
	if d.pending == nil || d.pending.side == at.side {
		if d.pending != nil && d.pending.line == line {
			d.pending = nil
			return at, "mark cancelled"
		}
		d.pending = &mark{at.side, line}
		at.side = at.side.other()
		return at, fmt.Sprintf("marked %s%d, pick the matching line (esc cancels)",
			sideName(d.pending.side), line+1)
	}

	an := diff.Anchor{Left: d.pending.line, Right: line}
	if at.side == left {
		an = diff.Anchor{Left: line, Right: d.pending.line}
	}
	if conflicts := diff.Conflicts(d.anchors, an); len(conflicts) > 0 {
		names := make([]string, len(conflicts))
		for i, c := range conflicts {
			names[i] = anchorName(c)
		}
		d.replace = &an
		return at, fmt.Sprintf("%s crosses %s: enter replaces, esc cancels",
			anchorName(an), strings.Join(names, ", "))
	}
	return d.addAnchor(an, at.side)
}

// addAnchor adds an, picked last on side s, and returns the cursor on its
// row: the line picked last keeps its place, the rest aligns around it.
func (d *fileDiff) addAnchor(an diff.Anchor, s side) (spot, string) {
	i, _ := slices.BinarySearchFunc(d.anchors, an, func(x, y diff.Anchor) int {
		return x.Left - y.Left
	})
	d.anchors = slices.Insert(d.anchors, i, an)
	d.pending = nil
	d.align()
	return spot{d.viewOf[d.rowOf[left][an.Left]], int(s), s}, "anchored " + anchorName(an)
}

// unifiedSide returns the side of the line under the cursor at in the normal
// layout. An unchanged line belongs to both: it takes the side opposite a
// pending mark, else the old side.
func (d *fileDiff) unifiedSide(at spot) side {
	if at.v >= len(d.view) || d.view[at.v].hidden > 0 {
		return at.side
	}
	switch r := d.rows[d.view[at.v].row]; {
	case r.Kind == diff.Delete:
		return left
	case r.Kind == diff.Insert:
		return right
	case r.Kind == diff.Change:
		return side(min(at.sub, 1))
	case d.pending != nil:
		return d.pending.side.other()
	}
	return left
}

func (d *fileDiff) removeAnchor(an diff.Anchor) {
	d.anchors = slices.DeleteFunc(d.anchors, func(x diff.Anchor) bool { return x == an })
}

func (d *fileDiff) anchorAt(s side, line int) (diff.Anchor, bool) {
	for _, an := range d.anchors {
		if anchorLine(an, s) == line {
			return an, true
		}
	}
	return diff.Anchor{}, false
}

// align computes the rows from the lines and anchors, then the displayed
// rows.
func (d *fileDiff) align() {
	d.rows = diff.Align(d.lines[left], d.lines[right], d.anchors,
		diff.Options{SplitIndent: d.cfg.Settings.OnlyChangedWhitespaces})
	for s := range d.rowOf {
		d.rowOf[s] = make([]int, len(d.lines[s]))
	}
	for i, r := range d.rows {
		if r.Left >= 0 {
			d.rowOf[left][r.Left] = i
		}
		if r.Right >= 0 {
			d.rowOf[right][r.Right] = i
		}
	}
	d.aligns++
	d.buildView()
}

// buildView computes the displayed rows. Unchanged rows further than the
// context from any change, anchor or pending mark form foldable blocks,
// shown as one fold row while closed; a single such row is never a block.
// The view is built anew, so views holding the old one can still read it.
func (d *fileDiff) buildView() {
	keep := make([]bool, len(d.rows))
	ctx := max(d.cfg.Settings.Context, 0)
	for i, r := range d.rows {
		if r.Kind != diff.Equal || r.Anchored || d.isPending(r) {
			for j := max(i-ctx, 0); j <= min(i+ctx, len(d.rows)-1); j++ {
				keep[j] = true
			}
		}
	}

	d.blocks = nil
	d.view = nil
	d.viewOf = make([]int, len(d.rows))
	for i := 0; i < len(d.rows); {
		j := i + 1
		if !keep[i] {
			for j < len(d.rows) && !keep[j] {
				j++
			}
		}
		if j-i < 2 {
			d.viewOf[i] = len(d.view)
			d.view = append(d.view, vrow{row: i, block: -1})
			i = j
			continue
		}
		b := len(d.blocks)
		d.blocks = append(d.blocks, block{i, j})
		open := d.blockOpen(d.blocks[b])
		for k := i; k < j; k++ {
			d.viewOf[k] = len(d.view)
			if open {
				d.view = append(d.view, vrow{row: k, block: b})
			}
		}
		if !open {
			d.view = append(d.view, vrow{row: i, hidden: j - i, block: b})
		}
		i = j
	}
	d.changes++
}

func (d *fileDiff) unified() bool { return !d.cfg.Settings.SideBySide }

// rowLines returns the number of screen lines of displayed row v.
func (d *fileDiff) rowLines(v int, unified bool) int {
	if !unified || d.view[v].hidden > 0 {
		return 1
	}
	if d.rows[d.view[v].row].Kind == diff.Change {
		return 2
	}
	return 1
}

// screenLines returns the screen lines of the displayed rows in the normal
// layout when unified, else side by side, and the first screen line of each
// displayed row.
func (d *fileDiff) screenLines(unified bool) (screen []sline, screenOf []int) {
	screenOf = make([]int, len(d.view))
	for v := range d.view {
		screenOf[v] = len(screen)
		for s := range d.rowLines(v, unified) {
			screen = append(screen, sline{v, s})
		}
	}
	return screen, screenOf
}

// blockOpen reports whether b is unfolded. Blocks start closed when
// collapse_unchanged is set; toggled state is kept by the block's first
// line, so it survives realigning as long as the block starts there.
func (d *fileDiff) blockOpen(b block) bool {
	if open, ok := d.foldOpen[d.rows[b.start].Left]; ok {
		return open
	}
	return !d.cfg.Settings.CollapseUnchanged
}

func (d *fileDiff) isPending(r diff.Row) bool {
	return d.pending != nil && lineOf(r, d.pending.side) == d.pending.line
}

// toggleNearestBlock folds or unfolds the block of unchanged rows under the
// cursor at, or else the one closest to it (the lower one on a tie). The
// cursor stays on its row, or on the fold containing it.
func (d *fileDiff) toggleNearestBlock(at spot) (spot, string) {
	b := d.nearestBlock(at.v)
	if b < 0 {
		return at, "no unchanged code to fold"
	}
	r := d.view[at.v].row
	blk := d.blocks[b]
	d.foldOpen[d.rows[blk.start].Left] = !d.blockOpen(blk)
	d.buildView()
	at.v = d.viewOf[r]
	return at, ""
}

// toggleAllBlocks unfolds all blocks of unchanged rows when one of them is
// folded, and folds them all otherwise. The cursor at stays on its row, or
// on the fold containing it.
func (d *fileDiff) toggleAllBlocks(at spot) (spot, string) {
	if len(d.blocks) == 0 {
		return at, "no unchanged code to fold"
	}
	open := false
	for _, b := range d.blocks {
		if !d.blockOpen(b) {
			open = true
			break
		}
	}
	r := d.view[at.v].row
	for _, b := range d.blocks {
		d.foldOpen[d.rows[b.start].Left] = open
	}
	d.buildView()
	at.v = d.viewOf[r]
	return at, ""
}

// nearestBlock returns the index of the block at or closest to displayed
// row v, or -1 when there is none.
func (d *fileDiff) nearestBlock(v int) int {
	for dist := 0; dist < len(d.view); dist++ {
		for _, w := range []int{v + dist, v - dist} {
			if w >= 0 && w < len(d.view) && d.view[w].block >= 0 {
				return d.view[w].block
			}
		}
	}
	return -1
}

// lineAt returns the line index shown on side s of displayed row v, or -1.
func (d *fileDiff) lineAt(s side, v int) int {
	if v < 0 || v >= len(d.view) || d.view[v].hidden > 0 {
		return -1
	}
	return lineOf(d.rows[d.view[v].row], s)
}

// changeStart reports whether displayed row v starts a block of non-equal
// rows.
func (d *fileDiff) changeStart(v int) bool {
	changed := func(v int) bool {
		return d.view[v].hidden == 0 && d.rows[d.view[v].row].Kind != diff.Equal
	}
	return changed(v) && (v == 0 || !changed(v-1))
}

func sideName(s side) string {
	if s == left {
		return "L"
	}
	return "R"
}

func anchorName(an diff.Anchor) string {
	return fmt.Sprintf("L%d↔R%d", an.Left+1, an.Right+1)
}
