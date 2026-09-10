package workrecording

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// leakAbandonedStart reproduces the production defect: a start that reserved the
// channel, then failed in prepare with a source lease already held. The engine
// deliberately keeps both (fail closed on an uncertain source), so the claim
// stays in starting and the job is folded to unknown.
func leakAbandonedStart(t *testing.T, service *Service, prepare *servicePrepare) Snapshot {
	t.Helper()
	prepare.prepareErr = errors.New("source cleanup pending")
	prepare.onError = PreparedRecording{
		Target: MediaTarget{
			NodeID: 1, VHost: "__defaultVhost__", App: "rtp", Stream: "channel-\x01", Generation: 1,
			RecordingRoot: "/var/lib/uvp/work-recordings/placeholder",
		},
		Release: func() { prepare.releases.Add(1) },
	}
	started, err := service.Start(context.Background(), 7, serviceStartRequest(1, "request-leak"), 30)
	require.ErrorIs(t, err, prepare.prepareErr)
	require.Equal(t, StateUnknown, started.State)
	return started
}

func ageClaim(t *testing.T, db *gorm.DB, channelID uint, age time.Duration) {
	t.Helper()
	// UpdateColumn bypasses GORM's automatic updated_at so the aged value sticks.
	require.NoError(t, db.Model(&models.GbRecorderClaim{}).
		Where("resource_key = ?", ChannelResource(channelID)).
		UpdateColumn("updated_at", time.Now().Add(-age)).Error)
}

func TestWorkServiceReclaimAbandonedStartReleasesTheLeakedReservation(t *testing.T) {
	service, client, prepare, db := serviceFixture(t)
	started := leakAbandonedStart(t, service, prepare)
	ageClaim(t, db, 1, time.Hour)

	// The leak really does block the channel, otherwise this fix is pointless.
	blocked := NewClaims(db)
	claim, err := blocked.Get(context.Background(), ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateStarting, claim.State)
	require.True(t, hasForeignActiveClaim([]models.GbRecorderClaim{*claim}))

	reclaimed, err := service.ReclaimAbandonedStarts(context.Background(), AbandonedStartGrace)
	require.NoError(t, err)
	require.Equal(t, 1, reclaimed)

	released, err := blocked.Get(context.Background(), ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateIdle, released.State)
	require.Empty(t, released.OwnerKind)
	require.Empty(t, released.OwnerID)
	require.False(t, hasForeignActiveClaim([]models.GbRecorderClaim{*released}))

	// Reclaiming goes through Stop, so the job lands in a terminal state and the
	// held source lease is released instead of pinning the live stream.
	job := mustServiceJob(t, db, started.ID)
	require.Equal(t, StateStopped, job.State)
	require.EqualValues(t, 1, prepare.releases.Load())
	require.Zero(t, client.starts)
}

func TestWorkServiceReclaimAbandonedStartSkipsFreshReservations(t *testing.T) {
	service, _, prepare, db := serviceFixture(t)
	started := leakAbandonedStart(t, service, prepare)

	reclaimed, err := service.ReclaimAbandonedStarts(context.Background(), AbandonedStartGrace)
	require.NoError(t, err)
	require.Zero(t, reclaimed, "刚刚预留的占用可能属于正在进行的启动，不能回收")

	claim, err := NewClaims(db).Get(context.Background(), ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateStarting, claim.State)
	require.Equal(t, StateUnknown, mustServiceJob(t, db, started.ID).State)
	require.Zero(t, prepare.releases.Load())
}

func TestWorkServiceReclaimAbandonedStartKeepsAnActiveRecording(t *testing.T) {
	service, client, _, db := serviceFixture(t)
	ctx := context.Background()
	started, err := service.Start(ctx, 7, serviceStartRequest(1, "request-live"), 30)
	require.NoError(t, err)
	require.Equal(t, StateRecording, started.State)
	claim, err := NewClaims(db).Get(ctx, ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateRecording, claim.State, "绑定后的占用必须离开 starting，回收只针对 starting")
	ageClaim(t, db, 1, time.Hour)

	reclaimed, err := service.ReclaimAbandonedStarts(ctx, AbandonedStartGrace)
	require.NoError(t, err)
	require.Zero(t, reclaimed, "正在录制的占用不能被回收")

	live := mustServiceJob(t, db, started.ID)
	require.Equal(t, StateRecording, live.State)
	require.Equal(t, 1, client.starts)
}

func TestWorkServiceReclaimAbandonedStartIsIdempotent(t *testing.T) {
	service, _, prepare, db := serviceFixture(t)
	leakAbandonedStart(t, service, prepare)
	ageClaim(t, db, 1, time.Hour)

	first, err := service.ReclaimAbandonedStarts(context.Background(), AbandonedStartGrace)
	require.NoError(t, err)
	require.Equal(t, 1, first)
	second, err := service.ReclaimAbandonedStarts(context.Background(), AbandonedStartGrace)
	require.NoError(t, err)
	require.Zero(t, second)
}

func TestWorkServiceReclaimAbandonedStartRejectsInvalidThreshold(t *testing.T) {
	service, _, _, _ := serviceFixture(t)
	for _, olderThan := range []time.Duration{0, -time.Second} {
		_, err := service.ReclaimAbandonedStarts(context.Background(), olderThan)
		require.ErrorIs(t, err, ErrInvalidRequest, olderThan.String())
	}
}

func TestAbandonedStartReconcilerIsOptionalAndStoppable(t *testing.T) {
	require.Nil(t, NewAbandonedStartReconciler(nil, time.Minute, time.Minute))
	service, _, _, _ := serviceFixture(t)
	require.Nil(t, NewAbandonedStartReconciler(service, 0, time.Minute))
	require.Nil(t, NewAbandonedStartReconciler(service, time.Minute, 0))

	var absent *AbandonedStartReconciler
	require.NotPanics(t, func() { absent.Stop() })
	require.NotPanics(t, func() { absent.Start(context.Background()) })

	reconciler := NewAbandonedStartReconciler(service, time.Millisecond, time.Minute)
	require.NotNil(t, reconciler)
	reconciler.Start(context.Background())
	reconciler.Stop()
	// Stop is idempotent; a second call must not panic on a closed channel.
	require.NotPanics(t, func() { reconciler.Stop() })
}

func TestWorkServiceReclaimAbandonedStartReleasesAClaimWithoutItsJob(t *testing.T) {
	service, _, prepare, db := serviceFixture(t)
	started := leakAbandonedStart(t, service, prepare)
	ageClaim(t, db, 1, time.Hour)
	// A claim whose ledger row is gone has nothing to drive through Stop.
	require.NoError(t, db.Where("id = ?", started.ID).Delete(&models.GbWorkRecording{}).Error)

	reclaimed, err := service.ReclaimAbandonedStarts(context.Background(), AbandonedStartGrace)
	require.NoError(t, err)
	require.Equal(t, 1, reclaimed)

	claim, err := NewClaims(db).Get(context.Background(), ChannelResource(1))
	require.NoError(t, err)
	require.Equal(t, StateIdle, claim.State)
}
