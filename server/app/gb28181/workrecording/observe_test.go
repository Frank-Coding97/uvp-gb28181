package workrecording

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecorderObserveFalseMarksRecordingPairStopped(t *testing.T) {
	r, client, _ := observeFixture(t)
	ctx := context.Background()
	target := testMedia()
	h := prepareObservedClaim(t, r, Owner{Kind: OwnerWork, ID: "job"}, StateRecording, target)

	observed, state, err := r.Observe(ctx, h)
	require.NoError(t, err)
	require.Equal(t, StateStopped, state)
	require.Equal(t, h.Version+1, observed.Version)
	require.Equal(t, 1, client.isCalls)
	require.Zero(t, client.starts)
	require.Zero(t, client.stops)
	for _, key := range []string{ChannelResource(1), target.key()} {
		claim := mustClaim(t, r.claims, ctx, key)
		require.Equal(t, StateStopped, claim.State)
		require.Equal(t, OwnerWork, claim.OwnerKind)
	}
}

func TestRecorderObserveReadErrorDoesNotReleaseOrMutateClaim(t *testing.T) {
	r, client, _ := observeFixture(t)
	ctx := context.Background()
	target := testMedia()
	h := prepareObservedClaim(t, r, Owner{Kind: OwnerWork, ID: "job"}, StateRecording, target)
	client.isErr = errors.New("zlm unavailable")

	observed, state, err := r.Observe(ctx, h)
	require.ErrorIs(t, err, client.isErr)
	require.Equal(t, StateUnknown, state)
	require.Equal(t, h, observed)
	require.Equal(t, 1, client.isCalls)
	for _, key := range []string{ChannelResource(1), target.key()} {
		claim := mustClaim(t, r.claims, ctx, key)
		require.Equal(t, StateRecording, claim.State)
		require.Equal(t, OwnerWork, claim.OwnerKind)
	}
}

func TestRecorderObserveFalseDatabaseFailureKeepsClaim(t *testing.T) {
	r, client, _ := observeFixture(t)
	ctx := context.Background()
	target := testMedia()
	h := prepareObservedClaim(t, r, Owner{Kind: OwnerWork, ID: "job"}, StateRecording, target)
	require.NoError(t, r.claims.db.Exec("CREATE TRIGGER reject_observe_stop BEFORE UPDATE ON gb_recorder_claim WHEN NEW.resource_key = '"+target.key()+"' AND NEW.state = 'stopped' BEGIN SELECT RAISE(ABORT,'unavailable'); END").Error)

	observed, state, err := r.Observe(ctx, h)
	require.Error(t, err)
	require.Equal(t, StateUnknown, state)
	require.Equal(t, h, observed)
	require.Equal(t, 1, client.isCalls)
	for _, key := range []string{ChannelResource(1), target.key()} {
		claim := mustClaim(t, r.claims, ctx, key)
		require.Equal(t, StateRecording, claim.State)
		require.Equal(t, OwnerWork, claim.OwnerKind)
	}
}

func TestRecorderObserveUnknownActivePreservesUnknownWithoutAdoption(t *testing.T) {
	r, client, _ := observeFixture(t)
	ctx := context.Background()
	target := testMedia()
	h := prepareObservedClaim(t, r, Owner{Kind: OwnerWork, ID: "job"}, StateUnknown, target)
	client.active = true

	observed, state, err := r.Observe(ctx, h)
	require.NoError(t, err)
	require.Equal(t, StateUnknown, state)
	require.Equal(t, h, observed)
	require.Equal(t, 1, client.isCalls)
	for _, key := range []string{ChannelResource(1), target.key()} {
		claim := mustClaim(t, r.claims, ctx, key)
		require.Equal(t, StateUnknown, claim.State)
		require.Equal(t, OwnerWork, claim.OwnerKind)
	}
}

func TestRecorderObservePinMismatchDoesNotCallZLM(t *testing.T) {
	r, client, pinErr := observeFixture(t)
	ctx := context.Background()
	target := testMedia()
	h := prepareObservedClaim(t, r, Owner{Kind: OwnerWork, ID: "job"}, StateRecording, target)
	*pinErr = errors.New("generation changed")

	observed, state, err := r.Observe(ctx, h)
	require.ErrorIs(t, err, *pinErr)
	require.Equal(t, StateUnknown, state)
	require.Equal(t, h, observed)
	require.Zero(t, client.isCalls)
	claim := mustClaim(t, r.claims, ctx, ChannelResource(1))
	require.Equal(t, StateRecording, claim.State)
	require.Equal(t, OwnerWork, claim.OwnerKind)
}

func TestRecorderObserveForeignOwnerDoesNotCallZLM(t *testing.T) {
	r, client, _ := observeFixture(t)
	ctx := context.Background()
	target := testMedia()
	h := prepareObservedClaim(t, r, Owner{Kind: OwnerWork, ID: "job"}, StateRecording, target)
	foreign := h
	foreign.Owner = Owner{Kind: OwnerPlan, ID: "other"}

	observed, state, err := r.Observe(ctx, foreign)
	require.ErrorIs(t, err, ErrOwnerConflict)
	require.Equal(t, StateUnknown, state)
	require.Equal(t, foreign, observed)
	require.Zero(t, client.isCalls)
	require.Equal(t, StateRecording, mustClaim(t, r.claims, ctx, ChannelResource(1)).State)
}

func TestRecorderObserveStartingAndStoppedDoNotCallZLM(t *testing.T) {
	r, client, _ := observeFixture(t)
	ctx := context.Background()
	target := testMedia()
	starting, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "starting"})
	require.NoError(t, err)
	observed, state, err := r.Observe(ctx, starting)
	require.NoError(t, err)
	require.Equal(t, StateStarting, state)
	require.Equal(t, starting, observed)

	stoppedTarget := target
	stoppedTarget.NodeID = 2
	stoppedTarget.Stream = "s2"
	stopped := prepareObservedClaimOnChannel(t, r, 2, Owner{Kind: OwnerWork, ID: "stopped"}, StateStopped, stoppedTarget)
	observed, state, err = r.Observe(ctx, stopped)
	require.NoError(t, err)
	require.Equal(t, StateStopped, state)
	require.Equal(t, stopped, observed)
	require.Zero(t, client.isCalls)
}

type observeClient struct {
	mu      sync.Mutex
	active  bool
	isErr   error
	isCalls int
	starts  int
	stops   int
}

func (c *observeClient) IsRecording(context.Context, string, string, string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isCalls++
	return c.active, c.isErr
}

func (c *observeClient) StartRecord(context.Context, string, string, string, int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starts++
	c.active = true
	return nil
}

func (c *observeClient) StopRecord(context.Context, string, string, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stops++
	c.active = false
	return nil
}

func observeFixture(t *testing.T) (*Recorder, *observeClient, *error) {
	t.Helper()
	client := &observeClient{}
	var pinErr error
	r := NewRecorder(NewClaims(claimDB(t)), func(context.Context, MediaTarget) (func(), error) {
		if pinErr != nil {
			return nil, pinErr
		}
		return func() {}, nil
	}, func(MediaTarget) (MP4Client, error) { return client, nil })
	return r, client, &pinErr
}

func prepareObservedClaim(t *testing.T, r *Recorder, owner Owner, state string, target MediaTarget) RecorderHandle {
	return prepareObservedClaimOnChannel(t, r, 1, owner, state, target)
}

func prepareObservedClaimOnChannel(t *testing.T, r *Recorder, channelID uint, owner Owner, state string, target MediaTarget) RecorderHandle {
	t.Helper()
	ctx := context.Background()
	h, err := r.Reserve(ctx, channelID, owner)
	require.NoError(t, err)
	h, err = r.bind(ctx, h, target)
	require.NoError(t, err)
	if state == StateStopped {
		h, err = r.pairState(ctx, h, StateRecording, false)
		require.NoError(t, err)
	}
	h, err = r.pairState(ctx, h, state, false)
	require.NoError(t, err)
	return h
}
