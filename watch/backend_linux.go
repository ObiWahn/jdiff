//go:build linux

package watch

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// mask selects the events that change a file's content or replace or
// remove it, and the move of a watched directory; touching its times or mode
// does not.
const mask = unix.IN_MODIFY | unix.IN_CLOSE_WRITE | unix.IN_CREATE | unix.IN_DELETE |
	unix.IN_MOVED_FROM | unix.IN_MOVED_TO | unix.IN_MOVE_SELF | unix.IN_ONLYDIR | unix.IN_EXCL_UNLINK

// inotify is the backend of Linux. All watches share one inotify instance,
// which takes one file descriptor. Its descriptor is used through Control,
// so that close cannot close it meanwhile.
type inotify struct {
	file *os.File
	mu   sync.Mutex
	live map[int32]bool // the watches not removed nor ended
	ch   chan event
	done chan struct{}
	once sync.Once
}

func newBackend() (backend, error) {
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("inotify: %w", err)
	}
	// Non-blocking, the file is read through Go's poller, so that close
	// ends a pending read.
	b := &inotify{file: os.NewFile(uintptr(fd), "inotify"), live: map[int32]bool{},
		ch: make(chan event), done: make(chan struct{})}
	go b.read()
	return b, nil
}

func (b *inotify) add(dir string) (int, error) {
	var wd int
	var err error
	if cerr := b.control(func(fd int) { wd, err = unix.InotifyAddWatch(fd, dir, mask) }); cerr != nil {
		return 0, cerr
	}
	if err != nil {
		return 0, fmt.Errorf("inotify: %s: %w", dir, err)
	}
	b.mu.Lock()
	b.live[int32(wd)] = true
	b.mu.Unlock()
	return wd, nil
}

func (b *inotify) remove(id int) {
	b.mu.Lock()
	live := b.live[int32(id)]
	delete(b.live, int32(id))
	b.mu.Unlock()
	if live {
		b.control(func(fd int) { unix.InotifyRmWatch(fd, uint32(id)) })
	}
}

func (b *inotify) events() <-chan event { return b.ch }

func (b *inotify) close() error {
	err := os.ErrClosed
	b.once.Do(func() {
		close(b.done)
		err = b.file.Close()
	})
	return err
}

// control runs f with the file's descriptor.
func (b *inotify) control(f func(fd int)) error {
	conn, err := b.file.SyscallConn()
	if err != nil {
		return err
	}
	return conn.Control(func(fd uintptr) { f(int(fd)) })
}

// read sends the events the kernel reports, until the file is closed. A
// watch removed is dropped, the end the kernel reports for it too. A
// directory moved away ends its watch, which would follow it.
func (b *inotify) read() {
	defer close(b.ch)
	buf := make([]byte, 64*1024)
	for {
		n, err := b.file.Read(buf)
		if err != nil {
			return
		}
		for off := 0; off+unix.SizeofInotifyEvent <= n; {
			wd := int32(binary.NativeEndian.Uint32(buf[off:]))
			evMask := binary.NativeEndian.Uint32(buf[off+4:])
			nameLen := int(binary.NativeEndian.Uint32(buf[off+12:]))
			name := bytes.TrimRight(buf[off+unix.SizeofInotifyEvent:off+unix.SizeofInotifyEvent+nameLen], "\x00")
			off += unix.SizeofInotifyEvent + nameLen
			ev, ok := b.event(wd, evMask, string(name))
			if !ok {
				continue
			}
			select {
			case b.ch <- ev:
			case <-b.done:
				return
			}
		}
	}
}

// event returns the event of watch wd, with evMask and name, and whether
// there is one.
func (b *inotify) event(wd int32, evMask uint32, name string) (event, bool) {
	if evMask&unix.IN_Q_OVERFLOW != 0 {
		return event{overflow: true}, true
	}
	b.mu.Lock()
	live := b.live[wd]
	ended := live && evMask&(unix.IN_IGNORED|unix.IN_MOVE_SELF) != 0
	if ended {
		delete(b.live, wd)
	}
	b.mu.Unlock()
	switch {
	case !live:
		return event{}, false
	case ended:
		if evMask&unix.IN_MOVE_SELF != 0 {
			b.control(func(fd int) { unix.InotifyRmWatch(fd, uint32(wd)) })
		}
		return event{id: int(wd)}, true
	case name == "":
		return event{}, false
	}
	return event{id: int(wd), name: name}, true
}
