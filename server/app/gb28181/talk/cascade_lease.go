package talk

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func (r *GormRepo) cascadeLeases(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Where("mode = ?", models.TalkSessionModeCascade)
}

// ReserveCascadeLease shares only the channel unique index with browser talk.
// It does not create a publish token, ZLM source, or browser-accessible session.
func (r *GormRepo) ReserveCascadeLease(ctx context.Context, sessionID string, channelID uint, deviceID, targetID string, expiresAt time.Time) (*models.GbTalkSession, error) {
	if !strings.HasPrefix(sessionID, "cascade-audio-") || len(sessionID) > 64 || channelID == 0 || !leaseGBID(deviceID) || !leaseGBID(targetID) || expiresAt.IsZero() {
		return nil, fmt.Errorf("invalid cascade audio lease identity")
	}
	row := models.GbTalkSession{SessionID: sessionID, ChannelID: channelID, DeviceID: deviceID, TargetID: targetID, Mode: models.TalkSessionModeCascade, App: "cascade", State: models.TalkSessionReserved, ExpiresAt: expiresAt, PublishTokenHash: "", SourceStream: ""}
	setActiveKeys(&row)
	// Write only the lease fields this owner uses. Browser publish/OpenAPI
	// metadata is intentionally absent, including on older installed schemas.
	if err := r.db.WithContext(ctx).Select("SessionID", "ChannelID", "DeviceID", "TargetID", "Mode", "App", "State", "ExpiresAt", "PublishTokenHash", "SourceStream", "LeaseKey", "CreatedAt", "UpdatedAt").Create(&row).Error; err != nil {
		return nil, classifyCreateError(&row, err)
	}
	return &row, nil
}

func leaseGBID(value string) bool { return len(value) == 20 && strings.Trim(value, "0123456789") == "" }

func (r *GormRepo) FindCascadeLease(ctx context.Context, sessionID string) (*models.GbTalkSession, error) {
	var row models.GbTalkSession
	result := r.cascadeLeases(ctx).Where("session_id = ?", sessionID).Limit(1).Find(&row)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, result.Error
	}
	return &row, nil
}

func (r *GormRepo) ListCascadeLeases(ctx context.Context) ([]models.GbTalkSession, error) {
	var rows []models.GbTalkSession
	err := r.cascadeLeases(ctx).Where("state IN ?", nonterminalStates).Order("id").Find(&rows).Error
	return rows, err
}

func (r *GormRepo) ActivateCascadeLease(ctx context.Context, sessionID string, now, expiresAt time.Time) (bool, error) {
	if !expiresAt.After(now) {
		return false, fmt.Errorf("invalid cascade audio lease expiry")
	}
	result := r.cascadeLeases(ctx).Model(&models.GbTalkSession{}).Where("session_id = ? AND state = ? AND expires_at > ?", sessionID, models.TalkSessionReserved, now).
		Updates(map[string]any{"state": models.TalkSessionActive, "started_at": now, "expires_at": expiresAt})
	return result.RowsAffected > 0, result.Error
}

func (r *GormRepo) RenewCascadeLease(ctx context.Context, sessionID string, now, expiresAt time.Time) (bool, error) {
	if !expiresAt.After(now) {
		return false, fmt.Errorf("invalid cascade audio lease expiry")
	}
	result := r.cascadeLeases(ctx).Model(&models.GbTalkSession{}).Where("session_id = ? AND state = ? AND expires_at > ?", sessionID, models.TalkSessionActive, now).Update("expires_at", expiresAt)
	return result.RowsAffected > 0, result.Error
}

// ReleaseCascadeLease is called only after the cascade owner has stopped its
// SIP/media resources. Expiry alone never releases a potentially live sender.
func (r *GormRepo) ReleaseCascadeLease(ctx context.Context, sessionID string, terminal models.TalkSessionState, reason string, at time.Time) (bool, error) {
	if !terminal.IsTerminal() {
		return false, ErrInvalidTerminalState
	}
	if len(reason) > 500 {
		reason = reason[:500]
	}
	result := r.cascadeLeases(ctx).Model(&models.GbTalkSession{}).Where("session_id = ? AND state IN ?", sessionID, nonterminalStates).
		Updates(map[string]any{"state": terminal, "lease_key": nil, "source_key": nil, "recv_key": nil, "ssrc_key": nil, "ended_at": at, "error": reason})
	return result.RowsAffected > 0, result.Error
}
