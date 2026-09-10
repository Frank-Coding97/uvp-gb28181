package workrecording

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// BatchService keeps the one-to-four-camera ledger separate from the existing
// per-channel recorder lifecycle. A batch is durable, while each child job
// still gets its own ZLM directory and file hooks.
type BatchService struct {
	db   *gorm.DB
	jobs BatchJobService
	now  func() time.Time
}

type BatchJobService interface {
	Start(context.Context, uint, StartRequest, int) (Snapshot, error)
	Stop(context.Context, string) (Snapshot, error)
}

func NewBatchService(db *gorm.DB, jobs BatchJobService) *BatchService {
	return &BatchService{db: db, jobs: jobs, now: time.Now}
}

func (s *BatchService) Start(ctx context.Context, actor uint, request BatchStartRequest) (BatchSnapshot, error) {
	var empty BatchSnapshot
	if s == nil || s.db == nil || s.jobs == nil || actor == 0 {
		return empty, ErrBatchInvalid
	}
	if err := request.Validate(); err != nil {
		return empty, err
	}
	var existing models.GbWorkRecordingBatch
	result := s.db.WithContext(ctx).Where("created_by = ? AND request_id = ?", actor, request.RequestID).First(&existing)
	if result.Error == nil && result.RowsAffected > 0 {
		return s.snapshot(ctx, existing.ID)
	}
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return empty, result.Error
	}
	batch := &models.GbWorkRecordingBatch{
		ID: uuid.NewString(), CreatedBy: actor, RequestID: request.RequestID,
		State: StateStarting, Version: 1, FormState: FormDraft, SchemaVersion: 1,
		FormJSON: "{}",
	}
	if err := s.db.WithContext(ctx).Create(batch).Error; err != nil {
		if retry := s.db.WithContext(ctx).Where("created_by = ? AND request_id = ?", actor, request.RequestID).First(&existing); retry.Error == nil && retry.RowsAffected > 0 {
			return s.snapshot(ctx, existing.ID)
		}
		return empty, err
	}

	type startResult struct {
		id  string
		err error
	}
	results := make(chan startResult, len(request.ChannelIDs))
	var wg sync.WaitGroup
	for _, channelID := range request.ChannelIDs {
		channelID := channelID
		wg.Add(1)
		go func() {
			defer wg.Done()
			childRequest := StartRequest{ChannelID: channelID, RequestID: fmt.Sprintf("%s/%d", request.RequestID, channelID)}
			snapshot, err := s.jobs.Start(ctx, actor, childRequest, 0)
			results <- startResult{id: snapshot.ID, err: err}
		}()
	}
	wg.Wait()
	close(results)
	started := make([]string, 0, len(request.ChannelIDs))
	var startErr error
	for result := range results {
		if result.id != "" {
			if err := s.db.WithContext(ctx).Model(&models.GbWorkRecording{}).Where("id = ?", result.id).Update("batch_id", batch.ID).Error; err != nil {
				startErr = err
			}
		}
		if result.err != nil {
			startErr = result.err
			continue
		}
		started = append(started, result.id)
	}
	if startErr != nil || len(started) != len(request.ChannelIDs) {
		for _, jobID := range started {
			_, _ = s.jobs.Stop(ctx, jobID)
		}
		message := "录像批次启动不完整"
		if startErr != nil {
			message = startErr.Error()
		}
		_ = s.db.WithContext(ctx).Model(batch).Updates(map[string]any{"state": StateFailed, "last_error": message, "version": gorm.Expr("version + 1")}).Error
		snapshot, snapshotErr := s.snapshot(ctx, batch.ID)
		if snapshotErr != nil {
			return BatchSnapshot{}, snapshotErr
		}
		return snapshot, ErrBatchIncomplete
	}
	if err := s.db.WithContext(ctx).Model(batch).Updates(map[string]any{"state": StateRecording, "version": gorm.Expr("version + 1")}).Error; err != nil {
		return s.snapshot(ctx, batch.ID)
	}
	return s.snapshot(ctx, batch.ID)
}

func (s *BatchService) Stop(ctx context.Context, batchID string) (BatchSnapshot, error) {
	if s == nil || s.db == nil || s.jobs == nil || !validJobID(batchID) {
		return BatchSnapshot{}, ErrBatchInvalid
	}
	var batch models.GbWorkRecordingBatch
	result := s.db.WithContext(ctx).Where("id = ?", batchID).First(&batch)
	if result.Error != nil || result.RowsAffected == 0 {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) || result.RowsAffected == 0 {
			return BatchSnapshot{}, gorm.ErrRecordNotFound
		}
		return BatchSnapshot{}, result.Error
	}
	var jobs []models.GbWorkRecording
	if err := s.db.WithContext(ctx).Where("batch_id = ?", batchID).Order("channel_id ASC").Find(&jobs).Error; err != nil {
		return BatchSnapshot{}, err
	}
	for _, job := range jobs {
		if job.State == StateRecording || job.State == StateUnknown || job.State == StateStarting {
			if _, err := s.jobs.Stop(ctx, job.ID); err != nil && !errors.Is(err, ErrAttributionUnknown) {
				snapshot, snapshotErr := s.snapshot(ctx, batchID)
				if snapshotErr != nil {
					return BatchSnapshot{}, snapshotErr
				}
				return snapshot, err
			}
		}
	}
	return s.snapshot(ctx, batchID)
}

func (s *BatchService) Get(ctx context.Context, batchID string) (BatchSnapshot, error) {
	if s == nil || s.db == nil || !validJobID(batchID) {
		return BatchSnapshot{}, ErrBatchInvalid
	}
	return s.snapshot(ctx, batchID)
}

func (s *BatchService) List(ctx context.Context, actor uint, page, pageSize int) ([]BatchSnapshot, int64, error) {
	if s == nil || s.db == nil || actor == 0 || page < 1 || pageSize < 1 || pageSize > 50 {
		return nil, 0, ErrBatchInvalid
	}
	query := s.db.WithContext(ctx).Model(&models.GbWorkRecordingBatch{}).Where("created_by = ?", actor)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var batches []models.GbWorkRecordingBatch
	if err := query.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&batches).Error; err != nil {
		return nil, 0, err
	}
	items := make([]BatchSnapshot, 0, len(batches))
	for _, batch := range batches {
		item, err := s.snapshot(ctx, batch.ID)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, nil
}

func (s *BatchService) snapshot(ctx context.Context, batchID string) (BatchSnapshot, error) {
	var batch models.GbWorkRecordingBatch
	result := s.db.WithContext(ctx).Where("id = ?", batchID).First(&batch)
	if result.Error != nil {
		return BatchSnapshot{}, result.Error
	}
	if result.RowsAffected == 0 {
		return BatchSnapshot{}, gorm.ErrRecordNotFound
	}
	var jobs []models.GbWorkRecording
	if err := s.db.WithContext(ctx).Where("batch_id = ?", batchID).Order("channel_id ASC").Find(&jobs).Error; err != nil {
		return BatchSnapshot{}, err
	}
	channelNames := make(map[uint]string, len(jobs))
	if len(jobs) > 0 {
		var channels []models.GbChannel
		if err := s.db.WithContext(ctx).Select("id, name").Where("id IN ?", jobChannelIDs(jobs)).Find(&channels).Error; err != nil {
			return BatchSnapshot{}, err
		}
		for _, channel := range channels {
			channelNames[channel.ID] = channel.Name
		}
	}
	children := make([]BatchCameraSnapshot, 0, len(jobs))
	state := StateStopped
	for _, job := range jobs {
		camera := BatchCameraSnapshot{ChannelID: job.ChannelID, ChannelName: channelNames[job.ChannelID], JobID: job.ID, State: job.State, FileState: job.FileState, StartedAt: job.StartedAt, StoppedAt: job.StoppedAt, LastError: job.LastError, Files: []BatchFileSnapshot{}}
		var files []struct {
			ID            uint64
			ChannelID     uint
			FileName      string
			StartTime     *time.Time
			TimeLen       *float64
			FileSize      *uint64
			MetadataState string
			MissingAt     *time.Time
		}
		if err := s.db.WithContext(ctx).Table("gb_work_recording_file").Select("gb_recording_file.id, gb_recording_file.channel_id, gb_recording_file.file_name, gb_recording_file.start_time, gb_recording_file.time_len, gb_recording_file.file_size, gb_recording_file.metadata_state, gb_recording_file.missing_at").Joins("JOIN gb_recording_file ON gb_recording_file.id = gb_work_recording_file.file_id").Where("gb_work_recording_file.work_recording_id = ?", job.ID).Order("gb_recording_file.start_time ASC, gb_recording_file.id ASC").Find(&files).Error; err != nil {
			return BatchSnapshot{}, err
		}
		for _, file := range files {
			state := FileReady
			if file.MissingAt != nil || file.MetadataState != models.RecordingMetadataComplete {
				state = FileUnknown
			}
			camera.Files = append(camera.Files, BatchFileSnapshot{ID: file.ID, ChannelID: file.ChannelID, FileName: file.FileName, StartTime: file.StartTime, TimeLen: file.TimeLen, FileSize: file.FileSize, State: state})
		}
		children = append(children, camera)
		state = combineBatchState(state, job.State)
	}
	if len(jobs) == 0 && state == StateStopped {
		state = StateStarting
	}
	if batch.State == StateFailed || batch.State == StateUnknown {
		state = batch.State
	}
	if len(jobs) > 1 {
		sort.Slice(children, func(i, j int) bool { return children[i].ChannelID < children[j].ChannelID })
	}
	return BatchSnapshot{ID: batch.ID, RequestID: batch.RequestID, State: state, FormState: batch.FormState, FormVersion: batch.FormVersion, Cameras: children, LastError: strings.TrimSpace(batch.LastError)}, nil
}

func combineBatchState(current, child string) string {
	priority := func(state string) int {
		switch state {
		case StateUnknown:
			return 5
		case StateFailed:
			return 4
		case StateStarting, StateStopping:
			return 3
		case StateRecording:
			return 2
		default:
			return 1
		}
	}
	if priority(child) > priority(current) {
		return child
	}
	return current
}

func jobChannelIDs(jobs []models.GbWorkRecording) []uint {
	ids := make([]uint, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.ChannelID)
	}
	return ids
}
