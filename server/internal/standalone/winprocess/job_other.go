//go:build !windows

package winprocess

// Job is unavailable outside Windows.
type Job struct{}

// Process is unavailable outside Windows.
type Process struct{}

// NewJob reports that Windows Job Objects are unavailable on this platform.
func NewJob() (*Job, error) { return nil, ErrUnsupported }

// Start reports that Windows Job Objects are unavailable on this platform.
func (*Job) Start(StartSpec) (*Process, error) { return nil, ErrUnsupported }

// Close reports that Windows Job Objects are unavailable on this platform.
func (*Job) Close() error { return ErrUnsupported }

// PID reports no process on this platform.
func (*Process) PID() int { return 0 }

// Wait reports that Windows Job Objects are unavailable on this platform.
func (*Process) Wait() (uint32, error) { return 0, ErrUnsupported }

// Close reports that Windows Job Objects are unavailable on this platform.
func (*Process) Close() error { return ErrUnsupported }
