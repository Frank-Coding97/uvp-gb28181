package recording

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
)

// installNotDataMask 装上生产同款的 "屏蔽 record-not-found" 回调。
//
// ⛔ 为什么要装：生产每个 gorm 实例都注册了它（app/utils/gormhelper/client.go），
// 自建 sqlite 默认**没有**，于是单测活在"`First` 会返回 ErrRecordNotFound"的理想世界，
// 永远绿 —— 这类静默错分支必须自己把回调装上才抓得到（见 topics/gorm-masked-notfound.md）。
func installNotDataMask(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("disable_raise_record_not_found", gormhelper.MaskNotDataError))
}

func newSnapshotTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.GbDevice{}, &models.GbChannel{}, &models.GbRecordingFile{}))
	installNotDataMask(t, db)
	return db
}

// 复刻 bug：通道存在时，名称快照能正确写入。
func TestLoadRecordingSnapshotsFillsNamesWhenChannelExists(t *testing.T) {
	db := newSnapshotTestDB(t)
	device := &models.GbDevice{DeviceID: "34020000001110000001", Name: "一号 NVR"}
	require.NoError(t, db.Create(device).Error)
	channel := &models.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000001", Name: "正门"}
	require.NoError(t, db.Create(channel).Error)

	file := &models.GbRecordingFile{ChannelID: channel.ID, DeviceID: device.DeviceID, FileKey: "k1", FilePath: "/f1.mp4"}
	require.NoError(t, loadRecordingSnapshots(db, file))

	require.Equal(t, "34020000001320000001", file.ChannelCode)
	require.Equal(t, "正门", file.ChannelName)
	require.Equal(t, "一号 NVR", file.DeviceName)
}

// ⛔ 复刻 bug 本体（2026-10-06 老板实测「设备/通道两列全空」）：
//
//	通道查不到时，loadRecordingSnapshots 里的 `if result.Error == nil` 判据在
//	**装了全局屏蔽回调的生产环境里恒为真** ⇒ 拿着零值结构体当成"查到了"，
//	把 ChannelCode/ChannelName/OwnerDeptID 覆盖成空串，并让 DeviceID 被清空，
//	于是后面查设备那一步直接 return。
//
//	后果：只要文件关联的 channel_id 在 gb_channel 里不存在（设备离线后被清理、
//	目录对账先于通道注册、历史数据导入等），**这两个字段就永远补不回来**。
//
//	反向断言：这里期望"查不到就别覆盖"，所以对不存在的通道断言"保持调用方传入的值不变"。
func TestLoadRecordingSnapshotsKeepsCallerValuesWhenChannelMissing(t *testing.T) {
	db := newSnapshotTestDB(t)

	file := &models.GbRecordingFile{
		ChannelID:999, // gb_channel 里不存在
		DeviceID:  "34020000001110000001",
		FileKey:   "k2",
		FilePath:  "/f2.mp4",
	}
	channelCode, deviceID := file.ChannelCode, file.DeviceID

	require.NoError(t, loadRecordingSnapshots(db, file))

	// 查不到通道 ⇒ 不该把调用方已经有的归属信息冲掉
	require.Equal(t, channelCode, file.ChannelCode, "通道查不到时 ChannelCode 被零值覆盖了")
	require.Equal(t, deviceID, file.DeviceID, "通道查不到时 DeviceID 被清空了")
	require.Equal(t, "", file.ChannelName, "通道确实查不到时，名称应当留空而不是被覆盖成脏数据")
}

// ⛔ 反向断言：钉住"屏蔽回调确实生效"这个前提，防止哪天全局回调被摘掉后
// 这条用例变成永远绿、失去护栏意义。
func TestNotDataErrorMaskIsActuallyInstalled(t *testing.T) {
	db := newSnapshotTestDB(t)
	var channel models.GbChannel
	result := db.First(&channel, uint(999))
	require.NoError(t, result.Error, "回调装上后 First 查不到必须返回 nil error")
	require.Equal(t, int64(0), result.RowsAffected)
	require.Equal(t, uint(0), channel.ID, "查不到时是零值结构体")
}

// ⛔⛔ 补盲区（2026-10-06 真机探针实测发现）：**未装 `MaskNotDataError`** 的 gorm
// 里，`First` 查不到会返回真的 `ErrRecordNotFound`。
// 若代码在 `err != nil` 时就判定"查不到"并直接返回，兜底反查永远走不到 ——
// 开发库实测 32 条空值记录一条都补不上，且**所有单测都绿**（因为单测全装了屏蔽回调）。
// 这条用例故意用「不装回调」的库来钉死这个形态。
func TestLoadRecordingSnapshotsFallsBackWhenNoNotFoundMaskInstalled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.GbDevice{}, &models.GbChannel{}, &models.GbRecordingFile{}))
	//⛔ 刻意**不装** installNotDataMask

	device := &models.GbDevice{DeviceID: "37010301021180000007", Name: "UVP-Sim"}
	require.NoError(t, db.Create(device).Error)
	channel := models.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000001", Name: "后置摄像头"}
	require.NoError(t, db.Create(&channel).Error)

	// 先确认这个环境确实会返回 ErrRecordNotFound（前提断言，否则用例空转）
	var probe models.GbChannel
	require.ErrorIs(t, db.First(&probe, uint(9999)).Error, gorm.ErrRecordNotFound,
		"本用例的前提是「未装屏蔽回调时 First 会真的报错」；若哪天 gorm 行为变了，这里会先红")

	file := &models.GbRecordingFile{
		ChannelID:9999, ChannelCode: "34020000001320000001",
		NodeID: 2, FileKey: "k5", FilePath: "/f5.mp4",
	}
	require.NoError(t, loadRecordingSnapshots(db, file))
	require.Equal(t, "后置摄像头", file.ChannelName, "即便 First 报 not-found，也必须继续按国标编码兜底")
	require.Equal(t, "UVP-Sim", file.DeviceName, "兜底命中后设备名也要补上（DeviceID 由通道带出）")
}

// ⛔ 覆盖第二条写入路径（目录对账）：它也调loadRecordingSnapshots，
// 所以同样的 bug 也会让对账发现的录像丢失归属信息。
func TestUpsertCompleteFileKeepsAttributionWhenChannelMissing(t *testing.T) {
	db := newSnapshotTestDB(t)
	repo := NewGormRepo(db)
	device := &models.GbDevice{DeviceID: "34020000001110000002", Name: "二号 NVR"}
	require.NoError(t, db.Create(device).Error)

	start := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	event := RecordMP4Event{
		VHost: "ipc", App: "rtp", Stream: "s1",
		FileName: "a.mp4", FilePath: "/rtp/s1/a.mp4", URL: "http://x/a.mp4",
		StartTime: start, TimeLen: 60, FileSize: 1024,
	}
	// 归属指向一个库里没有的通道（ChannelID=888），但 DeviceID 有值
	err := repo.UpsertCompleteFile(context.Background(), RecordingFileAttribution{
		SessionID: nil, ChannelID: 888, DeviceID: device.DeviceID, NodeID: 1,
	}, event)
	require.NoError(t, err)

	var stored models.GbRecordingFile
	require.NoError(t, db.Where("file_key = ?", BuildFileKey(1, event.FilePath)).First(&stored).Error)
	require.Equal(t, device.DeviceID, stored.DeviceID, "DeviceID 不该被清空")
}

// ⭐ 修复的核心价值（2026-10-06）：主键失效时**按国标编码 `channel_code` 兜底反查**。
//
// 真实故障形态：`file.ChannelID` 存的是 `gb_channel.id`（自增主键），通道行被删后
// 主键关联彻底失效。但 `channel_code` 存的是国标通道编码（稳定标识），仍在。
// 实测开发库：有编码的 38 条录像，**100% 能靠编码反查回通道和设备**。
//
// 这条用例就是钉住这个兜底：主键指向死行时，必须靠编码把名称救回来。
func TestLoadRecordingSnapshotsFallsBackToChannelCodeWhenPrimaryKeyStale(t *testing.T) {
	db := newSnapshotTestDB(t)
	device := &models.GbDevice{DeviceID: "37010301021180000007", Name: "UVP-Sim"}
	require.NoError(t, db.Create(device).Error)
	channel := &models.GbChannel{
		DeviceID: device.DeviceID, ChannelID: "34020000001320000001", Name: "后置摄像头",
	}
	require.NoError(t, db.Create(channel).Error)

	// ChannelID 指向一个已被删除的行（566），但 ChannelCode 是有效的国标编码
	file := &models.GbRecordingFile{
		ChannelID: 566, ChannelCode: "34020000001320000001",
		DeviceID: device.DeviceID, FileKey: "k3", FilePath: "/f3.mp4",
	}
	require.NoError(t, loadRecordingSnapshots(db, file))

	require.Equal(t, "后置摄像头", file.ChannelName, "主键失效时必须靠国标编码把通道名救回来")
	require.Equal(t, "UVP-Sim", file.DeviceName, "设备名同样要补上")
	// 主键应被刷成当前有效行，不再指向死引用
	require.Equal(t, channel.ID, file.ChannelID, "失效的主键要被修正成有效行的主键")
}

// 反向：编码也对不上时，才是真的查不到 ⇒ 保持调用方传入值不动。
func TestLoadRecordingSnapshotsKeepsValuesWhenBothLookupsFail(t *testing.T) {
	db := newSnapshotTestDB(t)

	file := &models.GbRecordingFile{
		ChannelID: 999, ChannelCode: "not-exist-code",
		DeviceID: "34020000001110000001", FileKey: "k4", FilePath: "/f4.mp4",
	}
	deviceID := file.DeviceID

	require.NoError(t, loadRecordingSnapshots(db, file))

	require.Equal(t, deviceID, file.DeviceID, "两条路都查不到时，DeviceID 也不该被清空")
	require.Equal(t, "not-exist-code", file.ChannelCode, "查不到就别动ChannelCode")
	require.Equal(t, "", file.ChannelName)
}