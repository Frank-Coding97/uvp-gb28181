package datascope

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/common"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func requestContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		if ctx := c.Request.Context(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// bindScopeContext updates the per-query statement in place so a scope can
// preserve both request cancellation and the query value returned by GORM.
func bindScopeContext(db *gorm.DB, ctx context.Context) {
	if db != nil && db.Statement != nil {
		db.Statement.Context = ensureContext(ctx)
	}
}

// 获取用户角色列表
func getUserRoles(ctx context.Context, userID uint) ([]*models.SysRole, error) {
	return getUserRolesWithDB(ctx, app.DB(), userID)
}

func getUserRolesWithDB(ctx context.Context, db *gorm.DB, userID uint) ([]*models.SysRole, error) {
	db = db.Session(&gorm.Session{NewDB: true, Initialized: true}).WithContext(ensureContext(ctx))
	var user models.User
	err := db.Model(&models.User{}).Preload("Roles").Where("id = ?", userID).First(&user).Error
	if err != nil {
		return nil, err
	}
	return user.Roles, nil
}

// 获取部门及其所有子部门ID
func getDepartmentAndChildrenIDs(departmentTree models.SysDepartmentList, deptID uint) ([]uint, error) {

	if departmentTree.IsEmpty() {
		return []uint{deptID}, nil
	}
	// 递归查找目标部门（支持任意层级）
	var findDepartment func(depts models.SysDepartmentList, targetID uint) *models.SysDepartment
	findDepartment = func(depts models.SysDepartmentList, targetID uint) *models.SysDepartment {
		for _, dept := range depts {
			if dept.ID == targetID {
				return dept
			}
			if len(dept.Children) > 0 {
				if found := findDepartment(dept.Children, targetID); found != nil {
					return found
				}
			}
		}
		return nil
	}

	// 查找目标部门
	targetDept := findDepartment(departmentTree, deptID)

	if targetDept == nil {
		return []uint{deptID}, nil
	}

	// 递归获取所有子部门ID
	var getAllChildrenIDs func(dept *models.SysDepartment, ids *[]uint)
	getAllChildrenIDs = func(dept *models.SysDepartment, ids *[]uint) {
		*ids = append(*ids, dept.ID)
		if len(dept.Children) > 0 {
			for _, child := range dept.Children {
				getAllChildrenIDs(child, ids)
			}
		}
	}

	var deptIDs []uint
	getAllChildrenIDs(targetDept, &deptIDs)
	return deptIDs, nil
}

// 字符串转uint切片
func stringToUintSlice(s string) ([]uint, error) {
	if s == "" {
		return []uint{}, nil
	}

	parts := strings.Split(s, ",")
	var result []uint
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			id, err := strconv.ParseUint(part, 10, 32)
			if err != nil {
				return nil, err
			}
			result = append(result, uint(id))
		}
	}
	return result, nil
}

// 获取用户所属部门ID
func getUserDepartmentID(ctx context.Context, userID uint) (uint, error) {
	return getUserDepartmentIDWithDB(ctx, app.DB(), userID)
}

func getUserDepartmentIDWithDB(ctx context.Context, db *gorm.DB, userID uint) (uint, error) {
	db = db.Session(&gorm.Session{NewDB: true, Initialized: true}).WithContext(ensureContext(ctx))
	var user models.User
	err := db.Model(&models.User{}).Select("dept_id").Where("id = ?", userID).First(&user).Error
	if err != nil {
		return 0, err
	}
	return user.DeptID, nil
}

// 根据部门ID获取用户ID列表
func getUserIDsByDepartmentIDs(ctx context.Context, deptIDs []uint) ([]uint, error) {
	if len(deptIDs) == 0 {
		return []uint{}, nil
	}

	var users []models.User
	err := app.DBContext(ensureContext(ctx)).Select("id").Where("dept_id IN ?", deptIDs).Find(&users).Error
	if err != nil {
		return nil, err
	}

	var userIDs []uint
	for _, user := range users {
		userIDs = append(userIDs, user.ID)
	}
	return userIDs, nil
}

// GetOwnerDeptIDs 返回当前用户可见的部门 ID 集合。
// 第二个返回值为 false 时表示无需加部门过滤(例如测试环境没有 claims,或用户拥有全量权限)。
func GetOwnerDeptIDs(c *gin.Context) ([]uint, bool) {
	return GetOwnerDeptIDsWithDB(c, nil)
}

// GetOwnerDeptIDsWithDB 返回当前用户可见的部门 ID 集合,使用传入 DB 查询用户/角色/部门。
func GetOwnerDeptIDsWithDB(c *gin.Context, db *gorm.DB) ([]uint, bool) {
	ctx := requestContext(c)
	claims := common.GetClaims(c)
	if claims == nil || claims.UserID == 0 {
		return nil, false
	}
	if db == nil {
		db = app.DB()
	}
	userID := claims.UserID
	if app.ConfigYml != nil {
		for _, notCheckUserID := range app.ConfigYml.GetUintSlice("server.notcheckuser") {
			if notCheckUserID == userID {
				return nil, false
			}
		}
	}

	roles, err := getUserRolesWithDB(ctx, db, userID)
	if err != nil {
		return []uint{}, true
	}
	userDeptID, _ := getUserDepartmentIDWithDB(ctx, db, userID)
	if len(roles) == 0 {
		if userDeptID == 0 {
			return []uint{}, true
		}
		return []uint{userDeptID}, true
	}

	var allDepartments models.SysDepartmentList
	departmentTree := models.SysDepartmentList{}
	if err := db.Session(&gorm.Session{NewDB: true, Initialized: true}).WithContext(ctx).Model(&models.SysDepartment{}).Find(&allDepartments).Error; err == nil {
		departmentTree = allDepartments.BuildTree(ctx)
	}

	allowedDeptIDs := make(map[uint]bool)
	for _, role := range roles {
		switch role.DataScope {
		case 1:
			return nil, false
		case 2:
			deptIDs, err := stringToUintSlice(role.CheckedDepts)
			if err == nil {
				for _, deptID := range deptIDs {
					if deptID != 0 {
						allowedDeptIDs[deptID] = true
					}
				}
			}
		case 3, 5:
			if userDeptID != 0 {
				allowedDeptIDs[userDeptID] = true
			}
		case 4:
			if userDeptID != 0 {
				deptIDs, err := getDepartmentAndChildrenIDs(departmentTree, userDeptID)
				if err != nil || len(deptIDs) == 0 {
					allowedDeptIDs[userDeptID] = true
				}
				for _, deptID := range deptIDs {
					if deptID != 0 {
						allowedDeptIDs[deptID] = true
					}
				}
			}
		}
	}

	if len(allowedDeptIDs) == 0 {
		if userDeptID == 0 {
			return []uint{}, true
		}
		return []uint{userDeptID}, true
	}
	deptIDs := make([]uint, 0, len(allowedDeptIDs))
	for deptID := range allowedDeptIDs {
		deptIDs = append(deptIDs, deptID)
	}
	sort.Slice(deptIDs, func(i, j int) bool { return deptIDs[i] < deptIDs[j] })
	return deptIDs, true
}

// OwnerDeptScope 按 owner_dept_id 过滤 GB28181 这类非人工创建数据。
func OwnerDeptScope(c *gin.Context, column string) func(db *gorm.DB) *gorm.DB {
	return OwnerDeptScopeWithDB(c, nil, column)
}

// OwnerDeptScopeWithDB 按 owner_dept_id 过滤,并用 lookupDB 查询当前用户的部门权限。
func OwnerDeptScopeWithDB(c *gin.Context, lookupDB *gorm.DB, column string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		ctx := requestContext(c)
		bindScopeContext(db, ctx)
		lookup := lookupDB
		if lookup == nil {
			lookup = db
		}
		// Do not replace the scoped query with the WithContext clone: callers
		// commonly reuse it for Count and Find, and GORM does not copy the
		// returned clone's clauses back to the original query. Bind the context
		// above in place and use the clone only for permission lookups.
		lookup = lookup.WithContext(ctx)
		deptIDs, needFilter := GetOwnerDeptIDsWithDB(c, lookup)
		if !needFilter {
			return db
		}
		if column == "" {
			column = "owner_dept_id"
		}
		if len(deptIDs) == 0 {
			return db.Where("1 = 0")
		}
		return db.Where(column+" IN ?", deptIDs)
	}
}

// 数据权限(默认可以查看自己创建的数据)
func GetDataScope(c *gin.Context) func(db *gorm.DB) *gorm.DB {
	// 定义数据权限函数
	return func(db *gorm.DB) *gorm.DB {
		ctx := requestContext(c)
		bindScopeContext(db, ctx)
		claims := common.GetClaims(c)
		if claims == nil {
			return db.Where("1 = 0")
		}

		userID := claims.UserID
		if userID == 0 {
			return db.Where("1 = 0")
		}
		notCheckUserIds := app.ConfigYml.GetUintSlice("server.notcheckuser")
		// 检查用户是否在不检查权限的用户列表中
		for _, notCheckUserID := range notCheckUserIds {
			if notCheckUserID == userID {
				// 如果用户在不检查权限的用户列表中，直接返回所有数据
				return db
			}
		}

		// 获取用户角色
		roles, err := getUserRoles(ctx, userID)
		if err != nil || len(roles) == 0 {
			// 如果没有角色或查询失败，默认只能查看自己的数据
			return db.Where("created_by = ?", userID)
		}

		// 检查是否有全表权限的角色
		hasFullPermission := false
		for _, role := range roles {
			if role.DataScope == 1 {
				hasFullPermission = true
				break
			}
		}

		if hasFullPermission {
			// 有全表权限，不添加任何限制
			return db
		}

		// 收集所有需要查询的部门ID
		allowedDeptIDs := make(map[uint]bool)

		// 获取用户所属部门ID（只查询一次）
		userDeptID, _ := getUserDepartmentID(ctx, userID)

		// 构建部门树
		var allDepartments models.SysDepartmentList
		err = app.DBContext(ctx).Find(&allDepartments).Error
		if err != nil {
			// 查询失败，默认只能查看自己的数据
			return db.Where("created_by = ?", userID)
		}
		// 构建部门树
		departmentTree := allDepartments.BuildTree(ctx)

		// 收集所有需要处理的部门ID
		var allDeptIDs []uint
		for _, role := range roles {
			switch role.DataScope {
			case 2: // 查询checked_depts字段定义的部门所属用户创建的数据
				if role.CheckedDepts != "" {
					deptIDs, err := stringToUintSlice(role.CheckedDepts)
					if err == nil && len(deptIDs) > 0 {
						allDeptIDs = append(allDeptIDs, deptIDs...)
					}
				}

			case 3: // 查询自身所属部门的所属用户创建的数据
				if userDeptID != 0 {
					allDeptIDs = append(allDeptIDs, userDeptID)
				}

			case 4: // 查询自身所属部门及该部门下所有子级部门的所属用户创建的数据
				if userDeptID != 0 {
					deptIDs, err := getDepartmentAndChildrenIDs(departmentTree, userDeptID)
					if err == nil && len(deptIDs) > 0 {
						allDeptIDs = append(allDeptIDs, deptIDs...)
					}
				}
			}
		}

		// 去重部门ID
		for _, deptID := range allDeptIDs {
			allowedDeptIDs[deptID] = true
		}

		// 将部门ID转换为切片
		var deptIDSlice []uint
		for deptID := range allowedDeptIDs {
			deptIDSlice = append(deptIDSlice, deptID)
		}

		// 批量查询所有相关部门的用户ID
		var allowedUserIDs []uint
		if len(deptIDSlice) > 0 {
			userIDs, err := getUserIDsByDepartmentIDs(ctx, deptIDSlice)
			if err == nil {
				allowedUserIDs = append(allowedUserIDs, userIDs...)
			}
		}

		// 添加当前用户ID（默认可以查看自己的数据）
		allowedUserIDs = append(allowedUserIDs, userID)

		// 去重用户ID
		userIDMap := make(map[uint]bool)
		var userIDSlice []uint
		for _, uid := range allowedUserIDs {
			if !userIDMap[uid] {
				userIDMap[uid] = true
				userIDSlice = append(userIDSlice, uid)
			}
		}

		// 添加数据权限过滤条件
		return db.Where("created_by IN ?", userIDSlice)
	}
}
