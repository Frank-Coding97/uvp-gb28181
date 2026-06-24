package stream

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestWaitReadyHookFirst T6.2-测1: hook 先到 → 立即返回
func TestWaitReadyHookFirst(t *testing.T) {
	n := NewNotifier()
	pollCalls := atomic.Int32{}
	poll := func(ctx context.Context) (bool, error) {
		pollCalls.Add(1)
		return false, nil
	}

	// 50ms 后由 hook 触发
	go func() {
		time.Sleep(50 * time.Millisecond)
		n.Publish("stream-A")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := WaitReady(ctx, n, "stream-A", poll, 200*time.Millisecond); err != nil {
		t.Fatalf("应通过 hook 触发,err=%v", err)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Errorf("hook 触发应快返回,实际 %v", elapsed)
	}
	// 立即一次轮询无论怎样都会跑,hook 后第一个 ticker(200ms)前 hook 已到,所以 polls<=1
	if pollCalls.Load() > 1 {
		t.Errorf("hook 先到不应触发额外轮询,polls=%d", pollCalls.Load())
	}
}

// TestWaitReadyPollFirst T6.2-测2: hook 不来,轮询发现 → 仍能就绪
func TestWaitReadyPollFirst(t *testing.T) {
	n := NewNotifier()
	pollReady := atomic.Bool{}
	pollCalls := atomic.Int32{}
	poll := func(ctx context.Context) (bool, error) {
		pollCalls.Add(1)
		return pollReady.Load(), nil
	}

	// 250ms 后让轮询返 true(此时 hook 永不来)
	go func() {
		time.Sleep(250 * time.Millisecond)
		pollReady.Store(true)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := WaitReady(ctx, n, "stream-B", poll, 100*time.Millisecond); err != nil {
		t.Fatalf("应通过轮询发现就绪,err=%v", err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("应至少等到第一次轮询命中,实际 %v", elapsed)
	}
	if pollCalls.Load() < 2 {
		t.Errorf("应至少 2 次轮询(立即 + 1 次 ticker),polls=%d", pollCalls.Load())
	}
}

// TestWaitReadyTimeout T6.2-测3: 都不到 → 超时
func TestWaitReadyTimeout(t *testing.T) {
	n := NewNotifier()
	poll := func(ctx context.Context) (bool, error) { return false, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := WaitReady(ctx, n, "stream-C", poll, 100*time.Millisecond)
	if err == nil {
		t.Fatal("应超时")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("应为 DeadlineExceeded,实际 %v", err)
	}
}

// TestWaitReadyPollErrorIgnored T6.2-测4: 轮询临时报错不致命,继续等
func TestWaitReadyPollErrorIgnored(t *testing.T) {
	n := NewNotifier()
	calls := atomic.Int32{}
	poll := func(ctx context.Context) (bool, error) {
		c := calls.Add(1)
		if c < 3 {
			return false, errors.New("模拟网络错误")
		}
		return true, nil // 第 3 次返就绪
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitReady(ctx, n, "stream-D", poll, 50*time.Millisecond); err != nil {
		t.Fatalf("轮询临时错误应忽略,最终就绪;err=%v", err)
	}
}

// TestNotifierPublishBeforeSubscribe T6.2-测5: Publish 在 Subscribe 前 → 信号丢(语义文档化)
func TestNotifierPublishBeforeSubscribe(t *testing.T) {
	n := NewNotifier()
	n.Publish("stream-X") // 无订阅者,丢弃
	ch := n.Subscribe("stream-X")
	select {
	case <-ch:
		t.Error("Publish 应在无订阅者时丢弃,Subscribe 不应立即可读")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestNotifierConcurrent T6.2-测6: 并发 Sub/Pub/Unsub 无 race
func TestNotifierConcurrent(t *testing.T) {
	n := NewNotifier()
	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func(i int) {
			id := string(rune('a' + i))
			ch := n.Subscribe(id)
			n.Publish(id)
			<-ch
			n.Unsubscribe(id)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}
