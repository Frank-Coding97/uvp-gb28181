package logcleanup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	gbrepo "uvplatform.com/uvp-gb28181/app/gb28181/zlm/repo"
	"uvplatform.com/uvp-gb28181/app/models"
)

// Kind identifies one database-backed log family handled by Clean.
type Kind string

const (
	SIP       Kind = "sip"
	Operation Kind = "operation"
	Login     Kind = "login"
	Job       Kind = "job"
	Playback  Kind = "playback"
	Scheduler Kind = "scheduler"
)

// Clean physically removes rows strictly older than cutoff. Each batch is
// committed independently, so the returned count includes work completed
// before a later batch fails or the context is canceled.
func Clean(ctx context.Context, db *gorm.DB, kind Kind, cutoff time.Time, batchSize int) (int64, error) {
	if ctx == nil {
		return 0, errors.New("log cleanup context is nil")
	}
	if db == nil {
		return 0, errors.New("log cleanup database is unavailable")
	}
	if batchSize <= 0 {
		return 0, errors.New("log cleanup batch size must be positive")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	cutoff = cutoff.UTC()
	switch kind {
	case Operation:
		if err := requireTables(db, &models.SysOperationLog{}); err != nil {
			return 0, err
		}
		return cleanBatches[uint64](ctx, db, &models.SysOperationLog{}, "id", batchSize, true,
			"created_at < ?", cutoff)
	case Login:
		if err := requireTables(db, &models.SysLoginLog{}); err != nil {
			return 0, err
		}
		return cleanBatches[uint64](ctx, db, &models.SysLoginLog{}, "id", batchSize, true,
			"created_at < ?", cutoff)
	case Job:
		if err := requireTables(db, &models.SysJobResults{}); err != nil {
			return 0, err
		}
		return cleanBatches[uint64](ctx, db, &models.SysJobResults{}, "id", batchSize, false,
			"end_time IS NOT NULL AND end_time < ?", cutoff)
	case Scheduler:
		if err := requireTables(db, &gbrepo.SchedulerLogDTO{}); err != nil {
			return 0, err
		}
		return cleanBatches[int64](ctx, db, &gbrepo.SchedulerLogDTO{}, "id", batchSize, false,
			"happened_at < ?", cutoff)
	case SIP:
		if err := requireTables(db, &gbmodels.GbSipTraceMessage{}, &gbmodels.GbSipTraceSessionDiagnosis{}, &gbmodels.GbSIPTraceCapture{}); err != nil {
			return 0, err
		}
		return cleanSIP(ctx, db, cutoff, batchSize)
	case Playback:
		if err := requireTables(db, &gbmodels.GbPlayAttempt{}, &gbmodels.GbPlayLifecycleEvent{}); err != nil {
			return 0, err
		}
		return cleanPlayback(ctx, db, cutoff, batchSize)
	default:
		return 0, fmt.Errorf("unknown log cleanup kind %q", kind)
	}
}

func requireTables(db *gorm.DB, modelsToCheck ...any) error {
	for _, model := range modelsToCheck {
		if db.Migrator().HasTable(model) {
			continue
		}
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return fmt.Errorf("resolve required log cleanup table: %w", err)
		}
		return fmt.Errorf("required log cleanup table %q is missing or unavailable", statement.Schema.Table)
	}
	return nil
}

func cleanBatches[T any](ctx context.Context, db *gorm.DB, model any, idColumn string, batchSize int, unscoped bool, condition string, args ...any) (int64, error) {
	if !db.Migrator().HasTable(model) {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return 0, fmt.Errorf("resolve required log cleanup table: %w", err)
		}
		return 0, fmt.Errorf("required log cleanup table %q is missing or unavailable", statement.Schema.Table)
	}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		var ids []T
		var batchDeleted int64
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			query := tx.Model(model)
			if unscoped {
				query = query.Unscoped()
			}
			if err := query.Where(condition, args...).Order(idColumn).Limit(batchSize).Pluck(idColumn, &ids).Error; err != nil {
				return err
			}
			if len(ids) == 0 {
				return nil
			}
			result := query.Where(idColumn+" IN ?", ids).Where(condition, args...).Delete(model)
			batchDeleted = result.RowsAffected
			return result.Error
		})
		if err != nil {
			return total, err
		}
		total += batchDeleted
		if len(ids) < batchSize {
			return total, nil
		}
	}
}

func cleanSIP(ctx context.Context, db *gorm.DB, cutoff time.Time, batchSize int) (int64, error) {
	var total int64
	var errs []error
	now := time.Now().UTC()

	messageCondition := `occurred_at < ? AND NOT EXISTS (
			SELECT 1 FROM gb_sip_trace_capture AS capture
			WHERE capture.device_code = gb_sip_trace_message.device_id
			  AND capture.active_key IS NOT NULL AND capture.active_key <> ''
			  AND capture.ended_at IS NULL AND capture.planned_end_at > ?
			  AND gb_sip_trace_message.occurred_at >= capture.started_at
			  AND gb_sip_trace_message.occurred_at <= capture.planned_end_at
		)`
	messageArgs := []any{cutoff, now}
	diagnosisCondition := `observed_at < ? AND NOT EXISTS (
			SELECT 1 FROM gb_sip_trace_capture AS capture
			WHERE capture.device_code = gb_sip_trace_session_diagnosis.device_id
			  AND capture.active_key IS NOT NULL AND capture.active_key <> ''
			  AND capture.ended_at IS NULL AND capture.planned_end_at > ?
			  AND gb_sip_trace_session_diagnosis.observed_at >= capture.started_at
			  AND gb_sip_trace_session_diagnosis.observed_at <= capture.planned_end_at
		)`
	diagnosisArgs := []any{cutoff, now}

	if n, err := cleanBatches[string](ctx, db, &gbmodels.GbSipTraceMessage{}, "event_id", batchSize, false, messageCondition, messageArgs...); err != nil {
		total += n
		errs = append(errs, fmt.Errorf("clean SIP messages: %w", err))
	} else {
		total += n
	}
	if ctx.Err() == nil {
		if n, err := cleanBatches[uint64](ctx, db, &gbmodels.GbSipTraceSessionDiagnosis{}, "id", batchSize, false, diagnosisCondition, diagnosisArgs...); err != nil {
			total += n
			errs = append(errs, fmt.Errorf("clean SIP diagnoses: %w", err))
		} else {
			total += n
		}
	}
	if ctx.Err() == nil {
		if n, err := cleanBatches[string](ctx, db, &gbmodels.GbSIPTraceCapture{}, "id", batchSize, false, "ended_at IS NOT NULL AND ended_at < ?", cutoff); err != nil {
			total += n
			errs = append(errs, fmt.Errorf("clean ended SIP captures: %w", err))
		} else {
			total += n
		}
	}
	if ctx.Err() != nil {
		errs = append(errs, ctx.Err())
	}
	return total, errors.Join(errs...)
}

func cleanPlayback(ctx context.Context, db *gorm.DB, cutoff time.Time, batchSize int) (int64, error) {
	var total int64
	var errs []error
	var attemptDeleted, eventDeleted int64

	for {
		if err := ctx.Err(); err != nil {
			return total + attemptDeleted + eventDeleted, errors.Join(append(errs, err)...)
		}
		var lifecycleIDs []string
		var attemptBatchDeleted, eventBatchDeleted int64
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			condition := `lifecycle_state IN ? AND finished_at IS NOT NULL AND finished_at < ? AND NOT EXISTS (
					SELECT 1 FROM gb_play_lifecycle_event AS play_event
					WHERE play_event.lifecycle_id = gb_play_attempt.correlation_id AND play_event.event_at >= ?
				)`
			conditionArgs := []any{[]string{"completed", "failed", "stale_in_progress"}, cutoff, cutoff}
			if err := tx.Model(&gbmodels.GbPlayAttempt{}).Where(condition, conditionArgs...).
				Order("id").Limit(batchSize).Pluck("correlation_id", &lifecycleIDs).Error; err != nil {
				return err
			}
			if len(lifecycleIDs) == 0 {
				return nil
			}
			result := tx.Model(&gbmodels.GbPlayAttempt{}).
				Where("correlation_id IN ?", lifecycleIDs).
				Where(condition, conditionArgs...).Delete(&gbmodels.GbPlayAttempt{})
			attemptBatchDeleted = result.RowsAffected
			if result.Error != nil {
				return result.Error
			}
			result = tx.Where("lifecycle_id IN ? AND NOT EXISTS (SELECT 1 FROM gb_play_attempt AS attempt WHERE attempt.correlation_id = gb_play_lifecycle_event.lifecycle_id)", lifecycleIDs).
				Delete(&gbmodels.GbPlayLifecycleEvent{})
			eventBatchDeleted = result.RowsAffected
			if result.Error != nil {
				return result.Error
			}
			return nil
		})
		if err != nil {
			if ctx.Err() != nil {
				return total + attemptDeleted + eventDeleted, errors.Join(append(errs, err)...)
			}
			errs = append(errs, fmt.Errorf("clean playback lifecycles: %w", err))
			break
		}
		attemptDeleted += attemptBatchDeleted
		eventDeleted += eventBatchDeleted
		if len(lifecycleIDs) < batchSize {
			break
		}
	}
	total += attemptDeleted + eventDeleted

	if ctx.Err() == nil {
		orphanCondition := "event_at < ? AND NOT EXISTS (SELECT 1 FROM gb_play_attempt AS attempt WHERE attempt.correlation_id = gb_play_lifecycle_event.lifecycle_id)"
		if n, err := cleanBatches[uint64](ctx, db, &gbmodels.GbPlayLifecycleEvent{}, "id", batchSize, false, orphanCondition, cutoff); err != nil {
			total += n
			errs = append(errs, fmt.Errorf("clean orphan playback events: %w", err))
		} else {
			total += n
		}
	}
	return total, errors.Join(errs...)
}
