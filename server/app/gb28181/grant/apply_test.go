package grant

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

func seedGrantApplyFixture(t *testing.T) (*Service, []gbmodels.GbDevice, []ApplyTarget) {
	t.Helper()
	db := newGrantServiceTestDB(t)
	active := int8(1)
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 10}, Name: "安保部", Status: &active}).Error)
	require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: 7}, Username: "guard", Password: "x", Status: 1, DeptID: 10}).Error)
	devices := []gbmodels.GbDevice{
		{DeviceID: "apply-a", Name: "设备 A", OwnerDeptID: 10},
		{DeviceID: "apply-b", Name: "设备 B", OwnerDeptID: 10},
	}
	require.NoError(t, db.Create(&devices).Error)
	return NewService(db, nil), devices, []ApplyTarget{
		{Type: gbmodels.GrantTargetTypeDept, ID: 10},
		{Type: gbmodels.GrantTargetTypeUser, ID: 7},
	}
}

func queryApplyItems(t *testing.T, service *Service, devices []gbmodels.GbDevice) []ApplyDevice {
	t.Helper()
	ids := make([]uint, 0, len(devices))
	for _, device := range devices {
		ids = append(ids, device.ID)
	}
	state, err := service.Query(context.Background(), ids)
	require.NoError(t, err)
	items := make([]ApplyDevice, 0, len(state.Devices))
	for _, device := range state.Devices {
		items = append(items, ApplyDevice{DeviceID: device.DeviceID, ExpectedRevision: device.Revision})
	}
	return items
}

func TestServiceApplyAddsAndSkipsIdempotently(t *testing.T) {
	service, devices, targets := seedGrantApplyFixture(t)
	items := queryApplyItems(t, service, devices)
	result, err := service.Apply(context.Background(), ApplyRequest{Items: items, Mode: ApplyModeAdd, Targets: targets, CreatedBy: 99}, datascope.OwnerDeptAccess{FullAccess: true})
	require.NoError(t, err)
	require.Equal(t, ApplySummary{Requested: 2, Changed: 2, Added: 4}, result.Summary)
	require.Len(t, result.Results, 2)

	retryItems := make([]ApplyDevice, 0, len(result.Results))
	for _, item := range result.Results {
		retryItems = append(retryItems, ApplyDevice{DeviceID: item.DeviceID, ExpectedRevision: item.Revision})
	}
	retry, err := service.Apply(context.Background(), ApplyRequest{Items: retryItems, Mode: ApplyModeAdd, Targets: targets, CreatedBy: 99}, datascope.OwnerDeptAccess{FullAccess: true})
	require.NoError(t, err)
	require.Equal(t, ApplySummary{Requested: 2, Skipped: 2, RelationsSkipped: 4}, retry.Summary)

	var current []gbmodels.GbDevice
	require.NoError(t, service.db.Order("id ASC").Find(&current).Error)
	require.EqualValues(t, 10, current[0].OwnerDeptID)
	require.EqualValues(t, 10, current[1].OwnerDeptID)
}

func TestServiceApplyRemoveAllowsMissingAndInvalidTargets(t *testing.T) {
	service, devices, targets := seedGrantApplyFixture(t)
	require.NoError(t, service.db.Create(&gbmodels.GbDeviceGrant{DeviceID: devices[0].ID, TargetType: targets[0].Type, TargetID: targets[0].ID}).Error)
	require.NoError(t, service.db.Model(&basemodels.SysDepartment{}).Where("id = ?", 10).Update("status", 0).Error)
	items := queryApplyItems(t, service, devices)

	result, err := service.Apply(context.Background(), ApplyRequest{Items: items, Mode: ApplyModeRemove, Targets: targets[:1]}, datascope.OwnerDeptAccess{})
	require.NoError(t, err)
	require.Equal(t, ApplySummary{Requested: 2, Changed: 1, Skipped: 1, Removed: 1, RelationsSkipped: 1}, result.Summary)
}

func TestServiceApplyIsolatesStaleAndInvisibleDevices(t *testing.T) {
	service, devices, targets := seedGrantApplyFixture(t)
	items := queryApplyItems(t, service, devices)
	require.NoError(t, service.db.Create(&gbmodels.GbDeviceGrant{DeviceID: devices[0].ID, TargetType: targets[0].Type, TargetID: targets[0].ID}).Error)
	service.scope = func(query *gorm.DB) *gorm.DB { return query.Where("id <> ?", devices[1].ID) }

	result, err := service.Apply(context.Background(), ApplyRequest{Items: items, Mode: ApplyModeAdd, Targets: targets[1:]}, datascope.OwnerDeptAccess{FullAccess: true})
	require.NoError(t, err)
	require.Equal(t, ApplySummary{Requested: 2, Failed: 2}, result.Summary)
	require.Equal(t, "failed", result.Results[0].Status)
	require.Contains(t, result.Results[0].Message, "修改")
	require.Equal(t, "failed", result.Results[1].Status)
	require.Contains(t, result.Results[1].Message, "范围")
}

func TestServiceApplyRejectsInactiveOrOutOfScopeAddTarget(t *testing.T) {
	service, devices, targets := seedGrantApplyFixture(t)
	items := queryApplyItems(t, service, devices[:1])
	require.NoError(t, service.db.Model(&basemodels.User{}).Where("id = ?", 7).Update("status", 0).Error)

	result, err := service.Apply(context.Background(), ApplyRequest{Items: items, Mode: ApplyModeAdd, Targets: targets[1:]}, datascope.OwnerDeptAccess{DeptIDs: []uint{10}})
	require.NoError(t, err)
	require.Equal(t, ApplySummary{Requested: 1, Failed: 1}, result.Summary)
	require.Contains(t, result.Results[0].Message, "目标")
}
