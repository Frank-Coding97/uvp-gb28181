package recordcache

import "sync"

// keyedLocker 是"按 key 串行"的锁：同一个任务的推进必须串行
// （Tick 可能与用户取消/删除并发），不同任务之间不该互相阻塞。
type keyedLocker struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	refs int
	mu   sync.Mutex
}

func newKeyedLocker() *keyedLocker {
	return &keyedLocker{locks: make(map[string]*keyedLock)}
}

// Lock 获取 key 对应的锁并返回释放函数。
func (l *keyedLocker) Lock(key string) func() {
	if l == nil {
		return func() {}
	}
	l.mu.Lock()
	entry, ok := l.locks[key]
	if !ok {
		entry = &keyedLock{}
		l.locks[key] = entry
	}
	entry.refs++
	l.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		l.mu.Lock()
		entry.refs--
		if entry.refs <= 0 {
			delete(l.locks, key)
		}
		l.mu.Unlock()
	}
}
