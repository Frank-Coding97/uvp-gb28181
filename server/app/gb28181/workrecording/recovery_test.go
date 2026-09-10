package workrecording

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestRecoverLegacyQuarantinesChannelAndMediaWithoutAdoption(t *testing.T) {
	db := claimDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}, &models.GbRecordingSession{}))
	c := models.GbChannel{DeviceID: "D", ChannelID: "C", CloudRecordingEnabled: true}
	require.NoError(t, db.Create(&c).Error)
	session := models.GbRecordingSession{ChannelID: c.ID, DeviceID: "D", NodeID: 3, VHost: "v", App: "rtp", Stream: "s", State: models.RecordingSessionStateRecording}
	require.NoError(t, db.Create(&session).Error)
	s := NewClaims(db)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		require.NoError(t, s.RecoverLegacy(ctx))
	}
	for _, key := range []string{ChannelResource(c.ID), MediaResource(3, "v", "rtp", "s")} {
		got, err := s.Get(ctx, key)
		require.NoError(t, err)
		require.Equal(t, OwnerLegacy, got.OwnerKind)
		require.Equal(t, StateUnknown, got.State)
		_, err = s.Acquire(ctx, key, Owner{Kind: OwnerWork, ID: "job"}, 0)
		require.ErrorIs(t, err, ErrOwnerConflict)
		require.Error(t, s.Release(ctx, key, Owner{Kind: OwnerLegacy, ID: got.OwnerID}, got.Version))
	}
	var stillEnabled models.GbChannel
	require.NoError(t, db.First(&stillEnabled, c.ID).Error)
	require.True(t, stillEnabled.CloudRecordingEnabled)
}

func TestRecoverLegacyPreservesExplicitOwnerAndStoppedHistory(t *testing.T) {
	db := claimDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}, &models.GbRecordingSession{}))
	c := models.GbChannel{DeviceID: "D", ChannelID: "C", CloudRecordingEnabled: true}
	require.NoError(t, db.Create(&c).Error)
	s := NewClaims(db)
	ctx := context.Background()
	owner := Owner{Kind: OwnerPlan, ID: "plan-1"}
	before, err := s.Acquire(ctx, ChannelResource(c.ID), owner, 0)
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.GbRecordingSession{ChannelID: c.ID, NodeID: 3, VHost: "v", App: "rtp", Stream: "old", State: models.RecordingSessionStateStopped}).Error)
	require.NoError(t, s.RecoverLegacy(ctx))
	after, err := s.Get(ctx, before.ResourceKey)
	require.NoError(t, err)
	require.Equal(t, before.OwnerKind, after.OwnerKind)
	require.Equal(t, before.OwnerID, after.OwnerID)
	require.Equal(t, before.Version, after.Version)
	var count int64
	require.NoError(t, db.Model(&models.GbRecorderClaim{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestRecoverLegacyFailureRollsBackAllQuarantineWrites(t *testing.T) {
	db := claimDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}, &models.GbRecordingSession{}))
	c := models.GbChannel{DeviceID: "D", ChannelID: "C", CloudRecordingEnabled: true}
	require.NoError(t, db.Create(&c).Error)
	require.NoError(t, db.Create(&models.GbRecordingSession{ChannelID: c.ID, NodeID: 3, VHost: "v", App: "rtp", Stream: "s", State: models.RecordingSessionStateRecording}).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER reject_media_claim BEFORE INSERT ON gb_recorder_claim WHEN NEW.stream = 's' BEGIN SELECT RAISE(ABORT, 'database write unavailable'); END").Error)
	require.Error(t, NewClaims(db).RecoverLegacy(context.Background()))
	var count int64
	require.NoError(t, db.Model(&models.GbRecorderClaim{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestRecoverLegacyReusesTombstoneWithoutResettingVersion(t *testing.T) {
	db := claimDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}, &models.GbRecordingSession{}))
	ctx := context.Background()
	claims := NewClaims(db)
	channel := models.GbChannel{DeviceID: "d", ChannelID: "c", CloudRecordingEnabled: true}
	require.NoError(t, db.Create(&channel).Error)
	owner := Owner{Kind: OwnerPlan, ID: "old"}
	row, err := claims.Acquire(ctx, ChannelResource(channel.ID), owner, 0)
	require.NoError(t, err)
	row, err = claims.Transition(ctx, row.ResourceKey, owner, row.Version, StateStopping)
	require.NoError(t, err)
	row, err = claims.Transition(ctx, row.ResourceKey, owner, row.Version, StateStopped)
	require.NoError(t, err)
	require.NoError(t, claims.Release(ctx, row.ResourceKey, owner, row.Version))
	require.NoError(t, claims.RecoverLegacy(ctx))
	restored, err := claims.Get(ctx, row.ResourceKey)
	require.NoError(t, err)
	require.Greater(t, restored.Version, row.Version)
	require.Equal(t, OwnerLegacy, restored.OwnerKind)
	require.Equal(t, StateUnknown, restored.State)
	version := restored.Version
	require.NoError(t, claims.RecoverLegacy(ctx))
	restored, err = claims.Get(ctx, row.ResourceKey)
	require.NoError(t, err)
	require.Equal(t, version, restored.Version)
}
