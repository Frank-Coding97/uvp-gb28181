package handler

import (
	"context"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
	"gorm.io/gorm"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

// 设备上报字符集的端到端回归锁：MESSAGE 入口必须把报文声明的字符集与
// 平台实际判定值落到 gb_device，且值不变时不写库（at 不推进）。
func TestMessageReportsDeviceCharset(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbDeviceStatusEvent{}))
	oldDB, oldConfig := app.GormDbSqlite, app.ConfigYml
	app.GormDbSqlite = db
	app.ConfigYml = keepaliveEndpointConfig{deviceInfoTestConfig{dbType: "sqlite"}, true}
	t.Cleanup(func() { app.GormDbSqlite, app.ConfigYml = oldDB, oldConfig })

	now := time.Now()
	registered := now.Add(-time.Minute)
	expires := now.Add(time.Hour)
	d := gbmodels.GbDevice{DeviceID: "34020000002000000052", IP: "112.25.55.9", Port: 51174, Transport: "UDP", RegisterTime: &registered, RegisterExpireAt: &expires, KeepaliveInterval: 20}
	require.NoError(t, db.Create(&d).Error)

	h := NewMessageHandler(gbconfig.Config{})
	send := func(t *testing.T, body []byte) {
		t.Helper()
		req := recordInfoRequest(body)
		req.RemoveHeader("From")
		req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: d.DeviceID, Host: "3402000000"}})
		req.SetSource("112.25.55.9:12230")
		req.SetTransport("UDP")
		tx := siptest.NewServerTxRecorder(req)
		h.Handle(req, tx)
		require.Len(t, tx.Result(), 1)
	}

	// 1. 真 GB2312 报文：声明与字节一致，两侧都记 GB2312。
	payload, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`<Notify><CmdType>Keepalive</CmdType><SN>1</SN><DeviceID>` + d.DeviceID + `</DeviceID><Status>OK</Status></Notify>`))
	require.NoError(t, err)
	send(t, append([]byte(`<?xml version="1.0" encoding="GB2312"?>`), payload...))
	got, err := gbmodels.FindByDeviceID(context.Background(), d.DeviceID)
	require.NoError(t, err)
	require.Equal(t, "GB2312", got.ReportedCharset)
	require.Equal(t, "GB2312", got.WireCharset)
	require.NotNil(t, got.ReportedCharsetAt)
	firstAt := *got.ReportedCharsetAt

	// 2. 同字符集重复上报：零写放大，at 不推进。
	time.Sleep(10 * time.Millisecond)
	send(t, append([]byte(`<?xml version="1.0" encoding="GB2312"?>`), payload...))
	got, err = gbmodels.FindByDeviceID(context.Background(), d.DeviceID)
	require.NoError(t, err)
	require.True(t, got.ReportedCharsetAt.Equal(firstAt), "值未变化时不得写库")

	// 3. 声明不可信的报文（声明 GB18030、内容 UTF-8，flyvideo 形态）：
	//    声明值与实际判定值分开记录——设备详情页能看出这台设备在撒谎。
	send(t, []byte(`<?xml version="1.0" encoding="GB18030"?><Notify><CmdType>Keepalive</CmdType><SN>2</SN><DeviceID>`+d.DeviceID+`</DeviceID><Status>OK</Status><DeviceName>飞视国标摄像头</DeviceName></Notify>`))
	got, err = gbmodels.FindByDeviceID(context.Background(), d.DeviceID)
	require.NoError(t, err)
	require.Equal(t, "GB18030", got.ReportedCharset)
	require.Equal(t, "UTF-8", got.WireCharset, "内容优先改判必须落到 wire_charset")
	require.True(t, got.ReportedCharsetAt.After(firstAt), "字符集变化时 at 要推进")
}

// 纯 ASCII 无声明的报文不得覆盖设备已上报的字符集事实。
func TestMessageSkipsCharsetUpdateWhenNothingDeclared(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbDeviceStatusEvent{}))
	oldDB, oldConfig := app.GormDbSqlite, app.ConfigYml
	app.GormDbSqlite = db
	app.ConfigYml = keepaliveEndpointConfig{deviceInfoTestConfig{dbType: "sqlite"}, true}
	t.Cleanup(func() { app.GormDbSqlite, app.ConfigYml = oldDB, oldConfig })

	now := time.Now()
	registered := now.Add(-time.Minute)
	expires := now.Add(time.Hour)
	d := gbmodels.GbDevice{DeviceID: "34020000002000000053", IP: "112.25.55.9", Port: 51174, Transport: "UDP", RegisterTime: &registered, RegisterExpireAt: &expires, KeepaliveInterval: 20, ReportedCharset: "GB2312", WireCharset: "GB2312", ReportedCharsetAt: &registered}
	require.NoError(t, db.Create(&d).Error)

	h := NewMessageHandler(gbconfig.Config{})
	req := recordInfoRequest([]byte(`<Notify><CmdType>Keepalive</CmdType><SN>1</SN><DeviceID>` + d.DeviceID + `</DeviceID><Status>OK</Status></Notify>`))
	req.RemoveHeader("From")
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: d.DeviceID, Host: "3402000000"}})
	req.SetSource("112.25.55.9:12230")
	req.SetTransport("UDP")
	h.Handle(req, siptest.NewServerTxRecorder(req))

	got, err := gbmodels.FindByDeviceID(context.Background(), d.DeviceID)
	require.NoError(t, err)
	require.Equal(t, "GB2312", got.ReportedCharset, "无声明报文不能清掉既有事实")
	require.True(t, got.ReportedCharsetAt.Equal(registered))
}
