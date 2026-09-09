package workrecording

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type testRecorderClient struct {
	active                  bool
	directories             []string
	starts, stops           int
	startErr, stopErr       error
	beforeStart, beforeStop func()
}

func (c *testRecorderClient) IsRecording(context.Context, string, string, string) (bool, error) {
	return c.active, nil
}
func (c *testRecorderClient) StartRecord(context.Context, string, string, string, int) error {
	if c.beforeStart != nil {
		c.beforeStart()
	}
	c.starts++
	c.active = true
	return c.startErr
}
func (c *testRecorderClient) StopRecord(context.Context, string, string, string) error {
	if c.beforeStop != nil {
		c.beforeStop()
	}
	c.stops++
	if c.stopErr == nil {
		c.active = false
	}
	return c.stopErr
}
func recorderFixture(t *testing.T) (*Recorder, *testRecorderClient, *bool) {
	t.Helper()
	client := &testRecorderClient{}
	matches := true
	r := NewRecorder(NewClaims(claimDB(t)), func(context.Context, MediaTarget) (func(), error) {
		if !matches {
			return nil, ErrVersionConflict
		}
		return func() {}, nil
	}, func(MediaTarget) (MP4Client, error) { return client, nil })
	return r, client, &matches
}
func testMedia() MediaTarget {
	return MediaTarget{NodeID: 1, VHost: "v", App: "rtp", Stream: "s", Generation: 1}
}

func TestRecorderRejectsEveryForeignOwnerBeforeExternalCalls(t *testing.T) {
	kinds := []string{OwnerWork, OwnerPlan, OwnerContinuous, OwnerManagement}
	for _, first := range kinds {
		for _, second := range kinds {
			if first == second {
				continue
			}
			t.Run(first+"/"+second, func(t *testing.T) {
				r, c, _ := recorderFixture(t)
				ctx := context.Background()
				h, err := r.Reserve(ctx, 1, Owner{Kind: first, ID: "first"})
				require.NoError(t, err)
				h, err = startTestRecording(r, ctx, h, testMedia(), 0)
				require.NoError(t, err)
				_, err = r.Reserve(ctx, 1, Owner{Kind: second, ID: "second"})
				require.ErrorIs(t, err, ErrOwnerConflict)
				foreign := h
				foreign.Owner = Owner{Kind: second, ID: "second"}
				_, err = r.Stop(ctx, foreign)
				require.ErrorIs(t, err, ErrOwnerConflict)
				require.Equal(t, 1, c.starts)
				require.Zero(t, c.stops)
			})
		}
	}
}
func TestRecorderUnknownStartIsNotRetriedOrAdopted(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	c.startErr = errors.New("timeout")
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	h, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.Error(t, err)
	claim, err := r.claims.Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateUnknown, claim.State)
	_, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.Error(t, err)
	require.Equal(t, 1, c.starts)
	h, err = r.Stop(ctx, h)
	require.NoError(t, err)
	require.Equal(t, 1, c.stops)
	_, err = r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "next"})
	require.ErrorIs(t, err, ErrOwnerConflict)
	// Stop confirmation does not assert file finalization or release ownership.
	claim, err = r.claims.Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateStopped, claim.State)
}
func TestRecorderDoesNotAdoptExistingMP4(t *testing.T) {
	r, c, _ := recorderFixture(t)
	c.active = true
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	_, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.ErrorIs(t, err, ErrAttributionUnknown)
	require.Zero(t, c.starts)
	require.Zero(t, c.stops)
	// The attempted owner cannot stop an unrelated preexisting MP4 either.
	row, err := r.claims.Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, OwnerLegacy, row.OwnerKind)
}
func TestRecorderStaleGenerationAndStaleHandleCannotStop(t *testing.T) {
	r, c, matches := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerPlan, ID: "run"})
	require.NoError(t, err)
	old := h
	h, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.NoError(t, err)
	_, err = r.Stop(ctx, old)
	require.ErrorIs(t, err, ErrVersionConflict)
	require.Zero(t, c.stops)
	*matches = false
	_, err = r.Stop(ctx, h)
	require.ErrorIs(t, err, ErrVersionConflict)
	require.Zero(t, c.stops)
}
func TestRecorderMediaCollisionDoesNotCallSecondStart(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "a"})
	require.NoError(t, err)
	_, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.NoError(t, err)
	other, err := r.Reserve(ctx, 2, Owner{Kind: OwnerWork, ID: "b"})
	require.NoError(t, err)
	_, err = startTestRecording(r, ctx, other, testMedia(), 0)
	require.ErrorIs(t, err, ErrOwnerConflict)
	require.Equal(t, 1, c.starts)
}
func TestRecorderIntentWriteFailurePreventsStart(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "a"})
	require.NoError(t, err)
	require.NoError(t, r.claims.db.Exec("CREATE TRIGGER fail_intent BEFORE UPDATE ON gb_recorder_claim WHEN NEW.state = 'unknown' BEGIN SELECT RAISE(ABORT,'unavailable'); END").Error)
	_, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.Error(t, err)
	require.Zero(t, c.starts)
}

func TestRecorderRestartAfterConfirmationWriteFailureKeepsIntent(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	require.NoError(t, r.claims.db.Exec("CREATE TRIGGER reject_confirmation BEFORE UPDATE ON gb_recorder_claim WHEN NEW.state = 'recording' BEGIN SELECT RAISE(ABORT,'unavailable'); END").Error)
	h, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.Error(t, err)
	require.Equal(t, 1, c.starts)
	require.NoError(t, r.claims.db.Exec("DROP TRIGGER reject_confirmation").Error)
	restarted := NewRecorder(NewClaims(r.claims.db), r.pin, r.client)
	restored, err := restarted.Reserve(ctx, 1, h.Owner)
	require.NoError(t, err)
	require.Equal(t, h, restored)
	_, err = startTestRecording(restarted, ctx, restored, testMedia(), 0)
	require.Error(t, err)
	require.Equal(t, 1, c.starts)
	restored, err = restarted.Stop(ctx, restored)
	require.NoError(t, err)
	_, err = startTestRecording(restarted, ctx, restored, testMedia(), 0)
	require.Error(t, err)
	require.Equal(t, 1, c.starts)
}
func TestRecorderStoppedClaimRejectsDelayedStartAndStopIsIdempotent(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	h, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.NoError(t, err)
	beforeStop := h
	h, err = r.Stop(ctx, h)
	require.NoError(t, err)
	_, err = startTestRecording(r, ctx, beforeStop, testMedia(), 0)
	require.ErrorIs(t, err, ErrVersionConflict)
	_, err = r.Stop(ctx, h)
	require.NoError(t, err)
	require.Equal(t, 1, c.stops)
	require.Equal(t, 1, c.starts)
}
func TestRecorderPairWriteFailureRollsBackChannelAndPreventsStop(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "job"})
	require.NoError(t, err)
	h, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.NoError(t, err)
	require.NoError(t, r.claims.db.Exec("CREATE TRIGGER reject_media BEFORE UPDATE ON gb_recorder_claim WHEN NEW.resource_key = '"+testMedia().key()+"' AND NEW.state = 'stopping' BEGIN SELECT RAISE(ABORT,'unavailable'); END").Error)
	_, err = r.Stop(ctx, h)
	require.Error(t, err)
	require.Zero(t, c.stops)
	row, err := r.claims.Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, h.Version, row.Version)
	require.Equal(t, StateRecording, row.State)
}

func TestRecorderPinsGenerationAcrossExternalWriteAndConfirmation(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	var generationMutex sync.Mutex
	pins, releases := 0, 0
	r.pin = func(context.Context, MediaTarget) (func(), error) {
		generationMutex.Lock()
		pins++
		return func() { releases++; generationMutex.Unlock() }, nil
	}
	assertPinned := func() {
		if generationMutex.TryLock() {
			generationMutex.Unlock()
			t.Fatal("source replacement was possible during external write")
		}
	}
	c.beforeStart = assertPinned
	c.beforeStop = assertPinned
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "a"})
	require.NoError(t, err)
	h, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.NoError(t, err)
	_, err = r.Stop(ctx, h)
	require.NoError(t, err)
	require.Equal(t, 2, pins)
	require.Equal(t, pins, releases)
	require.True(t, generationMutex.TryLock())
	generationMutex.Unlock()
}
func TestRecorderReleasesPinOnExternalFailure(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	released := false
	r.pin = func(context.Context, MediaTarget) (func(), error) { return func() { released = true }, nil }
	c.startErr = errors.New("timeout")
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "a"})
	require.NoError(t, err)
	_, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.Error(t, err)
	require.True(t, released)
}

func TestRecorderConcurrentChannelsShareOneMediaOwner(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	a, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "a"})
	require.NoError(t, err)
	b, err := r.Reserve(ctx, 2, Owner{Kind: OwnerPlan, ID: "b"})
	require.NoError(t, err)
	ready := make(chan struct{})
	results := make(chan error, 2)
	for _, h := range []RecorderHandle{a, b} {
		go func(h RecorderHandle) {
			<-ready
			_, err := startTestRecording(r, ctx, h, testMedia(), 0)
			results <- err
		}(h)
	}
	close(ready)
	wins := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			wins++
		} else {
			require.ErrorIs(t, err, ErrOwnerConflict)
		}
	}
	require.Equal(t, 1, wins)
	require.Equal(t, 1, c.starts)
}
func TestRecorderOldHandleCannotStopNextRunOfSameOwner(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	owner := Owner{Kind: OwnerContinuous, ID: "channel-1"}
	h, err := r.Reserve(ctx, 1, owner)
	require.NoError(t, err)
	h, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.NoError(t, err)
	h, err = r.Stop(ctx, h)
	require.NoError(t, err)
	old := h
	// Test-only simulation of a separately confirmed finalizer. Recorder.Stop
	// itself must never perform these releases.
	media, err := r.claims.Get(ctx, testMedia().key())
	require.NoError(t, err)
	require.NoError(t, r.claims.Release(ctx, media.ResourceKey, owner, media.Version))
	require.NoError(t, r.claims.Release(ctx, ChannelResource(1), owner, h.Version))
	h, err = r.Reserve(ctx, 1, owner)
	require.NoError(t, err)
	_, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.NoError(t, err)
	_, err = r.Stop(ctx, old)
	require.ErrorIs(t, err, ErrVersionConflict)
	require.Equal(t, 1, c.stops)
	require.True(t, c.active)
}

func TestRecorderReleaseStoppedAllowsNextJobBeforeFileFinalization(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	a := Owner{Kind: OwnerWork, ID: "a"}
	h, err := r.Reserve(ctx, 1, a)
	require.NoError(t, err)
	h, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.NoError(t, err)
	require.ErrorIs(t, r.ReleaseStopped(ctx, h), ErrVersionConflict)
	h, err = r.Stop(ctx, h)
	require.NoError(t, err)
	require.NoError(t, r.ReleaseStopped(ctx, h))
	require.NoError(t, r.ReleaseStopped(ctx, h))
	next, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "b"})
	require.NoError(t, err)
	next, err = startTestRecording(r, ctx, next, testMedia(), 0)
	require.NoError(t, err)
	_, err = r.Stop(ctx, h)
	require.Error(t, err)
	require.Equal(t, 1, c.stops)
	require.Equal(t, 2, c.starts)
	require.Equal(t, []string{"/tmp/work-recordings/a", "/tmp/work-recordings/b"}, c.directories)
	row, err := r.claims.Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, "/tmp/work-recordings/b", row.RecordingRoot)
}
func TestRecorderRejectsWorkStartWithoutOwnedDirectory(t *testing.T) {
	r, c, _ := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "a"})
	require.NoError(t, err)
	_, err = r.Start(ctx, h, testMedia(), 0)
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Zero(t, c.starts)
	m := testMedia()
	m.RecordingRoot = "/tmp/work-recordings/b"
	_, err = r.Start(ctx, h, m, 0)
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Zero(t, c.starts)
}

func startTestRecording(r *Recorder, ctx context.Context, h RecorderHandle, target MediaTarget, maxSecond int) (RecorderHandle, error) {
	if h.Owner.Kind == OwnerWork {
		target.RecordingRoot = "/tmp/work-recordings/" + h.Owner.ID
	}
	return r.Start(ctx, h, target, maxSecond)
}
func (c *testRecorderClient) StartMP4RecordInDirectory(ctx context.Context, vhost, app, stream string, maxSecond int, directory string) error {
	c.directories = append(c.directories, directory)
	return c.StartRecord(ctx, vhost, app, stream, maxSecond)
}

func TestRecorderReleaseStoppedRollsBackBothClaims(t *testing.T) {
	r, _, _ := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "a"})
	require.NoError(t, err)
	h, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.NoError(t, err)
	h, err = r.Stop(ctx, h)
	require.NoError(t, err)
	require.NoError(t, r.claims.db.Exec("CREATE TRIGGER reject_release BEFORE UPDATE ON gb_recorder_claim WHEN NEW.resource_key = '"+ChannelResource(1)+"' AND NEW.state = 'idle' BEGIN SELECT RAISE(ABORT,'unavailable'); END").Error)
	require.Error(t, r.ReleaseStopped(ctx, h))
	for _, key := range []string{ChannelResource(1), testMedia().key()} {
		claim, err := r.claims.Get(ctx, key)
		require.NoError(t, err)
		require.Equal(t, StateStopped, claim.State)
		require.Equal(t, "a", claim.OwnerID)
	}
}

func TestRecorderReleaseRejectsMissingPersistentDirectory(t *testing.T) {
	r, _, _ := recorderFixture(t)
	ctx := context.Background()
	h, err := r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "a"})
	require.NoError(t, err)
	h, err = startTestRecording(r, ctx, h, testMedia(), 0)
	require.NoError(t, err)
	h, err = r.Stop(ctx, h)
	require.NoError(t, err)
	require.NoError(t, r.claims.db.Exec("UPDATE gb_recorder_claim SET recording_root = ''").Error)
	require.ErrorIs(t, r.ReleaseStopped(ctx, h), ErrInvalidRequest)
	_, err = r.Reserve(ctx, 1, Owner{Kind: OwnerWork, ID: "b"})
	require.ErrorIs(t, err, ErrOwnerConflict)
}
