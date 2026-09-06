package playauth

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	openapiclient "uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

const (
	// OpenAPIRevocationStatusUnknown is deliberately not a success state. It is
	// returned when durable records do not prove that every revoked grant had a
	// viewer which the media worker later observed as closed.
	OpenAPIRevocationStatusUnknown = "unknown"
	OpenAPIRevocationStatusPending = "pending"
	OpenAPIRevocationStatusClosed  = "closed"

	// openAPIRevocationRetryError is a fixed, non-sensitive worker handoff
	// marker. The network worker may replace it with a more specific diagnostic
	// only after it has a fresh, authenticated media result.
	openAPIRevocationRetryError = "revocation_pending"
)

var (
	ErrOpenAPIRevocationUnavailable = errors.New("openapi revocation dependency unavailable")
	ErrOpenAPIRevocationInvalid     = errors.New("invalid openapi revocation intent")
	ErrOpenAPIRevocationStale       = errors.New("stale openapi revocation intent")
)

// OpenAPIRevocationStore is the durable, short-transaction half of T12. It
// records the exact grants affected by a client/scope epoch change and
// prepares already-known viewers for a later network worker. It never calls a
// media process and never treats a missing viewer as a successful kick.
type OpenAPIRevocationStore struct {
	db  *gorm.DB
	now func() time.Time
}

// OpenAPIRevocationProgress is intentionally narrower than the management
// response DTO. Pending includes every known pending/active/revoke_pending
// viewer; closed includes only viewers durably marked closed by a worker.
// Status remains unknown when a revoked grant has no viewer row, because that
// boundary may be the Hook-before-bind race rather than an observed clean
// exit.
type OpenAPIRevocationProgress struct {
	Pending int64  `json:"pending"`
	Closed  int64  `json:"closed"`
	Status  string `json:"status"`
}

func NewOpenAPIRevocationStore(db *gorm.DB, now func() time.Time) *OpenAPIRevocationStore {
	if now == nil {
		now = time.Now
	}
	return &OpenAPIRevocationStore{db: db, now: now}
}

var _ openapiclient.RevocationIntentStore = (*OpenAPIRevocationStore)(nil)

// RecordRevocationIntent implements client.RevocationIntentStore. The caller
// owns the surrounding transaction; this method deliberately does not start a
// nested transaction or perform any network work.
func (s *OpenAPIRevocationStore) RecordRevocationIntent(ctx context.Context, tx *gorm.DB, intent openapiclient.RevocationIntent) error {
	if err := validateOpenAPIRevocationIntent(ctx, tx, intent); err != nil {
		return err
	}
	if s == nil || s.now == nil {
		return ErrOpenAPIRevocationUnavailable
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return ErrOpenAPIRevocationUnavailable
	}
	ctx = normalizeOpenAPIRevocationContext(ctx)
	tx = tx.WithContext(ctx)

	// All lifecycle writers use client -> scope -> grant ordering. Keeping the
	// client row locked through the whole short transaction prevents a new
	// admission from slipping between epoch validation and grant selection.
	clientRow, err := lockOpenAPIClient(tx, intent.ClientID)
	if err != nil {
		return normalizeOpenAPIRevocationError(err)
	}
	if err := validateCurrentClientForIntent(clientRow, intent); err != nil {
		return err
	}

	if intent.Scope != "" {
		scopeRow, err := lockOpenAPIScope(tx, intent.ClientID, intent.Scope)
		if err != nil {
			return normalizeOpenAPIRevocationError(err)
		}
		if scopeRow.Scope != intent.Scope || scopeRow.ScopeEpoch != intent.ScopeEpoch || scopeRow.Enabled {
			return ErrOpenAPIRevocationStale
		}
		// Client.Service emits an intent for every known scope. Only the media
		// scope owns play grants/viewers; other capability revocations are a
		// validated, durable no-op and must not break device-list management.
		if intent.Scope != openAPIPlayScope {
			return nil
		}
	}

	grantIDs, err := selectOpenAPIRevocationGrantIDs(tx, intent, clientRow)
	if err != nil {
		return normalizeOpenAPIRevocationError(err)
	}
	for _, grantID := range grantIDs {
		grant, err := lockOpenAPIGrant(tx, grantID)
		if err != nil {
			return normalizeOpenAPIRevocationError(err)
		}
		if !matchesOpenAPIRevocationIntent(grant, intent, clientRow) || !isOpenAPINonTerminalGrant(grant.State) {
			continue
		}

		updated := tx.Model(&models.PlayGrant{}).
			Where("grant_id = ? AND state IN ?", grant.GrantID, openAPINonTerminalGrantStates).
			Updates(map[string]any{
				"state":      models.GrantStateRevoked,
				"reason":     intent.Reason,
				"updated_at": now,
			})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return ErrOpenAPIRevocationUnavailable
		}
		if err := markOpenAPIViewerForRevocation(tx, grant.GrantID, now); err != nil {
			return err
		}
	}
	return nil
}

// Progress reports durable evidence only. It intentionally does not infer
// clear from the absence of a viewer, because a revoked grant can be observed
// before its Hook bind is committed.
func (s *OpenAPIRevocationStore) Progress(ctx context.Context, clientID int64) (OpenAPIRevocationProgress, error) {
	if ctx == nil || clientID <= 0 {
		return OpenAPIRevocationProgress{}, ErrOpenAPIRevocationInvalid
	}
	if s == nil || s.db == nil {
		return OpenAPIRevocationProgress{}, ErrOpenAPIRevocationUnavailable
	}
	ctx = normalizeOpenAPIRevocationContext(ctx)
	var grantIDs []string
	if result := s.db.WithContext(ctx).Model(&models.PlayGrant{}).
		Where("client_id = ? AND state = ?", clientID, models.GrantStateRevoked).
		Order("grant_id ASC").Pluck("grant_id", &grantIDs); result.Error != nil {
		return OpenAPIRevocationProgress{}, ErrOpenAPIRevocationUnavailable
	}
	if len(grantIDs) == 0 {
		return OpenAPIRevocationProgress{Status: OpenAPIRevocationStatusUnknown}, nil
	}

	var pending int64
	if result := s.db.WithContext(ctx).Model(&models.Viewer{}).
		Where("grant_id IN ? AND state IN ?", grantIDs, openAPIPendingViewerStates).
		Count(&pending); result.Error != nil {
		return OpenAPIRevocationProgress{}, ErrOpenAPIRevocationUnavailable
	}
	var closed int64
	if result := s.db.WithContext(ctx).Model(&models.Viewer{}).
		Where("grant_id IN ? AND state = ?", grantIDs, models.ViewerStateClosed).
		Count(&closed); result.Error != nil {
		return OpenAPIRevocationProgress{}, ErrOpenAPIRevocationUnavailable
	}

	status := OpenAPIRevocationStatusUnknown
	switch {
	case pending > 0:
		status = OpenAPIRevocationStatusPending
	case closed == int64(len(grantIDs)):
		status = OpenAPIRevocationStatusClosed
	}
	return OpenAPIRevocationProgress{Pending: pending, Closed: closed, Status: status}, nil
}

var openAPINonTerminalGrantStates = []models.GrantState{
	models.GrantStatePending,
	models.GrantStateIssued,
	models.GrantStateBound,
}

var openAPIPendingViewerStates = []models.ViewerState{
	models.ViewerStatePending,
	models.ViewerStateActive,
	models.ViewerStateRevokePending,
}

var openAPIKnownRevocationScopes = map[string]struct{}{
	"device:list":    {},
	"device:detail":  {},
	"device:status":  {},
	"channel:list":   {},
	"channel:detail": {},
	"channel:status": {},
	openAPIPlayScope: {},
}

func validateOpenAPIRevocationIntent(ctx context.Context, tx *gorm.DB, intent openapiclient.RevocationIntent) error {
	if ctx == nil || tx == nil || intent.ClientID <= 0 || intent.ClientEpoch <= 0 || intent.CreatedAt.IsZero() || intent.Scope != strings.TrimSpace(intent.Scope) {
		return ErrOpenAPIRevocationInvalid
	}
	switch intent.Reason {
	case "client.disabled", "client.revoked":
		if intent.Scope != "" || intent.ScopeEpoch != 0 {
			return ErrOpenAPIRevocationInvalid
		}
	case "scope.revoke":
		if intent.Scope == "" || intent.ScopeEpoch <= 0 {
			return ErrOpenAPIRevocationInvalid
		}
		if _, ok := openAPIKnownRevocationScopes[intent.Scope]; !ok {
			return ErrOpenAPIRevocationInvalid
		}
	default:
		return ErrOpenAPIRevocationInvalid
	}
	return nil
}

func validateCurrentClientForIntent(row models.Client, intent openapiclient.RevocationIntent) error {
	if row.ID != intent.ClientID || row.AuthEpoch <= 0 {
		return ErrOpenAPIRevocationStale
	}
	switch intent.Reason {
	case "client.disabled":
		if row.Status != models.StatusDisabled || row.AuthEpoch != intent.ClientEpoch {
			return ErrOpenAPIRevocationStale
		}
	case "client.revoked":
		if row.Status != models.StatusRevoked || row.AuthEpoch != intent.ClientEpoch {
			return ErrOpenAPIRevocationStale
		}
	case "scope.revoke":
		// A later client-level revoke may have advanced the epoch while a
		// retried scope intent is being observed. The scope epoch itself is
		// exact; the client epoch is therefore a lower-bound guard.
		if row.AuthEpoch < intent.ClientEpoch {
			return ErrOpenAPIRevocationStale
		}
	}
	return nil
}

func selectOpenAPIRevocationGrantIDs(tx *gorm.DB, intent openapiclient.RevocationIntent, row models.Client) ([]string, error) {
	query := lockedOpenAPIModel(tx, &models.PlayGrant{}, (models.PlayGrant{}).TableName()).
		Select("grant_id").Where("client_id = ? AND state IN ?", row.ID, openAPINonTerminalGrantStates).
		Order("grant_id ASC")
	if intent.Scope == "" {
		query = query.Where("client_epoch < ?", intent.ClientEpoch)
	} else {
		query = query.Where("scope = ? AND scope_epoch < ? AND client_epoch <= ?", intent.Scope, intent.ScopeEpoch, row.AuthEpoch)
	}
	var grantIDs []string
	if result := query.Find(&grantIDs); result.Error != nil {
		return nil, result.Error
	}
	return grantIDs, nil
}

func matchesOpenAPIRevocationIntent(grant models.PlayGrant, intent openapiclient.RevocationIntent, row models.Client) bool {
	if grant.ClientID != row.ID {
		return false
	}
	if intent.Scope == "" {
		return grant.ClientEpoch < intent.ClientEpoch
	}
	return grant.Scope == intent.Scope && grant.ScopeEpoch < intent.ScopeEpoch && grant.ClientEpoch <= row.AuthEpoch
}

func isOpenAPINonTerminalGrant(state models.GrantState) bool {
	switch state {
	case models.GrantStatePending, models.GrantStateIssued, models.GrantStateBound:
		return true
	default:
		return false
	}
}

func markOpenAPIViewerForRevocation(tx *gorm.DB, grantID string, now time.Time) error {
	viewer, exists, err := lockOpenAPIViewerByGrant(tx, grantID)
	if err != nil {
		return normalizeOpenAPIRevocationError(err)
	}
	if !exists {
		return nil
	}
	switch viewer.State {
	case models.ViewerStatePending, models.ViewerStateActive:
		updated := tx.Model(&models.Viewer{}).
			Where("id = ? AND grant_id = ? AND state IN ?", viewer.ID, grantID, []models.ViewerState{models.ViewerStatePending, models.ViewerStateActive}).
			Updates(map[string]any{
				"state":            models.ViewerStateRevokePending,
				"retry_at":         now,
				"attempts":         0,
				"last_error_class": openAPIRevocationRetryError,
				"updated_at":       now,
			})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return ErrOpenAPIRevocationUnavailable
		}
	case models.ViewerStateRevokePending, models.ViewerStateClosed:
		// Replaying an intent must not rewrite worker retry/tombstone fields.
	default:
		return ErrOpenAPIRevocationUnavailable
	}
	return nil
}

func normalizeOpenAPIRevocationContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func normalizeOpenAPIRevocationError(err error) error {
	if err == nil {
		return ErrOpenAPIRevocationUnavailable
	}
	switch {
	case errors.Is(err, ErrOpenAPIRevocationInvalid):
		return ErrOpenAPIRevocationInvalid
	case errors.Is(err, ErrOpenAPIRevocationStale):
		return ErrOpenAPIRevocationStale
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ErrOpenAPIRevocationUnavailable
	default:
		return ErrOpenAPIRevocationUnavailable
	}
}
