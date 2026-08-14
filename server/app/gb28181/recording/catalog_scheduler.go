package recording

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

var (
	ErrCatalogSchedulerStopped     = errors.New("cloud recording catalog scheduler stopped")
	ErrCatalogSchedulerStopTimeout = errors.New("cloud recording catalog scheduler stop timeout")
)

type CatalogReconcileScheduler struct {
	reconciler *CatalogReconciler
	nodes      CatalogNodeLookup
	interval   time.Duration
	stopWait   time.Duration

	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	accepting bool
	jobs      map[int64]struct{}
	wg        sync.WaitGroup

	// 后台失败可观测:任务失败累计数与最近一次失败(供状态接口查询)
	failureCount atomic.Int64
	lastFailure  atomic.Value // string
}

// FailureStats 返回累计失败次数与最近一次失败描述.
func (s *CatalogReconcileScheduler) FailureStats() (int64, string) {
	last, _ := s.lastFailure.Load().(string)
	return s.failureCount.Load(), last
}

// recordFailure 记日志并累计失败 —— 后台任务失败不得静默
func (s *CatalogReconcileScheduler) recordFailure(scope string, err error) {
	s.failureCount.Add(1)
	s.lastFailure.Store(scope + ": " + err.Error())
	if app.ZapLog != nil {
		app.ZapLog.Error("录像目录后台任务失败", zap.String("scope", scope), zap.Error(err))
	}
}

func NewCatalogReconcileScheduler(reconciler *CatalogReconciler, nodes CatalogNodeLookup, interval, stopWait time.Duration) *CatalogReconcileScheduler {
	if stopWait <= 0 {
		stopWait = 10 * time.Second
	}
	return &CatalogReconcileScheduler{reconciler: reconciler, nodes: nodes, interval: interval, stopWait: stopWait, jobs: make(map[int64]struct{})}
}

func (s *CatalogReconcileScheduler) Start(parent context.Context) {
	s.mu.Lock()
	if s.accepting {
		s.mu.Unlock()
		return
	}
	s.ctx, s.cancel = context.WithCancel(parent)
	s.accepting = true
	ctx := s.ctx
	s.wg.Add(1)
	s.mu.Unlock()

	go s.loop(ctx)
	go func() {
		_, _ = s.Enqueue(ReconcileTriggerBootstrap, nil, nil, nil)
	}()
}

func (s *CatalogReconcileScheduler) Enqueue(trigger string, nodeIDs []int64, start, end *time.Time) ([]int64, error) {
	s.mu.Lock()
	if !s.accepting || s.ctx == nil || s.ctx.Err() != nil {
		s.mu.Unlock()
		return nil, ErrCatalogSchedulerStopped
	}
	requested := s.resolveNodeIDs(nodeIDs)
	accepted := make([]int64, 0, len(requested))
	for _, nodeID := range requested {
		if _, exists := s.jobs[nodeID]; exists {
			continue
		}
		s.jobs[nodeID] = struct{}{}
		accepted = append(accepted, nodeID)
	}
	ctx := s.ctx
	s.wg.Add(len(accepted))
	s.mu.Unlock()

	for _, nodeID := range accepted {
		nodeID := nodeID
		go func() {
			defer s.finishJob(nodeID)
			if err := s.reconciler.MarkQueued(ctx, nodeID, trigger, start, end); err != nil {
				s.recordFailure("mark-queued:node="+strconv.FormatInt(nodeID, 10), err)
				return
			}
			if _, err := s.reconciler.RunNode(ctx, nodeID, trigger, start, end); err != nil {
				s.recordFailure("run-node:node="+strconv.FormatInt(nodeID, 10), err)
			}
		}()
	}
	return accepted, nil
}

func (s *CatalogReconcileScheduler) Stop() error {
	s.mu.Lock()
	if !s.accepting {
		s.mu.Unlock()
		return nil
	}
	s.accepting = false
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(s.stopWait):
		return ErrCatalogSchedulerStopTimeout
	}
}

func (s *CatalogReconcileScheduler) loop(ctx context.Context) {
	defer s.wg.Done()
	if s.interval <= 0 {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.Enqueue(ReconcileTriggerScheduled, nil, nil, nil); err != nil && !errors.Is(err, ErrCatalogSchedulerStopped) {
				s.recordFailure("enqueue-scheduled", err)
			}
		}
	}
}

func (s *CatalogReconcileScheduler) resolveNodeIDs(requested []int64) []int64 {
	if len(requested) > 0 {
		result := append([]int64(nil), requested...)
		sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
		return result
	}
	nodes := s.nodes.List()
	result := make([]int64, 0, len(nodes))
	for _, n := range nodes {
		result = append(result, n.ID)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (s *CatalogReconcileScheduler) finishJob(nodeID int64) {
	s.mu.Lock()
	delete(s.jobs, nodeID)
	s.mu.Unlock()
	s.wg.Done()
}
