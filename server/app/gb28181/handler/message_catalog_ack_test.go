package handler

import (
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestCatalogMessageAcknowledgedBeforeIngest(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbCatalogNode{},
		&gbmodels.GbChannel{}, &gbmodels.GbChannelMount{}, &gbmodels.GbAnomalyRecord{}))
	deviceID := "37011200001170000001"
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: deviceID, OwnerDeptID: 1}).Error)

	blocked := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:block_catalog_ingest", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_device" {
			once.Do(func() { close(blocked) })
			<-release
		}
	}))
	SetCatalogPipeline(catalog.New(db))
	catalogAgg.reset()
	t.Cleanup(func() {
		SetCatalogPipeline(nil)
		catalogAgg.reset()
	})

	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "platform", Host: "127.0.0.1"})
	prepareNotifyRequest(req)
	callID := sip.CallIDHeader("catalog-early-ack")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.MESSAGE})
	req.SetBody(catalogResponseBody(1, 1, deviceID, "37011200001320000001"))
	tx := siptest.NewServerTxRecorder(req)
	handler := NewMessageHandler(gbconfig.Config{})
	done := make(chan struct{})
	go func() {
		handler.Handle(req, tx)
		close(done)
	}()
	defer func() {
		close(release)
		<-done
	}()

	select {
	case <-blocked:
	case <-time.After(3 * time.Second):
		t.Fatal("Catalog 入库没有进入阻塞查询")
	}
	require.Len(t, tx.Result(), 1, "入库等待数据库时，SIP MESSAGE 应已收到应答")
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
}
