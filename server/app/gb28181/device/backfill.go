package device

import (
	"context"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// BackfillZeroOwnerDept 把存量 owner_dept_id=0 的设备回填到默认部门(幂等)。
// 历史遗留:手动创建 bug 与早期自动注册可能留下 owner=0 的设备(除 DataScope=1 外不可见)。
// 返回影响行数;defaultDeptID 由调用方从配置解析并校验(见 defaultOwnerDeptIDWithDB)。
func BackfillZeroOwnerDept(ctx context.Context, db *gorm.DB, defaultDeptID uint) (int64, error) {
	if defaultDeptID == 0 {
		return 0, nil
	}
	res := db.WithContext(ctx).Model(&gbmodels.GbDevice{}).
		Where("owner_dept_id = ?", 0).
		UpdateColumns(map[string]interface{}{"owner_dept_id": defaultDeptID})
	return res.RowsAffected, res.Error
}

// BackfillZeroOwnerDeptFromConfig 从配置解析默认部门并执行回填。
// 配置缺失/部门无效时返回错误(不静默跳过,与自动注册同策略 fail-fast)。
func BackfillZeroOwnerDeptFromConfig(ctx context.Context, db *gorm.DB) (int64, error) {
	deptID, err := defaultOwnerDeptIDWithDB(ctx, db)
	if err != nil {
		return 0, err
	}
	return BackfillZeroOwnerDept(ctx, db, deptID)
}
