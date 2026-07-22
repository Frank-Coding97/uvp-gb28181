package recording

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestIndexRecordMP4AssociatesStoppedSessionAndIsIdempotent(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	now := time.Now().Add(-time.Minute).Truncate(time.Second)
	session := &models.GbRecordingSession{
		ChannelID: 7, DeviceID: "device-7", NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: "stream-7", State: models.RecordingSessionStateRecording, StartedAt: &now,
	}
	require.NoError(t, repo.UpsertSession(context.Background(), session))
	require.NoError(t, repo.MarkSessionStopped(context.Background(), session.ID, ""))
	indexer := NewFileIndexer(repo)
	event := RecordMP4Event{
		VHost: session.VHost, App: session.App, Stream: session.Stream,
		FileName: "2026-07-22-17-00-00.mp4", FilePath: "/record/stream-7/file.mp4",
		Folder: "/record/stream-7", URL: "record/stream-7/file.mp4",
		StartTime: now, TimeLen: 60.5, FileSize: 1024,
	}

	indexed, err := indexer.IndexRecordMP4(context.Background(), 2, event)
	require.NoError(t, err)
	require.True(t, indexed)
	indexed, err = indexer.IndexRecordMP4(context.Background(), 2, event)
	require.NoError(t, err)
	require.False(t, indexed)

	var files []models.GbRecordingFile
	require.NoError(t, db.Find(&files).Error)
	require.Len(t, files, 1)
	require.Equal(t, session.ID, *files[0].SessionID)
	require.EqualValues(t, 7, files[0].ChannelID)
	require.Equal(t, "device-7", files[0].DeviceID)
}

func TestIndexRecordMP4IgnoresUnknownSession(t *testing.T) {
	indexer := NewFileIndexer(NewGormRepo(newRepoTestDB(t)))
	indexed, err := indexer.IndexRecordMP4(context.Background(), 2, RecordMP4Event{
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: "unknown", FilePath: "/record/unknown.mp4", StartTime: time.Now(),
	})
	require.NoError(t, err)
	require.False(t, indexed)
}
