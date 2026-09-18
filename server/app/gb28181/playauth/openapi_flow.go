package playauth

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

var (
	ErrOpenAPIFlowUnavailable = errors.New("openapi flow dependency unavailable")
	ErrOpenAPIFlowDenied      = errors.New("openapi flow denied")
)

// OpenAPIFlowReport is the trusted, internal media-hook observation used to
// close a durable viewer. It is not an HTTP DTO and carries no client or
// grant authority; the database binding remains authoritative.
type OpenAPIFlowReport struct {
	NodeUUID   string `json:"nodeUuid"`
	BootNonce  string `json:"bootNonce"`
	Identifier string `json:"identifier"`
	Protocol   string `json:"protocol"`
	Schema     string `json:"schema"`
	VHost      string `json:"vhost"`
	App        string `json:"app"`
	Stream     string `json:"stream"`
	Player     bool   `json:"player"`
}

type openAPIFlowHint struct {
	GrantID  string `gorm:"column:grant_id"`
	ClientID int64  `gorm:"column:client_id"`
}

// ObserveFlow records the normal end of one already-known player session. It
// only performs the short client -> grant -> viewer transaction after a
// viewer-identity hint has found a candidate row. No row is created or
// guessed, and no media process is contacted while database locks are held.
func (s *OpenAPIGrantService) ObserveFlow(ctx context.Context, report OpenAPIFlowReport) error {
	if ctx == nil {
		return ErrOpenAPIFlowDenied
	}
	if s == nil || s.db == nil || s.now == nil {
		return ErrOpenAPIFlowUnavailable
	}
	if !report.Player {
		return nil
	}
	if err := validateOpenAPIFlowReport(report); err != nil {
		return err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return ErrOpenAPIFlowUnavailable
	}

	// The Hook identity is only a lookup hint. It is deliberately re-read and
	// checked under the mandated row locks before any state transition.
	var hints []openAPIFlowHint
	result := s.db.WithContext(ctx).
		Table((models.Viewer{}).TableName()+" AS v").
		Select("v.grant_id, g.client_id").
		Joins("JOIN "+(models.PlayGrant{}).TableName()+" AS g ON g.grant_id = v.grant_id").
		Where("v.node_uuid = ? AND v.boot_nonce = ? AND v.identifier = ?", report.NodeUUID, report.BootNonce, report.Identifier).
		Limit(2).
		Find(&hints)
	if result.Error != nil {
		return normalizeOpenAPIFlowError(result.Error)
	}
	if len(hints) == 0 || result.RowsAffected == 0 {
		return nil
	}
	if len(hints) != 1 || result.RowsAffected != 1 || hints[0].GrantID == "" || hints[0].ClientID <= 0 {
		return ErrOpenAPIFlowDenied
	}
	hint := hints[0]

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		client, err := lockOpenAPIClient(tx.WithContext(ctx), hint.ClientID)
		if err != nil {
			return err
		}
		grant, err := lockOpenAPIGrant(tx.WithContext(ctx), hint.GrantID)
		if err != nil {
			return err
		}
		if grant.ClientID != client.ID {
			return ErrOpenAPIFlowDenied
		}
		viewer, exists, err := lockOpenAPIViewerByGrant(tx.WithContext(ctx), grant.GrantID)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		if err := validateOpenAPIFlowBinding(grant, viewer, report); err != nil {
			return err
		}
		if viewer.State == models.ViewerStateClosed {
			return nil
		}

		// Revocation owns the complete worker token, including updated_at.
		// Liveness is independent: a final flow must not invalidate an in-flight
		// claim or close a row without the worker's fresh absence confirmation.
		if grant.State == models.GrantStateRevoked || viewer.State == models.ViewerStateRevokePending {
			if viewer.LastSeenAt != nil && !viewer.LastSeenAt.UTC().Truncate(time.Microsecond).Before(now) {
				return nil
			}
			updated := tx.WithContext(ctx).Model(&models.Viewer{}).
				Where("id = ? AND grant_id = ? AND state = ?", viewer.ID, grant.GrantID, viewer.State).
				UpdateColumn("last_seen_at", now)
			if updated.Error != nil || updated.RowsAffected != 1 {
				return ErrOpenAPIFlowUnavailable
			}
			return nil
		}

		if viewer.State != models.ViewerStateActive {
			return nil
		}
		if grant.State != models.GrantStateBound && grant.State != models.GrantStateExpired {
			return ErrOpenAPIFlowDenied
		}
		updated := tx.WithContext(ctx).Model(&models.Viewer{}).
			Where("id = ? AND grant_id = ? AND state = ?", viewer.ID, grant.GrantID, models.ViewerStateActive).
			Updates(map[string]any{
				"state":            models.ViewerStateClosed,
				"last_seen_at":     now,
				"retry_at":         nil,
				"attempts":         0,
				"last_error_class": "",
				"updated_at":       now,
			})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return ErrOpenAPIFlowUnavailable
		}
		return nil
	})
	if err != nil {
		return normalizeOpenAPIFlowError(err)
	}
	return nil
}

func validateOpenAPIFlowReport(report OpenAPIFlowReport) error {
	if !validOpenAPINodeUUID(report.NodeUUID) ||
		!validOpenAPIBootNonce(report.BootNonce) ||
		!validOpenAPIField(report.Identifier, 128) ||
		!validOpenAPIField(report.Schema, 32) ||
		!validOpenAPIField(report.VHost, 128) ||
		!validOpenAPIField(report.App, 64) ||
		!validOpenAPIField(report.Stream, 255) {
		return ErrOpenAPIFlowDenied
	}
	if report.Protocol != "https-flv" && report.Protocol != "wss-flv" {
		return ErrOpenAPIFlowDenied
	}
	return nil
}

func validateOpenAPIFlowBinding(grant models.PlayGrant, viewer models.Viewer, report OpenAPIFlowReport) error {
	if grant.GrantID == "" || grant.Scope != openAPIPlayScope || viewer.GrantID != grant.GrantID ||
		grant.NodeUUID == nil || grant.BootNonce == nil || grant.Schema == nil || grant.VHost == nil ||
		grant.App == nil || grant.Stream == nil || grant.MediaGeneration == nil || grant.Protocol == nil {
		return ErrOpenAPIFlowDenied
	}
	if viewer.NodeUUID != report.NodeUUID || viewer.BootNonce != report.BootNonce || viewer.Identifier != report.Identifier ||
		viewer.Schema != report.Schema || viewer.VHost != report.VHost || viewer.App != report.App || viewer.Stream != report.Stream {
		return ErrOpenAPIFlowDenied
	}
	if *grant.NodeUUID != report.NodeUUID || *grant.BootNonce != report.BootNonce || *grant.Protocol != report.Protocol ||
		*grant.Schema != report.Schema || *grant.VHost != report.VHost || *grant.App != report.App || *grant.Stream != report.Stream ||
		*grant.MediaGeneration == 0 || viewer.MediaGeneration == 0 || *grant.MediaGeneration != viewer.MediaGeneration {
		return ErrOpenAPIFlowDenied
	}
	return nil
}

func normalizeOpenAPIFlowError(err error) error {
	if err == nil {
		return ErrOpenAPIFlowUnavailable
	}
	switch {
	case errors.Is(err, ErrOpenAPIFlowDenied), errors.Is(err, ErrOpenAPIGrantDenied), errors.Is(err, ErrOpenAPIViewerDenied):
		return ErrOpenAPIFlowDenied
	case errors.Is(err, ErrOpenAPIFlowUnavailable), errors.Is(err, ErrOpenAPIGrantUnavailable), errors.Is(err, ErrOpenAPIViewerUnavailable), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ErrOpenAPIFlowUnavailable
	default:
		return ErrOpenAPIFlowUnavailable
	}
}
