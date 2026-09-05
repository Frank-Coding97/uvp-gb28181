package metrics

import (
	"context"
	"errors"
	"sync"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const persistentDashboardSnapshotTTL = 5 * time.Second

type persistentSnapshotKey struct {
	window    time.Duration
	precision time.Duration
}

type persistentSnapshotEntry struct {
	loadedAt time.Time
	value    *DashboardSnapshot
}

type metricSnapshotTotals struct {
	requests     uint64
	transactions uint64
	success      uint64
	failure      uint64
}

type metricSnapshotRow struct {
	Method             string
	RequestCount       uint64
	TransactionCount   uint64
	TransactionSuccess uint64
	TransactionFailure uint64
}

// PersistentDashboardSnapshot builds the minute-granularity SIP monitor from
// the durable ledger. Sub-minute requests remain delegated to the in-memory
// ring because minute rows cannot reconstruct a rolling second-level window.
type PersistentDashboardSnapshot struct {
	db       *gorm.DB
	realtime *Aggregator
	location *time.Location
	clock    func() time.Time

	mu    sync.Mutex
	cache map[persistentSnapshotKey]persistentSnapshotEntry
}

func NewPersistentDashboardSnapshot(db *gorm.DB, realtime *Aggregator, location *time.Location) *PersistentDashboardSnapshot {
	if location == nil {
		location = time.Local
	}
	return &PersistentDashboardSnapshot{
		db: db, realtime: realtime, location: location, clock: time.Now,
		cache: make(map[persistentSnapshotKey]persistentSnapshotEntry),
	}
}

func (service *PersistentDashboardSnapshot) SetClock(clock func() time.Time) {
	if service == nil || clock == nil {
		return
	}
	service.mu.Lock()
	service.clock = clock
	service.cache = make(map[persistentSnapshotKey]persistentSnapshotEntry)
	service.mu.Unlock()
}

func (service *PersistentDashboardSnapshot) Snapshot(ctx context.Context, window, precision time.Duration) (*DashboardSnapshot, error) {
	if service == nil {
		return nil, errors.New("SIP dashboard snapshot service is unavailable")
	}
	if window <= 0 {
		window = time.Hour
	}
	if precision <= 0 {
		precision = time.Minute
	}
	if window < time.Minute || precision < time.Minute || precision%time.Minute != 0 {
		if service.realtime == nil {
			return nil, errors.New("SIP realtime snapshot service is unavailable")
		}
		return service.realtime.Snapshot(window, precision), nil
	}
	if service.db == nil {
		return nil, errors.New("SIP metric database is unavailable")
	}
	if window > 24*time.Hour {
		window = 24 * time.Hour
	}
	if precision > window {
		precision = window
	}

	key := persistentSnapshotKey{window: window, precision: precision}
	service.mu.Lock()
	defer service.mu.Unlock()
	now := service.clock()
	if cached, ok := service.cache[key]; ok && !now.Before(cached.loadedAt) && now.Sub(cached.loadedAt) < persistentDashboardSnapshotTTL {
		return cloneDashboardSnapshot(cached.value), nil
	}
	for cachedKey, cached := range service.cache {
		if now.Before(cached.loadedAt) || now.Sub(cached.loadedAt) >= persistentDashboardSnapshotTTL {
			delete(service.cache, cachedKey)
		}
	}
	if len(service.cache) >= 16 {
		service.cache = make(map[persistentSnapshotKey]persistentSnapshotEntry)
	}
	value, err := service.loadSnapshot(ctx, now, window, precision)
	if err != nil {
		return nil, err
	}
	service.cache[key] = persistentSnapshotEntry{loadedAt: now, value: value}
	return cloneDashboardSnapshot(value), nil
}

func (service *PersistentDashboardSnapshot) loadSnapshot(ctx context.Context, now time.Time, window, precision time.Duration) (*DashboardSnapshot, error) {
	localNow := now.In(service.location)
	dayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, service.location)
	pulseEnd := localNow.Truncate(precision)
	bucketCount := int((window + precision - 1) / precision)
	if bucketCount < 1 {
		bucketCount = 1
	}
	pulseStart := pulseEnd.Add(-time.Duration(bucketCount-1) * precision)
	gapQueryStart := dayStart
	if pulseStart.Before(gapQueryStart) {
		gapQueryStart = pulseStart
	}

	var todayRows []metricSnapshotRow
	if err := service.db.WithContext(ctx).
		Model(&gbmodels.GbSipMetricMinute{}).
		Select("method, COALESCE(SUM(request_count), 0) AS request_count, COALESCE(SUM(transaction_count), 0) AS transaction_count, COALESCE(SUM(transaction_success), 0) AS transaction_success, COALESCE(SUM(transaction_failure), 0) AS transaction_failure").
		Where("bucket_start >= ? AND bucket_start <= ?", dayStart, localNow).
		Group("method").Scan(&todayRows).Error; err != nil {
		return nil, err
	}
	var pulseRows []gbmodels.GbSipMetricMinute
	if err := service.db.WithContext(ctx).
		Where("bucket_start >= ? AND bucket_start <= ?", pulseStart, localNow).
		Order("bucket_start ASC").Find(&pulseRows).Error; err != nil {
		return nil, err
	}
	var flushes []gbmodels.GbSipMetricFlush
	if err := service.db.WithContext(ctx).Select("created_at").
		Where("created_at >= ? AND created_at <= ?", pulseStart, localNow).
		Order("created_at ASC").Find(&flushes).Error; err != nil {
		return nil, err
	}
	var earliestFlush gbmodels.GbSipMetricFlush
	earliestFlushResult := service.db.WithContext(ctx).Select("created_at").
		Where("created_at >= ? AND created_at <= ?", dayStart, localNow).
		Order("created_at ASC").Limit(1).Find(&earliestFlush)
	if earliestFlushResult.Error != nil {
		return nil, earliestFlushResult.Error
	}
	var gaps []gbmodels.GbSipMetricGap
	if err := service.db.WithContext(ctx).
		Where("started_at < ? AND ended_at > ?", localNow, gapQueryStart).
		Order("started_at ASC").Find(&gaps).Error; err != nil {
		return nil, err
	}

	pulseBuckets := make(map[int64]metricSnapshotTotals)
	todayByKind := make(map[TxKind]metricSnapshotTotals, len(AllTxKinds))
	var today metricSnapshotTotals
	var earliestToday time.Time
	if earliestFlushResult.RowsAffected > 0 {
		earliestToday = earliestFlush.CreatedAt
	}
	for _, row := range todayRows {
		kind := txKindFromString(row.Method)
		if kind != TxUnknown {
			current := todayByKind[kind]
			addSnapshotRow(&current, row)
			todayByKind[kind] = current
		}
		addSnapshotRow(&today, row)
	}
	for _, row := range pulseRows {
		bucket := row.BucketStart.Truncate(precision).Unix()
		current := pulseBuckets[bucket]
		addMetricRow(&current, row)
		pulseBuckets[bucket] = current
	}

	heartbeats := make(map[int64]bool)
	for _, flush := range flushes {
		if !flush.CreatedAt.Before(pulseStart) {
			heartbeats[flush.CreatedAt.Truncate(precision).Unix()] = true
		}
	}

	transactions := make([]TransactionStat, 0, len(AllTxKinds))
	healthInput := healthInputs{count: make(map[TxKind]int64, len(AllTxKinds)), success: make(map[TxKind]int64, len(AllTxKinds)), abnorm: int64(today.failure)}
	for _, kind := range AllTxKinds {
		current := todayByKind[kind]
		count := int64(current.transactions)
		success := int64(current.success)
		rate := 0.0
		if count > 0 {
			rate = float64(success) / float64(count)
		}
		healthInput.count[kind] = count
		healthInput.success[kind] = success
		transactions = append(transactions, TransactionStat{
			Kind: kind, KindStr: kind.String(), LabelZh: kind.LabelZh(), LabelEn: kind.String(),
			TodayCount: count, SuccessRate: rate, Alert: count > 0 && rate < 0.95,
		})
	}

	pulse := make([]PulseSample, 0, bucketCount)
	missing := false
	for start := pulseStart; !start.After(pulseEnd); start = start.Add(precision) {
		current, hasMetrics := pulseBuckets[start.Unix()]
		known := (hasMetrics || heartbeats[start.Unix()]) && !metricBucketOverlapsGap(start, precision, gaps)
		failureRate := 0
		if current.transactions > 0 {
			failureRate = int(current.failure * 1000 / current.transactions)
		}
		pulse = append(pulse, PulseSample{T: start.Unix(), MsgPerSec: int(current.transactions), FailPct: failureRate, Known: known})
		if !known {
			missing = true
		}
	}

	todayPartial := false
	for _, gap := range gaps {
		if gap.StartedAt.Before(localNow) && gap.EndedAt.After(dayStart) {
			todayPartial = true
			break
		}
	}
	if !earliestToday.IsZero() && earliestToday.After(dayStart.Add(time.Minute)) {
		todayPartial = true
	}
	pending := int64(0)
	if today.requests > today.transactions {
		pending = int64(today.requests - today.transactions)
	}
	health := computeHealth(healthInput)
	return &DashboardSnapshot{
		Health: health, TodayTotal: int64(today.transactions), TodayAbnormal: int64(today.failure), Pending: pending,
		Transactions: transactions,
		Pulse:        PulseData{WindowMinutes: int(window / time.Minute), Samples: pulse, AbnormalWindows: detectAbnormalWindows(pulse, 50)},
		Partial:      missing || todayPartial, AsOf: now.Unix(),
	}, nil
}

func addMetricRow(value *metricSnapshotTotals, row gbmodels.GbSipMetricMinute) {
	value.requests += row.RequestCount
	value.transactions += row.TransactionCount
	value.success += row.TransactionSuccess
	value.failure += row.TransactionFailure
}

func addSnapshotRow(value *metricSnapshotTotals, row metricSnapshotRow) {
	value.requests += row.RequestCount
	value.transactions += row.TransactionCount
	value.success += row.TransactionSuccess
	value.failure += row.TransactionFailure
}

func txKindFromString(value string) TxKind {
	for _, kind := range AllTxKinds {
		if kind.String() == value {
			return kind
		}
	}
	return TxUnknown
}

func metricBucketOverlapsGap(start time.Time, duration time.Duration, gaps []gbmodels.GbSipMetricGap) bool {
	end := start.Add(duration)
	for _, gap := range gaps {
		if gap.StartedAt.Before(end) && gap.EndedAt.After(start) {
			return true
		}
	}
	return false
}

func cloneDashboardSnapshot(value *DashboardSnapshot) *DashboardSnapshot {
	if value == nil {
		return nil
	}
	result := *value
	result.Transactions = append([]TransactionStat(nil), value.Transactions...)
	result.Pulse.Samples = append([]PulseSample(nil), value.Pulse.Samples...)
	result.Pulse.AbnormalWindows = append([]AbnormalWindow(nil), value.Pulse.AbnormalWindows...)
	return &result
}
