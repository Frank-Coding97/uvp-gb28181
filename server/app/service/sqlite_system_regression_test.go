package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func TestSQLiteSystemCRUDUsesPinnedBaseline(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	ctx := context.Background()

	status := int8(1)
	parentDeptID := uint(1)
	dept := &models.SysDepartment{
		BaseModel: models.BaseModel{ID: 10001}, ParentID: &parentDeptID, Name: "SQLite child department",
		Status: &status, Sort: intPtr(1), CreatedBy: 10001,
	}
	require.NoError(t, dept.Create(ctx))
	loadedDept := models.NewSysDepartment()
	require.NoError(t, loadedDept.GetDepartmentByID(ctx, dept.ID))
	require.Equal(t, dept.Name, loadedDept.Name)
	updatedDept, err := NewSysDepartmentService().Update(ctx, &models.SysDepartmentUpdateRequest{
		ID: dept.ID, ParentID: &parentDeptID, Name: "SQLite updated department", Status: &status, Sort: intPtr(2),
	})
	require.NoError(t, err)
	require.Equal(t, "SQLite updated department", updatedDept.Name)

	role := &models.SysRole{
		BaseModel: models.BaseModel{ID: 10001}, Name: "SQLite role", Status: 1, ParentID: 0,
		DataScope: 3, CreatedBy: 10001,
	}
	require.NoError(t, db.Create(role).Error)
	var loadedRole models.SysRole
	require.NoError(t, db.First(&loadedRole, "id = ?", role.ID).Error)
	require.Equal(t, role.Name, loadedRole.Name)
	loadedRole.Name = "SQLite updated role"
	require.NoError(t, db.Save(&loadedRole).Error)
	require.NoError(t, db.Delete(&loadedRole).Error)
	var roleCount int64
	require.NoError(t, db.Model(&models.SysRole{}).Where("id = ?", role.ID).Count(&roleCount).Error)
	require.Zero(t, roleCount)

	menu := &models.SysMenu{
		BaseModel: models.BaseModel{ID: 10001}, Path: "/sqlite-system", Name: "SQLiteSystem", Title: "SQLite system",
		Type: 2, Permission: "sqlite:system:read", CreatedBy: 10001,
	}
	require.NoError(t, db.Create(menu).Error)
	loadedMenu := models.NewSysMenu()
	require.NoError(t, loadedMenu.Find(ctx, func(query *gorm.DB) *gorm.DB { return query.Where("id = ?", menu.ID) }))
	require.Equal(t, menu.Permission, loadedMenu.Permission)
	loadedMenu.Title = "SQLite system updated"
	require.NoError(t, db.Save(loadedMenu).Error)
	require.NoError(t, db.Delete(loadedMenu).Error)

	api := &models.SysApi{
		BaseModel: models.BaseModel{ID: 10001}, Title: "SQLite API", Path: "/sqlite-system", Method: "GET",
		ApiGroup: "system", CreatedBy: 10001,
	}
	require.NoError(t, db.Create(api).Error)
	loadedAPI := models.NewSysApi()
	require.NoError(t, loadedAPI.FindByPathAndMethod(db, api.Path, api.Method))
	require.Equal(t, api.ID, loadedAPI.ID)
	loadedAPI.Title = "SQLite API updated"
	require.NoError(t, db.Save(loadedAPI).Error)
	require.NoError(t, db.Delete(loadedAPI).Error)

	dictName, dictCode, dictDescription := "SQLite dictionary", "sqlite_system", "baseline CRUD"
	dict := &models.SysDict{Name: &dictName, Code: &dictCode, Status: &status, Description: &dictDescription, CreatedBy: uintPtr(10001)}
	require.NoError(t, dict.Create(ctx))
	itemName, itemValue := "Enabled", "1"
	item := &models.SysDictItem{Name: &itemName, Value: &itemValue, Status: &status, DictID: &dict.ID}
	require.NoError(t, item.Create(ctx))
	items, err := item.FindByDictCode(ctx, dictCode)
	require.NoError(t, err)
	require.Len(t, items, 1)
	itemName = "Enabled updated"
	item.Name = &itemName
	require.NoError(t, item.Update(ctx))
	require.NoError(t, item.Delete(ctx))
	require.NoError(t, dict.Delete(ctx))

	paramService := NewSysParamService()
	param, err := paramService.Add(ctx, &models.SysParamAddRequest{Name: "SQLite parameter", Code: "sqlite.system.parameter", Value: "v1", Status: 1})
	require.NoError(t, err)
	param, err = paramService.Update(ctx, &models.SysParamUpdateRequest{
		ID: param.ID, Name: "SQLite parameter updated", Code: "sqlite.system.parameter", Value: "v2", Status: 1,
	})
	require.NoError(t, err)
	require.Equal(t, "v2", *param.Value)
	loadedParam, err := paramService.GetByCode(ctx, "sqlite.system.parameter")
	require.NoError(t, err)
	require.Equal(t, param.ID, loadedParam.ID)
	require.NoError(t, paramService.Delete(ctx, param.ID))

	affix := &models.SysAffix{
		BaseModel: models.BaseModel{ID: 10001}, Name: "sqlite.txt", Path: "/tmp/sqlite.txt", Url: "/uploads/sqlite.txt",
		FileMd5: "0123456789abcdef0123456789abcdef", Size: 7, Ftype: "text/plain", CreatedBy: 10001, Suffix: ".txt",
	}
	require.NoError(t, affix.Create(ctx))
	loadedAffix := models.NewSysAffix()
	require.NoError(t, loadedAffix.GetByID(ctx, affix.ID))
	require.Equal(t, affix.Name, loadedAffix.Name)
	loadedAffix.Name = "sqlite-updated.txt"
	require.NoError(t, loadedAffix.Update(ctx))
	require.NoError(t, loadedAffix.Delete(ctx))

	user := &models.User{BaseModel: models.BaseModel{ID: 10001}, Username: "sqlite-system-user", Password: "x", Status: 1, DeptID: dept.ID, CreatedBy: 10001}
	require.NoError(t, user.Create(ctx))
	loadedUser := models.NewUser()
	require.NoError(t, loadedUser.GetUserByID(ctx, user.ID))
	require.Equal(t, user.Username, loadedUser.Username)
	require.NoError(t, user.DeleteByID(ctx, user.ID))
	deletedUser := models.NewUser()
	require.NoError(t, deletedUser.GetUserByID(ctx, user.ID))
	require.True(t, deletedUser.IsEmpty())

	var seededDepartment models.SysDepartment
	require.NoError(t, db.First(&seededDepartment, "id = ?", 1).Error)
}

func TestSQLiteSystemSessionRefreshCASAllowsExactlyOneWinner(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	now := time.Now().UTC()
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })
	tokens := &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
	user := &models.User{BaseModel: models.BaseModel{ID: 10002}, Username: "sqlite-refresh-user", Password: "x", Status: 1}
	require.NoError(t, db.Create(user).Error)
	pair, err := service.CreateLogin(context.Background(), user, LoginMetadata{ClientIP: "10.0.0.1"}, tokens, 24*time.Hour)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, rotateErr := service.RotateRefresh(ctx, pair.RefreshToken, tokens)
			results <- rotateErr
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for rotateErr := range results {
		if rotateErr == nil {
			success++
		}
	}
	require.Equal(t, 1, success)
}

func TestSQLiteSystemSessionRefreshAndForcedLogoutCannotReviveOldToken(t *testing.T) {
	for _, order := range []string{"refresh-first", "logout-first"} {
		t.Run(order, func(t *testing.T) {
			db := newSQLiteSystemTestDB(t)
			now := time.Now().UTC()
			service := NewAuthSessionService(db)
			service.SetClock(func() time.Time { return now })
			tokens := &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
			user := &models.User{BaseModel: models.BaseModel{ID: 10003}, Username: "sqlite-logout-user-" + order, Password: "x", Status: 1}
			require.NoError(t, db.Create(user).Error)
			pair, err := service.CreateLogin(context.Background(), user, LoginMetadata{}, tokens, 24*time.Hour)
			require.NoError(t, err)
			if order == "refresh-first" {
				_, err = service.RotateRefresh(context.Background(), pair.RefreshToken, tokens)
				require.NoError(t, err)
				_, err = service.ForceLogout(context.Background(), pair.SID, "other-session", 10003)
				require.NoError(t, err)
			} else {
				_, err = service.ForceLogout(context.Background(), pair.SID, "other-session", 10003)
				require.NoError(t, err)
				_, err = service.RotateRefresh(context.Background(), pair.RefreshToken, tokens)
				require.ErrorIs(t, err, ErrSessionUnavailable)
			}
			_, err = service.RotateRefresh(context.Background(), pair.RefreshToken, tokens)
			require.ErrorIs(t, err, ErrSessionUnavailable)
			_, err = service.Authenticate(context.Background(), pair.SID, user.ID)
			require.ErrorIs(t, err, ErrSessionUnavailable)
		})
	}
}

func TestSQLiteSystemConcurrentRefreshAndForcedLogoutCannotReviveOldToken(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	now := time.Now().UTC()
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })
	tokens := &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}
	user := &models.User{BaseModel: models.BaseModel{ID: 10005}, Username: "sqlite-concurrent-logout", Password: "x", Status: 1}
	require.NoError(t, db.Create(user).Error)
	pair, err := service.CreateLogin(context.Background(), user, LoginMetadata{}, tokens, 24*time.Hour)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	refreshErr := make(chan error, 1)
	logoutErr := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, rotateErr := service.RotateRefresh(ctx, pair.RefreshToken, tokens)
		refreshErr <- rotateErr
	}()
	go func() {
		defer wg.Done()
		<-start
		_, forceErr := service.ForceLogout(ctx, pair.SID, "other-session", 10006)
		logoutErr <- forceErr
	}()
	close(start)
	wg.Wait()
	require.NoError(t, <-logoutErr)
	refreshResult := <-refreshErr
	if refreshResult != nil {
		require.ErrorIs(t, refreshResult, ErrSessionUnavailable)
	}
	_, err = service.RotateRefresh(context.Background(), pair.RefreshToken, tokens)
	require.ErrorIs(t, err, ErrSessionUnavailable)
	_, err = service.Authenticate(context.Background(), pair.SID, user.ID)
	require.ErrorIs(t, err, ErrSessionUnavailable)
}

func TestSQLiteSystemSessionStoreFailureFailsClosedAndReopenPersistsRevocation(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	now := time.Now().UTC()
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })
	require.NoError(t, service.Create(context.Background(), testSession("sqlite-store-session", 10004, now)))
	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	_, err = service.Authenticate(context.Background(), "sqlite-store-session", 10004)
	require.ErrorIs(t, err, ErrSessionStore)
	_, err = service.Revoke(context.Background(), "sqlite-store-session", "forced", uintPtr(10005))
	require.ErrorIs(t, err, ErrSessionStore)
}

func TestSQLiteSystemRevokeTransactionRollbackKeepsSessionLive(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	now := time.Now().UTC()
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })
	require.NoError(t, service.Create(context.Background(), testSession("sqlite-rollback-session", 10006, now)))
	sentinel := errors.New("injected transaction failure")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := service.RevokeAllForUserTx(tx, 10006, "user_disabled"); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	_, err = service.Authenticate(context.Background(), "sqlite-rollback-session", 10006)
	require.NoError(t, err)
}

func TestSQLiteSystemRevokeTransactionStoreFailureFailsClosed(t *testing.T) {
	db := newSQLiteSystemTestDB(t)
	now := time.Now().UTC()
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })
	require.NoError(t, service.Create(context.Background(), testSession("sqlite-revoke-failure", 10009, now)))
	require.NoError(t, db.Exec(`CREATE TRIGGER revoke_failure_trigger
BEFORE UPDATE OF revoked_at ON sys_user_sessions
BEGIN
  SELECT RAISE(ABORT, 'injected revoke failure');
END`).Error)
	err := db.Transaction(func(tx *gorm.DB) error {
		return service.RevokeAllForUserTx(tx, 10009, "user_disabled")
	})
	require.ErrorIs(t, err, ErrSessionStore)
	_, err = service.Authenticate(context.Background(), "sqlite-revoke-failure", 10009)
	require.NoError(t, err)
}

func TestSQLiteSystemReopenPreservesRevokedSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	db := newSQLiteSystemTestDBAt(t, path)
	now := time.Now().UTC()
	service := NewAuthSessionService(db)
	service.SetClock(func() time.Time { return now })
	require.NoError(t, service.Create(context.Background(), testSession("sqlite-reopen-session", 10007, now)))
	revoked, err := service.Revoke(context.Background(), "sqlite-reopen-session", "forced", uintPtr(10008))
	require.NoError(t, err)
	require.True(t, revoked)
	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	reopened, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	reopenedRaw, err := reopened.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopenedRaw.Close() })
	result, err := sqlitebootstrap.Initialize(context.Background(), reopened)
	require.NoError(t, err)
	require.False(t, result.Created)
	reopenedService := NewAuthSessionService(reopened)
	reopenedService.SetClock(func() time.Time { return now })
	_, err = reopenedService.Authenticate(context.Background(), "sqlite-reopen-session", 10007)
	require.ErrorIs(t, err, ErrSessionUnavailable)
	var stored models.SysUserSession
	require.NoError(t, reopened.Unscoped().First(&stored, "sid = ?", "sqlite-reopen-session").Error)
	require.NotNil(t, stored.RevokedAt)
}

func intPtr(value int) *int { return &value }
