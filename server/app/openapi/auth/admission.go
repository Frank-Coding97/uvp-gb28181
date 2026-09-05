package auth

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

var (
	ErrDenied      = errors.New("openapi access denied")
	ErrExpired     = errors.New("openapi request expired")
	ErrReplay      = errors.New("openapi request replayed")
	ErrUnavailable = errors.New("openapi admission unavailable")
	ErrFrozen      = errors.New("openapi admission frozen")
)

// AdmissionRequest is internal, verified request metadata, not an HTTP binding
// DTO. The caller MUST verify HMAC before calling Admit. Epochs are the versions
// used for that verification; a concurrent rotation or revocation invalidates it.
type AdmissionRequest struct {
	ClientID, SecretVersion, AuthEpoch, ScopeEpoch int64
	Scope, Nonce, Timestamp, RequestID             string
	ResourceType, ResourceID, Source               string
}

type Admission struct {
	db       *gorm.DB
	now      func() time.Time
	transact func(context.Context, func(*gorm.DB) error) error
	mu       sync.Mutex
	lastWall time.Time
	frozen   bool
}

func NewAdmission(db *gorm.DB, now func() time.Time) *Admission {
	if now == nil {
		now = time.Now
	}
	a := &Admission{db: db, now: now}
	a.transact = func(ctx context.Context, fn func(*gorm.DB) error) error {
		if db == nil {
			return ErrUnavailable
		}
		return db.WithContext(ctx).Transaction(fn)
	}
	return a
}

// Freeze is sticky for this runtime. Recovery requires the explicit deployment
// procedure (ingress disabled, keys rotated, old media cleared), never a timer.
func (a *Admission) Freeze() { a.mu.Lock(); defer a.mu.Unlock(); a.frozen = true }

func (a *Admission) clock() (time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now().UTC().Round(0) // Compare wall time, not Go's monotonic component.
	if !a.lastWall.IsZero() && now.Before(a.lastWall) {
		a.frozen = true
	}
	if a.frozen {
		return time.Time{}, ErrFrozen
	}
	a.lastWall = now
	return now, nil
}

// Admit commits nonce and audit BEFORE business dispatch. authorize must recheck
// the current department/resource and any rate/quota guards using this tx. It
// must not perform network I/O or start business work. Nil always denies.
func (a *Admission) Admit(ctx context.Context, r AdmissionRequest, authorize func(*gorm.DB) error) error {
	if authorize == nil || r.ClientID <= 0 || r.SecretVersion <= 0 || r.AuthEpoch <= 0 || r.ScopeEpoch <= 0 || r.Scope == "" || len(r.Scope) > 64 || r.RequestID == "" || len(r.RequestID) > 64 || len(r.ResourceType) > 32 || len(r.ResourceID) > 128 || len(r.Source) > 64 {
		return ErrDenied
	}
	if validateNonce(r.Nonce) != nil || validateTimestamp(r.Timestamp) != nil {
		return ErrDenied
	}
	ts, _ := strconv.ParseInt(r.Timestamp, 10, 64)
	err := a.transact(ctx, func(tx *gorm.DB) error {
		now, err := a.clock()
		if err != nil {
			return err
		}
		if ts < now.Unix()-300 || ts > now.Unix()+300 {
			return ErrExpired
		}
		var current models.Client
		q := lockClient(tx).Where("id = ?", r.ClientID).Find(&current)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 || current.Status != models.StatusActive || current.SecretVersion != r.SecretVersion || current.AuthEpoch != r.AuthEpoch {
			return ErrDenied
		}
		// Every management mutation locks/CAS-updates client first. Holding its row
		// serializes scope mutations without a dialect-specific second lock order.
		var scope models.ClientScope
		q = tx.Where("client_id = ? AND scope = ?", r.ClientID, r.Scope).Find(&scope)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 || !scope.Enabled || scope.ScopeEpoch != r.ScopeEpoch {
			return ErrDenied
		}
		if err := authorize(tx); err != nil {
			return err
		}
		now, err = a.clock()
		if err != nil {
			return err
		}
		if ts < now.Unix()-300 || ts > now.Unix()+300 {
			return ErrExpired
		}
		n := models.Nonce{ClientID: r.ClientID, Value: r.Nonce, AcceptedAt: now, ExpiresAt: now.Add(660 * time.Second)}
		if err := tx.Create(&n).Error; err != nil {
			if isUniqueViolation(err) {
				return ErrReplay
			}
			return err
		}
		return tx.Create(&models.Audit{RequestID: r.RequestID, ClientID: &r.ClientID, Scope: r.Scope, ResourceType: r.ResourceType, ResourceID: r.ResourceID, Source: r.Source, Result: "started", CreatedAt: now}).Error
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrDenied), errors.Is(err, ErrExpired), errors.Is(err, ErrReplay), errors.Is(err, ErrFrozen):
		return err
	default:
		return ErrUnavailable // Includes commit-unknown; caller must NOT dispatch.
	}
}

func lockClient(tx *gorm.DB) *gorm.DB {
	if tx.Dialector.Name() == "sqlserver" {
		return tx.Table("sys_openapi_client WITH (UPDLOCK, HOLDLOCK)")
	}
	return tx.Model(&models.Client{}).Clauses(clause.Locking{Strength: "UPDATE"})
}

// Cleanup is bounded; the caller schedules it once per minute. It never deletes
// an entry before BOTH its expiry and minimum 660-second retention boundary.
func (a *Admission) Cleanup(ctx context.Context) (int64, error) {
	now, err := a.clock()
	if err != nil {
		return 0, err
	}
	if a.db == nil {
		return 0, ErrUnavailable
	}
	var removed int64
	err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []models.Nonce
		if err := tx.Where("expires_at <= ? AND accepted_at <= ?", now, now.Add(-660*time.Second)).Order("expires_at").Limit(500).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			result := tx.Where("client_id = ? AND nonce = ? AND expires_at <= ? AND accepted_at <= ?", row.ClientID, row.Value, now, now.Add(-660*time.Second)).Delete(&models.Nonce{})
			if result.Error != nil {
				return result.Error
			}
			removed += result.RowsAffected
		}
		return nil
	})
	if err != nil {
		return 0, ErrUnavailable
	}
	return removed, nil
}
