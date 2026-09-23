// Package audit persists machine-call outcomes without request/response bodies.
package audit

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"time"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

var ErrUnavailable = errors.New("openapi audit unavailable")
var ErrInvalidOutcome = errors.New("invalid openapi audit outcome")

type Store struct {
	db  *gorm.DB
	now func() time.Time
}

func New(db *gorm.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

// Complete only accepts published reason codes, never arbitrary upstream errors.
func (s *Store) Complete(ctx context.Context, requestID, code string, latency time.Duration) error {
	result := "failed"
	switch code {
	case "OK":
		result = "success"
	case "INVALID_REQUEST", "AUTHENTICATION_FAILED", "REQUEST_EXPIRED", "REQUEST_REPLAYED", "CAPABILITY_DENIED", "RESOURCE_NOT_FOUND", "CONFLICT", "RATE_LIMITED", "QUOTA_EXCEEDED", "SERVICE_UNAVAILABLE":
	default:
		return ErrInvalidOutcome
	}
	if requestID == "" || len(requestID) > 64 || latency < 0 {
		return ErrInvalidOutcome
	}
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	now := s.now().UTC()
	q := s.db.WithContext(ctx).Model(&models.Audit{}).Where("request_id = ? AND result = ?", requestID, "started").Updates(map[string]any{"result": result, "reason_class": code, "latency_ms": latency.Milliseconds(), "completed_at": now})
	if q.Error != nil || q.RowsAffected != 1 {
		return ErrUnavailable
	}
	return nil
}

// RecoverInterrupted is startup-only, before ingress opens in the supported
// single-process deployment. It must not run over another process's active work.
func (s *Store) RecoverInterrupted(ctx context.Context) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	err := s.db.WithContext(ctx).Model(&models.Audit{}).Where("result = ?", "started").Updates(map[string]any{"result": "outcome_unknown", "reason_class": "process_interrupted", "completed_at": s.now().UTC()}).Error
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) Cleanup(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	cutoff := s.now().UTC().Add(-30 * 24 * time.Hour)
	var deleted int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []int64
		if err := tx.Model(&models.Audit{}).Where("result IN ? AND completed_at < ?", []string{"success", "failed", "outcome_unknown"}, cutoff).Order("id").Limit(500).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		q := tx.Where("id IN ? AND result IN ? AND completed_at < ?", ids, []string{"success", "failed", "outcome_unknown"}, cutoff).Delete(&models.Audit{})
		deleted = q.RowsAffected
		return q.Error
	})
	if err != nil {
		return 0, ErrUnavailable
	}
	return deleted, nil
}
