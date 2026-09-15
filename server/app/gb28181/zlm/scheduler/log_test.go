package scheduler_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

// fakeSchedulerLogRepo 内存 repo,用于 LogService 单测
//
// 支持:
//   - sleepOnInsert:每次 Insert 故意 sleep,模拟慢 DB
//   - insertErr:Insert 注入错误
//   - 持久化已插入条目供断言
type fakeSchedulerLogRepo struct {
	mu             sync.Mutex
	rows           []scheduler.SchedulerLog
	sleepOnInsert  time.Duration
	insertErr      error
	prunedBefore   time.Time
	prunedCount    int64
	nowFn          func() time.Time
	insertCounter  int
}

func newFakeSchedulerLogRepo() *fakeSchedulerLogRepo {
	return &fakeSchedulerLogRepo{nowFn: time.Now}
}

func (r *fakeSchedulerLogRepo) Insert(_ context.Context, log scheduler.SchedulerLog) error {
	if r.sleepOnInsert > 0 {
		time.Sleep(r.sleepOnInsert)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.insertErr != nil {
		return r.insertErr
	}
	r.insertCounter++
	log.ID = int64(r.insertCounter)
	r.rows = append(r.rows, log)
	return nil
}

func (r *fakeSchedulerLogRepo) List(_ context.Context, limit int) ([]scheduler.SchedulerLog, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit >= len(r.rows) {
		out := make([]scheduler.SchedulerLog, len(r.rows))
		copy(out, r.rows)
		return out, nil
	}
	out := make([]scheduler.SchedulerLog, limit)
	copy(out, r.rows[len(r.rows)-limit:])
	return out, nil
}

func (r *fakeSchedulerLogRepo) PruneOlderThan(_ context.Context, t time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prunedBefore = t
	kept := r.rows[:0]
	var pruned int64
	for _, row := range r.rows {
		if row.HappenedAt.Before(t) {
			pruned++
			continue
		}
		kept = append(kept, row)
	}
	r.rows = kept
	r.prunedCount += pruned
	return pruned, nil
}

func (r *fakeSchedulerLogRepo) snapshot() []scheduler.SchedulerLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]scheduler.SchedulerLog, len(r.rows))
	copy(out, r.rows)
	return out
}

// ---------- 测试 ----------

func TestSchedulerLog_AsyncWrite_NeverBlocks(t *testing.T) {
	repo := newFakeSchedulerLogRepo()
	repo.sleepOnInsert = 1 * time.Second // 故意慢

	svc := scheduler.NewLogService(repo, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	start := time.Now()
	svc.Emit(scheduler.SchedulerLog{
		HappenedAt: time.Now(),
		Algorithm:  "roundrobin",
		NodeID:     1,
		NodeName:   "n1",
		StreamID:   "s1",
	})
	elapsed := time.Since(start)
	require.Less(t, elapsed, 10*time.Millisecond, "Emit 必须非阻塞,buffered channel 异步,实际 %s", elapsed)
}

func TestSchedulerLog_BufferFull_DropsEntries(t *testing.T) {
	repo := newFakeSchedulerLogRepo()
	// 让 worker 处理慢,buffer 容易满
	repo.sleepOnInsert = 50 * time.Millisecond

	svc := scheduler.NewLogService(repo, 5) // 小 buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	// 连发 100 条 — 不应阻塞,不应 panic
	start := time.Now()
	require.NotPanics(t, func() {
		for i := 0; i < 100; i++ {
			svc.Emit(scheduler.SchedulerLog{
				HappenedAt: time.Now(),
				Algorithm:  "roundrobin",
				NodeID:     int64(i),
				NodeName:   "n",
			})
		}
	})
	require.Less(t, time.Since(start), 500*time.Millisecond,
		"100 条 Emit 应在 500ms 内返回(buffer 满直接 drop)")

	// 给 worker 一点时间处理已入队的
	time.Sleep(200 * time.Millisecond)
	svc.Stop()

	written := len(repo.snapshot())
	require.LessOrEqual(t, written, 100, "落库条数 ≤ 100,buffer 满会 drop 一部分")
	require.Greater(t, written, 0, "至少要落几条")
}

func TestSchedulerLog_PruneOlderThan7d(t *testing.T) {
	repo := newFakeSchedulerLogRepo()
	now := time.Now()
	// 5 条 8 天前
	for i := 0; i < 5; i++ {
		repo.rows = append(repo.rows, scheduler.SchedulerLog{
			ID:         int64(i + 1),
			HappenedAt: now.Add(-8 * 24 * time.Hour),
			Algorithm:  "roundrobin",
		})
	}
	// 5 条 1 天前
	for i := 0; i < 5; i++ {
		repo.rows = append(repo.rows, scheduler.SchedulerLog{
			ID:         int64(i + 6),
			HappenedAt: now.Add(-24 * time.Hour),
			Algorithm:  "roundrobin",
		})
	}

	svc := scheduler.NewLogService(repo, 10)
	pruned, err := svc.PruneOlderThan(context.Background(), now.Add(-7*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(5), pruned)

	remain := repo.snapshot()
	require.Len(t, remain, 5)
	for _, r := range remain {
		require.True(t, r.HappenedAt.After(now.Add(-7*24*time.Hour)))
	}
}

func TestSchedulerLog_Stop_FlushesPending(t *testing.T) {
	repo := newFakeSchedulerLogRepo()
	svc := scheduler.NewLogService(repo, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	svc.Emit(scheduler.SchedulerLog{HappenedAt: time.Now(), Algorithm: "roundrobin", NodeID: 1, NodeName: "n1"})
	svc.Emit(scheduler.SchedulerLog{HappenedAt: time.Now(), Algorithm: "roundrobin", NodeID: 2, NodeName: "n2"})
	svc.Emit(scheduler.SchedulerLog{HappenedAt: time.Now(), Algorithm: "roundrobin", NodeID: 3, NodeName: "n3"})

	svc.Stop() // 应等 worker drain 完所有 pending

	rows := repo.snapshot()
	require.Len(t, rows, 3, "Stop 应优雅 drain,3 条全落库")
}

func TestSchedulerLog_NotStarted_EmitIsNoop(t *testing.T) {
	repo := newFakeSchedulerLogRepo()
	svc := scheduler.NewLogService(repo, 10)
	// 未 Start
	require.NotPanics(t, func() {
		svc.Emit(scheduler.SchedulerLog{HappenedAt: time.Now(), Algorithm: "roundrobin"})
	})
}

func TestSchedulerLog_StopWithoutStart_Safe(t *testing.T) {
	repo := newFakeSchedulerLogRepo()
	svc := scheduler.NewLogService(repo, 10)
	require.NotPanics(t, func() { svc.Stop() })
}
