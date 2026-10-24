//go:build linux

package notify

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// settle is how long changes are collected after the first one, so that
// the events of one save come as one batch.
const settle = 50 * time.Millisecond

// mask selects the events that change a file's content or replace or
// remove it; touching its times or mode does not.
const mask = unix.IN_MODIFY | unix.IN_CLOSE_WRITE | unix.IN_CREATE | unix.IN_DELETE |
	unix.IN_MOVED_FROM | unix.IN_MOVED_TO | unix.IN_ONLYDIR | unix.IN_EXCL_UNLINK

// Watcher reports changes to files. All watches share one inotify instance,
// which takes one file descriptor.
type Watcher struct {
	file    *os.File
	dirs    map[int32]string    // watch descriptor -> directory
	names   map[string][]string // see watched
	changes chan []string
	done    chan struct{}
	once    sync.Once
}

// New starts watching paths. It fails with ErrTooManyDirs when they are in
// more than maxDirs directories, counting those of symlink targets, and
// when the kernel refuses, e.g. because the user's inotify instances or
// watches are used up. A directory that does not exist is left out.
func New(paths []string, maxDirs int) (*Watcher, error) {
	names := watched(paths)
	ds := dirs(names)
	if len(ds) > maxDirs {
		return nil, ErrTooManyDirs
	}
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("inotify: %w", err)
	}
	// Non-blocking, the file is read through Go's poller, so that Close
	// ends a pending read.
	w := &Watcher{file: os.NewFile(uintptr(fd), "inotify"), dirs: map[int32]string{}, names: names,
		changes: make(chan []string), done: make(chan struct{})}
	for _, d := range ds {
		wd, err := unix.InotifyAddWatch(fd, d, mask)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			w.file.Close()
			return nil, fmt.Errorf("inotify: %s: %w", d, err)
		}
		w.dirs[int32(wd)] = d
	}
	raw := make(chan string)
	go w.read(raw)
	go w.batch(raw)
	return w, nil
}

// Changes returns the channel of changed paths, as given to New, in sorted
// batches. It is closed when the watcher is.
func (w *Watcher) Changes() <-chan []string { return w.changes }

// Close stops watching.
func (w *Watcher) Close() error {
	err := os.ErrClosed
	w.once.Do(func() {
		close(w.done)
		err = w.file.Close()
	})
	return err
}

// read sends the watched paths the kernel reports to raw, until the
// watcher is closed. When the kernel's queue overflowed, events were lost:
// all paths are sent.
func (w *Watcher) read(raw chan<- string) {
	defer close(raw)
	send := func(paths []string) bool {
		for _, p := range paths {
			select {
			case raw <- p:
			case <-w.done:
				return false
			}
		}
		return true
	}
	buf := make([]byte, 64*1024)
	for {
		n, err := w.file.Read(buf)
		if err != nil {
			return
		}
		for off := 0; off+unix.SizeofInotifyEvent <= n; {
			wd := int32(binary.NativeEndian.Uint32(buf[off:]))
			evMask := binary.NativeEndian.Uint32(buf[off+4:])
			nameLen := int(binary.NativeEndian.Uint32(buf[off+12:]))
			name := bytes.TrimRight(buf[off+unix.SizeofInotifyEvent:off+unix.SizeofInotifyEvent+nameLen], "\x00")
			off += unix.SizeofInotifyEvent + nameLen
			var paths []string
			if evMask&unix.IN_Q_OVERFLOW != 0 {
				for _, ps := range w.names {
					paths = append(paths, ps...)
				}
			} else if dir, ok := w.dirs[wd]; ok && len(name) > 0 {
				paths = w.names[filepath.Join(dir, string(name))]
			}
			if !send(paths) {
				return
			}
		}
	}
}

// batch collects the paths from raw for settle after the first one and
// sends them on as one batch, until raw is closed.
func (w *Watcher) batch(raw <-chan string) {
	defer close(w.changes)
	for {
		p, ok := <-raw
		if !ok {
			return
		}
		set := map[string]bool{p: true}
		timer := time.NewTimer(settle)
	collect:
		for {
			select {
			case p, ok := <-raw:
				if !ok {
					timer.Stop()
					return
				}
				set[p] = true
			case <-timer.C:
				break collect
			}
		}
		select {
		case w.changes <- slices.Sorted(maps.Keys(set)):
		case <-w.done:
			return
		}
	}
}
