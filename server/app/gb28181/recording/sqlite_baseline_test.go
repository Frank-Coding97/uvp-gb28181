package recording

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newRecordingSQLiteBaselineDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recording.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	return db, path
}

func TestGormRepoSQLiteBaselineTreatsMaskedEmptyAsMissing(t *testing.T) {
	db, _ := newRecordingSQLiteBaselineDB(t)
	repo := NewGormRepo(db)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	channel, err := repo.GetChannel(ctx, 999901)
	require.ErrorIs(t, err, ErrChannelNotFound)
	require.Nil(t, channel)
	channel, err = repo.FindChannelByStream(ctx, "missing-stream")
	require.NoError(t, err)
	require.Nil(t, channel)
	session, err := repo.FindSessionByMedia(ctx, 91, models.DefaultRecordingVHost, models.DefaultRecordingApp, "missing-stream")
	require.NoError(t, err)
	require.Nil(t, session)
}

func TestGormRepoSQLiteBaselineSessionNaturalKeyAndCatalogWindow(t *testing.T) {
	db, _ := newRecordingSQLiteBaselineDB(t)
	repo := NewGormRepo(db)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	started := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	session := &models.GbRecordingSession{
		ChannelID: 11, DeviceID: "recording-device", NodeID: 91,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: "recording-stream", State: models.RecordingSessionStateRecording, StartedAt: &started,
	}
	require.NoError(t, repo.UpsertSession(ctx, session))
	sessionID := session.ID
	session.ChannelID = 12
	session.State = models.RecordingSessionStateStopped
	require.NoError(t, repo.UpsertSession(ctx, session))
	require.Equal(t, sessionID, session.ID)
	var sessionCount int64
	require.NoError(t, db.Model(&models.GbRecordingSession{}).Where("node_id = ? AND vhost = ? AND app = ? AND stream = ?", 91, models.DefaultRecordingVHost, models.DefaultRecordingApp, "recording-stream").Count(&sessionCount).Error)
	require.EqualValues(t, 1, sessionCount)

	end := started.Add(2 * time.Hour)
	for _, file := range []models.GbRecordingFile{
		catalogFile(801, 1, 91, started, "at-start.mp4"),
		catalogFile(802, 1, 91, started.Add(time.Hour), "middle.mp4"),
		catalogFile(803, 1, 91, end, "at-end.mp4"),
	} {
		require.NoError(t, db.Create(&file).Error)
	}
	page, err := repo.ListCatalogFiles(ctx, FileQuery{FullAccess: true, Page: 1, PageSize: 1, Start: &started, End: &end})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.Len(t, page.Files, 1)
	require.Equal(t, "middle.mp4", page.Files[0].FileName)
	page, err = repo.ListCatalogFiles(ctx, FileQuery{FullAccess: true, Page: 2, PageSize: 1, Start: &started, End: &end})
	require.NoError(t, err)
	require.Len(t, page.Files, 1)
	require.Equal(t, "at-start.mp4", page.Files[0].FileName)
	page, err = repo.ListCatalogFiles(ctx, FileQuery{FullAccess: true, Page: 3, PageSize: 1, Start: &started, End: &end})
	require.NoError(t, err)
	require.Empty(t, page.Files)
	page, err = repo.ListCatalogFiles(ctx, FileQuery{FullAccess: true, Page: 1, PageSize: 20, Start: &end, End: &started})
	require.NoError(t, err)
	require.Zero(t, page.Total)
	require.Empty(t, page.Files)
}

func TestGormRepoSQLiteBaselineSessionUpsertSerializesMediaKey(t *testing.T) {
	db, path := newRecordingSQLiteBaselineDB(t)
	second, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	rawSecond, err := second.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawSecond.Close() })
	firstRepo, otherRepo := NewGormRepo(db), NewGormRepo(second)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Date(2026, 9, 7, 4, 0, 0, 0, time.UTC)
	startSignal := make(chan struct{})
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	for index, repo := range []*GormRepo{firstRepo, otherRepo} {
		wg.Add(1)
		go func(index int, repo *GormRepo) {
			defer wg.Done()
			<-startSignal
			errors <- repo.UpsertSession(ctx, &models.GbRecordingSession{
				ChannelID: uint(index + 21), DeviceID: "parallel-device", NodeID: 92,
				VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
				Stream: "parallel-stream", State: models.RecordingSessionStateRecording, StartedAt: &start,
			})
		}(index, repo)
	}
	close(startSignal)
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var count int64
	require.NoError(t, db.Model(&models.GbRecordingSession{}).Where("node_id = ? AND vhost = ? AND app = ? AND stream = ?", 92, models.DefaultRecordingVHost, models.DefaultRecordingApp, "parallel-stream").Count(&count).Error)
	require.EqualValues(t, 1, count)
}
