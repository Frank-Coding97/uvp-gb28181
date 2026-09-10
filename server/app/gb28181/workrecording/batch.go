package workrecording

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// BatchService owns the work-order ledger: one durable row per work session,
// with one child GbWorkRecording per camera so each keeps its own ZLM directory
// and file hooks. The stored row is the single source of truth for the ledger
// state; readers never re-derive it.
type BatchService struct {
	db   *gorm.DB
	jobs BatchJobService
	now  func() time.Time
	// formHistoryFailureSink 让 bootstrap 注入真实 logger；
	// 默认 nil 走静默路径，避免录制链路因为日志接入不到位而崩。
	formHistoryFailureSink func(ctx context.Context, field string, err error)
}

type BatchJobService interface {
	Start(context.Context, uint, StartRequest, int) (Snapshot, error)
	Stop(context.Context, string) (Snapshot, error)
}

func NewBatchService(db *gorm.DB, jobs BatchJobService) *BatchService {
	return &BatchService{db: db, jobs: jobs, now: time.Now}
}

// StartOrder records the operator's work order and starts recording only after
// the form validates. Nothing is claimed on the recorder until the ledger row
// carrying that form exists.
func (s *BatchService) StartOrder(ctx context.Context, actor uint, request OrderStartRequest) (BatchSnapshot, error) {
	if err := request.Validate(); err != nil {
		return BatchSnapshot{}, err
	}
	return s.start(ctx, actor, BatchStartRequest{RequestID: request.RequestID, ChannelIDs: request.ChannelIDs}, request.Form, FormSubmitted)
}

func (s *BatchService) start(ctx context.Context, actor uint, request BatchStartRequest, form Form, formState string) (BatchSnapshot, error) {
	var empty BatchSnapshot
	if s == nil || s.db == nil || s.jobs == nil || actor == 0 {
		return empty, ErrBatchInvalid
	}
	if err := request.Validate(); err != nil {
		return empty, err
	}
	encoded, err := json.Marshal(form.normalized())
	if err != nil {
		return empty, ErrFormInvalid
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
		State: StateStarting, Version: 1, FormState: formState, FormVersion: 1,
		SchemaVersion: 1, FormJSON: string(encoded),
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
	ledger := make([]string, 0, len(request.ChannelIDs))
	// Keep the first failure: later goroutines finishing with their own error
	// must not overwrite the cause that actually stopped the work order.
	var startErr error
	for result := range results {
		if result.id != "" {
			// A camera that failed to start still belongs in the ledger, so the
			// operator can see which channel did not come up.
			ledger = append(ledger, result.id)
		}
		if result.err != nil {
			if startErr == nil {
				startErr = result.err
			}
			continue
		}
		started = append(started, result.id)
	}
	if startErr == nil && len(started) != len(request.ChannelIDs) {
		startErr = ErrBatchIncomplete
	}
	if attachErr := s.attachChildren(ctx, batch.ID, ledger); attachErr != nil && startErr == nil {
		startErr = attachErr
	}
	if startErr != nil {
		s.abortBatch(ctx, batch, started, startErr)
		snapshot, snapshotErr := s.snapshot(ctx, batch.ID)
		if snapshotErr != nil {
			return BatchSnapshot{}, snapshotErr
		}
		// Keep ErrBatchIncomplete in the chain so the HTTP layer still answers
		// 409, but carry the real cause so callers can log what actually failed.
		return snapshot, fmt.Errorf("%w: %w", ErrBatchIncomplete, startErr)
	}
	if err := s.db.WithContext(ctx).Model(batch).Updates(map[string]any{"state": StateRecording, "version": gorm.Expr("version + 1")}).Error; err != nil {
		return s.snapshot(ctx, batch.ID)
	}
	// 历史值是辅助能力，写失败不回滚作业单；upsertFormHistory 内部已 swallow。
	s.recordFormHistory(ctx, form)
	return s.snapshot(ctx, batch.ID)
}

// attachChildren links every started child to the ledger in one statement. A
// partially linked batch would report a camera count that disagrees with the
// recordings actually running, so a short update rolls the whole order back.
func (s *BatchService) attachChildren(ctx context.Context, batchID string, jobIDs []string) error {
	if len(jobIDs) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.GbWorkRecording{}).Where("id IN ?", jobIDs).Update("batch_id", batchID)
		if result.Error != nil {
			return result.Error
		}
		if int(result.RowsAffected) != len(jobIDs) {
			return ErrBatchIncomplete
		}
		return nil
	})
}

// abortBatch stops whatever started and marks the ledger failed. Recorder
// claims live outside the database transaction, so they are compensated rather
// than rolled back.
func (s *BatchService) abortBatch(ctx context.Context, batch *models.GbWorkRecordingBatch, started []string, cause error) {
	for _, jobID := range started {
		_, _ = s.jobs.Stop(ctx, jobID)
	}
	message := errorText(cause)
	if strings.TrimSpace(message) == "" {
		message = ErrBatchIncomplete.Error()
	}
	_ = s.db.WithContext(ctx).Model(batch).Updates(map[string]any{"state": StateFailed, "last_error": message, "version": gorm.Expr("version + 1")}).Error
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
	if err := s.ReconcileState(ctx, batchID); err != nil {
		return BatchSnapshot{}, err
	}
	return s.snapshot(ctx, batchID)
}

func (s *BatchService) Get(ctx context.Context, batchID string) (BatchSnapshot, error) {
	if s == nil || s.db == nil || !validJobID(batchID) {
		return BatchSnapshot{}, ErrBatchInvalid
	}
	return s.snapshot(ctx, batchID)
}

// BatchListFilter narrows a ledger query. Every field is optional except Actor,
// which is what keeps one operator's work orders out of another operator's list.
type BatchListFilter struct {
	Actor     uint
	Page      int
	PageSize  int
	States    []string
	Keyword   string
	ChannelID uint
	From      *time.Time
	To        *time.Time
}

func (f BatchListFilter) validate() error {
	if f.Actor == 0 || f.Page < 1 || f.PageSize < 1 || f.PageSize > 50 {
		return ErrBatchInvalid
	}
	for _, state := range f.States {
		if !knownState(state) {
			return ErrBatchInvalid
		}
	}
	return nil
}

func (s *BatchService) List(ctx context.Context, filter BatchListFilter) ([]BatchSnapshot, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, ErrBatchInvalid
	}
	if err := filter.validate(); err != nil {
		return nil, 0, err
	}
	// Build the conditions twice on purpose: reusing one *gorm.DB for both Count
	// and Find lets the count projection leak into the page query.
	scoped := func() *gorm.DB {
		query := s.db.WithContext(ctx).Model(&models.GbWorkRecordingBatch{}).
			Where("gb_work_recording_batch.created_by = ?", filter.Actor)
		if len(filter.States) > 0 {
			query = query.Where("gb_work_recording_batch.state IN ?", filter.States)
		}
		if filter.From != nil {
			query = query.Where("gb_work_recording_batch.created_at >= ?", *filter.From)
		}
		if filter.To != nil {
			query = query.Where("gb_work_recording_batch.created_at <= ?", *filter.To)
		}
		if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
			// The project name lives inside the form JSON, so the operator's own
			// project label is matched as text instead of as a column.
			like := "%" + keyword + "%"
			query = query.Where("gb_work_recording_batch.id LIKE ? OR gb_work_recording_batch.form_json LIKE ?", like, like)
		}
		if filter.ChannelID > 0 {
			query = query.Where("EXISTS (SELECT 1 FROM gb_work_recording wr WHERE wr.batch_id = gb_work_recording_batch.id AND wr.channel_id = ?)", filter.ChannelID)
		}
		return query
	}
	var total int64
	if err := scoped().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var batches []models.GbWorkRecordingBatch
	if err := scoped().Order("gb_work_recording_batch.created_at DESC, gb_work_recording_batch.id DESC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&batches).Error; err != nil {
		return nil, 0, err
	}
	items, err := s.buildSnapshots(ctx, batches)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Delete removes settled work orders and everything hanging off them. A ledger
// that is still recording is skipped instead of failed: the operator asked to
// remove rows, and silently refusing the whole batch because one of them is
// live would be worse than reporting which ones stayed.
//
// Only rows created by the actor are visible, matching every other entry point.
// The archived GbRecordingFile rows are deliberately kept: they are the index
// of files that still exist on the media node, and dropping the index would
// orphan the recordings with nothing left to find or clean them by.
func (s *BatchService) Delete(ctx context.Context, actor uint, request BatchDeleteRequest) (BatchDeleteResult, error) {
	result := BatchDeleteResult{}
	if s == nil || s.db == nil || actor == 0 {
		return result, ErrBatchInvalid
	}
	if err := request.Validate(); err != nil {
		return result, err
	}
	var batches []models.GbWorkRecordingBatch
	if err := s.db.WithContext(ctx).Where("id IN ? AND created_by = ?", request.IDs, actor).Find(&batches).Error; err != nil {
		return result, err
	}
	found := make(map[string]struct{}, len(batches))
	deletable := make([]string, 0, len(batches))
	for _, batch := range batches {
		found[batch.ID] = struct{}{}
		if batchRecordingActive(batch.State) {
			result.Skipped = append(result.Skipped, batch.ID)
			continue
		}
		deletable = append(deletable, batch.ID)
	}
	for _, id := range request.IDs {
		if _, ok := found[id]; !ok {
			result.Missing = append(result.Missing, id)
		}
	}
	if len(deletable) == 0 {
		return result, nil
	}
	var jobIDs []string
	if err := s.db.WithContext(ctx).Model(&models.GbWorkRecording{}).
		Where("batch_id IN ?", deletable).Pluck("id", &jobIDs).Error; err != nil {
		return result, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(jobIDs) > 0 {
			if err := tx.Where("work_recording_id IN ?", jobIDs).Delete(&models.GbWorkRecordingFile{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id IN ?", jobIDs).Delete(&models.GbWorkRecording{}).Error; err != nil {
				return err
			}
		}
		deleted := tx.Where("id IN ?", deletable).Delete(&models.GbWorkRecordingBatch{})
		if deleted.Error != nil {
			return deleted.Error
		}
		result.Deleted = uint64(deleted.RowsAffected)
		return nil
	})
	if err != nil {
		return BatchDeleteResult{}, err
	}
	return result, nil
}

// batchRecordingActive mirrors the frontend's notion of an unfinished work
// order, so the list never offers a delete the service would refuse.
func batchRecordingActive(state string) bool {
	switch state {
	case StateStarting, StateRecording, StateStopping, StateUnknown:
		return true
	default:
		return false
	}
}

// Active returns the newest ledger that has not settled yet, which is what the
// multi-screen page restores its recording button from after a reload. It
// returns gorm.ErrRecordNotFound when the operator has nothing running.
func (s *BatchService) Active(ctx context.Context, actor uint) (BatchSnapshot, error) {
	items, _, err := s.List(ctx, BatchListFilter{
		Actor: actor, Page: 1, PageSize: 1,
		States: []string{StateStarting, StateRecording, StateStopping, StateUnknown},
	})
	if err != nil {
		return BatchSnapshot{}, err
	}
	if len(items) == 0 {
		return BatchSnapshot{}, gorm.ErrRecordNotFound
	}
	return items[0], nil
}

// ReconcileState folds the child recorder states into the ledger row, which is
// the single source of truth every reader and every list filter uses. Anything
// that can change a child calls it: the ledger's own stop, and the recorder
// engine's state observer.
func (s *BatchService) ReconcileState(ctx context.Context, batchID string) error {
	if s == nil || s.db == nil || !validJobID(batchID) {
		return ErrBatchInvalid
	}
	var batch models.GbWorkRecordingBatch
	result := s.db.WithContext(ctx).Select("id", "state", "version").Where("id = ?", batchID).First(&batch)
	if result.Error != nil || result.RowsAffected == 0 {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) || (result.Error == nil && result.RowsAffected == 0) {
			return gorm.ErrRecordNotFound
		}
		return result.Error
	}
	var states []string
	if err := s.db.WithContext(ctx).Model(&models.GbWorkRecording{}).Where("batch_id = ?", batchID).Pluck("state", &states).Error; err != nil {
		return err
	}
	next := foldChildStates(states)
	if next == "" || next == batch.State {
		return nil
	}
	return s.db.WithContext(ctx).Model(&models.GbWorkRecordingBatch{}).
		Where("id = ? AND version = ?", batchID, batch.Version).
		Updates(map[string]any{"state": next, "version": gorm.Expr("version + 1")}).Error
}

// ObserveJobState adapts ReconcileState to the recorder engine's observer shape.
// Reconciliation is best-effort: a ledger that cannot refresh must never fail
// the recording that triggered the notification.
func (s *BatchService) ObserveJobState(ctx context.Context, batchID string) {
	_ = s.ReconcileState(ctx, batchID)
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
	items, err := s.buildSnapshots(ctx, []models.GbWorkRecordingBatch{batch})
	if err != nil {
		return BatchSnapshot{}, err
	}
	if len(items) == 0 {
		return BatchSnapshot{}, gorm.ErrRecordNotFound
	}
	return items[0], nil
}

// buildSnapshots assembles ledger rows in a fixed number of queries no matter
// how many cameras or slices they hold. The previous implementation called
// snapshot once per row and issued one slice query per camera, so a ten-row page
// with four cameras each cost dozens of round-trips.
func (s *BatchService) buildSnapshots(ctx context.Context, batches []models.GbWorkRecordingBatch) ([]BatchSnapshot, error) {
	items := make([]BatchSnapshot, 0, len(batches))
	if len(batches) == 0 {
		return items, nil
	}
	batchIDs := make([]string, 0, len(batches))
	for _, batch := range batches {
		batchIDs = append(batchIDs, batch.ID)
	}
	childrenByBatch, filesByJob, err := s.loadLedgerGraph(ctx, batchIDs)
	if err != nil {
		return nil, err
	}
	names, err := s.channelNames(ctx, childrenByBatch)
	if err != nil {
		return nil, err
	}
	for _, batch := range batches {
		children := childrenByBatch[batch.ID]
		cameras := make([]BatchCameraSnapshot, 0, len(children))
		for _, job := range children {
			files := filesByJob[job.ID]
			if files == nil {
				files = []BatchFileSnapshot{}
			}
			info := names[job.ChannelID]
			cameras = append(cameras, BatchCameraSnapshot{
				ChannelID: job.ChannelID, ChannelName: info.Name, ChannelCode: info.Code,
				DeviceID: info.DeviceID, DeviceName: info.DeviceName,
				JobID: job.ID,
				State: job.State, FileState: job.FileState,
				StartedAt: job.StartedAt, StoppedAt: job.StoppedAt, LastError: job.LastError,
				Files: files,
			})
		}
		items = append(items, BatchSnapshot{
			ID: batch.ID, RequestID: batch.RequestID,
			// The stored column is authoritative, so the list filter and the
			// detail view can never disagree about the same work order.
			State: batch.State, FormState: batch.FormState, FormVersion: batch.FormVersion,
			ProjectName: formProjectName(batch.FormJSON),
			Cameras:     cameras, LastError: strings.TrimSpace(batch.LastError),
		})
	}
	return items, nil
}

// loadLedgerGraph reads every child row and every archived slice for the given
// ledgers in two queries, ordered the same way the previous per-row reads were.
func (s *BatchService) loadLedgerGraph(ctx context.Context, batchIDs []string) (map[string][]models.GbWorkRecording, map[string][]BatchFileSnapshot, error) {
	children := make(map[string][]models.GbWorkRecording, len(batchIDs))
	files := make(map[string][]BatchFileSnapshot)
	var jobs []models.GbWorkRecording
	if err := s.db.WithContext(ctx).Where("batch_id IN ?", batchIDs).Order("channel_id ASC").Find(&jobs).Error; err != nil {
		return nil, nil, err
	}
	if len(jobs) == 0 {
		return children, files, nil
	}
	jobIDs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		children[job.BatchID] = append(children[job.BatchID], job)
		jobIDs = append(jobIDs, job.ID)
	}
	var rows []struct {
		WorkRecordingID string
		ID              uint64
		ChannelID       uint
		FileName        string
		StartTime       *time.Time
		TimeLen         *float64
		FileSize        *uint64
		MetadataState   string
		MissingAt       *time.Time
	}
	if err := s.db.WithContext(ctx).Table("gb_work_recording_file").
		Select("gb_work_recording_file.work_recording_id, gb_recording_file.id, gb_recording_file.channel_id, gb_recording_file.file_name, gb_recording_file.start_time, gb_recording_file.time_len, gb_recording_file.file_size, gb_recording_file.metadata_state, gb_recording_file.missing_at").
		Joins("JOIN gb_recording_file ON gb_recording_file.id = gb_work_recording_file.file_id").
		Where("gb_work_recording_file.work_recording_id IN ?", jobIDs).
		Order("gb_recording_file.start_time ASC, gb_recording_file.id ASC").
		Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		state := FileReady
		if row.MissingAt != nil || row.MetadataState != models.RecordingMetadataComplete {
			state = FileUnknown
		}
		files[row.WorkRecordingID] = append(files[row.WorkRecordingID], BatchFileSnapshot{
			ID: row.ID, ChannelID: row.ChannelID, FileName: row.FileName,
			StartTime: row.StartTime, TimeLen: row.TimeLen, FileSize: row.FileSize, State: state,
		})
	}
	return children, files, nil
}

// channelInfo is everything a camera row needs to be explained to an operator.
type channelInfo struct {
	Name       string
	Code       string
	DeviceID   string
	DeviceName string
}

// channelNames resolves every channel a page of ledgers references, in two
// queries: one for the channels and one for the devices behind them. Looking the
// device up per camera would reintroduce the N+1 this aggregation removed.
func (s *BatchService) channelNames(ctx context.Context, childrenByBatch map[string][]models.GbWorkRecording) (map[uint]channelInfo, error) {
	seen := make(map[uint]struct{})
	ids := make([]uint, 0)
	for _, children := range childrenByBatch {
		for _, job := range children {
			if _, ok := seen[job.ChannelID]; ok {
				continue
			}
			seen[job.ChannelID] = struct{}{}
			ids = append(ids, job.ChannelID)
		}
	}
	infos := make(map[uint]channelInfo, len(ids))
	if len(ids) == 0 {
		return infos, nil
	}
	var channels []models.GbChannel
	if err := s.db.WithContext(ctx).Select("id", "name", "channel_id", "device_id").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return nil, err
	}
	deviceCodes := make([]string, 0, len(channels))
	deviceSeen := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		infos[channel.ID] = channelInfo{Name: channel.Name, Code: channel.ChannelID, DeviceID: channel.DeviceID}
		if channel.DeviceID == "" {
			continue
		}
		if _, ok := deviceSeen[channel.DeviceID]; ok {
			continue
		}
		deviceSeen[channel.DeviceID] = struct{}{}
		deviceCodes = append(deviceCodes, channel.DeviceID)
	}
	if len(deviceCodes) == 0 {
		return infos, nil
	}
	var devices []models.GbDevice
	if err := s.db.WithContext(ctx).Select("device_id", "name").Where("device_id IN ?", deviceCodes).Find(&devices).Error; err != nil {
		return nil, err
	}
	names := make(map[string]string, len(devices))
	for _, device := range devices {
		names[device.DeviceID] = device.Name
	}
	for id, info := range infos {
		info.DeviceName = names[info.DeviceID]
		infos[id] = info
	}
	return infos, nil
}

// foldChildStates resolves one ledger state from its children. A ledger with no
// children yet is still starting; everything else follows the documented
// priority, so an unknown or failed camera is never hidden by a healthy one.
func foldChildStates(states []string) string {
	if len(states) == 0 {
		return StateStarting
	}
	folded := StateStopped
	for _, state := range states {
		folded = combineBatchState(folded, state)
	}
	return folded
}

func combineBatchState(current, child string) string {
	if statePriority(child) > statePriority(current) {
		return child
	}
	return current
}

func statePriority(state string) int {
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

// formProjectName surfaces the operator's own project label so a list can show
// it without every client having to parse the stored form JSON.
func formProjectName(raw string) string {
	var envelope struct {
		ProjectName string `json:"projectName"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return ""
	}
	return strings.TrimSpace(envelope.ProjectName)
}

func knownState(state string) bool {
	switch state {
	case StateIdle, StateStarting, StateRecording, StateStopping, StateStopped, StateFailed, StateUnknown:
		return true
	default:
		return false
	}
}
