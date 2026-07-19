package subscribe

import (
	"context"
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
