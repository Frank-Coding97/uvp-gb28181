package device

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

func TestHandleRegisterPersistsReportedAndEffectiveProfile(t *testing.T) {
	db := newProfileTestDB(t)

	_, err := HandleRegister(context.Background(), RegisterInfo{
		DeviceID: "34020000002000000041", Transport: "UDP", IP: "192.0.2.41", Port: 5060,
		Expires: 3600, ReportedVersion: "3.0",
	}, 60)
	require.NoError(t, err)

	var got gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", "34020000002000000041").First(&got).Error)
	require.Equal(t, "3.0", got.ReportedVersion)
	require.Equal(t, gbmodels.ProtocolVersion2022, got.EffectiveVersion)
	require.Equal(t, gbmodels.ProtocolVersionSourceRegister, got.EffectiveVersionSource)
	require.NotNil(t, got.ReportedVersionAt)
	require.NotNil(t, got.EffectiveVersionAt)
}

func TestHandleRegisterMapsAdvertisedVersionsToProfiles(t *testing.T) {
	db := newProfileTestDB(t)
	for i, tc := range []struct {
		raw  string
		want string
	}{
		{raw: "1.0", want: gbmodels.ProtocolVersion2016},
		{raw: "1.1", want: gbmodels.ProtocolVersion2016},
		{raw: "2.0", want: gbmodels.ProtocolVersion2016},
		{raw: "3.0", want: gbmodels.ProtocolVersion2022},
		{raw: "not-a-version", want: gbmodels.ProtocolVersion2016},
	} {
		deviceID := "3402000000200000005" + string(rune('0'+i))
		_, err := HandleRegister(context.Background(), RegisterInfo{
			DeviceID: deviceID, Transport: "UDP", IP: "192.0.2.50", Port: 5060,
			Expires: 3600, ReportedVersion: tc.raw,
		}, 60)
		require.NoError(t, err)

		var got gbmodels.GbDevice
		require.NoError(t, db.Where("device_id = ?", deviceID).First(&got).Error)
		require.Equal(t, tc.raw, got.ReportedVersion)
		require.Equal(t, tc.want, got.EffectiveVersion, "raw=%q", tc.raw)
		require.Equal(t, gbmodels.ProtocolVersionSourceRegister, got.EffectiveVersionSource)
	}
}

func TestHandleRegisterOverridePreservesInProgressOperationSnapshot(t *testing.T) {
	db := newProfileTestDB(t)
	ctx := context.Background()
	deviceID := "34020000002000000042"

	_, err := HandleRegister(ctx, RegisterInfo{DeviceID: deviceID, Transport: "UDP", IP: "192.0.2.42", Port: 5060, Expires: 3600, ReportedVersion: "3.0"}, 60)
	require.NoError(t, err)
	var deviceRow gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", deviceID).First(&deviceRow).Error)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("id = ?", deviceRow.ID).Updates(map[string]any{
		"protocol_override": "2016",
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbPTZOperation{
		OperationID: "profile-op-1", IdempotencyKey: "profile-key-1", DeviceID: deviceRow.ID,
		DeviceCode: deviceID, ChannelID: 1, ChannelCode: "34020000001310000042", CmdType: "DeviceControl",
		Status: gbmodels.PTZOperationSent, ProfileVersion: "2022", ProfileCharset: "GB18030",
	}).Error)

	_, err = HandleRegister(ctx, RegisterInfo{DeviceID: deviceID, Transport: "UDP", IP: "192.0.2.42", Port: 5060, Expires: 3600, ReportedVersion: "3.0"}, 60)
	require.NoError(t, err)

	require.NoError(t, db.Where("device_id = ?", deviceID).First(&deviceRow).Error)
	require.Equal(t, gbmodels.ProtocolVersion2016, deviceRow.EffectiveVersion)
	require.Equal(t, gbmodels.ProtocolVersionSourceOverride, deviceRow.EffectiveVersionSource)
	var operation gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", "profile-op-1").First(&operation).Error)
	require.Equal(t, "2022", operation.ProfileVersion)
	require.Equal(t, "GB18030", operation.ProfileCharset)
}

func newProfileTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousConfig := app.GormDbMysql, app.ConfigYml
	t.Cleanup(func() { app.GormDbMysql, app.ConfigYml = previousDB, previousConfig })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbDeviceStatusEvent{}, &gbmodels.GbChannel{},
		&gbmodels.GbPTZOperation{}, &basemodels.SysDepartment{},
	))
	app.GormDbMysql = db
	app.ConfigYml = testConfig{}
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 1}, Name: "接入池"}).Error)
	return db
}
