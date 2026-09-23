package playauth

import (
	"context"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// FailUnboundGrant compensates only an application whose URL was not released.
// It never stops a shared stream or changes a known viewer. A caller handling
// an uncertain HTTP response must leave the grant intact instead of calling
// this method. The caller supplies its bounded cleanup context explicitly.
func (s *OpenAPIGrantService) FailUnboundGrant(ctx context.Context, clientID int64, grantID string) error {
	if ctx == nil || clientID <= 0 || !validOpenAPIUUID(grantID) {
		return ErrOpenAPIGrantDenied
	}
	if s == nil || s.db == nil || s.now == nil || ctx.Err() != nil {
		return ErrOpenAPIGrantUnavailable
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Match admission/binding/revocation's parent-first order. This is a
		// terminal transition, so disabled clients may still be compensated.
		if _, err := lockOpenAPIClient(tx, clientID); err != nil {
			return err
		}
		grant, err := lockOpenAPIGrant(tx, grantID)
		if err != nil {
			return err
		}
		if grant.ClientID != clientID {
			return ErrOpenAPIGrantDenied
		}
		switch grant.State {
		case models.GrantStateRevoked, models.GrantStateFailed, models.GrantStateExpired:
			return nil // Never rewrite a revocation tombstone or terminal time.
		case models.GrantStatePending, models.GrantStateIssued:
		default:
			return ErrOpenAPIGrantDenied
		}
		if _, exists, err := lockOpenAPIViewerByGrant(tx, grantID); err != nil {
			return err
		} else if exists {
			return ErrOpenAPIGrantDenied
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		if now.IsZero() {
			return ErrOpenAPIGrantUnavailable
		}
		result := tx.Model(&models.PlayGrant{}).
			Where("grant_id = ? AND client_id = ? AND state = ?", grantID, clientID, grant.State).
			Updates(map[string]any{"state": models.GrantStateFailed, "reason": "apply_failed", "updated_at": now})
		if result.Error != nil || result.RowsAffected != 1 {
			return ErrOpenAPIGrantUnavailable
		}
		return nil
	})
	if err != nil {
		return normalizeOpenAPIGrantError(err)
	}
	return nil
}
