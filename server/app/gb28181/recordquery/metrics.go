package recordquery

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type MetricOutcome string

const (
	MetricOutcomeComplete    MetricOutcome = "complete"
	MetricOutcomeEmpty       MetricOutcome = "empty"
	MetricOutcomePartial     MetricOutcome = "partial"
	MetricOutcomeCapacity    MetricOutcome = "capacity"
	MetricOutcomeTimeout     MetricOutcome = "timeout"
	MetricOutcomeCanceled    MetricOutcome = "canceled"
	MetricOutcomeBusy        MetricOutcome = "busy"
	MetricOutcomeSendFailed  MetricOutcome = "send_failed"
	MetricOutcomeUnavailable MetricOutcome = "unavailable"
	MetricOutcomeFailed      MetricOutcome = "failed"
)

var AllMetricOutcomes = []MetricOutcome{
	MetricOutcomeComplete, MetricOutcomeEmpty, MetricOutcomePartial, MetricOutcomeCapacity,
	MetricOutcomeTimeout, MetricOutcomeCanceled, MetricOutcomeBusy, MetricOutcomeSendFailed,
	MetricOutcomeUnavailable, MetricOutcomeFailed,
}

type MetricsSnapshot struct {
	Active   int64
	Total    int64
	Duration time.Duration
	Records  int64
	Outcomes map[MetricOutcome]int64
}

type Metrics struct {
	active        atomic.Int64
	total         atomic.Int64
	durationNanos atomic.Int64
	records       atomic.Int64
	outcomes      [10]atomic.Int64
}

func NewMetrics() *Metrics { return &Metrics{} }

func (m *Metrics) Begin() func(QueryStatus, ErrorCode, error, int, time.Duration) {
	if m == nil {
		return func(QueryStatus, ErrorCode, error, int, time.Duration) {}
	}
	m.active.Add(1)
	var once sync.Once
	return func(status QueryStatus, reason ErrorCode, err error, records int, duration time.Duration) {
		once.Do(func() {
			m.active.Add(-1)
			m.total.Add(1)
			if duration > 0 {
				m.durationNanos.Add(int64(duration))
			}
			if records > 0 {
				m.records.Add(int64(records))
			}
			m.outcomes[metricOutcomeIndex(classifyMetricOutcome(status, reason, err))].Add(1)
		})
	}
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	result := MetricsSnapshot{Outcomes: make(map[MetricOutcome]int64, len(AllMetricOutcomes))}
	if m == nil {
		return result
	}
	result.Active = m.active.Load()
	result.Total = m.total.Load()
	result.Duration = time.Duration(m.durationNanos.Load())
	result.Records = m.records.Load()
	for _, outcome := range AllMetricOutcomes {
		result.Outcomes[outcome] = m.outcomes[metricOutcomeIndex(outcome)].Load()
	}
	return result
}

func classifyMetricOutcome(status QueryStatus, reason ErrorCode, err error) MetricOutcome {
	switch reason {
	case ErrorCodeCapacity:
		return MetricOutcomeCapacity
	case ErrorCodeTimeout:
		return MetricOutcomeTimeout
	}
	var queryErr *QueryError
	if errors.As(err, &queryErr) {
		switch queryErr.Code {
		case ErrorCodeCapacity:
			return MetricOutcomeCapacity
		case ErrorCodeTimeout:
			return MetricOutcomeTimeout
		case ErrorCodeBusy:
			return MetricOutcomeBusy
		case ErrorCodeSendFailed:
			return MetricOutcomeSendFailed
		case ErrorCodeUnavailable:
			return MetricOutcomeUnavailable
		}
	}
	switch status {
	case QueryStatusComplete:
		return MetricOutcomeComplete
	case QueryStatusEmpty:
		return MetricOutcomeEmpty
	case QueryStatusPartial:
		return MetricOutcomePartial
	case QueryStatusTimeout:
		return MetricOutcomeTimeout
	case QueryStatusCanceled:
		return MetricOutcomeCanceled
	case QueryStatusSendFailed:
		return MetricOutcomeSendFailed
	case QueryStatusUnavailable:
		return MetricOutcomeUnavailable
	default:
		return MetricOutcomeFailed
	}
}

func metricOutcomeIndex(outcome MetricOutcome) int {
	for index, candidate := range AllMetricOutcomes {
		if candidate == outcome {
			return index
		}
	}
	return len(AllMetricOutcomes) - 1
}
