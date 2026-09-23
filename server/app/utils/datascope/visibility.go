package datascope

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
)

// VisibilityScope 设备可见性过滤:(归属可见)OR(共享可见)。
//
//   - 归属维度:ownerColumn IN (角色 DataScope 推导的可见部门集合)
//   - 共享维度:deviceIDColumn IN (gb_device_grant 中 target=当前用户 或 target=用户本部门 的设备)
//
// 参数:
//   - ownerColumn: 归属部门列,默认 "owner_dept_id"
//   - deviceIDColumn: 设备主键列(设备表传 "id",通道表传 "device_id",联表传 "device.id")
//
// 全量角色(DataScope=1)/notcheckuser 时不加任何过滤(与 OwnerDeptScope 语义一致)。
func VisibilityScope(c *gin.Context, ownerColumn, deviceIDColumn string) func(db *gorm.DB) *gorm.DB {
	return VisibilityScopeWithDB(c, nil, ownerColumn, deviceIDColumn)
}

// VisibilityScopeWithDB 同上,权限查询走 lookupDB(事务内共享同一连接)。
func VisibilityScopeWithDB(c *gin.Context, lookupDB *gorm.DB, ownerColumn, deviceIDColumn string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		ctx := requestContext(c)
		bindScopeContext(db, ctx)
		lookup := lookupDB
		if lookup == nil {
			lookup = db
		}
		// Keep the DB passed to the scope as the return value. GORM reuses a
		// scoped query (for example Count followed by Find); replacing it with
		// WithContext here returns a clone whose WHERE clauses are not written
		// back to that reused query. Bind the context above in place and use the
		// clone only for permission lookups.
		lookup = lookup.WithContext(ctx)
		if ownerColumn == "" {
			ownerColumn = "owner_dept_id"
		}
		if deviceIDColumn == "" {
			// 默认按设备编码列匹配:gb_device.device_id(20位国标编码)在各表语义一致
			deviceIDColumn = "device_id"
		}

		deptIDs, needFilter := GetOwnerDeptIDsWithDB(c, lookup)
		if !needFilter {
			return db
		}

		grantSub := grantDeviceCodeSubquery(lookup, c)

		if len(deptIDs) == 0 {
			// 归属维度不可见:只剩共享维度(无共享则 fail-closed)
			return db.Where(deviceIDColumn+" IN (?)", grantSub)
		}
		return db.Where(ownerColumn+" IN ? OR "+deviceIDColumn+" IN (?)", deptIDs, grantSub)
	}
}

// grantDeviceCodeSubquery 构造"共享给当前用户或其本部门"的设备**编码**(20位国标ID)子查询。
// grant 表存 gb_device.id,而业务表(设备/通道/告警)按编码列 device_id 关联,故 join gb_device 转换。
func grantDeviceCodeSubquery(db *gorm.DB, c *gin.Context) *gorm.DB {
	ctx := requestContext(c)
	claims := common.GetClaims(c)
	userID := uint(0)
	userDeptID := uint(0)
	if claims != nil {
		userID = claims.UserID
		if deptID, err := getUserDepartmentIDWithDB(ctx, db, userID); err == nil {
			userDeptID = deptID
		}
	}

	return db.Session(&gorm.Session{NewDB: true, Initialized: true}).WithContext(ctx).Model(&gbmodels.GbDeviceGrant{}).
		Select("d.device_id").
		Joins("JOIN gb_device d ON d.id = gb_device_grant.device_id AND d.deleted_at IS NULL").
		Where("gb_device_grant.deleted_at IS NULL AND ((gb_device_grant.target_type = ? AND gb_device_grant.target_id = ?) OR (gb_device_grant.target_type = ? AND gb_device_grant.target_id = ?))",
			gbmodels.GrantTargetTypeUser, userID,
			gbmodels.GrantTargetTypeDept, userDeptID)
}
