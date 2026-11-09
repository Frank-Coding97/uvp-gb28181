package cloudrecord

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	deviceCode   = "34020000002000000001"
	channelCode  = "34020000001320000001"
	otherChannel = "34020000001320000002"
)

func fixture(t *testing.T) (*Service, *gorm.DB, resource.DepartmentScope, uint) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sql, err := db.DB()
	require.NoError(t, err)
	sql.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sql.Close() })
	require.NoError(t, db.AutoMigrate(&appmodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbRecordingFile{}))
	active := int8(1)
	parent := uint(10)
	for _, department := range []appmodels.SysDepartment{{BaseModel: appmodels.BaseModel{ID: 10}, Name: "root", Status: &active}, {BaseModel: appmodels.BaseModel{ID: 11}, Name: "child", ParentID: &parent, Status: &active}, {BaseModel: appmodels.BaseModel{ID: 20}, Name: "other", Status: &active}} {
		require.NoError(t, db.Create(&department).Error)
	}
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: deviceCode, OwnerDeptID: 10}).Error)
	channel := gbmodels.GbChannel{DeviceID: deviceCode, ChannelID: channelCode, Name: "入口", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: deviceCode, ChannelID: otherChannel, OwnerDeptID: 10}).Error)
	return NewService(db), db, resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, channel.ID
}
func TestCloudIndexListInfoDatesAndNoStorageSecrets(t *testing.T) {
	service, db, scope, channelID := fixture(t)
	ctx := context.Background()
	first := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	duration := 60.5
	size := uint64(2048)
	missing := first
	rows := []gbmodels.GbRecordingFile{
		{ChannelID: channelID, DeviceID: deviceCode, OwnerDeptID: 10, ChannelCode: channelCode, FileKey: "one", FileName: "safe.mp4", FilePath: "/private/storage/secret.mp4", URL: "http://node-secret/key", NodeID: 99, StartTime: &first, RecordDate: &first, TimeLen: &duration, FileSize: &size, Source: "hook", MetadataState: "complete"},
		{ChannelID: channelID, DeviceID: deviceCode, OwnerDeptID: 10, FileKey: "two", FileName: "missing.mp4", StartTime: &second, RecordDate: &first, MissingAt: &missing, Source: "reconcile", MetadataState: "partial"},
		{ChannelID: channelID, DeviceID: deviceCode, OwnerDeptID: 20, FileKey: "other-dept", StartTime: &first, RecordDate: &first},
		{ChannelID: 0, DeviceID: deviceCode, OwnerDeptID: 10, FileKey: "cache-fragment", StartTime: &first, RecordDate: &first},
	}
	require.NoError(t, db.Create(&rows).Error)
	result, err := service.List(ctx, scope, deviceCode, channelCode, Options{Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, result.Total)
	require.Len(t, result.Items, 1)
	require.True(t, result.Items[0].Missing)
	end := first.Add(time.Minute)
	filtered, err := service.List(ctx, scope, deviceCode, channelCode, Options{StartTime: &first, EndTime: &end})
	require.NoError(t, err)
	require.EqualValues(t, 1, filtered.Total)
	detail, err := service.Info(ctx, scope, deviceCode, channelCode, filtered.Items[0].ID)
	require.NoError(t, err)
	require.Equal(t, "safe.mp4", detail.FileName)
	require.Equal(t, 60.5, *detail.DurationSeconds)
	require.EqualValues(t, 2048, *detail.SizeBytes)
	payload, err := json.Marshal(detail)
	require.NoError(t, err)
	for _, secret := range []string{"private", "node-secret", "filePath", "nodeId", "fileKey", "99"} {
		require.False(t, strings.Contains(string(payload), secret), secret)
	}
	_, err = service.Info(ctx, scope, deviceCode, otherChannel, detail.ID)
	require.ErrorIs(t, err, adapters.ErrNotFound)
	dates, err := service.Dates(ctx, scope, deviceCode, channelCode, DateOptions{StartDate: "2026-11-01", EndDate: "2026-12-01"})
	require.NoError(t, err)
	require.Len(t, dates.Items, 1)
	require.Equal(t, "2026-11-09", dates.Items[0].Date)
	require.EqualValues(t, 2, dates.Items[0].FileCount)
	_, err = service.List(ctx, resource.DepartmentScope{OwnerDeptID: 20, DataScope: resource.DataScopeDepartment}, deviceCode, channelCode, Options{})
	require.ErrorIs(t, err, adapters.ErrNotFound)
	// Current resource ownership is authoritative even if an old index row kept its former owner.
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("id = ?", channelID).Update("owner_dept_id", 20).Error)
	_, err = service.List(ctx, scope, deviceCode, channelCode, Options{})
	require.ErrorIs(t, err, adapters.ErrNotFound)
}
func TestCloudQueryRejectsUnboundedOrMalformedRanges(t *testing.T) {
	service, _, scope, _ := fixture(t)
	ctx := context.Background()
	now := time.Now()
	before := now.Add(-time.Second)
	for _, options := range []Options{{Page: -1}, {PageSize: 101}, {StartTime: &now, EndTime: &before}} {
		_, err := service.List(ctx, scope, deviceCode, channelCode, options)
		require.ErrorIs(t, err, adapters.ErrInvalidRequest)
	}
	for _, options := range []DateOptions{{}, {StartDate: "2026-02-30", EndDate: "2026-03-01"}, {StartDate: "2026-11-10", EndDate: "2026-11-01"}, {StartDate: "2020-01-01", EndDate: "2026-01-01"}} {
		_, err := service.Dates(ctx, scope, deviceCode, channelCode, options)
		require.ErrorIs(t, err, adapters.ErrInvalidRequest)
	}
	_, err := service.Info(ctx, scope, deviceCode, channelCode, "01")
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)
}

func TestCloudDatabaseErrorsDoNotMasqueradeAsEmptyPages(t *testing.T) {
	service, db, scope, _ := fixture(t)
	require.NoError(t, db.Migrator().DropTable(&gbmodels.GbRecordingFile{}))
	_, err := service.List(context.Background(), scope, deviceCode, channelCode, Options{})
	require.ErrorIs(t, err, adapters.ErrUnavailable)
	_, err = service.Info(context.Background(), scope, deviceCode, channelCode, "123")
	require.ErrorIs(t, err, adapters.ErrUnavailable)
	_, err = service.Dates(context.Background(), scope, deviceCode, channelCode, DateOptions{StartDate: "2026-11-01", EndDate: "2026-12-01"})
	require.ErrorIs(t, err, adapters.ErrUnavailable)
}

func TestCloudMissingMetadataRemainsNullAndHistoricalPathsStayPrivate(t *testing.T) {
	service, db, scope, channelID := fixture(t)
	row := gbmodels.GbRecordingFile{ChannelID: channelID, DeviceID: deviceCode, OwnerDeptID: 10, FileKey: "legacy-path", FileName: `C:\private\vault\display.mp4`, FilePath: "/private/physical", MetadataState: "partial"}
	require.NoError(t, db.Create(&row).Error)
	page, err := service.List(context.Background(), scope, deviceCode, channelCode, Options{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "display.mp4", page.Items[0].FileName)
	require.Nil(t, page.Items[0].StartTime)
	require.Nil(t, page.Items[0].DurationSeconds)
	require.Nil(t, page.Items[0].SizeBytes)
	require.Nil(t, page.Items[0].RecordDate)
	dates, err := service.Dates(context.Background(), scope, deviceCode, channelCode, DateOptions{StartDate: "2026-11-01", EndDate: "2026-12-01"})
	require.NoError(t, err)
	require.Empty(t, dates.Items)
}
