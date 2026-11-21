package service

import (
	"context"
	"go.uber.org/zap"
	"time"
	gbvideoshare "uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"

	"gorm.io/gorm"
)

type PermissionService struct {
	tx     *gorm.DB
	helper app.CasbinInterf
}

// NewPermissionService 创建权限服务
func NewPermissionService() *PermissionService {
	return &PermissionService{}
}

// 获取当前Casbin域
func (ps *PermissionService) GetDomain(c context.Context) []string {
	return nil
}

// Transaction owns the common authority gate, business writes, private Casbin
// policy writes and permanent share revocation. Nested service calls reuse it.
func (ps *PermissionService) Transaction(ctx context.Context, fn func(*gorm.DB, *PermissionService) error) error {
	if ps.tx != nil {
		return fn(ps.tx, ps)
	}
	err := app.DBContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authoritytx.Acquire(ctx, tx); err != nil {
			return err
		}
		helper, err := gbvideoshare.NewPolicyHelper(tx)
		if err != nil {
			return err
		}
		scoped := &PermissionService{tx: tx, helper: helper}
		if err := fn(tx, scoped); err != nil {
			return err
		}
		permission, err := gbvideoshare.TransactionalPermissionChecker(tx)
		if err != nil {
			return err
		}
		return gbvideoshare.RevokeAllUnauthorizedSharesTx(ctx, tx, permission, "creator_authority_changed", time.Time{})
	})
	if err == nil {
		if loader, ok := app.CasbinV2.(interface{ ReloadPolicy() error }); ok {
			if reloadErr := loader.ReloadPolicy(); reloadErr != nil {
				app.Log(ctx).Error("权限事务已提交，缓存重载失败", zap.String("event", "casbin.policy_reload.commit_failed"), zap.Error(reloadErr))
			}
		}
	}
	return err
}

// 删除角色的所有权限
func (ps *PermissionService) DeleteRoleApis(c context.Context, roleID uint) (err error) {
	if ps.tx == nil {
		return ps.Transaction(c, func(_ *gorm.DB, scoped *PermissionService) error { return scoped.DeleteRoleApis(c, roleID) })
	}
	domain := ps.GetDomain(c)

	// 删除该角色的所有权限
	if err = ps.helper.RemoveAllPoliciesForRole(roleID, domain...); err != nil {
		return err
	}
	return nil
}

// 为角色分配资源权限，原有权限会被清除
func (ps *PermissionService) AddPoliciesForRole(c context.Context, roleID uint, sysapilist models.SysApiList) (err error) {
	if ps.tx == nil {
		return ps.Transaction(c, func(_ *gorm.DB, scoped *PermissionService) error {
			return scoped.AddPoliciesForRole(c, roleID, sysapilist)
		})
	}
	domain := ps.GetDomain(c)

	// 删除该角色的所有权限
	if err = ps.helper.RemoveAllPoliciesForRole(roleID, domain...); err != nil {
		return
	}
	// 如果有API权限，则添加到casbin
	if !sysapilist.IsEmpty() {
		// 构建权限策略列表
		var policies [][]string
		for _, api := range sysapilist {
			// 处理路径中的参数，将 :roleId 等参数转换为 *
			path := api.Path
			// 使用正则表达式替换路径参数为通配符 *
			//path = common.ConvertPathToWildcard(path)

			// 构建策略：[obj, act]
			policy := []string{path, api.Method}
			policies = append(policies, policy)
		}
		// 对policies进行去重处理（按obj和act）
		policyMap := make(map[string]bool)
		var deduplicatedPolicies [][]string
		for _, policy := range policies {
			key := policy[0] + "|" + policy[1]
			if !policyMap[key] {
				policyMap[key] = true
				deduplicatedPolicies = append(deduplicatedPolicies, policy)
			}
		}
		// 批量添加权限策略
		if err = ps.helper.AddPoliciesForRole(roleID, deduplicatedPolicies, domain...); err != nil {
			return err
		}
	}
	return nil
}

// 添加角色继承关系
func (ps *PermissionService) AddRoleInheritance(c context.Context, roleID uint, parentRoleID uint) (err error) {
	if ps.tx == nil {
		return ps.Transaction(c, func(_ *gorm.DB, scoped *PermissionService) error {
			return scoped.AddRoleInheritance(c, roleID, parentRoleID)
		})
	}

	domain := ps.GetDomain(c)

	// 检查角色是否已继承自父角色
	// INFO 而非 WARN：这是**入参自校验**——子角色不能等于父角色、父角色不能为 0。
	// 拒绝后 `return nil`（调用方看到的是"成功"），对外行为与"这次调用没发生"等价。
	// 判据②答不出动作：没有人会为一个非法角色 ID 去修系统。
	if roleID == parentRoleID || parentRoleID == 0 {
		app.Log(c).Info("child role ID cannot be equal to parent role ID or parent role ID is 0", zap.String("event", "casbin.role_inheritance.add_rejected"),
			zap.Uint("role_id", roleID), zap.Uint("parent_role_id", parentRoleID))
		return nil
	}

	// 添加角色继承关系
	err = ps.helper.AddRoleInheritance(roleID, parentRoleID, domain...)
	return
}

// 编辑角色继承关系
func (ps *PermissionService) EditRoleInheritance(c context.Context, roleID uint, parentRoleID uint) (err error) {
	if ps.tx == nil {
		return ps.Transaction(c, func(_ *gorm.DB, scoped *PermissionService) error {
			return scoped.EditRoleInheritance(c, roleID, parentRoleID)
		})
	}

	domain := ps.GetDomain(c)

	if roleID == parentRoleID {
		app.Log(c).Info("child role ID cannot be equal to parent role ID", zap.String("event", "casbin.role_inheritance.update_rejected"),
			zap.Uint("role_id", roleID), zap.Uint("parent_role_id", parentRoleID))
		return nil
	}
	// 删除角色的所有继承关系
	err = ps.helper.DeleteRoleInheritance(roleID, 0, domain...)
	if err != nil {
		return err
	}
	if parentRoleID > 0 {
		// 添加角色继承关系
		err = ps.helper.AddRoleInheritance(roleID, parentRoleID, domain...)
		if err != nil {
			return err
		}
	}
	return nil
}

// 删除角色继承关系
func (ps *PermissionService) DeleteRoleInheritance(c context.Context, roleID uint, parentRoleID uint) (err error) {
	if ps.tx == nil {
		return ps.Transaction(c, func(_ *gorm.DB, scoped *PermissionService) error {
			return scoped.DeleteRoleInheritance(c, roleID, parentRoleID)
		})
	}

	domain := ps.GetDomain(c)

	// 检查角色是否已继承自父角色
	if roleID == parentRoleID || parentRoleID == 0 {
		app.Log(c).Info("child role ID cannot be equal to parent role ID or parent role ID is 0", zap.String("event", "casbin.role_inheritance.remove_rejected"),
			zap.Uint("role_id", roleID), zap.Uint("parent_role_id", parentRoleID))
		return nil
	}
	// 删除角色的继承关系
	err = ps.helper.DeleteRoleInheritance(roleID, parentRoleID, domain...)
	if err == nil {
		err = nil
	}
	return
}

// 为用户分配角色
func (ps *PermissionService) AddRoleForUser(c context.Context, userID uint, roles []uint) (err error) {
	if ps.tx == nil {
		return ps.Transaction(c, func(_ *gorm.DB, scoped *PermissionService) error { return scoped.AddRoleForUser(c, userID, roles) })
	}
	domain := ps.GetDomain(c)
	// 添加用户角色关系
	err = ps.helper.AddRolesForUserByID(userID, roles, domain...)
	return
}

// 编辑用户的角色
func (ps *PermissionService) EditUserRoles(c context.Context, userID uint, roles []uint) (err error) {
	if ps.tx == nil {
		return ps.Transaction(c, func(_ *gorm.DB, scoped *PermissionService) error { return scoped.EditUserRoles(c, userID, roles) })
	}
	domain := ps.GetDomain(c)
	// 删除用户的所有角色
	err = ps.helper.DeleteRolesForUserByID(userID, nil, domain...)
	if err != nil {
		return
	}
	// 添加用户角色关系
	err = ps.helper.AddRolesForUserByID(userID, roles, domain...)
	return
}

// 删除用户的角色关系
func (ps *PermissionService) DeleteUserRoles(c context.Context, userID uint, roles []uint) (err error) {
	if ps.tx == nil {
		return ps.Transaction(c, func(_ *gorm.DB, scoped *PermissionService) error { return scoped.DeleteUserRoles(c, userID, roles) })
	}
	domain := ps.GetDomain(c)
	// 删除用户角色关系
	err = ps.helper.DeleteRolesForUserByID(userID, roles, domain...)
	return
}

// 根据菜单ID调整与该菜单关联的角色的API权限
func (ps *PermissionService) UpdateRoleApiPermissionsByMenuID(c context.Context, menuID uint) (err error) {
	if ps.tx == nil {
		return ps.Transaction(c, func(_ *gorm.DB, scoped *PermissionService) error {
			return scoped.UpdateRoleApiPermissionsByMenuID(c, menuID)
		})
	}
	// 1. 查找与指定菜单ID关联的所有角色
	var roleMenus models.SysRoleMenuList
	if err = ps.tx.WithContext(c).Where("menu_id = ?", menuID).Preload("Role").Find(&roleMenus).Error; err != nil {
		return
	}

	// 如果没有关联的角色，直接返回
	if roleMenus.IsEmpty() {
		return
	}

	// 获取关联角色
	roles := roleMenus.GetRoles()
	// 2. 为每个关联的角色更新API权限
	for _, role := range roles {
		// 查找该角色关联的所有菜单
		var roleMenusForRole models.SysRoleMenuList
		if err = ps.tx.WithContext(c).Where("role_id = ?", role.ID).Find(&roleMenusForRole).Error; err != nil {
			return
		}
		domain := ps.GetDomain(c)
		// 如果角色没有关联任何菜单，则清除该角色的所有API权限
		if roleMenusForRole.IsEmpty() {
			if err = ps.helper.RemoveAllPoliciesForRole(role.ID, domain...); err != nil {
				return
			}
			continue
		}

		// 提取菜单ID列表
		menuIDs := make([]uint, len(roleMenusForRole))
		for i, rm := range roleMenusForRole {
			menuIDs[i] = rm.MenuID
		}

		// 查找这些菜单关联的所有API
		var menus models.SysMenuList
		if err = ps.tx.WithContext(c).Preload("Apis").Where("id IN ?", menuIDs).Find(&menus).Error; err != nil {
			return
		}

		// 收集所有API（去重）
		var allApis models.SysApiList
		for _, menu := range menus {
			allApis = append(allApis, menu.Apis...)
		}
		// 去重
		allApis = allApis.Unique()

		// 使用已有的 AddPoliciesForRole 方法为角色分配所有关联菜单的API权限
		if err = ps.AddPoliciesForRole(c, role.ID, allApis); err != nil {
			return
		}
	}

	return
}

// 根据API ID调整与该API关联的角色的权限
func (ps *PermissionService) UpdateRoleApiPermissionsByApiID(c context.Context, apiID uint) (err error) {
	if ps.tx == nil {
		return ps.Transaction(c, func(_ *gorm.DB, scoped *PermissionService) error {
			return scoped.UpdateRoleApiPermissionsByApiID(c, apiID)
		})
	}
	domain := ps.GetDomain(c)

	// 1. 通过api_id查找关联的menu_id
	var menuIds []uint
	err = ps.tx.WithContext(c).Model(&models.SysMenuApi{}).Where("api_id = ?", apiID).Pluck("menu_id", &menuIds).Error
	if err != nil {
		return
	}

	// 如果没有关联的菜单，直接返回
	if len(menuIds) == 0 {
		return
	}

	// 2. 通过menu_id查找关联的role_id
	var roleMenus models.SysRoleMenuList
	if err = ps.tx.WithContext(c).Where("menu_id IN ?", menuIds).Find(&roleMenus).Error; err != nil {
		return
	}

	// 如果没有关联的角色，直接返回
	if roleMenus.IsEmpty() {
		return
	}

	// 提取关联角色ID列表（去重）
	roleIDSet := make(map[uint]bool)
	for _, rm := range roleMenus {
		roleIDSet[rm.RoleID] = true
	}

	// 3. 为每个关联的角色重新设置casbin权限
	for roleID := range roleIDSet {
		// 查找该角色关联的所有菜单
		var roleMenusForRole models.SysRoleMenuList
		if err = ps.tx.WithContext(c).Where("role_id = ?", roleID).Find(&roleMenusForRole).Error; err != nil {
			return
		}

		// 如果角色没有关联任何菜单，则清除该角色的所有API权限
		if roleMenusForRole.IsEmpty() {
			if err = ps.helper.RemoveAllPoliciesForRole(roleID, domain...); err != nil {
				return
			}
			continue
		}

		// 提取菜单ID列表
		menuIDs := make([]uint, len(roleMenusForRole))
		for i, rm := range roleMenusForRole {
			menuIDs[i] = rm.MenuID
		}

		// 查找这些菜单关联的所有API
		var menus models.SysMenuList
		if err = ps.tx.WithContext(c).Preload("Apis").Where("id IN ?", menuIDs).Find(&menus).Error; err != nil {
			return
		}

		// 收集所有API（去重）
		var allApis models.SysApiList
		for _, menu := range menus {
			allApis = append(allApis, menu.Apis...)
		}
		// 去重
		allApis = allApis.Unique()

		// 使用已有的 AddPoliciesForRole 方法为角色分配所有关联菜单的API权限
		if err = ps.AddPoliciesForRole(c, roleID, allApis); err != nil {
			return
		}
	}

	return
}
