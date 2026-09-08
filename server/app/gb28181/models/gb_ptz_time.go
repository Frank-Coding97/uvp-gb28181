package models

import (
	"time"

	"gorm.io/gorm"
)

// PTZTimeComparison matches the PTZ tables' zone-less local wall-time columns.
// SQL Server sends time.Time as DATETIMEOFFSET, which otherwise promotes the
// column to UTC and shifts due/expiry comparisons. Cast only the parameter so
// indexes remain usable, retaining sub-millisecond comparison precision.
func PTZTimeComparison(db *gorm.DB, value time.Time) any {
	if db.Dialector.Name() == "sqlserver" {
		return gorm.Expr("CAST(? AS DATETIME2(7))", value.In(time.Local))
	}
	return value
}

// SQL Server DATETIME2 stores the historical PTZ local wall clock without a
// zone, but its driver attaches UTC when scanning. Reinterpret, don't convert.
// All nodes using these tables must retain the same local timezone. The UTC
// device-intent ledger deliberately does not use this normalization.
func ptzLocalWallTime(value time.Time) time.Time {
	if value.IsZero() {
		return value
	}
	return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), value.Minute(), value.Second(), value.Nanosecond(), time.Local)
}

func ptzLocalWallPointers(values ...**time.Time) {
	for _, value := range values {
		if *value != nil {
			normalized := ptzLocalWallTime(**value)
			*value = &normalized
		}
	}
}

func (op *GbPTZOperation) AfterFind(tx *gorm.DB) error {
	if tx.Dialector.Name() == "sqlserver" {
		op.CreatedAt = ptzLocalWallTime(op.CreatedAt)
		ptzLocalWallPointers(&op.SentAt, &op.CompletedAt, &op.QueueDeadlineAt, &op.DispatchStartedAt, &op.TransportDeadlineAt, &op.DeadlineAt, &op.NextAttemptAt, &op.ResponseAt)
	}
	return nil
}

func (attempt *GbPTZOperationAttempt) AfterFind(tx *gorm.DB) error {
	if tx.Dialector.Name() == "sqlserver" {
		attempt.StartedAt = ptzLocalWallTime(attempt.StartedAt)
		attempt.LeaseUntil = ptzLocalWallTime(attempt.LeaseUntil)
		attempt.CreatedAt = ptzLocalWallTime(attempt.CreatedAt)
		ptzLocalWallPointers(&attempt.SentAt, &attempt.CompletedAt, &attempt.LocalQuiescedAt)
	}
	return nil
}
