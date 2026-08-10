package recording

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestCatalogReconcilerManualWindowDeduplicatesCandidatesAndCreatesPartial(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return now }
	channel := seedCatalogCandidate(t, db, repo, 1, "stream-a")
	client := &fakeCatalogRecordClient{files: map[string][]zlm.MP4RecordFile{
		"2026-08-10": {{FilePath: "/record/stream-a/2026-08-10/a.mp4", FileName: "a.mp4", Folder: "/record/stream-a/2026-08-10"}},
	}}
	reconciler := NewCatalogReconciler(repo, fakeLocationLookup{"stream-a": 1}, fakeCatalogNodes{items: map[int64]*node.Node{1: {ID: 1, State: node.StateActive}}}, func(*node.Node) CatalogRecordClient { return client })
	reconciler.now = func() time.Time { return now }

	state, err := reconciler.RunNode(context.Background(), 1, ReconcileTriggerManual, nil, nil)
	require.NoError(t, err)
	require.Equal(t, models.RecordingReconcileSucceeded, state.Status)
	require.Equal(t, 1, state.CandidateCount, "session and current stream must be de-duplicated")
	require.Equal(t, 9, state.SuccessCount, "seven calendar days plus one day on each side")
	require.Equal(t, 1, state.InsertedCount)
	require.Len(t, client.periods(), 9)

	var file models.GbRecordingFile
	require.NoError(t, db.Where("file_key = ?", BuildFileKey(1, "/record/stream-a/2026-08-10/a.mp4")).First(&file).Error)
	require.Equal(t, channel.ID, file.ChannelID)
	require.Equal(t, models.RecordingFileSourceReconcile, file.Source)
	require.Equal(t, models.RecordingMetadataPartial, file.MetadataState)
	require.Nil(t, file.StartTime)
	require.Nil(t, file.TimeLen)
	require.Nil(t, file.FileSize)
}

func TestCatalogReconcilerRequiresTwoSuccessfulMissesAndRediscoveryClearsMissing(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return now }
	channel := seedCatalogCandidate(t, db, repo, 1, "stream-miss")
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	file := &models.GbRecordingFile{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, OwnerDeptID: channel.OwnerDeptID,
		NodeID: 1, VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp, Stream: "stream-miss",
		FileKey: BuildFileKey(1, "/record/miss.mp4"), FileName: "miss.mp4", FilePath: "/record/miss.mp4",
		Source: models.RecordingFileSourceReconcile, MetadataState: models.RecordingMetadataPartial,
		RecordDate: &date, DiscoveredAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
	require.NoError(t, db.Create(file).Error)
	client := &fakeCatalogRecordClient{files: map[string][]zlm.MP4RecordFile{}}
	reconciler := NewCatalogReconciler(repo, fakeLocationLookup{"stream-miss": 1}, fakeCatalogNodes{items: map[int64]*node.Node{1: {ID: 1, State: node.StateActive}}}, func(*node.Node) CatalogRecordClient { return client })
	reconciler.now = func() time.Time { return now }
	start, end := date, date

	_, err := reconciler.RunNode(context.Background(), 1, ReconcileTriggerManual, &start, &end)
	require.NoError(t, err)
	var stored models.GbRecordingFile
	require.NoError(t, db.First(&stored, file.ID).Error)
	require.Equal(t, 1, stored.ReconcileMissCount)
	require.Nil(t, stored.MissingAt)

	_, err = reconciler.RunNode(context.Background(), 1, ReconcileTriggerManual, &start, &end)
	require.NoError(t, err)
	stored = models.GbRecordingFile{}
	require.NoError(t, db.First(&stored, file.ID).Error)
	require.Equal(t, 2, stored.ReconcileMissCount)
	require.NotNil(t, stored.MissingAt)

	client.files["2026-08-10"] = []zlm.MP4RecordFile{{FilePath: file.FilePath, FileName: file.FileName}}
	_, err = reconciler.RunNode(context.Background(), 1, ReconcileTriggerManual, &start, &end)
	require.NoError(t, err)
	stored = models.GbRecordingFile{}
	require.NoError(t, db.First(&stored, file.ID).Error)
	require.Zero(t, stored.ReconcileMissCount)
	require.Nil(t, stored.MissingAt)
}

func TestCatalogReconcilerFailedUnitDoesNotIncrementMiss(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	channel := seedCatalogCandidate(t, db, repo, 1, "stream-fail")
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	file := &models.GbRecordingFile{ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 1, VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp, Stream: "stream-fail", FileKey: BuildFileKey(1, "/record/fail.mp4"), FileName: "fail.mp4", FilePath: "/record/fail.mp4", Source: models.RecordingFileSourceReconcile, MetadataState: models.RecordingMetadataPartial, RecordDate: &date, DiscoveredAt: now}
	require.NoError(t, db.Create(file).Error)
	client := &fakeCatalogRecordClient{err: errors.New("node unavailable")}
	reconciler := NewCatalogReconciler(repo, fakeLocationLookup{"stream-fail": 1}, fakeCatalogNodes{items: map[int64]*node.Node{1: {ID: 1, State: node.StateActive}}}, func(*node.Node) CatalogRecordClient { return client })
	reconciler.now = func() time.Time { return now }
	start, end := date, date

	state, err := reconciler.RunNode(context.Background(), 1, ReconcileTriggerManual, &start, &end)
	require.Error(t, err)
	require.Equal(t, models.RecordingReconcileFailed, state.Status)
	require.NoError(t, db.First(file, file.ID).Error)
	require.Zero(t, file.ReconcileMissCount)
	require.Nil(t, file.MissingAt)
}

func TestCatalogReconcilerAllowsOnlyOneRunPerNode(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	seedCatalogCandidate(t, db, repo, 1, "stream-lock")
	client := &blockingCatalogRecordClient{started: make(chan struct{}), release: make(chan struct{})}
	reconciler := NewCatalogReconciler(repo, fakeLocationLookup{"stream-lock": 1}, fakeCatalogNodes{items: map[int64]*node.Node{1: {ID: 1, State: node.StateActive}}}, func(*node.Node) CatalogRecordClient { return client })
	first := make(chan error, 1)
	go func() {
		_, err := reconciler.RunNode(context.Background(), 1, ReconcileTriggerScheduled, nil, nil)
		first <- err
	}()
	<-client.started
	_, err := reconciler.RunNode(context.Background(), 1, ReconcileTriggerManual, nil, nil)
	require.ErrorIs(t, err, ErrCatalogReconcileRunning)
	close(client.release)
	require.NoError(t, <-first)
}

func seedCatalogCandidate(t *testing.T, db *gorm.DB, repo *GormRepo, nodeID int64, stream string) *models.GbChannel {
	t.Helper()
	device := &models.GbDevice{DeviceID: "device-" + stream, Name: "device", OwnerDeptID: 8}
	require.NoError(t, db.Create(device).Error)
	channel := &models.GbChannel{DeviceID: device.DeviceID, ChannelID: "channel-" + stream, Name: "channel", OwnerDeptID: 8, StreamID: stream}
	require.NoError(t, db.Create(channel).Error)
	session := &models.GbRecordingSession{ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: nodeID, VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp, Stream: stream, State: models.RecordingSessionStateStopped}
	require.NoError(t, repo.UpsertSession(context.Background(), session))
	return channel
}

type fakeCatalogNodes struct{ items map[int64]*node.Node }

func (f fakeCatalogNodes) Get(id int64) (*node.Node, bool) { n, ok := f.items[id]; return n, ok }
func (f fakeCatalogNodes) List() []*node.Node {
	result := make([]*node.Node, 0, len(f.items))
	for _, n := range f.items {
		result = append(result, n)
	}
	return result
}

type fakeCatalogRecordClient struct {
	mu     sync.Mutex
	files  map[string][]zlm.MP4RecordFile
	err    error
	period []string
}

func (f *fakeCatalogRecordClient) GetMP4RecordFiles(_ context.Context, _, _, _, period string) ([]zlm.MP4RecordFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.period = append(f.period, period)
	if f.err != nil {
		return nil, f.err
	}
	return f.files[period], nil
}

func (f *fakeCatalogRecordClient) periods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.period...)
}

type blockingCatalogRecordClient struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (f *blockingCatalogRecordClient) GetMP4RecordFiles(context.Context, string, string, string, string) ([]zlm.MP4RecordFile, error) {
	f.once.Do(func() {
		close(f.started)
		<-f.release
	})
	return nil, nil
}
