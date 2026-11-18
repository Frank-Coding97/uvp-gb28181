package videoshare

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// StatusSummary contains only list-safe share lifecycle state.
type StatusSummary struct {
	Expired    bool       `json:"expired"`
	Enabled    bool       `json:"enabled"`
	Pending    bool       `json:"pending"`
	Revision   uint64     `json:"revision"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	ServerTime time.Time  `json:"serverTime"`
}

// BatchStatus reads one page without issuing credentials or activating shares.
// Unauthorized channels are omitted; dependency failures fail the whole batch.
func (s *Service) BatchStatus(ctx context.Context, channelIDs []uint, actorID uint) (map[uint]StatusSummary, error) {
	result := make(map[uint]StatusSummary, len(channelIDs))
	if actorID == 0 || len(channelIDs) == 0 {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		permission, err := TransactionalPermissionChecker(tx)
		if err != nil {
			return err
		}
		authorize := NewDatabaseAuthorizer(permission)
		snapshots := make(map[uint]AuthorizationSnapshot, len(channelIDs))
		seen := make(map[uint]bool, len(channelIDs))
		allowedIDs := make([]uint, 0, len(channelIDs))
		now := s.now().UTC()
		for _, id := range channelIDs {
			if id == 0 || seen[id] {
				continue
			}
			seen[id] = true
			snapshot, err := authorize(ctx, tx, actorID, id)
			if errors.Is(err, ErrExpired) || errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			snapshots[id] = snapshot
			allowedIDs = append(allowedIDs, id)
			result[id] = StatusSummary{ServerTime: now}
		}
		if len(allowedIDs) == 0 {
			return nil
		}
		var shares []gbmodels.GbChannelVideoShare
		if err := tx.Select("channel_pk, enabled, activated_at, expires_at, revision, revoked_at, revoke_reason, device_pk, device_id, device_epoch").Where("channel_pk IN ?", allowedIDs).Find(&shares).Error; err != nil {
			return err
		}
		for _, share := range shares {
			enabled := share.Enabled && share.RevokedAt == nil && matchesDevice(snapshots[share.ChannelPK], share.DevicePK, share.DeviceID, share.DeviceEpoch)
			status := StatusSummary{Expired: matchesDevice(snapshots[share.ChannelPK], share.DevicePK, share.DeviceID, share.DeviceEpoch) && naturallyExpired(share, now), Enabled: enabled, Pending: enabled && share.ActivatedAt == nil, Revision: share.Revision, ServerTime: now}
			if enabled {
				status.ExpiresAt = share.ExpiresAt
			}
			result[share.ChannelPK] = status
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return result, nil
}
