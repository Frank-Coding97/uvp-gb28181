package trace

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const DefaultCaptureDuration = 30 * time.Minute

type CaptureStatus string

const (
	CaptureStatusDisabled CaptureStatus = "disabled"
	CaptureStatusActive   CaptureStatus = "active"
	CaptureStatusEnded    CaptureStatus = "ended"
	CaptureEndManual                    = gbmodels.SIPTraceCaptureEndManual
	CaptureEndTimeout                   = gbmodels.SIPTraceCaptureEndTimeout
)

var (
	ErrCaptureDeviceNotFound = errors.New("SIP trace capture device not found")
	ErrCaptureNotFound       = errors.New("SIP trace capture not found")
	ErrCaptureForbidden      = errors.New("SIP trace capture forbidden")
)

type Capture struct {
	gbmodels.GbSIPTraceCapture
}

func (c Capture) StatusAt(now time.Time) CaptureStatus {
	if c.EndedAt != nil || !now.Before(c.PlannedEndAt) {
		return CaptureStatusEnded
	}
	return CaptureStatusActive
}

type CaptureWorkbenchFilter struct {
	CaptureID string    `json:"captureId"`
	DeviceID  string    `json:"deviceId"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
}

func (c Capture) WorkbenchFilter(_ time.Time) CaptureWorkbenchFilter {
	end := c.PlannedEndAt
	if c.EndedAt != nil {
		end = *c.EndedAt
	}
	return CaptureWorkbenchFilter{CaptureID: c.ID, DeviceID: c.DeviceCode, From: c.StartedAt, To: end}
}

type CaptureStartResult struct {
	Capture Capture `json:"capture"`
	Reused  bool    `json:"reused"`
}

type CaptureAuthorizer interface {
	CanManageCapture(context.Context, uint, *gbmodels.GbDevice) bool
}

type CaptureAuthorizerFunc func(context.Context, uint, *gbmodels.GbDevice) bool

func (f CaptureAuthorizerFunc) CanManageCapture(ctx context.Context, userID uint, device *gbmodels.GbDevice) bool {
	return f != nil && f(ctx, userID, device)
}

type CaptureService struct {
	db         *gorm.DB
	authorizer CaptureAuthorizer
	now        func() time.Time
	duration   time.Duration
}

func NewCaptureService(db *gorm.DB, authorizer CaptureAuthorizer, now func() time.Time) *CaptureService {
	if now == nil {
		now = time.Now
	}
	return &CaptureService{db: db, authorizer: authorizer, now: now, duration: DefaultCaptureDuration}
}

func (s *CaptureService) Start(ctx context.Context, deviceID, userID uint) (CaptureStartResult, error) {
	var result CaptureStartResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var device gbmodels.GbDevice
		if err := tx.First(&device, "id = ?", deviceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCaptureDeviceNotFound
			}
			return fmt.Errorf("load SIP trace capture device: %w", err)
		}
		if s.authorizer == nil || !s.authorizer.CanManageCapture(ctx, userID, &device) {
			return ErrCaptureForbidden
		}

		now := s.now().UTC()
		activeKey := strconv.FormatUint(uint64(device.ID), 10)
		if err := materializeExpiredCaptures(tx, device.ID, now); err != nil {
			return err
		}
		var existing gbmodels.GbSIPTraceCapture
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("active_key = ? AND ended_at IS NULL AND planned_end_at > ?", activeKey, now).
			First(&existing).Error
		if err == nil {
			result = CaptureStartResult{Capture: Capture{existing}, Reused: true}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find active SIP trace capture: %w", err)
		}
		capture := gbmodels.GbSIPTraceCapture{
			ID: uuid.NewString(), DeviceID: device.ID, DeviceCode: device.DeviceID, CreatedBy: userID,
			StartedAt: now, PlannedEndAt: now.Add(s.duration), ActiveKey: &activeKey,
		}
		if err := tx.Create(&capture).Error; err != nil {
			var concurrent gbmodels.GbSIPTraceCapture
			if queryErr := tx.Where("active_key = ?", activeKey).First(&concurrent).Error; queryErr == nil {
				result = CaptureStartResult{Capture: Capture{concurrent}, Reused: true}
				return nil
			}
			return fmt.Errorf("create SIP trace capture: %w", err)
		}
		result = CaptureStartResult{Capture: Capture{capture}}
		return nil
	})
	return result, err
}

func (s *CaptureService) Stop(ctx context.Context, captureID string, userID uint) (Capture, error) {
	var result Capture
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var capture gbmodels.GbSIPTraceCapture
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&capture, "id = ?", captureID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCaptureNotFound
			}
			return fmt.Errorf("load SIP trace capture: %w", err)
		}
		var device gbmodels.GbDevice
		if err := tx.Unscoped().First(&device, "id = ?", capture.DeviceID).Error; err != nil {
			return ErrCaptureDeviceNotFound
		}
		if s.authorizer == nil || !s.authorizer.CanManageCapture(ctx, userID, &device) {
			return ErrCaptureForbidden
		}
		if capture.EndedAt == nil {
			now := s.now().UTC()
			endedAt := now
			reason := CaptureEndManual
			if !now.Before(capture.PlannedEndAt) {
				endedAt = capture.PlannedEndAt
				reason = CaptureEndTimeout
			}
			if err := tx.Model(&capture).Updates(map[string]any{
				"ended_at": endedAt, "end_reason": reason, "active_key": nil,
			}).Error; err != nil {
				return fmt.Errorf("stop SIP trace capture: %w", err)
			}
			capture.EndedAt = &endedAt
			capture.EndReason = reason
			capture.ActiveKey = nil
		}
		result = Capture{capture}
		return nil
	})
	return result, err
}

func (s *CaptureService) Active(ctx context.Context, deviceID, userID uint) (*Capture, error) {
	var result *Capture
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var device gbmodels.GbDevice
		if err := tx.First(&device, "id = ?", deviceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCaptureDeviceNotFound
			}
			return fmt.Errorf("load SIP trace capture device: %w", err)
		}
		if s.authorizer == nil || !s.authorizer.CanManageCapture(ctx, userID, &device) {
			return ErrCaptureForbidden
		}
		now := s.now().UTC()
		if err := materializeExpiredCaptures(tx, device.ID, now); err != nil {
			return err
		}
		activeKey := strconv.FormatUint(uint64(device.ID), 10)
		var capture gbmodels.GbSIPTraceCapture
		if err := tx.Where("active_key = ? AND ended_at IS NULL AND planned_end_at > ?", activeKey, now).First(&capture).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("load active SIP trace capture: %w", err)
		}
		wrapped := Capture{capture}
		result = &wrapped
		return nil
	})
	return result, err
}

func materializeExpiredCaptures(tx *gorm.DB, deviceID uint, now time.Time) error {
	if err := tx.Model(&gbmodels.GbSIPTraceCapture{}).
		Where("device_id = ? AND active_key IS NOT NULL AND ended_at IS NULL AND planned_end_at <= ?", deviceID, now).
		Updates(map[string]any{
			"ended_at": gorm.Expr("planned_end_at"), "end_reason": CaptureEndTimeout, "active_key": nil,
		}).Error; err != nil {
		return fmt.Errorf("expire SIP trace captures: %w", err)
	}
	return nil
}
