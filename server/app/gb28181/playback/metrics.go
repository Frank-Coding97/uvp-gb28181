package playback

import "sync/atomic"

type Metrics struct {
	Created atomic.Int64
	Actions atomic.Int64
	Ended   atomic.Int64
	Failed  atomic.Int64
	Cleaned atomic.Int64
}

type MetricsSnapshot struct {
	Created int64
	Actions int64
	Ended   int64
	Failed  int64
	Cleaned int64
	Active  int
}

func (m *Metrics) Snapshot(active int) MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{Active: active}
	}
	return MetricsSnapshot{
		Created: m.Created.Load(), Actions: m.Actions.Load(), Ended: m.Ended.Load(),
		Failed: m.Failed.Load(), Cleaned: m.Cleaned.Load(), Active: active,
	}
}
