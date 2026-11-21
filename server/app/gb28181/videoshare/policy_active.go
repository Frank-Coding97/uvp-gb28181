package videoshare

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/casbinhelper"
)

// ActivePolicyChecker filters a freshly loaded, private transactional helper for
// share authorization only. The caller must not use this helper to write policy
// afterwards: removals are intentionally in memory and auto-save stays disabled.
func ActivePolicyChecker(tx *gorm.DB, helper *casbinhelper.CasbinHelper) (PermissionChecker, error) {
	if tx == nil || tx.Error != nil || tx.Statement == nil || helper == nil || helper.GetEnforcer() == nil {
		return nil, fmt.Errorf("active share policy requires a private transactional helper")
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return nil, fmt.Errorf("active share policy requires an active transaction")
	}
	var roleIDs []uint
	if err := tx.Model(&models.SysRole{}).Where("status = ?", 1).Pluck("id", &roleIDs).Error; err != nil {
		return nil, fmt.Errorf("read active share roles: %w", err)
	}
	active := make(map[string]bool, len(roleIDs))
	for _, id := range roleIDs {
		active[fmt.Sprintf("role_%d", id)] = true
	}
	inactiveRole := func(subject string) bool { return strings.HasPrefix(subject, "role_") && !active[subject] }
	enforcer := helper.GetEnforcer()
	enforcer.EnableAutoSave(false)
	policies, err := enforcer.GetPolicy()
	if err != nil {
		return nil, err
	}
	var removePolicies [][]string
	for _, policy := range policies {
		if len(policy) > 0 && inactiveRole(policy[0]) {
			removePolicies = append(removePolicies, policy)
		}
	}
	if len(removePolicies) > 0 {
		if _, err := enforcer.RemovePolicies(removePolicies); err != nil {
			return nil, err
		}
	}
	groups, err := enforcer.GetGroupingPolicy()
	if err != nil {
		return nil, err
	}
	var removeGroups [][]string
	for _, group := range groups {
		if len(group) > 1 && (inactiveRole(group[0]) || inactiveRole(group[1])) {
			removeGroups = append(removeGroups, group)
		}
	}
	if len(removeGroups) > 0 {
		if _, err := enforcer.RemoveGroupingPolicies(removeGroups); err != nil {
			return nil, err
		}
	}
	return PermissionCheckerFor(helper), nil
}
