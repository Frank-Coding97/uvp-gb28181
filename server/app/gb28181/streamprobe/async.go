package streamprobe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

const (
	probeStreamKey = "uvp:streamprobe:queue"
	probeGroup     = "uvp-streamprobe-workers"
	probePrefix    = "uvp:streamprobe:task:"
	probeDedupPre  = "uvp:streamprobe:dedup:"
	probeTaskTTL   = 10 * time.Minute

	// probeQueueGrace 覆盖从「任务创建」到「worker 真正开始采样」之间的排队与调度耗时。
	// DeadlineAt 是从创建时刻起算的绝对值,所以它必须比服务侧的调用预算再宽一档,否则
	// 排到队尾的任务会在采样还没结束时就先被判超时,把已经在手的快照丢掉。
	probeQueueGrace = 15 * time.Second

	// probeClaimIdle 是 pending 消息可被其它 worker 认领前的最小空闲时间。
	// ⛔ 必须大于「最长的合法任务预算」,否则一次正常的长探针会被另一个 worker 当成死消息
	// 抢走并重复采样 —— 单实例下由 Service 的 single-flight 兜住,多实例下兜不住。
	probeClaimIdle = 120 * time.Second
)

// probeTaskDeadline 是任务的绝对截止时间(从创建时刻起算)。
func probeTaskDeadline(created time.Time, durationMS int) time.Time {
	return created.Add(probeCallBudget(0, durationMS) + probeQueueGrace)
}

var (
	ErrTaskNotFound     = errors.New("stream probe task not found")
	ErrQueueUnavailable = errors.New("stream probe queue unavailable")
)

type TaskStatus string

const (
	TaskQueued    TaskStatus = "queued"
	TaskSampling  TaskStatus = "sampling"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
)

type Task struct {
	OperationID string         `json:"operationId"`
	StreamID    string         `json:"streamId"`
	DurationMS  int            `json:"durationMs"`
	Status      TaskStatus     `json:"status"`
	CreatedAt   time.Time      `json:"createdAt"`
	StartedAt   *time.Time     `json:"startedAt,omitempty"`
	CompletedAt *time.Time     `json:"completedAt,omitempty"`
	DeadlineAt  time.Time      `json:"deadlineAt"`
	Snapshot    *ProbeSnapshot `json:"snapshot,omitempty"`
	Error       string         `json:"error,omitempty"`
}

type TaskStore interface {
	Create(context.Context, string, int) (*Task, error)
	Get(context.Context, string) (*Task, error)
	RunWorker(context.Context, string, int)
}

type RedisTaskStore struct {
	client       *redis.Client
	service      *Service
	prefix       string
	ttl          time.Duration
	claimMinIdle time.Duration
}

func NewRedisTaskStore(client *redis.Client, service *Service) *RedisTaskStore {
	return &RedisTaskStore{client: client, service: service, prefix: probePrefix, ttl: probeTaskTTL, claimMinIdle: probeClaimIdle}
}

func (s *RedisTaskStore) taskKey(id string) string { return s.prefix + id }
func (s *RedisTaskStore) dedupKey(streamID string, durationMS int) string {
	return probeDedupPre + streamID + ":" + strconv.Itoa(durationMS)
}

func (s *RedisTaskStore) Create(ctx context.Context, streamID string, durationMS int) (*Task, error) {
	if s == nil || s.client == nil || s.service == nil {
		return nil, ErrQueueUnavailable
	}
	if !IsSupportedDuration(durationMS) {
		return nil, fmt.Errorf("unsupported duration %d", durationMS)
	}
	dedup := s.dedupKey(streamID, durationMS)
	if existing, err := s.client.Get(ctx, dedup).Result(); err == nil {
		return s.Get(ctx, existing)
	} else if err != redis.Nil {
		return nil, err
	}
	now := time.Now().UTC()
	task := &Task{OperationID: uuid.NewString(), StreamID: streamID, DurationMS: durationMS, Status: TaskQueued, CreatedAt: now, DeadlineAt: probeTaskDeadline(now, durationMS)}
	payload, err := json.Marshal(task)
	if err != nil {
		return nil, err
	}
	claimed, err := s.client.SetNX(ctx, dedup, task.OperationID, s.ttl).Result()
	if err != nil {
		return nil, err
	}
	if !claimed {
		id, getErr := s.client.Get(ctx, dedup).Result()
		if getErr != nil {
			return nil, getErr
		}
		return s.Get(ctx, id)
	}
	if err = s.client.Set(ctx, s.taskKey(task.OperationID), payload, s.ttl).Err(); err != nil {
		_ = s.client.Del(ctx, dedup).Err()
		return nil, err
	}
	if _, err = s.client.XAdd(ctx, &redis.XAddArgs{Stream: probeStreamKey, Values: map[string]interface{}{"operationId": task.OperationID}}).Result(); err != nil {
		_ = s.client.Del(ctx, dedup, s.taskKey(task.OperationID)).Err()
		return nil, err
	}
	return task, nil
}

func (s *RedisTaskStore) Get(ctx context.Context, operationID string) (*Task, error) {
	if s == nil || s.client == nil {
		return nil, ErrQueueUnavailable
	}
	value, err := s.client.Get(ctx, s.taskKey(operationID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	var task Task
	if err := json.Unmarshal([]byte(value), &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *RedisTaskStore) update(ctx context.Context, task *Task) error {
	payload, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.taskKey(task.OperationID), payload, s.ttl).Err()
}

func (s *RedisTaskStore) ensureGroup(ctx context.Context) error {
	if _, err := s.client.XGroupCreateMkStream(ctx, probeStreamKey, probeGroup, "0").Result(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

func (s *RedisTaskStore) RunWorker(ctx context.Context, consumer string, count int) {
	if s == nil || s.client == nil || s.service == nil {
		return
	}
	if count < 1 {
		count = 1
	}
	if err := s.ensureGroup(ctx); err != nil {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		claimed, err := s.claimPending(ctx, consumer, int64(count))
		if err == nil {
			for _, message := range claimed {
				s.processMessage(ctx, message)
			}
		}
		entries, err := s.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: probeGroup, Consumer: consumer, Streams: []string{probeStreamKey, ">"}, Count: int64(count), Block: 2 * time.Second}).Result()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		for _, stream := range entries {
			for _, message := range stream.Messages {
				s.processMessage(ctx, message)
			}
		}
	}
}

func (s *RedisTaskStore) claimPending(ctx context.Context, consumer string, count int64) ([]redis.XMessage, error) {
	pending, err := s.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: probeStreamKey, Group: probeGroup, Idle: s.claimMinIdle,
		Start: "-", End: "+", Count: count,
	}).Result()
	if err != nil || len(pending) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(pending))
	for _, item := range pending {
		ids = append(ids, item.ID)
	}
	return s.client.XClaim(ctx, &redis.XClaimArgs{
		Stream: probeStreamKey, Group: probeGroup, Consumer: consumer,
		MinIdle: s.claimMinIdle, Messages: ids,
	}).Result()
}

func (s *RedisTaskStore) processMessage(ctx context.Context, message redis.XMessage) {
	id, ok := message.Values["operationId"].(string)
	if !ok || id == "" {
		_, _ = s.client.XAck(ctx, probeStreamKey, probeGroup, message.ID).Result()
		return
	}
	task, err := s.Get(ctx, id)
	if err != nil {
		_, _ = s.client.XAck(ctx, probeStreamKey, probeGroup, message.ID).Result()
		return
	}
	if task.Status == TaskCompleted || task.Status == TaskFailed {
		_, _ = s.client.XAck(ctx, probeStreamKey, probeGroup, message.ID).Result()
		return
	}
	if !task.DeadlineAt.IsZero() && !time.Now().UTC().Before(task.DeadlineAt) {
		task.Status = TaskFailed
		task.Error = "视频探针任务已超过截止时间"
		completed := time.Now().UTC()
		task.CompletedAt = &completed
		_ = s.update(context.Background(), task)
		_, _ = s.client.XAck(context.Background(), probeStreamKey, probeGroup, message.ID).Result()
		return
	}
	now := time.Now().UTC()
	task.Status = TaskSampling
	task.StartedAt = &now
	if err := s.update(ctx, task); err != nil {
		// Keep the message pending so a later worker can retry after Redis recovers.
		return
	}
	runCtx := context.Background()
	var cancel context.CancelFunc
	if !task.DeadlineAt.IsZero() {
		runCtx, cancel = context.WithDeadline(context.Background(), task.DeadlineAt)
		defer cancel()
	}
	snapshot, runErr := s.service.Run(runCtx, task.StreamID, task.DurationMS)
	completed := time.Now().UTC()
	task.CompletedAt = &completed
	if runErr != nil {
		task.Status = TaskFailed
		task.Error = runErr.Error()
	} else {
		task.Status = TaskCompleted
		task.Snapshot = snapshot
	}
	if err := s.update(context.Background(), task); err != nil {
		// Do not ACK a result that was not durably stored. The pending claim path
		// will retry it after the configured idle lease.
		return
	}
	_, _ = s.client.XAck(context.Background(), probeStreamKey, probeGroup, message.ID).Result()
}
