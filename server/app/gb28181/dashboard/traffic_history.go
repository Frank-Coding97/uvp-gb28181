package dashboard

import (
	"context"
	"sort"
	"time"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type TrafficHistoryPoint struct {
	BucketStart     time.Time `json:"bucketStart"`
	UpstreamBytes   uint64    `json:"upstreamBytes"`
	DownstreamBytes uint64    `json:"downstreamBytes"`
}

type TrafficHistorySummary struct {
	UpstreamBytes   uint64 `json:"upstreamBytes"`
	DownstreamBytes uint64 `json:"downstreamBytes"`
}

type TrafficLedgerRow struct {
	DeviceCode      string `json:"deviceCode"`
	ChannelCode     string `json:"channelCode"`
	UpstreamBytes   uint64 `json:"upstreamBytes"`
	DownstreamBytes uint64 `json:"downstreamBytes"`
}

type TrafficLedgerPage struct {
	Rows     []TrafficLedgerRow `json:"rows"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
}

type TrafficHistoryGap struct {
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt"`
	Reason    string     `json:"reason"`
}

type TrafficHistory struct {
	Range         HistoryRange          `json:"range"`
	From          time.Time             `json:"from"`
	To            time.Time             `json:"to"`
	BucketSeconds int64                 `json:"bucketSeconds"`
	Timezone      string                `json:"timezone"`
	Points        []TrafficHistoryPoint `json:"points"`
	Summary       TrafficHistorySummary `json:"summary"`
	Ledger        TrafficLedgerPage     `json:"ledger"`
	Gaps          []TrafficHistoryGap   `json:"gaps"`
	Status        SectionStatus         `json:"status"`
	Coverage      Coverage              `json:"coverage"`
}

func (service *AssetSummaryService) TrafficHistory(ctx context.Context, window HistoryWindow, page, pageSize int, scope QueryScope) (TrafficHistory, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	result := TrafficHistory{
		Range: window.Range, From: window.From, To: window.To,
		BucketSeconds: int64(window.Bucket.Seconds()), Timezone: window.Timezone,
		Points: []TrafficHistoryPoint{}, Gaps: []TrafficHistoryGap{},
		Ledger: TrafficLedgerPage{Rows: []TrafficLedgerRow{}, Page: page, PageSize: pageSize},
		Status: StatusEmpty, Coverage: CoverageComplete,
	}
	type trafficRow struct {
		StatAt          time.Time
		DeviceCode      string
		ChannelCode     string
		UpstreamBytes   uint64
		DownstreamBytes uint64
	}
	var rows []trafficRow
	table, timeColumn := "gb_device_traffic_hourly AS traffic", "traffic.stat_hour"
	if window.Range == HistoryRange7D {
		table, timeColumn = "gb_device_traffic_daily AS traffic", "traffic.stat_date"
	}
	dbQuery := service.db.WithContext(ctx).Table(table).
		Select(timeColumn+" AS stat_at", "traffic.device_code", "traffic.channel_code", "traffic.upstream_bytes", "traffic.downstream_bytes").
		Where(timeColumn+" >= ? AND "+timeColumn+" <= ?", window.From, window.To)
	if scope != nil {
		dbQuery = scope(dbQuery)
	}
	if err := dbQuery.Order(timeColumn + " ASC").Scan(&rows).Error; err != nil {
		return TrafficHistory{}, err
	}

	var gapRows []gbmodels.GbDeviceTrafficGap
	if err := service.db.WithContext(ctx).
		Where("started_at <= ? AND (ended_at IS NULL OR ended_at >= ?)", window.To, window.From).
		Order("started_at ASC").Find(&gapRows).Error; err != nil {
		return TrafficHistory{}, err
	}
	for _, gap := range gapRows {
		result.Gaps = append(result.Gaps, TrafficHistoryGap{StartedAt: gap.StartedAt, EndedAt: gap.EndedAt, Reason: gap.Reason})
	}

	type totals struct{ Upstream, Downstream uint64 }
	buckets := make(map[int64]totals)
	ledger := make(map[string]TrafficLedgerRow)
	for _, row := range rows {
		start := service.trafficBucketStart(row.StatAt, window)
		current := buckets[start.Unix()]
		current.Upstream += row.UpstreamBytes
		current.Downstream += row.DownstreamBytes
		buckets[start.Unix()] = current
		result.Summary.UpstreamBytes += row.UpstreamBytes
		result.Summary.DownstreamBytes += row.DownstreamBytes

		key := row.DeviceCode + "\x1f" + row.ChannelCode
		item := ledger[key]
		item.DeviceCode, item.ChannelCode = row.DeviceCode, row.ChannelCode
		item.UpstreamBytes += row.UpstreamBytes
		item.DownstreamBytes += row.DownstreamBytes
		ledger[key] = item
	}
	allLedger := make([]TrafficLedgerRow, 0, len(ledger))
	for _, item := range ledger {
		allLedger = append(allLedger, item)
	}
	sort.Slice(allLedger, func(i, j int) bool {
		if allLedger[i].DeviceCode != allLedger[j].DeviceCode {
			return allLedger[i].DeviceCode < allLedger[j].DeviceCode
		}
		return allLedger[i].ChannelCode < allLedger[j].ChannelCode
	})
	result.Ledger.Total = len(allLedger)
	from := (page - 1) * pageSize
	if from < len(allLedger) {
		to := from + pageSize
		if to > len(allLedger) {
			to = len(allLedger)
		}
		result.Ledger.Rows = allLedger[from:to]
	}

	first, last := service.trafficBucketStart(window.From, window), service.trafficBucketStart(window.To, window)
	for start := first; !start.After(last); start = service.nextTrafficBucket(start, window) {
		current := buckets[start.Unix()]
		result.Points = append(result.Points, TrafficHistoryPoint{
			BucketStart: start, UpstreamBytes: current.Upstream, DownstreamBytes: current.Downstream,
		})
	}
	if len(result.Points) > window.MaxPoints {
		result.Points = result.Points[len(result.Points)-window.MaxPoints:]
	}
	if len(rows) > 0 {
		result.Status = StatusOK
	}
	if len(gapRows) > 0 {
		result.Status, result.Coverage = StatusPartial, CoveragePartial
	}
	return result, nil
}

func (service *AssetSummaryService) trafficBucketStart(value time.Time, window HistoryWindow) time.Time {
	local := value.In(service.location)
	if window.Range == HistoryRange7D {
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, service.location)
	}
	return local.Truncate(time.Hour)
}

func (service *AssetSummaryService) nextTrafficBucket(value time.Time, window HistoryWindow) time.Time {
	if window.Range == HistoryRange7D {
		return value.AddDate(0, 0, 1)
	}
	return value.Add(time.Hour)
}
