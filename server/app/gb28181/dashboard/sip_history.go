package dashboard

import (
	"context"
	"sort"
	"time"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type HistoryGap struct {
	StartedAt    time.Time `json:"startedAt"`
	EndedAt      time.Time `json:"endedAt"`
	Reason       string    `json:"reason"`
	DroppedCount uint64    `json:"droppedCount"`
}

type SIPHistoryPoint struct {
	BucketStart  time.Time `json:"bucketStart"`
	Requests     *uint64   `json:"requests"`
	Transactions *uint64   `json:"transactions"`
	Success      *uint64   `json:"success"`
	Failure      *uint64   `json:"failure"`
	RPM          *float64  `json:"rpm"`
}

type SIPHistoryLedgerRow struct {
	Method       string `json:"method"`
	Direction    string `json:"direction"`
	Requests     uint64 `json:"requests"`
	Transactions uint64 `json:"transactions"`
	Success      uint64 `json:"success"`
	Failure      uint64 `json:"failure"`
}

type SIPHistory struct {
	Range           HistoryRange          `json:"range"`
	From            time.Time             `json:"from"`
	To              time.Time             `json:"to"`
	BucketSeconds   int64                 `json:"bucketSeconds"`
	Timezone        string                `json:"timezone"`
	Points          []SIPHistoryPoint     `json:"points"`
	Ledger          []SIPHistoryLedgerRow `json:"ledger"`
	Gaps            []HistoryGap          `json:"gaps"`
	TodayRequests   uint64                `json:"todayRequests"`
	RollingRequests uint64                `json:"rollingRequests"`
	Status          SectionStatus         `json:"status"`
	Coverage        Coverage              `json:"coverage"`
}

func (query *SIPMetricQuery) History(ctx context.Context, window HistoryWindow) (SIPHistory, error) {
	result := SIPHistory{
		Range: window.Range, From: window.From, To: window.To,
		BucketSeconds: int64(window.Bucket.Seconds()), Timezone: window.Timezone,
		Points: []SIPHistoryPoint{}, Ledger: []SIPHistoryLedgerRow{}, Gaps: []HistoryGap{},
		Status: StatusEmpty, Coverage: CoverageNotStarted,
	}
	var rows []gbmodels.GbSipMetricMinute
	if err := query.db.WithContext(ctx).
		Select("bucket_start", "method", "direction", "request_count", "transaction_count", "transaction_success", "transaction_failure").
		Where("bucket_start >= ? AND bucket_start <= ?", window.From, window.To).
		Order("bucket_start ASC").Find(&rows).Error; err != nil {
		return SIPHistory{}, err
	}
	var flushes []gbmodels.GbSipMetricFlush
	if err := query.db.WithContext(ctx).Select("created_at").
		Where("created_at >= ? AND created_at <= ?", window.From, window.To).
		Order("created_at ASC").Find(&flushes).Error; err != nil {
		return SIPHistory{}, err
	}
	var gapRows []gbmodels.GbSipMetricGap
	if err := query.db.WithContext(ctx).
		Where("started_at <= ? AND ended_at >= ?", window.To, window.From).
		Order("started_at ASC").Find(&gapRows).Error; err != nil {
		return SIPHistory{}, err
	}
	for _, row := range gapRows {
		result.Gaps = append(result.Gaps, sipHistoryGap(row))
	}

	type totals struct {
		Requests     uint64
		Transactions uint64
		Success      uint64
		Failure      uint64
	}
	buckets := make(map[int64]totals)
	ledger := make(map[string]SIPHistoryLedgerRow)
	for _, row := range rows {
		start := bucketStart(row.BucketStart, window)
		current := buckets[start.Unix()]
		current.Requests += row.RequestCount
		current.Transactions += row.TransactionCount
		current.Success += row.TransactionSuccess
		current.Failure += row.TransactionFailure
		buckets[start.Unix()] = current
		result.RollingRequests += row.RequestCount

		key := row.Method + "\x1f" + row.Direction
		item := ledger[key]
		item.Method, item.Direction = row.Method, row.Direction
		item.Requests += row.RequestCount
		item.Transactions += row.TransactionCount
		item.Success += row.TransactionSuccess
		item.Failure += row.TransactionFailure
		ledger[key] = item
	}
	for _, item := range ledger {
		result.Ledger = append(result.Ledger, item)
	}
	sort.Slice(result.Ledger, func(i, j int) bool {
		if result.Ledger[i].Method != result.Ledger[j].Method {
			return result.Ledger[i].Method < result.Ledger[j].Method
		}
		return result.Ledger[i].Direction < result.Ledger[j].Direction
	})

	heartbeatBuckets := make(map[int64]bool)
	for _, flush := range flushes {
		heartbeatBuckets[bucketStart(flush.CreatedAt, window).Unix()] = true
	}
	missing := false
	for start := bucketStart(window.From, window); !start.After(bucketStart(window.To, window)); start = start.Add(window.Bucket) {
		current, hasMetrics := buckets[start.Unix()]
		known := (hasMetrics || heartbeatBuckets[start.Unix()]) && !sipBucketOverlapsGap(start, window.Bucket, gapRows)
		point := SIPHistoryPoint{BucketStart: start}
		if known {
			requests, transactions := current.Requests, current.Transactions
			success, failure := current.Success, current.Failure
			rpm := float64(requests) / window.Bucket.Minutes()
			point.Requests, point.Transactions = &requests, &transactions
			point.Success, point.Failure, point.RPM = &success, &failure, &rpm
		} else {
			missing = true
		}
		result.Points = append(result.Points, point)
	}
	if len(result.Points) > window.MaxPoints {
		result.Points = result.Points[len(result.Points)-window.MaxPoints:]
	}

	localTo := window.To.In(query.location)
	dayStart := time.Date(localTo.Year(), localTo.Month(), localTo.Day(), 0, 0, 0, 0, query.location)
	if err := query.db.WithContext(ctx).Model(&gbmodels.GbSipMetricMinute{}).
		Select("COALESCE(SUM(request_count), 0)").
		Where("bucket_start >= ? AND bucket_start <= ?", dayStart, localTo).
		Scan(&result.TodayRequests).Error; err != nil {
		return SIPHistory{}, err
	}
	hasEvidence := len(rows) > 0 || len(flushes) > 0 || len(gapRows) > 0
	if hasEvidence {
		result.Status, result.Coverage = StatusOK, CoverageComplete
		if missing || len(gapRows) > 0 {
			result.Status, result.Coverage = StatusPartial, CoveragePartial
		}
	}
	return result, nil
}

func sipHistoryGap(row gbmodels.GbSipMetricGap) HistoryGap {
	return HistoryGap{StartedAt: row.StartedAt, EndedAt: row.EndedAt, Reason: row.Reason, DroppedCount: row.DroppedCount}
}

func sipBucketOverlapsGap(start time.Time, duration time.Duration, gaps []gbmodels.GbSipMetricGap) bool {
	end := start.Add(duration)
	for _, gap := range gaps {
		if gap.StartedAt.Before(end) && gap.EndedAt.After(start) {
			return true
		}
	}
	return false
}
