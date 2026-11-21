package logcleanup

import (
	"context"
	"fmt"
	"sync"
)

var kindLocks sync.Map // map[Kind]chan struct{}

// Acquire prevents overlapping cleanup work for the same log family, even if
// two distinct scheduler jobs target that family.
func Acquire(ctx context.Context, kind Kind) (func(), error) {
	if ctx == nil {
		return nil, fmt.Errorf("log cleanup context is nil")
	}
	if !validKind(kind) {
		return nil, fmt.Errorf("unknown log cleanup kind %q", kind)
	}
	value, _ := kindLocks.LoadOrStore(kind, make(chan struct{}, 1))
	semaphore := value.(chan struct{})
	select {
	case semaphore <- struct{}{}:
		return func() { <-semaphore }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func validKind(kind Kind) bool {
	switch kind {
	case SIP, Operation, Login, Job, Playback, Scheduler:
		return true
	default:
		return false
	}
}
