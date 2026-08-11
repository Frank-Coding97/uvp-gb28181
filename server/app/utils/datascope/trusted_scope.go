package datascope

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/models"
)

var ErrOwnerDeptAccessDenied = errors.New("owner department access denied")

type OwnerDeptAccess struct {
	FullAccess bool
	DeptIDs    []uint
}

// ResolveOwnerDeptAccessByUserID resolves data scope from a trusted user ID.
// It deliberately fails closed and does not inherit the claims-less behavior
// of the request-context compatibility helpers in this package.
func ResolveOwnerDeptAccessByUserID(ctx context.Context, db *gorm.DB, userID uint) (OwnerDeptAccess, error) {
	if db == nil || userID == 0 {
		return OwnerDeptAccess{}, ErrOwnerDeptAccessDenied
	}

	var user models.User
	result := db.WithContext(ctx).Where("id = ? AND status = ?", userID, 1).First(&user)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return OwnerDeptAccess{}, ErrOwnerDeptAccessDenied
	}
	if result.Error != nil {
		return OwnerDeptAccess{}, fmt.Errorf("resolve owner department user: %w", result.Error)
	}

	var roles []models.SysRole
	if err := db.WithContext(ctx).
		Model(&models.SysRole{}).
		Joins("JOIN sys_user_role ON sys_user_role.role_id = sys_role.id").
		Where("sys_user_role.user_id = ? AND sys_role.status = ?", userID, 1).
		Find(&roles).Error; err != nil {
		return OwnerDeptAccess{}, fmt.Errorf("resolve owner department roles: %w", err)
	}
	if len(roles) == 0 {
		return OwnerDeptAccess{}, ErrOwnerDeptAccessDenied
	}

	allowed := make(map[uint]struct{})
	var departmentTree models.SysDepartmentList
	treeLoaded := false
	for _, role := range roles {
		switch role.DataScope {
		case 1:
			return OwnerDeptAccess{FullAccess: true}, nil
		case 2:
			ids, err := stringToUintSlice(role.CheckedDepts)
			if err != nil {
				return OwnerDeptAccess{}, ErrOwnerDeptAccessDenied
			}
			addOwnerDeptIDs(allowed, ids)
		case 3, 5:
			addOwnerDeptIDs(allowed, []uint{user.DeptID})
		case 4:
			if !treeLoaded {
				var departments models.SysDepartmentList
				if err := db.WithContext(ctx).Find(&departments).Error; err != nil {
					return OwnerDeptAccess{}, fmt.Errorf("resolve owner department tree: %w", err)
				}
				departmentTree = departments.BuildTree()
				treeLoaded = true
			}
			ids, err := getDepartmentAndChildrenIDs(departmentTree, user.DeptID)
			if err != nil {
				return OwnerDeptAccess{}, ErrOwnerDeptAccessDenied
			}
			addOwnerDeptIDs(allowed, ids)
		default:
			return OwnerDeptAccess{}, ErrOwnerDeptAccessDenied
		}
	}

	deptIDs := make([]uint, 0, len(allowed))
	for id := range allowed {
		deptIDs = append(deptIDs, id)
	}
	sort.Slice(deptIDs, func(i, j int) bool { return deptIDs[i] < deptIDs[j] })
	return OwnerDeptAccess{DeptIDs: deptIDs}, nil
}

func addOwnerDeptIDs(target map[uint]struct{}, ids []uint) {
	for _, id := range ids {
		if id != 0 {
			target[id] = struct{}{}
		}
	}
}
