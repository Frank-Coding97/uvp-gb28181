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
	device := &models.GbDevice{DeviceID: "device-7", Name: "设备名称", Alias: "设备别名", OwnerDeptID: 9}
	require.NoError(t, db.Create(device).Error)
	channel := &models.GbChannel{DeviceID: device.DeviceID, ChannelID: "channel-7", Name: "通道名称", Alias: "通道别名", OwnerDeptID: 9}
	require.NoError(t, db.Create(channel).Error)
	session := &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: "device-7", NodeID: 2,
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
	require.True(t, indexed)

	var files []models.GbRecordingFile
	require.NoError(t, db.Find(&files).Error)
	require.Len(t, files, 1)
	require.Equal(t, session.ID, *files[0].SessionID)
	require.EqualValues(t, channel.ID, files[0].ChannelID)
	require.Equal(t, "device-7", files[0].DeviceID)
	require.Equal(t, "channel-7", files[0].ChannelCode)
	require.Equal(t, "通道别名", files[0].ChannelName)
	require.Equal(t, "设备别名", files[0].DeviceName)
	require.EqualValues(t, 9, files[0].OwnerDeptID)
	require.Equal(t, BuildFileKey(2, event.FilePath), files[0].FileKey)
	require.Equal(t, models.RecordingFileSourceHook, files[0].Source)
	require.Equal(t, models.RecordingMetadataComplete, files[0].MetadataState)
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

func TestIndexRecordMP4FallsBackToCurrentLocatedStream(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	device := &models.GbDevice{DeviceID: "device-current", Name: "当前设备", OwnerDeptID: 5}
	require.NoError(t, db.Create(device).Error)
	channel := &models.GbChannel{DeviceID: device.DeviceID, ChannelID: "channel-current", Name: "当前通道", OwnerDeptID: 5, StreamID: "stream-current"}
	require.NoError(t, db.Create(channel).Error)
	locations := fakeLocationLookup{"stream-current": 2}
	indexer := NewFileIndexer(repo, locations)
	now := time.Now().UTC()

	indexed, err := indexer.IndexRecordMP4(context.Background(), 2, RecordMP4Event{
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp, Stream: "stream-current",
		FileName: "current.mp4", FilePath: "/record/current.mp4", StartTime: now, TimeLen: 3, FileSize: 9,
	})
	require.NoError(t, err)
	require.True(t, indexed)
	var file models.GbRecordingFile
	require.NoError(t, db.Where("file_key = ?", BuildFileKey(2, "/record/current.mp4")).First(&file).Error)
	require.Equal(t, channel.ID, file.ChannelID)
}

func TestIndexRecordMP4CompletesPartialFileAndClearsMissing(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	now := time.Now().UTC().Truncate(time.Second)
	channel := &models.GbChannel{DeviceID: "device-partial", ChannelID: "channel-partial", Name: "partial", OwnerDeptID: 6}
	require.NoError(t, db.Create(channel).Error)
	session := &models.GbRecordingSession{ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 2, VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp, Stream: "stream-partial", State: models.RecordingSessionStateStopped}
	require.NoError(t, repo.UpsertSession(context.Background(), session))
	missingAt := now.Add(-time.Hour)
	partial := &models.GbRecordingFile{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 2, VHost: session.VHost, App: session.App, Stream: session.Stream,
		FileKey: BuildFileKey(2, "/record/partial.mp4"), FileName: "partial.mp4", FilePath: "/record/partial.mp4",
		Source: models.RecordingFileSourceReconcile, MetadataState: models.RecordingMetadataPartial,
		DiscoveredAt: now.Add(-2 * time.Hour), MissingAt: &missingAt, ReconcileMissCount: 2,
	}
	require.NoError(t, db.Create(partial).Error)

	indexed, err := NewFileIndexer(repo).IndexRecordMP4(context.Background(), 2, RecordMP4Event{
		VHost: session.VHost, App: session.App, Stream: session.Stream, FileName: partial.FileName, FilePath: partial.FilePath,
		StartTime: now, TimeLen: 60, FileSize: 4096,
	})
	require.NoError(t, err)
	require.True(t, indexed)
	var stored models.GbRecordingFile
	require.NoError(t, db.Where("file_key = ?", partial.FileKey).First(&stored).Error)
	require.Equal(t, partial.ID, stored.ID)
	require.Equal(t, models.RecordingMetadataComplete, stored.MetadataState)
	require.Equal(t, models.RecordingFileSourceHook, stored.Source)
	require.Nil(t, stored.MissingAt)
	require.Zero(t, stored.ReconcileMissCount)
	require.NotNil(t, stored.TimeLen)
	require.Equal(t, 60.0, *stored.TimeLen)
}

type fakeLocationLookup map[string]int64

func (f fakeLocationLookup) Lookup(stream string) (int64, bool) {
	id, ok := f[stream]
	return id, ok
}
