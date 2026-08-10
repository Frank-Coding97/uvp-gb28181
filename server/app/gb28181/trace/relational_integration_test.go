package trace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestRelationalStoreDatabaseMatrix(t *testing.T) {
	cases := []struct {
		name, env, migration string
		open                 func(string) gorm.Dialector
	}{
		{"mysql", "UVP_TRACE_TEST_MYSQL_DSN", "2026-08-10-sip-trace-message.sql", func(dsn string) gorm.Dialector { return mysql.Open(dsn) }},
		{"postgresql", "UVP_TRACE_TEST_POSTGRESQL_DSN", "2026-08-10-sip-trace-message-postgresql.sql", func(dsn string) gorm.Dialector { return postgres.Open(dsn) }},
		{"sqlserver", "UVP_TRACE_TEST_SQLSERVER_DSN", "2026-08-10-sip-trace-message-sqlserver.sql", func(dsn string) gorm.Dialector { return sqlserver.Open(dsn) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dsn := os.Getenv(tc.env)
			if dsn == "" {
				t.Skip(tc.env + " is not configured")
			}
			db, err := gorm.Open(tc.open(dsn), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })
			require.NoError(t, sqlDB.PingContext(t.Context()))

			migration, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations", tc.migration))
			require.NoError(t, err)
			require.NoError(t, db.Exec(string(migration)).Error)
			require.True(t, db.Migrator().HasTable(&gbmodels.GbSipTraceMessage{}))

			store, err := NewRelationalStore(db)
			require.NoError(t, err)
			now := time.Now().UTC().Truncate(time.Microsecond)
			old := testRelationalEvent(uuid.Must(uuid.NewV7()).String(), now.Add(-10*24*time.Hour), "matrix-device", "matrix-old", "MESSAGE", 0)
			fresh := testRelationalEvent(uuid.Must(uuid.NewV7()).String(), now, "matrix-device", "matrix-fresh", "MESSAGE", 0)
			fresh.FromURI = "unicode-\u6d4b\u8bd5@example"
			require.NoError(t, store.InsertBatch(t.Context(), []StoredEvent{old, fresh}))
			t.Cleanup(func() {
				_ = db.Where("event_id IN ?", []string{old.EventID, fresh.EventID}).Delete(&gbmodels.GbSipTraceMessage{}).Error
			})

			page, err := store.ListMessages(t.Context(), MessageFilter{From: now.Add(-time.Hour), To: now.Add(time.Hour), DeviceID: "matrix-device", Limit: 10})
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			stored, err := store.GetMessage(t.Context(), fresh.EventID)
			require.NoError(t, err)
			require.Equal(t, fresh.Payload.Ciphertext, stored.Payload.Ciphertext)

			deleted, err := store.Prune(t.Context(), now.Add(-7*24*time.Hour), 1)
			require.NoError(t, err)
			require.EqualValues(t, 1, deleted)
		})
	}
}
