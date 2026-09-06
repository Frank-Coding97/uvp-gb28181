package playauth

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/openapi/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

var (
	ErrOpenAPIViewerUnavailable = errors.New("openapi viewer dependency unavailable")
	ErrOpenAPIViewerDenied      = errors.New("openapi viewer denied")
	ErrOpenAPIViewerExpired     = errors.New("openapi viewer authorization expired")
)

// OpenAPIViewerBindRequest is the trusted media-process tuple supplied by the
// Hook adapter. It is compared to the signed binding before any row is read;
// it is not an authorization source and contains no client/device authority.
type OpenAPIViewerBindRequest struct {
	NodeUUID        string
	BootNonce       string
	Identifier      string
	Protocol        string
	Schema          string
	VHost           string
	App             string
	Stream          string
	MediaGeneration uint64
}

// BindViewer authenticates a v3 token, then performs the complete durable
// client/scope/device/grant/viewer check in one short transaction. A token's
// signature never substitutes for the current DB epochs or node authority.
// No network I/O is permitted while any row lock is held.
func (s *OpenAPIGrantService) BindViewer(ctx context.Context, token string, request OpenAPIViewerBindRequest) (models.Viewer, error) {
	if ctx == nil || s == nil || s.db == nil || s.signer == nil || s.nodeAuthority == nil || s.now == nil || token == "" {
		return models.Viewer{}, ErrOpenAPIViewerDenied
	}
	claims, err := s.signer.AuthenticateOpenAPI(token)
	if err != nil {
		return models.Viewer{}, ErrOpenAPIViewerDenied
	}
	if err := validateOpenAPIViewerRequest(claims, request); err != nil {
		return models.Viewer{}, err
	}

	var hint struct {
		ClientID int64 `gorm:"column:client_id"`
	}
	result := s.db.WithContext(ctx).Table((models.PlayGrant{}).TableName()).
		Select("client_id").Where("grant_id = ?", claims.GrantID).Limit(2).Find(&hint)
	if result.Error != nil {
		return models.Viewer{}, normalizeOpenAPIViewerError(result.Error)
	}
	if result.RowsAffected != 1 || hint.ClientID <= 0 || hint.ClientID != claims.ClientID {
		return models.Viewer{}, ErrOpenAPIViewerDenied
	}

	var bound models.Viewer
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := s.now().UTC().Truncate(time.Microsecond)
		if now.IsZero() {
			return ErrOpenAPIViewerUnavailable
		}
		client, err := lockOpenAPIClient(tx.WithContext(ctx), claims.ClientID)
		if err != nil {
			return err
		}
		if client.Status != models.StatusActive || client.AuthEpoch != claims.ClientEpoch || client.OwnerDeptID == 0 {
			return ErrOpenAPIViewerDenied
		}
		scope, err := lockOpenAPIScope(tx.WithContext(ctx), client.ID, claims.Scope)
		if err != nil {
			return err
		}
		if !scope.Enabled || scope.ScopeEpoch != claims.ScopeEpoch || scope.Scope != openAPIPlayScope {
			return ErrOpenAPIViewerDenied
		}
		deviceEpoch, err := lockOpenAPIRootDevice(tx.WithContext(ctx), claims.DeviceID)
		if err != nil {
			return err
		}
		if deviceEpoch != claims.DeviceEpoch {
			return ErrOpenAPIViewerDenied
		}
		grant, err := lockOpenAPIGrant(tx.WithContext(ctx), claims.GrantID)
		if err != nil {
			return err
		}
		if err := validateViewerGrant(grant, claims, now); err != nil {
			return err
		}
		if _, err := resource.New(tx.WithContext(ctx)).GetChannel(ctx, client.OwnerDeptID, claims.DeviceID, claims.ChannelID); err != nil {
			if errors.Is(err, resource.ErrResourceNotFound) {
				return ErrOpenAPIViewerDenied
			}
			return ErrOpenAPIViewerUnavailable
		}
		if err := s.nodeAuthority.AuthorizeOpenAPI(ctx, tx.WithContext(ctx), OpenAPINodeAuthorization{
			NodeUUID: claims.NodeUUID, BootNonce: claims.BootNonce, Protocol: claims.Protocol,
		}); err != nil {
			return normalizeOpenAPIViewerError(err)
		}

		existing, exists, err := lockOpenAPIViewerByGrant(tx.WithContext(ctx), claims.GrantID)
		if err != nil {
			return err
		}
		switch grant.State {
		case models.GrantStateIssued:
			if exists {
				return ErrOpenAPIViewerUnavailable
			}
			if !now.Before(grant.ExpiresAt) || !now.Before(time.Unix(claims.ExpiresAt, 0)) {
				return ErrOpenAPIViewerExpired
			}
			seen := now
			row := models.Viewer{
				GrantID: claims.GrantID, NodeUUID: claims.NodeUUID, BootNonce: claims.BootNonce, Identifier: request.Identifier,
				Schema: claims.Schema, VHost: claims.VHost, App: claims.App, Stream: claims.Stream,
				MediaGeneration: claims.MediaGeneration, State: models.ViewerStateActive,
				LastSeenAt: &seen, Attempts: 0, LastErrorClass: "", CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
				return ErrOpenAPIViewerUnavailable
			}
			updated := tx.Model(&models.PlayGrant{}).
				Where("grant_id = ? AND state = ?", grant.GrantID, models.GrantStateIssued).
				Updates(map[string]any{"state": models.GrantStateBound, "updated_at": now, "reason": ""})
			if updated.Error != nil || updated.RowsAffected != 1 {
				return ErrOpenAPIViewerUnavailable
			}
			bound = row
			return nil
		case models.GrantStateBound:
			if !exists || !sameOpenAPIViewerBinding(existing, claims, request) {
				return ErrOpenAPIViewerDenied
			}
			if existing.State != models.ViewerStateActive {
				return ErrOpenAPIViewerDenied
			}
			if existing.LastSeenAt != nil && existing.LastSeenAt.UTC().Equal(now) && existing.UpdatedAt.UTC().Equal(now) {
				bound = existing
				return nil
			}
			seen := now
			updated := tx.Model(&models.Viewer{}).
				Where("id = ? AND grant_id = ? AND state = ?", existing.ID, claims.GrantID, models.ViewerStateActive).
				Updates(map[string]any{"last_seen_at": seen, "updated_at": now})
			if updated.Error != nil || updated.RowsAffected != 1 {
				return ErrOpenAPIViewerUnavailable
			}
			existing.LastSeenAt = &seen
			existing.UpdatedAt = now
			bound = existing
			return nil
		case models.GrantStateExpired:
			return ErrOpenAPIViewerExpired
		default:
			return ErrOpenAPIViewerDenied
		}
	})
	if err != nil {
		return models.Viewer{}, normalizeOpenAPIViewerError(err)
	}
	return bound, nil
}

func lockOpenAPIViewerByGrant(tx *gorm.DB, grantID string) (models.Viewer, bool, error) {
	var rows []models.Viewer
	result := lockedOpenAPIModel(tx, &models.Viewer{}, (models.Viewer{}).TableName()).
		Where("grant_id = ?", grantID).Limit(2).Find(&rows)
	if result.Error != nil {
		return models.Viewer{}, false, normalizeOpenAPIViewerError(result.Error)
	}
	if len(rows) > 1 || result.RowsAffected > 1 {
		return models.Viewer{}, false, ErrOpenAPIViewerUnavailable
	}
	if len(rows) == 0 {
		return models.Viewer{}, false, nil
	}
	return rows[0], true, nil
}

func validateOpenAPIViewerRequest(claims OpenAPIClaims, request OpenAPIViewerBindRequest) error {
	if !validOpenAPINodeUUID(request.NodeUUID) || !validOpenAPIBootNonce(request.BootNonce) || !validOpenAPIField(request.Identifier, 128) || !validOpenAPIField(request.Schema, 32) || !validOpenAPIField(request.VHost, 128) || !validOpenAPIField(request.App, 64) || !validOpenAPIField(request.Stream, 255) || request.MediaGeneration == 0 {
		return ErrOpenAPIViewerDenied
	}
	if request.Protocol != "https-flv" && request.Protocol != "wss-flv" {
		return ErrOpenAPIViewerDenied
	}
	if request.NodeUUID != claims.NodeUUID || request.BootNonce != claims.BootNonce || request.Protocol != claims.Protocol || request.Schema != claims.Schema || request.VHost != claims.VHost || request.App != claims.App || request.Stream != claims.Stream || request.MediaGeneration != claims.MediaGeneration {
		return ErrOpenAPIViewerDenied
	}
	return nil
}

func validateViewerGrant(grant models.PlayGrant, claims OpenAPIClaims, now time.Time) error {
	if grant.GrantID != claims.GrantID || grant.ClientID != claims.ClientID || grant.Scope != claims.Scope || grant.ClientEpoch != claims.ClientEpoch || grant.ScopeEpoch != claims.ScopeEpoch || grant.DeviceEpoch != claims.DeviceEpoch || grant.DeviceID == nil || grant.ChannelID == nil || grant.NodeUUID == nil || grant.BootNonce == nil || grant.Schema == nil || grant.VHost == nil || grant.App == nil || grant.Stream == nil || grant.MediaGeneration == nil || grant.Protocol == nil {
		return ErrOpenAPIViewerDenied
	}
	if *grant.DeviceID != claims.DeviceID || *grant.ChannelID != claims.ChannelID || *grant.NodeUUID != claims.NodeUUID || *grant.BootNonce != claims.BootNonce || *grant.Schema != claims.Schema || *grant.VHost != claims.VHost || *grant.App != claims.App || *grant.Stream != claims.Stream || *grant.MediaGeneration != claims.MediaGeneration || *grant.Protocol != claims.Protocol {
		return ErrOpenAPIViewerDenied
	}
	if !grant.IssuedAt.UTC().Equal(time.Unix(claims.IssuedAt, 0).UTC()) || !grant.ExpiresAt.UTC().Equal(time.Unix(claims.ExpiresAt, 0).UTC()) {
		return ErrOpenAPIViewerDenied
	}
	if grant.State == models.GrantStateIssued && !now.Before(grant.ExpiresAt) {
		return ErrOpenAPIViewerExpired
	}
	if grant.State != models.GrantStateIssued && grant.State != models.GrantStateBound && grant.State != models.GrantStateExpired {
		return ErrOpenAPIViewerDenied
	}
	return nil
}

func sameOpenAPIViewerBinding(viewer models.Viewer, claims OpenAPIClaims, request OpenAPIViewerBindRequest) bool {
	return viewer.GrantID == claims.GrantID && viewer.NodeUUID == request.NodeUUID && viewer.BootNonce == request.BootNonce && viewer.Identifier == request.Identifier && viewer.Schema == request.Schema && viewer.VHost == request.VHost && viewer.App == request.App && viewer.Stream == request.Stream && viewer.MediaGeneration == request.MediaGeneration
}

func normalizeOpenAPIViewerError(err error) error {
	if err == nil {
		return ErrOpenAPIViewerUnavailable
	}
	switch {
	case errors.Is(err, ErrOpenAPIViewerUnavailable):
		return ErrOpenAPIViewerUnavailable
	case errors.Is(err, ErrOpenAPIViewerDenied):
		return ErrOpenAPIViewerDenied
	case errors.Is(err, ErrOpenAPIViewerExpired):
		return ErrOpenAPIViewerExpired
	case errors.Is(err, ErrOpenAPIGrantUnavailable):
		return ErrOpenAPIViewerUnavailable
	case errors.Is(err, ErrOpenAPIGrantDenied):
		return ErrOpenAPIViewerDenied
	case errors.Is(err, ErrOpenAPIGrantExpired):
		return ErrOpenAPIViewerExpired
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ErrOpenAPIViewerUnavailable
	default:
		return ErrOpenAPIViewerUnavailable
	}
}
