package migration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const recorderOwnershipMigration = "2026-09-09-z-recorder-ownership"

func TestRecorderOwnershipMigrationArtifacts(t *testing.T) {
	for _, pair := range [][2]string{
		{"", "uvp-gb28181.sql"},
		{"-postgresql", "postgresql_converted.sql"},
		{"-sqlserver", "sqlserver_converted.sql"},
	} {
		suffix := pair[0]
		t.Run(suffix, func(t *testing.T) {
			up := readRecorderMigration(t, recorderOwnershipMigration+suffix+".sql")
			down := readRecorderMigration(t, recorderOwnershipMigration+suffix+"-down.sql")
			snapshot := readRecorderSnapshot(t, pair[1])

			for _, column := range []string{
				"recorder_owner_kind",
				"recorder_owner_id",
				"recorder_claim_version",
				"channel_id",
				"node_id",
				"v_host",
				"app",
				"stream",
			} {
				require.Contains(t, strings.ToLower(up), column)
				require.Contains(t, strings.ToLower(snapshot), column)
			}
			require.NotContains(t, strings.ToUpper(down), "DROP TABLE")
			require.Contains(t, strings.ToLower(down), "rollback")
		})
	}
}

func TestRecorderOwnershipMigrationComposesWithBaseSchema(t *testing.T) {
	planBody := readRecorderMigration(t, "2026-08-29-recording-plan-postgresql.sql")
	claimBody := readRecorderMigration(t, "2026-09-09-work-recording-postgresql.sql")
	ownershipBody := readRecorderMigration(t, recorderOwnershipMigration+"-postgresql.sql")

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()

	for _, source := range []struct {
		body  string
		table string
	}{
		{body: planBody, table: "gb_recording_plan_channel_state"},
		{body: claimBody, table: "gb_recorder_claim"},
	} {
		stmt, ok := createTableStatement(source.body, source.table)
		require.Truef(t, ok, "base migration must create %s", source.table)
		// SQLite's driver scans DATETIME into time.Time more reliably than
		// PostgreSQL's TIMESTAMP declaration; the columns remain the same.
		stmt = strings.ReplaceAll(stmt, "TIMESTAMP(3)", "DATETIME")
		stmt = strings.ReplaceAll(stmt, "TIMESTAMP", "DATETIME")
		require.NoError(t, db.Exec(stmt).Error)
	}

	// SQLite is only the schema contract runner. Strip PostgreSQL's idempotency
	// guards while keeping the production column definitions and ALTER TABLE DDL.
	sqliteOwnership := strings.ReplaceAll(ownershipBody, "ALTER TABLE IF EXISTS ", "ALTER TABLE ")
	sqliteOwnership = strings.ReplaceAll(sqliteOwnership, "ADD COLUMN IF NOT EXISTS", "ADD COLUMN")
	for _, stmt := range splitStatements(sqliteOwnership) {
		require.NoError(t, db.Exec(stmt).Error)
	}

	claim := models.GbRecorderClaim{
		ResourceKey: "media-key",
		OwnerKind:   "work_job",
		OwnerID:     "job-1",
		State:       "recording",
		Version:     7,
		Generation:  3,
		ChannelID:   12,
		NodeID:      34,
		VHost:       "__defaultVhost__",
		App:         "rtp",
		Stream:      "stream-12",
	}
	// This test targets the ownership migration; keep the later directory
	// column out of the historical insert and select contract.
	require.NoError(t, db.Omit("RecordingRoot").Create(&claim).Error)
	var restoredClaim models.GbRecorderClaim
	require.NoError(t, db.Select("channel_id", "node_id", "v_host", "app", "stream").First(&restoredClaim, "resource_key = ?", claim.ResourceKey).Error)
	require.Equal(t, claim.ChannelID, restoredClaim.ChannelID)
	require.Equal(t, claim.NodeID, restoredClaim.NodeID)
	require.Equal(t, claim.VHost, restoredClaim.VHost)
	require.Equal(t, claim.App, restoredClaim.App)
	require.Equal(t, claim.Stream, restoredClaim.Stream)

	state := models.GbRecordingPlanChannelState{
		ChannelID:            12,
		DesiredState:         models.RecordingDesiredRecording,
		ActualState:          models.RecordingStateRecording,
		ReconcileAt:          restoredClaim.CreatedAt,
		RecorderOwnerKind:    "plan",
		RecorderOwnerID:      "plan-run-1",
		RecorderClaimVersion: 7,
	}
	require.NoError(t, db.Create(&state).Error)
	var restoredState models.GbRecordingPlanChannelState
	require.NoError(t, db.First(&restoredState, "channel_id = ?", state.ChannelID).Error)
	require.Equal(t, state.RecorderOwnerKind, restoredState.RecorderOwnerKind)
	require.Equal(t, state.RecorderOwnerID, restoredState.RecorderOwnerID)
	require.Equal(t, state.RecorderClaimVersion, restoredState.RecorderClaimVersion)
}

func readRecorderMigration(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", name))
	require.NoError(t, err)
	return string(body)
}

func readRecorderSnapshot(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
	require.NoError(t, err)
	return string(body)
}

func createTableStatement(body, table string) (string, bool) {
	pattern := regexp.MustCompile(`(?is)CREATE TABLE IF NOT EXISTS\s+` + regexp.QuoteMeta(table) + `\s*\(.*?\);`)
	match := pattern.FindString(body)
	if match == "" {
		return "", false
	}
	return strings.TrimSpace(match), true
}

func TestRecorderOwnershipMigrationSortsAfterWorkRecording(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("../../../resource/database/gb28181/migrations"))
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	ups := FilterUpFiles(names, DialectMySQL)
	workIndex := -1
	ownershipIndex := -1
	for i, name := range ups {
		switch name {
		case "2026-09-09-work-recording.sql":
			workIndex = i
		case recorderOwnershipMigration + ".sql":
			ownershipIndex = i
		}
	}
	require.GreaterOrEqual(t, workIndex, 0)
	require.Greater(t, ownershipIndex, workIndex)
}
