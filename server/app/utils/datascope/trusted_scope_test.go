package datascope

import (
	"context"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/models"
)

func TestResolveOwnerDeptAccessByUserID(t *testing.T) {
	db := newTrustedScopeDB(t)
	root, child := uint(10), uint(11)
	enabled := int8(1)
	require.NoError(t, db.Create(&models.SysDepartment{BaseModel: models.BaseModel{ID: root}, Name: "root", Status: &enabled}).Error)
	require.NoError(t, db.Create(&models.SysDepartment{BaseModel: models.BaseModel{ID: child}, ParentID: &root, Name: "child", Status: &enabled}).Error)

	tests := []struct {
		name       string
		dataScope  int8
		checked    string
		wantFull   bool
		wantDeptID []uint
	}{
		{name: "full", dataScope: 1, wantFull: true},
		{name: "selected", dataScope: 2, checked: "11,10", wantDeptID: []uint{10, 11}},
		{name: "department", dataScope: 3, wantDeptID: []uint{10}},
		{name: "department children", dataScope: 4, wantDeptID: []uint{10, 11}},
	}
	for index, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			userID := uint(100 + index)
			seedTrustedScopeUser(t, db, userID, root, tc.dataScope, tc.checked, true)
			access, err := ResolveOwnerDeptAccessByUserID(context.Background(), db, userID)
			require.NoError(t, err)
			require.Equal(t, tc.wantFull, access.FullAccess)
			require.Equal(t, tc.wantDeptID, access.DeptIDs)
		})
	}
}

func TestResolveOwnerDeptAccessByUserIDFailsClosed(t *testing.T) {
	db := newTrustedScopeDB(t)
	seedTrustedScopeUser(t, db, 201, 10, 3, "", false)
	_, err := ResolveOwnerDeptAccessByUserID(context.Background(), db, 201)
	require.ErrorIs(t, err, ErrOwnerDeptAccessDenied)

	user := &models.User{BaseModel: models.BaseModel{ID: 202}, Username: "revoked", Password: "x", Status: 1, DeptID: 10}
	require.NoError(t, db.Create(user).Error)
	_, err = ResolveOwnerDeptAccessByUserID(context.Background(), db, 202)
	require.ErrorIs(t, err, ErrOwnerDeptAccessDenied)

	_, err = ResolveOwnerDeptAccessByUserID(context.Background(), db, 999)
	require.ErrorIs(t, err, ErrOwnerDeptAccessDenied)

	require.NoError(t, db.Migrator().DropTable(&models.SysRole{}))
	_, err = ResolveOwnerDeptAccessByUserID(context.Background(), db, 202)
	require.Error(t, err)
}

func newTrustedScopeDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.SysRole{}, &models.SysUserRole{}, &models.SysDepartment{}))
	return db
}

func seedTrustedScopeUser(t *testing.T, db *gorm.DB, userID, deptID uint, dataScope int8, checked string, userEnabled bool) {
	t.Helper()
	status := int8(0)
	if userEnabled {
		status = 1
	}
	user := &models.User{BaseModel: models.BaseModel{ID: userID}, Username: fmt.Sprintf("user-%d", userID), Password: "x", Status: status, DeptID: deptID}
	require.NoError(t, db.Create(user).Error)
	if !userEnabled {
		require.NoError(t, db.Model(user).UpdateColumn("status", 0).Error)
	}
	role := &models.SysRole{Name: "role", Status: 1, DataScope: dataScope, CheckedDepts: checked}
	require.NoError(t, db.Create(role).Error)
	require.NoError(t, db.Create(&models.SysUserRole{UserID: userID, RoleID: role.ID}).Error)
}
