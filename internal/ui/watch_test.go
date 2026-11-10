package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jdiff/internal/config"
	"jdiff/internal/diff"
	"jdiff/internal/settings"
	"jdiff/internal/source"
	"jdiff/watch"
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

// polling returns the default configuration with auto reload on, which
// watches the files, and inotify off, so that files are checked every
// watch_interval.
func polling() *config.Config {
	cfg := config.Default()
	cfg.Settings.InotifyMaxDirs = 0
	cfg.AutoReload = true
	return cfg
}

// tickOf returns the regular check of auto reload that s waits for.
func tickOf(s *DiffSwitch) autoTickMsg { return autoTickMsg{s.autoGen, s.tick} }

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
	m.fileAlign.addAnchor(diff.Anchor{Left: 20, Right: 20}, right, shape{})
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

// newWatchedDiff returns the file list of directories with the files
// a.txt to d.txt, each changed in the new directory, and the new directory;
// the files are checked by polling.
func newWatchedDiff(t *testing.T) (*Diff, string) {
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
	d := NewDiff(polling(), files)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	return d, newDir
}

func TestDiffReloadsCurrentFileAndNeighbors(t *testing.T) {
	d, dir := newWatchedDiff(t)
	if d.Init() == nil {
		t.Fatal("Init does not start watching")
	}
	pressDiff(d, "j") // on b.txt
	for _, name := range []string{"a.txt", "c.txt", "d.txt"} {
		writeLines(t, filepath.Join(dir, name), append(numbered("x", 10), "added", "more", "lines"))
	}
	if _, cmd := d.Update(watchTickMsg{}); cmd == nil {
		t.Error("no next check")
	}
	added := func(i int) int { return d.files[i].added }
	if added(0) != 3 || added(2) != 3 || added(3) != 1 {
		t.Errorf("added lines a %d c %d d %d; want 3, 3 and d, two away from the cursor, not reloaded",
			added(0), added(2), added(3))
	}
	if d.msg != "a.txt reloaded; c.txt reloaded" {
		t.Errorf("msg %q", d.msg)
	}
	// The open diff view shows the new content.
	pressDiff(d, "k", "enter")
	if m := d.open.view; len(m.lines[right]) != 13 {
		t.Errorf("opened a.txt: %d lines, want the reloaded 13", len(m.lines[right]))
	}
	if NewLog(config.Default(), "main", nil, false, nil, nil).Init() != nil {
		t.Error("the log starts watching")
	}
}

func TestFlowFollowsReloadedLine(t *testing.T) {
	d, dir := newWatchedDiff(t)
	d.cfg.Settings.CollapseUnchanged = false
	pressDiff(d, "f", "j", "j", "j", "j", "j") // on a.txt, the row of line 5
	_, file, line := d.flow.position()
	a := d.files[file].align
	before := a.lineAt(right, line)
	writeLines(t, filepath.Join(dir, "a.txt"), append([]string{"new top"}, append(numbered("x", 10), "added")...))
	d.Update(watchTickMsg{})
	_, file, line = d.flow.position()
	if got := a.lineAt(right, line); file != 0 || got != before+1 {
		t.Errorf("flow cursor on file %d right line %d, want file 0 line %d: its line moved by one", file, got, before+1)
	}
	if d.flow.msg != "a.txt reloaded" {
		t.Errorf("flow msg %q", d.flow.msg)
	}
}

func TestFlowFollowsLineWhenBothSidesReload(t *testing.T) {
	d, newDir := newWatchedDiff(t)
	oldDir := filepath.Dir(d.files[0].OldFile)
	pressDiff(d, "f")
	a := d.files[0].align
	at := a.reveal(left, 9, d.flow.built) // the last unchanged line, x9 on both sides
	d.flow.show(0, 0, at.v, at.sub, 5)
	// In one tick the old side shrinks past the cursor's line and the new
	// side gets a line on top.
	writeLines(t, filepath.Join(oldDir, "a.txt"), numbered("x", 1))
	writeLines(t, filepath.Join(newDir, "a.txt"), append([]string{"new top"}, append(numbered("x", 10), "added")...))
	d.Update(watchTickMsg{})
	_, file, line := d.flow.position()
	if got := a.lineAt(right, line); file != 0 || got != 10 {
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
	pressRoot(m, "r")
	if m.msg != "nothing changed" {
		t.Errorf("R on unchanged files: msg %q", m.msg)
	}
	writeLines(t, filepath.Join(dir, "new.txt"), numbered("x", 6))
	pressRoot(m, "f5")
	if m.msg != "new.txt reloaded" || len(m.lines[right]) != 6 {
		t.Errorf("F5 after a change: msg %q, %d lines", m.msg, len(m.lines[right]))
	}
	plain := newTestModel(numbered("x", 2), numbered("x", 2))
	pressRoot(plain, "r")
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

// newInotifyDiff returns the file list of newWatchedDiff with inotify
// on, as configured by default, started by Init, and the new directory.
func newInotifyDiff(t *testing.T) (*Diff, string, tea.Cmd) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("inotify is Linux only")
	}
	d, dir := newWatchedDiff(t)
	d.cfg.Settings.InotifyMaxDirs = settings.Default().InotifyMaxDirs
	cmd := d.Init()
	if d.disk.w == nil {
		t.Fatalf("not watched by inotify, msg %q", d.msg)
	}
	t.Cleanup(d.stop)
	return d, dir, cmd
}

func TestDiffInotifyReloadsAnyFile(t *testing.T) {
	d, dir, cmd := newInotifyDiff(t)
	// d.txt is three files away from the cursor: polling would not see it.
	writeLines(t, filepath.Join(dir, "d.txt"), append(numbered("x", 10), "added", "more", "lines"))
	msg := awaitMsg(t, cmd)
	if _, ok := msg.(changedMsg); !ok {
		t.Fatalf("message %T, want changedMsg", msg)
	}
	if _, cmd = d.Update(msg); cmd == nil {
		t.Error("not waiting for the next changes")
	}
	if d.files[3].added != 3 || d.msg != "d.txt reloaded" {
		t.Errorf("d.txt +%d, msg %q; want +3 and reloaded", d.files[3].added, d.msg)
	}
	// Ticks left from polling neither check nor go on.
	if _, cmd := d.Update(watchTickMsg{}); cmd != nil {
		t.Error("a tick while inotify watches schedules the next one")
	}
	// Changes of a watcher no longer used are dropped.
	old := changedMsg{w: nil, paths: []string{filepath.Join(dir, "a.txt")}}
	if _, cmd := d.Update(old); cmd != nil {
		t.Error("a change of another watcher is waited on")
	}
}

// Over inotify_max_dirs, the directories used last are watched; the files
// near the cursor in the others are checked every watch_interval.
func TestDiffInotifyLimit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("inotify is Linux only")
	}
	d, _ := newWatchedDiff(t) // the old and the new directory
	d.cfg.Settings.InotifyMaxDirs = 1
	if d.Init() == nil || d.disk.w == nil || !d.ticking {
		t.Errorf("two directories, inotify_max_dirs 1: watcher %v ticking %v; want one watched and polling", d.disk.w, d.ticking)
	}
	t.Cleanup(d.stop)
	f := d.files[0].File
	if d.disk.watched(f.OldFile) == d.disk.watched(f.NewFile) {
		t.Errorf("old watched %v, new %v; want one of them", d.disk.watched(f.OldFile), d.disk.watched(f.NewFile))
	}
	b, _ := newWatchedDiff(t)
	b.cfg.Settings.WatchInterval = 0
	if b.Init() != nil || b.disk.w != nil {
		t.Error("watch_interval 0 watches")
	}
}

// The files listed again are watched by the same watcher; in a directory
// over the limit they are checked while near the cursor.
func TestDiffRelistKeepsWatcher(t *testing.T) {
	d, dir, _ := newInotifyDiff(t)
	var list []source.File
	for _, e := range d.files {
		list = append(list, e.File)
	}
	d.WithRelist(func(context.Context) ([]source.File, error) { return list, nil })

	d.cfg.Settings.InotifyMaxDirs = 2
	d.Update(settingChangedMsg{action: "inotify_max_dirs"})
	third := filepath.Join(t.TempDir(), "e.txt")
	writeLines(t, third, []string{"e"})
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(third, old, old); err != nil {
		t.Fatal(err)
	}
	list = append(slices.Clone(list), source.File{Path: "e.txt", NewFile: third, New: []string{"e"}, InNew: true})
	w := d.disk.w
	_, cmd := d.Update(refreshMsg{})
	d.Update(cmd()) // the listing, which runs in the background
	if d.disk.w != w || d.disk.watched(third) {
		t.Fatalf("after r: watcher replaced %v, e.txt watched %v; want the same watcher, e.txt over the limit", d.disk.w != w, d.disk.watched(third))
	}
	if d.ticking {
		t.Error("polling while the files near the cursor are watched")
	}
	writeLines(t, filepath.Join(dir, "b.txt"), numbered("x", 12))
	if msg := awaitMsg(t, waitChanges(w)); msg.(changedMsg).paths[0] != filepath.Join(dir, "b.txt") {
		t.Errorf("message %+v, want b.txt changed", msg)
	}
}

// With the limit at 1, the cursor on a file brings its directory in, and the
// file is reloaded when it changed while it was not watched.
func TestDiffCursorWatches(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("inotify is Linux only")
	}
	dirs := []string{t.TempDir(), t.TempDir()}
	var files []source.File
	for i, d := range dirs {
		p := filepath.Join(d, fmt.Sprintf("f%d.txt", i))
		writeLines(t, p, numbered("x", 3))
		lines, st, err := source.Read(p)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, source.File{Path: filepath.Base(p), NewFile: p, New: lines, NewStamp: st, InNew: true})
	}
	cfg := config.Default()
	cfg.Settings.InotifyMaxDirs = 1
	cfg.AutoReload = true
	d := NewDiff(cfg, files)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	cmd := d.Init()
	t.Cleanup(d.stop)
	if !d.disk.watched(files[0].NewFile) || d.disk.watched(files[1].NewFile) {
		t.Fatalf("on f0: watched %v %v, want true false", d.disk.watched(files[0].NewFile), d.disk.watched(files[1].NewFile))
	}
	// Changed while not watched, with another size: reloaded when the
	// cursor comes to it.
	writeLines(t, files[1].NewFile, numbered("x", 5))
	pressDiff(d, "j")
	if !d.disk.watched(files[1].NewFile) || d.disk.watched(files[0].NewFile) {
		t.Errorf("on f1: watched %v %v, want false true", d.disk.watched(files[0].NewFile), d.disk.watched(files[1].NewFile))
	}
	if d.files[1].added != 5 {
		t.Errorf("f1 +%d, want the 5 lines read again", d.files[1].added)
	}
	if cmd == nil || !d.ticking {
		t.Errorf("Init: command %v, ticking %v; want f1, next to the cursor and not watched, checked", cmd != nil, d.ticking)
	}
	writeLines(t, files[1].NewFile, numbered("x", 7))
	d.Update(awaitMsg(t, waitChanges(d.disk.w)))
	if d.files[1].added != 7 {
		t.Errorf("f1 +%d after its change, want 7", d.files[1].added)
	}
}

func TestModelInotify(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("inotify is Linux only")
	}
	m, dir := newWatchedModel(t, numbered("x", 5), numbered("x", 5))
	m.cfg.Settings.InotifyMaxDirs = 1
	cmd := m.Init()
	if m.disk.w == nil {
		t.Fatalf("not watched by inotify, msg %q", m.msg)
	}
	t.Cleanup(m.stop)
	writeLines(t, filepath.Join(dir, "new.txt"), numbered("x", 6))
	if _, cmd = m.Update(awaitMsg(t, cmd)); cmd == nil {
		t.Error("not waiting for the next changes")
	}
	if m.msg != "new.txt reloaded" || len(m.lines[right]) != 6 {
		t.Errorf("msg %q, %d lines; want the reloaded 6", m.msg, len(m.lines[right]))
	}
}

func TestDiffSwitchStopsWatching(t *testing.T) {
	d, _, _ := newInotifyDiff(t)
	old := d.disk.w
	s, _ := newDiffSwitch(d)
	pressRoot(s, "d", "HEAD", "enter")
	if _, ok := <-old.Changes(); ok {
		t.Error("the replaced view still watches")
	}
}

func TestStaleTickDropped(t *testing.T) {
	m, _ := newWatchedModel(t, numbered("x", 2), numbered("x", 2))
	d, _ := newWatchedDiff(t)
	for _, v := range []tea.Model{m, d} {
		if _, cmd := v.Update(watchTickMsg{owner: newTestDiff()}); cmd != nil {
			t.Errorf("%T: a tick of another view schedules the next one", v)
		}
		if _, cmd := v.Update(watchTickMsg{owner: v}); cmd == nil {
			t.Errorf("%T: its own tick does not schedule the next one", v)
		}
	}
}

func TestWatchDropsAnchors(t *testing.T) {
	m, dir := newWatchedModel(t, numbered("x", 30), numbered("x", 30))
	m.cfg.Settings.CollapseUnchanged = false
	m.realign()
	m.fileAlign.addAnchor(diff.Anchor{Left: 10, Right: 10}, right, shape{})
	m.fileAlign.addAnchor(diff.Anchor{Left: 20, Right: 20}, right, shape{})
	m.realign()
	lines := numbered("x", 30)
	lines[10], lines[20] = "changed10", "changed20"
	writeLines(t, filepath.Join(dir, "new.txt"), lines)
	m.Update(watchTickMsg{})
	if len(m.anchors) != 0 || m.msg != "new.txt reloaded, 2 anchors dropped" {
		t.Errorf("both anchored lines changed: anchors %v, msg %q", m.anchors, m.msg)
	}
}

// fakeRepo is the repository auto reload checks.
type fakeRepo struct {
	status    string
	err       error
	paths     []string
	dirs      []string        // of the working tree
	dirsCalls int             // the calls of getDirs
	ctx       context.Context // of the last status
}

func (r *fakeRepo) getStatus(ctx context.Context) (string, error) {
	r.ctx = ctx
	return r.status, r.err
}
func (r *fakeRepo) getPaths() ([]string, error) { return r.paths, nil }
func (r *fakeRepo) getDirs(func(string) bool) (map[string]time.Time, error) {
	r.dirsCalls++
	dirs := map[string]time.Time{}
	for _, d := range r.dirs {
		dirs[d] = time.Time{}
	}
	return dirs, nil
}

// newAutoReload returns the switch around a file list of git diff, with
// the repository of a watched file, and the file. The view and the switch share
// their configuration, as in the program.
// loadNow runs the listing of the files that cmd starts, as the runtime does
// in the background, and gives s its result. Auto reload starts it in a batch
// with its tick and the wait for the watcher, which the tests do not run: the
// listing comes first.
func loadNow(t *testing.T, s tea.Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no listing started")
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		msg = b[0]()
	}
	if _, ok := msg.(filesListedMsg); !ok {
		t.Fatalf("got %T, want the files listed", msg)
	}
	s.Update(msg)
}

func newAutoReload(t *testing.T) (*DiffSwitch, *fakeGitDiff, *fakeRepo, string) {
	t.Helper()
	g := &fakeGitDiff{}
	file := filepath.Join(t.TempDir(), "HEAD")
	writeLines(t, file, []string{"x0"})
	repo := &fakeRepo{status: "s0", paths: []string{file}}
	cfg := config.Default()
	cfg.Settings.AutoReloadInterval = 3600 // the tests send the ticks
	d := NewDiff(cfg, nil).WithGitDiff(nil, g.diff)
	s := WithDiffSwitch(cfg, d, func(m tea.Model) tea.Model { return m }, g.diff).WithAutoReload(repo.getStatus, repo.getPaths, repo.getDirs)
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	t.Cleanup(s.stop)
	return s, g, repo, file
}

// R goes to the switch on every page; off, nothing is checked or shown.
func TestAutoReloadKeyOnAllPages(t *testing.T) {
	for _, page := range settingsPages() {
		m, _ := page.make()
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		pressRoot(m, page.keys...)
		_, cmd := m.Update(keyMsg("R"))
		if cmd == nil {
			t.Errorf("%s: R does nothing", page.name)
			continue
		}
		if _, ok := cmd().(askAutoReloadMsg); !ok {
			t.Errorf("%s: R asks for %T", page.name, cmd())
		}
	}
}

func TestAutoReloadOnAndOff(t *testing.T) {
	s, g, repo, _ := newAutoReload(t)
	if strings.Contains(ansi.Strip(s.frame()), "auto reload") {
		t.Errorf("the badge shows while off:\n%s", ansi.Strip(s.frame()))
	}
	_, cmd := s.Update(askAutoReloadMsg{})
	loadNow(t, s, cmd)
	if !s.cfg.AutoReload || len(g.runs) != 1 {
		t.Fatalf("on: %v, runs %v; want one load", s.cfg.AutoReload, g.runs)
	}
	if !strings.Contains(ansi.Strip(s.frame()), "auto reload") {
		t.Errorf("the badge does not show:\n%s", ansi.Strip(s.frame()))
	}
	// The same status does not load again, nor say so; a new one loads.
	gen := s.autoGen
	s.statusRuns++
	s.Update(autoStatusMsg{gen: gen, status: "s0"})
	if len(g.runs) != 1 {
		t.Errorf("same status: runs %v", g.runs)
	}
	repo.status = "s1"
	s.statusRuns++
	_, cmd = s.Update(autoStatusMsg{gen: gen, status: "s1"})
	loadNow(t, s, cmd)
	if len(g.runs) != 2 {
		t.Errorf("new status: runs %v", g.runs)
	}
	if strings.Contains(ansi.Strip(s.frame()), nothingChanged) {
		t.Errorf("says nothing changed:\n%s", ansi.Strip(s.frame()))
	}
	// Off: results and ticks under way are dropped.
	s.Update(askAutoReloadMsg{})
	s.statusRuns++
	s.Update(autoStatusMsg{gen: gen, status: "s2"})
	if _, cmd := s.Update(autoTickMsg{gen, s.tick}); s.cfg.AutoReload || s.pins != nil || len(g.runs) != 2 || cmd != nil {
		t.Errorf("off: on %v, pinned %v, runs %v, tick goes on %v", s.cfg.AutoReload, s.pins, g.runs, cmd != nil)
	}
	if s.statusRuns != 0 {
		t.Errorf("checks under way: %d", s.statusRuns)
	}
}

// The timeout ends auto reload and so the git commands; keys and changes put it
// off, a check that finds the same status does not; 0 is never.
func TestAutoReloadTimeout(t *testing.T) {
	s, _, _, _ := newAutoReload(t)
	const idle = 6 * time.Minute
	s.Update(askAutoReloadMsg{})
	gen := s.autoGen
	if _, cmd := s.Update(tickOf(s)); cmd == nil || s.statusRuns != 1 {
		t.Fatalf("active: cmd %v, checks %d; want a check", cmd != nil, s.statusRuns)
	}
	// While git is at it, no second check starts.
	if s.Update(tickOf(s)); s.statusRuns != 1 {
		t.Errorf("checks %d, want 1", s.statusRuns)
	}
	s.lastActive = time.Now().Add(-idle)
	s.Update(autoStatusMsg{gen: gen, status: "s0"})
	if time.Since(s.lastActive) < idle {
		t.Error("the same status counts as activity")
	}
	s.Update(keyMsg("j"))
	if time.Since(s.lastActive) >= idle {
		t.Error("a key does not count as activity")
	}
	s.lastActive = time.Now().Add(-idle)
	s.statusRuns++
	s.Update(autoStatusMsg{gen: gen, status: "s1"})
	if time.Since(s.lastActive) >= idle {
		t.Error("a change does not count as activity")
	}
	s.cfg.Settings.AutoReloadTimeout = 0
	s.lastActive = time.Now().Add(-idle)
	if s.Update(tickOf(s)); !s.cfg.AutoReload {
		t.Error("timeout 0 ended it")
	}
	s.statusRuns = 0
	s.cfg.Settings.AutoReloadTimeout = 5
	_, cmd := s.Update(tickOf(s))
	if s.cfg.AutoReload || s.pins != nil || s.statusRuns != 0 || cmd == nil {
		t.Fatalf("idle: on %v, pinned %v, checks %d", s.cfg.AutoReload, s.pins, s.statusRuns)
	}
	s.Update(cmd())
	if view := ansi.Strip(s.frame()); !strings.Contains(view, "auto reload off: 5 min") {
		t.Errorf("does not say why:\n%s", view)
	}
}

func TestAutoReloadSurvivesDiffSwitch(t *testing.T) {
	s, g, _, _ := newAutoReload(t)
	s.Update(askAutoReloadMsg{})
	gen := s.autoGen
	s.input = &lineInput{}
	s.Update(s.show([]string{"HEAD~1"})()) // git diff, which runs in the background
	s.statusRuns++
	_, cmd := s.Update(autoStatusMsg{gen: gen, status: "s1"})
	loadNow(t, s, cmd)
	if !s.cfg.AutoReload || len(g.runs) < 2 || !slices.Equal(g.runs[len(g.runs)-1], []string{"HEAD~1"}) {
		t.Errorf("on %v, runs %v; want the new diff loaded", s.cfg.AutoReload, g.runs)
	}
}

// Without a repository it does not start, and says why; git failing later ends it.
func TestAutoReloadErrors(t *testing.T) {
	s, _, repo, _ := newAutoReload(t)
	repo.err = errors.New("not a git repository\nmore")
	_, cmd := s.Update(askAutoReloadMsg{})
	s.Update(cmd())
	view := ansi.Strip(s.frame())
	if s.cfg.AutoReload || !strings.Contains(view, "auto reload: not a git repository") || strings.Contains(view, "more") {
		t.Errorf("on %v:\n%s", s.cfg.AutoReload, view)
	}
	repo.err = nil
	s.Update(askAutoReloadMsg{})
	if !s.cfg.AutoReload {
		t.Fatal("does not start")
	}
	s.statusRuns++
	_, cmd = s.Update(autoStatusMsg{gen: s.autoGen, err: errors.New("gone")})
	s.Update(cmd())
	if s.cfg.AutoReload || !strings.Contains(ansi.Strip(s.frame()), "auto reload: gone") {
		t.Errorf("on %v:\n%s", s.cfg.AutoReload, ansi.Strip(s.frame()))
	}
	s.cfg.Settings.AutoReloadInterval = 0
	_, cmd = s.Update(askAutoReloadMsg{})
	s.Update(cmd())
	if s.cfg.AutoReload || !strings.Contains(ansi.Strip(s.frame()), "auto_reload_interval is 0") {
		t.Errorf("interval 0: on %v:\n%s", s.cfg.AutoReload, ansi.Strip(s.frame()))
	}
}

// startAutoReload turns auto reload on in s, started by Init with its
// watcher, and gives it what to watch, which it finds in the background.
func startAutoReload(t *testing.T, s *DiffSwitch) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("inotify is Linux only")
	}
	s.Init()
	if s.w == nil {
		t.Fatal("no watcher")
	}
	s.Update(askAutoReloadMsg{})
	s.Update(s.watchRepo()())
	if len(s.pins) == 0 {
		t.Fatal("the repository is not watched")
	}
}

// A change the watcher reports of the repository's files, or in the working
// tree, is checked at once, but not of a file watch_ignore matches; one while
// git is at it once more after.
func TestAutoReloadWatcher(t *testing.T) {
	s, _, repo, file := newAutoReload(t)
	tree := t.TempDir()
	repo.dirs = []string{tree}
	startAutoReload(t, s)
	changed := func(path string) {
		t.Helper()
		writeLines(t, path, []string{"x1"})
		s.Update(awaitMsg(t, waitChanges(s.w)))
	}
	changed(file)
	if s.statusRuns != 1 {
		t.Errorf("repository changed: checks %d, want 1", s.statusRuns)
	}
	// While git is at it, once more after.
	changed(filepath.Join(tree, "new.txt"))
	if s.statusRuns != 1 || !s.statusAgain {
		t.Errorf("working tree changed during a check: checks %d, again %v; want 1, true", s.statusRuns, s.statusAgain)
	}
	if _, cmd := s.Update(autoStatusMsg{gen: s.autoGen, status: "s0"}); s.statusRuns != 1 || cmd == nil {
		t.Errorf("after the check: checks %d; want the one more", s.statusRuns)
	}
	s.statusRuns = 0
	changed(filepath.Join(tree, ".new.txt.swp"))
	if s.statusRuns != 0 {
		t.Errorf("swap file changed: checks %d, want none", s.statusRuns)
	}
	s.cfg.AutoReload = false
	changed(filepath.Join(tree, "other.txt"))
	if s.statusRuns != 0 {
		t.Errorf("auto reload off: checks %d, want none", s.statusRuns)
	}
}

// Off, the watcher no longer watches the repository nor the working tree; a
// late result of what to watch is dropped.
func TestAutoReloadOffUnwatches(t *testing.T) {
	s, _, repo, file := newAutoReload(t)
	tree := t.TempDir()
	repo.dirs = []string{tree}
	startAutoReload(t, s)
	if !s.w.Watched(file) || !s.w.Watched(filepath.Join(tree, "x")) {
		t.Fatal("not watched")
	}
	late := s.watchRepo()()
	s.Update(askAutoReloadMsg{})
	s.Update(late)
	if s.w.Watched(file) || s.w.Watched(filepath.Join(tree, "x")) || s.pins != nil {
		t.Errorf("off: watched %v %v, pinned %v", s.w.Watched(file), s.w.Watched(filepath.Join(tree, "x")), s.pins)
	}
}

// Auto reload is not saved, the timeout is.
func TestAutoReloadNotSaved(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg.AutoReload = true
	cfg.Settings.AutoReloadTimeout = 7
	if err := cfg.SaveState(); err != nil {
		t.Fatal(err)
	}
	again, err := config.Load(dir, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if again.AutoReload || again.Settings.AutoReloadTimeout != 7 {
		t.Errorf("after a restart: on %v, timeout %d; want off, 7", again.AutoReload, again.Settings.AutoReloadTimeout)
	}
}

// When its watcher ends by itself, as when a directory cannot be watched any
// more, a view checks its files every watch_interval; the end of a watcher
// it replaced changes nothing.
func TestWatchEndedPolls(t *testing.T) {
	d, _, _ := newInotifyDiff(t)
	old := d.disk.w
	t.Cleanup(func() { old.Close() })
	if _, cmd := d.Update(watchEndedMsg{&watch.Watcher{}}); cmd != nil || d.disk.w != old {
		t.Fatalf("another watcher ended: command %v, watcher changed %v", cmd != nil, d.disk.w != old)
	}
	if _, cmd := d.Update(watchEndedMsg{old}); cmd == nil || d.disk.w != nil || !d.ticking {
		t.Errorf("its watcher ended: command %v, watcher %v, ticking %v; want polling", cmd != nil, d.disk.w, d.ticking)
	}
}

// Turning auto reload off ends its git status.
func TestAutoReloadOffEndsStatus(t *testing.T) {
	s, _, repo, _ := newAutoReload(t)
	_, cmd := s.Update(askAutoReloadMsg{})
	loadNow(t, s, cmd)
	_, check := s.Update(tickOf(s))
	s.Update(askAutoReloadMsg{})
	if b, ok := check().(tea.BatchMsg); ok {
		b[1]() // the git status; the first is the next tick
	}
	if repo.ctx == nil || repo.ctx.Err() == nil {
		t.Error("git status runs on after auto reload ended")
	}
}

// Without auto reload nothing is watched nor checked; R starts both, reading
// again what changed while it was off, and off ends both. Of directories
// compared, R watches the files shown without git status, whose ticks only
// end it after the timeout.
func TestAutoReloadSwitchesWatching(t *testing.T) {
	d, dir := newWatchedDiff(t)
	d.cfg.AutoReload = false
	d.cfg.Settings.AutoReloadInterval = 3600 // the tests send the ticks
	if cmd := d.Init(); cmd != nil || d.disk.w != nil || d.ticking {
		t.Fatalf("off: Init %v, watcher %v, ticking %v; want nothing", cmd != nil, d.disk.w, d.ticking)
	}
	if _, cmd := d.Update(watchTickMsg{owner: d}); cmd != nil {
		t.Error("off: a tick checks on")
	}
	repo := &fakeRepo{status: "s0"}
	s := WithDiffSwitch(d.cfg, d, func(m tea.Model) tea.Model { return m }, nil).WithAutoReload(repo.getStatus, repo.getPaths, repo.getDirs)
	s.Init()
	t.Cleanup(s.stop)
	writeLines(t, filepath.Join(dir, "a.txt"), append(numbered("x", 10), "added", "more"))
	if _, cmd := s.Update(askAutoReloadMsg{}); !s.cfg.AutoReload || s.gitMode || repo.ctx != nil || !d.ticking || cmd == nil {
		t.Fatalf("on: %v, git %v, status run %v, ticking %v", s.cfg.AutoReload, s.gitMode, repo.ctx != nil, d.ticking)
	}
	if d.files[0].added != 2 {
		t.Errorf("a.txt +%d, want 2: changed while off", d.files[0].added)
	}
	if _, cmd := s.Update(tickOf(s)); s.statusRuns != 0 || cmd == nil {
		t.Errorf("tick: checks %d, next tick %v; want no git status, a next tick", s.statusRuns, cmd != nil)
	}
	s.Update(askAutoReloadMsg{})
	if _, cmd := d.Update(watchTickMsg{owner: d}); cmd != nil || d.watching {
		t.Errorf("off: the pending tick checks on %v, watching %v", cmd != nil, d.watching)
	}
}

// A change reported checks at once and starts the regular checks anew: the
// tick pending is dropped.
func TestAutoReloadChangeResetsTick(t *testing.T) {
	s, _, _, file := newAutoReload(t)
	startAutoReload(t, s)
	old := tickOf(s)
	writeLines(t, file, []string{"x1"})
	_, cmd := s.Update(awaitMsg(t, waitChanges(s.w)))
	if s.statusRuns != 1 || s.tick == old.tick || cmd == nil {
		t.Fatalf("change: checks %d, tick %d (was %d)", s.statusRuns, s.tick, old.tick)
	}
	if _, cmd := s.Update(old); cmd != nil || s.statusRuns != 1 {
		t.Errorf("the old tick: command %v, checks %d; want it dropped", cmd != nil, s.statusRuns)
	}
	s.statusRuns = 0
	if _, cmd := s.Update(tickOf(s)); cmd == nil || s.statusRuns != 1 {
		t.Errorf("the new tick: command %v, checks %d; want a check", cmd != nil, s.statusRuns)
	}
}

// With inotify_max_dirs 0 the repository's files are still watched, the
// working tree is not even looked at.
func TestAutoReloadLimitZero(t *testing.T) {
	s, _, repo, file := newAutoReload(t)
	repo.dirs = []string{t.TempDir()}
	s.cfg.Settings.InotifyMaxDirs = 0
	startAutoReload(t, s)
	if !s.w.Watched(file) || s.w.Watched(filepath.Join(repo.dirs[0], "x")) || repo.dirsCalls != 0 {
		t.Errorf("limit 0: HEAD watched %v, tree watched %v, getDirs called %d; want true, false, 0",
			s.w.Watched(file), s.w.Watched(filepath.Join(repo.dirs[0], "x")), repo.dirsCalls)
	}
}
