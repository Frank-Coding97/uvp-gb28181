package workrecording

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type legacyCheckClient struct {
	mu       sync.Mutex
	active   map[string]bool
	err      map[string]error
	checked  []string
	startCnt int
}

func (c *legacyCheckClient) IsRecording(_ context.Context, vhost, app, stream string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := vhost + "/" + app + "/" + stream
	c.checked = append(c.checked, key)
	return c.active[key], c.err[key]
}

func (c *legacyCheckClient) StartRecord(context.Context, string, string, string, int) error {
	c.mu.Lock()
	c.startCnt++
	c.mu.Unlock()
	return nil
}

func (c *legacyCheckClient) StopRecord(context.Context, string, string, string) error { return nil }

func newLegacyCheckFixture(t *testing.T) (*Recorder, *legacyCheckClient) {
	t.Helper()
	client := &legacyCheckClient{active: map[string]bool{}, err: map[string]error{}}
	r := NewRecorder(NewClaims(claimDB(t)), func(context.Context, MediaTarget) (func(), error) {
		return func() {}, nil
	}, func(MediaTarget) (MP4Client, error) {
		return client, nil
	})
	require.NoError(t, r.claims.db.AutoMigrate(&models.GbChannel{}, &models.GbRecordingSession{}))
	return r, client
}

func seedLegacyChannel(t *testing.T, r *Recorder, channelID uint, target MediaTarget, version uint64) {
	t.Helper()
	db := r.claims.db
	channel := models.GbChannel{ID: channelID, DeviceID: "device", ChannelID: "channel", CloudRecordingEnabled: false, CloudRecordingState: models.CloudRecordingStateDisabled}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&models.GbRecorderClaim{
		ResourceKey: ChannelResource(channelID), ChannelID: channelID, OwnerKind: OwnerLegacy,
		OwnerID: fmt.Sprintf("channel:%d", channelID), State: StateUnknown, Version: version,
	}).Error)
	require.NoError(t, db.Create(&models.GbRecorderClaim{
		ResourceKey: target.key(), ChannelID: channelID, NodeID: target.NodeID, VHost: target.VHost,
		App: target.App, Stream: target.Stream, Generation: target.Generation, OwnerKind: OwnerLegacy,
		OwnerID: "media:legacy", State: StateUnknown, Version: version,
	}).Error)
}

func seedLegacyMedia(t *testing.T, r *Recorder, channelID uint, target MediaTarget, ownerID string, version uint64) {
	t.Helper()
	require.NoError(t, r.claims.db.Create(&models.GbRecorderClaim{
		ResourceKey: target.key(), ChannelID: channelID, NodeID: target.NodeID, VHost: target.VHost,
		App: target.App, Stream: target.Stream, Generation: target.Generation, OwnerKind: OwnerLegacy,
		OwnerID: ownerID, State: StateUnknown, Version: version,
	}).Error)
}

func legacyMediaKey(target MediaTarget) string {
	return target.VHost + "/" + target.App + "/" + target.Stream
}

func TestReserveAfterLegacyCheckReclaimsDisabledLegacyAndAcquiresWork(t *testing.T) {
	r, client := newLegacyCheckFixture(t)
	target := testMedia()
	seedLegacyChannel(t, r, 1, target, 7)

	h, err := r.ReserveAfterLegacyCheck(context.Background(), 1, Owner{Kind: OwnerWork, ID: "job"}, target)
	require.NoError(t, err)
	require.Equal(t, RecorderHandle{ChannelID: 1, Owner: Owner{Kind: OwnerWork, ID: "job"}, Version: 9}, h)
	require.Equal(t, []string{legacyMediaKey(target)}, client.checked)
	require.Zero(t, client.startCnt)

	channel := mustClaim(t, r.claims, context.Background(), ChannelResource(1))
	require.Equal(t, OwnerWork, channel.OwnerKind)
	require.Equal(t, "job", channel.OwnerID)
	require.Equal(t, StateStarting, channel.State)
	require.Equal(t, uint64(9), channel.Version)
	require.Equal(t, uint(1), channel.ChannelID)
	media := mustClaim(t, r.claims, context.Background(), target.key())
	require.Empty(t, media.OwnerKind)
	require.Equal(t, StateIdle, media.State)
	require.Empty(t, media.OwnerID)
	require.Equal(t, uint64(8), media.Version)
}

func TestReserveAfterLegacyCheckRejectsNoLegacyOrForeignClaim(t *testing.T) {
	r, _ := newLegacyCheckFixture(t)
	target := testMedia()
	channel := models.GbChannel{ID: 1, DeviceID: "device", ChannelID: "channel", CloudRecordingEnabled: false}
	require.NoError(t, r.claims.db.Create(&channel).Error)
	_, err := r.ReserveAfterLegacyCheck(context.Background(), 1, Owner{Kind: OwnerWork, ID: "job"}, target)
	require.ErrorIs(t, err, ErrOwnerConflict)

	require.NoError(t, r.claims.db.Create(&models.GbRecorderClaim{
		ResourceKey: ChannelResource(1), ChannelID: 1, OwnerKind: OwnerPlan, OwnerID: "plan",
		State: StateUnknown, Version: 1,
	}).Error)
	_, err = r.ReserveAfterLegacyCheck(context.Background(), 1, Owner{Kind: OwnerWork, ID: "job"}, target)
	require.ErrorIs(t, err, ErrOwnerConflict)
}

func TestReserveAfterLegacyCheckKeepsClaimWhenZLMIsActiveOrUnavailable(t *testing.T) {
	tests := []struct {
		name string
		set  func(*legacyCheckClient, string)
	}{
		{name: "active", set: func(c *legacyCheckClient, key string) { c.active[key] = true }},
		{name: "error", set: func(c *legacyCheckClient, key string) { c.err[key] = errors.New("zlm unavailable") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, client := newLegacyCheckFixture(t)
			target := testMedia()
			seedLegacyChannel(t, r, 1, target, 3)
			tc.set(client, legacyMediaKey(target))

			_, err := r.ReserveAfterLegacyCheck(context.Background(), 1, Owner{Kind: OwnerWork, ID: "job"}, target)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrAttributionUnknown)
			require.Zero(t, client.startCnt)
			for _, key := range []string{ChannelResource(1), target.key()} {
				claim := mustClaim(t, r.claims, context.Background(), key)
				require.Equal(t, OwnerLegacy, claim.OwnerKind)
				require.Equal(t, StateUnknown, claim.State)
				require.Equal(t, uint64(3), claim.Version)
			}
		})
	}
}

func TestReserveAfterLegacyCheckChecksEveryLegacyMediaBeforeReclaim(t *testing.T) {
	r, client := newLegacyCheckFixture(t)
	first := testMedia()
	seedLegacyChannel(t, r, 1, first, 4)
	second := first
	second.Stream = "second"
	seedLegacyMedia(t, r, 1, second, "media:second", 6)
	client.active[legacyMediaKey(second)] = true

	_, err := r.ReserveAfterLegacyCheck(context.Background(), 1, Owner{Kind: OwnerWork, ID: "job"}, first)
	require.ErrorIs(t, err, ErrAttributionUnknown)
	require.ElementsMatch(t, []string{legacyMediaKey(first), legacyMediaKey(second)}, client.checked)
	for _, key := range []string{ChannelResource(1), first.key(), second.key()} {
		claim := mustClaim(t, r.claims, context.Background(), key)
		require.Equal(t, OwnerLegacy, claim.OwnerKind)
		require.Equal(t, StateUnknown, claim.State)
	}
}

func TestReserveAfterLegacyCheckRejectsIncompleteLegacyMedia(t *testing.T) {
	r, client := newLegacyCheckFixture(t)
	target := testMedia()
	seedLegacyChannel(t, r, 1, target, 2)
	require.NoError(t, r.claims.db.Model(&models.GbRecorderClaim{}).
		Where("resource_key = ?", target.key()).Updates(map[string]any{"stream": ""}).Error)

	_, err := r.ReserveAfterLegacyCheck(context.Background(), 1, Owner{Kind: OwnerWork, ID: "job"}, target)
	require.ErrorIs(t, err, ErrAttributionUnknown)
	require.Empty(t, client.checked)
	require.Equal(t, StateUnknown, mustClaim(t, r.claims, context.Background(), ChannelResource(1)).State)
}

func TestReserveAfterLegacyCheckRollsBackClaimReclaimOnDatabaseFailure(t *testing.T) {
	r, _ := newLegacyCheckFixture(t)
	target := testMedia()
	seedLegacyChannel(t, r, 1, target, 5)
	require.NoError(t, r.claims.db.Exec("CREATE TRIGGER reject_legacy_reclaim BEFORE UPDATE ON gb_recorder_claim WHEN NEW.owner_kind = 'work_job' BEGIN SELECT RAISE(ABORT, 'write unavailable'); END").Error)

	h, err := r.ReserveAfterLegacyCheck(context.Background(), 1, Owner{Kind: OwnerWork, ID: "job"}, target)
	require.Error(t, err)
	require.Zero(t, h.Version)
	for _, key := range []string{ChannelResource(1), target.key()} {
		claim := mustClaim(t, r.claims, context.Background(), key)
		require.Equal(t, OwnerLegacy, claim.OwnerKind)
		require.Equal(t, StateUnknown, claim.State)
		require.Equal(t, uint64(5), claim.Version)
	}
}

func TestReserveAfterLegacyCheckRechecksDesiredAndSessions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Recorder, MediaTarget)
		errIs  error
	}{
		{name: "desired enabled", mutate: func(r *Recorder, _ MediaTarget) {
			require.NoError(t, r.claims.db.Model(&models.GbChannel{}).Where("id = ?", 1).Update("cloud_recording_enabled", true).Error)
		}, errIs: ErrOwnerConflict},
		{name: "unfinished session", mutate: func(r *Recorder, target MediaTarget) {
			require.NoError(t, r.claims.db.Create(&models.GbRecordingSession{ChannelID: 1, DeviceID: "device", NodeID: target.NodeID, VHost: target.VHost, App: target.App, Stream: target.Stream, State: models.RecordingSessionStateRecording}).Error)
		}, errIs: ErrAttributionUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, client := newLegacyCheckFixture(t)
			target := testMedia()
			seedLegacyChannel(t, r, 1, target, 2)
			clientHook := func() {
				tc.mutate(r, target)
			}
			client.active = map[string]bool{}
			client.err = map[string]error{}
			// The factory returns a client whose first observation performs the
			// mutation after the external proof and before the final transaction.
			wrapped := &callbackLegacyClient{legacyCheckClient: client, after: clientHook}
			r.client = func(MediaTarget) (MP4Client, error) { return wrapped, nil }

			_, err := r.ReserveAfterLegacyCheck(context.Background(), 1, Owner{Kind: OwnerWork, ID: "job"}, target)
			require.ErrorIs(t, err, tc.errIs)
			require.Equal(t, OwnerLegacy, mustClaim(t, r.claims, context.Background(), ChannelResource(1)).OwnerKind)
		})
	}
}

type callbackLegacyClient struct {
	*legacyCheckClient
	after func()
}

func (c *callbackLegacyClient) IsRecording(ctx context.Context, vhost, app, stream string) (bool, error) {
	active, err := c.legacyCheckClient.IsRecording(ctx, vhost, app, stream)
	if c.after != nil {
		c.after()
		c.after = nil
	}
	return active, err
}

func TestReserveAfterLegacyCheckRejectsChangedLegacyVersion(t *testing.T) {
	r, client := newLegacyCheckFixture(t)
	target := testMedia()
	seedLegacyChannel(t, r, 1, target, 2)
	wrapped := &callbackLegacyClient{legacyCheckClient: client, after: func() {
		require.NoError(t, r.claims.db.Model(&models.GbRecorderClaim{}).
			Where("resource_key = ?", target.key()).Update("version", 99).Error)
	}}
	r.client = func(MediaTarget) (MP4Client, error) { return wrapped, nil }

	_, err := r.ReserveAfterLegacyCheck(context.Background(), 1, Owner{Kind: OwnerWork, ID: "job"}, target)
	require.ErrorIs(t, err, ErrVersionConflict)
	require.Equal(t, OwnerLegacy, mustClaim(t, r.claims, context.Background(), ChannelResource(1)).OwnerKind)
}
