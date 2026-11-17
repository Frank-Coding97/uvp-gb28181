//go:build video_share_database

package migration

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	mysqlconfig "github.com/go-sql-driver/mysql"
	dameng "github.com/godoes/gorm-dameng"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"
)

// Run only in a freshly provisioned, disposable database/schema. The harness
// never creates a database, drops tables, or falls back to a different engine.
// UVP_VIDEO_SHARE_DATABASE_ALLOW_DDL=1 is mandatory, together with one or more:
// UVP_VIDEO_SHARE_TEST_DSN_MYSQL / POSTGRES / SQLSERVER / SQLITE / DM.
// Native database names (DM: schema/user) must start with uvp_share_. SQLite
// requires an absolute uvp_share_*.db path; do not use an existing application DB.
//
//	Example: go test -tags video_share_database ./app/gb28181/migration \
//	  -run '^TestVideoShareDatabase' -v -count=1
func TestVideoShareDatabaseSafety(t *testing.T) {
	for _, name := range []string{"production", "uvp", "uvp_share_", "uvp_share_test;DROP", "../uvp_share_test"} {
		require.Error(t, videoShareDatabaseSafety("1", name), name)
	}
	require.Error(t, videoShareDatabaseSafety("", "uvp_share_test"))
	require.NoError(t, videoShareDatabaseSafety("1", "uvp_share_test_20261117"))
}

func videoShareDatabaseSafety(optIn, name string) error {
	if optIn != "1" || !regexp.MustCompile(`^uvp_share_[a-z0-9_]+$`).MatchString(strings.ToLower(name)) {
		return errors.New("refusing DDL: explicit opt-in and uvp_share_ test database/schema required")
	}
	return nil
}

type videoShareDatabaseTarget struct {
	dialect     Dialect
	suffix      string
	identitySQL string
}

func TestVideoShareDatabaseLive(t *testing.T) {
	for _, target := range []videoShareDatabaseTarget{
		{DialectMySQL, "", "SELECT DATABASE()"},
		{DialectPostgres, "-postgresql", "SELECT current_database()"},
		{DialectSQLServer, "-sqlserver", "SELECT DB_NAME()"},
		{DialectSQLite, "-sqlite", ""},
		{DialectDM, "-dm", "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL"},
	} {
		t.Run(string(target.dialect), func(t *testing.T) { videoShareDatabaseRun(t, target) })
	}
}

func videoShareDatabaseDriver(t *testing.T, dialect Dialect, dsn string) gorm.Dialector {
	t.Helper()
	// Validate the DSN target before even connecting. Never print DSNs or errors
	// returned by driver parsers because they may include credentials.
	var name string
	var driver gorm.Dialector
	switch dialect {
	case DialectMySQL:
		config, err := mysqlconfig.ParseDSN(dsn)
		if err != nil {
			t.Fatal("invalid MySQL test DSN; credentials suppressed")
		}
		name, driver = config.DBName, mysql.Open(dsn)
	case DialectPostgres:
		config, err := pgconn.ParseConfig(dsn)
		if err != nil {
			t.Fatal("invalid PostgreSQL test DSN; credentials suppressed")
		}
		name, driver = config.Database, postgres.Open(dsn)
	case DialectSQLServer, DialectDM:
		config, err := url.Parse(dsn)
		if err != nil || config.User == nil {
			t.Fatal("test DSN must be an explicit URL; credentials suppressed")
		}
		if dialect == DialectSQLServer {
			if config.Scheme != "sqlserver" {
				t.Fatal("SQLServer test URL required")
			}
			name, driver = config.Query().Get("database"), sqlserver.Open(dsn)
		} else {
			if config.Scheme != "dm" {
				t.Fatal("DM test URL required")
			}
			name = config.Query().Get("schema")
			if name == "" {
				name = config.User.Username()
			}
			driver = dameng.Open(dsn)
		}
	case DialectSQLite:
		if !filepath.IsAbs(dsn) || filepath.Ext(dsn) != ".db" || strings.ContainsAny(dsn, "?#") {
			t.Fatal("SQLite test DSN must be an absolute uvp_share_*.db file path")
		}
		name, driver = strings.TrimSuffix(filepath.Base(dsn), ".db"), sqlite.Open(dsn)
	default:
		t.Fatal("unsupported database dialect")
	}
	require.NoError(t, videoShareDatabaseSafety(os.Getenv("UVP_VIDEO_SHARE_DATABASE_ALLOW_DDL"), name))
	return driver
}

func videoShareDatabaseOpen(t *testing.T, target videoShareDatabaseTarget, dsn string) *gorm.DB {
	t.Helper()
	driver := videoShareDatabaseDriver(t, target.dialect, dsn)
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal("database connection failed; credentials suppressed")
	}
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	db = db.WithContext(ctx)
	require.NoError(t, raw.PingContext(ctx), "explicitly configured database must be reachable")
	require.Equal(t, target.dialect, DialectOf(db.Dialector))
	if target.identitySQL != "" {
		var actual string
		require.NoError(t, db.Raw(target.identitySQL).Scan(&actual).Error)
		require.NoError(t, videoShareDatabaseSafety("1", actual), "connected database/schema identity must also be isolated")
	} else {
		var attached []struct{ Name, File string }
		require.NoError(t, db.Raw("PRAGMA database_list").Scan(&attached).Error)
		require.Len(t, attached, 1, "only the explicitly supplied SQLite main database is allowed")
		require.Equal(t, "main", attached[0].Name)
		expectedPath, pathErr := filepath.EvalSymlinks(dsn)
		require.NoError(t, pathErr)
		actualPath, pathErr := filepath.EvalSymlinks(attached[0].File)
		require.NoError(t, pathErr)
		require.Equal(t, expectedPath, actualPath)
		require.NoError(t, db.Exec("PRAGMA busy_timeout = 10000").Error)
	}
	return db
}

func videoShareDatabaseRun(t *testing.T, target videoShareDatabaseTarget) {
	dsn := os.Getenv("UVP_VIDEO_SHARE_TEST_DSN_" + strings.ToUpper(string(target.dialect)))
	if dsn == "" {
		t.Skip("no explicit isolated DSN: native database acceptance NOT executed")
	}
	db := videoShareDatabaseOpen(t, target, dsn)
	tables, err := db.Migrator().GetTables()
	require.NoError(t, err)
	require.Empty(t, tables, "refusing DDL in a nonempty database/schema; nothing is dropped")
	up, err := (&embedSource{dialect: target.dialect}).ReadSQL("2026-11-17-channel-video-share" + target.suffix + ".sql")
	require.NoError(t, err)
	executor := &dbExecutor{db: db}
	require.NoError(t, executor.ExecSQL(up), "actual embedded share UP SQL must execute on its native engine")
	require.NoError(t, executor.ExecSQL(up), "actual embedded share UP SQL must be safely reentrant")
	var gate authoritytx.Gate
	require.NoError(t, db.Take(&gate, "id = ?", 1).Error)
	require.Zero(t, gate.Revision)
	var gateCount int64
	require.NoError(t, db.Model(&authoritytx.Gate{}).Count(&gateCount).Error)
	require.EqualValues(t, 1, gateCount)
	t.Run("uniqueness_and_revision", func(t *testing.T) { videoShareDatabaseConstraints(t, db) })
	// A second GORM/sql.DB pool is essential: serialization cannot be explained
	// by local MaxOpenConns=1 queueing.
	second := videoShareDatabaseOpen(t, target, dsn)
	t.Run("gate_commit_and_rollback", func(t *testing.T) { videoShareDatabaseGate(t, db, second) })
	t.Run("service_issue_revoke_permanent", func(t *testing.T) { videoShareDatabaseLifecycle(t, db) })
	t.Logf("%s native UP/up, uniqueness, revision, cross-pool gate, rollback and service lifecycle executed", target.dialect)
}

func videoShareDatabaseConstraints(t *testing.T, db *gorm.DB) {
	now := time.Now().UTC()
	share := gbmodels.GbChannelVideoShare{ChannelPK: 901, PublicID: "native-unique-public", Generation: "native-generation", Source: "manual", DevicePK: 901, DeviceID: "34020000002000000901", KeyID: videoshare.KeyID, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&share).Error)
	require.EqualValues(t, 1, share.Revision)
	duplicate := share
	duplicate.ID, duplicate.PublicID = 0, "native-other-public"
	require.Error(t, db.Create(&duplicate).Error, "channel_pk must be unique")
	duplicate.ID, duplicate.ChannelPK, duplicate.PublicID = 0, 902, share.PublicID
	require.Error(t, db.Create(&duplicate).Error, "public_id must be unique")
	updated := db.Model(&gbmodels.GbChannelVideoShare{}).Where("id = ? AND revision = ?", share.ID, 1).UpdateColumn("revision", gorm.Expr("revision + 1"))
	require.NoError(t, updated.Error)
	require.EqualValues(t, 1, updated.RowsAffected)
	stale := db.Model(&gbmodels.GbChannelVideoShare{}).Where("id = ? AND revision = ?", share.ID, 1).UpdateColumn("revision", gorm.Expr("revision + 1"))
	require.NoError(t, stale.Error)
	require.Zero(t, stale.RowsAffected, "stale revision must not update")
	var stored gbmodels.GbChannelVideoShare
	require.NoError(t, db.Take(&stored, share.ID).Error)
	require.EqualValues(t, 2, stored.Revision)
	policy := gbmodels.GbDeviceVideoSharePolicy{DevicePK: 901, Source: "registration", InitializedAt: now, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&policy).Error)
	policy.ID = 0
	require.Error(t, db.Create(&policy).Error, "device policy must be unique per device")
}

func videoShareDatabaseGate(t *testing.T, first, second *gorm.DB) {
	require.Error(t, authoritytx.Acquire(context.Background(), first), "nontransaction gate acquisition must fail")
	for _, rollbackFirst := range []bool{true, false} {
		var before authoritytx.Gate
		require.NoError(t, first.Take(&before, "id = ?", 1).Error)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		tx := first.WithContext(ctx).Begin()
		require.NoError(t, tx.Error)
		require.NoError(t, authoritytx.Acquire(ctx, tx))
		started, finished := make(chan struct{}), make(chan error, 1)
		rollbackMarker := errors.New("intentional second rollback")
		go func() {
			finished <- second.WithContext(ctx).Transaction(func(other *gorm.DB) error {
				close(started)
				if err := authoritytx.Acquire(ctx, other); err != nil {
					return err
				}
				if !rollbackFirst {
					return rollbackMarker
				}
				return nil
			})
		}()
		select {
		case <-started:
		case <-ctx.Done():
			_ = tx.Rollback().Error
			cancel()
			t.Fatal("second physical-pool transaction did not start")
		}
		select {
		case err := <-finished:
			_ = tx.Rollback().Error
			cancel()
			t.Fatalf("second authority transaction escaped the first lock: %v", err)
		case <-time.After(200 * time.Millisecond):
		}
		if rollbackFirst {
			require.NoError(t, tx.Rollback().Error)
		} else {
			require.NoError(t, tx.Commit().Error)
		}
		select {
		case err := <-finished:
			if rollbackFirst {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, rollbackMarker)
			}
		case <-ctx.Done():
			cancel()
			t.Fatal("second authority transaction did not resume after lock release")
		}
		cancel()
		var after authoritytx.Gate
		require.NoError(t, first.Take(&after, "id = ?", 1).Error)
		require.Equal(t, before.Revision+1, after.Revision, "only the committed gate increment may persist")
	}
}

type videoShareDatabaseDevice struct {
	ID          uint   `gorm:"primaryKey;autoIncrement:false"`
	DeviceID    string `gorm:"size:20"`
	AccessEpoch uint64
	DeletedAt   gorm.DeletedAt
}

func (videoShareDatabaseDevice) TableName() string { return "gb_device" }

type videoShareDatabaseChannel struct {
	ID        uint   `gorm:"primaryKey;autoIncrement:false"`
	DeviceID  string `gorm:"size:20"`
	DeletedAt gorm.DeletedAt
}

func (videoShareDatabaseChannel) TableName() string { return "gb_channel" }

func videoShareDatabaseLifecycle(t *testing.T, db *gorm.DB) {
	// Only surrounding fixture tables use AutoMigrate. All share lifecycle tables
	// above were created by the exact embedded product migration SQL.
	require.NoError(t, db.AutoMigrate(&videoShareDatabaseDevice{}, &videoShareDatabaseChannel{}, &basemodels.SysOperationLog{}))
	device := videoShareDatabaseDevice{ID: 71, DeviceID: "34020000002000000071", AccessEpoch: 7}
	require.NoError(t, db.Create(&device).Error)
	require.NoError(t, db.Create(&videoShareDatabaseChannel{ID: 71, DeviceID: device.DeviceID}).Error)
	var allowed atomic.Bool
	allowed.Store(true)
	denied := errors.New("test creator authority revoked")
	signer, err := videoshare.NewSigner(make([]byte, 32))
	require.NoError(t, err)
	service, err := videoshare.NewService(db, signer, func() time.Duration { return 5 * time.Minute }, func(ctx context.Context, tx *gorm.DB, actor, channel uint) (videoshare.AuthorizationSnapshot, error) {
		if !allowed.Load() {
			return videoshare.AuthorizationSnapshot{}, denied
		}
		var current videoShareDatabaseDevice
		if err := tx.WithContext(ctx).Take(&current, "id = ?", device.ID).Error; err != nil {
			return videoshare.AuthorizationSnapshot{}, err
		}
		return videoshare.AuthorizationSnapshot{DevicePK: current.ID, DeviceID: current.DeviceID, DeviceEpoch: current.AccessEpoch}, nil
	})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, service.EnsurePending(ctx, 71, device.ID, device.DeviceID, 7, nil, 3))
	status, err := service.Status(ctx, 71, 9)
	require.NoError(t, err)
	require.True(t, status.Pending)
	require.Nil(t, status.ExpiresAt, "auto enable must not start expiry")
	issued, err := service.AcquireLink(ctx, 71, 9, status.Revision)
	require.NoError(t, err)
	require.NotEmpty(t, issued.Signature)
	require.WithinDuration(t, time.Now().Add(5*time.Minute), *issued.ExpiresAt, 5*time.Second)
	_, err = service.Resolve(ctx, issued.PublicID, issued.Signature)
	require.NoError(t, err)
	_, err = service.AcquireLink(ctx, 71, 9, status.Revision)
	require.ErrorIs(t, err, videoshare.ErrConflict, "stale revision must fail in the real service")
	again, err := service.AcquireLink(ctx, 71, 9, issued.Revision)
	require.NoError(t, err)
	require.Equal(t, issued.Signature, again.Signature)
	require.True(t, issued.ExpiresAt.Equal(*again.ExpiresAt), "repeated acquisition must not extend expiry")
	auditCount := func() int64 {
		var n int64
		require.NoError(t, db.Model(&basemodels.SysOperationLog{}).Count(&n).Error)
		return n
	}
	beforeAudit := auditCount()
	rollbackMarker := errors.New("intentional authority rollback")
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := authoritytx.Acquire(ctx, tx); err != nil {
			return err
		}
		if err := videoshare.RevokeCreatorTx(ctx, tx, 9, "test_rollback", time.Time{}); err != nil {
			return err
		}
		return rollbackMarker
	})
	require.ErrorIs(t, err, rollbackMarker)
	require.Equal(t, beforeAudit, auditCount(), "revocation audit rolls back with the share")
	_, err = service.Resolve(ctx, issued.PublicID, issued.Signature)
	require.NoError(t, err, "rolled back revocation must leave the original link usable")
	allowed.Store(false)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := authoritytx.Acquire(ctx, tx); err != nil {
			return err
		}
		return videoshare.RevokeCreatorTx(ctx, tx, 9, "test_access_revoked", time.Time{})
	}))
	allowed.Store(true)
	_, err = service.Resolve(ctx, issued.PublicID, issued.Signature)
	require.ErrorIs(t, err, videoshare.ErrExpired, "restoring creator authority must never revive a revoked link")
	require.Equal(t, beforeAudit+1, auditCount())
	current, err := service.Status(ctx, 71, 9)
	require.NoError(t, err)
	replacement, err := service.Regenerate(ctx, 71, 9, current.Revision)
	require.NoError(t, err)
	require.NotEqual(t, issued.Generation, replacement.Generation)
	_, err = service.Resolve(ctx, replacement.PublicID, replacement.Signature)
	require.NoError(t, err)
	_, err = service.Resolve(ctx, issued.PublicID, issued.Signature)
	require.Error(t, err, "regeneration must not revive the earlier credential")
}
