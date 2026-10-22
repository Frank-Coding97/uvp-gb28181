package client

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/openapi/models"
)

// ⛔ 这条用例锁的是一个**只在绿色包（SQLite）上复现**的线上缺陷：
// 能力授权保存时，事务持有连接池里唯一一条连接，而事务内的管理边界授权
// 又要通过应用库句柄再取一条 ⇒ 永久等待，直到请求上下文被取消，用户看到
// 「保存授权」转圈后 503 service unavailable。
//
// 之前的用例都用 `:memory:` + 默认连接池（或多连接池的 HTTP 夹具），
// 池子够大，永远复现不出来。所以这里必须**按生产口径**把上限压到 1。
func TestSetScopesDoesNotBlockOnSingleConnectionPool(t *testing.T) {
	db := openSingleConnectionSQLite(t)

	// 授权边界需要的三张业务表：用户、角色、用户角色、部门。
	require.NoError(t, db.AutoMigrate(
		&models.Client{}, &models.ClientScope{}, &models.Audit{},
		&appmodels.User{}, &appmodels.SysRole{}, &appmodels.SysDepartment{}, &appmodels.SysOperationLog{},
	))
	status := int8(1)
	require.NoError(t, db.Create(&appmodels.SysDepartment{Name: "总部", Status: &status}).Error)
	require.NoError(t, db.Create(&appmodels.User{Username: "operator", Status: 1, DeptID: 1}).Error)
	require.NoError(t, db.Create(&appmodels.SysRole{Name: "系统管理员", Status: 1, DataScope: 1}).Error)
	require.NoError(t, db.Exec("INSERT INTO sys_user_role (user_id, role_id) VALUES (1, 1)").Error)

	secrets, err := NewSecretManager(bytes.Repeat([]byte{0x7C}, 32), "scope-pool-test")
	require.NoError(t, err)
	service, err := NewService(db, secrets,
		WithClock(func() time.Time { return time.Unix(1790000000, 0).UTC() }),
		WithManagementBoundary(NewManagementScopeBoundary(db, allowAllEnforcer{})),
	)
	require.NoError(t, err)

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelSetup()
	view, _, err := service.Create(setupCtx, CreateRequest{Name: "单连接池", OwnerDeptID: 1, CreatedBy: 1})
	require.NoError(t, err)

	// 修复前：这里会一直阻塞到 deadline，返回 ErrConflict 之类也好、context 错误也好，
	// 总之不会成功。修复后：正常返回并落地 scope 行。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updated, err := service.SetScopes(ctx, view.ID, []string{"device:list"}, view.RowVersion, 1)
	require.NoErrorf(t, err, "能力授权保存不应在单连接池上阻塞：%v", err)
	require.Equal(t, view.RowVersion+1, updated.RowVersion)

	granted, err := service.GetScope(context.Background(), view.ID, "device:list")
	require.NoError(t, err)
	require.True(t, granted.Enabled)
}

// 单条 / 状态 / 密钥轮换走的是同一条授权路径，同样不能被事务锁死。
func TestSingleMutationPathsDoNotBlockOnSingleConnectionPool(t *testing.T) {
	db := openSingleConnectionSQLite(t)
	status := int8(1)
	require.NoError(t, db.AutoMigrate(
		&models.Client{}, &models.ClientScope{}, &models.Audit{},
		&appmodels.User{}, &appmodels.SysRole{}, &appmodels.SysDepartment{}, &appmodels.SysOperationLog{},
	))
	require.NoError(t, db.Create(&appmodels.SysDepartment{Name: "总部", Status: &status}).Error)
	require.NoError(t, db.Create(&appmodels.User{Username: "operator", Status: 1, DeptID: 1}).Error)
	require.NoError(t, db.Create(&appmodels.SysRole{Name: "系统管理员", Status: 1, DataScope: 1}).Error)
	require.NoError(t, db.Exec("INSERT INTO sys_user_role (user_id, role_id) VALUES (1, 1)").Error)

	secrets, err := NewSecretManager(bytes.Repeat([]byte{0x7C}, 32), "scope-pool-test")
	require.NoError(t, err)
	service, err := NewService(db, secrets,
		WithClock(func() time.Time { return time.Unix(1790000000, 0).UTC() }),
		WithManagementBoundary(NewManagementScopeBoundary(db, allowAllEnforcer{})),
	)
	require.NoError(t, err)

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelSetup()
	view, _, err := service.Create(setupCtx, CreateRequest{Name: "单连接池", OwnerDeptID: 1, CreatedBy: 1})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	scoped, err := service.SetScope(ctx, view.ID, "device:list", true, view.RowVersion, 1)
	require.NoErrorf(t, err, "单条能力授权不应阻塞：%v", err)

	disabled, err := service.SetStatus(ctx, view.ID, models.StatusDisabled, scoped.RowVersion, 1)
	require.NoErrorf(t, err, "启停不应阻塞：%v", err)
	require.Equal(t, models.StatusDisabled, disabled.Status)

	_, _, err = service.RotateSecret(ctx, view.ID, disabled.RowVersion, 1)
	require.NoErrorf(t, err, "密钥轮换不应阻塞：%v", err)
}

func openSingleConnectionSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "scope-pool.sqlite")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	// 与 gormhelper.GetOneSqliteClient 保持一致的绿色包口径。
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	return db
}

type allowAllEnforcer struct{}

func (allowAllEnforcer) Enforce(string, string, string, ...string) (bool, error) { return true, nil }
