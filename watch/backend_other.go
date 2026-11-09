//go:build !linux

package watch

// newBackend fails: only Linux has a backend yet.
func newBackend() (backend, error) { return nil, ErrUnsupported }
