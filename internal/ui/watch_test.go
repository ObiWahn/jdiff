package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/source"
)

// writeLines writes lines to path, one per line.
func writeLines(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newWatchedModel returns the watched diff view of two files with lines a
// and b in a new directory.
func newWatchedModel(t *testing.T, a, b []string) (*Model, string) {
	t.Helper()
	dir := t.TempDir()
	f := source.File{Path: "new.txt", OldFile: filepath.Join(dir, "old.txt"), NewFile: filepath.Join(dir, "new.txt"),
		InOld: true, InNew: true}
	writeLines(t, f.OldFile, a)
	writeLines(t, f.NewFile, b)
	var err error
	if f.Old, f.OldStamp, err = source.Read(f.OldFile); err != nil {
		t.Fatal(err)
	}
	if f.New, f.NewStamp, err = source.Read(f.NewFile); err != nil {
		t.Fatal(err)
	}
	m := NewWatched(config.Default(), f)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m, dir
}

func TestWatchedModelReloads(t *testing.T) {
	m, dir := newWatchedModel(t, numbered("x", 30), numbered("x", 30))
	if m.Init() == nil {
		t.Fatal("Init does not start watching")
	}
	m.cfg.Settings.CollapseUnchanged = false
	m.realign()
	m.fileDiff.addAnchor(diff.Anchor{Left: 20, Right: 20}, right)
	m.realign()
	moveTo(t, m, right, 25)

	// Unchanged files: no reload, but the next check is scheduled.
	if _, cmd := m.Update(watchTickMsg{}); cmd == nil || m.reloads != 0 {
		t.Fatalf("tick without changes: cmd %v, %d reloads", cmd, m.reloads)
	}
	writeLines(t, filepath.Join(dir, "new.txt"), append([]string{"top1", "top2"}, numbered("x", 30)...))
	if _, cmd := m.Update(watchTickMsg{}); cmd == nil {
		t.Error("no next check after a reload")
	}
	if len(m.lines[right]) != 32 || m.msg != "new.txt reloaded" {
		t.Fatalf("after the change: %d lines, msg %q; want 32 and the reload message", len(m.lines[right]), m.msg)
	}
	if want := []diff.Anchor{{Left: 20, Right: 22}}; !slices.Equal(m.anchors, want) {
		t.Errorf("anchors %v, want %v: moved with its line", m.anchors, want)
	}
	if line := m.lineAt(right, m.cursor); line != 27 {
		t.Errorf("cursor on right line %d, want 27, its line moved by 2", line)
	}

	// The anchored line changes: the anchor is dropped and counted.
	lines := slices.Clone(m.lines[right])
	lines[22] = "changed and longer"
	writeLines(t, filepath.Join(dir, "new.txt"), lines)
	m.Update(watchTickMsg{})
	if len(m.anchors) != 0 || m.msg != "new.txt reloaded, 1 anchor dropped" {
		t.Errorf("anchored line changed: anchors %v, msg %q", m.anchors, m.msg)
	}
}

func TestWatchKeepsContentOnBinaryOrMissing(t *testing.T) {
	m, dir := newWatchedModel(t, []string{"a"}, []string{"b"})
	path := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(path, []byte("bin\x00ary"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Update(watchTickMsg{})
	if !slices.Equal(m.lines[right], []string{"b"}) || m.msg != "new.txt is binary now, not reloaded" {
		t.Errorf("binary: lines %q, msg %q; want the old content and a message", m.lines[right], m.msg)
	}
	// A file being saved by rename is gone for a moment: checked again later.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m.msg = ""
	m.Update(watchTickMsg{})
	if !slices.Equal(m.lines[right], []string{"b"}) || m.msg != "" {
		t.Errorf("missing file: lines %q, msg %q; want no change", m.lines[right], m.msg)
	}
	writeLines(t, path, []string{"back"})
	m.Update(watchTickMsg{})
	if !slices.Equal(m.lines[right], []string{"back"}) {
		t.Errorf("file back: lines %q, want it reloaded", m.lines[right])
	}
}

func TestWatchOff(t *testing.T) {
	m, dir := newWatchedModel(t, []string{"a"}, []string{"b"})
	m.cfg.Settings.WatchInterval = 0
	if m.Init() != nil {
		t.Error("Init starts watching with watch_interval 0")
	}
	writeLines(t, filepath.Join(dir, "new.txt"), []string{"changed"})
	if _, cmd := m.Update(watchTickMsg{}); cmd != nil {
		t.Error("a tick with watch_interval 0 schedules the next one")
	}
	if New(config.Default(), "a", "b", nil, nil).Init() != nil {
		t.Error("a model without files on disk starts watching")
	}
}

// newWatchedApp returns the file selector of directories with the files
// a.txt to d.txt, each changed in the new directory, and the new directory.
func newWatchedApp(t *testing.T) (*App, string) {
	t.Helper()
	oldDir, newDir := t.TempDir(), t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		writeLines(t, filepath.Join(oldDir, name), numbered("x", 10))
		writeLines(t, filepath.Join(newDir, name), append(numbered("x", 10), "added"))
	}
	files, err := source.LoadDirs(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp(config.Default(), files)
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	return a, newDir
}

func TestAppReloadsCurrentFileAndNeighbors(t *testing.T) {
	a, dir := newWatchedApp(t)
	if a.Init() == nil {
		t.Fatal("Init does not start watching")
	}
	pressApp(a, "j") // on b.txt
	for _, name := range []string{"a.txt", "c.txt", "d.txt"} {
		writeLines(t, filepath.Join(dir, name), append(numbered("x", 10), "added", "more", "lines"))
	}
	if _, cmd := a.Update(watchTickMsg{}); cmd == nil {
		t.Error("no next check")
	}
	added := func(i int) int { return a.files[i].added }
	if added(0) != 3 || added(2) != 3 || added(3) != 1 {
		t.Errorf("added lines a %d c %d d %d; want 3, 3 and d, two away from the cursor, not reloaded",
			added(0), added(2), added(3))
	}
	if a.msg != "a.txt reloaded; c.txt reloaded" {
		t.Errorf("msg %q", a.msg)
	}
	// The open diff view shows the new content.
	pressApp(a, "k", "enter")
	if m := a.open.view; len(m.lines[right]) != 13 {
		t.Errorf("opened a.txt: %d lines, want the reloaded 13", len(m.lines[right]))
	}
	if NewLog(config.Default(), "main", nil, nil).Init() != nil {
		t.Error("the log starts watching")
	}
}

func TestFlowFollowsReloadedLine(t *testing.T) {
	a, dir := newWatchedApp(t)
	a.cfg.Settings.CollapseUnchanged = false
	pressApp(a, "f", "j", "j", "j", "j", "j") // on a.txt, the row of line 5
	_, file, line := a.flow.position()
	d := a.files[file].diff
	before := d.lineAt(right, line)
	writeLines(t, filepath.Join(dir, "a.txt"), append([]string{"new top"}, append(numbered("x", 10), "added")...))
	a.Update(watchTickMsg{})
	_, file, line = a.flow.position()
	if got := d.lineAt(right, line); file != 0 || got != before+1 {
		t.Errorf("flow cursor on file %d right line %d, want file 0 line %d: its line moved by one", file, got, before+1)
	}
	if a.flow.msg != "a.txt reloaded" {
		t.Errorf("flow msg %q", a.flow.msg)
	}
}

func TestWatchSeesQuickSameSizeChange(t *testing.T) {
	m, dir := newWatchedModel(t, []string{"aaa"}, []string{"bbb"})
	path := filepath.Join(dir, "new.txt")
	// Rewritten at once with the same size: the modification time may not
	// have changed yet, the content has.
	writeLines(t, path, []string{"ccc"})
	m.Update(watchTickMsg{})
	if !slices.Equal(m.lines[right], []string{"ccc"}) {
		t.Errorf("same size change within the timestamp resolution: lines %q, want it reloaded", m.lines[right])
	}
	// The same content written again is no change.
	m.msg = ""
	writeLines(t, path, []string{"ccc"})
	m.Update(watchTickMsg{})
	if m.msg != "" || m.reloads != 1 {
		t.Errorf("unchanged content: msg %q, %d reloads; want no reload", m.msg, m.reloads)
	}
}
