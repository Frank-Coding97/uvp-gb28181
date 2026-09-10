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

const workRecordingDirectoryMigration = "2026-09-09-zz-work-recording-directory"

func TestWorkRecordingDirectoryMigrationArtifacts(t *testing.T) {
	for _, pair := range [][2]string{
		{"", "uvp-gb28181.sql"},
		{"-postgresql", "postgresql_converted.sql"},
		{"-sqlserver", "sqlserver_converted.sql"},
	} {
		suffix := pair[0]
		t.Run(suffix, func(t *testing.T) {
			up := readWorkRecordingDirectoryMigration(t, workRecordingDirectoryMigration+suffix+".sql")
			down := readWorkRecordingDirectoryMigration(t, workRecordingDirectoryMigration+suffix+"-down.sql")
			snapshot := readWorkRecordingDirectorySnapshot(t, pair[1])
			normalizedUp := strings.ToLower(up)
			normalizedDown := strings.ToLower(down)
			normalizedSnapshot := strings.ToLower(snapshot)

			for _, column := range []string{"recording_root", "recorder_claim_version"} {
				require.Contains(t, normalizedUp, column)
				require.Contains(t, normalizedSnapshot, column)
			}
			require.Contains(t, normalizedUp, "gb_work_recording")
			require.Contains(t, normalizedUp, "gb_recorder_claim")
			require.NotContains(t, normalizedUp, "create table", "directory migration must only alter the prerequisite tables")
			require.NotContains(t, strings.ToUpper(down), "DROP TABLE")
			require.Contains(t, normalizedDown, "rollback")

			switch suffix {
			case "":
				require.Contains(t, normalizedUp, "information_schema.tables")
				require.Contains(t, normalizedUp, "information_schema.columns")
				require.Contains(t, normalizedUp, "prerequisite_missing")
				require.Contains(t, normalizedDown, "signal sqlstate")
			case "-postgresql":
				require.Contains(t, normalizedUp, "to_regclass")
				require.Contains(t, normalizedUp, "raise exception")
				require.Contains(t, normalizedUp, "add column if not exists")
				require.Contains(t, normalizedDown, "raise exception")
			case "-sqlserver":
				require.Contains(t, normalizedUp, "object_id")
				require.Contains(t, normalizedUp, "col_length")
				require.Contains(t, normalizedUp, "throw")
				require.Contains(t, normalizedDown, "throw")
			}

			for _, column := range []string{"recording_root", "recorder_claim_version"} {
				require.Regexp(t, regexp.MustCompile(`(?i)`+column+`\s+(?:bigint|varchar|nvarchar)`), snapshot)
			}
		})
	}
}

func TestWorkRecordingDirectoryMigrationSortsAfterRecorderOwnership(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("../../../resource/database/gb28181/migrations"))
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	ups := FilterUpFiles(names, DialectMySQL)
	ownershipIndex := -1
	directoryIndex := -1
	for i, name := range ups {
		switch name {
		case "2026-09-09-z-recorder-ownership.sql":
			ownershipIndex = i
		case workRecordingDirectoryMigration + ".sql":
			directoryIndex = i
		}
	}
	require.GreaterOrEqual(t, ownershipIndex, 0)
	require.Greater(t, directoryIndex, ownershipIndex)
}

func TestWorkRecordingDirectoryMigrationComposesWithBaseSchema(t *testing.T) {
	planBody := readWorkRecordingDirectoryMigration(t, "2026-08-29-recording-plan-postgresql.sql")
	baseBody := readWorkRecordingDirectoryMigration(t, "2026-09-09-work-recording-postgresql.sql")
	ownershipBody := readWorkRecordingDirectoryMigration(t, "2026-09-09-z-recorder-ownership-postgresql.sql")
	directoryBody := readWorkRecordingDirectoryMigration(t, workRecordingDirectoryMigration+"-postgresql.sql")

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
		{body: baseBody, table: "gb_work_recording"},
		{body: baseBody, table: "gb_recorder_claim"},
	} {
		stmt, ok := createTableStatement(source.body, source.table)
		require.Truef(t, ok, "base migration must create %s", source.table)
		require.NoError(t, db.Exec(stmt).Error)
	}

	for _, stmt := range splitStatements(strings.ReplaceAll(ownershipBody, "ADD COLUMN IF NOT EXISTS", "ADD COLUMN")) {
		require.NoError(t, db.Exec(stmt).Error)
	}
	applyDirectoryPostgresDDLToSQLite(t, db, directoryBody)

	job := models.GbWorkRecording{
		ID:                   "job-directory-1",
		ChannelID:            12,
		CreatedBy:            7,
		RequestID:            "directory-request-1",
		State:                "starting",
		DesiredAction:        "start",
		Version:              1,
		RecorderClaimVersion: 9,
		RecordingRoot:        "/srv/uvp/work-recordings/job-directory-1",
		FormJSON:             `{}`,
	}
	require.NoError(t, db.Omit("BatchID").Create(&job).Error)
	claim := models.GbRecorderClaim{
		ResourceKey:   "channel-directory-12",
		OwnerKind:     "work_job",
		OwnerID:       job.ID,
		State:         "recording",
		Version:       9,
		RecordingRoot: job.RecordingRoot,
	}
	require.NoError(t, db.Create(&claim).Error)

	var restoredJob models.GbWorkRecording
	require.NoError(t, db.First(&restoredJob, "id = ?", job.ID).Error)
	require.Equal(t, job.RecorderClaimVersion, restoredJob.RecorderClaimVersion)
	require.Equal(t, job.RecordingRoot, restoredJob.RecordingRoot)
	var restoredClaim models.GbRecorderClaim
	require.NoError(t, db.First(&restoredClaim, "resource_key = ?", claim.ResourceKey).Error)
	require.Equal(t, claim.RecordingRoot, restoredClaim.RecordingRoot)
}

func TestWorkRecordingDirectoryMigrationFailsWithoutPrerequisiteTables(t *testing.T) {
	directoryBody := readWorkRecordingDirectoryMigration(t, workRecordingDirectoryMigration+"-postgresql.sql")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()

	var firstAlterErr error
	for _, stmt := range splitStatements(directoryBody) {
		if strings.HasPrefix(strings.TrimSpace(stmt), "DO $$") {
			continue
		}
		stmt = strings.ReplaceAll(stmt, "ADD COLUMN IF NOT EXISTS", "ADD COLUMN")
		firstAlterErr = db.Exec(stmt).Error
		if firstAlterErr != nil {
			break
		}
	}
	require.Error(t, firstAlterErr, "directory migration must fail when its prerequisite tables are absent")
}

func applyDirectoryPostgresDDLToSQLite(t *testing.T, db *gorm.DB, body string) {
	t.Helper()
	for _, stmt := range splitStatements(body) {
		if strings.HasPrefix(strings.TrimSpace(stmt), "DO $$") {
			continue
		}
		stmt = strings.ReplaceAll(stmt, "ADD COLUMN IF NOT EXISTS", "ADD COLUMN")
		require.NoError(t, db.Exec(stmt).Error)
	}
}

func readWorkRecordingDirectoryMigration(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", name))
	require.NoError(t, err)
	return string(body)
}

func readWorkRecordingDirectorySnapshot(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
	require.NoError(t, err)
	return string(body)
}
