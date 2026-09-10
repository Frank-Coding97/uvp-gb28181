package workrecording

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// WorkStartCheck is installed during recorder bootstrap. It must verify that
// no unfinished legacy GbRecordingSession belongs to the requested channel
// and media generation. A nil check is accepted for the unit-test fixture; a
// production recorder must install this check before publishing writers.
type WorkStartCheck func(context.Context, uint, MediaTarget) error

// MP4MutationGate is the process-local serialization point for every MP4
// start/stop and source-close operation. NewRecorder gives each recorder a
// gate backed by the same process mutex, while retaining its own Claims DB
// handle for claim checks.
//
// Callbacks passed to LegacyMutation and GuardSourceClose receive a private
// context token. They must run synchronously and pass that context through any
// nested legacy callback. The token is invalidated when the callback returns;
// it must not be retained or used from another goroutine.
type MP4MutationGate struct {
	mu             *sync.Mutex
	claims         *Claims
	workStartCheck atomic.Value // stores workStartCheckValue
	recoverErr     error        // guarded by mu
}

type workStartCheckValue struct{ fn WorkStartCheck }

// One process-local mutex is enough for the single active UVP control process.
// Claims remain recorder-specific because tests and future nodes can use
// separate databases while still sharing the external MP4 mutation fence.
var processMP4MutationMu sync.Mutex

type mutationGateContextKey struct{}

type mutationGateToken struct {
	gate   *MP4MutationGate
	active atomic.Bool
}

// NewMP4MutationGate creates a gate backed by the active process mutation
// mutex and the supplied durable claims store.
func NewMP4MutationGate(claims *Claims) *MP4MutationGate {
	g := &MP4MutationGate{mu: &processMP4MutationMu, claims: claims}
	g.workStartCheck.Store(workStartCheckValue{})
	return g
}

// SetWorkStartCheck installs the durable unfinished-session preflight used by
// work Start. Bootstrap must call it before publishing any recorder writer.
// Tests may leave it unset because their minimal claims fixture has no session
// table.
func (g *MP4MutationGate) SetWorkStartCheck(check WorkStartCheck) {
	if g == nil {
		return
	}
	g.workStartCheck.Store(workStartCheckValue{fn: check})
}

func (g *MP4MutationGate) configuredWorkStartCheck() WorkStartCheck {
	if g == nil {
		return nil
	}
	value := g.workStartCheck.Load()
	if value == nil {
		return nil
	}
	return value.(workStartCheckValue).fn
}

func (g *MP4MutationGate) run(ctx context.Context, check func() error, fn func(context.Context) error) error {
	if g == nil || g.mu == nil || g.claims == nil || ctx == nil || fn == nil {
		return ErrInvalidRequest
	}
	if token, ok := ctx.Value(mutationGateContextKey{}).(*mutationGateToken); ok && token != nil && token.gate == g && token.active.Load() {
		if g.recoverErr != nil {
			return g.recoverErr
		}
		if err := check(); err != nil {
			return err
		}
		return fn(ctx)
	}

	g.mu.Lock()
	token := &mutationGateToken{gate: g}
	token.active.Store(true)
	defer func() {
		token.active.Store(false)
		g.mu.Unlock()
	}()
	if g.recoverErr != nil {
		return g.recoverErr
	}
	gctx := context.WithValue(ctx, mutationGateContextKey{}, token)
	if err := check(); err != nil {
		return err
	}
	return fn(gctx)
}

func (g *MP4MutationGate) lockMutation(ctx context.Context) (func(), error) {
	if g == nil || g.mu == nil || g.claims == nil || ctx == nil {
		return nil, ErrInvalidRequest
	}
	if token, ok := ctx.Value(mutationGateContextKey{}).(*mutationGateToken); ok && token != nil && token.gate == g && token.active.Load() {
		if g.recoverErr != nil {
			return nil, g.recoverErr
		}
		return func() {}, nil
	}
	g.mu.Lock()
	if g.recoverErr != nil {
		err := g.recoverErr
		g.mu.Unlock()
		return nil, err
	}
	return g.mu.Unlock, nil
}

// RecoverLegacy serializes startup or hot-reload quarantine with all MP4
// mutations. Callers must complete recovery successfully before publishing
// any writer or legacy adapter.
func (g *MP4MutationGate) RecoverLegacy(ctx context.Context) error {
	if g == nil || g.mu == nil || g.claims == nil || ctx == nil {
		return ErrInvalidRequest
	}
	if token, ok := ctx.Value(mutationGateContextKey{}).(*mutationGateToken); ok && token != nil && token.gate == g && token.active.Load() {
		err := g.claims.RecoverLegacy(ctx)
		g.recoverErr = err
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	err := g.claims.RecoverLegacy(ctx)
	g.recoverErr = err
	return err
}

// LegacyMutation serializes an old MP4 mutator with work recording. The
// callback is not invoked if durable claims show any non-idle non-legacy owner
// or if claim lookup fails.
func (g *MP4MutationGate) LegacyMutation(ctx context.Context, channelID uint, target MediaTarget, fn func(context.Context) error) error {
	return g.run(ctx, func() error {
		rows, err := g.matchingClaims(ctx, channelID, target, false)
		if err != nil {
			return err
		}
		if hasForeignActiveClaim(rows) {
			return ErrOwnerConflict
		}
		return nil
	}, fn)
}

// GuardSourceClose protects a source close that may not know the app or vhost.
// A non-zero channel ID covers an unbound Reserve claim as well as bound media
// claims; node/stream matching is intentionally app-independent.
func (g *MP4MutationGate) GuardSourceClose(ctx context.Context, channelID uint, nodeID int64, stream string, fn func(context.Context) error) error {
	return g.run(ctx, func() error {
		if nodeID < 0 || channelID == 0 && stream == "" {
			return ErrInvalidRequest
		}
		rows, err := g.matchingClaims(ctx, channelID, MediaTarget{NodeID: nodeID, Stream: stream}, true)
		if err != nil {
			return err
		}
		if hasForeignActiveClaim(rows) {
			return ErrOwnerConflict
		}
		return nil
	}, fn)
}

// checkWorkStart is called while the recorder already owns g.mu. The current
// work channel/media pair is allowed; every other active claim, including
// legacy_unknown quarantine, blocks adoption. The optional durable session
// check runs while the same gate is held and any error retains the claim.
func (g *MP4MutationGate) checkWorkStart(ctx context.Context, h RecorderHandle, target MediaTarget) error {
	if g == nil || g.claims == nil || h.ChannelID == 0 || h.Owner.Kind != OwnerWork || !h.Owner.valid() || !target.mediaIdentityValid() {
		return ErrInvalidRequest
	}
	if g.recoverErr != nil {
		return g.recoverErr
	}
	rows, err := g.matchingClaims(ctx, h.ChannelID, target, false)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.State == StateIdle {
			continue
		}
		if row.OwnerKind == h.Owner.Kind && row.OwnerID == h.Owner.ID && row.ChannelID == h.ChannelID && row.NodeID == target.NodeID && row.VHost == target.VHost && row.App == target.App && row.Stream == target.Stream {
			continue
		}
		return ErrOwnerConflict
	}
	if check := g.configuredWorkStartCheck(); check != nil {
		return check(ctx, h.ChannelID, target)
	}
	return nil
}

func (g *MP4MutationGate) matchingClaims(ctx context.Context, channelID uint, target MediaTarget, sourceMatch bool) ([]models.GbRecorderClaim, error) {
	if g == nil || g.claims == nil || g.claims.db == nil || ctx == nil {
		return nil, ErrInvalidRequest
	}
	if channelID == 0 && !(target.mediaIdentityValid() || sourceMatch && target.sourceIdentityValid()) {
		return nil, ErrInvalidRequest
	}
	db := g.claims.db.WithContext(ctx)
	rows := make([]models.GbRecorderClaim, 0, 4)
	seen := make(map[string]struct{})
	appendRows := func(found []models.GbRecorderClaim) {
		for _, row := range found {
			if _, exists := seen[row.ResourceKey]; exists {
				continue
			}
			seen[row.ResourceKey] = struct{}{}
			rows = append(rows, row)
		}
	}

	if channelID != 0 {
		var found []models.GbRecorderClaim
		if err := db.Where("channel_id = ? OR resource_key = ?", channelID, ChannelResource(channelID)).Find(&found).Error; err != nil {
			return nil, err
		}
		appendRows(found)
	}
	if target.mediaIdentityValid() || sourceMatch && target.sourceIdentityValid() {
		var found []models.GbRecorderClaim
		query := db.Where("stream = ?", target.Stream)
		if !sourceMatch {
			query = query.Where("node_id = ?", target.NodeID)
			query = query.Where("v_host = ? AND app = ?", target.VHost, target.App)
		} else if target.NodeID > 0 {
			query = query.Where("node_id = ?", target.NodeID)
		}
		if err := query.Find(&found).Error; err != nil {
			return nil, err
		}
		appendRows(found)
	}
	return rows, nil
}

func hasForeignActiveClaim(rows []models.GbRecorderClaim) bool {
	for _, row := range rows {
		if row.State != StateIdle && row.OwnerKind != OwnerLegacy {
			return true
		}
	}
	return false
}

func (m MediaTarget) mediaIdentityValid() bool {
	return m.NodeID > 0 && m.VHost != "" && len(m.VHost) <= 128 && m.App != "" && len(m.App) <= 64 && m.Stream != "" && len(m.Stream) <= 64 && !hasNUL(m.VHost) && !hasNUL(m.App) && !hasNUL(m.Stream)
}

func (m MediaTarget) sourceIdentityValid() bool {
	return m.NodeID >= 0 && m.Stream != "" && len(m.Stream) <= 64 && !hasNUL(m.Stream)
}

func hasNUL(value string) bool {
	return strings.IndexByte(value, 0) >= 0
}
