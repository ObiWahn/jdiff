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

// emphOf returns the emphasized parts of the line of side s in row i, none
// unless it is a Change row. They are computed when the row is first drawn
// and kept until the next alignment: computing them for all rows when a
// large change is loaded took seconds.
func (a *fileAlign) emphOf(i int, s side) []diff.Span {
	r := a.rows[i]
	if r.Kind != diff.Change {
		return nil
	}
	e, ok := a.emph[i]
	if !ok {
		e[left], e[right] = diff.Intraline(a.lines[left][r.Left], a.lines[right][r.Right],
			a.cfg.Settings.OnlyChangedWhitespaces)
		a.emph[i] = e
	}
	return e[s]
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
// screenLines puts into the old and the new half of their block.
type sline struct{ v, sub int }

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

// fileAlign is the diff of one file as the user anchored and folded it. The
// single file view, the flow view and the preview all show the same
// fileAlign, each with its own cursor.
type fileAlign struct {
	cfg   *config.Config
	names [2]string
	lines [2][]string

	anchors []diff.Anchor // sorted by Left, never crossing
	rows    []diff.Row
	rowOf   [2][]int               // line index -> row index, per side
	emph    map[int][2][]diff.Span // emphasized parts by row, see emphOf

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

	widths [2][]int // the columns each line takes when drawn, per side, -1 until measured
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

func newFileDiff(cfg *config.Config, names [2]string, lines [2][]string) *fileAlign {
	a := &fileAlign{cfg: cfg, names: names, lines: lines, foldOpen: map[int]bool{}}
	a.measure(left)
	a.measure(right)
	a.align()
	return a
}

// measure forgets the widths of the lines of side s, after its lines were
// set: see width.
func (a *fileAlign) measure(s side) {
	a.widths[s] = make([]int, len(a.lines[s]))
	for i := range a.widths[s] {
		a.widths[s][i] = -1
	}
}

// width returns the columns line of side s takes when drawn, without the \r
// of a \r\n line end (see crShown). It is measured on first use: only
// wrapping needs it, and measuring all lines took most of the time loading a
// large change takes.
func (a *fileAlign) width(s side, line int) int {
	if w := a.widths[s][line]; w >= 0 {
		return w
	}
	w := ansi.StringWidth(printable(strings.TrimSuffix(a.lines[s][line], "\r")))
	a.widths[s][line] = w
	return w
}

// crShown reports whether line of side s ends in a \r that is drawn in sh:
// where it changed, or on all lines with sh.cr.
func (a *fileAlign) crShown(s side, line int, sh shape) bool {
	text := a.lines[s][line]
	if !strings.HasSuffix(text, "\r") {
		return false
	}
	if sh.cr {
		return true
	}
	for _, sp := range a.emphOf(a.rowOf[s][line], s) {
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
func (a *fileAlign) reload(s side, lines []string) (dropped int) {
	moved := lineMap(a.lines[s], lines)
	var anchors []diff.Anchor
	for _, an := range a.anchors {
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
	a.anchors = anchors
	if s == left {
		open := map[int]bool{}
		for line, o := range a.foldOpen {
			if n := moved[line]; n >= 0 {
				open[n] = o
			}
		}
		a.foldOpen = open
	}
	a.pending, a.replace = nil, nil
	a.lines[s] = lines
	a.measure(s)
	a.reloads, a.reloadSide, a.moved = a.reloads+1, s, moved
	a.align()
	return dropped
}

// follow returns the displayed row that now shows a line of r, a row before
// the diff changed: its left line, else its right one, through the last
// reload when reloaded is set. It returns -1 when neither line is left.
func (a *fileAlign) follow(r diff.Row, reloaded bool) int {
	if i := a.rowNow(r, reloaded); i >= 0 {
		return a.viewOf[i]
	}
	return -1
}

// rowAfterReload returns the index of the row that now holds a line of r, a
// row before the last reload, as follow does, or -1.
func (a *fileAlign) rowAfterReload(r diff.Row) int { return a.rowNow(r, true) }

// rowNow returns the index of the row that now holds the left line of r,
// else its right one, through the last reload when reloaded is set, or -1.
func (a *fileAlign) rowNow(r diff.Row, reloaded bool) int {
	for _, s := range []side{left, right} {
		line := lineOf(r, s)
		if line >= 0 && reloaded && s == a.reloadSide {
			line = a.moved[line]
		}
		if line >= 0 {
			return a.rowOf[s][line]
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
	for _, r := range diff.Align(old, new, nil) {
		if r.Kind == diff.Equal {
			moved[r.Left] = r.Right
		}
	}
	return moved
}

// act runs the anchor and fold action (anchor, cancel_mark, fold or
// fold_all) at the cursor at, in the screen lines of sh. It returns the cursor
// afterwards and the message to show, "" for none.
func (a *fileAlign) act(action string, at spot, sh shape) (spot, string) {
	if a.replace != nil {
		an := *a.replace
		a.replace = nil
		switch action {
		case "anchor":
			for _, c := range diff.Conflicts(a.anchors, an) {
				a.removeAnchor(c)
			}
			return a.addAnchor(an, at.side, sh)
		case "cancel_mark":
			return at, "replace cancelled"
		}
	}
	switch action {
	case "cancel_mark":
		if a.pending != nil {
			a.pending = nil
			return at, "mark cancelled"
		}
	case "anchor":
		return a.enter(at, sh)
	case "fold":
		return a.toggleNearestBlock(at)
	case "fold_all":
		return a.toggleAllBlocks(at)
	}
	return at, ""
}

// markPrompt returns what to do with the pending mark, "" without one: the
// status lines show it until another message, while the mark waits.
func (a *fileAlign) markPrompt() string {
	if a.pending == nil {
		return ""
	}
	return fmt.Sprintf("marked %s%d, pick the matching line (esc cancels)", sideName(a.pending.side), a.pending.line+1)
}

// enter implements the anchor workflow: mark a line, then pick its partner
// in the other pane. Enter on an anchored line removes that anchor. In the
// normal layout the line under the cursor decides the side.
func (a *fileAlign) enter(at spot, sh shape) (spot, string) {
	if sh.unified {
		at.side = a.unifiedSide(at, sh)
	}
	line := a.lineAt(at.side, at.v)
	if line < 0 {
		if at.v < len(a.view) && a.view[at.v].hidden > 0 {
			return at, "unchanged lines folded, space expands"
		}
		return at, "no line here"
	}
	if an, ok := a.anchorAt(at.side, line); ok {
		a.removeAnchor(an)
		a.align()
		return a.spotOf(at.side, line, sh), "removed anchor " + anchorName(an)
	}
	if a.pending == nil || a.pending.side == at.side {
		if a.pending != nil && a.pending.line == line {
			a.pending = nil
			return at, "mark cancelled"
		}
		a.pending = &mark{at.side, line}
		at.side = at.side.other()
		return at, a.markPrompt()
	}

	an := diff.Anchor{Left: a.pending.line, Right: line}
	if at.side == left {
		an = diff.Anchor{Left: line, Right: a.pending.line}
	}
	if conflicts := diff.Conflicts(a.anchors, an); len(conflicts) > 0 {
		names := make([]string, len(conflicts))
		for i, c := range conflicts {
			names[i] = anchorName(c)
		}
		a.replace = &an
		return at, fmt.Sprintf("%s crosses %s: enter replaces, esc cancels",
			anchorName(an), strings.Join(names, ", "))
	}
	return a.addAnchor(an, at.side, sh)
}

// addAnchor adds an, picked last on side s, and returns the cursor on its
// row in the screen lines of sh: the line picked last keeps its place, the
// rest aligns around it.
func (a *fileAlign) addAnchor(an diff.Anchor, s side, sh shape) (spot, string) {
	i, _ := slices.BinarySearchFunc(a.anchors, an, func(x, y diff.Anchor) int {
		return x.Left - y.Left
	})
	a.anchors = slices.Insert(a.anchors, i, an)
	a.pending = nil
	a.align()
	return a.spotOf(s, anchorLine(an, s), sh), "anchored " + anchorName(an)
}

// unifiedSide returns the side of the line under the cursor at in the normal
// layout with the screen lines of sh. An unchanged line belongs to both: it
// takes the side opposite a pending mark, else the old side.
func (a *fileAlign) unifiedSide(at spot, sh shape) side {
	if at.v >= len(a.view) || a.view[at.v].hidden > 0 {
		return at.side
	}
	switch r := a.rows[a.view[at.v].row]; {
	case r.Kind == diff.Delete:
		return left
	case r.Kind == diff.Insert:
		return right
	case r.Kind == diff.Change:
		s, _ := a.unifiedPiece(at.v, at.sub, sh)
		return s
	case a.pending != nil:
		return a.pending.side.other()
	}
	return left
}

func (a *fileAlign) removeAnchor(an diff.Anchor) {
	a.anchors = slices.DeleteFunc(a.anchors, func(x diff.Anchor) bool { return x == an })
}

func (a *fileAlign) anchorAt(s side, line int) (diff.Anchor, bool) {
	for _, an := range a.anchors {
		if anchorLine(an, s) == line {
			return an, true
		}
	}
	return diff.Anchor{}, false
}

// align computes the rows from the lines and anchors, then the displayed
// rows.
func (a *fileAlign) align() {
	a.rows = diff.Align(a.lines[left], a.lines[right], a.anchors)
	a.emph = map[int][2][]diff.Span{}
	for s := range a.rowOf {
		a.rowOf[s] = make([]int, len(a.lines[s]))
	}
	for i, r := range a.rows {
		if r.Left >= 0 {
			a.rowOf[left][r.Left] = i
		}
		if r.Right >= 0 {
			a.rowOf[right][r.Right] = i
		}
	}
	a.aligns++
	a.buildView()
}

// buildView computes the displayed rows. Unchanged rows further than the
// context from any change, anchor or pending mark form foldable blocks,
// shown as one fold row while closed; a single such row is never a block.
// The view is built anew, so views holding the old one can still read it.
func (a *fileAlign) buildView() {
	keep := make([]bool, len(a.rows))
	ctx := max(a.cfg.Settings.Context, 0)
	for i, r := range a.rows {
		if r.Kind != diff.Equal || r.Anchored || a.isPending(r) {
			for j := max(i-ctx, 0); j <= min(i+ctx, len(a.rows)-1); j++ {
				keep[j] = true
			}
		}
	}

	a.blocks = nil
	a.view = nil
	a.viewOf = make([]int, len(a.rows))
	for i := 0; i < len(a.rows); {
		j := i + 1
		if !keep[i] {
			for j < len(a.rows) && !keep[j] {
				j++
			}
		}
		if j-i < 2 {
			a.viewOf[i] = len(a.view)
			a.view = append(a.view, vrow{row: i, block: -1})
			i = j
			continue
		}
		b := len(a.blocks)
		a.blocks = append(a.blocks, block{i, j})
		open := a.blockOpen(a.blocks[b])
		for k := i; k < j; k++ {
			a.viewOf[k] = len(a.view)
			if open {
				a.view = append(a.view, vrow{row: k, block: b})
			}
		}
		if !open {
			a.view = append(a.view, vrow{row: i, hidden: j - i, block: b})
		}
		i = j
	}
	a.changes++
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
func (a *fileAlign) contentWidth(width int, unified bool) int {
	nw := a.numWidth()
	if unified {
		return max(width-2*nw-4, 0)
	}
	return max((width-1)/2-nw-3, 0)
}

// pieces returns the number of screen lines line of side s takes in sh: 1,
// or the pieces it is wrapped into. A missing line takes 1.
func (a *fileAlign) pieces(s side, line int, sh shape) int {
	cw := a.contentWidth(sh.wrap, sh.unified)
	if sh.wrap == 0 || line < 0 || cw == 0 {
		return 1
	}
	w := a.width(s, line)
	if sh.lineEnd {
		w++
	}
	if a.crShown(s, line, sh) {
		w++
	}
	return max((w+cw-1)/cw, 1)
}

// rowLines returns the number of screen lines of displayed row v in sh.
func (a *fileAlign) rowLines(v int, sh shape) int {
	if a.view[v].hidden > 0 {
		return 1
	}
	r := a.rows[a.view[v].row]
	switch {
	case !sh.unified:
		return max(a.pieces(left, r.Left, sh), a.pieces(right, r.Right, sh))
	case r.Kind == diff.Change:
		return a.pieces(left, r.Left, sh) + a.pieces(right, r.Right, sh)
	}
	s, _ := a.unifiedPiece(v, 0, sh)
	return a.pieces(s, lineOf(r, s), sh)
}

// unifiedPiece returns the side whose line screen line sub of displayed row
// v shows in the normal layout with sh, and which piece of it.
func (a *fileAlign) unifiedPiece(v, sub int, sh shape) (side, int) {
	if a.view[v].hidden > 0 {
		return right, 0
	}
	switch r := a.rows[a.view[v].row]; r.Kind {
	case diff.Delete:
		return left, sub
	case diff.Change:
		if n := a.pieces(left, r.Left, sh); sub >= n {
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
func (a *fileAlign) firstSub(v int, s side, sh shape) int {
	if !sh.unified && sh.wrap > 0 {
		return 0
	}
	r := a.rows[a.view[v].row]
	if a.view[v].hidden > 0 || r.Kind != diff.Change || s == left {
		return 0
	}
	return a.pieces(left, r.Left, sh)
}

// convertSub returns the screen line of displayed row v in sh that shows
// what screen line sub in from shows: the same piece of the same side's
// line, as far as it has one. Side by side with wrapping, active is the side
// of the cursor. Without wrapping in either shape, sub is kept as it is, as
// it was before lines could wrap.
func (a *fileAlign) convertSub(v, sub int, from, sh shape, active side) int {
	if from.wrap == 0 && sh.wrap == 0 || a.view[v].hidden > 0 {
		return sub
	}
	s, piece := active, sub
	switch {
	case from.unified:
		s, piece = a.unifiedPiece(v, sub, from)
	case from.wrap == 0:
		s, piece = a.unifiedPiece(v, sub, shape{unified: true})
	}
	if !sh.unified && sh.wrap == 0 {
		return a.firstSub(v, s, sh)
	}
	r := a.rows[a.view[v].row]
	if !sh.unified {
		return min(piece, a.rowLines(v, sh)-1)
	}
	if lineOf(r, s) < 0 || (r.Kind != diff.Change && r.Kind != diff.Equal) {
		s, _ = a.unifiedPiece(v, 0, sh) // the row shows only one side
	}
	return a.firstSub(v, s, sh) + min(piece, a.pieces(s, lineOf(r, s), sh)-1)
}

// screenLines returns the screen lines of the displayed rows in sh and the
// index of each of them. In the normal layout a block of changed rows shows
// all its old lines, then all its new ones, as git diff does; so the two
// lines of a changed row are not next to each other.
func (a *fileAlign) screenLines(sh shape) (screen []sline, at map[sline]int) {
	at = make(map[sline]int, len(a.view))
	add := func(v, sub int) {
		at[sline{v, sub}] = len(screen)
		screen = append(screen, sline{v, sub})
	}
	for v := 0; v < len(a.view); {
		if !sh.unified || !a.changed(v) {
			for sub := range a.rowLines(v, sh) {
				add(v, sub)
			}
			v++
			continue
		}
		end := v
		for end < len(a.view) && a.changed(end) {
			end++
		}
		for _, s := range []side{left, right} {
			for w := v; w < end; w++ {
				for sub := range a.rowLines(w, sh) {
					if of, _ := a.unifiedPiece(w, sub, sh); of == s {
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
func (a *fileAlign) blockStart(screen []sline, i int) bool {
	return a.changed(screen[i].v) && (i == 0 || !a.changed(screen[i-1].v))
}

// blockOpen reports whether b is unfolded. Blocks start closed when
// collapse_unchanged is set; toggled state is kept by the block's first
// line, so it survives realigning as long as the block starts there.
func (a *fileAlign) blockOpen(b block) bool {
	if open, ok := a.foldOpen[a.rows[b.start].Left]; ok {
		return open
	}
	return !a.cfg.Settings.CollapseUnchanged
}

func (a *fileAlign) isPending(r diff.Row) bool {
	return a.pending != nil && lineOf(r, a.pending.side) == a.pending.line
}

// toggleNearestBlock folds or unfolds the block of unchanged rows under the
// cursor at, or else the one closest to it (the lower one on a tie). The
// cursor stays on its row, or on the fold containing it.
func (a *fileAlign) toggleNearestBlock(at spot) (spot, string) {
	b := a.nearestBlock(at.v)
	if b < 0 {
		return at, "no unchanged code to fold"
	}
	r := a.view[at.v].row
	blk := a.blocks[b]
	a.foldOpen[a.rows[blk.start].Left] = !a.blockOpen(blk)
	a.buildView()
	at.v = a.viewOf[r]
	return at, ""
}

// toggleAllBlocks unfolds all blocks of unchanged rows when one of them is
// folded, and folds them all otherwise. The cursor at stays on its row, or
// on the fold containing it.
func (a *fileAlign) toggleAllBlocks(at spot) (spot, string) {
	if len(a.blocks) == 0 {
		return at, "no unchanged code to fold"
	}
	open := false
	for _, b := range a.blocks {
		if !a.blockOpen(b) {
			open = true
			break
		}
	}
	r := a.view[at.v].row
	for _, b := range a.blocks {
		a.foldOpen[a.rows[b.start].Left] = open
	}
	a.buildView()
	at.v = a.viewOf[r]
	return at, ""
}

// nearestBlock returns the index of the block at or closest to displayed
// row v, or -1 when there is none.
func (a *fileAlign) nearestBlock(v int) int {
	for dist := 0; dist < len(a.view); dist++ {
		for _, w := range []int{v + dist, v - dist} {
			if w >= 0 && w < len(a.view) && a.view[w].block >= 0 {
				return a.view[w].block
			}
		}
	}
	return -1
}

// lineAt returns the line index shown on side s of displayed row v, or -1.
func (a *fileAlign) lineAt(s side, v int) int {
	if v < 0 || v >= len(a.view) || a.view[v].hidden > 0 {
		return -1
	}
	return lineOf(a.rows[a.view[v].row], s)
}

// changed reports whether displayed row v is a changed row, not unchanged or
// a fold.
func (a *fileAlign) changed(v int) bool {
	return a.view[v].hidden == 0 && a.rows[a.view[v].row].Kind != diff.Equal
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
