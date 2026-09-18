package media

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

const (
	defaultRevocationScanLimit = 100
	defaultRevocationLease     = 6 * time.Second
	minimumRevocationRetry     = time.Second
	maximumHookBudget          = 5 * time.Second
	playLiveApplyScope         = "play:live:apply"
	revocationNetworkTimeout   = 5 * time.Second

	RevocationErrorPending             = "revocation_pending"
	RevocationErrorAwaitingLateSession = "awaiting_late_session"
	RevocationErrorAlreadyGone         = "already_gone"
	RevocationErrorKicked              = "kicked"
	RevocationErrorShutdownScheduled   = "shutdown_scheduled"
	RevocationErrorNetworkUnavailable  = "network_unavailable"
	RevocationErrorNetworkCanceled     = "network_canceled"
	RevocationErrorRuntimeUnavailable  = "runtime_unavailable"
	RevocationErrorRuntimeMismatch     = "runtime_mismatch"
	RevocationErrorHookBudgetInvalid   = "hook_budget_invalid"
	RevocationErrorHookBudgetExceeded  = "hook_budget_exceeds_max"
	RevocationErrorSnapshotIncomplete  = "snapshot_incomplete"
	RevocationErrorPartialSnapshot     = "partial_runtime_snapshot"
	RevocationErrorKickNotFound        = "kick_not_found"
	RevocationErrorGrantNotRevoked     = "grant_not_revoked"
	RevocationErrorBindingMismatch     = "binding_mismatch"
	RevocationErrorGrantTombstone      = "grant_tombstone_invalid"
)

var ErrRevocationWorkerUnavailable = errors.New("openapi revocation worker unavailable")

// RevocationMediaControl is deliberately narrower than the ordinary ZLM
// client. It has no ID-only kick, Stop, or configuration-mutating entrypoint.
type RevocationMediaControl interface {
	GetRuntimeMediaPlayers(context.Context, zlm.StreamTarget) (zlm.RuntimePlayers, error)
	GetRuntimeSessions(context.Context) (zlm.RuntimeSessions, error)
	KickSessionIfMatch(context.Context, string, string) (zlm.ConditionalKickResult, error)
}

// RevocationRuntime is the trusted, fixed endpoint returned by the resolver.
// The worker never derives trust or a current boot from an active node row.
// Trusted is set only after endpoint, TLS qualification, and the durable node
// identity have been checked by the caller-owned resolver.
type RevocationRuntime struct {
	Control          RevocationMediaControl
	CurrentBootNonce string
	HookBudget       time.Duration
	Trusted          bool
	Release          func() // caller releases the resolved control on every path
}

// RevocationRuntimeFactory resolves one already-qualified runtime endpoint.
// A missing resolver, an unknown node, or an unqualified endpoint is a
// pending result, not permission to use a different client.
type RevocationRuntimeFactory interface {
	Resolve(context.Context, string) (RevocationRuntime, error)
}

// RevocationRuntimeResolver is kept as a descriptive alias for callers that
// prefer to name the dependency by its read-only role.
type RevocationRuntimeResolver = RevocationRuntimeFactory

// RevocationWorker is a manually ticked durable worker. Construction has no
// side effects and never starts a background goroutine; root wiring decides
// when, or whether, Tick is invoked.
type RevocationWorker struct {
	db      *gorm.DB
	factory RevocationRuntimeFactory
	now     func() time.Time
	limit   int
	lease   time.Duration
}

type RevocationTickResult struct {
	Scanned int
	Claimed int
	Closed  int
	Pending int
	Stale   int
	Alarms  int
}

type revocationBinding struct {
	NodeUUID        string
	BootNonce       string
	Schema          string
	VHost           string
	App             string
	Stream          string
	MediaGeneration uint64
}

type revocationViewerToken struct {
	ID              int64
	GrantID         string
	NodeUUID        string
	BootNonce       string
	Identifier      string
	Schema          string
	VHost           string
	App             string
	Stream          string
	MediaGeneration uint64
	State           models.ViewerState
	Attempts        int
	RetryAt         *time.Time
	UpdatedAt       time.Time
}

type revocationClaim struct {
	token           revocationViewerToken
	binding         revocationBinding
	grantUpdatedAt  time.Time
	grantReason     string
	priorErrorClass string
}

type revocationOutcome struct {
	closed bool
	stale  bool
	alarm  bool
}

func NewRevocationWorker(db *gorm.DB, factory RevocationRuntimeFactory, now func() time.Time, limit int) *RevocationWorker {
	if now == nil {
		now = time.Now
	}
	if limit <= 0 {
		limit = defaultRevocationScanLimit
	}
	return &RevocationWorker{db: db, factory: factory, now: now, limit: limit, lease: defaultRevocationLease}
}

// Tick claims at most the configured number of due rows, performs all network
// calls after the claim transaction has committed, and CASes the result back.
func (w *RevocationWorker) Tick(ctx context.Context) (RevocationTickResult, error) {
	var result RevocationTickResult
	if ctx == nil {
		ctx = context.Background()
	}
	if w == nil || w.db == nil {
		return result, ErrRevocationWorkerUnavailable
	}
	now := w.currentTime()
	candidates, err := w.dueCandidates(ctx, now)
	if err != nil {
		return result, err
	}
	result.Scanned = len(candidates)
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		claim, changed, err := w.claim(ctx, candidate, w.currentTime())
		if err != nil {
			return result, err
		}
		if claim == nil {
			if changed {
				result.Pending++
			}
			continue
		}
		result.Claimed++
		outcome, err := w.processClaim(ctx, claim)
		if err != nil {
			return result, err
		}
		if outcome.alarm {
			result.Alarms++
		}
		if outcome.stale {
			result.Stale++
		} else if outcome.closed {
			result.Closed++
		} else {
			result.Pending++
		}
	}
	return result, nil
}

type revocationCandidate struct {
	ID      int64  `gorm:"column:id"`
	GrantID string `gorm:"column:grant_id"`
}

func (w *RevocationWorker) dueCandidates(ctx context.Context, now time.Time) ([]revocationCandidate, error) {
	now = normalizeRevocationTime(now)
	var candidates []revocationCandidate
	err := w.db.WithContext(ctx).Model(&models.Viewer{}).
		Select("id, grant_id").
		Where("state = ? AND (retry_at IS NULL OR retry_at <= ?)", models.ViewerStateRevokePending, now).
		Order("CASE WHEN retry_at IS NULL THEN 0 ELSE 1 END ASC").
		Order("retry_at ASC").
		Order("id ASC").Limit(w.limit).Find(&candidates).Error
	return candidates, err
}

// claim locks the grant first and the viewer second. The transaction contains
// no network call and commits a lease token before the resolver is consulted.
// The bool says that a conservative pending marker was written without a
// network claim (for example, a tuple mismatch).
func (w *RevocationWorker) claim(ctx context.Context, candidate revocationCandidate, now time.Time) (*revocationClaim, bool, error) {
	var claim *revocationClaim
	var changed bool
	err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		grant, found, err := lockRevocationGrant(tx, candidate.GrantID)
		if err != nil || !found {
			return err
		}
		viewer, found, err := lockRevocationViewer(tx, candidate.ID)
		if err != nil || !found {
			return err
		}
		if viewer.GrantID != grant.GrantID || viewer.GrantID != candidate.GrantID || viewer.State != models.ViewerStateRevokePending || (viewer.RetryAt != nil && viewer.RetryAt.After(now)) {
			return nil
		}
		if grant.State != models.GrantStateRevoked {
			changed, err = markRevocationPending(tx, viewer, now, RevocationErrorGrantNotRevoked)
			return err
		}
		grantBinding, grantOK := bindingFromGrant(grant)
		viewerBinding, viewerOK := bindingFromViewer(viewer)
		if !grantOK || !viewerOK || !grantBinding.equal(viewerBinding) {
			changed, err = markRevocationPending(tx, viewer, now, RevocationErrorBindingMismatch)
			return err
		}
		if grant.UpdatedAt.IsZero() {
			changed, err = markRevocationPending(tx, viewer, now, RevocationErrorGrantTombstone)
			return err
		}
		newAttempts := viewer.Attempts + 1
		claimAt := normalizeRevocationTime(now)
		leaseAt := normalizeRevocationTime(claimAt.Add(w.lease))
		oldToken := viewerToken(viewer)
		updates := map[string]any{"attempts": newAttempts, "retry_at": leaseAt, "updated_at": claimAt}
		write := withViewerToken(tx.Model(&models.Viewer{}), oldToken).Updates(updates)
		if write.Error != nil {
			return write.Error
		}
		if write.RowsAffected != 1 {
			return nil
		}
		claimToken := oldToken
		claimToken.Attempts = newAttempts
		claimToken.RetryAt = timePtr(leaseAt)
		claimToken.UpdatedAt = claimAt
		claim = &revocationClaim{token: claimToken, binding: grantBinding, grantUpdatedAt: normalizeRevocationTime(grant.UpdatedAt), grantReason: grant.Reason, priorErrorClass: viewer.LastErrorClass}
		return nil
	})
	return claim, changed, err
}

func (w *RevocationWorker) processClaim(ctx context.Context, claim *revocationClaim) (revocationOutcome, error) {
	if w.factory == nil {
		return w.pending(ctx, claim, RevocationErrorRuntimeUnavailable, nil, false)
	}
	// Resolution and control share one monotonic budget, not two consecutive
	// five-second windows. The lease is overlap suppression, not an SLA proof.
	// Persistence uses the caller context so an expired network budget can
	// still record pending; cancellation cannot undo an already sent kick.
	totalCtx, cancelTotal := context.WithTimeout(ctx, revocationNetworkTimeout)
	defer cancelTotal()
	if err := totalCtx.Err(); err != nil {
		return w.pending(ctx, claim, classifyContextError(err), nil, false)
	}
	runtime, err := w.factory.Resolve(totalCtx, claim.binding.NodeUUID)
	if runtime.Release != nil {
		defer runtime.Release()
	}
	if deadlineErr := totalCtx.Err(); deadlineErr != nil {
		err = deadlineErr
	}
	if err != nil {
		return w.pending(ctx, claim, classifyContextError(err), nil, false)
	}
	if !runtime.Trusted || runtime.Control == nil || strings.TrimSpace(runtime.CurrentBootNonce) == "" {
		return w.pending(ctx, claim, RevocationErrorRuntimeUnavailable, nil, false)
	}
	if runtime.CurrentBootNonce != claim.binding.BootNonce {
		return w.pending(ctx, claim, RevocationErrorRuntimeMismatch, nil, false)
	}
	if runtime.HookBudget <= 0 {
		return w.pending(ctx, claim, RevocationErrorHookBudgetInvalid, nil, false)
	}
	if runtime.HookBudget > maximumHookBudget {
		return w.pending(ctx, claim, RevocationErrorHookBudgetExceeded, nil, true)
	}

	networkCtx, cancel := context.WithTimeout(totalCtx, runtime.HookBudget)
	defer cancel()
	target := zlm.StreamTarget{Schema: claim.binding.Schema, VHost: claim.binding.VHost, App: claim.binding.App, Stream: claim.binding.Stream}
	if err := networkCtx.Err(); err != nil {
		return w.pending(ctx, claim, classifyContextError(err), nil, false)
	}
	players, err := runtime.Control.GetRuntimeMediaPlayers(networkCtx, target)
	if deadlineErr := networkCtx.Err(); deadlineErr != nil {
		err = deadlineErr
	}
	if err != nil {
		return w.pending(ctx, claim, classifyContextError(err), nil, false)
	}
	sessions, err := runtime.Control.GetRuntimeSessions(networkCtx)
	if deadlineErr := networkCtx.Err(); deadlineErr != nil {
		err = deadlineErr
	}
	if err != nil {
		return w.pending(ctx, claim, classifyContextError(err), nil, false)
	}
	if players.Players == nil || sessions.Sessions == nil {
		return w.pending(ctx, claim, RevocationErrorSnapshotIncomplete, nil, false)
	}
	if players.BootNonce != claim.binding.BootNonce || sessions.BootNonce != claim.binding.BootNonce {
		return w.pending(ctx, claim, RevocationErrorRuntimeMismatch, nil, false)
	}
	playerPresent := runtimePlayerPresent(players.Players, claim.bindingIdentifier())
	sessionPresent := runtimeSessionPresent(sessions.Sessions, claim.bindingIdentifier())
	if playerPresent != sessionPresent {
		return w.pending(ctx, claim, RevocationErrorPartialSnapshot, nil, false)
	}
	if err := networkCtx.Err(); err != nil {
		return w.pending(ctx, claim, classifyContextError(err), nil, false)
	}
	if !playerPresent {
		return w.processAbsent(ctx, claim, runtime.HookBudget)
	}

	kick, err := runtime.Control.KickSessionIfMatch(networkCtx, claim.binding.BootNonce, claim.bindingIdentifier())
	if deadlineErr := networkCtx.Err(); deadlineErr != nil {
		err = deadlineErr
	}
	if err != nil {
		return w.pending(ctx, claim, classifyContextError(err), nil, false)
	}
	switch kick {
	case zlm.KickShutdownScheduled:
		return w.pending(ctx, claim, RevocationErrorShutdownScheduled, nil, false)
	case zlm.KickNotFound:
		// A race between the two fresh snapshots and the conditional kick is
		// not proof of disconnection; it resets the absence phase.
		return w.pending(ctx, claim, RevocationErrorKickNotFound, nil, false)
	case zlm.KickRuntimeMismatch:
		return w.pending(ctx, claim, RevocationErrorRuntimeMismatch, nil, false)
	default:
		return w.pending(ctx, claim, RevocationErrorRuntimeUnavailable, nil, false)
	}
}

func (w *RevocationWorker) processAbsent(ctx context.Context, claim *revocationClaim, hookBudget time.Duration) (revocationOutcome, error) {
	now := w.currentTime()
	windowEnd := normalizeRevocationTime(claim.grantUpdatedAt.Add(hookBudget))
	if claim.priorErrorClass == RevocationErrorShutdownScheduled {
		return w.close(ctx, claim, RevocationErrorKicked)
	}
	if claim.priorErrorClass == RevocationErrorAwaitingLateSession && now.After(windowEnd) {
		return w.close(ctx, claim, RevocationErrorAlreadyGone)
	}
	retryAt := normalizeRevocationTime(now.Add(minimumRevocationRetry))
	if !retryAt.After(windowEnd) {
		retryAt = normalizeRevocationTime(windowEnd.Add(time.Microsecond))
	}
	return w.pending(ctx, claim, RevocationErrorAwaitingLateSession, &retryAt, false)
}

func (w *RevocationWorker) pending(ctx context.Context, claim *revocationClaim, class string, retryAt *time.Time, alarm bool) (revocationOutcome, error) {
	class = sanitizeRevocationClass(class)
	now := w.currentTime()
	elapsedAlarm := now.Sub(claim.grantUpdatedAt) > maximumHookBudget
	if retryAt == nil {
		next := normalizeRevocationTime(now.Add(minimumRevocationRetry))
		retryAt = &next
	}
	applied, err := w.finish(ctx, claim, models.ViewerStateRevokePending, class, retryAt)
	if err != nil {
		return revocationOutcome{}, err
	}
	return revocationOutcome{stale: !applied, alarm: applied && (alarm || elapsedAlarm)}, nil
}

func (w *RevocationWorker) close(ctx context.Context, claim *revocationClaim, class string) (revocationOutcome, error) {
	applied, err := w.finish(ctx, claim, models.ViewerStateClosed, sanitizeRevocationClass(class), nil)
	if err != nil {
		return revocationOutcome{}, err
	}
	return revocationOutcome{closed: applied, stale: !applied}, nil
}

func (w *RevocationWorker) finish(ctx context.Context, claim *revocationClaim, state models.ViewerState, class string, retryAt *time.Time) (bool, error) {
	var applied bool
	err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		grant, found, err := lockRevocationGrant(tx, claim.token.GrantID)
		if err != nil || !found {
			return err
		}
		viewer, found, err := lockRevocationViewer(tx, claim.token.ID)
		if err != nil || !found {
			return err
		}
		grantBinding, grantOK := bindingFromGrant(grant)
		viewerBinding, viewerOK := bindingFromViewer(viewer)
		if grant.State != models.GrantStateRevoked || !normalizeRevocationTime(grant.UpdatedAt).Equal(claim.grantUpdatedAt) || grant.Reason != claim.grantReason || !grantOK || !viewerOK || !grantBinding.equal(claim.binding) || !viewerBinding.equal(claim.binding) {
			return nil
		}
		updates := map[string]any{"state": state, "last_error_class": class, "updated_at": w.currentTime()}
		if retryAt == nil {
			updates["retry_at"] = nil
		} else {
			updates["retry_at"] = normalizeRevocationTime(*retryAt)
		}
		write := withViewerToken(tx.Model(&models.Viewer{}), claim.token).Updates(updates)
		if write.Error != nil {
			return write.Error
		}
		applied = write.RowsAffected == 1
		return nil
	})
	return applied, err
}

func lockRevocationGrant(tx *gorm.DB, grantID string) (models.PlayGrant, bool, error) {
	var grant models.PlayGrant
	query := lockedRevocationModel(tx, &models.PlayGrant{}, (models.PlayGrant{}).TableName()).Where("grant_id = ?", grantID).Limit(1).Take(&grant)
	if errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return models.PlayGrant{}, false, nil
	}
	if query.Error != nil {
		return models.PlayGrant{}, false, query.Error
	}
	return grant, true, nil
}

func lockRevocationViewer(tx *gorm.DB, id int64) (models.Viewer, bool, error) {
	var viewer models.Viewer
	query := lockedRevocationModel(tx, &models.Viewer{}, (models.Viewer{}).TableName()).Where("id = ?", id).Limit(1).Take(&viewer)
	if errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return models.Viewer{}, false, nil
	}
	if query.Error != nil {
		return models.Viewer{}, false, query.Error
	}
	return viewer, true, nil
}

func lockedRevocationModel(tx *gorm.DB, model any, table string) *gorm.DB {
	if tx.Dialector.Name() == "sqlserver" {
		return tx.Table(table + " WITH (UPDLOCK, HOLDLOCK)")
	}
	return tx.Model(model).Clauses(clause.Locking{Strength: "UPDATE"})
}

func markRevocationPending(tx *gorm.DB, viewer models.Viewer, now time.Time, class string) (bool, error) {
	retryAt := normalizeRevocationTime(now.Add(minimumRevocationRetry))
	updates := map[string]any{"last_error_class": sanitizeRevocationClass(class), "retry_at": retryAt, "updated_at": normalizeRevocationTime(now)}
	write := withViewerToken(tx.Model(&models.Viewer{}), viewerToken(viewer)).Updates(updates)
	return write.RowsAffected == 1, write.Error
}

func withViewerToken(query *gorm.DB, token revocationViewerToken) *gorm.DB {
	query = query.Where("id = ? AND grant_id = ? AND state = ? AND attempts = ? AND node_uuid = ? AND boot_nonce = ? AND identifier = ? AND schema = ? AND vhost = ? AND app = ? AND stream = ? AND media_generation = ? AND updated_at = ?", token.ID, token.GrantID, token.State, token.Attempts, token.NodeUUID, token.BootNonce, token.Identifier, token.Schema, token.VHost, token.App, token.Stream, token.MediaGeneration, normalizeRevocationTime(token.UpdatedAt))
	if token.RetryAt == nil {
		return query.Where("retry_at IS NULL")
	}
	return query.Where("retry_at = ?", normalizeRevocationTime(*token.RetryAt))
}

func viewerToken(viewer models.Viewer) revocationViewerToken {
	return revocationViewerToken{
		ID: viewer.ID, GrantID: viewer.GrantID, NodeUUID: viewer.NodeUUID, BootNonce: viewer.BootNonce,
		Identifier: viewer.Identifier, Schema: viewer.Schema, VHost: viewer.VHost, App: viewer.App,
		Stream: viewer.Stream, MediaGeneration: viewer.MediaGeneration, State: viewer.State,
		Attempts: viewer.Attempts, RetryAt: copyTime(viewer.RetryAt), UpdatedAt: normalizeRevocationTime(viewer.UpdatedAt),
	}
}

func bindingFromGrant(grant models.PlayGrant) (revocationBinding, bool) {
	if grant.Scope != playLiveApplyScope || grant.Protocol == nil || (*grant.Protocol != "https-flv" && *grant.Protocol != "wss-flv") || grant.NodeUUID == nil || grant.BootNonce == nil || grant.Schema == nil || grant.VHost == nil || grant.App == nil || grant.Stream == nil || grant.MediaGeneration == nil {
		return revocationBinding{}, false
	}
	binding := revocationBinding{NodeUUID: *grant.NodeUUID, BootNonce: *grant.BootNonce, Schema: *grant.Schema, VHost: *grant.VHost, App: *grant.App, Stream: *grant.Stream, MediaGeneration: *grant.MediaGeneration}
	return binding, binding.valid()
}

func bindingFromViewer(viewer models.Viewer) (revocationBinding, bool) {
	binding := revocationBinding{NodeUUID: viewer.NodeUUID, BootNonce: viewer.BootNonce, Schema: viewer.Schema, VHost: viewer.VHost, App: viewer.App, Stream: viewer.Stream, MediaGeneration: viewer.MediaGeneration}
	return binding, binding.valid()
}

func (b revocationBinding) valid() bool {
	return strings.TrimSpace(b.NodeUUID) != "" && validRevocationBootNonce(b.BootNonce) && strings.TrimSpace(b.Schema) != "" && strings.TrimSpace(b.VHost) != "" && strings.TrimSpace(b.App) != "" && strings.TrimSpace(b.Stream) != "" && b.MediaGeneration > 0
}

func (b revocationBinding) equal(other revocationBinding) bool {
	return b.NodeUUID == other.NodeUUID && b.BootNonce == other.BootNonce && b.Schema == other.Schema && b.VHost == other.VHost && b.App == other.App && b.Stream == other.Stream && b.MediaGeneration == other.MediaGeneration
}

func (c *revocationClaim) bindingIdentifier() string {
	return c.token.Identifier
}

func validRevocationBootNonce(value string) bool {
	return len(value) == 32 && strings.Trim(value, "0123456789abcdef") == ""
}

func runtimePlayerPresent(players []zlm.MediaPlayer, identifier string) bool {
	for _, player := range players {
		if player.Identifier == identifier {
			return true
		}
	}
	return false
}

func runtimeSessionPresent(sessions []zlm.Session, identifier string) bool {
	for _, session := range sessions {
		if session.ID == identifier && session.Identifier == identifier {
			return true
		}
	}
	return false
}

func classifyContextError(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return RevocationErrorNetworkCanceled
	}
	return RevocationErrorNetworkUnavailable
}

func sanitizeRevocationClass(class string) string {
	switch class {
	case RevocationErrorPending, RevocationErrorAwaitingLateSession, RevocationErrorAlreadyGone, RevocationErrorKicked,
		RevocationErrorShutdownScheduled, RevocationErrorNetworkUnavailable, RevocationErrorNetworkCanceled,
		RevocationErrorRuntimeUnavailable, RevocationErrorRuntimeMismatch, RevocationErrorHookBudgetInvalid,
		RevocationErrorHookBudgetExceeded, RevocationErrorSnapshotIncomplete, RevocationErrorPartialSnapshot,
		RevocationErrorKickNotFound, RevocationErrorGrantNotRevoked, RevocationErrorBindingMismatch,
		RevocationErrorGrantTombstone:
		return class
	default:
		return RevocationErrorPending
	}
}

func (w *RevocationWorker) currentTime() time.Time {
	if w == nil || w.now == nil {
		return normalizeRevocationTime(time.Now())
	}
	now := w.now()
	if now.IsZero() {
		return normalizeRevocationTime(time.Now())
	}
	return normalizeRevocationTime(now)
}

func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := normalizeRevocationTime(*value)
	return &copy
}

func timePtr(value time.Time) *time.Time {
	value = normalizeRevocationTime(value)
	return &value
}

func normalizeRevocationTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}
