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

// 设备主键不存在时，add 分支必须判"不在可操作范围内"，且**一条授权都不能落库**。
//
// 回归背景：生产全局回调 MaskNotDataError(app/utils/gormhelper/hook.go) 把
// Statement.RaiseErrorOnNotFound 恒置 false，设备查询的 First 查不到只返回
// nil error + 零值设备。若只信 err 不看主键，就会把"设备不存在"当成"设备可见"
// 继续走，最终写出 device_id 指向不存在设备的孤儿授权（零值设备的 DeletedAt
// 同样会让 addGrant 误判，两处必须一起守）。
func TestServiceApplyRejectsMissingDeviceWithoutWritingGrant(t *testing.T) {
	service, _, targets := seedGrantApplyFixture(t)
	// revisionFor(nil) = 空授权的 SHA256，即真机上"该设备还没有任何共享"的版本号
	emptyRevision := revisionFor(nil)
	result, err := service.Apply(context.Background(), ApplyRequest{
		Items:   []ApplyDevice{{DeviceID: 999999, ExpectedRevision: emptyRevision}},
		Mode:    ApplyModeAdd,
		Targets: targets[:1],
	}, datascope.OwnerDeptAccess{FullAccess: true})
	require.NoError(t, err)
	require.Equal(t, ApplySummary{Requested: 1, Failed: 1}, result.Summary)
	require.Contains(t, result.Results[0].Message, "范围")

	var count int64
	require.NoError(t, service.db.Model(&gbmodels.GbDeviceGrant{}).Count(&count).Error)
	require.EqualValues(t, 0, count, "设备不存在时不得写入共享授权")
}

// 无任何行（含软删）时 add 必须真插入一行 —— 直接复刻"首次共享"在真机上的失败。
func TestServiceApplyAddInsertsWhenNoRowExists(t *testing.T) {
	service, devices, targets := seedGrantApplyFixture(t)
	items := []ApplyDevice{{DeviceID: devices[0].ID, ExpectedRevision: revisionFor(nil)}}

	result, err := service.Apply(context.Background(), ApplyRequest{Items: items, Mode: ApplyModeAdd, Targets: targets[:1]}, datascope.OwnerDeptAccess{FullAccess: true})
	require.NoError(t, err)
	require.Equal(t, ApplySummary{Requested: 1, Changed: 1, Added: 1}, result.Summary)
	require.Equal(t, applyStatusChanged, result.Results[0].Status)

	var rows []gbmodels.GbDeviceGrant
	require.NoError(t, service.db.Where("device_id = ?", devices[0].ID).Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, gbmodels.GrantTargetTypeDept, rows[0].TargetType)
}
