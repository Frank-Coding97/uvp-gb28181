package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
)

func TestDeviceConfigsReadUsesDispatchTarget(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOnline)
	require.NoError(t, fixture.db.AutoMigrate(&gbmodels.GbDeviceConfig{}))
	observedAt := time.Now().UTC().Truncate(time.Millisecond)
	deviceCode, channelCode := fixture.device.DeviceID, fixture.channel.ChannelID
	for _, row := range []gbmodels.GbDeviceConfig{
		{DeviceID: fixture.device.ID, TargetCode: deviceCode, ConfigType: "BasicParam", PayloadJSON: `{"name":"UVP-Sim","expiration":3600,"heartBeatInterval":60,"heartBeatCount":3}`, ObservedAt: observedAt},
		{DeviceID: fixture.device.ID, TargetCode: channelCode, ConfigType: "BasicParam", PayloadJSON: `{"name":"wrong-channel"}`, ObservedAt: observedAt.Add(time.Hour)},
		{DeviceID: fixture.device.ID, TargetCode: channelCode, ConfigType: "VideoParamOpt", PayloadJSON: `{"downloadSpeed":"1/2/4"}`, ObservedAt: observedAt},
		{DeviceID: fixture.device.ID, TargetCode: deviceCode, ConfigType: "VideoParamOpt", PayloadJSON: `{"downloadSpeed":"wrong-device"}`, ObservedAt: observedAt.Add(time.Hour)},
		{DeviceID: fixture.device.ID + 1, TargetCode: deviceCode, ConfigType: "BasicParam", PayloadJSON: `{"name":"other-device"}`, ObservedAt: observedAt.Add(time.Hour)},
	} {
		require.NoError(t, fixture.db.Create(&row).Error)
	}
	hasData := true
	// 设备级后台回读没有通道 ID，仍应成为基本参数的回读事实。
	require.NoError(t, fixture.db.Create(&gbmodels.GbPTZOperation{
		OperationID: "basic-read", IdempotencyKey: "basic-read", DeviceID: fixture.device.ID,
		DeviceCode: deviceCode, TargetScope: gbmodels.ControlTargetScopeDevice, TargetCode: deviceCode,
		Action: ptz.ActionRefreshDeviceConfigs, CmdType: "ConfigDownload", PayloadJSON: `{"configTypes":["BasicParam"]}`,
		Status: gbmodels.PTZOperationAccepted, ResponseHasData: &hasData, CreatedAt: observedAt,
	}).Error)
	require.NoError(t, fixture.db.Create(&gbmodels.GbPTZOperation{
		OperationID: "channel-read", IdempotencyKey: "channel-read", DeviceID: fixture.device.ID,
		DeviceCode: deviceCode, ChannelID: fixture.channel.ID, ChannelCode: channelCode,
		TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: channelCode,
		Action: ptz.ActionRefreshDeviceConfigs, CmdType: "ConfigDownload", PayloadJSON: `{"configTypes":["VideoParamOpt"]}`,
		Status: gbmodels.PTZOperationTimeout, CreatedAt: observedAt.Add(time.Minute),
	}).Error)
	require.NoError(t, fixture.db.Create(&gbmodels.GbPTZOperation{
		OperationID: "record-plan-read", IdempotencyKey: "record-plan-read", DeviceID: fixture.device.ID,
		DeviceCode: deviceCode, ChannelID: fixture.channel.ID, ChannelCode: channelCode,
		TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: channelCode,
		Action: ptz.ActionRefreshDeviceConfigs, CmdType: "ConfigDownload", PayloadJSON: `{"configTypes":["VideoRecordPlan"]}`,
		Status: gbmodels.PTZOperationAccepted, ResponseHasData: &hasData,
	}).Error)
	// 相同目标编码的其他设备操作不能覆盖当前设备的回读状态。
	require.NoError(t, fixture.db.Create(&gbmodels.GbPTZOperation{
		OperationID: "other-read", IdempotencyKey: "other-read", DeviceID: fixture.device.ID + 1,
		DeviceCode: deviceCode, TargetScope: gbmodels.ControlTargetScopeDevice, TargetCode: deviceCode,
		Action: ptz.ActionRefreshDeviceConfigs, CmdType: "ConfigDownload",
		Status: gbmodels.PTZOperationTimeout, CreatedAt: observedAt.Add(2 * time.Minute),
	}).Error)

	router := gin.New()
	router.GET("/channel/:id/device-configs", fixture.controller.GetChannelDeviceConfigs)
	for _, test := range []struct {
		configType, targetCode, field, value, state string
	}{
		{"BasicParam", deviceCode, "name", "UVP-Sim", "read_ok"},
		{"VideoParamOpt", channelCode, "downloadSpeed", "1/2/4", "failed"},
	} {
		t.Run(test.configType, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet,
				"/channel/"+uintStr(fixture.channel.ID)+"/device-configs?configTypes="+test.configType, nil))
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var envelope struct {
				Code int `json:"code"`
				Data struct {
					TargetCode string `json:"targetCode"`
					List       []struct {
						ConfigType string                 `json:"configType"`
						Payload    map[string]interface{} `json:"payload"`
					} `json:"list"`
					ObservedAt  time.Time                      `json:"observedAt"`
					AbsentTypes []string                       `json:"absentTypes"`
					Reconcile   ptz.DeviceConfigReconcileState `json:"reconcile"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
			require.Zero(t, envelope.Code, response.Body.String())
			require.Equal(t, test.targetCode, envelope.Data.TargetCode)
			require.Len(t, envelope.Data.List, 1)
			require.Equal(t, test.configType, envelope.Data.List[0].ConfigType)
			require.Equal(t, test.value, envelope.Data.List[0].Payload[test.field])
			require.Equal(t, observedAt, envelope.Data.ObservedAt)
			require.Empty(t, envelope.Data.AbsentTypes)
			require.Equal(t, test.state, envelope.Data.Reconcile.State)
		})
	}
}

func TestDeviceConfigsReadDoesNotPairOldSnapshotWithCompletedRead(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOnline)
	require.NoError(t, fixture.db.AutoMigrate(&gbmodels.GbDeviceConfig{}))
	oldTime := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	newTime := oldTime.Add(time.Hour)
	row := gbmodels.GbDeviceConfig{
		DeviceID: fixture.device.ID, TargetCode: fixture.device.DeviceID, ConfigType: "BasicParam",
		PayloadJSON: `{"name":"old"}`, ObservedAt: oldTime,
	}
	require.NoError(t, fixture.db.Create(&row).Error)
	operation := gbmodels.GbPTZOperation{
		OperationID: "in-flight-read", IdempotencyKey: "in-flight-read", DeviceID: fixture.device.ID,
		DeviceCode: fixture.device.DeviceID, TargetScope: gbmodels.ControlTargetScopeDevice,
		TargetCode: fixture.device.DeviceID, Action: ptz.ActionRefreshDeviceConfigs,
		CmdType: "ConfigDownload", PayloadJSON: `{"configTypes":["BasicParam"]}`,
		Status: gbmodels.PTZOperationSent,
	}
	require.NoError(t, fixture.db.Create(&operation).Error)
	// 模拟设备应答恰好在快照 SELECT 完成后提交；旧实现随后读到 accepted，
	// 会让前端拿着旧快照结束轮询。
	committed := false
	const callback = "test:complete_config_read_after_snapshot"
	require.NoError(t, fixture.db.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
		if committed || db.Statement.Table != "gb_device_config" {
			return
		}
		committed = true
		require.NoError(t, fixture.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&row).Updates(map[string]interface{}{
				"payload_json": `{"name":"new"}`, "observed_at": newTime,
				"source_operation_id": operation.OperationID,
			}).Error; err != nil {
				return err
			}
			return tx.Model(&operation).Updates(map[string]interface{}{
				"status": gbmodels.PTZOperationAccepted, "response_has_data": true, "completed_at": newTime,
			}).Error
		}))
	}))
	t.Cleanup(func() { fixture.db.Callback().Query().Remove(callback) })
	router := gin.New()
	router.GET("/channel/:id/device-configs", fixture.controller.GetChannelDeviceConfigs)
	read := func() (string, time.Time) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet,
			"/channel/"+uintStr(fixture.channel.ID)+"/device-configs?configTypes=BasicParam", nil))
		var envelope struct {
			Code int `json:"code"`
			Data struct {
				ObservedAt time.Time                      `json:"observedAt"`
				Reconcile  ptz.DeviceConfigReconcileState `json:"reconcile"`
			} `json:"data"`
		}
		require.Equal(t, http.StatusOK, response.Code)
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
		require.Zero(t, envelope.Code, response.Body.String())
		return envelope.Data.Reconcile.State, envelope.Data.ObservedAt
	}
	state, observedAt := read()
	require.True(t, committed)
	require.Equal(t, "pending", state, "旧快照不能与本轮回读成功同时返回")
	require.Equal(t, oldTime, observedAt)
	state, observedAt = read()
	require.Equal(t, "read_ok", state)
	require.Equal(t, newTime, observedAt)
}
