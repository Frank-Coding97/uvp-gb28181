package recording

import (
	"context"
	"sync"
)

type keyedLockEntry struct {
	token chan struct{}
	refs  int
}

type keyedLocker struct {
	mu    sync.Mutex
	items map[uint]*keyedLockEntry
}

func newKeyedLocker() *keyedLocker {
	return &keyedLocker{items: make(map[uint]*keyedLockEntry)}
}

func (l *keyedLocker) Lock(key uint) func() {
	unlock, err := l.LockContext(context.Background(), key)
	if err != nil {
		panic(err)
	}
	return unlock
}

func (l *keyedLocker) LockContext(ctx context.Context, key uint) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	l.mu.Lock()
	entry := l.items[key]
	if entry == nil {
		entry = &keyedLockEntry{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		l.items[key] = entry
	}
	entry.refs++
	l.mu.Unlock()

	select {
	case <-entry.token:
		if err := ctx.Err(); err != nil {
			l.releaseAcquired(key, entry)
			return nil, err
		}
		return func() { l.releaseAcquired(key, entry) }, nil
	case <-ctx.Done():
		l.releaseWaiting(key, entry)
		return nil, ctx.Err()
	}
}

func (l *keyedLocker) releaseAcquired(key uint, entry *keyedLockEntry) {
	entry.token <- struct{}{}
	l.releaseWaiting(key, entry)
}

func (l *keyedLocker) releaseWaiting(key uint, entry *keyedLockEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry.refs--
	if entry.refs == 0 {
		delete(l.items, key)
	}
}
