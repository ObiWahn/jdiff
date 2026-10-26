package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/settings"
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

// polling returns the default configuration with inotify off, so that
// files are checked every watch_interval.
func polling() *config.Config {
	cfg := config.Default()
	cfg.Settings.InotifyMaxDirs = 0
	return cfg
}

// newWatchedModel returns the watched diff view of two files with lines a
// and b in a new directory, checked by polling.
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
	m := NewWatched(polling(), f)
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
	m.fileDiff.addAnchor(diff.Anchor{Left: 20, Right: 20}, right, shape{})
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
// a.txt to d.txt, each changed in the new directory, and the new directory;
// the files are checked by polling.
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
	a := NewApp(polling(), files)
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
	if NewLog(config.Default(), "main", nil, false, nil, nil).Init() != nil {
		t.Error("the log starts watching")
	}
}

func TestFlowFollowsReloadedLine(t *testing.T) {
	a, dir := newWatchedApp(t)
	a.cfg.Settings.CollapseUnchanged = false
	pressApp(a, "f", "j", "j", "j", "j") // on a.txt, the row of line 5
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

func TestFlowFollowsLineWhenBothSidesReload(t *testing.T) {
	a, newDir := newWatchedApp(t)
	oldDir := filepath.Dir(a.files[0].OldFile)
	pressApp(a, "f")
	d := a.files[0].diff
	at := d.reveal(left, 9, a.flow.built) // the last unchanged line, x9 on both sides
	a.flow.show(0, 0, at.v, at.sub, 5)
	// In one tick the old side shrinks past the cursor's line and the new
	// side gets a line on top.
	writeLines(t, filepath.Join(oldDir, "a.txt"), numbered("x", 1))
	writeLines(t, filepath.Join(newDir, "a.txt"), append([]string{"new top"}, append(numbered("x", 10), "added")...))
	a.Update(watchTickMsg{})
	_, file, line := a.flow.position()
	if got := d.lineAt(right, line); file != 0 || got != 10 {
		t.Errorf("flow cursor on file %d right line %d, want file 0 line 10: x9 moved down by one", file, got)
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

func TestWatchedModelRefresh(t *testing.T) {
	m, dir := newWatchedModel(t, numbered("x", 5), numbered("x", 5))
	m.cfg.Settings.WatchInterval = 0
	pressRoot(m, "R")
	if m.msg != "nothing changed" {
		t.Errorf("R on unchanged files: msg %q", m.msg)
	}
	writeLines(t, filepath.Join(dir, "new.txt"), numbered("x", 6))
	pressRoot(m, "f5")
	if m.msg != "new.txt reloaded" || len(m.lines[right]) != 6 {
		t.Errorf("F5 after a change: msg %q, %d lines", m.msg, len(m.lines[right]))
	}
	plain := newTestModel(numbered("x", 2), numbered("x", 2))
	pressRoot(plain, "R")
	if plain.msg != "nothing to load again" {
		t.Errorf("R without files: msg %q", plain.msg)
	}
}

// awaitMsg runs cmd, which waits for something, and returns its message,
// failing after two seconds.
func awaitMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("no message")
		return nil
	}
}

// newInotifyApp returns the file selector of newWatchedApp with inotify
// on, as configured by default, started by Init, and the new directory.
func newInotifyApp(t *testing.T) (*App, string, tea.Cmd) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("inotify is Linux only")
	}
	a, dir := newWatchedApp(t)
	a.cfg.Settings.InotifyMaxDirs = settings.Default().InotifyMaxDirs
	cmd := a.Init()
	if a.notify == nil {
		t.Fatalf("not watched by inotify, msg %q", a.msg)
	}
	t.Cleanup(func() { a.notify.Close() })
	return a, dir, cmd
}

func TestAppInotifyReloadsAnyFile(t *testing.T) {
	a, dir, cmd := newInotifyApp(t)
	// d.txt is three files away from the cursor: polling would not see it.
	writeLines(t, filepath.Join(dir, "d.txt"), append(numbered("x", 10), "added", "more", "lines"))
	msg := awaitMsg(t, cmd)
	if _, ok := msg.(changedMsg); !ok {
		t.Fatalf("message %T, want changedMsg", msg)
	}
	if _, cmd = a.Update(msg); cmd == nil {
		t.Error("not waiting for the next changes")
	}
	if a.files[3].added != 3 || a.msg != "d.txt reloaded" {
		t.Errorf("d.txt +%d, msg %q; want +3 and reloaded", a.files[3].added, a.msg)
	}
	// Ticks left from polling neither check nor go on.
	if _, cmd := a.Update(watchTickMsg{}); cmd != nil {
		t.Error("a tick while inotify watches schedules the next one")
	}
	// Changes of a watcher no longer used are dropped.
	old := changedMsg{w: nil, paths: []string{filepath.Join(dir, "a.txt")}}
	if _, cmd := a.Update(old); cmd != nil {
		t.Error("a change of another watcher is waited on")
	}
}

func TestAppInotifyFallsBackToPolling(t *testing.T) {
	a, _ := newWatchedApp(t) // the old and the new directory
	a.cfg.Settings.InotifyMaxDirs = 1
	if a.Init() == nil || a.notify != nil || !a.ticking {
		t.Errorf("two directories, inotify_max_dirs 1: notify %v ticking %v; want polling", a.notify, a.ticking)
	}
	b, _ := newWatchedApp(t)
	b.cfg.Settings.WatchInterval = 0
	if b.Init() != nil || b.notify != nil {
		t.Error("watch_interval 0 watches")
	}
}

func TestAppRelistSwitchesWatching(t *testing.T) {
	a, dir, _ := newInotifyApp(t)
	var list []source.File
	for _, e := range a.files {
		list = append(list, e.File)
	}
	a.WithRelist(func() ([]source.File, error) { return list, nil })

	// Files in a third directory: over the limit of 2, so polling.
	a.cfg.Settings.InotifyMaxDirs = 2
	third := filepath.Join(t.TempDir(), "e.txt")
	writeLines(t, third, []string{"e"})
	list = append(slices.Clone(list), source.File{Path: "e.txt", NewFile: third, New: []string{"e"}, InNew: true})
	old := a.notify
	_, cmd := a.Update(refreshMsg{})
	if a.notify != nil || !a.ticking || cmd == nil {
		t.Fatalf("over the limit after R: notify %v ticking %v; want polling", a.notify, a.ticking)
	}
	if _, ok := <-old.Changes(); ok {
		t.Error("the watcher replaced is still open")
	}
	// Back under the limit: inotify again, and the pending tick ends.
	list = list[:len(list)-1]
	_, cmd = a.Update(refreshMsg{})
	if a.notify == nil {
		t.Fatal("under the limit after R: not watched by inotify")
	}
	t.Cleanup(func() { a.notify.Close() })
	if _, tick := a.Update(watchTickMsg{}); tick != nil {
		t.Error("the tick pending from polling goes on")
	}
	writeLines(t, filepath.Join(dir, "b.txt"), numbered("x", 12))
	if msg := awaitMsg(t, cmd); msg.(changedMsg).paths[0] != filepath.Join(dir, "b.txt") {
		t.Errorf("message %+v, want b.txt changed", msg)
	}
}

func TestModelInotify(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("inotify is Linux only")
	}
	m, dir := newWatchedModel(t, numbered("x", 5), numbered("x", 5))
	m.cfg.Settings.InotifyMaxDirs = 1
	cmd := m.Init()
	if m.notify == nil {
		t.Fatalf("not watched by inotify, msg %q", m.msg)
	}
	t.Cleanup(func() { m.notify.Close() })
	writeLines(t, filepath.Join(dir, "new.txt"), numbered("x", 6))
	if _, cmd = m.Update(awaitMsg(t, cmd)); cmd == nil {
		t.Error("not waiting for the next changes")
	}
	if m.msg != "new.txt reloaded" || len(m.lines[right]) != 6 {
		t.Errorf("msg %q, %d lines; want the reloaded 6", m.msg, len(m.lines[right]))
	}
}

func TestDiffSwitchStopsWatching(t *testing.T) {
	a, _, _ := newInotifyApp(t)
	old := a.notify
	s, _ := newDiffSwitch(a)
	pressRoot(s, "d", "HEAD", "enter")
	if _, ok := <-old.Changes(); ok {
		t.Error("the replaced view still watches")
	}
}

func TestStaleTickDropped(t *testing.T) {
	m, _ := newWatchedModel(t, numbered("x", 2), numbered("x", 2))
	a, _ := newWatchedApp(t)
	for _, v := range []tea.Model{m, a} {
		if _, cmd := v.Update(watchTickMsg{owner: newTestApp()}); cmd != nil {
			t.Errorf("%T: a tick of another view schedules the next one", v)
		}
		if _, cmd := v.Update(watchTickMsg{owner: v}); cmd == nil {
			t.Errorf("%T: its own tick does not schedule the next one", v)
		}
	}
}
