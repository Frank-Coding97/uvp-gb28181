package client

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
)

var managementBoundaryDBID atomic.Int64

type casbinManagementAuthorizer struct {
	enforcer *casbin.Enforcer
}

func (a casbinManagementAuthorizer) Enforce(sub, obj, act string, domain ...string) (bool, error) {
	dom := "*"
	if len(domain) > 0 {
		dom = domain[0]
	}
	return a.enforcer.Enforce(sub, obj, act, dom)
}

func TestOpenAPIAdminBoundaryUsesCurrentTrustedUserAndRealPermissions(t *testing.T) {
	db := newManagementBoundaryDB(t)
	seedManagementDepartments(t, db)
	seedManagementUser(t, db, 7, 10, 1, 3, "", true)
	seedManagementUser(t, db, 8, 10, 2, 3, "", true)
	seedManagementUser(t, db, 9, 10, 3, 3, "", false)

	enforcer := newManagementBoundaryEnforcer(t)
	_, err := enforcer.AddGroupingPolicy("user_7", "role_1", "*")
	require.NoError(t, err)
	for _, action := range []string{
		ManagementActionRead,
		ManagementActionCreate,
		ManagementActionGrant,
		ManagementActionRotate,
		ManagementActionStatus,
		ManagementActionAudit,
	} {
		_, err = enforcer.AddPolicy("role_1", ManagementPermissionObject, action, "*")
		require.NoError(t, err)
	}

	boundary := NewManagementScopeBoundary(db, casbinManagementAuthorizer{enforcer: enforcer})
	ctx := context.Background()

	require.NoError(t, boundary.AuthorizeCreate(ctx, 7, 10))
	require.NoError(t, boundary.AuthorizeClient(ctx, 7, "client.read", 10))
	require.NoError(t, boundary.AuthorizeClient(ctx, 7, "client.rotate", 10))
	require.NoError(t, boundary.AuthorizeClient(ctx, 7, "scope.set", 10))
	require.NoError(t, boundary.AuthorizeClient(ctx, 7, "client.disabled", 10))
	require.NoError(t, boundary.AuthorizeClient(ctx, 7, "client.audit", 10))

	assertManagementDenied(t, boundary.AuthorizeCreate(ctx, 7, 11), "a department-only role must not expand to child departments")
	assertManagementDenied(t, boundary.AuthorizeCreate(ctx, 7, 20), "a department-only role must not cross departments")
	assertManagementDenied(t, boundary.AuthorizeClient(ctx, 7, "client.unknown", 10), "unknown action must fail closed")
	assertManagementDenied(t, boundary.AuthorizeCreate(ctx, 8, 10), "missing Casbin permission must fail closed")
	assertManagementDenied(t, boundary.AuthorizeCreate(ctx, 9, 10), "disabled user must fail closed")
	assertManagementDenied(t, boundary.AuthorizeCreate(ctx, 7, 30), "disabled target department must fail closed")
}

func TestOpenAPIAdminBoundaryChecksEachPermissionIndependently(t *testing.T) {
	db := newManagementBoundaryDB(t)
	seedManagementDepartments(t, db)
	seedManagementUser(t, db, 7, 10, 1, 1, "", true)
	enforcer := newManagementBoundaryEnforcer(t)
	_, err := enforcer.AddGroupingPolicy("user_7", "role_1", "*")
	require.NoError(t, err)
	_, err = enforcer.AddPolicy("role_1", ManagementPermissionObject, ManagementActionRead, "*")
	require.NoError(t, err)

	boundary := NewManagementScopeBoundary(db, casbinManagementAuthorizer{enforcer: enforcer})
	require.NoError(t, boundary.AuthorizeClient(context.Background(), 7, "client.read", 10))
	for _, action := range []string{
		"client.create",
		"scope.set",
		"client.rotate",
		"client.active",
		"client.audit",
	} {
		assertManagementDenied(t, boundary.AuthorizeClient(context.Background(), 7, action, 10), fmt.Sprintf("permission %q must not inherit read", action))
	}
}

func TestOpenAPIAdminBoundaryNilDependenciesNeverMeanFullAccess(t *testing.T) {
	ctx := context.Background()
	for _, boundary := range []*ManagementScopeBoundary{
		NewManagementScopeBoundary(nil, nil),
		NewManagementScopeBoundary(newManagementBoundaryDB(t), nil),
		NewManagementScopeBoundary(nil, casbinManagementAuthorizer{}),
	} {
		assertManagementUnavailable(t, boundary.AuthorizeCreate(ctx, 7, 10), "nil dependency must not become full access")
		assertManagementUnavailable(t, boundary.AuthorizeClient(ctx, 7, "client.read", 10), "nil dependency must not become full access")
	}
	assertManagementUnavailable(t, (&ManagementScopeBoundary{}).AuthorizeCreate(ctx, 7, 10), "zero-value boundary must fail closed")
}

func assertManagementDenied(t *testing.T, err error, message string) {
	t.Helper()
	require.ErrorIs(t, err, ErrManagementBoundaryDenied, message)
}

func assertManagementUnavailable(t *testing.T, err error, message string) {
	t.Helper()
	require.ErrorIs(t, err, ErrAuthorizationUnavailable, message)
}

func newManagementBoundaryDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:openapi_management_boundary_%d?mode=memory&cache=shared", managementBoundaryDBID.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&appmodels.User{}, &appmodels.SysRole{}, &appmodels.SysUserRole{}, &appmodels.SysDepartment{}))
	return db
}

func newManagementBoundaryEnforcer(t *testing.T) *casbin.Enforcer {
	t.Helper()
	config := `[request_definition]
r = sub, obj, act, dom

[policy_definition]
p = sub, obj, act, dom

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.dom) && r.obj == p.obj && r.act == p.act && (r.dom == p.dom || p.dom == "*")`
	m, err := model.NewModelFromString(config)
	require.NoError(t, err)
	enforcer, err := casbin.NewEnforcer(m)
	require.NoError(t, err)
	return enforcer
}

func seedManagementDepartments(t *testing.T, db *gorm.DB) {
	t.Helper()
	active, disabled := int8(1), int8(0)
	childParent := uint(10)
	require.NoError(t, db.Create([]*appmodels.SysDepartment{
		{BaseModel: appmodels.BaseModel{ID: 10}, Name: "root", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: 11}, ParentID: &childParent, Name: "child", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: 20}, Name: "other", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: 30}, Name: "disabled", Status: &disabled},
	}).Error)
}

func seedManagementUser(t *testing.T, db *gorm.DB, userID, deptID, roleID uint, dataScope int8, checkedDepts string, enabled bool) {
	t.Helper()
	status := int8(0)
	if enabled {
		status = 1
	}
	require.NoError(t, db.Create(&appmodels.User{
		BaseModel: appmodels.BaseModel{ID: userID},
		Username:  fmt.Sprintf("management-user-%d", userID),
		Password:  "test-password",
		Status:    status,
		DeptID:    deptID,
	}).Error)
	require.NoError(t, db.Create(&appmodels.SysRole{
		BaseModel:    appmodels.BaseModel{ID: roleID},
		Name:         fmt.Sprintf("management-role-%d", roleID),
		Status:       1,
		DataScope:    dataScope,
		CheckedDepts: checkedDepts,
	}).Error)
	require.NoError(t, db.Create(&appmodels.SysUserRole{UserID: userID, RoleID: roleID}).Error)
}
