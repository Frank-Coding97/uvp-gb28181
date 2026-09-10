package workrecording

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
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

// A work order records every playing channel, so the only rejection at the top
// end is the defensive bound rather than a four-camera business limit.
func TestBatchStartRequestAcceptsUniqueChannelsUpToTheDefensiveBound(t *testing.T) {
	full := make([]uint, 0, BatchMaxCameraCount)
	for i := 1; i <= BatchMaxCameraCount; i++ {
		full = append(full, uint(i))
	}
	for _, ids := range [][]uint{{1}, {1, 2}, {1, 2, 3, 4}, full} {
		if err := (BatchStartRequest{ChannelIDs: ids, RequestID: "batch-1"}).Validate(); err != nil {
			t.Fatalf("ids %v: valid batch rejected: %v", ids, err)
		}
	}
	overBound := append(append([]uint{}, full...), uint(BatchMaxCameraCount+1))
	for _, ids := range [][]uint{{}, {1, 2, 2}, {0}, overBound} {
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

// seedLedgerBatch writes one ledger row plus its child cameras directly, which
// is how the recorder engine leaves them behind.
func seedLedgerBatch(t *testing.T, db *gorm.DB, id string, actor uint, state, project string, channels ...uint) {
	t.Helper()
	created := time.Now().UTC()
	require.NoError(t, db.Create(&models.GbWorkRecordingBatch{
		ID: id, CreatedBy: actor, RequestID: "req-" + id, State: state, Version: 1,
		FormState: FormSubmitted, FormVersion: 1, SchemaVersion: 1,
		FormJSON: `{"projectName":"` + project + `"}`, CreatedAt: created, UpdatedAt: created,
	}).Error)
	for _, channelID := range channels {
		require.NoError(t, db.Create(&models.GbWorkRecording{
			ID: uuid.NewString(), BatchID: id, ChannelID: channelID, CreatedBy: actor,
			RequestID: id + "/" + strconv.FormatUint(uint64(channelID), 10), State: state,
			DesiredAction: DesiredActionStart, Version: 1, FileState: FilePending,
			FormState: FormSubmitted, SchemaVersion: 1, FormJSON: "{}",
		}).Error)
	}
}

func TestBatchServiceListFiltersByActorStateKeywordChannelAndWindow(t *testing.T) {
	db := batchServiceDB(t)
	service := NewBatchService(db, &batchJobFake{db: db})
	seedLedgerBatch(t, db, "11111111-1111-1111-1111-111111111111", 100, StateRecording, "沪宁线放线作业", 12, 15)
	seedLedgerBatch(t, db, "22222222-2222-2222-2222-222222222222", 100, StateStopped, "京沪线检修作业", 27)
	seedLedgerBatch(t, db, "33333333-3333-3333-3333-333333333333", 200, StateRecording, "沪宁线放线作业", 12)

	items, total, err := service.List(context.Background(), BatchListFilter{Actor: 100, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, items, 2)
	// Another operator's work order must never leak into this list.
	for _, item := range items {
		require.NotEqual(t, "33333333-3333-3333-3333-333333333333", item.ID)
	}

	items, total, err = service.List(context.Background(), BatchListFilter{Actor: 100, Page: 1, PageSize: 10, States: []string{StateRecording}})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "11111111-1111-1111-1111-111111111111", items[0].ID)

	items, total, err = service.List(context.Background(), BatchListFilter{Actor: 100, Page: 1, PageSize: 10, Keyword: "京沪线"})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "22222222-2222-2222-2222-222222222222", items[0].ID)

	items, total, err = service.List(context.Background(), BatchListFilter{Actor: 100, Page: 1, PageSize: 10, ChannelID: 27})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "22222222-2222-2222-2222-222222222222", items[0].ID)

	future := time.Now().UTC().Add(time.Hour)
	_, total, err = service.List(context.Background(), BatchListFilter{Actor: 100, Page: 1, PageSize: 10, From: &future})
	require.NoError(t, err)
	require.Zero(t, total)

	_, _, err = service.List(context.Background(), BatchListFilter{Actor: 100, Page: 1, PageSize: 10, States: []string{"nonsense"}})
	require.ErrorIs(t, err, ErrBatchInvalid)
}

func TestBatchServiceListReportsProjectNameAndCamerasWithoutExtraQueries(t *testing.T) {
	db := batchServiceDB(t)
	service := NewBatchService(db, &batchJobFake{db: db})
	seedLedgerBatch(t, db, "11111111-1111-1111-1111-111111111111", 100, StateRecording, "沪宁线放线作业", 12, 15)
	require.NoError(t, db.Create(&models.GbChannel{ID: 12, DeviceID: "ours", ChannelID: "a", Name: "K12+300 左线", OwnerDeptID: 10}).Error)
	require.NoError(t, db.Create(&models.GbChannel{ID: 15, DeviceID: "ours", ChannelID: "b", Name: "K12+300 右线", OwnerDeptID: 10}).Error)

	items, _, err := service.List(context.Background(), BatchListFilter{Actor: 100, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "沪宁线放线作业", items[0].ProjectName)
	require.Len(t, items[0].Cameras, 2)
	require.Equal(t, "K12+300 左线", items[0].Cameras[0].ChannelName)
	require.Equal(t, "K12+300 右线", items[0].Cameras[1].ChannelName)
}

// The stored column is authoritative: readers never redraw the aggregate, so the
// list filter and the detail view cannot disagree.
func TestBatchServiceReadersReturnTheStoredStateUntilReconciled(t *testing.T) {
	db := batchServiceDB(t)
	service := NewBatchService(db, &batchJobFake{db: db})
	seedLedgerBatch(t, db, "11111111-1111-1111-1111-111111111111", 100, StateRecording, "沪宁线放线作业", 12)

	// A camera that stopped on its own leaves the ledger stale until reconciled.
	require.NoError(t, db.Model(&models.GbWorkRecording{}).Where("batch_id = ?", "11111111-1111-1111-1111-111111111111").Update("state", StateStopped).Error)
	snapshot, err := service.Get(context.Background(), "11111111-1111-1111-1111-111111111111")
	require.NoError(t, err)
	require.Equal(t, StateRecording, snapshot.State)

	require.NoError(t, service.ReconcileState(context.Background(), "11111111-1111-1111-1111-111111111111"))
	snapshot, err = service.Get(context.Background(), "11111111-1111-1111-1111-111111111111")
	require.NoError(t, err)
	require.Equal(t, StateStopped, snapshot.State)
}

func TestBatchServiceReconcileStateKeepsTheSeverestCameraState(t *testing.T) {
	db := batchServiceDB(t)
	service := NewBatchService(db, &batchJobFake{db: db})
	seedLedgerBatch(t, db, "11111111-1111-1111-1111-111111111111", 100, StateRecording, "沪宁线放线作业", 12, 15)
	require.NoError(t, db.Model(&models.GbWorkRecording{}).Where("batch_id = ? AND channel_id = ?", "11111111-1111-1111-1111-111111111111", 15).Update("state", StateUnknown).Error)

	require.NoError(t, service.ReconcileState(context.Background(), "11111111-1111-1111-1111-111111111111"))
	var stored models.GbWorkRecordingBatch
	require.NoError(t, db.First(&stored, "id = ?", "11111111-1111-1111-1111-111111111111").Error)
	require.Equal(t, StateUnknown, stored.State)
	require.EqualValues(t, 2, stored.Version)

	// Reconciling again must be a no-op rather than bumping the version forever.
	require.NoError(t, service.ReconcileState(context.Background(), "11111111-1111-1111-1111-111111111111"))
	require.NoError(t, db.First(&stored, "id = ?", "11111111-1111-1111-1111-111111111111").Error)
	require.EqualValues(t, 2, stored.Version)
}

// Every child transition funnels through Service.updateJob, so that is where the
// ledger has to hear about it. Other column updates must stay silent.
func TestServiceNotifiesLedgerObserverOnlyOnChildStateChange(t *testing.T) {
	db := formDB(t, filepath.Join(t.TempDir(), "observer.db"))
	job := formJob(uuid.NewString(), 8, 100, time.Now().UTC(), time.Now().UTC())
	job.BatchID = uuid.NewString()
	require.NoError(t, db.Create(&job).Error)

	service := NewService(db, nil, nil)
	notified := make([]string, 0, 1)
	service.SetStateObserver(func(_ context.Context, batchID string) { notified = append(notified, batchID) })

	current, err := service.findJob(context.Background(), job.ID)
	require.NoError(t, err)
	_, err = service.updateJob(context.Background(), current, map[string]any{"state": StateStopped})
	require.NoError(t, err)
	require.Equal(t, []string{job.BatchID}, notified)

	notified = notified[:0]
	current, err = service.findJob(context.Background(), job.ID)
	require.NoError(t, err)
	_, err = service.updateJob(context.Background(), current, map[string]any{"last_checked_at": time.Now().UTC()})
	require.NoError(t, err)
	require.Empty(t, notified)
}

func batchServiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "batch.db")+"?_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecordingBatch{}, &models.GbWorkRecording{}, &models.GbRecordingFile{}, &models.GbWorkRecordingFile{}, &models.GbChannel{}, &models.GbDevice{}))
	return db
}

func TestBatchServiceStartsOnlySelectedJobsAndIsIdempotent(t *testing.T) {
	db := batchServiceDB(t)
	jobs := &batchJobFake{db: db}
	service := NewBatchService(db, jobs)
	request := OrderStartRequest{ChannelIDs: []uint{4, 2}, RequestID: "order-success", Form: requiredForm()}

	started, err := service.StartOrder(context.Background(), 100, request)
	require.NoError(t, err)
	require.Equal(t, StateRecording, started.State)
	require.Len(t, started.Cameras, 2)
	require.Equal(t, []uint{2, 4}, []uint{started.Cameras[0].ChannelID, started.Cameras[1].ChannelID})

	retried, err := service.StartOrder(context.Background(), 100, request)
	require.NoError(t, err)
	require.Equal(t, started.ID, retried.ID)
	require.Len(t, jobs.starts, 2)
}

func TestBatchServiceRollsBackSuccessfulJobsAndKeepsFailedCameraInLedger(t *testing.T) {
	db := batchServiceDB(t)
	jobs := &batchJobFake{db: db, failChannel: 3}
	service := NewBatchService(db, jobs)

	snapshot, err := service.StartOrder(context.Background(), 100, OrderStartRequest{ChannelIDs: []uint{1, 2, 3, 4}, RequestID: "order-failure", Form: requiredForm()})
	require.ErrorIs(t, err, ErrBatchIncomplete)
	// The real cause must survive in the chain instead of being flattened into
	// the generic sentinel.
	require.Contains(t, err.Error(), "camera start failed")
	require.Equal(t, StateFailed, snapshot.State)
	require.Len(t, snapshot.Cameras, 4)
	require.Len(t, jobs.stops, 3)
	require.Equal(t, "camera start failed", snapshot.LastError)
}

// "Fill the work order first, then record": an incomplete form must not create a
// ledger row and must not claim a single camera on the recorder.
func TestBatchServiceStartOrderRefusesToRecordBeforeTheFormIsComplete(t *testing.T) {
	db := batchServiceDB(t)
	jobs := &batchJobFake{db: db}
	service := NewBatchService(db, jobs)

	_, err := service.StartOrder(context.Background(), 100, OrderStartRequest{
		RequestID: "order-incomplete", ChannelIDs: []uint{1, 2},
		Form: Form{ProjectName: "沪宁线放线作业"},
	})
	require.ErrorIs(t, err, ErrFormRequired)
	require.Empty(t, jobs.starts)

	var count int64
	require.NoError(t, db.Model(&models.GbWorkRecordingBatch{}).Count(&count).Error)
	require.Zero(t, count, "an invalid form must not leave a ledger row behind")
}

func TestBatchServiceStartOrderPersistsSubmittedFormWithTheRecording(t *testing.T) {
	db := batchServiceDB(t)
	jobs := &batchJobFake{db: db}
	service := NewBatchService(db, jobs)

	snapshot, err := service.StartOrder(context.Background(), 100, OrderStartRequest{
		RequestID: "order-complete", ChannelIDs: []uint{7},
		Form: Form{
			ProjectName: "沪宁线放线作业", StationArea: "南京南—江宁",
			AnchorSectionNo: "A-12", WorkLeader: "张伟",
			WorkPersonnel: []string{"张伟", "李强"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, StateRecording, snapshot.State)
	require.Equal(t, FormSubmitted, snapshot.FormState)
	require.EqualValues(t, 1, snapshot.FormVersion)

	var stored models.GbWorkRecordingBatch
	require.NoError(t, db.First(&stored, "id = ?", snapshot.ID).Error)
	require.Equal(t, FormSubmitted, stored.FormState)
	require.EqualValues(t, 1, stored.FormVersion)
	require.Contains(t, stored.FormJSON, "沪宁线放线作业")
	require.Contains(t, stored.FormJSON, "A-12")

	decoded, err := decodeStoredForm(stored.FormJSON)
	require.NoError(t, err)
	require.Equal(t, []string{"张伟", "李强"}, decoded.WorkPersonnel)
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

func TestBatchServiceDeleteRemovesSettledLedgerAndItsCameras(t *testing.T) {
	db := batchServiceDB(t)
	service := NewBatchService(db, &batchJobFake{db: db})
	id := "11111111-1111-1111-1111-111111111111"
	seedLedgerBatch(t, db, id, 100, StateStopped, "沪宁线放线作业", 12, 15)

	var jobIDs []string
	require.NoError(t, db.Model(&models.GbWorkRecording{}).Where("batch_id = ?", id).Pluck("id", &jobIDs).Error)
	require.Len(t, jobIDs, 2)
	fileIDs := make([]uint64, 0, len(jobIDs))
	for index, jobID := range jobIDs {
		fileID := uint64(100 + index)
		fileIDs = append(fileIDs, fileID)
		require.NoError(t, db.Create(&models.GbRecordingFile{ID: fileID, ChannelID: uint(index + 1), DeviceID: "ours", FileKey: uuid.NewString(), FileName: "slice.mp4", FilePath: "/node/record/slice.mp4", MetadataState: models.RecordingMetadataComplete}).Error)
		require.NoError(t, db.Create(&models.GbWorkRecordingFile{FileID: fileID, WorkRecordingID: jobID}).Error)
	}
	// A slice of another, unrelated recording must survive untouched.
	other := models.GbRecordingFile{ID: 999, ChannelID: 3, DeviceID: "ours", FileKey: uuid.NewString(), FileName: "other.mp4", FilePath: "/node/record/other.mp4", MetadataState: models.RecordingMetadataComplete}
	require.NoError(t, db.Create(&other).Error)

	result, err := service.Delete(context.Background(), 100, BatchDeleteRequest{IDs: []string{id}})
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Deleted)
	require.Empty(t, result.Skipped)
	require.Empty(t, result.Missing)

	for _, table := range []struct {
		name  string
		model any
		query string
		args  []any
	}{
		{"gb_work_recording_batch", &models.GbWorkRecordingBatch{}, "id = ?", []any{id}},
		{"gb_work_recording", &models.GbWorkRecording{}, "batch_id = ?", []any{id}},
		{"gb_work_recording_file", &models.GbWorkRecordingFile{}, "work_recording_id IN ?", []any{jobIDs}},
	} {
		var count int64
		require.NoError(t, db.Model(table.model).Where(table.query, table.args...).Count(&count).Error, table.name)
		require.Zero(t, count, table.name+" 应随作业单一起删除")
	}
	// 归档索引保留：文件还在节点上，删掉索引会让录像无从查找与清理。
	var files int64
	require.NoError(t, db.Model(&models.GbRecordingFile{}).Where("id IN ?", fileIDs).Count(&files).Error)
	require.EqualValues(t, len(fileIDs), files)
	var survivors int64
	require.NoError(t, db.Model(&models.GbRecordingFile{}).Where("id = ?", other.ID).Count(&survivors).Error)
	require.EqualValues(t, 1, survivors)
}

func TestBatchServiceDeleteSkipsRunningLedgerAndReportsMissingIDs(t *testing.T) {
	db := batchServiceDB(t)
	service := NewBatchService(db, &batchJobFake{db: db})
	running := "11111111-1111-1111-1111-111111111111"
	settled := "22222222-2222-2222-2222-222222222222"
	seedLedgerBatch(t, db, running, 100, StateRecording, "正在录制的作业", 12)
	seedLedgerBatch(t, db, settled, 100, StateStopped, "已结束的作业", 15)
	seedLedgerBatch(t, db, "44444444-4444-4444-4444-444444444444", 200, StateStopped, "别人的作业", 27)

	result, err := service.Delete(context.Background(), 100, BatchDeleteRequest{IDs: []string{running, settled, "55555555-5555-5555-5555-555555555555"}})
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Deleted)
	require.Equal(t, []string{running}, result.Skipped)
	require.Equal(t, []string{"55555555-5555-5555-5555-555555555555"}, result.Missing)

	// 被跳过的那张单原样保留，还能继续录制与结束。
	var kept models.GbWorkRecordingBatch
	require.NoError(t, db.First(&kept, "id = ?", running).Error)
	require.Equal(t, StateRecording, kept.State)
	// 另一操作员的作业单不在可见范围内，必然不动。
	var untouched models.GbWorkRecordingBatch
	require.NoError(t, db.First(&untouched, "id = ?", "44444444-4444-4444-4444-444444444444").Error)
}

func TestBatchServiceDeleteRejectsInvalidRequests(t *testing.T) {
	db := batchServiceDB(t)
	service := NewBatchService(db, &batchJobFake{db: db})
	for _, request := range []BatchDeleteRequest{
		{},
		{IDs: []string{""}},
		{IDs: []string{"not-a-uuid"}},
		{IDs: []string{"11111111-1111-1111-1111-111111111111", "11111111-1111-1111-1111-111111111111"}},
	} {
		_, err := service.Delete(context.Background(), 100, request)
		require.ErrorIs(t, err, ErrBatchInvalid, "%v", request)
	}
	_, err := service.Delete(context.Background(), 0, BatchDeleteRequest{IDs: []string{"11111111-1111-1111-1111-111111111111"}})
	require.ErrorIs(t, err, ErrBatchInvalid)
}

// 作业单详情要能只凭快照说清"录的是哪台设备的哪个通道"，所以相机行必须带上
// 设备国标编码、设备名称与通道国标编码——而不是让前端再逐个反查。
func TestBatchSnapshotCarriesDeviceAndChannelCodes(t *testing.T) {
	db := batchServiceDB(t)
	id := "11111111-1111-1111-1111-111111111111"
	seedLedgerBatch(t, db, id, 100, StateStopped, "沪宁线放线作业", 12)
	require.NoError(t, db.Create(&models.GbDevice{DeviceID: "37010301021180000007", Name: "前置摄像头"}).Error)
	require.NoError(t, db.Create(&models.GbChannel{ID: 12, DeviceID: "37010301021180000007", ChannelID: "34020000001320000010", Name: "K12+300 左线", OwnerDeptID: 10}).Error)

	service := NewBatchService(db, &batchJobFake{db: db})
	snapshot, err := service.Get(context.Background(), id)
	require.NoError(t, err)
	require.Len(t, snapshot.Cameras, 1)
	camera := snapshot.Cameras[0]
	require.Equal(t, "K12+300 左线", camera.ChannelName)
	require.Equal(t, "34020000001320000010", camera.ChannelCode)
	require.Equal(t, "37010301021180000007", camera.DeviceID)
	require.Equal(t, "前置摄像头", camera.DeviceName)
}

// 设备行缺失（未同步/已删除）时只留空，不能因此丢掉整个列表。
func TestBatchSnapshotToleratesAMissingDeviceRow(t *testing.T) {
	db := batchServiceDB(t)
	id := "11111111-1111-1111-1111-111111111111"
	seedLedgerBatch(t, db, id, 100, StateStopped, "沪宁线放线作业", 12)
	require.NoError(t, db.Create(&models.GbChannel{ID: 12, DeviceID: "unknown-device", ChannelID: "34020000001320000010", Name: "K12+300 左线", OwnerDeptID: 10}).Error)

	service := NewBatchService(db, &batchJobFake{db: db})
	snapshot, err := service.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, "34020000001320000010", snapshot.Cameras[0].ChannelCode)
	require.Equal(t, "unknown-device", snapshot.Cameras[0].DeviceID)
	require.Empty(t, snapshot.Cameras[0].DeviceName)
}

// 「录入后寄存」的语义：upsert 累加 use_count + 1 并刷新 last_used_at。
// 同时验证 list 按 last_used_at desc 排序，limit 50 上限。
func TestBatchServiceRecordFormHistoryUpsertsAndListsByField(t *testing.T) {
	db := batchServiceDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbWorkOrderFormHistory{}))
	service := NewBatchService(db, &batchJobFake{db: db})
	now := time.Now().Add(-time.Hour)
	service.now = func() time.Time { return now }

	service.recordFormHistory(context.Background(), Form{
		ProjectName:   "沪宁线放线作业",
		StationArea:   "南京南—江宁",
		WorkLeader:    "张伟",
		WorkPersonnel: []string{"张伟", "李强", "王芳"},
	})

	// 三个字符串字段 + 3 个作业人员 = 5 条历史值
	for _, tc := range []struct {
		field string
		want  int
	}{
		{models.FormHistoryFieldProjectName, 1},
		{models.FormHistoryFieldStationArea, 1},
		{models.FormHistoryFieldWorkLeader, 1},
		{models.FormHistoryFieldWorkPersonnel, 3},
	} {
		var n int64
		require.NoError(t, db.Model(&models.GbWorkOrderFormHistory{}).Where("field_key = ?", tc.field).Count(&n).Error)
		require.EqualValues(t, tc.want, n, tc.field)
	}

	// 二次提交：相同字段相同值应累加 use_count + 1
	later := now.Add(2 * time.Minute)
	service.now = func() time.Time { return later }
	service.recordFormHistory(context.Background(), Form{
		ProjectName:   "沪宁线放线作业",
		StationArea:   "南京南—江宁",
		WorkLeader:    "张伟",
		WorkPersonnel: []string{"张伟"},
	})
	var row models.GbWorkOrderFormHistory
	require.NoError(t, db.Where("field_key = ? AND value = ?", models.FormHistoryFieldProjectName, "沪宁线放线作业").First(&row).Error)
	t.Logf("DEBUG row=%+v", row)
	require.EqualValues(t, 2, row.UseCount, "同值再 upsert 必须 use_count 累加")
	require.True(t, row.LastUsedAt.Equal(later), "last_used_at 必须刷新到最近一次提交时间")
	// workPersonnel=张伟 累加到 2，李强和王芳仍是 1
	var leader models.GbWorkOrderFormHistory
	require.NoError(t, db.Where("field_key = ? AND value = ?", models.FormHistoryFieldWorkPersonnel, "张伟").First(&leader).Error)
	require.EqualValues(t, 2, leader.UseCount, "提交两次的人名 use_count 必须累加到 2")
	var wang models.GbWorkOrderFormHistory
	require.NoError(t, db.Where("field_key = ? AND value = ?", models.FormHistoryFieldWorkPersonnel, "王芳").First(&wang).Error)
	require.EqualValues(t, 1, wang.UseCount, "未重复提交的人名 use_count 应保持为 1")

	// ListFormHistory 必须按 last_used_at desc 排序
	entries, err := service.ListFormHistory(context.Background(), models.FormHistoryFieldProjectName, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "沪宁线放线作业", entries[0].Value)
	require.EqualValues(t, 2, entries[0].UseCount)
}

func TestBatchServiceListFormHistoryRejectsUnknownField(t *testing.T) {
	db := batchServiceDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbWorkOrderFormHistory{}))
	service := NewBatchService(db, &batchJobFake{db: db})

	_, err := service.ListFormHistory(context.Background(), "userId", 10)
	require.ErrorIs(t, err, ErrFormHistoryInvalidField)

	_, err = service.ListFormHistory(context.Background(), "", 10)
	require.ErrorIs(t, err, ErrFormHistoryInvalidField)
}

func TestBatchServiceListFormHistoryClampsLimitToFifty(t *testing.T) {
	db := batchServiceDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbWorkOrderFormHistory{}))
	service := NewBatchService(db, &batchJobFake{db: db})

	// 注入 60 条同名同 field，service.now 不变（用真实 time）
	base := time.Now().Add(-24 * time.Hour)
	for i := 0; i < 60; i++ {
		require.NoError(t, db.Create(&models.GbWorkOrderFormHistory{
			FieldKey: models.FormHistoryFieldProjectName, Value: "项目-" + strconv.Itoa(i),
			UseCount: 1, LastUsedAt: base.Add(time.Duration(i) * time.Minute),
		}).Error)
	}
	entries, err := service.ListFormHistory(context.Background(), models.FormHistoryFieldProjectName, 0)
	require.NoError(t, err)
	require.Len(t, entries, 50, "limit<=0 时必须夹到 formHistoryListLimit=50")
	// desc 排序：第一条应是最新插入的「项目-59」
	require.Equal(t, "项目-59", entries[0].Value)
}
