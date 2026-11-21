package migration

import (
	"os"
	"strings"
	"testing"
	"time"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCascadeAlarmDeliveryMigrationsCoverEveryDialect(t *testing.T) {
	files := []struct {
		dialect Dialect
		up      string
		down    string
	}{
		{DialectMySQL, "2026-10-23-cascade-alarm-delivery.sql", "2026-10-23-cascade-alarm-delivery-down.sql"},
		{DialectPostgres, "2026-10-23-cascade-alarm-delivery-postgresql.sql", "2026-10-23-cascade-alarm-delivery-postgresql-down.sql"},
		{DialectSQLServer, "2026-10-23-cascade-alarm-delivery-sqlserver.sql", "2026-10-23-cascade-alarm-delivery-sqlserver-down.sql"},
		{DialectSQLite, "2026-10-23-cascade-alarm-delivery-sqlite.sql", "2026-10-23-cascade-alarm-delivery-sqlite-down.sql"},
	}
	for _, file := range files {
		t.Run(string(file.dialect), func(t *testing.T) {
			up, err := (&embedSource{dialect: file.dialect}).ReadSQL(file.up)
			require.NoError(t, err)
			if file.dialect == DialectSQLServer {
				require.Len(t, splitStatements(up), 3, "conditional create and indexes must each be executable statements")
			}
			for _, field := range []string{"delivery_key", "platform_id", "source_device_code", "source_resource_code", "published_code", "profile_version", "binding_fingerprint", "notify_targets", "payload", "attempts", "next_attempt_at", "lease_owner", "lease_until", "sent_at"} {
				require.Contains(t, up, field)
			}
			_, err = (&embedSource{dialect: file.dialect}).ReadSQL(file.down)
			require.NoError(t, err)
			require.Equal(t, []string{"gb_cascade_alarm_delivery"}, baselineTableMigrations[file.up].tables)
		})
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	up, err := (&embedSource{dialect: DialectSQLite}).ReadSQL("2026-10-23-cascade-alarm-delivery-sqlite.sql")
	require.NoError(t, err)
	executor := &dbExecutor{db: db}
	require.NoError(t, executor.ExecSQL(up))
	require.NoError(t, executor.ExecSQL(up), "outbox migration must be idempotent")
	require.True(t, db.Migrator().HasTable("gb_cascade_alarm_delivery"))
	require.True(t, db.Migrator().HasIndex("gb_cascade_alarm_delivery", "idx_cascade_alarm_delivery_due"))
}

func TestCascadeAlarmDeliverySQLiteUpgradeAndBaselineSupportOutbox(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "upgrade", true: "baseline"}[fresh], func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			raw.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = raw.Close() })
			if fresh {
				body, err := os.ReadFile("../../../resource/database/sqlitebaseline/baseline.sql")
				require.NoError(t, err)
				start := strings.Index(string(body), `CREATE TABLE IF NOT EXISTS "gb_cascade_alarm_delivery" (`)
				require.NotEqual(t, -1, start)
				section := string(body)[start:]
				end := strings.Index(section, "\n);")
				require.NotEqual(t, -1, end)
				require.NoError(t, db.Exec(section[:end+3]).Error)
				for _, statement := range splitStatements(string(body)) {
					if strings.Contains(statement, `ON "gb_cascade_alarm_delivery"`) {
						require.NoError(t, db.Exec(statement).Error)
					}
				}
			} else {
				up, err := (&embedSource{dialect: DialectSQLite}).ReadSQL("2026-10-23-cascade-alarm-delivery-sqlite.sql")
				require.NoError(t, err)
				require.NoError(t, (&dbExecutor{db: db}).ExecSQL(up))
			}
			repo := repository.NewGormRepository(db)
			now := time.Now().UTC().Truncate(time.Millisecond)
			item := &model.GbCascadeAlarmDelivery{DeliveryKey: "key", PlatformID: 1, SourceDeviceCode: "34020000001320000001", SourceResourceCode: "34020000001320000002", PublishedCode: "34020000001320000003", ProfileVersion: "2022", BindingFingerprint: strings.Repeat("a", 64), Payload: []byte("sanitized alarm"), NotifyTargets: `[{"id":"dialog"}]`, NextAttemptAt: now}
			created, err := repo.EnqueueAlarmDelivery(t.Context(), item)
			require.NoError(t, err)
			require.True(t, created)
			item.ID = 0
			created, err = repo.EnqueueAlarmDelivery(t.Context(), item)
			require.NoError(t, err)
			require.False(t, created)
			rows, err := repo.ClaimAlarmDeliveries(t.Context(), "token", now, time.Minute, 1)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, item.BindingFingerprint, rows[0].BindingFingerprint)
			ok, err := repo.CheckpointAlarmDelivery(t.Context(), rows[0].ID, "token", now, `[{"id":"dialog","state":"sent"}]`)
			require.NoError(t, err)
			require.True(t, ok)
			ok, err = repo.CompleteAlarmDelivery(t.Context(), rows[0].ID, "token", now)
			require.NoError(t, err)
			require.True(t, ok)
			var got model.GbCascadeAlarmDelivery
			require.NoError(t, db.First(&got, rows[0].ID).Error)
			require.Equal(t, model.CascadeAlarmDeliverySent, got.State)
			require.Contains(t, got.NotifyTargets, `"state":"sent"`)
		})
	}
}
