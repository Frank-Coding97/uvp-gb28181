package recording

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}, &models.GbRecordingSession{}, &models.GbRecordingFile{}))
	return db
}

func TestGormRepoDesiredStateAndConditionalUpdate(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	channel := &models.GbChannel{DeviceID: "device", ChannelID: "channel"}
	require.NoError(t, db.Create(channel).Error)

	got, err := repo.SetDesired(context.Background(), channel.ID, true)
	require.NoError(t, err)
	require.True(t, got.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateStarting, got.CloudRecordingState)

	_, err = repo.SetDesired(context.Background(), channel.ID, false)
	require.NoError(t, err)
	updated, err := repo.MarkState(context.Background(), channel.ID, true, models.CloudRecordingStateRecording, "")
	require.NoError(t, err)
	require.False(t, updated)

	require.NoError(t, db.First(&got, channel.ID).Error)
	require.False(t, got.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateStopping, got.CloudRecordingState)
}

func TestGormRepoSessionAndFileIdempotency(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	now := time.Now()
	session := &models.GbRecordingSession{
		ChannelID: 1, DeviceID: "device", NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: "stream", State: models.RecordingSessionStateRecording, StartedAt: &now,
	}
	require.NoError(t, repo.UpsertSession(context.Background(), session))
	session.State = models.RecordingSessionStateFailed
	require.NoError(t, repo.UpsertSession(context.Background(), session))

	var sessionCount int64
	require.NoError(t, db.Model(&models.GbRecordingSession{}).Count(&sessionCount).Error)
	require.EqualValues(t, 1, sessionCount)

	require.NoError(t, repo.MarkSessionStopped(context.Background(), session.ID, ""))
	stored, err := repo.FindSessionByMedia(context.Background(), 2, models.DefaultRecordingVHost, models.DefaultRecordingApp, "stream")
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateStopped, stored.State)
	require.NotNil(t, stored.StoppedAt)

	file := &models.GbRecordingFile{
		SessionID: &stored.ID, ChannelID: 1, DeviceID: "device", NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp, Stream: "stream",
		FileName: "one.mp4", FilePath: "/record/one.mp4", StartTime: now,
	}
	inserted, err := repo.InsertFile(context.Background(), file)
	require.NoError(t, err)
	require.True(t, inserted)
	inserted, err = repo.InsertFile(context.Background(), file)
	require.NoError(t, err)
	require.False(t, inserted)
}

func TestGormRepoListsEnabledChannelsOnly(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	require.NoError(t, db.Create(&models.GbChannel{DeviceID: "d", ChannelID: "on", CloudRecordingEnabled: true}).Error)
	require.NoError(t, db.Create(&models.GbChannel{DeviceID: "d", ChannelID: "off", CloudRecordingEnabled: false}).Error)

	got, err := repo.ListEnabledChannels(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "on", got[0].ChannelID)
}
