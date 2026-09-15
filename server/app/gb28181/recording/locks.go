package recording

import "sync"

type keyedLockEntry struct {
	mu   sync.Mutex
	refs int
}

type keyedLocker struct {
	mu    sync.Mutex
	items map[uint]*keyedLockEntry
}

func newKeyedLocker() *keyedLocker {
	return &keyedLocker{items: make(map[uint]*keyedLockEntry)}
}

func (l *keyedLocker) Lock(key uint) func() {
	l.mu.Lock()
	entry := l.items[key]
	if entry == nil {
		entry = &keyedLockEntry{}
		l.items[key] = entry
	}
	entry.refs++
	l.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		l.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.items, key)
		}
		l.mu.Unlock()
	}
}
