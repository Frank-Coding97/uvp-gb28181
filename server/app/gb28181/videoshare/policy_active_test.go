package videoshare

import (
	"context"
	"testing"

	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/casbinhelper"
)

const activePolicyModel = `[request_definition]
r = sub, obj, act, dom
[policy_definition]
p = sub, obj, act, dom
[role_definition]
g = _, _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = g(r.sub,p.sub,r.dom) && keyMatch2(r.obj,p.obj) && regexMatch(r.act,p.act) && (r.dom == p.dom || p.dom == "*")`

func activePolicyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	require.NoError(t, db.AutoMigrate(&models.SysRole{}))
	require.NoError(t, db.Table("active_policy_rules").AutoMigrate(&gormadapter.CasbinRule{}))
	return db
}

func activePolicyHelper(t *testing.T, tx *gorm.DB) *casbinhelper.CasbinHelper {
	t.Helper()
	helper, err := casbinhelper.NewTransactionalHelper(tx, activePolicyModel, "active_", "policy_rules")
	require.NoError(t, err)
	return helper
}

func TestActivePolicyCheckerExcludesInactiveRolesWithoutChangingPolicy(t *testing.T) {
	for _, tc := range []struct {
		name       string
		active     []uint
		inactive   []uint
		deleted    []uint
		grants     []uint
		inherits   [][2]uint
		policyRole uint
		direct     bool
		allowed    bool
	}{
		{name: "only role disabled", inactive: []uint{1}, grants: []uint{1}, policyRole: 1},
		{name: "parent role disabled", active: []uint{1}, inactive: []uint{2}, grants: []uint{1}, inherits: [][2]uint{{1, 2}}, policyRole: 2},
		{name: "disabled intermediate severs inheritance", active: []uint{1, 3}, inactive: []uint{2}, grants: []uint{1}, inherits: [][2]uint{{1, 2}, {2, 3}}, policyRole: 3},
		{name: "soft deleted role", deleted: []uint{1}, grants: []uint{1}, policyRole: 1},
		{name: "nonexistent role", grants: []uint{1}, policyRole: 1},
		{name: "another active role still grants access", active: []uint{2}, inactive: []uint{1}, grants: []uint{1, 2}, policyRole: 2, allowed: true},
		{name: "direct user permission retained", inactive: []uint{1}, grants: []uint{1}, policyRole: 1, direct: true, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := activePolicyDB(t)
			for _, id := range tc.active {
				require.NoError(t, db.Create(&models.SysRole{BaseModel: models.BaseModel{ID: id}, Status: 1}).Error)
			}
			for _, id := range tc.inactive {
				require.NoError(t, db.Create(&models.SysRole{BaseModel: models.BaseModel{ID: id}, Status: 0}).Error)
			}
			for _, id := range tc.deleted {
				role := models.SysRole{BaseModel: models.BaseModel{ID: id}, Status: 1}
				require.NoError(t, db.Create(&role).Error)
				require.NoError(t, db.Delete(&role).Error)
			}
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				helper := activePolicyHelper(t, tx)
				require.NoError(t, helper.AddRolesForUserByID(7, tc.grants))
				for _, inheritance := range tc.inherits {
					require.NoError(t, helper.AddRoleInheritance(inheritance[0], inheritance[1]))
				}
				require.NoError(t, helper.AddPolicyForRole(tc.policyRole, "/shared/:id", "GET"))
				if tc.direct {
					_, err := helper.GetEnforcer().AddPolicy("user_7", "/shared/:id", "GET", "*")
					require.NoError(t, err)
				}
				global := activePolicyHelper(t, tx)
				previous := app.CasbinV2
				app.CasbinV2 = global
				defer func() { app.CasbinV2 = previous }()
				before, err := helper.Enforce("user_7", "/shared/42", "GET", "*")
				require.NoError(t, err)
				require.True(t, before)
				var countBefore int64
				require.NoError(t, tx.Table("active_policy_rules").Count(&countBefore).Error)
				checker, err := ActivePolicyChecker(tx, helper)
				require.NoError(t, err)
				allowed, err := checker(context.Background(), 7, "/shared/42", "GET")
				require.NoError(t, err)
				require.Equal(t, tc.allowed, allowed)
				var countAfter int64
				require.NoError(t, tx.Table("active_policy_rules").Count(&countAfter).Error)
				require.Equal(t, countBefore, countAfter, "filtering must not persist policy removals")
				fresh := activePolicyHelper(t, tx)
				retained, err := fresh.Enforce("user_7", "/shared/42", "GET", "*")
				require.NoError(t, err)
				require.True(t, retained, "a new helper must still load the original policy")
				require.Same(t, global, app.CasbinV2)
				retained, err = global.Enforce("user_7", "/shared/42", "GET", "*")
				require.NoError(t, err)
				require.True(t, retained, "global policy must remain unchanged")
				return nil
			}))
		})
	}
}

func TestActivePolicyCheckerUsesSameTransactionAndPreservesDomains(t *testing.T) {
	db := activePolicyDB(t)
	require.NoError(t, db.Create(&models.SysRole{BaseModel: models.BaseModel{ID: 1}, Status: 1}).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		helper := activePolicyHelper(t, tx)
		require.NoError(t, helper.AddRolesForUserByID(7, []uint{1}, "/tenants/:tenant"))
		require.NoError(t, helper.AddPolicyForRole(1, "/shared/:id", "GET"))
		_, err := ActivePolicyChecker(tx, helper)
		require.NoError(t, err)
		allowed, err := helper.Enforce("user_7", "/shared/42", "GET", "/tenants/one")
		require.NoError(t, err)
		require.True(t, allowed)
		allowed, err = helper.Enforce("user_7", "/shared/42", "GET", "/other/one")
		require.NoError(t, err)
		require.False(t, allowed)
		// The next private model must see an uncommitted role-status change.
		require.NoError(t, tx.Model(&models.SysRole{}).Where("id = 1").Update("status", 0).Error)
		fresh := activePolicyHelper(t, tx)
		_, err = ActivePolicyChecker(tx, fresh)
		require.NoError(t, err)
		allowed, err = fresh.Enforce("user_7", "/shared/42", "GET", "/tenants/one")
		require.NoError(t, err)
		require.False(t, allowed)
		return nil
	}))
}

func TestActivePolicyCheckerFailsClosedOnRoleQueryError(t *testing.T) {
	db := activePolicyDB(t)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		helper := activePolicyHelper(t, tx)
		require.NoError(t, tx.Migrator().DropTable(&models.SysRole{}))
		checker, err := ActivePolicyChecker(tx, helper)
		require.Error(t, err)
		require.Nil(t, checker)
		return nil
	}))
}
