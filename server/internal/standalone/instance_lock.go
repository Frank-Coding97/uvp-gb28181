package standalone

import (
	"errors"
	"sync"
)

var ErrInstanceRunning = errors.New("standalone: this installation is already running or in maintenance")

// InstanceLock holds an OS file handle for the whole lifecycle. On Windows the
// file cannot be deleted or replaced while open. PID files are never consulted.
type InstanceLock struct {
	once    sync.Once
	release func() error
	err     error
}

func (l *InstanceLock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() { l.err = l.release() })
	return l.err
}
