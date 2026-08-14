package stream

import (
	"context"
	"sync"
	"time"
)

// Notifier 流就绪事件分发器(hook 收到 on_stream_changed regist=true 时 publish)
// 同一 streamID 支持多等待者:每个 Subscribe 拿到独立 channel,Publish 广播唤醒全部
type Notifier struct {
	mu   sync.Mutex
	subs map[string][]chan struct{}
}

// NewNotifier 创建分发器
func NewNotifier() *Notifier {
	return &Notifier{subs: make(map[string][]chan struct{})}
}

// Subscribe 订阅一个 streamID 的就绪事件,返回只读 channel(buffered=1)
// 若已被 Publish 过,channel 会立即可读
// 调用方负责 Unsubscribe(streamID, ch) 释放
func (n *Notifier) Subscribe(streamID string) <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	ch := make(chan struct{}, 1)
	n.subs[streamID] = append(n.subs[streamID], ch)
	return ch
}

// Unsubscribe 释放订阅(WaitReady 退出时调用,避免泄漏)
func (n *Notifier) Unsubscribe(streamID string, target <-chan struct{}) {
	n.mu.Lock()
	defer n.mu.Unlock()
	list := n.subs[streamID]
	for i, ch := range list {
		if ch == target {
			n.subs[streamID] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(n.subs[streamID]) == 0 {
		delete(n.subs, streamID)
	}
}

// Publish 发布就绪事件;广播给该 streamID 的所有订阅者
// 重复 Publish 安全(channel buffered=1,非阻塞写)
func (n *Notifier) Publish(streamID string) {
	n.mu.Lock()
	list := append([]chan struct{}(nil), n.subs[streamID]...)
	n.mu.Unlock()
	for _, ch := range list {
		select {
		case ch <- struct{}{}:
		default: // 已经有信号在 buffer 里,丢弃即可
		}
	}
}

// PollFn 轮询函数:返回 (ready, err);err 不致命(网络抖动)继续轮询
type PollFn func(ctx context.Context) (bool, error)

type PollRefFn func(ctx context.Context, ref LiveRef) (bool, error)

// WaitReady 双源等待流就绪:hook channel 与 polling 取早(ADR-002 创新3)
//   - hook 通常 100-500ms 内到,正常路径
//   - polling 200ms 间隔兜底,hook 丢失/延迟时仍可成功
//   - 任一信号宣告就绪;超时则返回 ctx.Err
//
// 调用方应在退出时 Unsubscribe(本函数已包,但只 Subscribe 一次)
func WaitReady(ctx context.Context, n *Notifier, streamID string, poll PollFn, pollInterval time.Duration) error {
	hookCh := n.Subscribe(streamID)
	defer n.Unsubscribe(streamID, hookCh)

	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// 立即先轮询一次:hook 可能在 Subscribe 前已经发(竞态);流也可能因复用提前就绪
	if poll != nil {
		if ok, _ := poll(ctx); ok {
			return nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-hookCh:
			return nil
		case <-ticker.C:
			if poll == nil {
				continue
			}
			ok, _ := poll(ctx) // err 视为暂未就绪,继续轮询直到 ctx 超时
			if ok {
				return nil
			}
		}
	}
}

// WaitReadyRef treats a hook as a wake-up signal only. Readiness succeeds only
// after the caller verifies the exact generation and target node represented
// by ref.
func WaitReadyRef(ctx context.Context, n *Notifier, ref LiveRef, poll PollRefFn, pollInterval time.Duration) error {
	hookCh := n.Subscribe(ref.StreamID)
	defer n.Unsubscribe(ref.StreamID, hookCh)

	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	check := func() bool {
		if poll == nil {
			return false
		}
		ok, _ := poll(ctx, ref)
		return ok
	}
	if check() {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-hookCh:
			if check() {
				return nil
			}
		case <-ticker.C:
			if check() {
				return nil
			}
		}
	}
}
