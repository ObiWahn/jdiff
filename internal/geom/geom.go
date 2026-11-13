// Package geom holds the sizes and positions the other packages pass: a
// Size is how large something is, a Pos where it stands; kept apart so that
// one is never passed for the other.
package geom

// Size is a width and a height: of a screen or a box, or of an area in it.
type Size struct{ W, H int }

// Pos is a position: the column X and the row Y, from the top left (0, 0).
type Pos struct{ X, Y int }
