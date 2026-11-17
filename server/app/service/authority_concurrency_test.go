package service

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	gbvideoshare "uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"
)

// These tests use separate SQLite pools for signing and authority changes.
// They establish SQLite behavior only, not behavior of other database engines.
type authorityConcurrencyFixture struct {
	ctx         context.Context
	authorityDB *gorm.DB
	issuanceDB  *gorm.DB
	channel     gbmodels.GbChannel
	policies    models.SysApiList
	permission  *PermissionService
	signer      *gbvideoshare.Signer
}

func newAuthorityConcurrencyFixture(t *testing.T) *authorityConcurrencyFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	path := filepath.Join(t.TempDir(), "authority.db")
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		pool, err := db.DB()
		require.NoError(t, err)
		pool.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = pool.Close() })
		require.NoError(t, db.Exec("PRAGMA busy_timeout=5000").Error)
		return db
	}
	authorityDB := open()
	require.NoError(t, authorityDB.Exec("PRAGMA journal_mode=WAL").Error)
	issuanceDB := open()
	previousConfig, previousDB, previousCasbin := app.ConfigYml, app.GormDbSqlite, app.CasbinV2
	app.ConfigYml, app.GormDbSqlite, app.CasbinV2 = authorityTestConfig{}, authorityDB, nil
	t.Cleanup(func() { app.ConfigYml, app.GormDbSqlite, app.CasbinV2 = previousConfig, previousDB, previousCasbin })
	require.NoError(t, authorityDB.AutoMigrate(&models.SysOperationLog{}, &authoritytx.Gate{}, &gbmodels.GbChannelVideoShare{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceGrant{}, &models.User{}, &models.SysRole{}, &models.SysUserRole{}, &models.SysDepartment{}))
	require.NoError(t, authorityDB.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch INTEGER NOT NULL DEFAULT 1").Error)
	require.NoError(t, authorityDB.Table("sys_casbin_rule").AutoMigrate(&gormadapter.CasbinRule{}))
	require.NoError(t, authorityDB.Create(&authoritytx.Gate{ID: 1}).Error)
	require.NoError(t, authorityDB.Create(&models.User{BaseModel: models.BaseModel{ID: 7}, Username: "creator", Status: 1, DeptID: 10}).Error)
	require.NoError(t, authorityDB.Create(&models.SysRole{BaseModel: models.BaseModel{ID: 3}, Name: "sharing", Status: 1, DataScope: 1}).Error)
	require.NoError(t, authorityDB.Create(&models.SysUserRole{UserID: 7, RoleID: 3}).Error)
	require.NoError(t, authorityDB.Create(&gbmodels.GbDevice{DeviceID: "device", OwnerDeptID: 10}).Error)
	channel := gbmodels.GbChannel{DeviceID: "device", ChannelID: "camera", OwnerDeptID: 10}
	require.NoError(t, authorityDB.Create(&channel).Error)
	permission := NewPermissionService()
	policies := models.SysApiList{&models.SysApi{Path: "/api/gb28181/channel-shares/:id", Method: "PUT"}, &models.SysApi{Path: "/api/gb28181/play/:device/:channel", Method: "POST"}}
	require.NoError(t, permission.Transaction(ctx, func(_ *gorm.DB, p *PermissionService) error {
		if err := p.AddRoleForUser(ctx, 7, []uint{3}); err != nil {
			return err
		}
		return p.AddPoliciesForRole(ctx, 3, policies)
	}))
	signer, err := gbvideoshare.NewSigner(make([]byte, 32))
	require.NoError(t, err)
	return &authorityConcurrencyFixture{ctx: ctx, authorityDB: authorityDB, issuanceDB: issuanceDB, channel: channel, policies: policies, permission: permission, signer: signer}
}

func (f *authorityConcurrencyFixture) shareService(t *testing.T, auto bool, authorizer gbvideoshare.Authorizer) (*gbvideoshare.Service, func() (gbvideoshare.Link, error)) {
	t.Helper()
	s, err := gbvideoshare.NewService(f.issuanceDB, f.signer, func() time.Duration { return 5 * time.Minute }, authorizer)
	require.NoError(t, err)
	if auto {
		require.NoError(t, s.EnsurePending(f.ctx, f.channel.ID, 1, "device", 1, nil, 1))
		return s, func() (gbvideoshare.Link, error) { return s.AcquireLink(f.ctx, f.channel.ID, 7, 1) }
	}
	return s, func() (gbvideoshare.Link, error) { return s.SetEnabled(f.ctx, f.channel.ID, 7, true, 0) }
}

// Observe when a competing connection actually reaches its first database write,
// so a scheduler delay cannot be mistaken for waiting on the authority lock.
func observeAuthorityWrite(t *testing.T, db *gorm.DB) <-chan struct{} {
	t.Helper()
	attempted := make(chan struct{}, 1)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:authority_write_attempt", func(*gorm.DB) {
		select {
		case attempted <- struct{}{}:
		default:
		}
	}))
	return attempted
}

func waitAuthoritySignal(t *testing.T, ctx context.Context, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatal("timed out waiting for authority operation", ctx.Err())
	}
}

type authorityIssuedLink struct {
	link gbvideoshare.Link
	err  error
}

func TestAuthorityConcurrentIssuanceCommitsBeforeRevocation(t *testing.T) {
	for _, mode := range []struct {
		name string
		auto bool
	}{{"manual first creation", false}, {"automatic first activation", true}} {
		t.Run(mode.name, func(t *testing.T) {
			f := newAuthorityConcurrencyFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var unblock, pause sync.Once
			defer unblock.Do(func() { close(release) })
			realAuthorize := gbvideoshare.NewTransactionalAuthorizer()
			authorizer := func(ctx context.Context, tx *gorm.DB, actorID, channelID uint) (gbvideoshare.AuthorizationSnapshot, error) {
				snapshot, err := realAuthorize(ctx, tx, actorID, channelID)
				if err != nil {
					return snapshot, err
				}
				pause.Do(func() {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
					}
				})
				return snapshot, ctx.Err()
			}
			service, issue := f.shareService(t, mode.auto, authorizer)
			issued := make(chan authorityIssuedLink, 1)
			go func() { link, err := issue(); issued <- authorityIssuedLink{link, err} }()
			waitAuthoritySignal(t, f.ctx, entered)
			writeAttempted := observeAuthorityWrite(t, f.authorityDB)
			revoked := make(chan error, 1)
			go func() { revoked <- f.permission.DeleteRoleApis(f.ctx, 3) }()
			waitAuthoritySignal(t, f.ctx, writeAttempted)
			select {
			case err := <-revoked:
				t.Fatalf("revocation finished before the in-progress issuance committed: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			unblock.Do(func() { close(release) })
			var first authorityIssuedLink
			select {
			case first = <-issued:
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			require.NoError(t, first.err)
			require.NotEmpty(t, first.link.Signature)
			select {
			case err := <-revoked:
				require.NoError(t, err)
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			var retired gbmodels.GbChannelVideoShare
			require.NoError(t, f.authorityDB.Where("channel_pk = ?", f.channel.ID).Take(&retired).Error)
			require.False(t, retired.Enabled)
			require.NotNil(t, retired.RevokedAt)
			require.Equal(t, first.link.Revision+1, retired.Revision)
			require.NoError(t, f.permission.AddPoliciesForRole(f.ctx, 3, f.policies))
			_, err := service.Resolve(f.ctx, first.link.PublicID, first.link.Signature)
			require.ErrorIs(t, err, gbvideoshare.ErrExpired, "quick regrant must never revive the credential")
		})
	}
}

func TestAuthorityConcurrentRevocationCommitsBeforeIssuance(t *testing.T) {
	for _, mode := range []struct {
		name string
		auto bool
	}{{"manual first creation", false}, {"automatic first activation", true}} {
		t.Run(mode.name, func(t *testing.T) {
			f := newAuthorityConcurrencyFixture(t)
			authorizing := make(chan struct{}, 1)
			realAuthorize := gbvideoshare.NewTransactionalAuthorizer()
			authorizer := func(ctx context.Context, tx *gorm.DB, actorID, channelID uint) (gbvideoshare.AuthorizationSnapshot, error) {
				select {
				case authorizing <- struct{}{}:
				default:
				}
				return realAuthorize(ctx, tx, actorID, channelID)
			}
			_, issue := f.shareService(t, mode.auto, authorizer)
			entered, release := make(chan struct{}), make(chan struct{})
			var unblock sync.Once
			defer unblock.Do(func() { close(release) })
			revoked := make(chan error, 1)
			go func() {
				revoked <- f.permission.Transaction(f.ctx, func(_ *gorm.DB, permission *PermissionService) error {
					if err := permission.DeleteRoleApis(f.ctx, 3); err != nil {
						return err
					}
					close(entered)
					select {
					case <-release:
						return nil
					case <-f.ctx.Done():
						return f.ctx.Err()
					}
				})
			}()
			waitAuthoritySignal(t, f.ctx, entered)
			writeAttempted := observeAuthorityWrite(t, f.issuanceDB)
			issued := make(chan authorityIssuedLink, 1)
			go func() { link, err := issue(); issued <- authorityIssuedLink{link, err} }()
			waitAuthoritySignal(t, f.ctx, writeAttempted)
			select {
			case result := <-issued:
				t.Fatalf("issuance finished before revocation committed: %+v", result)
			case <-authorizing:
				t.Fatal("issuance read authority before the revocation transaction released its lock")
			case <-time.After(100 * time.Millisecond):
			}
			unblock.Do(func() { close(release) })
			select {
			case err := <-revoked:
				require.NoError(t, err)
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			select {
			case result := <-issued:
				require.ErrorIs(t, result.err, gbvideoshare.ErrExpired)
				require.Empty(t, result.link.Signature)
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			var rows []gbmodels.GbChannelVideoShare
			require.NoError(t, f.authorityDB.Where("channel_pk = ?", f.channel.ID).Find(&rows).Error)
			if mode.auto {
				require.Len(t, rows, 1)
				require.Nil(t, rows[0].ActivatedAt)
				require.Nil(t, rows[0].CreatedBy)
				require.Nil(t, rows[0].ExpiresAt)
			} else {
				require.Empty(t, rows)
			}
		})
	}
}
