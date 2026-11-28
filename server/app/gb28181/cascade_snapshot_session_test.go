package gb28181

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

func saveCascadeSnapshotSession(ctx context.Context, db *gorm.DB, session model.GbCascadeSnapshotSession) error {
	_, _, err := reserveCascadeSnapshotSession(ctx, db, session)
	return err
}

func TestCascadeSnapshotSessionIsScopedAndIdempotent(t *testing.T) {
	db, platform, _ := cascadeDeviceStatusFixture(t, "2022")
	require.NoError(t, db.AutoMigrate(&model.GbCascadeSnapshotSession{}))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	session := model.GbCascadeSnapshotSession{
		PlatformID: platform.ID, UpstreamSessionID: "upstream-session-1",
		PublishedChannelID: "34020000001320000012", SourceDeviceID: 1, SourceChannelID: 2,
		ConfigRevision: platform.ConfigRevision, ProjectionRevision: platform.ProjectionRevision,
		State: model.CascadeSnapshotSessionPending, ExpiresAt: now.Add(time.Minute),
	}
	require.NoError(t, saveCascadeSnapshotSession(ctx, db, session))
	require.NoError(t, saveCascadeSnapshotSession(ctx, db, session), "same SIP transaction retry must be idempotent")

	got, err := findCascadeSnapshotSession(ctx, db, platform.ID, session.UpstreamSessionID)
	require.NoError(t, err)
	require.Equal(t, session.PublishedChannelID, got.PublishedChannelID)
	require.Equal(t, session.SourceDeviceID, got.SourceDeviceID)
	require.Equal(t, session.ConfigRevision, got.ConfigRevision)

	changed := session
	changed.PublishedChannelID = "34020000001320000013"
	require.ErrorIs(t, saveCascadeSnapshotSession(ctx, db, changed), ErrCascadeSnapshotSessionConflict)
	for _, change := range []func(*model.GbCascadeSnapshotSession){
		func(s *model.GbCascadeSnapshotSession) { s.SourceDeviceEpoch++ },
		func(s *model.GbCascadeSnapshotSession) { s.RequestKey = "another-transaction" },
		func(s *model.GbCascadeSnapshotSession) { s.RequestFingerprint = "another-payload" },
	} {
		changed = session
		change(&changed)
		require.ErrorIs(t, saveCascadeSnapshotSession(ctx, db, changed), ErrCascadeSnapshotSessionConflict)
	}
	require.NoError(t, db.Model(&model.GbCascadeSnapshotSession{}).Where("id = ?", got.ID).Update("state", model.CascadeSnapshotSessionCompleted).Error)
	require.ErrorIs(t, saveCascadeSnapshotSession(ctx, db, session), ErrCascadeSnapshotSessionConflict, "terminal SessionIDs must not be reused")

	_, err = findCascadeSnapshotSession(ctx, db, platform.ID+1, session.UpstreamSessionID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestCascadeSnapshotSessionCompletionRequiresCurrentAuthorization(t *testing.T) {
	db, platform, _ := cascadeDeviceStatusFixture(t, "2022")
	require.NoError(t, db.AutoMigrate(&model.GbCascadeSnapshotSession{}))
	platform.ConfigEnabled = true
	session := model.GbCascadeSnapshotSession{
		PlatformID: platform.ID, UpstreamSessionID: "upstream-session-2",
		PublishedChannelID: "34020000001320000012", SourceDeviceID: 1, SourceChannelID: 2,
		ConfigRevision: platform.ConfigRevision, ProjectionRevision: platform.ProjectionRevision,
		State: model.CascadeSnapshotSessionPending, ExpiresAt: time.Now().Add(time.Minute),
	}
	require.NoError(t, saveCascadeSnapshotSession(t.Context(), db, session))
	require.True(t, cascadeSnapshotSessionAuthorized(session, platform, session.SourceDeviceID, session.SourceChannelID, time.Now()))
	require.False(t, cascadeSnapshotSessionAuthorized(session, platform, session.SourceDeviceID+1, session.SourceChannelID, time.Now()))
	require.False(t, cascadeSnapshotSessionAuthorized(session, platform, session.SourceDeviceID, session.SourceChannelID, time.Now().Add(2*time.Minute)))

	changed := platform
	changed.ConfigRevision++
	require.False(t, cascadeSnapshotSessionAuthorized(session, changed, session.SourceDeviceID, session.SourceChannelID, time.Now()))
	changed = platform
	changed.ProjectionRevision++
	require.False(t, cascadeSnapshotSessionAuthorized(session, changed, session.SourceDeviceID, session.SourceChannelID, time.Now()))
	changed = platform
	changed.Enabled = false
	require.False(t, cascadeSnapshotSessionAuthorized(session, changed, session.SourceDeviceID, session.SourceChannelID, time.Now()))
}

func TestFailedCascadeSnapshotSessionCannotBeForwarded(t *testing.T) {
	db, platform, _ := cascadeDeviceStatusFixture(t, "2022")
	require.NoError(t, db.AutoMigrate(&model.GbCascadeSnapshotSession{}))
	now := time.Now().UTC()
	session := model.GbCascadeSnapshotSession{
		PlatformID: platform.ID, UpstreamSessionID: "upstream-session-failed",
		PublishedChannelID: "34020000001320000012", SourceDeviceID: 1, SourceChannelID: 2,
		ConfigRevision: platform.ConfigRevision, ProjectionRevision: platform.ProjectionRevision,
		State: model.CascadeSnapshotSessionPending, ExpiresAt: now.Add(time.Minute),
	}
	require.NoError(t, saveCascadeSnapshotSession(t.Context(), db, session))
	var saved model.GbCascadeSnapshotSession
	require.NoError(t, db.Where("platform_id = ? AND upstream_session_id = ?", session.PlatformID, session.UpstreamSessionID).Take(&saved).Error)
	failCascadeSnapshotSession(t.Context(), db, saved.ID)
	require.NoError(t, db.Where("id = ?", saved.ID).Take(&saved).Error)
	require.Equal(t, model.CascadeSnapshotSessionFailed, saved.State)
	require.False(t, cascadeSnapshotSessionAuthorized(saved, platform, session.SourceDeviceID, session.SourceChannelID, now))
}

func TestCascadeSnapshotForwardingLeaseOnlyReclaimsStaleSend(t *testing.T) {
	db, platform, _ := cascadeDeviceStatusFixture(t, "2022")
	require.NoError(t, db.AutoMigrate(&model.GbCascadeSnapshotSession{}))
	now := time.Now().UTC()
	session := model.GbCascadeSnapshotSession{
		PlatformID: platform.ID, UpstreamSessionID: "upstream-session-forwarding",
		PublishedChannelID: "34020000001320000012", SourceDeviceID: 1, SourceChannelID: 2,
		ConfigRevision: platform.ConfigRevision, ProjectionRevision: platform.ProjectionRevision,
		State: model.CascadeSnapshotSessionForwarding, ExpiresAt: now.Add(time.Minute), UpdatedAt: now.Add(-time.Minute),
	}
	require.NoError(t, saveCascadeSnapshotSession(t.Context(), db, session))
	var saved model.GbCascadeSnapshotSession
	require.NoError(t, db.Where("platform_id = ? AND upstream_session_id = ?", session.PlatformID, session.UpstreamSessionID).Take(&saved).Error)
	stale := saved
	claimed, err := reclaimStaleCascadeSnapshotForwarding(t.Context(), db, saved, now, 15*time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, db.Where("id = ?", saved.ID).Take(&saved).Error)
	require.Equal(t, model.CascadeSnapshotSessionPending, saved.State)

	claimed, err = claimCascadeSnapshotSession(t.Context(), db, saved.ID, now)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, db.Where("id = ?", saved.ID).Take(&saved).Error)
	claimed, err = reclaimStaleCascadeSnapshotForwarding(t.Context(), db, saved, now, 15*time.Second)
	require.NoError(t, err)
	require.False(t, claimed, "a fresh in-flight request must not be stolen")
	claimed, err = reclaimStaleCascadeSnapshotForwarding(t.Context(), db, stale, now, 15*time.Second)
	require.NoError(t, err)
	require.False(t, claimed, "a stale read must not reclaim another sender's fresh lease")
	require.Error(t, completeCascadeSnapshotSession(t.Context(), db, stale, now), "an expired sender must not complete another sender's lease")
	require.NoError(t, retryCascadeSnapshotSession(t.Context(), db, stale, now), "a late failure must not release another sender's lease")
	require.NoError(t, db.Where("id = ?", saved.ID).Take(&saved).Error)
	require.Equal(t, model.CascadeSnapshotSessionForwarding, saved.State)
	require.NoError(t, completeCascadeSnapshotSession(t.Context(), db, saved, now))
}
