package streamprobe

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

func newRedisTaskStore(t *testing.T, service *Service) (*RedisTaskStore, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisTaskStore(client, service), server
}

func waitForTaskStatus(t *testing.T, store *RedisTaskStore, operationID string, status TaskStatus) *Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := store.Get(context.Background(), operationID)
		if err == nil && task.Status == status {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach %s", operationID, status)
	return nil
}

func TestRedisTaskStoreCreatesAndDeduplicatesQueuedProbe(t *testing.T) {
	store, _ := newRedisTaskStore(t, newProbeService(&countingProbeClient{}))
	first, err := store.Create(context.Background(), "stream", 10000)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(context.Background(), "stream", 10000)
	if err != nil {
		t.Fatal(err)
	}
	if first.OperationID != second.OperationID {
		t.Fatalf("operation ids differ: %s != %s", first.OperationID, second.OperationID)
	}
	if first.Status != TaskQueued {
		t.Fatalf("status=%s", first.Status)
	}
}

func TestRedisTaskWorkerCompletesProbeAndStoresResult(t *testing.T) {
	client := &countingProbeClient{}
	store, _ := newRedisTaskStore(t, newProbeService(client))
	task, err := store.Create(context.Background(), "stream", 3000)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); store.RunWorker(ctx, "worker-1", 1) }()
	completed := waitForTaskStatus(t, store, task.OperationID, TaskCompleted)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not stop")
	}
	if completed.Snapshot == nil {
		t.Fatal("snapshot is nil")
	}
	if client.calls.Load() != 1 {
		t.Fatalf("calls=%d", client.calls.Load())
	}
}

func TestRedisTaskWorkerFailsExpiredTaskWithoutRunningProbe(t *testing.T) {
	client := &countingProbeClient{}
	store, _ := newRedisTaskStore(t, newProbeService(client))
	task, err := store.Create(context.Background(), "stream", 3000)
	if err != nil {
		t.Fatal(err)
	}
	task.DeadlineAt = time.Now().Add(-time.Second)
	if err := store.update(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); store.RunWorker(ctx, "worker-expired", 1) }()
	failed := waitForTaskStatus(t, store, task.OperationID, TaskFailed)
	cancel()
	<-done
	if failed.Error == "" {
		t.Fatal("expired task has no error")
	}
	if client.calls.Load() != 0 {
		t.Fatalf("expired task ran probe %d times", client.calls.Load())
	}
}

func TestRedisTaskWorkerReclaimsStalePendingMessage(t *testing.T) {
	client := &countingProbeClient{}
	store, server := newRedisTaskStore(t, newProbeService(client))
	store.claimMinIdle = time.Second
	task, err := store.Create(context.Background(), "stream", 3000)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ensureGroup(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := store.client.XReadGroup(context.Background(), &redis.XReadGroupArgs{
		Group: probeGroup, Consumer: "dead-worker", Streams: []string{probeStreamKey, ">"}, Count: 1,
	}).Result()
	if err != nil || len(entries) != 1 || len(entries[0].Messages) != 1 {
		t.Fatalf("make pending entry: entries=%v err=%v", entries, err)
	}
	server.FastForward(2 * time.Second)
	store.claimMinIdle = 0
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); store.RunWorker(ctx, "worker-recovery", 1) }()
	waitForTaskStatus(t, store, task.OperationID, TaskCompleted)
	cancel()
	<-done
	if client.calls.Load() != 1 {
		t.Fatalf("calls=%d", client.calls.Load())
	}
}

func TestRedisTaskStoreExpiresCompletedTask(t *testing.T) {
	store, server := newRedisTaskStore(t, newProbeService(&countingProbeClient{}))
	store.ttl = time.Minute
	task, err := store.Create(context.Background(), "stream", 3000)
	if err != nil {
		t.Fatal(err)
	}
	server.FastForward(2 * time.Minute)
	if _, err := store.Get(context.Background(), task.OperationID); err != ErrTaskNotFound {
		t.Fatalf("err=%v", err)
	}
}
