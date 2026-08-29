package scheduler

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// SchedulerLog 一次调度决策的日志记录(M3 T3.3)
//
// 每次 Manager.Pick 后(无论命中还是 ErrNoActiveNode)生成一条,
// LogService 异步写入 scheduler_log 表。供运维事后审计 / 复盘负载分布。
//
// JSON tag 走 camelCase 跟 web/src/api 类型对齐。
type SchedulerLog struct {
	ID           int64     `json:"id"`
	HappenedAt   time.Time `json:"happenedAt"`
	Algorithm    string    `json:"algorithm"`
	NodeID       int64     `json:"nodeID"`
	NodeName     string    `json:"nodeName"`
	StreamID     string    `json:"streamID"`
	DeviceID     string    `json:"deviceID"`
	ChannelID    string    `json:"channelID"`
	ErrorMessage string    `json:"errorMessage"` // 空表示成功,ErrNoActiveNode 等错误填这里
}

// SchedulerLogResult is a typed query result. Success is derived from an
// empty ErrorMessage; it is not a separately persisted flag.
type SchedulerLogResult string

const (
	SchedulerLogResultSuccess SchedulerLogResult = "success"
	SchedulerLogResultError   SchedulerLogResult = "error"
)

// SchedulerLogFilter is the bounded, typed log query contract.
type SchedulerLogFilter struct {
	From      *time.Time
	To        *time.Time
	NodeID    *int64
	Algorithm string
	Policy    string // compatibility alias for Algorithm in log query clients
	Result    SchedulerLogResult
	StreamID  string
	Limit     int
}

// SchedulerLogRepo 持久化抽象
//
// 实现:repo.GormSchedulerLogRepo(本工程 gorm 版)。
// 单测注入 fake 实现验证非阻塞 / drop / prune 行为。
type SchedulerLogRepo interface {
	Insert(ctx context.Context, log SchedulerLog) error
	List(ctx context.Context, limit int) ([]SchedulerLog, error)
	PruneOlderThan(ctx context.Context, t time.Time) (int64, error)
}

// LogService 调度日志异步采集 + 持久化
//
// 设计:
//   - Emit 走 buffered channel,非阻塞 send(满即 drop)— 不能阻塞 SIP play 主路径
//   - Start 起 worker goroutine 消费 channel,逐条 Insert
//   - Stop 关闭 channel + 等 worker drain pending(优雅退出)
//   - DB 写失败 zap.Warn 后继续,不阻塞主循环
//
// 线程安全:
//   - Emit 可并发调用
//   - Start / Stop 可重复调用(由 bootstrap 控制)
type LogService struct {
	repo       SchedulerLogRepo
	bufferSize int

	mu        sync.Mutex
	ch        chan SchedulerLog
	started   atomic.Bool
	stopped   atomic.Bool
	workerWG  sync.WaitGroup
	dropCount atomic.Int64
}

// NewLogService 构造,bufferSize <= 0 → 默认 1000
func NewLogService(repo SchedulerLogRepo, bufferSize int) *LogService {
	if bufferSize <= 0 {
		bufferSize = 1000
	}
	return &LogService{
		repo:       repo,
		bufferSize: bufferSize,
	}
}

// Start 启动 worker goroutine 消费 channel
//
// ctx cancel 后 worker 结束(但 Stop() drain 路径不依赖 ctx,
// 即便 ctx 已 cancel 仍能 drain 残余条目)。
func (s *LogService) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started.Load() || s.stopped.Load() {
		s.mu.Unlock()
		return // 已启动或已停止
	}
	s.started.Store(true)
	s.ch = make(chan SchedulerLog, s.bufferSize)
	ch := s.ch
	s.workerWG.Add(1)
	s.mu.Unlock()

	go s.runWorker(ctx, ch)
}

// runWorker 消费 channel,逐条写 DB
//
// 退出条件:
//   - channel 被 Stop 关闭 → for-range 自然退出(已 drain 完)
//   - ctx 被 cancel → 退出循环,丢弃剩余(若 Stop 先调,这分支走不到)
//
// 注意:闭包持有的 ch 是 Start 时建的实例,Stop 把 s.ch 置 nil 后,
// 这里仍能从 ch 读到 closed 信号(否则会从 nil 读永久阻塞)。
func (s *LogService) runWorker(ctx context.Context, ch chan SchedulerLog) {
	defer s.workerWG.Done()
	for {
		select {
		case <-ctx.Done():
			// ctx cancel:尽量 drain 已入队的,再退出(非阻塞)
			s.drainRemaining(ch)
			return
		case entry, ok := <-ch:
			if !ok {
				return // channel 已关 + drain 完
			}
			s.writeOne(ctx, entry)
		}
	}
}

// drainRemaining ctx cancel 后,把已入队的尽量写完(每条用独立短超时)
func (s *LogService) drainRemaining(ch chan SchedulerLog) {
	for {
		select {
		case entry, ok := <-ch:
			if !ok {
				return
			}
			// 用独立短超时 ctx,避免父 ctx 已 cancel 时 Insert 立即失败
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			s.writeOne(ctx, entry)
			cancel()
		default:
			return
		}
	}
}

// writeOne 写单条日志,失败 zap.Warn 不重试不阻塞
func (s *LogService) writeOne(ctx context.Context, entry SchedulerLog) {
	if err := s.repo.Insert(ctx, entry); err != nil {
		if app.ZapLog != nil {
			app.ZapLog.Warn("scheduler log insert failed",
				zap.String("algorithm", entry.Algorithm),
				zap.Int64("nodeID", entry.NodeID),
				zap.Error(err))
		}
	}
}

// Emit 异步派发一条日志(非阻塞)
//
// channel 满直接 drop,zap.Warn 上报丢弃总数(避免每条一行刷屏)。
// 未 Start 时 noop(bootstrap 装配失败的降级路径)。
func (s *LogService) Emit(entry SchedulerLog) {
	if !s.started.Load() || s.stopped.Load() {
		return
	}
	s.mu.Lock()
	if s.stopped.Load() {
		s.mu.Unlock()
		return
	}
	ch := s.ch
	if ch == nil {
		s.mu.Unlock()
		return
	}
	select {
	case ch <- entry:
		// 入队成功
		s.mu.Unlock()
	default:
		// 满 → drop
		dropped := s.dropCount.Add(1)
		s.mu.Unlock()
		// 每 100 条 drop 警告一次,避免刷屏
		if dropped%100 == 1 && app.ZapLog != nil {
			app.ZapLog.Warn("scheduler log buffer full, entry dropped",
				zap.Int64("totalDropped", dropped))
		}
	}
}

// Stop 关闭 channel + 等 worker drain pending
//
// 调用后再 Emit 都是 noop;多次 Stop 安全。
func (s *LogService) Stop() {
	s.mu.Lock()
	if s.stopped.Load() {
		started := s.started.Load()
		s.mu.Unlock()
		if started {
			s.workerWG.Wait()
		}
		return
	}
	s.stopped.Store(true)
	if !s.started.Load() {
		s.mu.Unlock()
		return // 未 Start
	}
	ch := s.ch
	s.ch = nil
	if ch != nil {
		close(ch)
	}
	s.mu.Unlock()
	s.workerWG.Wait()
}

// PruneOlderThan 主动清理 happened_at < t 的历史(bootstrap ticker 调)
func (s *LogService) PruneOlderThan(ctx context.Context, t time.Time) (int64, error) {
	return s.repo.PruneOlderThan(ctx, t)
}

// List 透传 repo.List(给 Controller 用)
func (s *LogService) List(ctx context.Context, limit int) ([]SchedulerLog, error) {
	return s.repo.List(ctx, limit)
}

// ListFiltered uses the repository's parameterized filter when available. The
// fallback keeps old injected repositories source-compatible and applies the
// same filter in memory with the hard limit still enforced.
func (s *LogService) ListFiltered(ctx context.Context, filter SchedulerLogFilter) ([]SchedulerLog, error) {
	filter = NormalizeSchedulerLogFilter(filter)
	if repo, ok := s.repo.(SchedulerLogFilteredRepo); ok {
		return repo.ListFiltered(ctx, filter)
	}
	rows, err := s.repo.List(ctx, 1000)
	if err != nil {
		return nil, err
	}
	// Older repositories only promise List and may return rows in insertion or
	// arbitrary order. Sort before applying the requested limit so the fallback
	// has the same newest-first semantics as the typed query path.
	SortSchedulerLogsByTime(rows)
	out := make([]SchedulerLog, 0, len(rows))
	for _, row := range rows {
		if !matchesSchedulerLogFilter(row, filter) {
			continue
		}
		out = append(out, row)
		if len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

// SchedulerLogFilteredRepo is optional to preserve the original three-method
// repository interface used by older bootstrap adapters.
type SchedulerLogFilteredRepo interface {
	ListFiltered(ctx context.Context, filter SchedulerLogFilter) ([]SchedulerLog, error)
}

func matchesSchedulerLogFilter(row SchedulerLog, filter SchedulerLogFilter) bool {
	if filter.From != nil && row.HappenedAt.Before(*filter.From) {
		return false
	}
	if filter.To != nil && row.HappenedAt.After(*filter.To) {
		return false
	}
	if filter.NodeID != nil && row.NodeID != *filter.NodeID {
		return false
	}
	algorithm := filter.Algorithm
	if algorithm == "" {
		algorithm = filter.Policy
	}
	if algorithm != "" && row.Algorithm != algorithm {
		return false
	}
	if filter.StreamID != "" && row.StreamID != filter.StreamID {
		return false
	}
	switch filter.Result {
	case "", SchedulerLogResultSuccess:
		if filter.Result == SchedulerLogResultSuccess && row.ErrorMessage != "" {
			return false
		}
	case SchedulerLogResultError:
		if row.ErrorMessage == "" {
			return false
		}
	default:
		return false
	}
	return true
}

// NormalizeSchedulerLogFilter makes repository/controller integrations
// deterministic without allowing arbitrary algorithms or unbounded limits.
func NormalizeSchedulerLogFilter(filter SchedulerLogFilter) SchedulerLogFilter {
	filter.Algorithm = strings.TrimSpace(filter.Algorithm)
	filter.Policy = strings.TrimSpace(filter.Policy)
	if filter.Algorithm == "" {
		filter.Algorithm = filter.Policy
	}
	if filter.Policy == "" {
		filter.Policy = filter.Algorithm
	}
	filter.StreamID = strings.TrimSpace(filter.StreamID)
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}
	return filter
}

// SortSchedulerLogsByTime is a small deterministic helper for fallback repos
// that do not promise ordering.
func SortSchedulerLogsByTime(rows []SchedulerLog) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].HappenedAt.Equal(rows[j].HappenedAt) {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].HappenedAt.After(rows[j].HappenedAt)
	})
}

// DropCount 返回当前累计丢弃数(给监控 / 测试用)
func (s *LogService) DropCount() int64 {
	return s.dropCount.Load()
}
