package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/settings"
	"jdiff/internal/theme"
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
// side a displayed row is one line, or with wrapping as many as its longer
// line needs; in the normal layout a changed row has the lines of its
// removed line, then of its added line, each as many as it needs, which
// screenLines puts into the old and the new half of their block. A fold
// has an empty line above and below, sub gapAbove and gapBelow, but at the
// start and end of the diff; the cursor does not stop on them.
type sline struct{ v, sub int }

const gapAbove, gapBelow = -1, -2

// gap reports whether s is an empty line next to a fold.
func (s sline) gap() bool { return s.sub < 0 }

// block is a foldable run of unchanged rows [start, end).
type block struct{ start, end int }

// spot is a cursor in a diff: on line sub of displayed row v, in the pane of
// side side.
type spot struct {
	v, sub int
	side   side
}

// shape is what the screen lines of a diff depend on: the normal layout
// when unified, the width of the view when wrap is not 0, at which long
// lines are wrapped instead of cut, and with lineEnd the column of the
// symbol at the end of lines (line_end, one column).
type shape struct {
	unified bool
	wrap    int
	lineEnd bool
	cr      bool // the \r of all \r\n line ends is drawn, see crShown
}

// layout is how a view draws a diff: width columns wide, scrolled by xoff
// columns unless lines are wrapped, with the cursor in the pane of side
// active, in shape.
type layout struct {
	width, xoff int
	active      side
	shape
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

	widths [2][]int // the columns each line takes when drawn, per side
	// highlighted is the line drawn last, per side, see content.
	highlighted [2]highlightCache

	markPath   string     // the file's path in marks
	aligns     int        // counts the alignments
	group      *moveGroup // the files searched for moved code with this one
	groupIndex int        // the index of this file in group
}

// highlightKey is what the highlight of a line depends on: the line, its row
// in the alignment, the cursor on it, whether it moved, the text searched
// for, the settings and the theme.
type highlightKey struct {
	line, aligns  int
	cursor, moved bool
	search        config.Query
	settings      settings.Settings
	theme         *theme.Theme
}

type highlightCache struct {
	key highlightKey
	out string
	ok  bool
}

func newFileDiff(cfg *config.Config, names [2]string, lines [2][]string) *fileDiff {
	d := &fileDiff{cfg: cfg, names: names, lines: lines, foldOpen: map[int]bool{}}
	d.measure(left)
	d.measure(right)
	d.align()
	return d
}

// measure computes the widths of the lines of side s, without the \r of a
// \r\n line end: see crShown.
func (d *fileDiff) measure(s side) {
	d.widths[s] = make([]int, len(d.lines[s]))
	for i, l := range d.lines[s] {
		d.widths[s][i] = ansi.StringWidth(printable(strings.TrimSuffix(l, "\r")))
	}
}

// crShown reports whether line of side s ends in a \r that is drawn in sh:
// where it changed, or on all lines with sh.cr.
func (d *fileDiff) crShown(s side, line int, sh shape) bool {
	text := d.lines[s][line]
	if !strings.HasSuffix(text, "\r") {
		return false
	}
	if sh.cr {
		return true
	}
	for _, sp := range emphOf(d.rows[d.rowOf[s][line]], s) {
		if sp.End == len(text) {
			return true
		}
	}
	return false
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
	d.measure(s)
	d.reloads, d.reloadSide, d.moved = d.reloads+1, s, moved
	d.align()
	return dropped
}

// follow returns the displayed row that now shows a line of r, a row before
// the diff changed: its left line, else its right one, through the last
// reload when reloaded is set. It returns -1 when neither line is left.
func (d *fileDiff) follow(r diff.Row, reloaded bool) int {
	if i := d.rowNow(r, reloaded); i >= 0 {
		return d.viewOf[i]
	}
	return -1
}

// rowAfterReload returns the index of the row that now holds a line of r, a
// row before the last reload, as follow does, or -1.
func (d *fileDiff) rowAfterReload(r diff.Row) int { return d.rowNow(r, true) }

// rowNow returns the index of the row that now holds the left line of r,
// else its right one, through the last reload when reloaded is set, or -1.
func (d *fileDiff) rowNow(r diff.Row, reloaded bool) int {
	for _, s := range []side{left, right} {
		line := lineOf(r, s)
		if line >= 0 && reloaded && s == d.reloadSide {
			line = d.moved[line]
		}
		if line >= 0 {
			return d.rowOf[s][line]
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
// the cursor at, in the screen lines of sh. It returns the cursor afterwards
// and the message to show, "" for none.
func (d *fileDiff) act(k string, at spot, sh shape) (spot, string) {
	if d.replace != nil {
		an := *d.replace
		d.replace = nil
		switch k {
		case "enter":
			for _, c := range diff.Conflicts(d.anchors, an) {
				d.removeAnchor(c)
			}
			return d.addAnchor(an, at.side, sh)
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
		return d.enter(at, sh)
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
func (d *fileDiff) enter(at spot, sh shape) (spot, string) {
	if sh.unified {
		at.side = d.unifiedSide(at, sh)
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
		return d.spotOf(at.side, line, sh), "removed anchor " + anchorName(an)
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
	return d.addAnchor(an, at.side, sh)
}

// addAnchor adds an, picked last on side s, and returns the cursor on its
// row in the screen lines of sh: the line picked last keeps its place, the
// rest aligns around it.
func (d *fileDiff) addAnchor(an diff.Anchor, s side, sh shape) (spot, string) {
	i, _ := slices.BinarySearchFunc(d.anchors, an, func(x, y diff.Anchor) int {
		return x.Left - y.Left
	})
	d.anchors = slices.Insert(d.anchors, i, an)
	d.pending = nil
	d.align()
	return d.spotOf(s, anchorLine(an, s), sh), "anchored " + anchorName(an)
}

// unifiedSide returns the side of the line under the cursor at in the normal
// layout with the screen lines of sh. An unchanged line belongs to both: it
// takes the side opposite a pending mark, else the old side.
func (d *fileDiff) unifiedSide(at spot, sh shape) side {
	if at.v >= len(d.view) || d.view[at.v].hidden > 0 {
		return at.side
	}
	switch r := d.rows[d.view[at.v].row]; {
	case r.Kind == diff.Delete:
		return left
	case r.Kind == diff.Insert:
		return right
	case r.Kind == diff.Change:
		s, _ := d.unifiedPiece(at.v, at.sub, sh)
		return s
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

// newShape returns the shape of the screen lines of a view width columns
// wide, as the settings of cfg ask for.
func newShape(cfg *config.Config, width int) shape {
	sh := shape{unified: !cfg.Settings.SideBySide}
	if cfg.Settings.Wrap {
		sh.wrap, sh.lineEnd, sh.cr = width, cfg.Settings.ShowWhitespaces, cfg.Settings.ShowCR
	}
	return sh
}

// contentWidth returns the columns left for the text of a line in a view
// width columns wide, in the normal layout when unified, else side by side.
func (d *fileDiff) contentWidth(width int, unified bool) int {
	nw := d.numWidth()
	if unified {
		return max(width-2*nw-4, 0)
	}
	return max((width-1)/2-nw-3, 0)
}

// pieces returns the number of screen lines line of side s takes in sh: 1,
// or the pieces it is wrapped into. A missing line takes 1.
func (d *fileDiff) pieces(s side, line int, sh shape) int {
	cw := d.contentWidth(sh.wrap, sh.unified)
	if sh.wrap == 0 || line < 0 || cw == 0 {
		return 1
	}
	w := d.widths[s][line]
	if sh.lineEnd {
		w++
	}
	if d.crShown(s, line, sh) {
		w++
	}
	return max((w+cw-1)/cw, 1)
}

// rowLines returns the number of screen lines of displayed row v in sh.
func (d *fileDiff) rowLines(v int, sh shape) int {
	if d.view[v].hidden > 0 {
		return 1
	}
	r := d.rows[d.view[v].row]
	switch {
	case !sh.unified:
		return max(d.pieces(left, r.Left, sh), d.pieces(right, r.Right, sh))
	case r.Kind == diff.Change:
		return d.pieces(left, r.Left, sh) + d.pieces(right, r.Right, sh)
	}
	s, _ := d.unifiedPiece(v, 0, sh)
	return d.pieces(s, lineOf(r, s), sh)
}

// unifiedPiece returns the side whose line screen line sub of displayed row
// v shows in the normal layout with sh, and which piece of it.
func (d *fileDiff) unifiedPiece(v, sub int, sh shape) (side, int) {
	if d.view[v].hidden > 0 {
		return right, 0
	}
	switch r := d.rows[d.view[v].row]; r.Kind {
	case diff.Delete:
		return left, sub
	case diff.Change:
		if n := d.pieces(left, r.Left, sh); sub >= n {
			return right, sub - n
		}
		return left, sub
	}
	return right, sub
}

// firstSub returns the screen line of displayed row v that starts the line
// of side s in sh. Side by side without wrapping it is the one of the normal
// layout: the view clamps it to the row's only line, and it keeps the side
// for switching back.
func (d *fileDiff) firstSub(v int, s side, sh shape) int {
	if !sh.unified && sh.wrap > 0 {
		return 0
	}
	r := d.rows[d.view[v].row]
	if d.view[v].hidden > 0 || r.Kind != diff.Change || s == left {
		return 0
	}
	return d.pieces(left, r.Left, sh)
}

// convertSub returns the screen line of displayed row v in sh that shows
// what screen line sub in from shows: the same piece of the same side's
// line, as far as it has one. Side by side with wrapping, active is the side
// of the cursor. Without wrapping in either shape, sub is kept as it is, as
// it was before lines could wrap.
func (d *fileDiff) convertSub(v, sub int, from, sh shape, active side) int {
	if from.wrap == 0 && sh.wrap == 0 || d.view[v].hidden > 0 {
		return sub
	}
	s, piece := active, sub
	switch {
	case from.unified:
		s, piece = d.unifiedPiece(v, sub, from)
	case from.wrap == 0:
		s, piece = d.unifiedPiece(v, sub, shape{unified: true})
	}
	if !sh.unified && sh.wrap == 0 {
		return d.firstSub(v, s, sh)
	}
	r := d.rows[d.view[v].row]
	if !sh.unified {
		return min(piece, d.rowLines(v, sh)-1)
	}
	if lineOf(r, s) < 0 || (r.Kind != diff.Change && r.Kind != diff.Equal) {
		s, _ = d.unifiedPiece(v, 0, sh) // the row shows only one side
	}
	return d.firstSub(v, s, sh) + min(piece, d.pieces(s, lineOf(r, s), sh)-1)
}

// screenLines returns the screen lines of the displayed rows in sh and the
// index of each of them. In the normal layout a block of changed rows shows
// all its old lines, then all its new ones, as git diff does; so the two
// lines of a changed row are not next to each other. Folds have empty lines
// around them.
func (d *fileDiff) screenLines(sh shape) (screen []sline, at map[sline]int) {
	at = make(map[sline]int, len(d.view))
	add := func(v, sub int) {
		at[sline{v, sub}] = len(screen)
		screen = append(screen, sline{v, sub})
	}
	for v := 0; v < len(d.view); {
		if fold := d.view[v].hidden > 0; fold || !sh.unified || !d.changed(v) {
			if fold && v > 0 {
				add(v, gapAbove)
			}
			for sub := range d.rowLines(v, sh) {
				add(v, sub)
			}
			if fold && v < len(d.view)-1 {
				add(v, gapBelow)
			}
			v++
			continue
		}
		end := v
		for end < len(d.view) && d.changed(end) {
			end++
		}
		for _, s := range []side{left, right} {
			for w := v; w < end; w++ {
				for sub := range d.rowLines(w, sh) {
					if of, _ := d.unifiedPiece(w, sub, sh); of == s {
						add(w, sub)
					}
				}
			}
		}
		v = end
	}
	return screen, at
}

// blockStart reports whether screen line i of screen starts a block of
// changed rows.
func (d *fileDiff) blockStart(screen []sline, i int) bool {
	return d.changed(screen[i].v) && (i == 0 || !d.changed(screen[i-1].v))
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

// changed reports whether displayed row v is a changed row, not unchanged or
// a fold.
func (d *fileDiff) changed(v int) bool {
	return d.view[v].hidden == 0 && d.rows[d.view[v].row].Kind != diff.Equal
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
