package subscribe

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newCatalogProcessorDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbCatalogNode{}, &gbmodels.GbChannelMount{}, &gbmodels.GbAnomalyRecord{}))
	return db
}

func TestCatalogProcessor_IngestsInitialAndDeltaNotify(t *testing.T) {
	db := newCatalogProcessorDB(t)
	device := &gbmodels.GbDevice{DeviceID: "34020000002000000001", OwnerDeptID: 1}
	require.NoError(t, db.Create(device).Error)
	p := NewCatalogProcessor(catalog.New(db))
	initial := []byte(`<Notify><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>34020000002000000001</DeviceID><DeviceList Num="1"><Item><DeviceID>37011200001310000001</DeviceID><Name>入口</Name><CivilCode>370112</CivilCode><Status>ON</Status></Item></DeviceList></Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: initial}))
	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", "37011200001310000001").First(&channel).Error)

	deleted := []byte(`<Notify><CmdType>Catalog</CmdType><SN>2</SN><DeviceID>34020000002000000001</DeviceID><DeviceList Num="1"><Item><DeviceID>37011200001310000001</DeviceID><Event>DEL</Event></Item></DeviceList></Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: deleted}))
	require.Error(t, db.Where("id = ?", channel.ID).First(&gbmodels.GbChannel{}).Error)
}

func TestCatalogProcessor_IgnoresOnlyNegativeStatusNotifyWhenConfigured(t *testing.T) {
	db := newCatalogProcessorDB(t)
	device := &gbmodels.GbDevice{DeviceID: "34020000002000000001", OwnerDeptID: 1}
	require.NoError(t, db.Create(device).Error)
	p := NewCatalogProcessor(catalog.New(db))
	p.ignoreOfflineStatusNotify = func() bool { return false }

	initial := []byte(`<Notify><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>34020000002000000001</DeviceID><DeviceList Num="1"><Item><DeviceID>37011200001310000001</DeviceID><Name>入口</Name><CivilCode>370112</CivilCode><Status>ON</Status></Item></DeviceList></Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: initial}))
	offline := []byte(`<Notify><CmdType>Catalog</CmdType><SN>2</SN><DeviceID>34020000002000000001</DeviceID><DeviceList Num="1"><Item><DeviceID>37011200001310000001</DeviceID><Event>OFF</Event></Item></DeviceList></Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: offline}))
	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", "37011200001310000001").First(&channel).Error)
	require.Equal(t, gbmodels.ChannelStatusOffline, channel.Status)

	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", "37011200001310000001").Update("status", gbmodels.ChannelStatusOnline).Error)
	p.ignoreOfflineStatusNotify = func() bool { return true }

	for _, event := range []string{"OFF", "VLOST", "DEFECT"} {
		body := []byte(`<Notify><CmdType>Catalog</CmdType><SN>2</SN><DeviceID>34020000002000000001</DeviceID><DeviceList Num="1"><Item><DeviceID>37011200001310000001</DeviceID><Event>` + event + `</Event></Item></DeviceList></Notify>`)
		require.NoError(t, p.Process(context.Background(), device, Notification{Body: body}))
		channel = gbmodels.GbChannel{}
		require.NoError(t, db.Where("channel_id = ?", "37011200001310000001").First(&channel).Error)
		require.Equal(t, gbmodels.ChannelStatusOnline, channel.Status, event)
	}

	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("channel_id = ?", "37011200001310000001").Update("status", gbmodels.ChannelStatusOffline).Error)
	online := []byte(`<Notify><CmdType>Catalog</CmdType><SN>3</SN><DeviceID>34020000002000000001</DeviceID><DeviceList Num="1"><Item><DeviceID>37011200001310000001</DeviceID><Event>ON</Event></Item></DeviceList></Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: online}))
	channel = gbmodels.GbChannel{}
	require.NoError(t, db.Where("channel_id = ?", "37011200001310000001").First(&channel).Error)
	require.Equal(t, gbmodels.ChannelStatusOnline, channel.Status)
}

func TestCatalogProcessor_AggregatesMultiResponseNotifyBySN(t *testing.T) {
	db := newCatalogProcessorDB(t)
	device := &gbmodels.GbDevice{DeviceID: "34020000002000000001", OwnerDeptID: 1}
	require.NoError(t, db.Create(device).Error)
	p := NewCatalogProcessor(catalog.New(db))

	first := []byte(`<Notify><CmdType>Catalog</CmdType><SN>9</SN><DeviceID>34020000002000000001</DeviceID><SumNum>2</SumNum><DeviceList Num="1"><Item><DeviceID>37011200001310000001</DeviceID><Name>入口</Name><CivilCode>370112</CivilCode><Status>ON</Status></Item></DeviceList></Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: first}))
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Count(&count).Error)
	require.Zero(t, count, "未收齐的订阅通知不能提前落库")

	// 重复第一包不能推进聚合计数。
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: first}))
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Count(&count).Error)
	require.Zero(t, count)

	second := []byte(`<Notify><CmdType>Catalog</CmdType><SN>9</SN><DeviceID>34020000002000000001</DeviceID><SumNum>2</SumNum><DeviceList Num="1"><Item><DeviceID>37011200001310000002</DeviceID><Name>出口</Name><CivilCode>370112</CivilCode><Status>ON</Status></Item></DeviceList></Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: second}))
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

// ────────────────────────── B-2:目录通道属性(附录 A / §9.3.1)端到端锚点 ──────────────────────────
//
// 这几条用例是 **B-1 的配套回归锚点**,跑的是「原始 XML → manscdp 解析 → 适配器 → 入库管道 →
// 目录树挂载」全链路,而不是直接构造 catalog.CatalogItem。B-1 把模拟器的 V2016 分支改成
// 在 `<Info>` 内写 BusinessGroupID(这是 2016 标准要求的正确位置)之后,平台侧若仍只读 Item 层,
// 2016 设备的通道就会**静默退出业务组织挂载**,退化为只按 ParentID/行政区划挂 —— 单测直接造
// catalog.CatalogItem 是发现不了的。故锚点必须落在 XML 边界上。

const (
	b2DeviceID = "34020000002000000001"
	b2GroupID  = "37011200002150000001"
	b2ChanID   = "37011200001320000001"
)

func newCatalogAttributeTest(t *testing.T) (*gorm.DB, *CatalogProcessor, *gbmodels.GbDevice) {
	t.Helper()
	db := newCatalogProcessorDB(t)
	device := &gbmodels.GbDevice{DeviceID: b2DeviceID, OwnerDeptID: 1}
	require.NoError(t, db.Create(device).Error)
	return db, NewCatalogProcessor(catalog.New(db)), device
}

// 业务分组项(2016/2022 都在 Item 层,只是 2016 额外允许把 BusinessGroupID 写进 <Info>)。
func b2GroupItemXML() string {
	return `<Item><DeviceID>` + b2GroupID + `</DeviceID><Name>重点场所</Name><Parental>1</Parental>` +
		`<ParentID>` + b2DeviceID + `</ParentID></Item>`
}

func b2NotifyXML(sn string, items ...string) []byte {
	return []byte(`<Notify><CmdType>Catalog</CmdType><SN>` + sn + `</SN><DeviceID>` + b2DeviceID + `</DeviceID>` +
		`<DeviceList Num="` + strconv.Itoa(len(items)) + `">` + strings.Join(items, "") + `</DeviceList></Notify>`)
}

// 断言通道节点挂在业务分组节点之下(而非直接挂在设备节点下)。
func requireChannelMountedUnderBusinessGroup(t *testing.T, db *gorm.DB) {
	t.Helper()
	var channelNode gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", b2ChanID).First(&channelNode).Error)
	var groupNode gbmodels.GbCatalogNode
	require.NoError(t, db.Where("code = ?", b2GroupID).First(&groupNode).Error)
	require.NotNil(t, channelNode.ParentID,
		"通道节点必须挂在业务分组下;ParentID 为空说明 BusinessGroupID 根本没被解析出来")
	require.Equal(t, groupNode.ID, *channelNode.ParentID,
		"2016 的 BusinessGroupID 在 <Info> 容器内、2022 的在 Item 层,两条路径都必须被读到")
}

func requireChannelAttributes(t *testing.T, db *gorm.DB, want gbmodels.GbChannel) {
	t.Helper()
	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", b2ChanID).First(&channel).Error)
	require.Equal(t, want.PTZType, channel.PTZType, "ptz_type")
	require.Equal(t, want.RoomType, channel.RoomType, "room_type")
	require.Equal(t, want.SupplyLightType, channel.SupplyLightType, "supply_light_type")
	require.Equal(t, want.DirectionType, channel.DirectionType, "direction_type")
	require.Equal(t, want.Resolution, channel.Resolution, "resolution")
	require.Equal(t, want.PositionType, channel.PositionType, "position_type(2016 独有)")
	require.Equal(t, want.UseType, channel.UseType, "use_type(2016 独有)")
	require.Equal(t, want.PhotoelectricImagingType, channel.PhotoelectricImagingType, "photoelectric_imaging_type(2022 独有)")
	require.Equal(t, want.CapturePositionType, channel.CapturePositionType, "capture_position_type(2022 独有)")
}

// 2016 形态:BusinessGroupID 在 <Info> 内;PositionType/UseType 存在;2022 独有字段不出现。
func TestCatalogProcessor_PersistsV2016CatalogAttributesAndKeepsBusinessGroupMount(t *testing.T) {
	db, p, device := newCatalogAttributeTest(t)

	channelItem := `<Item><DeviceID>` + b2ChanID + `</DeviceID><Name>校门</Name><Parental>0</Parental>` +
		`<ParentID>` + b2DeviceID + `</ParentID><Status>ON</Status>` +
		`<Info><PTZType>1</PTZType><PositionType>6</PositionType><RoomType>1</RoomType>` +
		`<UseType>1</UseType><SupplyLightType>2</SupplyLightType><DirectionType>3</DirectionType>` +
		`<Resolution>1920*1080</Resolution><BusinessGroupID>` + b2GroupID + `</BusinessGroupID></Info></Item>`

	require.NoError(t, p.Process(context.Background(), device, Notification{Body: b2NotifyXML("11", b2GroupItemXML(), channelItem)}))

	requireChannelMountedUnderBusinessGroup(t, db)
	requireChannelAttributes(t, db, gbmodels.GbChannel{
		PTZType:         1,
		RoomType:        1,
		SupplyLightType: 2,
		DirectionType:   3,
		Resolution:      "1920*1080",
		PositionType:    6,
		UseType:         1,
		// 2022 独有字段 2016 设备不会报,应保持零值。
		PhotoelectricImagingType: "",
		CapturePositionType:      "",
	})
}

// 2022 形态:PTZType 取 2022 才有的 5(遥控半球);BusinessGroupID 上提到 Item 层;
// PositionType/UseType 已从标准删除,不应出现;PhotoelectricImagingType/CapturePositionType 出现。
func TestCatalogProcessor_PersistsV2022CatalogAttributesAndKeepsBusinessGroupMount(t *testing.T) {
	db, p, device := newCatalogAttributeTest(t)

	channelItem := `<Item><DeviceID>` + b2ChanID + `</DeviceID><Name>球机通道</Name><Parental>0</Parental>` +
		`<ParentID>` + b2DeviceID + `</ParentID><BusinessGroupID>` + b2GroupID + `</BusinessGroupID><Status>ON</Status>` +
		`<Info><PTZType>5</PTZType><PhotoelectricImagingType>1/2</PhotoelectricImagingType>` +
		`<CapturePositionType>3</CapturePositionType><RoomType>2</RoomType>` +
		`<SupplyLightType>4</SupplyLightType><DirectionType>7</DirectionType>` +
		`<Resolution>3840*2160</Resolution></Info></Item>`

	require.NoError(t, p.Process(context.Background(), device, Notification{Body: b2NotifyXML("12", b2GroupItemXML(), channelItem)}))

	requireChannelMountedUnderBusinessGroup(t, db)
	requireChannelAttributes(t, db, gbmodels.GbChannel{
		PTZType:         5, // 2022 新增值域,平台侧不得判为非法或截断
		RoomType:        2,
		SupplyLightType: 4, // 2022 新增的激光补光
		DirectionType:   7,
		Resolution:      "3840*2160",
		// 2016 独有字段 2022 设备不会报,应保持零值。
		PositionType:             0,
		UseType:                  0,
		PhotoelectricImagingType: "1/2",
		CapturePositionType:      "3",
	})
}

// 数据保全:设备这一轮只报 Event=UPDATE 且**不带 <Info>** 时,已落库的属性不能被清零。
// (解析层修好 <Info> 之后 ptz_type 才第一次真能拿到值,若无条件覆盖,这种报文就是新的丢数据点。)
func TestCatalogProcessor_KeepsKnownAttributesWhenUpdateCarriesNoInfo(t *testing.T) {
	db, p, device := newCatalogAttributeTest(t)

	full := `<Item><DeviceID>` + b2ChanID + `</DeviceID><Name>校门</Name><Parental>0</Parental>` +
		`<ParentID>` + b2DeviceID + `</ParentID><Status>ON</Status>` +
		`<Info><PTZType>1</PTZType><RoomType>1</RoomType><SupplyLightType>2</SupplyLightType>` +
		`<DirectionType>3</DirectionType><Resolution>1920*1080</Resolution>` +
		`<UseType>1</UseType><BusinessGroupID>` + b2GroupID + `</BusinessGroupID></Info></Item>`
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: b2NotifyXML("21", b2GroupItemXML(), full)}))

	// 只改名、不带 <Info> 的 UPDATE 事件。
	partial := `<Item><DeviceID>` + b2ChanID + `</DeviceID><Name>校门(改名)</Name><Event>UPDATE</Event></Item>`
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: b2NotifyXML("22", b2GroupItemXML(), partial)}))

	var channel gbmodels.GbChannel
	require.NoError(t, db.Where("channel_id = ?", b2ChanID).First(&channel).Error)
	require.Equal(t, "校门(改名)", channel.Name, "名称应被更新")
	require.Equal(t, int8(1), channel.PTZType, "不带 <Info> 的 UPDATE 不得把 ptz_type 清零")
	require.Equal(t, int8(1), channel.RoomType, "不带 <Info> 的 UPDATE 不得把 room_type 清零")
	require.Equal(t, int8(2), channel.SupplyLightType, "不带 <Info> 的 UPDATE 不得把 supply_light_type 清零")
	require.Equal(t, int8(3), channel.DirectionType, "不带 <Info> 的 UPDATE 不得把 direction_type 清零")
	require.Equal(t, "1920*1080", channel.Resolution, "不带 <Info> 的 UPDATE 不得把 resolution 清空")
	require.Equal(t, int8(1), channel.UseType, "不带 <Info> 的 UPDATE 不得把 use_type 清零")
}
