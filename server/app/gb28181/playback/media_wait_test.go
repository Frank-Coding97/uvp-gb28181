package playback

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type deadlineRecorder struct{ budget time.Duration }

func (f *deadlineRecorder) Wait(ctx context.Context, _ string) (MediaReady, error) {
	if deadline, ok := ctx.Deadline(); ok {
		f.budget = time.Until(deadline)
	}
	return MediaReady{URLs: map[string]string{"wsFlv": "ws://node/rtp/live.flv"}}, nil
}

// ⛔⛔ 回归：媒体等待预算是**固定值**，不随回放窗口长短变化。
//
// 曾经按「窗口越长等越久」放宽过（30 分钟窗口 35 秒），那是伪相关：
// 国标回放 S=Play + RANGE 是从起点按 1x 顺序推，终点有多远不影响首包到达。
// 谁再引入窗口相关的放宽，这个用例会直接失败。
func TestPlaybackServiceCreateUsesFixedMediaWaitBudget(t *testing.T) {
	now := time.Unix(1700000000, 0)
	cases := []struct {
		name string
		span time.Duration
	}{
		{"4 分钟窗口", 4 * time.Minute},
		{"30 分钟窗口", 30 * time.Minute},
		{"6 小时窗口", 6 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			media := &deadlineRecorder{}
			service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}),
				&fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}},
				&fakeRTP{}, &fakeInvite{}, media,
				ServiceConfig{ServerID: "34020000002000000001", MediaWait: 15 * time.Second})
			request := validCreate(now)
			request.SIPChannelID = "34020000001320000001"
			request.SegmentEnd = request.SegmentStart.Add(tc.span)
			if _, err := service.Create(context.Background(), request); err != nil {
				t.Fatalf("create: %v", err)
			}
			// 固定 15 秒，允许 1 秒调度抖动。
			if media.budget < 14*time.Second || media.budget > 15*time.Second {
				t.Fatalf("media wait budget = %s, want ~15s regardless of segment span", media.budget)
			}
		})
	}
}

// firstCallTimesOut 第一次收流超时，之后正常就绪——用来验证「超时后通道已被
// 释放、能立刻重试」。
type firstCallTimesOut struct{ calls atomic.Int32 }

func (f *firstCallTimesOut) Wait(ctx context.Context, _ string) (MediaReady, error) {
	if f.calls.Add(1) == 1 {
		return MediaReady{}, context.DeadlineExceeded
	}
	return MediaReady{URLs: map[string]string{"wsFlv": "ws://node/rtp/live.flv"}}, nil
}

// 收流超时（设备在预算内没回传媒体）判定失败，并且**当场拆掉会话**：
// BYE 拆信令、关 RTP 端口、解绑流，通道立刻可重试，不留幽灵会话。
func TestPlaybackServiceMediaWaitTimeoutCleansUpSession(t *testing.T) {
	now := time.Unix(1700000000, 0)
	rtp := &fakeRTP{}
	invite := &fakeInvite{}
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}},
		rtp, invite, &firstCallTimesOut{},
		ServiceConfig{ServerID: "34020000002000000001", MediaWait: 15 * time.Second})
	request := validCreate(now)
	request.SIPChannelID = "34020000001320000001"

	_, err := service.Create(context.Background(), request)

	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Stage != "media_wait" || serviceErr.Code != "timeout" {
		t.Fatalf("err = %v, want media_wait/timeout ServiceError", err)
	}
	if rtp.closeCalls.Load() == 0 || rtp.unbindCalls.Load() == 0 {
		t.Fatalf("收流超时后未释放 RTP 端口: close=%d unbind=%d", rtp.closeCalls.Load(), rtp.unbindCalls.Load())
	}
	if invite.teardown.Load() == 0 {
		t.Fatalf("收流超时后未拆除设备侧会话(BYE)")
	}
	// 通道必须已经释放：同一通道再点一次要能直接成功，而不是 ErrPlaybackBusy。
	if _, err := service.Create(context.Background(), request); err != nil {
		t.Fatalf("收流超时后通道未释放，重试失败: %v", err)
	}
}

// blockingMedia 在进入等待时发信号，然后一直阻塞到上下文结束。
type blockingMedia struct{ entered chan struct{} }

func (b *blockingMedia) Wait(ctx context.Context, _ string) (MediaReady, error) {
	close(b.entered)
	<-ctx.Done()
	return MediaReady{}, ctx.Err()
}

// 收流超时的拆除不能借用请求上下文：前端请求超时后 ctx 会被取消，若拿它去
// BYE/关端口会当场失败，只在库里留一条清理未完成的幽灵会话。
func TestPlaybackServiceMediaWaitCleansUpAfterClientGone(t *testing.T) {
	now := time.Unix(1700000000, 0)
	rtp := &fakeRTP{}
	invite := &fakeInvite{}
	media := &blockingMedia{entered: make(chan struct{})}
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}},
		rtp, invite, media,
		ServiceConfig{ServerID: "34020000002000000001", MediaWait: 15 * time.Second})
	request := validCreate(now)
	request.SIPChannelID = "34020000001320000001"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := service.Create(ctx, request)
		done <- err
	}()
	<-media.entered
	cancel() // 模拟浏览器在服务端等媒体期间已经断开
	<-done

	if rtp.closeCalls.Load() == 0 || invite.teardown.Load() == 0 {
		t.Fatalf("请求上下文已取消时仍未拆除: close=%d teardown=%d", rtp.closeCalls.Load(), invite.teardown.Load())
	}
}
