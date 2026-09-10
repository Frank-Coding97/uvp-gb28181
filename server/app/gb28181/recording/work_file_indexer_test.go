package recording

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
)

const (
	workIndexerJobA       = "8d9b6784-df9a-4486-84ce-3e6eedb18280"
	workIndexerJobB       = "4d1f6c2a-7e83-4a9b-9f10-2c0e5b6a7d81"
	workIndexerNode       = int64(17)
	workIndexerBusinessID = "12345678901234567890"
)

type countingRecordMP4Indexer struct {
	calls   atomic.Int32
	indexed bool
	err     error
}

func (i *countingRecordMP4Indexer) IndexRecordMP4(context.Context, int64, RecordMP4Event) (bool, error) {
	i.calls.Add(1)
	return i.indexed, i.err
}

func workIndexerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newRepoTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecording{}, &models.GbWorkRecordingFile{}))
	return db
}

func workIndexerFixture(t *testing.T, db *gorm.DB) (*models.GbChannel, models.GbWorkRecording, models.GbWorkRecording) {
	t.Helper()
	device := &models.GbDevice{DeviceID: "device-work-indexer", Alias: "设备别名", OwnerDeptID: 11}
	require.NoError(t, db.Create(device).Error)
	channel := &models.GbChannel{DeviceID: device.DeviceID, ChannelID: "channel-work-indexer", Alias: "通道别名", OwnerDeptID: 11}
	require.NoError(t, db.Create(channel).Error)
	now := time.Date(2026, 9, 9, 12, 42, 52, 0, time.UTC)
	jobA := models.GbWorkRecording{
		ID: workIndexerJobA, ChannelID: channel.ID, CreatedBy: 1, RequestID: "request-" + workIndexerJobA,
		State: workrecording.StateStopped, DesiredAction: workrecording.DesiredActionStop, Version: 3, NodeID: workIndexerNode,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp, Stream: "stream-work-indexer",
		RecordingRoot: "/opt/zlm/work-recordings/" + workIndexerJobA, Generation: 1,
		StartedAt: &now, StoppedAt: &now, FileState: workrecording.FileFinalizing, FormState: workrecording.FormSubmitted,
		// DeviceID on the work form is a customer business identifier. The
		// catalog must snapshot the actual GB device ID from the channel.
		SchemaVersion: 1, DeviceID: workIndexerBusinessID, FormJSON: "{}",
	}
	jobB := jobA
	jobB.ID = workIndexerJobB
	jobB.RequestID = "request-" + workIndexerJobB
	jobB.State = workrecording.StateRecording
	jobB.RecordingRoot = "/opt/zlm/work-recordings/" + workIndexerJobB
	jobB.StoppedAt = nil
	require.NoError(t, db.Create(&jobA).Error)
	require.NoError(t, db.Create(&jobB).Error)
	return channel, jobA, jobB
}

func workIndexerEvent(job models.GbWorkRecording, name string) RecordMP4Event {
	path := filepath.Join(job.RecordingRoot, "record", job.App, job.Stream, "2026-09-09", name)
	return RecordMP4Event{
		VHost: job.VHost, App: job.App, Stream: job.Stream,
		FileName: name, FilePath: path, Folder: filepath.Dir(path), URL: "record/" + name,
		StartTime: time.Date(2026, 9, 9, 12, 42, 52, 0, time.UTC), TimeLen: 2.5, FileSize: 1024,
	}
}

func TestWorkFileIndexerAssociatesDelayedJobsByExactDirectoryAndIsIdempotent(t *testing.T) {
	db := workIndexerDB(t)
	channel, jobA, jobB := workIndexerFixture(t, db)
	require.NotEqual(t, channel.DeviceID, jobA.DeviceID)
	legacy := &countingRecordMP4Indexer{indexed: true}
	indexer := NewWorkFileIndexer(db, legacy)
	eventA := workIndexerEvent(jobA, "2026-09-09-12-42-52-0.mp4")
	eventB := workIndexerEvent(jobB, "2026-09-09-12-42-52-0.mp4")

	// B is already recording when the delayed A callback arrives. Replaying
	// the callbacks in reverse order must still use each job's directory.
	for _, event := range []RecordMP4Event{eventB, eventA, eventA, eventB} {
		indexed, err := indexer.IndexRecordMP4(context.Background(), workIndexerNode, event)
		require.NoError(t, err)
		require.True(t, indexed)
	}

	var files []models.GbRecordingFile
	require.NoError(t, db.Order("file_path").Find(&files).Error)
	require.Len(t, files, 2)
	var links []models.GbWorkRecordingFile
	require.NoError(t, db.Order("work_recording_id").Find(&links).Error)
	require.Len(t, links, 2)
	for _, file := range files {
		var link models.GbWorkRecordingFile
		require.NoError(t, db.Where("file_id = ?", file.ID).First(&link).Error)
		switch file.FilePath {
		case eventA.FilePath:
			require.Equal(t, jobA.ID, link.WorkRecordingID)
		case eventB.FilePath:
			require.Equal(t, jobB.ID, link.WorkRecordingID)
		default:
			t.Fatalf("unexpected work file path %q", file.FilePath)
		}
		require.Equal(t, workIndexerNode, file.NodeID)
		require.Equal(t, channel.DeviceID, file.DeviceID)
		require.Equal(t, models.RecordingFileSourceHook, file.Source)
		require.Equal(t, models.RecordingMetadataComplete, file.MetadataState)
		require.Equal(t, "isolated-directory:"+link.WorkRecordingID, link.Evidence)
		require.LessOrEqual(t, len(link.Evidence), 500)
	}
	require.Zero(t, legacy.calls.Load(), "isolated work paths must not fall back to legacy session attribution")
}

func TestWorkFileIndexerBoundsEvidenceForLongRecordingPath(t *testing.T) {
	db := workIndexerDB(t)
	_, jobA, _ := workIndexerFixture(t, db)
	indexer := NewWorkFileIndexer(db, &countingRecordMP4Indexer{indexed: true})
	event := workIndexerEvent(jobA, "long-path.mp4")
	event.FilePath = filepath.Join(jobA.RecordingRoot, strings.Repeat("a", 620), event.FileName)
	event.Folder = filepath.Dir(event.FilePath)

	indexed, err := indexer.IndexRecordMP4(context.Background(), workIndexerNode, event)
	require.NoError(t, err)
	require.True(t, indexed)
	require.Greater(t, len(event.FilePath), 500)

	var file models.GbRecordingFile
	require.NoError(t, db.Where("file_path = ?", event.FilePath).First(&file).Error)
	var link models.GbWorkRecordingFile
	require.NoError(t, db.Where("file_id = ?", file.ID).First(&link).Error)
	require.Equal(t, "isolated-directory:"+jobA.ID, link.Evidence)
	require.LessOrEqual(t, len(link.Evidence), 500)
}

func TestWorkFileIndexerDoesNotFallbackUnknownWorkPathButDelegatesLegacyPath(t *testing.T) {
	db := workIndexerDB(t)
	channel, _, jobB := workIndexerFixture(t, db)
	repo := NewGormRepo(db)
	now := time.Now().UTC()
	session := &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: workIndexerNode,
		VHost: jobB.VHost, App: jobB.App, Stream: jobB.Stream,
		State: models.RecordingSessionStateStopped, StartedAt: &now,
	}
	require.NoError(t, repo.UpsertSession(context.Background(), session))
	legacy := NewFileIndexer(repo)
	indexer := NewWorkFileIndexer(db, legacy)

	unknownWork := workIndexerEvent(jobB, "unknown.mp4")
	unknownWork.FilePath = "/opt/zlm/work-recordings/" + "c1a2b3c4-d5e6-4789-8abc-def012345678" + "/record/rtp/" + jobB.Stream + "/unknown.mp4"
	indexed, err := indexer.IndexRecordMP4(context.Background(), workIndexerNode, unknownWork)
	require.False(t, indexed)
	require.ErrorIs(t, err, workrecording.ErrAttributionUnknown)
	require.Zero(t, legacyFileCount(t, db))

	legacyEvent := unknownWork
	legacyEvent.FilePath = "/record/" + jobB.Stream + "/legacy.mp4"
	legacyEvent.Folder = "/record/" + jobB.Stream
	indexed, err = indexer.IndexRecordMP4(context.Background(), workIndexerNode, legacyEvent)
	require.NoError(t, err)
	require.True(t, indexed)
	require.EqualValues(t, 1, legacyFileCount(t, db))
}

func TestWorkFileIndexerTransactionRollsBackFileWhenWorkLinkFails(t *testing.T) {
	db := workIndexerDB(t)
	_, jobA, _ := workIndexerFixture(t, db)
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_work_recording_file_link BEFORE INSERT ON gb_work_recording_file BEGIN SELECT RAISE(ABORT, 'work link rejected'); END`).Error)
	indexer := NewWorkFileIndexer(db, &countingRecordMP4Indexer{indexed: true})

	indexed, err := indexer.IndexRecordMP4(context.Background(), workIndexerNode, workIndexerEvent(jobA, "rollback.mp4"))
	require.False(t, indexed)
	require.Error(t, err)
	require.Zero(t, legacyFileCount(t, db))
	var links int64
	require.NoError(t, db.Model(&models.GbWorkRecordingFile{}).Count(&links).Error)
	require.Zero(t, links)
}

func legacyFileCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&models.GbRecordingFile{}).Count(&count).Error)
	return count
}
