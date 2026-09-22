package handler

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 现场原型：220 上一个已从平台移除的 ZLM 实例，心跳仍在回调平台。
// 折叠规则必须满足两点：窗口内只留一条明细；窗口到期时把折叠掉的条数交代清楚。
func TestHookRejectionTrackerFoldsChronicRepeat(t *testing.T) {
	tracker := newHookRejectionTracker()
	key := hookRejectionKey{
		reason:    "node_unknown",
		nodeID:    "56e37a2a-41ff-4346-832b-58c24ae93911",
		sourceIP:  "192.168.10.220",
		hookEvent: "on_server_keepalive",
	}
	base := time.Date(2026, 9, 21, 15, 30, 0, 0, time.Local)

	verdict, record := tracker.observe(key, base)
	require.Equal(t, HookRejectionFirst, verdict)
	require.Equal(t, 0, record.suppressed)

	// 10s 心跳打到窗口边界之前：全部静默
	suppressed := 0
	for offset := 10 * time.Second; offset < hookRejectionWindow; offset += 10 * time.Second {
		verdict, _ = tracker.observe(key, base.Add(offset))
		require.Equal(t, HookRejectionSuppressed, verdict)
		suppressed++
	}
	require.Equal(t, 179, suppressed, "30 分钟窗口内 10s 一条 => 179 条被折叠")

	// 窗口到期：一条汇总，suppressed_count 就是被折叠掉的条数（不含那条明细）
	verdict, record = tracker.observe(key, base.Add(hookRejectionWindow))
	require.Equal(t, HookRejectionSummary, verdict)
	require.Equal(t, 179, record.suppressed)
	require.Equal(t, base, record.firstSeen)
	require.Equal(t, base.Add(hookRejectionWindow-10*time.Second), record.lastSeen)

	// 本次同时开新窗口，但不再另打一条明细
	verdict, record = tracker.observe(key, base.Add(hookRejectionWindow+10*time.Second))
	require.Equal(t, HookRejectionSuppressed, verdict)
	require.Equal(t, 1, record.suppressed)
}

// 折叠必须按「谁 + 为什么」分桶：不同节点、不同原因、不同 hook 各有自己的首次明细，
// 否则一个新的陌生来源会被另一个来源的历史静默掉。
func TestHookRejectionTrackerKeepsDistinctSourcesApart(t *testing.T) {
	tracker := newHookRejectionTracker()
	base := time.Date(2026, 9, 21, 15, 30, 0, 0, time.Local)
	first := hookRejectionKey{reason: "node_unknown", nodeID: "node-a", sourceIP: "192.168.10.220", hookEvent: "on_server_keepalive"}
	variants := map[string]hookRejectionKey{
		"another node":    {reason: "node_unknown", nodeID: "node-b", sourceIP: "192.168.10.220", hookEvent: "on_server_keepalive"},
		"another reason":  {reason: "capability_invalid", nodeID: "node-a", sourceIP: "192.168.10.220", hookEvent: "on_server_keepalive"},
		"another peer":    {reason: "node_unknown", nodeID: "node-a", sourceIP: "10.0.0.9", hookEvent: "on_server_keepalive"},
		"another hook":    {reason: "node_unknown", nodeID: "node-a", sourceIP: "192.168.10.220", hookEvent: "on_play"},
		"no node claimed": {reason: "node_unknown", nodeID: "", sourceIP: "192.168.10.220", hookEvent: "on_server_keepalive"},
	}

	require.Equal(t, HookRejectionFirst, observeVerdict(tracker, first, base))
	for name, key := range variants {
		require.Equal(t, HookRejectionFirst, observeVerdict(tracker, key, base), name)
		// 同键第二次才静默，证明上面那条确实是这个键的首次
		require.Equal(t, HookRejectionSuppressed, observeVerdict(tracker, key, base.Add(time.Second)), name)
	}
	require.Equal(t, 1+len(variants), tracker.tracked())
}

// 键里的 node_id / source_ip 由调用方自报：伪造一批随机 node_id 不能把表撑爆。
func TestHookRejectionTrackerBoundsItsTable(t *testing.T) {
	tracker := newHookRejectionTracker()
	base := time.Date(2026, 9, 21, 15, 30, 0, 0, time.Local)

	for index := 0; index < hookRejectionMaxTracked*8; index++ {
		key := hookRejectionKey{
			reason:   "node_unknown",
			nodeID:   fmt.Sprintf("spoofed-%d", index),
			sourceIP: "203.0.113.7",
		}
		// 每个来源只来一次，是最坏情况：没有窗口内的重复可供折叠
		require.Equal(t, HookRejectionFirst, observeVerdict(tracker, key, base.Add(time.Duration(index)*time.Millisecond)))
	}
	require.LessOrEqual(t, tracker.tracked(), hookRejectionMaxTracked)

	// 淘汰只针对最旧的四分之一，最近出现过的来源仍在表里、仍会被折叠
	latestIndex := hookRejectionMaxTracked*8 - 1
	latest := hookRejectionKey{reason: "node_unknown", nodeID: fmt.Sprintf("spoofed-%d", latestIndex), sourceIP: "203.0.113.7"}
	require.Equal(t, HookRejectionSuppressed, observeVerdict(tracker, latest, base.Add(time.Duration(latestIndex)*time.Millisecond+time.Second)))
}

// 认证器没装配（中间件本身坏了）时不折叠：那条路径宁可多打。
func TestHookRejectionTrackerWithoutTrackerAlwaysLogs(t *testing.T) {
	var tracker *hookRejectionTracker
	key := hookRejectionKey{reason: "auth_runtime_unavailable", nodeID: "node-a", sourceIP: "192.168.10.1"}
	base := time.Date(2026, 9, 21, 15, 30, 0, 0, time.Local)
	for index := 0; index < 3; index++ {
		verdict, record := tracker.observe(key, base.Add(time.Duration(index)*time.Second))
		require.Equal(t, HookRejectionFirst, verdict)
		require.Equal(t, 0, record.suppressed)
	}
	require.Equal(t, 0, tracker.tracked())
}

func observeVerdict(tracker *hookRejectionTracker, key hookRejectionKey, now time.Time) HookRejectionVerdict {
	verdict, _ := tracker.observe(key, now)
	return verdict
}

// 折叠只针对**对方造成的**拒绝：平台自身故障（auth_runtime_unavailable）与
// 尚未判过的新 reason_code 一律按次打，不许被"折叠"顺手变安静。
func TestHookRejectionFoldableCoversPeerCausedReasonsOnly(t *testing.T) {
	for _, reason := range []string{"credentials_invalid", "node_unknown", "capability_invalid"} {
		require.True(t, hookRejectionFoldable[reason], reason)
	}
	for _, reason := range []string{"auth_runtime_unavailable", "brand_new_reason"} {
		require.False(t, hookRejectionFoldable[reason], reason)
	}
}
