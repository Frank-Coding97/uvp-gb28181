package catalog_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const (
	targetDeviceCode  = "34020000001180000001"
	targetChannelCode = "34020000001310000001"
)

func newAlarmTargetDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbAlarmResource{},
		&gbmodels.GbAlarmResourceParent{},
		&gbmodels.GbAlarmBinding{},
	))
	return db
}

func createAlarmResource(t *testing.T, db *gorm.DB, deviceID uint, code string, parents ...string) gbmodels.GbAlarmResource {
	t.Helper()
	resource := gbmodels.GbAlarmResource{
		OwnerDeptID: 1, DeviceID: deviceID, DeviceCode: targetDeviceCode,
		AlarmCode: code, ResourceType: gbmodels.AlarmResourceInput, TypeCode: "134", Name: code,
	}
	require.NoError(t, db.Create(&resource).Error)
	for _, parent := range parents {
		require.NoError(t, db.Create(&gbmodels.GbAlarmResourceParent{AlarmResourceID: resource.ID, ParentCode: parent}).Error)
	}
	return resource
}

func TestResolveAlarmTargetPrefersManualBinding(t *testing.T) {
	db := newAlarmTargetDB(t)
	direct := createAlarmResource(t, db, 7, "34020000001340000001", targetChannelCode)
	manual := createAlarmResource(t, db, 7, "34020000001340000002", targetDeviceCode)
	require.NoError(t, db.Create(&gbmodels.GbAlarmBinding{
		DeviceID: 7, ChannelCode: targetChannelCode, AlarmResourceID: manual.ID, Source: gbmodels.AlarmBindingSourceManual,
	}).Error)

	got, err := catalog.ResolveAlarmTarget(context.Background(), db, 7, targetDeviceCode, targetChannelCode)
	require.NoError(t, err)
	require.Equal(t, catalog.AlarmTargetResolved, got.Status)
	require.Equal(t, catalog.AlarmTargetSourceManual, got.Source)
	require.NotNil(t, got.Target)
	require.Equal(t, manual.AlarmCode, got.Target.AlarmCode)
	require.NotEqual(t, direct.AlarmCode, got.Target.AlarmCode)
}

func TestResolveAlarmTargetUsesDirectParentBeforeDeviceFallback(t *testing.T) {
	db := newAlarmTargetDB(t)
	direct := createAlarmResource(t, db, 7, "34020000001340000001", targetChannelCode)
	createAlarmResource(t, db, 7, "34020000001340000002", targetDeviceCode)

	got, err := catalog.ResolveAlarmTarget(context.Background(), db, 7, targetDeviceCode, targetChannelCode)
	require.NoError(t, err)
	require.Equal(t, catalog.AlarmTargetResolved, got.Status)
	require.Equal(t, catalog.AlarmTargetSourceDirectParent, got.Source)
	require.NotNil(t, got.Target)
	require.Equal(t, direct.AlarmCode, got.Target.AlarmCode)
}

func TestResolveAlarmTargetUsesOnlyUniqueDeviceInput(t *testing.T) {
	db := newAlarmTargetDB(t)
	resource := createAlarmResource(t, db, 7, "34020000001340000001", "34020000002160000001")

	got, err := catalog.ResolveAlarmTarget(context.Background(), db, 7, targetDeviceCode, targetChannelCode)
	require.NoError(t, err)
	require.Equal(t, catalog.AlarmTargetResolved, got.Status)
	require.Equal(t, catalog.AlarmTargetSourceUniqueDevice, got.Source)
	require.NotNil(t, got.Target)
	require.Equal(t, resource.AlarmCode, got.Target.AlarmCode)
}

func TestResolveAlarmTargetReturnsAmbiguousInsteadOfGuessing(t *testing.T) {
	db := newAlarmTargetDB(t)
	createAlarmResource(t, db, 7, "34020000001340000001", targetChannelCode)
	createAlarmResource(t, db, 7, "34020000001340000002", targetChannelCode)

	got, err := catalog.ResolveAlarmTarget(context.Background(), db, 7, targetDeviceCode, targetChannelCode)
	require.NoError(t, err)
	require.Equal(t, catalog.AlarmTargetAmbiguous, got.Status)
	require.Nil(t, got.Target)
	require.Len(t, got.Candidates, 2)
}

func TestResolveAlarmTargetIgnoresAlarmOutputs(t *testing.T) {
	db := newAlarmTargetDB(t)
	output := gbmodels.GbAlarmResource{
		OwnerDeptID: 1, DeviceID: 7, DeviceCode: targetDeviceCode, AlarmCode: "34020000001350000001",
		ResourceType: gbmodels.AlarmResourceOutput, TypeCode: "135", Name: "输出",
	}
	require.NoError(t, db.Create(&output).Error)

	got, err := catalog.ResolveAlarmTarget(context.Background(), db, 7, targetDeviceCode, targetChannelCode)
	require.NoError(t, err)
	require.Equal(t, catalog.AlarmTargetUnavailable, got.Status)
	require.Nil(t, got.Target)
	require.Empty(t, got.Candidates)
}

func TestResolveAlarmTargetDoesNotCrossLocalDeviceBoundaryForDuplicateCode(t *testing.T) {
	db := newAlarmTargetDB(t)
	local := createAlarmResource(t, db, 7, "34020000001340000001", targetChannelCode)
	createAlarmResource(t, db, 8, "34020000001340000002", targetChannelCode)

	got, err := catalog.ResolveAlarmTarget(context.Background(), db, 7, targetDeviceCode, targetChannelCode)
	require.NoError(t, err)
	require.Equal(t, catalog.AlarmTargetResolved, got.Status)
	require.NotNil(t, got.Target)
	require.Equal(t, local.ID, got.Target.ID)
	require.Len(t, got.Candidates, 1)
}
