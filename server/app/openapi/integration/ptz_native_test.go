package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

func ptzNativeBaselineStatements(body string) []string {
	var selected []string
	body = strings.Split(body, "-- ptz-device-intent:begin")[0]
	pattern := regexp.MustCompile(`(?is)^CREATE\s+(?:TABLE\s+|(?:UNIQUE\s+)?INDEX\s+\S+\s+ON\s+)(gb_ptz_operation(?:_attempt)?)\s*\(`)
	profileUpgrade := regexp.MustCompile(`(?is)^(?:ALTER TABLE gb_ptz_operation\s+ADD COLUMN profile_version\b|IF COL_LENGTH\(N'gb_ptz_operation', N'(?:profile_version|profile_charset|target_scope|target_code|scope_key)'\))`)
	for _, statement := range nativeSchemaStatements(body) {
		plain := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.TrimSpace(statement))
		if pattern.MatchString(plain) || profileUpgrade.MatchString(plain) {
			selected = append(selected, statement)
		}
	}
	return selected
}

func TestPTZNativeBaselineSelection(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgresql", "sqlserver"} {
		name, _ := fullInitializationFileName(dialect)
		body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
		require.NoError(t, err)
		statements := ptzNativeBaselineStatements(string(body))
		var tables int
		for _, statement := range statements {
			if strings.HasPrefix(strings.TrimSpace(statement), "CREATE TABLE") {
				tables++
			}
			require.NotContains(t, strings.ToUpper(statement), "DROP TABLE")
		}
		require.Equal(t, 2, tables, dialect)
		require.Contains(t, strings.Join(statements, "\n"), "profile_version", dialect)
		require.Contains(t, strings.Join(statements, "\n"), "scope_key", dialect)
	}
}

// Called only after the enclosing native fixture verifies an explicitly named
// empty test database and registers its one concrete process authority.
func exercisePTZNative(t *testing.T, ctx context.Context, conn *sql.Conn, db *gorm.DB, store *playauth.DeviceOperationIntentStore, authority *processauthority.Authority, dialect, suffix string) {
	t.Helper()
	name, _ := fullInitializationFileName(dialect)
	dir := os.Getenv("UVP_OPENAPI_TEST_INIT_DIR")
	if dir == "" {
		dir = "../../../resource/database"
	}
	body, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	statements := ptzNativeBaselineStatements(string(body))
	require.NotEmpty(t, statements)
	for _, statement := range statements {
		_, err := conn.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	stem := "migrations/2026-09-08-ptz-device-intent" + suffix
	up, err := migrationsfs.FS.ReadFile(stem + ".sql")
	require.NoError(t, err)
	applyCleanupBarrierScript(t, ctx, conn, up)
	applyCleanupBarrierScript(t, ctx, conn, up)
	// Keep this family separate from the enclosing live/SIP/RTP discovery
	// fixture, whose exact resource-count assertions belong to devices 1/2.
	require.NoError(t, db.Exec("INSERT INTO gb_device (id,device_id,access_epoch,cleanup_completed_epoch) VALUES (90001,'34020000001320090001',1,1)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_channel (id,device_id,channel_id) VALUES (90001,'34020000001320090001','34020000001320090002')").Error)
	id := playauth.DeviceOperationIntentIdentity{OperationID: "10000000000000000000000000000001", DevicePK: 90001, DeviceCode: "34020000001320090001", DeviceEpoch: 1,
		TargetScope: "channel", TargetPK: 90001, TargetCode: "34020000001320090002", Kind: "ptz"}
	op, err := store.ReservePTZOperation(ctx, id, gbmodels.GbPTZOperation{
		OperationID: "native-ptz", IdempotencyKey: "native-ptz", DeviceID: uint(id.DevicePK), DeviceCode: id.DeviceCode, ChannelID: uint(id.TargetPK), ChannelCode: id.TargetCode,
		TargetScope: "alarm", TargetCode: "34020000001340000001", CmdType: "DeviceControl", Action: "guard_set", Status: gbmodels.PTZOperationQueued,
		MaxAttempts: 1, CreatedAt: time.Now(), SN: 1})
	require.NoError(t, err)
	claimedAt := time.Now()
	attempt, err := store.ClaimPTZAttempt(ctx, id, op.ID, 0, claimedAt)
	require.NoError(t, err)
	require.True(t, attempt.StartedAt.Equal(claimedAt.Truncate(time.Millisecond)), "database normalization must preserve the actual dispatch instant: got %s want %s", attempt.StartedAt, claimedAt)
	require.Equal(t, 15*time.Second, attempt.LeaseUntil.Sub(attempt.StartedAt), "one-way transport lease must not drift with database timezone")
	var stored gbmodels.GbPTZOperationAttempt
	require.NoError(t, db.First(&stored, attempt.ID).Error)
	require.True(t, stored.StartedAt.Equal(attempt.StartedAt))
	require.True(t, stored.LeaseUntil.Equal(attempt.LeaseUntil))
	require.NotNil(t, stored.OwnerRunID)
	down, err := migrationsfs.FS.ReadFile(stem + "-down.sql")
	require.NoError(t, err)
	applyCleanupBarrierScript(t, ctx, conn, down)
	applyCleanupBarrierScript(t, ctx, conn, up)
	var preserved gbmodels.GbPTZOperation
	require.NoError(t, db.First(&preserved, op.ID).Error)
	require.Equal(t, id.OperationID, *preserved.DeviceIntentID)
	require.EqualValues(t, 1, *preserved.DeviceEpoch)
	// The retirement columns arrive through their own migration: the full
	// initialization baseline stops before the device-intent section. Apply it
	// twice, converge a foreign-generation attempt through the durable scan,
	// then prove that down keeps the append-only certificate.
	retireStem := "migrations/2026-09-08-ptz-owner-retirement" + suffix
	retireUp, err := migrationsfs.FS.ReadFile(retireStem + ".sql")
	require.NoError(t, err)
	applyCleanupBarrierScript(t, ctx, conn, retireUp)
	applyCleanupBarrierScript(t, ctx, conn, retireUp)
	require.NoError(t, db.Exec("UPDATE gb_ptz_operation_attempt SET owner_process_id=?, owner_run_id=? WHERE id=?",
		"ffffffffffffffffffffffffffffffff", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", attempt.ID).Error)
	skipped, err := store.RecoverRetiredPTZ(ctx)
	require.NoError(t, err)
	require.Empty(t, skipped)
	var retired gbmodels.GbPTZOperationAttempt
	require.NoError(t, db.First(&retired, attempt.ID).Error)
	require.NotNil(t, retired.RetiredByProcessID)
	require.Equal(t, authority.GenerationID(), *retired.RetiredByProcessID)
	require.Equal(t, playauth.PTZOwnerProcessRetired, retired.ErrorCode)
	require.Equal(t, gbmodels.PTZOperationAttemptUnknown, retired.Status)
	retireDown, err := migrationsfs.FS.ReadFile(retireStem + "-down.sql")
	require.NoError(t, err)
	applyCleanupBarrierScript(t, ctx, conn, retireDown)
	var kept gbmodels.GbPTZOperationAttempt
	require.NoError(t, db.First(&kept, attempt.ID).Error)
	require.NotNil(t, kept.RetiredByProcessID, "down must keep the append-only retirement history")
	exercisePTZNativeCommitFaults(t, ctx, db, store, authority, id)
	exercisePTZNativeLegacyTimes(t, db, id)
	t.Log("PTZ native product schema/up twice/down-preserve, alarm channel authority and exact attempt time round-trip passed")
}

func exercisePTZNativeLegacyTimes(t *testing.T, db *gorm.DB, target playauth.DeviceOperationIntentIdentity) {
	t.Helper()
	now := time.Now().In(time.Local).Truncate(time.Millisecond)
	for _, offset := range []time.Duration{-time.Second, 0, time.Second} {
		deadline := now.Add(offset)
		name := fmt.Sprintf("native-legacy-%d", offset)
		// Seed the pre-upgrade representation: local wall time, no epoch,
		// intent, process or run identity. Never fabricate those identities.
		op := gbmodels.GbPTZOperation{OperationID: name, IdempotencyKey: name,
			DeviceID: uint(target.DevicePK), DeviceCode: target.DeviceCode, ChannelID: uint(target.TargetPK), ChannelCode: target.TargetCode,
			TargetScope: "channel", TargetCode: target.TargetCode, CmdType: "DeviceControl", Action: "query",
			Status: gbmodels.PTZOperationQueued, ResponseRequired: true, MaxAttempts: 3, Attempt: 1,
			CreatedAt: now.Add(-time.Minute), DispatchStartedAt: &now, NextAttemptAt: &deadline, TransportDeadlineAt: &deadline}
		require.NoError(t, db.Create(&op).Error)
		attempt := gbmodels.GbPTZOperationAttempt{OperationID: op.ID, AttemptNo: 1,
			Status: gbmodels.PTZOperationAttemptDispatching, StartedAt: now.Add(-time.Second), LeaseUntil: deadline, CreatedAt: now.Add(-time.Second)}
		require.NoError(t, db.Create(&attempt).Error)
		var recovered gbmodels.GbPTZOperation
		require.NoError(t, db.First(&recovered, op.ID).Error)
		require.True(t, recovered.NextAttemptAt.Equal(deadline))
		require.Equal(t, offset > 0, recovered.NextAttemptAt.After(now))
		require.Nil(t, recovered.DeviceEpoch)
		require.Nil(t, recovered.DeviceIntentID)
		var expired []gbmodels.GbPTZOperationAttempt
		require.NoError(t, db.Table("gb_ptz_operation_attempt AS attempt").Select("attempt.*").
			Joins("JOIN gb_ptz_operation AS operation ON operation.id=attempt.operation_id").
			Where("operation.id=? AND attempt.status=? AND attempt.lease_until<=?", op.ID, gbmodels.PTZOperationAttemptDispatching, gbmodels.PTZTimeComparison(db, now)).
			Find(&expired).Error)
		if offset <= 0 {
			require.Len(t, expired, 1)
			require.True(t, expired[0].LeaseUntil.Equal(deadline), "joined Find must apply model time boundary")
			require.False(t, expired[0].LeaseUntil.After(now))
			require.Nil(t, expired[0].OwnerProcessID)
			require.Nil(t, expired[0].OwnerRunID)
		} else {
			require.Empty(t, expired)
		}
		for _, delta := range []time.Duration{-100 * time.Microsecond, 0, 100 * time.Microsecond} {
			var count int64
			require.NoError(t, db.Model(&gbmodels.GbPTZOperationAttempt{}).
				Where("id=? AND lease_until<=?", attempt.ID, gbmodels.PTZTimeComparison(db, deadline.Add(delta))).Count(&count).Error)
			if delta < 0 {
				require.Zero(t, count, "comparison must not round up to the stored millisecond")
			} else {
				require.EqualValues(t, 1, count)
			}
		}
	}
	t.Log("PTZ native legacy local-time rows and joined expired-lease classification passed; no historical authority backfill or dispatch exercised")
}

func exercisePTZNativeCommitFaults(t *testing.T, ctx context.Context, db *gorm.DB, store *playauth.DeviceOperationIntentStore, authority *processauthority.Authority, target playauth.DeviceOperationIntentIdentity) {
	t.Helper()
	for index, scenario := range []string{"rollback", "committed", "started-1ms", "lease-1ms"} {
		id := target
		id.OperationID = fmt.Sprintf("%032x", 91000+index)
		op, err := store.ReservePTZOperation(ctx, id, gbmodels.GbPTZOperation{
			OperationID: "native-fault-" + scenario, IdempotencyKey: "native-fault-" + scenario,
			DeviceID: uint(id.DevicePK), DeviceCode: id.DeviceCode, ChannelID: uint(id.TargetPK), ChannelCode: id.TargetCode,
			TargetScope: "channel", TargetCode: id.TargetCode, CmdType: "DeviceControl", Action: "guard_set",
			Status: gbmodels.PTZOperationQueued, MaxAttempts: 1, CreatedAt: time.Now(), SN: 2 + index})
		require.NoError(t, err)
		faultDB := authoritytest.CommitFaultDB(t, db, 1, scenario != "rollback")
		faultStore, err := playauth.NewAuthorizedDeviceOperationIntentStore(faultDB, authority)
		require.NoError(t, err)
		var before int64
		require.NoError(t, db.Table("sys_openapi_process_authority").Pluck("row_version", &before).Error)
		attempt, err := faultStore.ClaimPTZAttempt(ctx, id, op.ID, 0, time.Now())
		require.Zero(t, attempt.ID, "unknown commit must issue no dispatch permission")
		var ticket *playauth.PTZUnissuedAttempt
		require.ErrorAs(t, err, &ticket)
		var after int64
		require.NoError(t, db.Table("sys_openapi_process_authority").Pluck("row_version", &after).Error)
		var rows []gbmodels.GbPTZOperationAttempt
		require.NoError(t, db.Where("operation_id=?", op.ID).Find(&rows).Error)
		if scenario == "rollback" {
			require.Empty(t, rows)
			require.Equal(t, before, after, "authority fence must roll back with business claim")
		} else {
			require.Len(t, rows, 1)
			require.Equal(t, before+1, after)
			if scenario == "started-1ms" || scenario == "lease-1ms" {
				column, original := "started_at", rows[0].StartedAt
				if scenario == "lease-1ms" {
					column, original = "lease_until", rows[0].LeaseUntil
				}
				require.NoError(t, db.Model(&rows[0]).UpdateColumn(column, original.Add(time.Millisecond)).Error)
				require.ErrorIs(t, ticket.Reconcile(ctx), playauth.ErrDeviceIntentConflict, "one millisecond mutation must not pass exact snapshot check")
				require.NoError(t, db.Model(&rows[0]).UpdateColumn(column, original).Error)
			}
		}
		require.NoError(t, ticket.Reconcile(ctx), scenario)
		require.NoError(t, ticket.Reconcile(ctx), "repeated reconciliation is fact-only")
		var final gbmodels.GbPTZOperation
		require.NoError(t, db.First(&final, op.ID).Error)
		if scenario == "rollback" {
			require.Zero(t, final.Attempt)
		} else {
			require.Equal(t, gbmodels.PTZOperationUnknown, final.Status)
			require.NoError(t, db.First(&rows[0], rows[0].ID).Error)
			require.Equal(t, gbmodels.PTZOperationAttemptFailed, rows[0].Status)
			require.NotNil(t, rows[0].LocalQuiescedAt)
			require.Equal(t, "DISPATCH_NOT_INVOKED", rows[0].ErrorCode)
		}
	}
	t.Log("PTZ native real driver commit/rollback reply loss: no dispatch permission; fence atomicity, exact 1ms mutations rejected, fact-only reconciliation passed")
}
