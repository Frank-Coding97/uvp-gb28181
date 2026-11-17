package videoshare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"
)

type auditTraceKey struct{}

// WithAuditTraceID accepts the server-generated request correlation identifier,
// never a client-supplied header. Background work receives its own identifier.
func WithAuditTraceID(ctx context.Context, traceID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	valid := len(traceID) > 0 && len(traceID) <= 64
	for _, r := range traceID {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			valid = false
		}
	}
	if !valid {
		traceID = uuid.NewString()
	}
	return context.WithValue(ctx, auditTraceKey{}, traceID)
}

type auditMetadata struct {
	Event        string     `json:"event"`
	OperatorID   uint       `json:"operator_id"`
	ChannelRowID uint       `json:"channel_row_id"`
	Generation   string     `json:"generation"`
	ExpiresAt    *time.Time `json:"expires_at"`
	Result       string     `json:"result"`
	TraceID      string     `json:"trace_id"`
}

func shareAuditMetadata(ctx context.Context, event string, operatorID uint, share *gbmodels.GbChannelVideoShare) auditMetadata {
	digest := sha256.Sum256([]byte(share.Generation))
	traceID, _ := ctx.Value(auditTraceKey{}).(string)
	if traceID == "" {
		traceID = uuid.NewString()
	}
	return auditMetadata{Event: event, OperatorID: operatorID, ChannelRowID: share.ChannelPK,
		Generation: hex.EncodeToString(digest[:8]), ExpiresAt: share.ExpiresAt, Result: "success", TraceID: traceID}
}

// RecordAuditTx persists only lifecycle metadata in the caller's transaction.
// A failed audit insert rolls back the accompanying lifecycle change. It does
// not emit a success log before that transaction has committed.
func RecordAuditTx(ctx context.Context, tx *gorm.DB, event string, operatorID uint, share *gbmodels.GbChannelVideoShare) error {
	if ctx == nil || tx == nil || tx.Statement == nil || share == nil || share.ChannelPK == 0 || share.Generation == "" {
		return errors.New("video share audit requires context, transaction, and share identity")
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errors.New("video share audit requires an explicit transaction")
	}
	switch event {
	case "video_share.enabled", "video_share.disabled", "video_share.regenerated", "video_share.activated", "video_share.expired", "video_share.revoked", "video_share.auto_pending":
	default:
		return errors.New("unknown video share audit event")
	}
	metadata, err := json.Marshal(shareAuditMetadata(ctx, event, operatorID, share))
	if err != nil {
		return err
	}
	return tx.WithContext(ctx).Create(&basemodels.SysOperationLog{
		UserID: operatorID, Module: "gb28181.video_share", Operation: event,
		Path: "/api/gb28181/channel-shares/:id", RequestData: string(metadata), StatusCode: http.StatusOK,
	}).Error
}

// ScanExpired records missed expirations in bounded, idempotent batches. Media
// authorization compares expires_at directly and never waits for this scanner.
func (s *Service) ScanExpired(ctx context.Context, limit int) (int, error) {
	if ctx == nil || s == nil || s.db == nil || s.now == nil {
		return 0, errors.New("video share expiry scan requires context and service")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if traceID, _ := ctx.Value(auditTraceKey{}).(string); traceID == "" {
		ctx = WithAuditTraceID(ctx, "")
	}
	now := s.now().UTC()
	var expired []gbmodels.GbChannelVideoShare
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authoritytx.Acquire(ctx, tx); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("enabled = ? AND revoked_at IS NULL AND expires_at <= ?", true, now).
			Order("id ASC").Limit(limit).Find(&expired).Error; err != nil {
			return err
		}
		for i := range expired {
			share := &expired[i]
			updated := tx.Model(&gbmodels.GbChannelVideoShare{}).
				Where("id = ? AND revision = ? AND generation = ? AND enabled = ? AND revoked_at IS NULL AND expires_at <= ?",
					share.ID, share.Revision, share.Generation, true, now).
				Updates(map[string]any{"enabled": false, "revoked_at": now, "revoke_reason": "expired", "revision": share.Revision + 1, "updated_at": now})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrConflict
			}
			share.Enabled, share.RevokedAt, share.RevokeReason, share.Revision = false, &now, "expired", share.Revision+1
			if err := RecordAuditTx(ctx, tx, "video_share.expired", 0, share); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for i := range expired {
		metadata := shareAuditMetadata(ctx, "video_share.expired", 0, &expired[i])
		app.Log(ctx).Named("audit").Info("视频分享已到期",
			zap.String("event", metadata.Event), zap.Uint("operator_id", metadata.OperatorID),
			zap.Uint("channel_row_id", metadata.ChannelRowID), zap.String("generation", metadata.Generation),
			zap.Timep("expires_at", metadata.ExpiresAt), zap.String("result", metadata.Result), zap.String("trace_id", metadata.TraceID))
	}
	return len(expired), nil
}
