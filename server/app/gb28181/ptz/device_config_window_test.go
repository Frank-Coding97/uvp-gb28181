package ptz

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 后台排队窗口必须覆盖"万级首轮注册"的积压，否则会重现 86% 排队超时。
//
// 这个用例锁的是**不变量**而不是某个具体数字：窗口 ≥ 积压 / 调度吞吐。
// 调度吞吐由在途槽位数决定（一条指令占一个槽位，等设备 200 OK 实测约 2 秒），
// 所以要么把窗口调到够用，要么把 defaultSchedulerCapacity 提到够用 —— 两条路都合法，
// 但两者必须自洽。2026-11-09 之前是 capacity=16 + 窗口 60 秒：
// 吞吐 8 条/秒、2 万条要 41 分钟，窗口 60 秒 ⇒ 17291 条（86%）被判 PTZ_QUEUE_TIMEOUT。
func TestBackgroundQueueWindowCoversTenThousandDeviceBurst(t *testing.T) {
	// 一万台设备的固定工作量：每台 1 条 BasicParam（设备级）+ 每通道 1 条 VideoParamOpt。
	const tenThousandDeviceBurst = 20000
	// 单条指令占用发送槽位的时长（设备回 200 OK）实测约 2 秒。
	const perOperationSlot = 2 * time.Second

	drain := time.Duration(tenThousandDeviceBurst) * perOperationSlot / time.Duration(defaultSchedulerCapacity)
	require.GreaterOrEqual(t, DeviceConfigBackgroundQueueWindow, drain,
		"后台排队窗口必须覆盖万级首轮注册的积压，否则设备配置会大面积被判排队超时")
}

// 交互式窗口不能被后台窗口带跑：页面上有人等结果时，"明确失败"比干等更有用。
func TestInteractiveQueueWindowStaysShort(t *testing.T) {
	require.Equal(t, 5*time.Second, defaultQueueWindow)
	require.Greater(t, DeviceConfigBackgroundQueueWindow, defaultQueueWindow)
}
