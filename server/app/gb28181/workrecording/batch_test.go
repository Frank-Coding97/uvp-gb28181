package workrecording

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestBatchStartRequestAllowsOneToFourUniqueChannels(t *testing.T) {
	for _, ids := range [][]uint{{1}, {1, 2}, {1, 2, 3, 4}} {
		if err := (BatchStartRequest{ChannelIDs: ids, RequestID: "batch-1"}).Validate(); err != nil {
			t.Fatalf("ids %v: valid batch rejected: %v", ids, err)
		}
	}
	for _, ids := range [][]uint{{}, {1, 2, 2}, {0}, {1, 2, 3, 4, 5}} {
		if err := (BatchStartRequest{ChannelIDs: ids, RequestID: "batch-1"}).Validate(); err != ErrBatchInvalid {
			t.Fatalf("ids %v: expected ErrBatchInvalid, got %v", ids, err)
		}
	}
}

type batchJobFake struct {
	db          *gorm.DB
	failChannel uint
	mu          sync.Mutex
	starts      []uint
	stops       []string
}

func (f *batchJobFake) Start(_ context.Context, actor uint, request StartRequest, _ int) (Snapshot, error) {
	f.mu.Lock()
	f.starts = append(f.starts, request.ChannelID)
	f.mu.Unlock()
	job := models.GbWorkRecording{
		ID: uuid.NewString(), ChannelID: request.ChannelID, CreatedBy: actor, RequestID: request.RequestID,
		State: StateRecording, DesiredAction: DesiredActionStart, Version: 1, FileState: FilePending,
		FormState: FormDraft, SchemaVersion: 1, FormJSON: "{}",
	}
	if request.ChannelID == f.failChannel {
		job.State = StateFailed
		job.LastError = "camera start failed"
	}
	f.mu.Lock()
	err := f.db.Create(&job).Error
	f.mu.Unlock()
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := jobSnapshot(&job)
	if request.ChannelID == f.failChannel {
		return snapshot, errors.New("camera start failed")
	}
	return snapshot, nil
}

func (f *batchJobFake) Stop(_ context.Context, jobID string) (Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops = append(f.stops, jobID)
	now := time.Now().UTC()
	if err := f.db.Model(&models.GbWorkRecording{}).Where("id = ?", jobID).Updates(map[string]any{"state": StateStopped, "stopped_at": now}).Error; err != nil {
		return Snapshot{}, err
	}
	var job models.GbWorkRecording
	if err := f.db.First(&job, "id = ?", jobID).Error; err != nil {
		return Snapshot{}, err
	}
	return jobSnapshot(&job), nil
}

func batchServiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "batch.db")+"?_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecordingBatch{}, &models.GbWorkRecording{}, &models.GbRecordingFile{}, &models.GbWorkRecordingFile{}, &models.GbChannel{}))
	return db
}

func TestBatchServiceStartsOnlySelectedJobsAndIsIdempotent(t *testing.T) {
	db := batchServiceDB(t)
	jobs := &batchJobFake{db: db}
	service := NewBatchService(db, jobs)
	request := BatchStartRequest{ChannelIDs: []uint{4, 2}, RequestID: "batch-success"}

	started, err := service.Start(context.Background(), 100, request)
	require.NoError(t, err)
	require.Equal(t, StateRecording, started.State)
	require.Len(t, started.Cameras, 2)
	require.Equal(t, []uint{2, 4}, []uint{started.Cameras[0].ChannelID, started.Cameras[1].ChannelID})

	retried, err := service.Start(context.Background(), 100, request)
	require.NoError(t, err)
	require.Equal(t, started.ID, retried.ID)
	require.Len(t, jobs.starts, 2)
}

func TestBatchServiceRollsBackSuccessfulJobsAndKeepsFailedCameraInLedger(t *testing.T) {
	db := batchServiceDB(t)
	jobs := &batchJobFake{db: db, failChannel: 3}
	service := NewBatchService(db, jobs)

	snapshot, err := service.Start(context.Background(), 100, BatchStartRequest{ChannelIDs: []uint{1, 2, 3, 4}, RequestID: "batch-failure"})
	require.ErrorIs(t, err, ErrBatchIncomplete)
	require.Equal(t, StateFailed, snapshot.State)
	require.Len(t, snapshot.Cameras, 4)
	require.Len(t, jobs.stops, 3)
	require.Equal(t, "camera start failed", snapshot.LastError)
}

func TestBatchSnapshotDoesNotReportMixedStoppedAndRecordingJobsAsStopped(t *testing.T) {
	db := batchServiceDB(t)
	batchID := uuid.NewString()
	require.NoError(t, db.Create(&models.GbWorkRecordingBatch{ID: batchID, CreatedBy: 100, RequestID: "mixed", State: StateRecording, Version: 1, FormState: FormDraft, SchemaVersion: 1, FormJSON: "{}"}).Error)
	for channelID, state := range map[uint]string{1: StateStopped, 2: StateRecording, 3: StateStopped, 4: StateStopped} {
		require.NoError(t, db.Create(&models.GbWorkRecording{ID: uuid.NewString(), BatchID: batchID, ChannelID: channelID, CreatedBy: 100, RequestID: uuid.NewString(), State: state, DesiredAction: DesiredActionStart, Version: 1, FormJSON: "{}"}).Error)
	}
	service := NewBatchService(db, &batchJobFake{db: db})
	snapshot, err := service.Get(context.Background(), batchID)
	require.NoError(t, err)
	require.Equal(t, StateRecording, snapshot.State)
}
