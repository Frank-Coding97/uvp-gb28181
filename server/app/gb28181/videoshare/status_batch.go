package videoshare

import (
	"context"
	"database/sql"
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
		// ⛔ 必须用整页版授权器：逐通道版每通道都要重查 sys_users + datascope，
		//    200 条通道在慢链路上必然撞穿上面那 2s 预算，整页列表连带 503。
		snapshots, err := NewDatabaseBatchAuthorizer(permission)(ctx, tx, actorID, channelIDs)
		if err != nil {
			return err
		}
		if len(snapshots) == 0 {
			return nil
		}
		allowedIDs := make([]uint, 0, len(snapshots))
		now := s.now().UTC()
		for id := range snapshots {
			allowedIDs = append(allowedIDs, id)
			result[id] = StatusSummary{ServerTime: now}
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
