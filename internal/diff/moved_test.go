package diff

import (
	"maps"
	"testing"
)

// input returns the move input of a file changed from a to b.
func input(a, b []string) MoveInput {
	return MoveInput{Old: a, New: b, Rows: Align(a, b, nil, Options{})}
}

var block = []string{
	"func helper(x int) int {",
	"	return x * 2",
	"}",
}

func TestFindMovesWithinFile(t *testing.T) {
	a := append(append([]string{}, block...), "keep one", "keep two", "keep three")
	b := append([]string{"keep one", "keep two", "keep three"}, block...)
	m := FindMoves([]MoveInput{input(a, b)})
	wantFrom := map[int]Ref{0: {0, 3}, 1: {0, 4}, 2: {0, 5}}
	wantTo := map[int]Ref{3: {0, 0}, 4: {0, 1}, 5: {0, 2}}
	if !maps.Equal(m.From[0], wantFrom) || !maps.Equal(m.To[0], wantTo) {
		t.Errorf("moves from %v to %v, want %v and %v", m.From[0], m.To[0], wantFrom, wantTo)
	}
}

func TestFindMovesAcrossFiles(t *testing.T) {
	m := FindMoves([]MoveInput{
		input(append([]string{"package a"}, block...), []string{"package a"}),
		input([]string{"package b"}, append([]string{"package b", ""}, block...)),
	})
	if want := (map[int]Ref{1: {1, 2}, 2: {1, 3}, 3: {1, 4}}); !maps.Equal(m.From[0], want) {
		t.Errorf("from a.go: %v, want %v", m.From[0], want)
	}
	if want := (map[int]Ref{2: {0, 1}, 3: {0, 2}, 4: {0, 3}}); !maps.Equal(m.To[1], want) {
		t.Errorf("to b.go: %v, want %v", m.To[1], want)
	}
	if len(m.To[0]) != 0 || len(m.From[1]) != 0 {
		t.Errorf("unexpected moves: to a.go %v, from b.go %v", m.To[0], m.From[1])
	}
}

func TestFindMovesIgnoresIndentation(t *testing.T) {
	indented := []string{"	" + block[0], "		return x * 2", "	}"}
	m := FindMoves([]MoveInput{
		input(append([]string{"top"}, block...), []string{"top"}),
		input([]string{"type T struct{}"}, append([]string{"type T struct{}"}, indented...)),
	})
	if len(m.From[0]) != 3 || len(m.To[1]) != 3 {
		t.Errorf("re-indented block: from %v, to %v; want 3 lines each", m.From[0], m.To[1])
	}
}

func TestFindMovesSkipsSmallBlocks(t *testing.T) {
	// "}" and "end" move, but are too short to count.
	m := FindMoves([]MoveInput{input(
		[]string{"}", "end", "alpha beta gamma", "delta"},
		[]string{"alpha beta gamma", "delta", "}", "end"},
	)})
	if len(m.From[0]) != 0 {
		t.Errorf("short block marked: %v", m.From[0])
	}
}

func TestFindMovesNotInPlace(t *testing.T) {
	// A line re-indented where it was is a change in its row, not a move.
	m := FindMoves([]MoveInput{input(
		[]string{"start", "x := computeSomethingLong(a, b, c)", "end"},
		[]string{"start", "	x := computeSomethingLong(a, b, c)", "end"},
	)})
	if len(m.From[0]) != 0 || len(m.To[0]) != 0 {
		t.Errorf("change in place counted as move: from %v to %v", m.From[0], m.To[0])
	}
}

func TestFindMovesUsesEachLineOnce(t *testing.T) {
	// The block is removed from two files and added to a third once: only
	// the first removal is its move.
	m := FindMoves([]MoveInput{
		input(append([]string{"package a"}, block...), []string{"package a"}),
		input(append([]string{"package b"}, block...), []string{"package b"}),
		input([]string{"package c"}, append([]string{"package c"}, block...)),
	})
	if len(m.From[0]) != 3 || len(m.From[1]) != 0 || len(m.To[2]) != 3 {
		t.Errorf("from a %v, from b %v, to c %v; want only a's block moved to c", m.From[0], m.From[1], m.To[2])
	}
}

func TestFindMovesLongestBlockFirst(t *testing.T) {
	// The block moves below four lines that stay; its first line also
	// appears alone before it: the whole block wins.
	stay := []string{"stay one", "stay two", "stay three", "stay four"}
	a := append(append([]string{}, block...), stay...)
	b := append(append(append([]string{}, stay...), block[0], "separator line"), block...)
	m := FindMoves([]MoveInput{input(a, b)})
	if got, ok := m.From[0][0]; !ok || got != (Ref{0, 6}) {
		t.Errorf("first line moved to %v (found %v), want {0 6}, the start of the whole block", got, ok)
	}
}
