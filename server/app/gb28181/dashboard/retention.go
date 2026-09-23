package dashboard

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const DashboardHistoryRetention = 30 * 24 * time.Hour
const PlayLifecycleEventRetention = 7 * 24 * time.Hour
const PlayLifecycleStaleAfter = 24 * time.Hour

type RetentionResult struct {
	SIPMinutes          int64
	SIPFlushes          int64
	SIPGaps             int64
	PlayAttempts        int64
	PlayLifecycleEvents int64
	PlayLifecyclesStale int64
}

func (result RetentionResult) Total() int64 {
	return result.SIPMinutes + result.SIPFlushes + result.SIPGaps + result.PlayAttempts + result.PlayLifecycleEvents
}

type DashboardRetention struct {
	db        *gorm.DB
	retention time.Duration
	batchSize int
	clock     func() time.Time
}

func NewDashboardRetention(db *gorm.DB, batchSize int) *DashboardRetention {
	if batchSize <= 0 {
		batchSize = 500
	}
	return &DashboardRetention{db: db, retention: DashboardHistoryRetention, batchSize: batchSize, clock: time.Now}
}

func (service *DashboardRetention) SetClock(clock func() time.Time) {
	if clock != nil {
		service.clock = clock
	}
}

func (service *DashboardRetention) Prune(ctx context.Context) (RetentionResult, error) {
	if service == nil || service.db == nil {
		return RetentionResult{}, errors.New("dashboard retention database is unavailable")
	}
	cutoff := service.clock().Add(-service.retention)
	result := RetentionResult{}
	var err error
	if service.db.Migrator().HasTable(&gbmodels.GbPlayLifecycleEvent{}) {
		lifecycleStore := NewPlayLifecycleStore(service.db)
		lifecycleStore.SetClock(service.clock)
		if result.PlayLifecyclesStale, err = lifecycleStore.MarkStale(ctx, service.clock().Add(-PlayLifecycleStaleAfter)); err != nil {
			return result, err
		}
		lifecycleCutoff := service.clock().Add(-PlayLifecycleEventRetention)
		if result.PlayLifecycleEvents, err = service.deleteBatches(ctx, &gbmodels.GbPlayLifecycleEvent{}, "event_at < ?", lifecycleCutoff); err != nil {
			return result, err
		}
	}
	if result.SIPMinutes, err = service.deleteBatches(ctx, &gbmodels.GbSipMetricMinute{}, "bucket_start < ?", cutoff); err != nil {
		return result, err
	}
	if result.SIPFlushes, err = service.deleteBatches(ctx, &gbmodels.GbSipMetricFlush{}, "created_at < ?", cutoff); err != nil {
		return result, err
	}
	if result.SIPGaps, err = service.deleteBatches(ctx, &gbmodels.GbSipMetricGap{}, "ended_at < ?", cutoff); err != nil {
		return result, err
	}
	result.PlayAttempts, err = service.deleteBatches(ctx, &gbmodels.GbPlayAttempt{},
		"(lifecycle_state IN ? OR outcome IN ?) AND finished_at IS NOT NULL AND finished_at < ?",
		[]string{"completed", "failed", "stale_in_progress"}, []string{PlayOutcomeSuccess, PlayOutcomeFailure}, cutoff)
	return result, err
}

func (service *DashboardRetention) deleteBatches(ctx context.Context, model any, condition string, args ...any) (int64, error) {
	var total int64
	for {
		var ids []uint64
		if err := service.db.WithContext(ctx).Model(model).Select("id").Where(condition, args...).Order("id ASC").Limit(service.batchSize).Scan(&ids).Error; err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		deleted := int64(0)
		if err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			query := tx.Model(model).Where("id IN ?", ids).Delete(model)
			deleted = query.RowsAffected
			return query.Error
		}); err != nil {
			return total, err
		}
		total += deleted
		if len(ids) < service.batchSize {
			return total, nil
		}
	}
}

func (service *DashboardRetention) Run(ctx context.Context, interval time.Duration, report func(RetentionResult, error)) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	run := func() {
		result, err := service.Prune(ctx)
		if report != nil {
			report(result, err)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
