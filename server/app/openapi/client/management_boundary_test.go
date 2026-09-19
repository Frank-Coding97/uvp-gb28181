package client

import (
	"context"
	"fmt"
	"net/http"
	"sync"
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
	mu       sync.Mutex
	calls    []managementEnforceCall
}

func (a *casbinManagementAuthorizer) Enforce(sub, obj, act string, domain ...string) (bool, error) {
	return a.enforce(sub, obj, act, domain...)
}

func (a *casbinManagementAuthorizer) enforce(sub, obj, act string, domain ...string) (bool, error) {
	dom := "*"
	if len(domain) > 0 {
		dom = domain[0]
	}
	a.mu.Lock()
	a.calls = append(a.calls, managementEnforceCall{subject: sub, path: obj, method: act, domain: dom})
	a.mu.Unlock()
	return a.enforcer.Enforce(sub, obj, act, dom)
}

type managementEnforceCall struct {
	subject string
	path    string
	method  string
	domain  string
}

type managementBoundaryTestRoute struct {
	path   string
	method string
}

func managementBoundaryTestRoutes() []managementBoundaryTestRoute {
	const base = "/api/gb28181/openapi-clients"
	return []managementBoundaryTestRoute{
		{path: base, method: http.MethodPost},
		{path: base, method: http.MethodGet},
		{path: base + "/:id/scopes", method: http.MethodPut},
		{path: base + "/:id/rotate-secret", method: http.MethodPost},
		{path: base + "/:id/enable", method: http.MethodPost},
		{path: base + "/:id/disable", method: http.MethodPost},
		{path: base + "/:id/revoke", method: http.MethodPost},
		{path: base + "/:id/audits", method: http.MethodGet},
	}
}

func (a *casbinManagementAuthorizer) snapshotCalls() []managementEnforceCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]managementEnforceCall(nil), a.calls...)
}

func TestOpenAPIAdminBoundaryUsesCurrentTrustedUserAndRealPermissions(t *testing.T) {
	db := newManagementBoundaryDB(t)
	seedManagementDepartments(t, db)
	seedManagementUser(t, db, 7, 10, 1, 2, "10,30,31,32,33", true)
	seedManagementUser(t, db, 8, 10, 2, 3, "", true)
	seedManagementUser(t, db, 9, 10, 3, 3, "", false)

	enforcer := newManagementBoundaryEnforcer(t)
	_, err := enforcer.AddGroupingPolicy("user_7", "role_1", "*")
	require.NoError(t, err)
	for _, route := range managementBoundaryTestRoutes() {
		_, err = enforcer.AddPolicy("role_1", route.path, route.method, "*")
		require.NoError(t, err)
	}

	authorizer := &casbinManagementAuthorizer{enforcer: enforcer}
	boundary := NewManagementScopeBoundary(db, authorizer)
	ctx := context.Background()

	require.NoError(t, boundary.AuthorizeCreate(ctx, 7, 10))
	assertManagementDenied(t, boundary.AuthorizeCreateWithDataScope(ctx, 7, 10, 4), "a department-only role must not grant a child-department client")
	require.NoError(t, boundary.AuthorizeRead(ctx, 7, 10))
	require.NoError(t, boundary.AuthorizeRotate(ctx, 7, 10))
	require.NoError(t, boundary.AuthorizeGrant(ctx, 7, 10))
	require.NoError(t, boundary.AuthorizeStatus(ctx, 7, 10))
	require.NoError(t, boundary.AuthorizeAudit(ctx, 7, 10))
	require.NoError(t, boundary.AuthorizeClient(ctx, 7, "client.rotate", 10))
	require.NoError(t, boundary.AuthorizeClient(ctx, 7, "scope.set", 10))
	require.NoError(t, boundary.AuthorizeClient(ctx, 7, "client.disabled", 10))
	require.NoError(t, boundary.AuthorizeClient(ctx, 7, "client.revoked", 10))
	require.NoError(t, boundary.AuthorizeClient(ctx, 7, "client.active", 10))

	assertManagementDenied(t, boundary.AuthorizeCreate(ctx, 7, 11), "a department-only role must not expand to child departments")
	assertManagementDenied(t, boundary.AuthorizeCreate(ctx, 7, 20), "a department-only role must not cross departments")
	assertManagementDenied(t, boundary.AuthorizeClient(ctx, 7, "client.unknown", 10), "unknown service action must fail closed")
	assertManagementDenied(t, boundary.AuthorizeCreate(ctx, 8, 10), "missing Casbin permission must fail closed")
	assertManagementDenied(t, boundary.AuthorizeCreate(ctx, 9, 10), "disabled user must fail closed")

	for _, departmentID := range []uint{30, 31, 32, 33} {
		require.NoError(t, boundary.AuthorizeRead(ctx, 7, departmentID), "read must retain historical client visibility for department %d", departmentID)
		require.NoError(t, boundary.AuthorizeAudit(ctx, 7, departmentID), "audit must retain historical client visibility for department %d", departmentID)
		require.NoError(t, boundary.AuthorizeClient(ctx, 7, "client.disabled", departmentID), "disable must clear historical client for department %d", departmentID)
		require.NoError(t, boundary.AuthorizeClient(ctx, 7, "client.revoked", departmentID), "revoke must clear historical client for department %d", departmentID)
		assertManagementDenied(t, boundary.AuthorizeCreate(ctx, 7, departmentID), "create must reject historical department")
		assertManagementDenied(t, boundary.AuthorizeGrant(ctx, 7, departmentID), "grant must reject historical department")
		assertManagementDenied(t, boundary.AuthorizeClient(ctx, 7, "client.active", departmentID), "enable must reject historical department")
		assertManagementDenied(t, boundary.AuthorizeRotate(ctx, 7, departmentID), "rotate must reject historical department")
	}

	calls := authorizer.snapshotCalls()
	routes := managementBoundaryTestRoutes()
	expectedRoutes := []managementBoundaryTestRoute{
		routes[0], routes[1], routes[3], routes[2], routes[5], routes[7],
		routes[3], routes[2], routes[5], routes[6], routes[4],
	}
	require.GreaterOrEqual(t, len(calls), len(expectedRoutes), "the successful action matrix must reach Casbin")
	for index, expected := range expectedRoutes {
		require.Equal(t, expected.path, calls[index].path, "Casbin path at call %d", index)
		require.Equal(t, expected.method, calls[index].method, "Casbin method at call %d", index)
	}
	require.Equal(t, "user_7", calls[0].subject)
	require.Equal(t, "*", calls[0].domain)
}

func TestOpenAPIAdminBoundaryAllowsChildScopeOnlyWhenOperatorManagesAllDescendants(t *testing.T) {
	db := newManagementBoundaryDB(t)
	seedManagementDepartments(t, db)
	seedManagementUser(t, db, 7, 10, 1, 4, "", true)
	enforcer := newManagementBoundaryEnforcer(t)
	_, err := enforcer.AddGroupingPolicy("user_7", "role_1", "*")
	require.NoError(t, err)
	_, err = enforcer.AddPolicy("role_1", managementBoundaryTestRoutes()[0].path, managementBoundaryTestRoutes()[0].method, "*")
	require.NoError(t, err)

	boundary := NewManagementScopeBoundary(db, &casbinManagementAuthorizer{enforcer: enforcer})
	require.NoError(t, boundary.AuthorizeCreateWithDataScope(context.Background(), 7, 10, 4))
	assertManagementDenied(t, boundary.AuthorizeCreateWithDataScope(context.Background(), 7, 10, 2), "unsupported client data scope must fail closed")
}

func TestOpenAPIAdminBoundaryChecksEachPermissionIndependently(t *testing.T) {
	db := newManagementBoundaryDB(t)
	seedManagementDepartments(t, db)
	seedManagementUser(t, db, 7, 10, 1, 1, "", true)
	enforcer := newManagementBoundaryEnforcer(t)
	_, err := enforcer.AddGroupingPolicy("user_7", "role_1", "*")
	require.NoError(t, err)
	_, err = enforcer.AddPolicy("role_1", managementBoundaryTestRoutes()[1].path, managementBoundaryTestRoutes()[1].method, "*")
	require.NoError(t, err)

	boundary := NewManagementScopeBoundary(db, &casbinManagementAuthorizer{enforcer: enforcer})
	require.NoError(t, boundary.AuthorizeRead(context.Background(), 7, 10))
	for _, check := range []func() error{
		func() error { return boundary.AuthorizeCreate(context.Background(), 7, 10) },
		func() error { return boundary.AuthorizeGrant(context.Background(), 7, 10) },
		func() error { return boundary.AuthorizeRotate(context.Background(), 7, 10) },
		func() error { return boundary.AuthorizeStatus(context.Background(), 7, 10) },
		func() error { return boundary.AuthorizeAudit(context.Background(), 7, 10) },
		func() error { return boundary.AuthorizeClient(context.Background(), 7, "scope.set", 10) },
		func() error { return boundary.AuthorizeClient(context.Background(), 7, "client.rotate", 10) },
		func() error { return boundary.AuthorizeClient(context.Background(), 7, "client.active", 10) },
		func() error { return boundary.AuthorizeClient(context.Background(), 7, "client.disabled", 10) },
		func() error { return boundary.AuthorizeClient(context.Background(), 7, "client.revoked", 10) },
	} {
		assertManagementDenied(t, check(), "each non-read route permission must remain independent")
	}
}

func TestOpenAPIAdminBoundaryNilDependenciesNeverMeanFullAccess(t *testing.T) {
	ctx := context.Background()
	for _, boundary := range []*ManagementScopeBoundary{
		NewManagementScopeBoundary(nil, nil),
		NewManagementScopeBoundary(newManagementBoundaryDB(t), nil),
		NewManagementScopeBoundary(nil, &casbinManagementAuthorizer{}),
	} {
		assertManagementUnavailable(t, boundary.AuthorizeCreate(ctx, 7, 10), "nil dependency must not become full access")
		assertManagementUnavailable(t, boundary.AuthorizeClient(ctx, 7, "client.rotate", 10), "nil dependency must not become full access")
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
m = g(r.sub, p.sub, r.dom) && keyMatch2(r.obj, p.obj) && r.act == p.act && (r.dom == p.dom || p.dom == "*")`
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
		{BaseModel: appmodels.BaseModel{ID: 31}, Name: "unknown-status"},
		{BaseModel: appmodels.BaseModel{ID: 32}, Name: "deleted", Status: &active},
	}).Error)
	require.NoError(t, db.Delete(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: 32}}).Error)
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
	if !enabled {
		require.NoError(t, db.Model(&appmodels.User{}).Where("id = ?", userID).Update("status", 0).Error)
	}
	require.NoError(t, db.Create(&appmodels.SysRole{
		BaseModel:    appmodels.BaseModel{ID: roleID},
		Name:         fmt.Sprintf("management-role-%d", roleID),
		Status:       1,
		DataScope:    dataScope,
		CheckedDepts: checkedDepts,
	}).Error)
	require.NoError(t, db.Create(&appmodels.SysUserRole{UserID: userID, RoleID: roleID}).Error)
}
