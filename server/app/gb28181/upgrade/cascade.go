package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
)

var ErrCascadeUnauthorized = errors.New("cascade upgrade authorization revoked or incomplete")

// NewAuthorizedService borrows the root's transfer barrier. A plain local
// upgrade service cannot silently send a cascade request without admission.
func NewAuthorizedService(db *gorm.DB, sender TrackedSender, allocator SNAllocator, now func() time.Time, operations *playauth.DeviceOperationBarrier) (*Service, error) {
	if operations == nil {
		return nil, ErrServiceUnavailable
	}
	s, err := NewService(db, sender, allocator, now)
	if err != nil {
		return nil, err
	}
	s.operations = operations
	return s, nil
}

// CascadeBinding is fixed before the downstream command is sent. Whole-device
// upgrade permission is an explicit platform grant, scoped to a published
// channel and its registered parent; neither PTZ nor configuration grants it.
// RequestKey identifies the original SIP transaction, without its payload.
type CascadeBinding struct {
	PlatformID         uint64 `json:"platformId"`
	ConfigRevision     uint64 `json:"configRevision"`
	ProjectionRevision uint64 `json:"projectionRevision"`
	PublishedID        string `json:"publishedId"`
	SourceChannelID    uint64 `json:"sourceChannelId"`
	SourceChannelCode  string `json:"sourceChannelCode"`
	SourceOwnerDeptID  uint   `json:"sourceOwnerDeptId"`
	SourceDeviceEpoch  int64  `json:"sourceDeviceEpoch"`
	UpstreamSN         int    `json:"upstreamSN"`
	UpstreamSessionID  string `json:"upstreamSessionId"`
	RequestKey         string `json:"requestKey"`
}

func encodeCascadeBinding(binding *CascadeBinding) string {
	if binding == nil {
		return ""
	}
	body, _ := json.Marshal(binding)
	return string(body)
}

func cascadeKey(platform uint64, value string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", platform, value)))
	return hex.EncodeToString(digest[:])
}

func bindCascadeRequest(row *gbmodels.GbDeviceFirmwareUpgrade, binding *CascadeBinding) {
	if binding == nil {
		return
	}
	row.CascadeBinding = encodeCascadeBinding(binding)
	requestKey, sessionKey := cascadeKey(binding.PlatformID, binding.RequestKey), cascadeKey(binding.PlatformID, binding.UpstreamSessionID)
	row.CascadeRequestKey, row.CascadeSessionKey = &requestKey, &sessionKey
}

func authorizeCascade(db *gorm.DB, deviceID uint, deviceCode string, a *CascadeBinding) error {
	if a == nil || a.PlatformID == 0 || a.ConfigRevision == 0 || a.ProjectionRevision == 0 || a.PublishedID == "" || a.SourceChannelID == 0 || a.SourceChannelCode == "" || a.SourceDeviceEpoch <= 0 || a.UpstreamSN <= 0 || strings.TrimSpace(a.UpstreamSessionID) == "" || strings.TrimSpace(a.RequestKey) == "" {
		return ErrCascadeUnauthorized
	}
	var count int64
	err := db.Table("gb_cascade_platform AS p").
		Joins("JOIN gb_cascade_channel_projection AS c ON c.platform_id=p.id").
		Joins("JOIN gb_cascade_device_projection AS d ON d.id=c.device_projection_id AND d.platform_id=p.id").
		Joins("JOIN gb_device AS sd ON sd.id=d.source_device_id").
		Joins("JOIN gb_channel AS sc ON sc.id=c.source_channel_id AND sc.device_id=sd.device_id AND sc.owner_dept_id=sd.owner_dept_id").
		Where("p.id=? AND p.enabled=? AND p.upgrade_enabled=? AND p.deleted_at IS NULL AND p.config_revision=? AND p.projection_revision=?", a.PlatformID, true, true, a.ConfigRevision, a.ProjectionRevision).
		Where("c.active=? AND d.active=? AND c.published_channel_id=? AND c.source_channel_id=? AND d.source_device_id=?", true, true, a.PublishedID, a.SourceChannelID, deviceID).
		Where("sd.device_id=? AND sc.channel_id=? AND sd.owner_dept_id=? AND sd.deleted_at IS NULL AND sc.deleted_at IS NULL AND sd.access_epoch=? AND sd.cleanup_completed_epoch=?", deviceCode, a.SourceChannelCode, a.SourceOwnerDeptID, a.SourceDeviceEpoch, a.SourceDeviceEpoch).
		Where("sd.effective_version = ?", "2022").
		Count(&count).Error
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCascadeUnauthorized, err)
	}
	if count != 1 {
		return ErrCascadeUnauthorized
	}
	return nil
}

func DecodeCascadeBinding(row gbmodels.GbDeviceFirmwareUpgrade) (CascadeBinding, error) {
	var binding CascadeBinding
	if row.CascadeRequestKey == nil || row.CascadeSessionKey == nil || json.Unmarshal([]byte(row.CascadeBinding), &binding) != nil || *row.CascadeRequestKey != cascadeKey(binding.PlatformID, binding.RequestKey) || *row.CascadeSessionKey != cascadeKey(binding.PlatformID, binding.UpstreamSessionID) {
		return binding, ErrCascadeUnauthorized
	}
	return binding, nil
}

func authorizeCascadeRow(db *gorm.DB, row gbmodels.GbDeviceFirmwareUpgrade) error {
	if row.CascadeBinding == "" && row.CascadeRequestKey == nil && row.CascadeSessionKey == nil {
		return nil
	}
	binding, err := DecodeCascadeBinding(row)
	if err != nil {
		return err
	}
	return authorizeCascade(db, row.DeviceID, row.DeviceCode, &binding)
}

func (s *Service) AuthorizeCascade(ctx context.Context, row gbmodels.GbDeviceFirmwareUpgrade) error {
	return authorizeCascadeRow(s.db.Clauses(dbresolver.Write).WithContext(ctx), row)
}

func (s *Service) PendingCascadeResults(ctx context.Context, limit int) ([]gbmodels.GbDeviceFirmwareUpgrade, error) {
	return s.CascadeResultsAfter(ctx, 0, limit)
}

// Cursor pagination prevents revoked or unready tasks from starving later ones.
func (s *Service) CascadeResultsAfter(ctx context.Context, after uint, limit int) ([]gbmodels.GbDeviceFirmwareUpgrade, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var rows []gbmodels.GbDeviceFirmwareUpgrade
	err := s.db.Clauses(dbresolver.Write).WithContext(ctx).
		Where("id > ? AND cascade_request_key IS NOT NULL AND ((cascade_business_result <> '' AND cascade_business_delivered = ?) OR (status IN ? AND cascade_final_delivered = ?))", after, false, []gbmodels.FirmwareUpgradeStatus{gbmodels.FirmwareUpgradeSucceeded, gbmodels.FirmwareUpgradeFailed}, false).
		Order("id").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *Service) MarkCascadeDelivered(ctx context.Context, row gbmodels.GbDeviceFirmwareUpgrade, final bool) error {
	column := "cascade_business_delivered"
	if final {
		column = "cascade_final_delivered"
	}
	return s.db.Clauses(dbresolver.Write).WithContext(ctx).Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
		Where("id = ? AND cascade_request_key = ?", row.ID, row.CascadeRequestKey).Update(column, true).Error
}
