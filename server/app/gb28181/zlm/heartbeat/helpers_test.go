package heartbeat_test

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// memoryRepo 内存版 Repo,只用来测 Collector / Watcher(repo 包另测真 DB 版)
// 复制自 zlm/node/registry_test.go,本测试包私用。
type memoryRepo struct {
	mu        sync.Mutex
	nextID    int64
	rows      map[int64]node.Node
	updateCnt int64 // 原子计数 DB.Update 调用次数,Watcher 测试用
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{rows: make(map[int64]node.Node)}
}

func (r *memoryRepo) List(_ context.Context) ([]node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]node.Node, 0, len(r.rows))
	for _, n := range r.rows {
		out = append(out, n)
	}
	return out, nil
}

func (r *memoryRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.rows[id]; ok {
		nc := n
		return &nc, nil
	}
	return nil, nil
}

func (r *memoryRepo) Create(_ context.Context, n node.Node) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	n.ID = r.nextID
	r.rows[n.ID] = n
	return n.ID, nil
}

func (r *memoryRepo) Update(_ context.Context, n node.Node) error {
	r.mu.Lock()
	r.rows[n.ID] = n
	r.mu.Unlock()
	atomic.AddInt64(&r.updateCnt, 1)
	return nil
}

func (r *memoryRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, id)
	return nil
}

func (r *memoryRepo) UpdateCount() int64 {
	return atomic.LoadInt64(&r.updateCnt)
}

// fakeClock 测试时钟,Watcher 注入用
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
