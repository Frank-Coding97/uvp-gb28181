package gb28181

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

var ErrCascadeSnapshotSessionConflict = errors.New("cascade snapshot session conflicts with an existing request")

func reserveCascadeSnapshotSession(ctx context.Context, db *gorm.DB, session model.GbCascadeSnapshotSession) (model.GbCascadeSnapshotSession, bool, error) {
	var existing model.GbCascadeSnapshotSession
	err := db.WithContext(ctx).Where("platform_id = ? AND upstream_session_id = ?", session.PlatformID, session.UpstreamSessionID).First(&existing).Error
	if err == nil {
		if !replayableCascadeSnapshotSession(existing, session) {
			return existing, false, ErrCascadeSnapshotSessionConflict
		}
		return existing, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return existing, false, err
	}
	if session.DownstreamSessionID == nil {
		id := uuid.NewString()
		session.DownstreamSessionID = &id
	}
	if session.UploadToken == nil {
		var secret [32]byte
		if _, err := rand.Read(secret[:]); err != nil {
			return existing, false, err
		}
		token := hex.EncodeToString(secret[:])
		session.UploadToken = &token
	}
	if err := db.WithContext(ctx).Create(&session).Error; err != nil {
		// A concurrent retransmission can win the unique-key insert. Read back
		// and accept it only if it represents the same authorized target.
		if lookupErr := db.WithContext(ctx).Where("platform_id = ? AND upstream_session_id = ?", session.PlatformID, session.UpstreamSessionID).First(&existing).Error; lookupErr == nil {
			if replayableCascadeSnapshotSession(existing, session) {
				return existing, false, nil
			}
			return existing, false, ErrCascadeSnapshotSessionConflict
		}
		if session.RequestKey != "" {
			var count int64
			if db.WithContext(ctx).Model(&model.GbCascadeSnapshotSession{}).Where("platform_id = ? AND request_key = ?", session.PlatformID, session.RequestKey).Count(&count).Error == nil && count > 0 {
				return existing, false, ErrCascadeSnapshotSessionConflict
			}
		}
		return existing, false, err
	}
	return session, true, nil
}

func replayableCascadeSnapshotSession(existing, request model.GbCascadeSnapshotSession) bool {
	return sameCascadeSnapshotSession(existing, request) &&
		(existing.RequestResult != "" || ((existing.State == model.CascadeSnapshotSessionPending || existing.State == model.CascadeSnapshotSessionForwarding) && time.Now().Before(existing.ExpiresAt)))
}

func findCascadeSnapshotSession(ctx context.Context, db *gorm.DB, platformID uint64, sessionID string) (model.GbCascadeSnapshotSession, error) {
	var session model.GbCascadeSnapshotSession
	err := db.WithContext(ctx).Where("platform_id = ? AND upstream_session_id = ?", platformID, sessionID).First(&session).Error
	return session, err
}

func findCascadeSnapshotSessions(ctx context.Context, db *gorm.DB, sessionID string) ([]model.GbCascadeSnapshotSession, error) {
	var sessions []model.GbCascadeSnapshotSession
	// Legacy captures retain their original identity; new captures never route
	// by an upstream-supplied identifier shared by multiple platforms.
	err := db.WithContext(ctx).Where("downstream_session_id = ? OR (downstream_session_id IS NULL AND upstream_session_id = ?)", sessionID, sessionID).Find(&sessions).Error
	return sessions, err
}

func sameCascadeSnapshotSession(a, b model.GbCascadeSnapshotSession) bool {
	return a.PlatformID == b.PlatformID && a.UpstreamSessionID == b.UpstreamSessionID &&
		a.UpstreamUploadURL == b.UpstreamUploadURL &&
		a.PublishedChannelID == b.PublishedChannelID && a.SourceDeviceID == b.SourceDeviceID && a.SourceChannelID == b.SourceChannelID &&
		a.SourceDeviceEpoch == b.SourceDeviceEpoch &&
		a.RequestKey == b.RequestKey && a.RequestFingerprint == b.RequestFingerprint &&
		a.ConfigRevision == b.ConfigRevision && a.ProjectionRevision == b.ProjectionRevision
}

func cascadeSnapshotSessionAuthorized(session model.GbCascadeSnapshotSession, platform model.GbCascadePlatform, sourceDeviceID, sourceChannelID uint64, now time.Time) bool {
	return session.State == model.CascadeSnapshotSessionPending && now.Before(session.ExpiresAt) &&
		session.PlatformID == platform.ID && platform.Enabled && platform.ConfigEnabled &&
		session.ConfigRevision == platform.ConfigRevision && session.ProjectionRevision == platform.ProjectionRevision &&
		session.SourceDeviceID == sourceDeviceID && session.SourceChannelID == sourceChannelID
}

func completeCascadeSnapshotSession(ctx context.Context, db *gorm.DB, session model.GbCascadeSnapshotSession, now time.Time) error {
	result := db.WithContext(ctx).Model(&model.GbCascadeSnapshotSession{}).
		Where("id = ? AND state = ? AND updated_at = ?", session.ID, model.CascadeSnapshotSessionForwarding, session.UpdatedAt).
		Updates(map[string]interface{}{"state": model.CascadeSnapshotSessionCompleted, "completed_at": now, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("cascade snapshot session is no longer forwarding")
	}
	return nil
}

func claimCascadeSnapshotSession(ctx context.Context, db *gorm.DB, id uint64, now time.Time) (bool, error) {
	result := db.WithContext(ctx).Model(&model.GbCascadeSnapshotSession{}).
		Where("id = ? AND state = ?", id, model.CascadeSnapshotSessionPending).
		Updates(map[string]interface{}{"state": model.CascadeSnapshotSessionForwarding, "updated_at": now})
	return result.RowsAffected == 1, result.Error
}

func reclaimStaleCascadeSnapshotForwarding(ctx context.Context, db *gorm.DB, session model.GbCascadeSnapshotSession, now time.Time, lease time.Duration) (bool, error) {
	if session.State != model.CascadeSnapshotSessionForwarding || now.Sub(session.UpdatedAt) < lease {
		return false, nil
	}
	result := db.WithContext(ctx).Model(&model.GbCascadeSnapshotSession{}).
		Where("id = ? AND state = ? AND updated_at = ?", session.ID, model.CascadeSnapshotSessionForwarding, session.UpdatedAt).
		Updates(map[string]interface{}{"state": model.CascadeSnapshotSessionPending, "updated_at": now})
	return result.RowsAffected == 1, result.Error
}

func retryCascadeSnapshotSession(ctx context.Context, db *gorm.DB, session model.GbCascadeSnapshotSession, now time.Time) error {
	return db.WithContext(ctx).Model(&model.GbCascadeSnapshotSession{}).
		Where("id = ? AND state = ? AND updated_at = ?", session.ID, model.CascadeSnapshotSessionForwarding, session.UpdatedAt).
		Updates(map[string]interface{}{"state": model.CascadeSnapshotSessionPending, "updated_at": now}).Error
}

func failCascadeSnapshotSession(ctx context.Context, db *gorm.DB, id uint64) {
	_ = db.WithContext(ctx).Model(&model.GbCascadeSnapshotSession{}).
		Where("id = ? AND state = ?", id, model.CascadeSnapshotSessionPending).
		Updates(map[string]interface{}{"state": model.CascadeSnapshotSessionFailed, "request_result": "ERROR", "updated_at": time.Now().UTC()}).Error
}

func recordCascadeSnapshotRequestResult(ctx context.Context, db *gorm.DB, id uint64, result string) error {
	return db.WithContext(ctx).Model(&model.GbCascadeSnapshotSession{}).
		Where("id = ? AND request_result = ?", id, "").UpdateColumn("request_result", result).Error
}

// Retransmissions observe the durable result; they never take ownership of a
// capture that may already have reached the device before a process restart.
func awaitCascadeSnapshotRequestResult(ctx context.Context, db *gorm.DB, id uint64) (string, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var session model.GbCascadeSnapshotSession
		if err := db.WithContext(ctx).First(&session, id).Error; err != nil {
			return "", err
		}
		if session.RequestResult != "" {
			return session.RequestResult, nil
		}
		if !time.Now().Before(session.ExpiresAt) {
			return "", fmt.Errorf("snapshot request expired")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}
