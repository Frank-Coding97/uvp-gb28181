package workrecording

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// OwnerLegacy is only created by recovery. It intentionally is not a valid
// caller Owner: neither Acquire nor Release can adopt an unidentified recorder.
const OwnerLegacy = "legacy_unknown"

// RecoverLegacy runs before any recorder writer is published. It only adds
// quarantine claims; it neither guesses an old owner nor calls ZLM. The caller
// must fail closed if this transaction fails, including during hot reload.
func (s *Claims) RecoverLegacy(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var channels []models.GbChannel
		if err := tx.Select("id").Where("cloud_recording_enabled = ?", true).Find(&channels).Error; err != nil {
			return err
		}
		for _, channel := range channels {
			if err := quarantine(tx, models.GbRecorderClaim{ResourceKey: ChannelResource(channel.ID), ChannelID: channel.ID, OwnerID: fmt.Sprintf("channel:%d", channel.ID)}); err != nil {
				return err
			}
		}
		var sessions []models.GbRecordingSession
		if err := tx.Where("state <> ?", models.RecordingSessionStateStopped).Find(&sessions).Error; err != nil {
			return err
		}
		for _, session := range sessions {
			if session.ChannelID != 0 {
				if err := quarantine(tx, models.GbRecorderClaim{ResourceKey: ChannelResource(session.ChannelID), ChannelID: session.ChannelID, OwnerID: fmt.Sprintf("channel:%d", session.ChannelID)}); err != nil {
					return err
				}
			}
			if session.NodeID <= 0 || session.VHost == "" || session.App == "" || session.Stream == "" {
				continue
			}
			key := MediaResource(session.NodeID, session.VHost, session.App, session.Stream)
			row := models.GbRecorderClaim{ResourceKey: key, ChannelID: session.ChannelID, NodeID: session.NodeID, VHost: session.VHost, App: session.App, Stream: session.Stream, OwnerID: "media:" + key}
			if err := quarantine(tx, row); err != nil {
				return err
			}
		}
		return nil
	})
}

func quarantine(tx *gorm.DB, row models.GbRecorderClaim) error {
	row.OwnerKind = OwnerLegacy
	row.State = StateUnknown
	row.Version = 1
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "resource_key"}}, DoNothing: true}).Create(&row).Error; err != nil {
		return err
	}
	// A previously released resource must also be fenced if old active metadata
	// remains. Preserve every non-idle claim and its existing owner/version.
	return tx.Model(&models.GbRecorderClaim{}).Where("resource_key = ? AND state = ?", row.ResourceKey, StateIdle).
		Updates(map[string]any{"owner_kind": OwnerLegacy, "owner_id": row.OwnerID, "state": StateUnknown, "version": gorm.Expr("version + 1"), "channel_id": row.ChannelID, "node_id": row.NodeID, "v_host": row.VHost, "app": row.App, "stream": row.Stream}).Error
}
