package recording

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newBackfillTestDB(t *testing.T) *gorm.DB {
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

func seedBackfillFixtures(t *testing.T, db *gorm.DB) (valid models.GbChannel, orphan models.GbChannel) {
	t.Helper()
	device := &models.GbDevice{DeviceID: "37010301021180000007", Name: "UVP-Sim"}
	require.NoError(t, db.Create(device).Error)
	valid = models.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000001", Name: "后置摄像头", OwnerDeptID: 4}
	require.NoError(t, db.Create(&valid).Error)
	orphan = models.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000020", Name: "前置摄像头", OwnerDeptID: 4}
	require.NoError(t, db.Create(&orphan).Error)
	return valid, orphan
}

// ⭐ 存量修复的核心用例（2026-10-06）：历史录像的归属名称空了之后，
// 光修写入路径救不回来（那些行不会再被重写），必须有回填。
// 这里模拟真实故障形态：**主键已失效、名称空、但国标编码还在**。
func TestBackfillAttributionRecoversNamesViaChannelCode(t *testing.T) {
	db := newBackfillTestDB(t)
	valid, _ := seedBackfillFixtures(t, db)
	repo := NewGormRepo(db)

	// 一条"主键指向已删除行、名称空、编码有效"的存量录像
	row := models.GbRecordingFile{
		ChannelID: 9999, // 已被清理的通道行
		ChannelCode: "34020000001320000001",
		DeviceID:  "37010301021180000007",
		NodeID:    2, FileKey: "bk1", FilePath: "/rtp/s1/bk1.mp4", FileName: "bk1.mp4",
		Source: "hook", MetadataState: models.RecordingMetadataComplete,
	}
	require.NoError(t, db.Create(&row).Error)

	updated, err := repo.BackfillAttribution(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, 1, updated)

	var got models.GbRecordingFile
	require.NoError(t, db.Where("file_key = ?", "bk1").First(&got).Error)
	require.Equal(t, "后置摄像头", got.ChannelName, "通道名要靠国标编码反查回来")
	require.Equal(t, "UVP-Sim", got.DeviceName, "设备名要一并补上")
	require.Equal(t, valid.ID, got.ChannelID, "失效的主键要修正成有效行")
}

// 幂等：已有名字的记录绝不能被覆盖（避免把运维手工改过的名称刷掉）。
func TestBackfillAttributionIsIdempotentAndKeepsExistingNames(t *testing.T) {
	db := newBackfillTestDB(t)
	valid, _ := seedBackfillFixtures(t, db)
	repo := NewGormRepo(db)

	row := models.GbRecordingFile{
		ChannelID: valid.ID, ChannelCode: "34020000001320000001",
		ChannelName: "运维手工改过的名字", DeviceName: "手工设备名",
		NodeID: 2, FileKey: "bk2", FilePath: "/rtp/s1/bk2.mp4", FileName: "bk2.mp4",
		Source: "hook", MetadataState: models.RecordingMetadataComplete,
	}
	require.NoError(t, db.Create(&row).Error)

	updated, err := repo.BackfillAttribution(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, 0, updated, "名称齐全的行不该被回填触碰")

	var got models.GbRecordingFile
	require.NoError(t, db.Where("file_key = ?", "bk2").First(&got).Error)
	require.Equal(t, "运维手工改过的名字", got.ChannelName)
	require.Equal(t, "手工设备名", got.DeviceName)

	// 再跑一次仍然不该变（幂等）
	updated, err = repo.BackfillAttribution(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, 0, updated, "回填必须幂等")
}

// 只缺设备名（通道还在）的情况也要能补。
func TestBackfillAttributionFillsOnlyDeviceName(t *testing.T) {
	db := newBackfillTestDB(t)
	valid, _ := seedBackfillFixtures(t, db)
	repo := NewGormRepo(db)

	row := models.GbRecordingFile{
		ChannelID: valid.ID, ChannelCode: "34020000001320000001", ChannelName: "后置摄像头",
		NodeID: 2, FileKey: "bk3", FilePath: "/rtp/s1/bk3.mp4", FileName: "bk3.mp4",
		Source: "hook", MetadataState: models.RecordingMetadataComplete,
	}
	require.NoError(t, db.Create(&row).Error)

	updated, err := repo.BackfillAttribution(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, 1, updated)

	var got models.GbRecordingFile
	require.NoError(t, db.Where("file_key = ?", "bk3").First(&got).Error)
	require.Equal(t, "后置摄像头", got.ChannelName, "已有通道名不能变")
	require.Equal(t, "UVP-Sim", got.DeviceName, "缺的设备名要补上")
}

// 没有 channel_code 的行（连稳定标识都没有）必须跳过，不能瞎猜。
func TestBackfillAttributionSkipsRowsWithoutChannelCode(t *testing.T) {
	db := newBackfillTestDB(t)
	seedBackfillFixtures(t, db)
	repo := NewGormRepo(db)

	row := models.GbRecordingFile{
		ChannelID: 9999, ChannelCode: "",
		NodeID: 2, FileKey: "bk4", FilePath: "/rtp/s1/bk4.mp4", FileName: "bk4.mp4",
		Source: "hook", MetadataState: models.RecordingMetadataComplete,
	}
	require.NoError(t, db.Create(&row).Error)

	updated, err := repo.BackfillAttribution(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, 0, updated, "连国标编码都没有的行没法反查，应当跳过")

	var got models.GbRecordingFile
	require.NoError(t, db.Where("file_key = ?", "bk4").First(&got).Error)
	require.Equal(t, uint(9999), got.ChannelID, "查不到就不能乱改主键")
}

// limit 生效：分批回填，不一次性锁全表。
func TestBackfillAttributionRespectsLimit(t *testing.T) {
	db := newBackfillTestDB(t)
	valid, _ := seedBackfillFixtures(t, db)
	repo := NewGormRepo(db)

	for i := 0; i < 3; i++ {
		row := models.GbRecordingFile{
			ChannelID: 9999, ChannelCode: "34020000001320000001",
			NodeID: 2, FileKey: "bk-lim-" + string(rune('a'+i)), FilePath: "/rtp/s1/lim" + string(rune('a'+i)) + ".mp4",
			FileName: "lim.mp4", Source: "hook", MetadataState: models.RecordingMetadataComplete,
			DiscoveredAt: time.Now(), UpdatedAt: time.Now(),
		}
		require.NoError(t, db.Create(&row).Error)
	}
	updated, err := repo.BackfillAttribution(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, 2, updated, "limit=2 时只处理 2 条，剩下留给下一轮")

	// 第二轮补完剩下的
	updated, err = repo.BackfillAttribution(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	require.Equal(t, valid.ID, func() uint {
		var got models.GbRecordingFile
		require.NoError(t, db.Where("file_key = ?", "bk-lim-c").First(&got).Error)
		return got.ChannelID
	}())
}
