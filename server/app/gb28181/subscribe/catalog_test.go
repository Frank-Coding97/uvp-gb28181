package subscribe

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

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
