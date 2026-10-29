package devicesessions

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/devicecapture"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const librarySecondChannel = "37011200001310000011"

var libraryJPEG = []byte{0xff, 0xd8, 0xff, 0xd9}

// seedLibraryImage 造一张真实落盘的图像库行（库行走 registry 的文件清单，不手写 rel_path）。
func seedLibraryImage(t *testing.T, db *gorm.DB, registry *devicecapture.Registry, channelCode, fileName string, capturedAt time.Time, source string) (uint64, string) {
	t.Helper()
	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", channelCode).First(&channel).Error)
	var device gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", captureTestDevice).First(&device).Error)
	session := registry.Create(devicecapture.CreateRequest{
		OwnerID: "openapi-client:91", ChannelID: strconv.FormatUint(uint64(channel.ID), 10),
		ChannelCode: channelCode, DeviceCode: captureTestDevice, SnapNum: 1, Interval: 1,
	})
	file, err := registry.Upload(session.UploadToken, fileName, "image/jpeg", bytes.NewReader(libraryJPEG))
	require.NoError(t, err)
	sessionID := session.ID
	row := gbmodels.GbChannelSnapshot{
		DeviceID: device.ID, ChannelID: channel.ID, ChannelCode: channelCode,
		FileName: file.Name, RelPath: file.RelPath, Size: file.Size, MD5: file.MD5,
		CapturedAt: capturedAt, Source: source, SessionID: &sessionID,
		CreatedAt: capturedAt, UpdatedAt: capturedAt,
	}
	require.NoError(t, db.Create(&row).Error)
	return uint64(row.ID), file.RelPath
}

func TestSnapshotLibraryListsAndReadsImagesThatBelongToTheChannelScope(t *testing.T) {
	service, _, registry, db, _ := newCaptureTestService(t)
	require.NoError(t, db.Create(&gbmodels.GbChannel{
		DeviceID: captureTestDevice, ChannelID: librarySecondChannel, Status: gbmodels.ChannelStatusOnline,
		OwnerDeptID: 10, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error)
	base := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	firstID, _ := seedLibraryImage(t, db, registry, captureTestChannel, "34020000001320000001_20261020080000000.jpg", base, gbmodels.SnapshotSourceDevice)
	secondID, _ := seedLibraryImage(t, db, registry, captureTestChannel, "34020000001320000001_20261020090000000.jpg", base.Add(time.Hour), gbmodels.SnapshotSourceZLM)
	otherID, _ := seedLibraryImage(t, db, registry, librarySecondChannel, "37011200001310000011_20261020070000000.jpg", base.Add(-time.Hour), gbmodels.SnapshotSourceDevice)

	page, err := service.ListLibrary(context.Background(), captureScope(), captureTestDevice, captureTestChannel, LibraryListOptions{})
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total, "another channel's images must not leak into this channel's library")
	require.Len(t, page.Items, 2)
	require.Equal(t, secondID, page.Items[0].SnapshotID, "newest capture first")
	require.Equal(t, firstID, page.Items[1].SnapshotID)
	require.Equal(t, 20, page.PageSize)
	for _, item := range page.Items {
		require.NotContains(t, item.ContentPath, "37011200001310000011")
	}
	require.Contains(t, page.Items[0].ContentPath, "snapshot-library/")

	filtered, err := service.ListLibrary(context.Background(), captureScope(), captureTestDevice, captureTestChannel, LibraryListOptions{
		To: base.Add(30 * time.Minute).Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), filtered.Total)
	require.Equal(t, firstID, filtered.Items[0].SnapshotID)

	bySource, err := service.ListLibrary(context.Background(), captureScope(), captureTestDevice, captureTestChannel, LibraryListOptions{Source: gbmodels.SnapshotSourceZLM})
	require.NoError(t, err)
	require.Equal(t, int64(1), bySource.Total)
	require.Equal(t, secondID, bySource.Items[0].SnapshotID)

	for _, invalid := range []LibraryListOptions{
		{Source: "unknown"},
		{From: "2026-10-20 08:00:00"},
		{PageSize: maxLibraryPageSize + 1},
		{Page: -1},
		{From: base.Format(time.RFC3339), To: base.Add(-time.Hour).Format(time.RFC3339)},
	} {
		_, err := service.ListLibrary(context.Background(), captureScope(), captureTestDevice, captureTestChannel, invalid)
		require.ErrorIs(t, err, ErrInvalidRequest, "invalid filter %+v must be rejected instead of returning an empty page", invalid)
	}

	// 另一个通道的图不能从本通道读到 —— 即使是同一台设备。
	_, err = service.ReadLibraryImage(context.Background(), captureScope(), captureTestDevice, captureTestChannel, otherID)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = service.ReadLibraryImage(context.Background(), captureScope(), captureTestDevice, librarySecondChannel, secondID)
	require.ErrorIs(t, err, ErrNotFound)
	// 别的部门连通道都解析不出来。
	_, err = service.ListLibrary(context.Background(), resource.DepartmentScope{OwnerDeptID: 11, DataScope: resource.DataScopeDepartment}, captureTestDevice, captureTestChannel, LibraryListOptions{})
	require.ErrorIs(t, err, ErrNotFound)

	content, err := service.ReadLibraryImage(context.Background(), captureScope(), captureTestDevice, captureTestChannel, firstID)
	require.NoError(t, err)
	require.Equal(t, libraryJPEG, content.Bytes)
	require.Equal(t, "image/jpeg", content.ContentType)
}

// ⛔ 这条钉的是语义分界：抓图要设备在线（要下命令），图像库**不要** ——
// 图已经在盘上了，设备离线照样得能翻历史、取图、删除。改回 resolveTarget 就会红。
func TestSnapshotLibraryWorksWhileTheDeviceIsOffline(t *testing.T) {
	service, _, registry, db, _ := newCaptureTestService(t)
	id, _ := seedLibraryImage(t, db, registry, captureTestChannel, "34020000001320000001_20261020080000000.jpg",
		time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC), gbmodels.SnapshotSourceDevice)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", captureTestDevice).Update("status", gbmodels.DeviceStatusOffline).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", captureTestChannel).Update("status", gbmodels.ChannelStatusOffline).Error)

	page, err := service.ListLibrary(context.Background(), captureScope(), captureTestDevice, captureTestChannel, LibraryListOptions{})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	content, err := service.ReadLibraryImage(context.Background(), captureScope(), captureTestDevice, captureTestChannel, id)
	require.NoError(t, err)
	require.Equal(t, libraryJPEG, content.Bytes)
}

func TestSnapshotLibraryRefusesCorruptedFilesInsteadOfReturningTheWrongImage(t *testing.T) {
	service, _, registry, db, root := newCaptureTestService(t)
	id, relPath := seedLibraryImage(t, db, registry, captureTestChannel, "34020000001320000001_20261020080000000.jpg",
		time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC), gbmodels.SnapshotSourceDevice)

	path, err := service.captures.FilePath(relPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte{0xff, 0xd8, 0xff, 0xd9, 0x00}, 0o600))
	_, err = service.ReadLibraryImage(context.Background(), captureScope(), captureTestDevice, captureTestChannel, id)
	require.ErrorIs(t, err, ErrUnavailable, "a file that no longer matches its row must not be served")

	require.NoError(t, os.Remove(path))
	_, err = service.ReadLibraryImage(context.Background(), captureScope(), captureTestDevice, captureTestChannel, id)
	require.ErrorIs(t, err, ErrNotFound, "a missing file is an expected operational state, not a server fault")
	require.DirExists(t, root)
}

func TestSnapshotLibraryDeleteRemovesFilesAndRowsOnlyInsideTheChannel(t *testing.T) {
	service, _, registry, db, _ := newCaptureTestService(t)
	require.NoError(t, db.Create(&gbmodels.GbChannel{
		DeviceID: captureTestDevice, ChannelID: librarySecondChannel, Status: gbmodels.ChannelStatusOnline,
		OwnerDeptID: 10, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error)
	capturedAt := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	mineID, minePath := seedLibraryImage(t, db, registry, captureTestChannel, "34020000001320000001_20261020080000000.jpg", capturedAt, gbmodels.SnapshotSourceDevice)
	otherID, otherPath := seedLibraryImage(t, db, registry, librarySecondChannel, "37011200001310000011_20261020080000000.jpg", capturedAt, gbmodels.SnapshotSourceDevice)
	mineFile, err := service.captures.FilePath(minePath)
	require.NoError(t, err)
	otherFile, err := service.captures.FilePath(otherPath)
	require.NoError(t, err)

	for _, invalid := range [][]uint64{nil, {}, make([]uint64, maxLibraryIDs+1)} {
		_, err := service.DeleteLibraryImages(context.Background(), captureScope(), captureTestDevice, captureTestChannel, invalid)
		require.ErrorIs(t, err, ErrInvalidRequest)
	}

	result, err := service.DeleteLibraryImages(context.Background(), captureScope(), captureTestDevice, captureTestChannel, []uint64{mineID, mineID, otherID, 999999})
	require.NoError(t, err)
	require.Equal(t, 1, result.Deleted, "duplicate ids count once")
	require.Equal(t, 0, result.FilesFailed)
	require.Equal(t, 2, result.Missing, "ids outside this channel and unknown ids are indistinguishable")
	require.NoFileExists(t, mineFile)
	require.FileExists(t, otherFile, "another channel's image must survive")

	var stillThere gbmodels.GbChannelSnapshot
	require.NoError(t, db.Where("id = ?", otherID).First(&stillThere).Error)
	var removed gbmodels.GbChannelSnapshot
	require.Error(t, db.Unscoped().Where("id = ?", mineID).First(&removed).Error, "deletion is physical, not a soft delete flag")

	// 幂等：同一批 id 再删一次是 no-op，不是错误。
	again, err := service.DeleteLibraryImages(context.Background(), captureScope(), captureTestDevice, captureTestChannel, []uint64{mineID})
	require.NoError(t, err)
	require.Equal(t, 0, again.Deleted)
	require.Equal(t, 1, again.Missing)
}
