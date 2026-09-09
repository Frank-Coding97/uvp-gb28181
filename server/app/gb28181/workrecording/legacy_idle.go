package workrecording

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// ReserveAfterLegacyCheck is the narrow recovery path for a channel which was
// quarantined as legacy_unknown. The old cloud intent must already be off and
// every media tuple observed in the quarantine must be confirmed stopped.
//
// The external observations happen before the final short transaction. That
// transaction repeats the durable checks and only then clears the exact
// legacy claim versions before acquiring the new work owner.
func (r *Recorder) ReserveAfterLegacyCheck(ctx context.Context, channelID uint, owner Owner, target MediaTarget) (RecorderHandle, error) {
	h := RecorderHandle{ChannelID: channelID, Owner: owner}
	if r == nil || r.claims == nil || r.claims.db == nil || r.gate == nil || ctx == nil || channelID == 0 || owner.Kind != OwnerWork || !owner.valid() || !target.valid() || !target.mediaIdentityValid() {
		return h, ErrInvalidRequest
	}

	gateUnlock, err := r.gate.lockMutation(ctx)
	if err != nil {
		return h, err
	}
	defer gateUnlock()
	unlock := r.lock(channelID)
	defer unlock()

	channel, err := loadLegacyIdleChannel(ctx, r.claims.db, channelID)
	if err != nil {
		return h, err
	}
	if channel.CloudRecordingEnabled {
		return h, ErrOwnerConflict
	}

	rows, err := legacyIdleClaims(ctx, r.claims.db, channelID, target)
	if err != nil {
		return h, err
	}
	expected, mediaTargets, err := classifyLegacyIdleClaims(rows, channelID, target)
	if err != nil {
		return h, err
	}
	if len(expected) == 0 {
		// This method is only a confirmed legacy-recovery path. A normal reserve
		// must use Recorder.Reserve so that an idle tombstone is not mistaken for
		// proof that an unidentified recorder has stopped.
		return h, ErrOwnerConflict
	}

	mediaTargets = appendUniqueMediaTarget(mediaTargets, target)
	if err := ensureNoUnfinishedLegacySessions(ctx, r.claims.db, channelID, mediaTargets); err != nil {
		return h, err
	}
	if err := confirmLegacyMediaStopped(ctx, r, mediaTargets); err != nil {
		return h, err
	}

	var acquiredVersion uint64
	err = r.claims.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		currentChannel, err := loadLegacyIdleChannel(ctx, tx, channelID)
		if err != nil {
			return err
		}
		if currentChannel.CloudRecordingEnabled {
			return ErrOwnerConflict
		}

		currentRows, err := legacyIdleClaims(ctx, tx, channelID, target)
		if err != nil {
			return err
		}
		if err := verifyLegacyClaimSet(currentRows, expected, channelID, target); err != nil {
			return err
		}
		if err := ensureNoUnfinishedLegacySessions(ctx, tx, channelID, mediaTargets); err != nil {
			return err
		}

		for _, row := range expected {
			result := tx.Model(&models.GbRecorderClaim{}).
				Where("resource_key = ? AND owner_kind = ? AND owner_id = ? AND state = ? AND version = ?", row.ResourceKey, OwnerLegacy, row.OwnerID, StateUnknown, row.Version).
				Updates(map[string]any{
					"state":      StateIdle,
					"owner_kind": "",
					"owner_id":   "",
					"version":    gorm.Expr("version + 1"),
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrVersionConflict
			}
		}

		claims := NewClaims(tx)
		acquired, err := claims.Acquire(ctx, ChannelResource(channelID), owner, 0)
		if err != nil {
			return err
		}
		result := tx.Model(&models.GbRecorderClaim{}).
			Where("resource_key = ? AND owner_kind = ? AND owner_id = ? AND version = ?", acquired.ResourceKey, owner.Kind, owner.ID, acquired.Version).
			Update("channel_id", channelID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVersionConflict
		}
		acquiredVersion = acquired.Version
		return nil
	})
	if err == nil {
		h.Version = acquiredVersion
	}
	return h, err
}

func loadLegacyIdleChannel(ctx context.Context, db *gorm.DB, channelID uint) (*models.GbChannel, error) {
	if db == nil || ctx == nil || channelID == 0 {
		return nil, ErrInvalidRequest
	}
	var channel models.GbChannel
	if err := db.WithContext(ctx).First(&channel, "id = ?", channelID).Error; err != nil {
		return nil, err
	}
	return &channel, nil
}

func legacyIdleClaims(ctx context.Context, db *gorm.DB, channelID uint, target MediaTarget) ([]models.GbRecorderClaim, error) {
	if db == nil || ctx == nil || channelID == 0 {
		return nil, ErrInvalidRequest
	}
	var rows []models.GbRecorderClaim
	query := db.WithContext(ctx).Where("channel_id = ? OR resource_key = ? OR resource_key = ?", channelID, ChannelResource(channelID), target.key())
	if err := query.Order("resource_key").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func classifyLegacyIdleClaims(rows []models.GbRecorderClaim, channelID uint, target MediaTarget) ([]models.GbRecorderClaim, []MediaTarget, error) {
	channelKey := ChannelResource(channelID)
	expected := make([]models.GbRecorderClaim, 0, len(rows))
	mediaTargets := make([]MediaTarget, 0, len(rows))
	for _, row := range rows {
		if row.State == StateIdle {
			continue
		}
		if row.OwnerKind != OwnerLegacy {
			return nil, nil, ErrOwnerConflict
		}
		if row.State != StateUnknown || strings.TrimSpace(row.OwnerID) == "" {
			return nil, nil, ErrAttributionUnknown
		}
		expected = append(expected, row)
		if row.ResourceKey == channelKey {
			continue
		}
		if !legacyMediaClaimComplete(row) || row.ResourceKey != MediaResource(row.NodeID, row.VHost, row.App, row.Stream) {
			return nil, nil, ErrAttributionUnknown
		}
		mediaTargets = appendUniqueMediaTarget(mediaTargets, targetOf(&row))
	}
	return expected, mediaTargets, nil
}

func legacyMediaClaimComplete(row models.GbRecorderClaim) bool {
	return row.NodeID > 0 && row.VHost != "" && len(row.VHost) <= 128 && row.App != "" && len(row.App) <= 64 && row.Stream != "" && len(row.Stream) <= 64 && !hasNUL(row.VHost) && !hasNUL(row.App) && !hasNUL(row.Stream)
}

func appendUniqueMediaTarget(targets []MediaTarget, target MediaTarget) []MediaTarget {
	for _, existing := range targets {
		if existing.key() == target.key() {
			return targets
		}
	}
	return append(targets, target)
}

func ensureNoUnfinishedLegacySessions(ctx context.Context, db *gorm.DB, channelID uint, targets []MediaTarget) error {
	if db == nil || ctx == nil || channelID == 0 {
		return ErrInvalidRequest
	}
	var sessions []models.GbRecordingSession
	if err := db.WithContext(ctx).Where("channel_id = ? AND state <> ?", channelID, models.RecordingSessionStateStopped).Find(&sessions).Error; err != nil {
		return err
	}
	if len(sessions) > 0 {
		return ErrAttributionUnknown
	}
	for _, target := range targets {
		var associated []models.GbRecordingSession
		if err := db.WithContext(ctx).
			Where("node_id = ? AND vhost = ? AND app = ? AND stream = ? AND state <> ?", target.NodeID, target.VHost, target.App, target.Stream, models.RecordingSessionStateStopped).
			Find(&associated).Error; err != nil {
			return err
		}
		if len(associated) > 0 {
			return ErrAttributionUnknown
		}
	}
	return nil
}

func confirmLegacyMediaStopped(ctx context.Context, r *Recorder, targets []MediaTarget) error {
	if r == nil || r.client == nil || ctx == nil {
		return ErrInvalidRequest
	}
	var firstErr error
	for _, target := range targets {
		client, err := r.client(target)
		if err != nil {
			if firstErr == nil {
				firstErr = errors.Join(ErrAttributionUnknown, err)
			}
			continue
		}
		if client == nil {
			if firstErr == nil {
				firstErr = ErrInvalidRequest
			}
			continue
		}
		active, err := client.IsRecording(ctx, target.VHost, target.App, target.Stream)
		if err != nil {
			if firstErr == nil {
				firstErr = errors.Join(ErrAttributionUnknown, err)
			}
			continue
		}
		if active && firstErr == nil {
			firstErr = ErrAttributionUnknown
		}
	}
	return firstErr
}

func verifyLegacyClaimSet(current, expected []models.GbRecorderClaim, channelID uint, target MediaTarget) error {
	classified, _, err := classifyLegacyIdleClaims(current, channelID, target)
	if err != nil {
		return err
	}
	if len(classified) != len(expected) {
		return ErrVersionConflict
	}
	byKey := make(map[string]models.GbRecorderClaim, len(classified))
	for _, row := range classified {
		byKey[row.ResourceKey] = row
	}
	for _, row := range expected {
		currentRow, ok := byKey[row.ResourceKey]
		if !ok || currentRow.OwnerKind != OwnerLegacy || currentRow.OwnerID != row.OwnerID || currentRow.State != StateUnknown || currentRow.Version != row.Version {
			return ErrVersionConflict
		}
	}
	return nil
}
