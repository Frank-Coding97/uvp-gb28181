package handler

import (
	"context"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

func TestMessageKeepaliveEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name, sender, source, transport         string
		unregistered, expired, disabled, reject bool
	}{
		{name: "NAT port changes", source: "112.25.55.9:12230", transport: "UDP"},
		{name: "TCP reconnect", source: "112.25.55.9:12230", transport: "TCP"},
		{name: "disabled recovery still refreshes route", source: "112.25.55.9:12230", transport: "UDP", disabled: true},
		{name: "forged XML identity", sender: "another-device", source: "112.25.55.9:12230", transport: "UDP", reject: true},
		{name: "missing From identity", sender: "-", source: "112.25.55.9:12230", transport: "UDP", reject: true},
		{name: "other IP", source: "112.25.55.12:12230", transport: "UDP", reject: true},
		{name: "other transport", source: "112.25.55.9:12230", transport: "TLS", reject: true},
		{name: "invalid port", source: "112.25.55.9:0", transport: "UDP", reject: true},
		{name: "unregistered", source: "112.25.55.9:12230", transport: "UDP", unregistered: true, reject: true},
		{name: "expired registration", source: "112.25.55.9:12230", transport: "UDP", expired: true, reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })
			require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbDeviceStatusEvent{}))
			oldDB, oldConfig := app.GormDbSqlite, app.ConfigYml
			app.GormDbSqlite = db
			app.ConfigYml = keepaliveEndpointConfig{deviceInfoTestConfig: deviceInfoTestConfig{dbType: "sqlite"}, enabled: !tc.disabled}
			t.Cleanup(func() { app.GormDbSqlite, app.ConfigYml = oldDB, oldConfig })
			now := time.Now()
			registered := now.Add(-time.Minute)
			heartbeat := now.Add(-20 * time.Second)
			expires := now.Add(time.Hour)
			if tc.expired {
				expires = now.Add(-time.Second)
			}
			d := gbmodels.GbDevice{DeviceID: "34020000002000000052", IP: "112.25.55.9", Port: 51174, Transport: tc.transport, RegisterTime: &registered, RegisterExpireAt: &expires, KeepaliveTime: &heartbeat, KeepaliveInterval: 20}
			if tc.transport == "TLS" {
				d.Transport = "UDP"
			}
			if tc.unregistered {
				d.RegisterTime = nil
			}
			require.NoError(t, db.Create(&d).Error)
			req := recordInfoRequest([]byte(`<Notify><CmdType>Keepalive</CmdType><SN>1</SN><DeviceID>` + d.DeviceID + `</DeviceID><Status>OK</Status></Notify>`))
			sender := tc.sender
			if sender == "" {
				sender = d.DeviceID
			}
			req.RemoveHeader("From")
			if sender != "-" {
				req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: sender, Host: "3402000000"}})
			}
			req.SetSource(tc.source)
			req.SetTransport(tc.transport)
			h := NewMessageHandler(gbconfig.Config{})
			tx := siptest.NewServerTxRecorder(req)
			h.Handle(req, tx)
			require.Len(t, tx.Result(), 1)
			got, err := gbmodels.FindByDeviceID(context.Background(), d.DeviceID)
			require.NoError(t, err)
			require.NotNil(t, got)
			var events int64
			require.NoError(t, db.Model(&gbmodels.GbDeviceStatusEvent{}).Count(&events).Error)
			if tc.reject {
				require.Equal(t, d.Port, got.Port)
				require.True(t, got.KeepaliveTime.Equal(heartbeat))
				require.Equal(t, d.Status, got.Status)
				require.Zero(t, events)
				return
			}
			require.Equal(t, 12230, got.Port, "pointing at the old REGISTER port reproduces the production outage")
			require.True(t, got.KeepaliveTime.After(heartbeat))
			if tc.disabled {
				require.Equal(t, gbmodels.DeviceStatusOffline, got.Status)
				require.Zero(t, events)
			} else {
				require.Equal(t, gbmodels.DeviceStatusOnline, got.Status)
				require.EqualValues(t, 1, events)
			}
			if tc.transport == "UDP" {
				req.SetSource("112.25.55.9:63404")
				h.Handle(req, siptest.NewServerTxRecorder(req))
				got, err = gbmodels.FindByDeviceID(context.Background(), d.DeviceID)
				require.NoError(t, err)
				require.Equal(t, 63404, got.Port)
				require.NoError(t, db.Model(&gbmodels.GbDeviceStatusEvent{}).Count(&events).Error)
				if !tc.disabled {
					require.EqualValues(t, 1, events, "no duplicate recovery event")
				}
			}
		})
	}
}

type keepaliveEndpointConfig struct {
	deviceInfoTestConfig
	enabled bool
}

func (c keepaliveEndpointConfig) Get(key string) interface{} {
	if key == gbconfig.OnlineOnHeartbeatConfigKey {
		return c.enabled
	}
	return nil
}
func (c keepaliveEndpointConfig) GetBool(key string) bool {
	return key == gbconfig.OnlineOnHeartbeatConfigKey && c.enabled
}
