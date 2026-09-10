package workrecording

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestMutationGateIsSharedByRecorderAndBlocksWorkFromLegacyMutation(t *testing.T) {
	r, _, _ := recorderFixture(t)
	g := r.MutationGate()
	require.Same(t, g, r.MutationGate())

	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	called := false
	err = g.LegacyMutation(ctx, 1, testMedia(), func(context.Context) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, ErrOwnerConflict)
	require.False(t, called)
	require.Equal(t, h.Version, mustClaim(t, r.claims, ctx, ChannelResource(1)).Version)
}

func TestMutationGateAllowsLegacyUnknownForLegacyMutationButWorkCannotUseIt(t *testing.T) {
	r, _, _ := recorderFixture(t)
	ctx := context.Background()
	target := testMedia()
	insertLegacyUnknownClaims(t, r.claims.db, 1, target)
	g := r.MutationGate()

	called := false
	err := g.LegacyMutation(ctx, 1, target, func(gctx context.Context) error {
		called = true
		return g.LegacyMutation(gctx, 1, target, func(context.Context) error { return nil })
	})
	require.NoError(t, err)
	require.True(t, called)

	h, err := r.Reserve(ctx, 2, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	target.RecordingRoot = "/tmp/work-recordings/job"
	_, err = r.Start(ctx, h, target, 0)
	require.ErrorIs(t, err, ErrOwnerConflict)
	claim := mustClaim(t, r.claims, ctx, ChannelResource(2))
	require.Equal(t, StateStarting, claim.State)
}

func TestMutationGateChecksUnboundChannelAndSourceStream(t *testing.T) {
	r, _, _ := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	g := r.MutationGate()

	called := false
	err = g.GuardSourceClose(ctx, 1, 1, "s", func(context.Context) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, ErrOwnerConflict)
	require.False(t, called)

	// A source close may not bypass a media claim merely because its app is
	// unavailable to the close caller.
	require.NoError(t, r.claims.db.Create(&models.GbRecorderClaim{
		ResourceKey: MediaResource(1, "other-vhost", "other-app", "s"),
		OwnerKind:   OwnerWork,
		OwnerID:     "other-job",
		State:       StateRecording,
		Version:     1,
		NodeID:      1,
		VHost:       "other-vhost",
		App:         "other-app",
		Stream:      "s",
	}).Error)

	called = false
	err = g.GuardSourceClose(ctx, 0, 1, "s", func(context.Context) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, ErrOwnerConflict)
	require.False(t, called)
	require.NoError(t, r.claims.db.Create(&models.GbRecorderClaim{
		ResourceKey: MediaResource(2, "v2", "app2", "s"),
		OwnerKind:   OwnerWork,
		OwnerID:     "node-two-job",
		State:       StateRecording,
		Version:     1,
		NodeID:      2,
		VHost:       "v2",
		App:         "app2",
		Stream:      "s",
	}).Error)
	called = false
	err = g.GuardSourceClose(ctx, 0, 0, "s", func(context.Context) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, ErrOwnerConflict)
	require.False(t, called)
	require.Equal(t, h.Version, mustClaim(t, r.claims, ctx, ChannelResource(1)).Version)
}

func TestMutationGateDoesNotCallCallbackOnDatabaseFailure(t *testing.T) {
	r, _, _ := recorderFixture(t)
	ctx := context.Background()
	closed := false
	// Closing the underlying test database makes the claim read fail without
	// requiring a write-side test hook in production code.
	if closer, ok := r.claims.db.ConnPool.(interface{ Close() error }); ok {
		require.NoError(t, closer.Close())
		closed = true
	}
	if !closed {
		t.Skip("test database does not expose a closable connection pool")
	}
	called := false
	err := r.MutationGate().LegacyMutation(ctx, 1, testMedia(), func(context.Context) error {
		called = true
		return nil
	})
	require.Error(t, err)
	require.False(t, called)
}

func TestMutationGateRecoveryFailureRemainsFailClosedUntilRetrySucceeds(t *testing.T) {
	r, _, _ := recorderFixture(t)
	ctx := context.Background()
	g := r.MutationGate()
	err := g.RecoverLegacy(ctx)
	require.Error(t, err)

	called := false
	blockedErr := g.LegacyMutation(ctx, 0, testMedia(), func(context.Context) error {
		called = true
		return nil
	})
	require.ErrorIs(t, blockedErr, err)
	require.False(t, called)

	require.NoError(t, r.claims.db.AutoMigrate(&models.GbChannel{}, &models.GbRecordingSession{}))
	require.NoError(t, g.RecoverLegacy(ctx))
	require.NoError(t, g.LegacyMutation(ctx, 0, testMedia(), func(context.Context) error {
		called = true
		return nil
	}))
	require.True(t, called)
}

func TestMutationGateNestedCallbackDoesNotDeadlockAndTokenCannotEscape(t *testing.T) {
	r, _, _ := recorderFixture(t)
	g := r.MutationGate()
	ctx := context.Background()
	var escaped context.Context
	done := make(chan error, 1)
	go func() {
		done <- g.LegacyMutation(ctx, 0, testMedia(), func(gctx context.Context) error {
			escaped = gctx
			return g.LegacyMutation(gctx, 0, testMedia(), func(context.Context) error { return nil })
		})
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("nested mutation callback deadlocked")
	}
	// A callback context held past the synchronous callback must not bypass a
	// future gate acquisition.
	require.NotNil(t, escaped)
	started := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		close(started)
		_ = g.LegacyMutation(escaped, 0, testMedia(), func(context.Context) error { return nil })
		close(finished)
	}()
	<-started
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("escaped callback context bypassed or retained gate incorrectly")
	}
}

func TestMutationGateSerializesDifferentRecorderInstances(t *testing.T) {
	r1, _, _ := recorderFixture(t)
	r2, _, _ := recorderFixture(t)
	shared := r1.MutationGate()
	r2.SetMutationGate(shared)
	require.Same(t, shared, r2.MutationGate())
	// A production hot reload reuses the same gate and therefore the same
	// process mutex and claim data source as well.
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		_ = r1.MutationGate().LegacyMutation(context.Background(), 0, testMedia(), func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	go func() {
		_ = r2.MutationGate().LegacyMutation(context.Background(), 0, testMedia(), func(context.Context) error {
			close(finished)
			return nil
		})
	}()
	select {
	case <-finished:
		t.Fatal("different recorder instances did not share the process gate")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("second recorder did not enter after gate release")
	}
}

func TestMutationGateWorkStartCheckRejectsActiveOrUnknownSession(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	preflightErr := errors.New("unfinished session")
	checked := 0
	target := testMedia()
	target.RecordingRoot = "/tmp/work-recordings/job"
	expectedTarget := target
	r.MutationGate().SetWorkStartCheck(func(_ context.Context, channelID uint, actualTarget MediaTarget) error {
		checked++
		require.Equal(t, uint(1), channelID)
		require.Equal(t, expectedTarget, actualTarget)
		return preflightErr
	})
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	_, err = r.Start(ctx, h, target, 0)
	require.ErrorIs(t, err, preflightErr)
	require.Equal(t, 1, checked)
	require.Zero(t, c.starts)
	require.Equal(t, StateStarting, mustClaim(t, r.claims, ctx, ChannelResource(1)).State)
}

func TestMutationGatePreventsLegacyCloseDuringWorkExternalStart(t *testing.T) {
	db := claimDB(t)
	client := &blockingDirectoryClient{started: make(chan struct{}), release: make(chan struct{})}
	r := NewRecorder(NewClaims(db), func(context.Context, MediaTarget) (func(), error) {
		return func() {}, nil
	}, func(MediaTarget) (MP4Client, error) { return client, nil })
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	target := testMedia()
	target.RecordingRoot = "/tmp/work-recordings/job"
	startDone := make(chan error, 1)
	go func() {
		_, startErr := r.Start(ctx, h, target, 0)
		startDone <- startErr
	}()
	<-client.started

	legacyCalled := false
	legacyDone := make(chan error, 1)
	go func() {
		legacyDone <- r.MutationGate().GuardSourceClose(ctx, 1, target.NodeID, target.Stream, func(context.Context) error {
			legacyCalled = true
			return nil
		})
	}()
	select {
	case <-legacyDone:
		t.Fatal("legacy close entered while work Start held mutation gate")
	case <-time.After(50 * time.Millisecond):
	}

	close(client.release)
	require.NoError(t, <-startDone)
	select {
	case err := <-legacyDone:
		require.ErrorIs(t, err, ErrOwnerConflict)
		require.False(t, legacyCalled)
	case <-time.After(time.Second):
		t.Fatal("legacy close did not finish after work Start released mutation gate")
	}
}

func TestRecorderAbortStartingReleasesUnboundClaim(t *testing.T) {
	r, _, _ := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	require.NoError(t, r.AbortStarting(ctx, h))
	claim := mustClaim(t, r.claims, ctx, ChannelResource(1))
	require.Equal(t, StateIdle, claim.State)
	require.Empty(t, claim.OwnerKind)
	require.Equal(t, h.Version+1, claim.Version)
	require.ErrorIs(t, r.AbortStarting(ctx, h), ErrOwnerConflict)
	next, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "next"})
	require.NoError(t, err)
	require.Equal(t, h.Version+2, next.Version)
}

func TestRecorderAbortStartingReleasesBoundChannelAndMediaAtomically(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	target := testMedia()
	target.RecordingRoot = "/tmp/work-recordings/job"
	preflightErr := errors.New("unfinished session")
	r.MutationGate().SetWorkStartCheck(func(context.Context, uint, MediaTarget) error { return preflightErr })
	h, err = r.Start(ctx, h, target, 0)
	require.ErrorIs(t, err, preflightErr)
	require.Zero(t, c.starts)
	require.NoError(t, r.AbortStarting(ctx, h))
	for _, key := range []string{ChannelResource(1), target.key()} {
		claim := mustClaim(t, r.claims, ctx, key)
		require.Equal(t, StateIdle, claim.State)
		require.Empty(t, claim.OwnerKind)
	}
	r.MutationGate().SetWorkStartCheck(nil)
	next, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "next"})
	require.NoError(t, err)
	nextTarget := target
	nextTarget.RecordingRoot = "/tmp/work-recordings/next"
	_, err = r.Start(ctx, next, nextTarget, 0)
	require.NoError(t, err)
}

func TestRecorderAbortStartingRollsBackBothClaimsOnDatabaseFailure(t *testing.T) {
	r, _, _ := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	target := testMedia()
	target.RecordingRoot = "/tmp/work-recordings/job"
	// bind is intentionally used here to keep the external StartRecord call
	// out of the fixture while exercising the bound pair cleanup.
	h, err = r.bind(ctx, h, target)
	require.NoError(t, err)
	require.NoError(t, r.claims.db.Exec("CREATE TRIGGER reject_abort BEFORE UPDATE ON gb_recorder_claim WHEN NEW.resource_key = '"+target.key()+"' AND NEW.state = 'idle' BEGIN SELECT RAISE(ABORT,'unavailable'); END").Error)
	require.Error(t, r.AbortStarting(ctx, h))
	for _, key := range []string{ChannelResource(1), target.key()} {
		claim := mustClaim(t, r.claims, ctx, key)
		require.Equal(t, StateStarting, claim.State)
		require.Equal(t, OwnerWork, claim.OwnerKind)
	}
}

func TestRecorderAbortStartingRejectsUnknownClaim(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	c.startErr = errors.New("timeout")
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	target := testMedia()
	target.RecordingRoot = "/tmp/work-recordings/job"
	h, err = r.Start(ctx, h, target, 0)
	require.Error(t, err)
	require.Equal(t, StateUnknown, mustClaim(t, r.claims, ctx, ChannelResource(1)).State)
	require.ErrorIs(t, r.AbortStarting(ctx, h), ErrVersionConflict)
	require.Equal(t, StateUnknown, mustClaim(t, r.claims, ctx, ChannelResource(1)).State)
	require.Equal(t, 1, c.starts)
}

type blockingDirectoryClient struct {
	mu      sync.Mutex
	active  bool
	started chan struct{}
	release chan struct{}
}

func (c *blockingDirectoryClient) IsRecording(context.Context, string, string, string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active, nil
}

func (c *blockingDirectoryClient) StartRecord(context.Context, string, string, string, int) error {
	return nil
}

func (c *blockingDirectoryClient) StopRecord(context.Context, string, string, string) error {
	c.mu.Lock()
	c.active = false
	c.mu.Unlock()
	return nil
}

func (c *blockingDirectoryClient) StartMP4RecordInDirectory(context.Context, string, string, string, int, string) error {
	close(c.started)
	<-c.release
	c.mu.Lock()
	c.active = true
	c.mu.Unlock()
	return nil
}

func insertLegacyUnknownClaims(t *testing.T, db *gorm.DB, channelID uint, target MediaTarget) {
	t.Helper()
	require.NoError(t, db.Create(&models.GbRecorderClaim{
		ResourceKey: ChannelResource(channelID),
		ChannelID:   channelID,
		OwnerKind:   OwnerLegacy,
		OwnerID:     "channel:legacy",
		State:       StateUnknown,
		Version:     1,
	}).Error)
	require.NoError(t, db.Create(&models.GbRecorderClaim{
		ResourceKey: MediaResource(target.NodeID, target.VHost, target.App, target.Stream),
		ChannelID:   channelID,
		OwnerKind:   OwnerLegacy,
		OwnerID:     "media:legacy",
		State:       StateUnknown,
		Version:     1,
		NodeID:      target.NodeID,
		VHost:       target.VHost,
		App:         target.App,
		Stream:      target.Stream,
		Generation:  target.Generation,
	}).Error)
}

func mustClaim(t *testing.T, claims *Claims, ctx context.Context, key string) *models.GbRecorderClaim {
	t.Helper()
	claim, err := claims.Get(ctx, key)
	require.NoError(t, err)
	return claim
}
