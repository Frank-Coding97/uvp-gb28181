package repository

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newCascadeSQLiteBaselineDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cascade.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	result, err := sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	require.NotEmpty(t, result.Version)
	require.NotEmpty(t, result.Checksum)
	require.NoError(t, sqlitebootstrap.Migrate(context.Background(), db))
	return db, path
}

func TestGormRepositorySQLiteBaselineProjectionAndMediaLifecycle(t *testing.T) {
	db, _ := newCascadeSQLiteBaselineDB(t)
	repo := NewGormRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	platform := newPlatform("sqlite-upstream-a", "34020000001320000001")
	otherPlatform := newPlatform("sqlite-upstream-b", "34020000001320000002")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	require.NoError(t, repo.CreatePlatform(ctx, otherPlatform))
	loaded, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.Equal(t, platform.Name, loaded.Name)
	_, err = repo.FindPlatform(ctx, 999991)
	require.ErrorIs(t, err, ErrPlatformNotFound)

	registeredAt := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	require.NoError(t, repo.RecordRegistrationSuccess(ctx, platform.ID, registeredAt, registeredAt.Add(time.Hour)))
	loaded, err = repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded.RegisterAt)
	require.True(t, registeredAt.Equal(*loaded.RegisterAt))

	devices := []DeviceProjectionInput{{
		SourceDeviceID: 100, PublishedDeviceID: "34020000001320000011", Name: "device-a",
	}}
	channels := []ChannelProjectionInput{{
		SourceDeviceID: 100, SourceChannelID: 101, PublishedChannelID: "34020000001320000021", Name: "channel-a", PTZAllowed: true,
	}}
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0, devices, channels))
	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Revision)
	require.Len(t, snapshot.Devices, 1)
	require.Len(t, snapshot.Channels, 1)
	require.True(t, snapshot.Channels[0].PTZAllowed)

	devices[0].Name = "device-a-updated"
	channels[0].Name = "channel-a-updated"
	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, snapshot.Revision, devices, channels))
	updatedSnapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, updatedSnapshot.Revision)
	require.Equal(t, "device-a-updated", updatedSnapshot.Devices[0].Name)
	require.Equal(t, "channel-a-updated", updatedSnapshot.Channels[0].Name)
	otherSnapshot, err := repo.ProjectionSnapshot(ctx, otherPlatform.ID)
	require.NoError(t, err)
	require.Empty(t, otherSnapshot.Devices)
	require.Empty(t, otherSnapshot.Channels)

	now := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	media := &model.GbCascadeMediaSession{
		PlatformID: platform.ID, DialogKey: "sqlite-dialog-a", CallID: "sqlite-call-a",
		SourceDeviceID: 100, SourceChannelID: 101, PublishedChannelID: channels[0].PublishedChannelID,
		State: model.CascadeMediaSessionStateReceived, ReceivedAt: &now,
	}
	require.NoError(t, repo.CreateMediaSession(ctx, media))
	duplicate := *media
	duplicate.ID = 0
	require.Error(t, repo.CreateMediaSession(ctx, &duplicate))
	unfinished, err := repo.ListNonterminalMediaSessions(ctx, platform.ID)
	require.NoError(t, err)
	require.Len(t, unfinished, 1)

	for index, state := range []model.CascadeMediaSessionState{
		model.CascadeMediaSessionStateProvisioning,
		model.CascadeMediaSessionStateAnswered,
		model.CascadeMediaSessionStateActive,
		model.CascadeMediaSessionStateClosing,
		model.CascadeMediaSessionStateClosed,
	} {
		changed, transitionErr := repo.TransitionMediaSession(ctx, media.DialogKey, media.State, state, now.Add(time.Duration(index+1)*time.Second))
		require.NoError(t, transitionErr)
		require.True(t, changed)
		media.State = state
	}
	changed, err := repo.TransitionMediaSession(ctx, media.DialogKey, model.CascadeMediaSessionStateClosed, model.CascadeMediaSessionStateFailed, now.Add(time.Minute))
	require.ErrorIs(t, err, ErrInvalidMediaTransition)
	require.False(t, changed)
	unfinished, err = repo.ListNonterminalMediaSessions(ctx, platform.ID)
	require.NoError(t, err)
	require.Empty(t, unfinished)
	stored, err := repo.FindMediaSessionByDialog(ctx, media.DialogKey)
	require.NoError(t, err)
	require.Equal(t, model.CascadeMediaSessionStateClosed, stored.State)
	require.NotNil(t, stored.ClosedAt)
	require.True(t, now.Add(5*time.Second).Equal(*stored.ClosedAt))

	require.NoError(t, db.Exec(`
CREATE TRIGGER fail_cascade_media_insert
BEFORE INSERT ON gb_cascade_media_session
WHEN NEW.dialog_key = 'sqlite-trigger-failure'
BEGIN
  SELECT RAISE(ABORT, 'injected cascade media insert failure');
END`).Error)
	failing := &model.GbCascadeMediaSession{
		PlatformID: platform.ID, DialogKey: "sqlite-trigger-failure", CallID: "sqlite-call-failure",
		SourceDeviceID: 100, SourceChannelID: 101, PublishedChannelID: channels[0].PublishedChannelID,
	}
	require.Error(t, repo.CreateMediaSession(ctx, failing))
	require.NoError(t, db.Exec("DROP TRIGGER fail_cascade_media_insert").Error)
	require.NoError(t, repo.CreateMediaSession(ctx, failing))

	require.NoError(t, repo.SoftDeletePlatform(ctx, platform.ID))
	_, err = repo.FindPlatform(ctx, platform.ID)
	require.ErrorIs(t, err, ErrPlatformNotFound)
	deletedSnapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.ErrorIs(t, err, ErrPlatformNotFound)
	require.Nil(t, deletedSnapshot)
}

func TestGormRepositorySQLiteBaselineProjectionCASIsSerializedAcrossClients(t *testing.T) {
	db, path := newCascadeSQLiteBaselineDB(t)
	other, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	otherRaw, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = otherRaw.Close() })
	firstRepo, secondRepo := NewGormRepository(db), NewGormRepository(other)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	platform := newPlatform("sqlite-cas-upstream", "34020000001320000003")
	require.NoError(t, firstRepo.CreatePlatform(ctx, platform))

	start := make(chan struct{})
	type outcome struct{ err error }
	outcomes := make(chan outcome, 2)
	var wg sync.WaitGroup
	projections := []struct {
		deviceID, channelID uint64
		publishedDevice     string
		publishedChannel    string
	}{
		{201, 202, "34020000001320000201", "34020000001320000202"},
		{301, 302, "34020000001320000301", "34020000001320000302"},
	}
	for index, repo := range []*GormRepository{firstRepo, secondRepo} {
		wg.Add(1)
		go func(index int, repo *GormRepository) {
			defer wg.Done()
			<-start
			item := projections[index]
			outcomes <- outcome{err: repo.ReplaceProjection(ctx, platform.ID, 0,
				[]DeviceProjectionInput{{SourceDeviceID: item.deviceID, PublishedDeviceID: item.publishedDevice}},
				[]ChannelProjectionInput{{SourceDeviceID: item.deviceID, SourceChannelID: item.channelID, PublishedChannelID: item.publishedChannel}},
			)}
		}(index, repo)
	}
	close(start)
	wg.Wait()
	close(outcomes)

	successes := 0
	for result := range outcomes {
		if result.err == nil {
			successes++
			continue
		}
		require.True(t, errors.Is(result.err, ErrRevisionConflict), "unexpected projection CAS result: %v", result.err)
	}
	require.Equal(t, 1, successes)
	snapshot, err := firstRepo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Revision)
	require.Len(t, snapshot.Devices, 1)
	require.Len(t, snapshot.Channels, 1)
}
