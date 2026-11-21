package playback

import "sync/atomic"

type Metrics struct {
	Created atomic.Int64
	Actions atomic.Int64
	Ended   atomic.Int64
	Failed  atomic.Int64
	Cleaned atomic.Int64
	// CleanupAbandoned 统计设备侧拆除重试耗尽后被强制释放的会话数。
	// 这些会话的平台侧资源已经释放(因此也计入 Cleaned),但设备从未确认拆除,
	// 单独计数用于识别"长期不应答 BYE/TEARDOWN"的不良设备。
	CleanupAbandoned atomic.Int64
}

type MetricsSnapshot struct {
	Created          int64
	Actions          int64
	Ended            int64
	Failed           int64
	Cleaned          int64
	CleanupAbandoned int64
	Active           int
}

func (m *Metrics) Snapshot(active int) MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{Active: active}
	}
	return MetricsSnapshot{
		Created: m.Created.Load(), Actions: m.Actions.Load(), Ended: m.Ended.Load(),
		Failed: m.Failed.Load(), Cleaned: m.Cleaned.Load(),
		CleanupAbandoned: m.CleanupAbandoned.Load(), Active: active,
	}
}
