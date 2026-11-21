package casbinhelper

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

var errPolicyBackendUnavailable = errors.New("policy backend unavailable")

type failingReloadAdapter struct {
	scriptedPolicyAdapter
	fail    atomic.Bool
	revoked atomic.Bool
}

func (a *failingReloadAdapter) LoadPolicy(m model.Model) error {
	if a.fail.Load() {
		return errPolicyBackendUnavailable
	}
	if a.revoked.Load() {
		return nil
	}
	for _, rule := range [][]string{
		{"g", "user_7", "role_1", "*"},
		{"p", "role_1", "/items/:id", "GET", "*"},
	} {
		if err := persist.LoadPolicyArray(rule, m); err != nil {
			return err
		}
	}
	return nil
}

func TestReloadFailureRejectsCachedAuthorityUntilSuccessfulReload(t *testing.T) {
	adapter := &failingReloadAdapter{}
	helper := &CasbinHelper{enforcer: newLoggingPolicyEnforcer(t, adapter)}
	allowed, err := helper.Enforce("user_7", "/items/42", "GET", "*")
	require.NoError(t, err)
	require.True(t, allowed)

	adapter.fail.Store(true)
	require.ErrorIs(t, helper.ReloadPolicy(), errPolicyBackendUnavailable)
	allowed, err = helper.Enforce("user_7", "/items/42", "GET", "*")
	require.ErrorIs(t, err, errPolicyBackendUnavailable, "old permissions must not survive a failed authoritative reload")
	require.False(t, allowed)
	roles, err := helper.GetRolesForUserByID(7)
	require.ErrorIs(t, err, errPolicyBackendUnavailable)
	require.Empty(t, roles)
	users, err := helper.GetUsersForRole(1)
	require.ErrorIs(t, err, errPolicyBackendUnavailable)
	require.Empty(t, users)
	allowed, err = helper.HasRoleForUser(7, 1)
	require.ErrorIs(t, err, errPolicyBackendUnavailable)
	require.False(t, allowed)
	permissions, err := helper.GetPermissionsForUser(7)
	require.ErrorIs(t, err, errPolicyBackendUnavailable)
	require.Empty(t, permissions)

	// A local mutation cannot establish that the complete persisted model is fresh.
	require.NoError(t, helper.AddPolicyForRole(1, "/local", "GET"))
	_, err = helper.Enforce("user_7", "/local", "GET", "*")
	require.ErrorIs(t, err, errPolicyBackendUnavailable)

	adapter.fail.Store(false)
	require.NoError(t, helper.ReloadPolicy())
	allowed, err = helper.Enforce("user_7", "/items/42", "GET", "*")
	require.NoError(t, err)
	require.True(t, allowed)
	roles, err = helper.GetRolesForUserByID(7)
	require.NoError(t, err)
	require.Equal(t, []uint{1}, roles)

	// Recovery publishes the current database contents, including actual revocation.
	adapter.fail.Store(true)
	require.ErrorIs(t, helper.ReloadPolicy(), errPolicyBackendUnavailable)
	adapter.revoked.Store(true)
	adapter.fail.Store(false)
	require.NoError(t, helper.ReloadPolicy())
	allowed, err = helper.Enforce("user_7", "/items/42", "GET", "*")
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestAutoReloadFailureRejectsCachedAuthorityAndRecovers(t *testing.T) {
	observed := installCasbinObserver(t)
	adapter := &failingReloadAdapter{}
	helper := &CasbinHelper{enforcer: newLoggingPolicyEnforcer(t, adapter)}
	adapter.fail.Store(true)
	startAutoLoadForTest(t, helper, time.Millisecond)
	defer helper.Close()
	waitForCasbinEvent(t, observed, "casbin.policy_reload.failed")
	allowed, err := helper.Enforce("user_7", "/items/42", "GET", "*")
	require.ErrorIs(t, err, errPolicyBackendUnavailable)
	require.False(t, allowed)

	adapter.fail.Store(false)
	waitForCasbinEvent(t, observed, "casbin.policy_reload.succeeded")
	allowed, err = helper.Enforce("user_7", "/items/42", "GET", "*")
	require.NoError(t, err)
	require.True(t, allowed)
}

type reloadInitConfig struct{ staticCasbinConfig }

func (reloadInitConfig) GetString(key string) string {
	switch key {
	case "casbin.tableprefix":
		return "reload_"
	case "casbin.tablename":
		return "rules"
	}
	return ""
}

func TestSuccessfulInitializationClearsReloadFailure(t *testing.T) {
	installCasbinObserver(t)
	previousConfig := app.ConfigYml
	app.ConfigYml = reloadInitConfig{}
	t.Cleanup(func() { app.ConfigYml = previousConfig })
	adapter := &failingReloadAdapter{}
	helper := &CasbinHelper{enforcer: newLoggingPolicyEnforcer(t, adapter)}
	adapter.fail.Store(true)
	require.ErrorIs(t, helper.ReloadPolicy(), errPolicyBackendUnavailable)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	require.NoError(t, helper.InitCasbin(db, testModelConfig))
	defer helper.Close()
	require.NoError(t, helper.AddRolesForUserByID(7, []uint{1}))
	require.NoError(t, helper.AddPolicyForRole(1, "/new", "GET"))
	allowed, err := helper.Enforce("user_7", "/new", "GET", "*")
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = helper.Enforce("user_7", "/items/42", "GET", "*")
	require.NoError(t, err)
	require.False(t, allowed)
}
