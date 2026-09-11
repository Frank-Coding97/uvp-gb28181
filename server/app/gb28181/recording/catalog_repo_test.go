package recording

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestCatalogRepoListUsesOneScopedQueryAndStableOrdering(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:catalog-list?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbRecordingFile{}))
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour)
	unknown := catalogFile(1, 1, 10, now.Add(-time.Hour), "camera-a.mp4")
	unknown.StartTime = nil
	require.NoError(t, db.Create(&unknown).Error)
	file2 := catalogFile(2, 1, 10, now.Add(-30*time.Minute), "camera-b.mp4")
	file3 := catalogFile(3, 2, 20, now.Add(-20*time.Minute), "secret.mp4")
	file4 := catalogFile(4, 1, 10, old, "old.mp4")
	require.NoError(t, db.Create(&file2).Error)
	require.NoError(t, db.Create(&file3).Error)
	require.NoError(t, db.Create(&file4).Error)

	repo := NewGormRepo(db)
	page, err := repo.ListCatalogFiles(context.Background(), FileQuery{
		Page: 1, PageSize: 20, Start: ptrTime(now.Add(-24 * time.Hour)), End: ptrTime(now),
		AllowedDeptIDs: []uint{1}, KnownNodeIDs: []int64{10}, AccessibleNodeIDs: []int64{10},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Len(t, page.Files, 1)
	require.Equal(t, uint64(2), page.Files[0].ID)
}

func TestCatalogRepoAvailabilityFiltersAndScopedDetail(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:catalog-availability?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbRecordingFile{}))
	now := time.Now().UTC()
	available := catalogFile(11, 1, 10, now, "available.mp4")
	offline := catalogFile(12, 1, 20, now, "offline.mp4")
	missing := catalogFile(13, 1, 10, now, "missing.mp4")
	missing.MissingAt = ptrTime(now)
	nodeMissing := catalogFile(14, 1, 30, now, "node-missing.mp4")
	for _, file := range []models.GbRecordingFile{available, offline, missing, nodeMissing} {
		require.NoError(t, db.Create(&file).Error)
	}
	repo := NewGormRepo(db)
	base := FileQuery{Page: 1, PageSize: 20, AllowedDeptIDs: []uint{1}, KnownNodeIDs: []int64{10, 20}, OfflineNodeIDs: []int64{20}, AccessibleNodeIDs: []int64{10}}
	for status, wantID := range map[string]uint64{
		AvailabilityAvailable: 11, AvailabilityNodeOffline: 12, AvailabilityFileMissing: 13, AvailabilityNodeMissing: 14,
	} {
		query := base
		query.Availability = status
		page, queryErr := repo.ListCatalogFiles(context.Background(), query)
		require.NoError(t, queryErr, status)
		require.EqualValues(t, 1, page.Total, status)
		require.Equal(t, wantID, page.Files[0].ID, status)
	}
	_, err = repo.GetCatalogFile(context.Background(), 11, []uint{2}, false)
	require.ErrorIs(t, err, ErrRecordingFileNotFound)
}

func TestCatalogRepoDefaultsListWithoutDateFilter(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:catalog-default-window?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbRecordingFile{}))
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	recent := catalogFile(21, 1, 10, now.Add(-time.Hour), "recent.mp4")
	old := catalogFile(22, 1, 10, now.Add(-25*time.Hour), "old.mp4")
	require.NoError(t, db.Create(&recent).Error)
	require.NoError(t, db.Create(&old).Error)

	repo := NewGormRepo(db)
	page, err := repo.ListCatalogFiles(context.Background(), FileQuery{FullAccess: true})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.Len(t, page.Files, 2)
	require.Equal(t, uint64(21), page.Files[0].ID)
	require.Equal(t, uint64(22), page.Files[1].ID)
}

func TestCatalogRepoDeletesOneFileByID(t *testing.T) {
	db := newRepoTestDB(t)
	now := time.Now().UTC()
	file := catalogFile(71, 10, 1, now, "delete-me.mp4")
	require.NoError(t, db.Create(&file).Error)
	repo := NewGormRepo(db)

	require.NoError(t, repo.DeleteCatalogFile(context.Background(), file.ID))
	_, err := repo.GetCatalogFile(context.Background(), file.ID, nil, true)
	require.ErrorIs(t, err, ErrRecordingFileNotFound)
}

func TestCatalogRepoOptionsUseFileSnapshotsAndFailClosedScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:catalog-options?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbRecordingFile{}))
	now := time.Now().UTC()
	visible := catalogFile(31, 1, 10, now, "visible.mp4")
	visible.ChannelName = "大厅"
	visible.DeviceName = "一号设备"
	hidden := catalogFile(32, 2, 20, now, "hidden.mp4")
	require.NoError(t, db.Create(&visible).Error)
	require.NoError(t, db.Create(&hidden).Error)

	repo := NewGormRepo(db)
	var optionQueries []string
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("capture_catalog_option_sql", func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "SELECT DISTINCT") {
			optionQueries = append(optionQueries, tx.Statement.SQL.String())
		}
	}))
	options, err := repo.CatalogOptions(context.Background(), FileQuery{AllowedDeptIDs: []uint{1}})
	require.NoError(t, err)
	require.Equal(t, []int64{10}, options.NodeIDs)
	require.Len(t, optionQueries, 1)
	require.NotContains(t, optionQueries[0], "channel_name")
	require.NotContains(t, optionQueries[0], "device_name")

	closed, err := repo.CatalogOptions(context.Background(), FileQuery{})
	require.NoError(t, err)
	require.Empty(t, closed.NodeIDs)
}

func TestCatalogAvailabilityUsesDocumentedPriority(t *testing.T) {
	now := time.Now().UTC()
	file := catalogFile(41, 1, 10, now, "priority.mp4")
	file.MissingAt = &now

	require.Equal(t, AvailabilityNodeMissing, CatalogAvailability(file, nil, nil, nil))
	require.Equal(t, AvailabilityNodeOffline, CatalogAvailability(file, []int64{10}, []int64{10}, nil))
	require.Equal(t, AvailabilityFileMissing, CatalogAvailability(file, []int64{10}, nil, nil))
	file.MissingAt = nil
	require.Equal(t, AvailabilityAccessUnavailable, CatalogAvailability(file, []int64{10}, nil, nil))
	require.Equal(t, AvailabilityAvailable, CatalogAvailability(file, []int64{10}, nil, []int64{10}))
}

func TestCatalogRepoListsScopedActiveSessionsAndReconciliations(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:catalog-active?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}, &models.GbRecordingSession{}, &models.GbRecordingReconcileState{}))
	now := time.Now().UTC()
	visibleChannel := models.GbChannel{ID: 1, ChannelID: "C1", DeviceID: "D1", Name: "大厅", OwnerDeptID: 10}
	hiddenChannel := models.GbChannel{ID: 2, ChannelID: "C2", DeviceID: "D2", Name: "库房", OwnerDeptID: 20}
	require.NoError(t, db.Create(&visibleChannel).Error)
	require.NoError(t, db.Create(&hiddenChannel).Error)
	for _, session := range []models.GbRecordingSession{
		{ID: 1, ChannelID: 1, DeviceID: "D1", NodeID: 11, VHost: "v", App: "a", Stream: "private-1", State: models.RecordingSessionStateRecording, StartedAt: &now},
		{ID: 2, ChannelID: 2, DeviceID: "D2", NodeID: 12, VHost: "v", App: "a", Stream: "private-2", State: models.RecordingSessionStateStarting, StartedAt: &now},
		{ID: 3, ChannelID: 1, DeviceID: "D1", NodeID: 11, VHost: "v", App: "a", Stream: "private-3", State: models.RecordingSessionStateStopped, StartedAt: &now},
	} {
		require.NoError(t, db.Create(&session).Error)
	}
	for _, state := range []models.GbRecordingReconcileState{
		{NodeID: 12, Status: models.RecordingReconcileFailed, UpdatedAt: now.Add(-time.Minute)},
		{NodeID: 11, Status: models.RecordingReconcileSucceeded, UpdatedAt: now},
	} {
		require.NoError(t, db.Create(&state).Error)
	}

	repo := NewGormRepo(db)
	active, err := repo.ListActiveCatalogSessions(context.Background(), []uint{10}, false)
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, uint64(1), active[0].ID)
	require.Equal(t, "大厅", active[0].ChannelName)
	require.Equal(t, "C1", active[0].ChannelCode)
	payload, err := json.Marshal(active[0])
	require.NoError(t, err)
	require.NotContains(t, string(payload), "private-1")

	states, err := repo.ListCatalogReconcileStates(context.Background())
	require.NoError(t, err)
	require.Equal(t, []int64{11, 12}, []int64{states[0].NodeID, states[1].NodeID})
}

func catalogFile(id uint64, dept uint, nodeID int64, start time.Time, name string) models.GbRecordingFile {
	length, size := 60.0, uint64(1024)
	return models.GbRecordingFile{
		ID: id, ChannelID: uint(id), DeviceID: "34020000002000000001", ChannelCode: "34020000001320000001",
		ChannelName: name, OwnerDeptID: dept, NodeID: nodeID, VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: "stream", FileKey: BuildFileKey(nodeID, "/record/"+name), FileName: name, FilePath: "/record/" + name,
		StartTime: &start, TimeLen: &length, FileSize: &size, Source: models.RecordingFileSourceHook, MetadataState: models.RecordingMetadataComplete,
		DiscoveredAt: start, LastSeenAt: &start, CreatedAt: start, UpdatedAt: start,
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
