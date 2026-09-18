package routes

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm/clause"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
)

var browserPermissionActions = []string{"read", "create", "grant", "rotate", "status", "audit"}

func browserRoutePermission(route openAPIAdminHTTPRoute) string {
	switch {
	case route.method == http.MethodPost && route.path == openAPIAdminHTTPPathPrefix:
		return "create"
	case strings.HasSuffix(route.path, "/scopes"):
		return "grant"
	case strings.HasSuffix(route.path, "/rotate-secret"):
		return "rotate"
	case strings.HasSuffix(route.path, "/audits"):
		return "audit"
	case route.method == http.MethodPost || strings.HasSuffix(route.path, "/revocation-status"):
		return "status"
	default:
		return "read"
	}
}

// Test-only fault control: change this temporary role's real menu associations
// and Casbin policies together. It never changes production route behavior.
func setBrowserMissingPermission(f *openAPIAdminHTTPFixture, missing string) error {
	if missing != "none" && !slices.Contains(browserPermissionActions, missing) {
		return fmt.Errorf("unknown fixture permission")
	}
	for _, action := range browserPermissionActions {
		var menu appmodels.SysMenu
		if err := f.db.Where("permission = ?", "gb28181:openapi:client:"+action).First(&menu).Error; err != nil {
			return err
		}
		link := appmodels.SysRoleMenu{RoleID: 1, MenuID: menu.ID}
		if action == missing {
			if err := f.db.Where("role_id = ? AND menu_id = ?", 1, menu.ID).Delete(&appmodels.SysRoleMenu{}).Error; err != nil {
				return err
			}
		} else if err := f.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&link).Error; err != nil {
			return err
		}
	}
	for _, route := range openAPIAdminHTTPRoutes() {
		var err error
		if browserRoutePermission(route) == missing {
			err = f.helper.RemovePolicyForRole(1, route.path, route.method)
		} else {
			err = f.helper.AddPolicyForRole(1, route.path, route.method)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func TestOpenAPIAdminBrowserPermissionMatrix(t *testing.T) {
	f := newOpenAPIAdminBrowserFixture(t)
	for _, missing := range browserPermissionActions {
		t.Run(missing, func(t *testing.T) {
			require.NoError(t, setBrowserMissingPermission(f, missing))
			profile := f.do(t, f.adminToken, http.MethodGet, "/api/users/profile", "")
			f.requestCount.Add(-1)
			require.Equal(t, http.StatusOK, profile.status)
			require.NotContains(t, string(profile.body), "gb28181:openapi:client:"+missing)
			for _, action := range browserPermissionActions {
				if action != missing {
					require.Contains(t, string(profile.body), "gb28181:openapi:client:"+action)
				}
			}
			for _, route := range openAPIAdminHTTPRoutes() {
				if browserRoutePermission(route) != missing {
					continue
				}
				response := f.do(t, f.adminToken, route.method, strings.ReplaceAll(route.path, ":id", "1"), "")
				require.Equal(t, http.StatusForbidden, response.status, route.path)
			}
		})
	}
	waitForBrowserSessionLogs(t, f, 8)
	require.NoError(t, setBrowserMissingPermission(f, "none"))
}
