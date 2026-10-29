package heartbeat_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/heartbeat"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

// 现场（2026-10-06，192.168.10.220:18090）：平台比 ZLM 晚接入，节点建好后
// "最后心跳"恒为「从未上报」，而同��台 ZLM 的 on_stream_changed 正常到达平台。
// 根因在 ZLM 侧：reportServerKeepalive()（server/WebHook.cpp）只在 installWebHook()
// 里被调用一次，且要求那一刻 hook.on_server_keepalive 非空才建 g_keepalive_timer ——
// 定时器不存在，之后改 URL（热生效）也永远救不回来。
//
// 因此平台的存活证据不能只认对端心跳：自己周期主动探测成功一次就是等价的证据。

func TestThreadLoadPoller_ProbeSuccess_RefreshesLiveness_ForNodeThatNeverReportsHeartbeat(t *testing.T) {
	reg, _ := setupRegistry(t, "uuid-late")

	// 前置事实：这个节点从未收到过任何 keepalive（LastHeartbeatAt 为零值），
	// 正是「从未上报」在界面上的来源。
	before, ok := reg.GetByUUID("uuid-late")
	require.True(t, ok)
	require.True(t, before.Stats.LastHeartbeatAt.IsZero(),
		"前置条件：本用例针对的就是 LastHeartbeatAt 为零值的节点")

	f := newFakeFetcher()
	f.netReturn["uuid-late"] = 0.4
	f.wrkReturn["uuid-late"] = 0.3

	beforeTick := time.Now()
	p := heartbeat.NewThreadLoadPoller(reg, f, 30*time.Second)
	p.Tick(context.Background())

	require.Eventually(t, func() bool {
		got, _ := reg.GetByUUID("uuid-late")
		return got != nil && !got.Stats.LastHeartbeatAt.IsZero()
	}, 2*time.Second, 10*time.Millisecond,
		"主动探测成功即应刷新 LastHeartbeatAt，不能让健康节点停在「从未上报」")

	got, _ := reg.GetByUUID("uuid-late")
	require.False(t, got.Stats.LastHeartbeatAt.Before(beforeTick),
		"存活时间应被刷到本次探测之后")
}

func TestThreadLoadPoller_ProbeSuccess_DoesNotZeroStreamCounts(t *testing.T) {
	// ⛔ 探测通道不携带 MediaSource/Session 计数，绝不能把它们清零 ——
	// 否则「当前流数」会在两次心跳之间凭空归零（比原bug 更难查）。
	reg, _ := setupRegistry(t, "uuid-cnt")
	reg.UpdateStats("uuid-cnt", node.Stats{
		LastHeartbeatAt:  time.Now().Add(-time.Minute),
		MediaSourceCount: 7,
		SessionCount:     5,
	})

	f := newFakeFetcher()
	f.netReturn["uuid-cnt"] = 0.4
	f.wrkReturn["uuid-cnt"] = 0.3

	p := heartbeat.NewThreadLoadPoller(reg, f, 30*time.Second)
	p.Tick(context.Background())

	require.Eventually(t, func() bool {
		got, _ := reg.GetByUUID("uuid-cnt")
		return got != nil && got.Stats.NetThreadLoadAvg > 0
	}, 2*time.Second, 10*time.Millisecond)

	got, _ := reg.GetByUUID("uuid-cnt")
	require.Equal(t, 7, got.Stats.MediaSourceCount, "探测不得清零 MediaSourceCount")
	require.Equal(t, 5, got.Stats.SessionCount, "探测不得清零 SessionCount")
}

func TestThreadLoadPoller_ProbeFailure_LeavesLivenessUntouched(t *testing.T) {
	// 探测失败**不能**被当成存活：否则节点真的挂了也会一直显示心跳正常。
	// 这里钉住"失败不刷新"，与成功刷新构成对照。
	reg, _ := setupRegistry(t, "uuid-down")
	seed := time.Now().Add(-2 * time.Minute)
	reg.UpdateStats("uuid-down", node.Stats{LastHeartbeatAt: seed})

	f := newFakeFetcher()
	f.netErr["uuid-down"] = errors.New("connection refused")
	f.wrkErr["uuid-down"] = errors.New("connection refused")

	p := heartbeat.NewThreadLoadPoller(reg, f, 30*time.Second)
	p.Tick(context.Background())
	time.Sleep(100 * time.Millisecond)

	got, _ := reg.GetByUUID("uuid-down")
	require.WithinDuration(t, seed, got.Stats.LastHeartbeatAt, time.Millisecond,
		"探测失败不得刷新存活时间")
}

func TestRegistry_UpdateLivenessAt_NeverMovesBackward(t *testing.T) {
	// 两条存活证据通道各有延迟，并发到达时较早的那条不能把时间戳拽回旧值，
	// 否则 Watcher 可能读到比实际更旧的LastHeartbeatAt 而误判离线。
	reg, _ := setupRegistry(t, "uuid-order")
	newer := time.Now()
	reg.UpdateLivenessAt("uuid-order", newer)
	reg.UpdateLivenessAt("uuid-order", newer.Add(-10*time.Minute))

	got, _ := reg.GetByUUID("uuid-order")
	require.WithinDuration(t, newer, got.Stats.LastHeartbeatAt, time.Millisecond,
		"存活时间只应前进，不应后退")
}

func TestRegistry_UpdateLivenessAt_RevivesOfflineNode(t *testing.T) {
	// 只缺心跳回调的活节点会被 Watcher 判offline；探测通了就该被救回来，
	// 与 Collector 心跳通道保持同一语义（否则 DB 里的 offline 永不自愈）。
	reg, id := setupRegistry(t, "uuid-revive")
	require.NoError(t, reg.MarkOffline(context.Background(), id))

	reg.UpdateLivenessAt("uuid-revive", time.Now())

	got, ok := reg.GetByUUID("uuid-revive")
	require.True(t, ok)
	require.Equal(t, node.StateActive, got.State, "探测成功后应自动翻回 active")
}

func TestRegistry_UpdateLivenessAt_IgnoresUnknownUUID(t *testing.T) {
	reg, _ := setupRegistry(t, "uuid-known")
	require.NotPanics(t, func() {
		reg.UpdateLivenessAt("uuid-never-registered", time.Now())
	})
	got, ok := reg.GetByUUID("uuid-known")
	require.True(t, ok)
	require.True(t, got.Stats.LastHeartbeatAt.IsZero(),
		"未知 uuid 不应影响任何已注册节点")
}