package migration

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dameng "github.com/godoes/gorm-dameng"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Driver-boundary regression only: native syntax acceptance is a separate test.
func TestVideoShareNativeSQLExecutorBoundaries(t *testing.T) {
	for _, dialect := range []Dialect{DialectSQLServer, DialectDM} {
		t.Run(string(dialect), func(t *testing.T) {
			state := &videoShareBatchCapture{dialect: dialect}
			name := fmt.Sprintf("video_share_batch_%d", videoShareBatchID.Add(1))
			sql.Register(name, &videoShareBatchDriver{state: state})
			conn, err := sql.Open(name, "isolated-driver-boundary")
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			db, err := gorm.Open(sqlserver.New(sqlserver.Config{Conn: conn}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
			require.NoError(t, err)
			for _, direction := range []string{"", "-down"} {
				body, err := (&embedSource{dialect: dialect}).ReadSQL("2026-11-17-channel-video-share-" + string(dialect) + direction + ".sql")
				require.NoError(t, err)
				state.statements = nil
				require.NoError(t, (&dbExecutor{db: db}).ExecSQL(body))
				expected := 7
				if direction != "" {
					expected = 3
				}
				require.Len(t, state.statements, expected)
			}
		})
	}
}

// Explicitly opt in with a NEW uvp_share_* database/schema and ALLOW_DDL=1.
// No substitute engine: missing native databases are reported as unverified.
func TestVideoShareNativeSQLReentryAndDown(t *testing.T) {
	for _, dialect := range []Dialect{DialectSQLServer, DialectDM} {
		t.Run(string(dialect), func(t *testing.T) {
			dsn := os.Getenv("UVP_VIDEO_SHARE_TEST_DSN_" + strings.ToUpper(string(dialect)))
			if dsn == "" {
				t.Skip("native DSN absent: syntax, reentry and Down NOT verified")
			}
			u, err := url.Parse(dsn)
			if err != nil || u.User == nil {
				t.Fatal("invalid native test URL; credentials suppressed")
			}
			name, identity := u.Query().Get("database"), "SELECT DB_NAME()"
			var dialector gorm.Dialector
			if dialect == DialectDM {
				require.Equal(t, "dm", u.Scheme)
				name = u.Query().Get("schema")
				if name == "" {
					name = u.User.Username()
				}
				identity = "SELECT SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') FROM DUAL"
				dialector = dameng.Open(dsn)
			} else {
				require.Equal(t, "sqlserver", u.Scheme)
				dialector = sqlserver.Open(dsn)
			}
			require.Equal(t, "1", os.Getenv("UVP_VIDEO_SHARE_DATABASE_ALLOW_DDL"))
			require.True(t, regexp.MustCompile(`(?i)^uvp_share_[a-z0-9_]+$`).MatchString(name), "refusing DDL outside isolated test database/schema")
			db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Discard})
			if err != nil {
				t.Fatal("native connection failed; credentials suppressed")
			}
			raw, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = raw.Close() })
			raw.SetMaxOpenConns(1)
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			db = db.WithContext(ctx)
			var actual string
			require.NoError(t, db.Raw(identity).Scan(&actual).Error)
			require.Equal(t, strings.ToLower(name), strings.ToLower(actual))
			tables, err := db.Migrator().GetTables()
			require.NoError(t, err)
			require.Empty(t, tables, "refusing DDL in nonempty database/schema")
			source := &embedSource{dialect: dialect}
			base := "2026-11-17-channel-video-share-" + string(dialect)
			up, err := source.ReadSQL(base + ".sql")
			require.NoError(t, err)
			down, err := source.ReadSQL(base + "-down.sql")
			require.NoError(t, err)
			executor := &dbExecutor{db: db}
			require.NoError(t, executor.ExecSQL(up))
			require.NoError(t, db.Exec("UPDATE gb_video_share_authority SET revision=7 WHERE id=1").Error)
			require.NoError(t, executor.ExecSQL(up), "reentry must preserve existing authority")
			var revision, count int64
			require.NoError(t, db.Raw("SELECT revision FROM gb_video_share_authority WHERE id=1").Scan(&revision).Error)
			require.EqualValues(t, 7, revision)
			require.NoError(t, db.Raw("SELECT count(*) FROM gb_video_share_authority").Scan(&count).Error)
			require.EqualValues(t, 1, count)
			require.NoError(t, executor.ExecSQL(down))
			require.NoError(t, executor.ExecSQL(down), "Down must also be reentrant")
			for _, table := range []string{"gb_channel_video_share", "gb_device_video_share_policy", "gb_video_share_authority"} {
				query := "SELECT count(*) FROM sys.tables WHERE name = ? AND schema_id = SCHEMA_ID('dbo')"
				if dialect == DialectDM {
					query = "SELECT count(*) FROM ALL_TABLES WHERE OWNER = SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') AND TABLE_NAME = ?"
					table = strings.ToUpper(table)
				}
				require.NoError(t, db.Raw(query, table).Scan(&count).Error)
				require.Zero(t, count, table)
			}
			require.NoError(t, executor.ExecSQL(up), "UP after Down")
			require.NoError(t, db.Raw("SELECT revision FROM gb_video_share_authority WHERE id=1").Scan(&revision).Error)
			require.Zero(t, revision)
		})
	}
}

var videoShareBatchID atomic.Uint64

type videoShareBatchCapture struct {
	dialect    Dialect
	statements []string
}
type videoShareBatchDriver struct{ state *videoShareBatchCapture }
type videoShareBatchConn struct{ state *videoShareBatchCapture }

func (d *videoShareBatchDriver) Open(string) (driver.Conn, error) {
	return &videoShareBatchConn{state: d.state}, nil
}
func (c *videoShareBatchConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared migration")
}
func (c *videoShareBatchConn) Close() error { return nil }
func (c *videoShareBatchConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c *videoShareBatchConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	upper := strings.ToUpper(query)
	if c.state.dialect == DialectSQLServer {
		outsideLiterals := regexp.MustCompile(`'([^']|'')*'`).ReplaceAllString(upper, "''")
		beginCount := len(regexp.MustCompile(`\bBEGIN\b`).FindAllString(outsideLiterals, -1))
		endCount := len(regexp.MustCompile(`\bEND\b`).FindAllString(outsideLiterals, -1))
		if !strings.HasPrefix(upper, "IF ") || beginCount != endCount {
			return nil, errors.New("driver received split-prone SQL Server conditional DDL")
		}
	} else if strings.HasPrefix(upper, "INSERT ") {
		if !strings.Contains(upper, "WHERE NOT EXISTS") {
			return nil, errors.New("driver received non-reentrant DM singleton seed")
		}
	} else if !strings.HasPrefix(upper, "DECLARE ") || !strings.HasSuffix(upper, "END;") {
		return nil, errors.New("driver received unguarded or fragmented DM DDL block")
	}
	c.state.statements = append(c.state.statements, query)
	return driver.RowsAffected(0), nil
}
