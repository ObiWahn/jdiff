//go:build !linux

package notify

// Watcher reports changes to files; see the Linux version.
type Watcher struct{}

// New fails: only Linux is supported.
func New(paths []string, maxDirs int) (*Watcher, error) { return nil, ErrUnsupported }

// Changes returns a channel that is never sent on.
func (w *Watcher) Changes() <-chan []string { return nil }

// Close does nothing.
func (w *Watcher) Close() error { return nil }
