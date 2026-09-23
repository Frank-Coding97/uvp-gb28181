package catalog

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// 目录声明这一路（三通路里的 catalog）对 gb_channel 坐标两列的覆盖门禁。
//
// 这三条是本次修复的核心，缺任何一条都会让另外两条通路白做：
//   A. 设备本轮没报坐标（标准里两个元素都是 minOccurs=0）→ **不得清零**已有坐标；
//   B. 人工录入（manual）的修正 → 不被例行目录刷新抹掉；
//   C. 实时位置（mobile）→ 同上（它是比目录更强的"设备现在在哪"）。
//
// 反向锚点：设备本轮**确实**报了新坐标、且当前来源是目录/空 → 必须覆盖。
// 这条不能少 —— 只有"都不覆盖"的实现才能让上面三条全绿，那是把功能做死。

const (
	testCatalogChannelID = "37011200001310000001"
	testCatalogDeviceID  = "37011200001320000001"
)

func newCatalogUpsertDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbChannel{}, &gbmodels.GbCatalogNode{}, &gbmodels.GbChannelMount{},
	))
	return db
}

// upsertTestChannel 跑一次 upsertChannel（无父节点 —— 挂在兜底根下）。
func upsertTestChannel(t *testing.T, db *gorm.DB, item CatalogItem) gbmodels.GbChannel {
	t.Helper()
	_, ch, err := upsertChannel(
		context.Background(), db, 0, testCatalogDeviceID, item, Classification{}, nil,
	)
	require.NoError(t, err)

	var fresh gbmodels.GbChannel
	// ⛔ 读进新结构体：GORM 往已填过的结构体扫描时，库里为 NULL 的可空列不会把指针置回 nil。
	require.NoError(t, db.First(&fresh, ch.ID).Error)
	return fresh
}

func catalogItemWithPosition(lng, lat float64) CatalogItem {
	return CatalogItem{
		DeviceID:  testCatalogChannelID,
		Name:      "园区西门",
		Longitude: lng,
		Latitude:  lat,
		StatusOn:  true,
	}
}

func TestUpsertChannel_PositionSourceOnCreate(t *testing.T) {
	db := newCatalogUpsertDB(t)

	ch := upsertTestChannel(t, db, catalogItemWithPosition(117.05, 36.685))
	assert.InDelta(t, 117.05, ch.Longitude, 1e-6)
	assert.InDelta(t, 36.685, ch.Latitude, 1e-6)
	assert.Equal(t, gbmodels.ChannelPositionSourceCatalog, ch.PositionSource)
	require.NotNil(t, ch.PositionUpdatedAt)
}

// 设备没声明坐标 → 来源留空，而不是写一个"有人报过 0,0"的假来源。
func TestUpsertChannel_NoPositionKeepsEmptySource(t *testing.T) {
	db := newCatalogUpsertDB(t)

	item := catalogItemWithPosition(0, 0)
	item.Name = "无线通道"
	ch := upsertTestChannel(t, db, item)
	assert.Zero(t, ch.Longitude)
	assert.Zero(t, ch.Latitude)
	assert.Empty(t, ch.PositionSource)
	assert.Nil(t, ch.PositionUpdatedAt)
}

// A. 目录刷新不带坐标 → 已有坐标必须原样保留（修复前会被清零）。
func TestUpsertChannel_RefreshWithoutPositionKeepsExisting(t *testing.T) {
	db := newCatalogUpsertDB(t)

	ch := upsertTestChannel(t, db, catalogItemWithPosition(117.05, 36.685))
	require.InDelta(t, 117.05, ch.Longitude, 1e-6)

	// 第二轮的 Item 既不报坐标、也只带一个 Status 变化（UPDATE 事件的典型形态）。
	refresh := CatalogItem{DeviceID: testCatalogChannelID, Name: "园区西门", StatusOn: false}
	ch = upsertTestChannel(t, db, refresh)

	assert.InDelta(t, 117.05, ch.Longitude, 1e-6, "不带坐标的目录刷新不得清零已有坐标")
	assert.InDelta(t, 36.685, ch.Latitude, 1e-6)
	assert.Equal(t, gbmodels.ChannelPositionSourceCatalog, ch.PositionSource)
	assert.False(t, ch.Status == gbmodels.ChannelStatusOnline, "同一轮里 status 仍应照常更新")
}

// 目录本轮报了新坐标、当前来源也是目录 → 正常覆盖（不能让上面那条把功能锁死）。
func TestUpsertChannel_CatalogOverwritesCatalog(t *testing.T) {
	db := newCatalogUpsertDB(t)

	upsertTestChannel(t, db, catalogItemWithPosition(117.05, 36.685))
	ch := upsertTestChannel(t, db, catalogItemWithPosition(118.11, 37.22))

	assert.InDelta(t, 118.11, ch.Longitude, 1e-6)
	assert.InDelta(t, 37.22, ch.Latitude, 1e-6)
	assert.Equal(t, gbmodels.ChannelPositionSourceCatalog, ch.PositionSource)
}

// B. 人工录入的坐标不被目录刷新覆盖 —— 否则"手动填一个"就是个一次性动作。
func TestUpsertChannel_CatalogDoesNotClobberManualPosition(t *testing.T) {
	db := newCatalogUpsertDB(t)

	ch := upsertTestChannel(t, db, catalogItemWithPosition(117.05, 36.685))
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("id = ?", ch.ID).Updates(map[string]any{
		"longitude":       121.47,
		"latitude":        31.23,
		"position_source": gbmodels.ChannelPositionSourceManual,
	}).Error)

	ch = upsertTestChannel(t, db, catalogItemWithPosition(118.11, 37.22))

	assert.InDelta(t, 121.47, ch.Longitude, 1e-6, "人工录入的坐标不得被目录刷新抹掉")
	assert.InDelta(t, 31.23, ch.Latitude, 1e-6)
	assert.Equal(t, gbmodels.ChannelPositionSourceManual, ch.PositionSource, "来源也必须保持 manual")
}

// C. 实时位置同理：它比目录声明更接近"设备现在在哪"。
func TestUpsertChannel_CatalogDoesNotClobberMobilePosition(t *testing.T) {
	db := newCatalogUpsertDB(t)

	ch := upsertTestChannel(t, db, catalogItemWithPosition(117.05, 36.685))
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("id = ?", ch.ID).Updates(map[string]any{
		"longitude":       116.99,
		"latitude":        36.66,
		"position_source": gbmodels.ChannelPositionSourceMobile,
	}).Error)

	ch = upsertTestChannel(t, db, catalogItemWithPosition(118.11, 37.22))

	assert.InDelta(t, 116.99, ch.Longitude, 1e-6, "实时位置不得被目录刷新抹掉")
	assert.Equal(t, gbmodels.ChannelPositionSourceMobile, ch.PositionSource)
}
