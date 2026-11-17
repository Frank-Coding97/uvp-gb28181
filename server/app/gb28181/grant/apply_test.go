package grant

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"
	"uvplatform.com/uvp-gb28181/app/utils/datascope"
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

func TestServiceApplyRemoveRevokesCreatorsInSameTransaction(t *testing.T) {
	service, devices, targets := seedGrantApplyFixture(t)
	require.NoError(t, service.db.Create(&gbmodels.GbDeviceGrant{DeviceID: devices[0].ID, TargetType: targets[0].Type, TargetID: targets[0].ID}).Error)
	items := []ApplyDevice{{DeviceID: devices[0].ID, ExpectedRevision: revisionFor([]gbmodels.GbDeviceGrant{{DeviceID: devices[0].ID, TargetType: targets[0].Type, TargetID: targets[0].ID}})}}
	var affected []uint
	service.SetShareRevoker(func(_ context.Context, tx *gorm.DB, actorIDs []uint) error {
		var count int64
		if err := tx.Model(&gbmodels.GbDeviceGrant{}).Where("device_id = ?", devices[0].ID).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count, "grant removal is visible inside its transaction")
		affected = actorIDs
		return nil
	})

	result, err := service.Apply(context.Background(), ApplyRequest{Items: items, Mode: ApplyModeRemove, Targets: targets[:1]}, datascope.OwnerDeptAccess{})
	require.NoError(t, err)
	require.Equal(t, 1, result.Summary.Removed)
	require.Equal(t, []uint{7}, affected)
}

func TestServiceApplyRemoveRollsBackWhenShareRevocationFails(t *testing.T) {
	service, devices, targets := seedGrantApplyFixture(t)
	grant := gbmodels.GbDeviceGrant{DeviceID: devices[0].ID, TargetType: targets[1].Type, TargetID: targets[1].ID}
	require.NoError(t, service.db.Create(&grant).Error)
	items := []ApplyDevice{{DeviceID: devices[0].ID, ExpectedRevision: revisionFor([]gbmodels.GbDeviceGrant{grant})}}
	service.SetShareRevoker(func(context.Context, *gorm.DB, []uint) error { return errors.New("revocation unavailable") })

	result, err := service.Apply(context.Background(), ApplyRequest{Items: items, Mode: ApplyModeRemove, Targets: targets[1:]}, datascope.OwnerDeptAccess{})
	require.NoError(t, err)
	require.Equal(t, 1, result.Summary.Failed)
	var count int64
	require.NoError(t, service.db.Model(&gbmodels.GbDeviceGrant{}).Where("id = ?", grant.ID).Count(&count).Error)
	require.EqualValues(t, 1, count, "failed share revocation must roll back grant deletion")
}

func TestServiceApplyRechecksOwnerAccessAfterAcquiringGate(t *testing.T) {
	service, devices, targets := seedGrantApplyFixture(t)
	require.NoError(t, service.db.AutoMigrate(&basemodels.SysRole{}, &basemodels.SysUserRole{}))
	role := basemodels.SysRole{Name: "grant-operator", Status: 1, DataScope: 1}
	require.NoError(t, service.db.Create(&role).Error)
	require.NoError(t, service.db.Create(&basemodels.SysUserRole{UserID: 7, RoleID: role.ID}).Error)
	oldAccess, err := datascope.ResolveOwnerDeptAccessByUserID(context.Background(), service.db, 7)
	require.NoError(t, err)
	require.True(t, oldAccess.FullAccess)
	deviceReads, gateAcquired, resolverCalls := 0, false, 0
	require.NoError(t, service.db.Callback().Query().Before("gorm:query").Register("recheck_before_device", func(query *gorm.DB) {
		if query.Statement.Table == "gb_device" {
			deviceReads++
			require.True(t, resolverCalls > 0, "device must not be queried before current access is resolved")
		}
	}))
	require.NoError(t, service.db.Callback().Update().After("gorm:update").Register("revoke_before_owner_resolution", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_video_share_authority" && tx.Error == nil {
			gateAcquired = true
			require.NoError(t, tx.Session(&gorm.Session{NewDB: true}).Model(&basemodels.SysRole{}).Where("id = ?", role.ID).Update("status", 0).Error)
		}
	}))
	service.SetOwnerAccessResolver(func(ctx context.Context, tx *gorm.DB, actorID uint) (datascope.OwnerDeptAccess, error) {
		resolverCalls++
		require.True(t, gateAcquired, "the gate must be held before resolving current roles")
		_, active := tx.Statement.ConnPool.(gorm.TxCommitter)
		require.True(t, active)
		require.Equal(t, uint(7), actorID)
		return datascope.ResolveOwnerDeptAccessByUserID(ctx, tx, actorID)
	})
	result, err := service.Apply(context.Background(), ApplyRequest{Items: []ApplyDevice{{DeviceID: devices[0].ID, ExpectedRevision: revisionFor(nil)}}, Mode: ApplyModeAdd, Targets: targets[:1], CreatedBy: 7}, oldAccess)
	require.NoError(t, err)
	require.Equal(t, 1, resolverCalls)
	require.Equal(t, ApplySummary{Requested: 1, Failed: 1}, result.Summary)
	require.Zero(t, deviceReads, "revoked actor must be rejected before reading a target device")
	var count int64
	require.NoError(t, service.db.Model(&gbmodels.GbDeviceGrant{}).Count(&count).Error)
	require.Zero(t, count, "the pre-gate full-access snapshot must not authorize a grant")
}

func TestServiceApplyUsesFreshOwnerScopeInsteadOfInputSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name         string
		old, current datascope.OwnerDeptAccess
		wantAdded    int
	}{
		{name: "narrowed current scope rejects formerly allowed target", old: datascope.OwnerDeptAccess{FullAccess: true}, current: datascope.OwnerDeptAccess{DeptIDs: []uint{20}}},
		{name: "fresh full scope replaces obsolete denial", old: datascope.OwnerDeptAccess{}, current: datascope.OwnerDeptAccess{FullAccess: true}, wantAdded: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, devices, targets := seedGrantApplyFixture(t)
			calls := 0
			service.SetOwnerAccessResolver(func(_ context.Context, tx *gorm.DB, actorID uint) (datascope.OwnerDeptAccess, error) {
				calls++
				var gate authoritytx.Gate
				require.NoError(t, tx.First(&gate, 1).Error)
				require.EqualValues(t, 1, gate.Revision)
				require.Equal(t, uint(7), actorID)
				return tc.current, nil
			})
			result, err := service.Apply(context.Background(), ApplyRequest{Items: []ApplyDevice{{DeviceID: devices[0].ID, ExpectedRevision: revisionFor(nil)}}, Mode: ApplyModeAdd, Targets: targets[:1], CreatedBy: 7}, tc.old)
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.Equal(t, tc.wantAdded, result.Summary.Added)
			var count int64
			require.NoError(t, service.db.Model(&gbmodels.GbDeviceGrant{}).Count(&count).Error)
			require.EqualValues(t, tc.wantAdded, count)
		})
	}
}

func TestServiceApplyOwnerResolverFailureNeverFallsBackToOldSnapshot(t *testing.T) {
	for _, failure := range []error{datascope.ErrOwnerDeptAccessDenied, errors.New("private owner database unavailable")} {
		t.Run(failure.Error(), func(t *testing.T) {
			service, devices, targets := seedGrantApplyFixture(t)
			service.SetOwnerAccessResolver(func(context.Context, *gorm.DB, uint) (datascope.OwnerDeptAccess, error) {
				return datascope.OwnerDeptAccess{FullAccess: true}, failure
			})
			result, err := service.Apply(context.Background(), ApplyRequest{Items: []ApplyDevice{{DeviceID: devices[0].ID, ExpectedRevision: revisionFor(nil)}}, Mode: ApplyModeAdd, Targets: targets[:1], CreatedBy: 7}, datascope.OwnerDeptAccess{FullAccess: true})
			require.NoError(t, err)
			require.Equal(t, ApplySummary{Requested: 1, Failed: 1}, result.Summary)
			require.NotContains(t, result.Results[0].Message, "private owner database")
			var count int64
			require.NoError(t, service.db.Model(&gbmodels.GbDeviceGrant{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestServiceApplyOwnerResolverRejectsMissingActorAndKeepsRemovalSemantics(t *testing.T) {
	service, devices, targets := seedGrantApplyFixture(t)
	calls := 0
	service.SetOwnerAccessResolver(func(context.Context, *gorm.DB, uint) (datascope.OwnerDeptAccess, error) {
		calls++
		return datascope.OwnerDeptAccess{FullAccess: true}, nil
	})
	request := ApplyRequest{Items: []ApplyDevice{{DeviceID: devices[0].ID, ExpectedRevision: revisionFor(nil)}}, Mode: ApplyModeAdd, Targets: targets[:1]}
	result, err := service.Apply(context.Background(), request, datascope.OwnerDeptAccess{FullAccess: true})
	require.NoError(t, err)
	require.Equal(t, 1, result.Summary.Failed)
	require.Zero(t, calls, "zero actor must be rejected without allowing a permissive callback to grant access")
	grant := gbmodels.GbDeviceGrant{DeviceID: devices[0].ID, TargetType: targets[0].Type, TargetID: targets[0].ID}
	require.NoError(t, service.db.Create(&grant).Error)
	request.Mode = ApplyModeRemove
	request.Items[0].ExpectedRevision = revisionFor([]gbmodels.GbDeviceGrant{grant})
	result, err = service.Apply(context.Background(), request, datascope.OwnerDeptAccess{})
	require.NoError(t, err)
	require.Equal(t, 1, result.Summary.Removed)
	require.Zero(t, calls, "removing an invalid target must not require an add-target scope")
}

func TestServiceApplyRechecksActiveActorForEachDevice(t *testing.T) {
	service, devices, targets := seedGrantApplyFixture(t)
	require.NoError(t, service.db.AutoMigrate(&basemodels.SysRole{}, &basemodels.SysUserRole{}))
	role := basemodels.SysRole{Name: "grant-operator", Status: 1, DataScope: 1}
	require.NoError(t, service.db.Create(&role).Error)
	require.NoError(t, service.db.Create(&basemodels.SysUserRole{UserID: 7, RoleID: role.ID}).Error)
	gateCount := 0
	require.NoError(t, service.db.Callback().Update().After("gorm:update").Register("disable_actor_between_devices", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_video_share_authority" && tx.Error == nil {
			gateCount++
			if gateCount == 2 {
				require.NoError(t, tx.Session(&gorm.Session{NewDB: true}).Model(&basemodels.User{}).Where("id = ?", 7).Update("status", 0).Error)
			}
		}
	}))
	service.SetOwnerAccessResolver(datascope.ResolveOwnerDeptAccessByUserID)
	result, err := service.Apply(context.Background(), ApplyRequest{
		Items: []ApplyDevice{{DeviceID: devices[0].ID, ExpectedRevision: revisionFor(nil)}, {DeviceID: devices[1].ID, ExpectedRevision: revisionFor(nil)}},
		Mode:  ApplyModeAdd, Targets: targets[:1], CreatedBy: 7,
	}, datascope.OwnerDeptAccess{FullAccess: true})
	require.NoError(t, err)
	require.Equal(t, ApplySummary{Requested: 2, Changed: 1, Failed: 1, Added: 1}, result.Summary)
	var grants []gbmodels.GbDeviceGrant
	require.NoError(t, service.db.Find(&grants).Error)
	require.Len(t, grants, 1)
	require.Equal(t, devices[0].ID, grants[0].DeviceID)
}
