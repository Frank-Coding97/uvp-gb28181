package migration

import (
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestWorkRecordingMigrationAndSnapshotSchemas(t *testing.T) {
	for _, pair := range [][2]string{{"", "uvp-gb28181.sql"}, {"-postgresql", "postgresql_converted.sql"}, {"-sqlserver", "sqlserver_converted.sql"}} {
		t.Run(pair[0], func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", "2026-09-09-work-recording"+pair[0]+".sql"))
			require.NoError(t, err)
			snapshot, err := os.ReadFile(filepath.Join("../../../resource/database", pair[1]))
			require.NoError(t, err)
			for _, table := range []string{"gb_work_recording", "gb_recorder_claim", "gb_work_recording_file"} {
				require.Contains(t, string(body), table)
				require.Contains(t, string(snapshot), table)
			}
			if pair[0] == "-sqlserver" {
				return
			} // SQLite execution is not a SQL Server validation.
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			defer raw.Close()
			for i := 0; i < 2; i++ {
				// SQLite only recognizes unparameterized DATETIME for time scanning.
				// This exercises schema contracts, not MySQL server syntax.
				for _, stmt := range splitStatements(strings.ReplaceAll(string(body), "DATETIME(6)", "DATETIME")) {
					require.NoError(t, db.Exec(stmt).Error)
				}
			}
			// This test targets the historical base migration. Keep later directory
			// columns out of both the insert and select contract.
			job := models.GbWorkRecording{ID: "job-1", ChannelID: 1, CreatedBy: 1, RequestID: "req-1", State: "unknown", DesiredAction: "start", Version: 1, DeviceID: "00000000000000000001", FormJSON: `{"projectName":"测试"}`, VHost: "__defaultVhost__"}
			require.NoError(t, db.Omit("RecordingRoot", "RecorderClaimVersion", "BatchID").Create(&job).Error)
			var restored models.GbWorkRecording
			require.NoError(t, db.Select("id", "form_json", "v_host").First(&restored, "id = ?", job.ID).Error)
			require.Equal(t, job.FormJSON, restored.FormJSON)
			require.Equal(t, job.VHost, restored.VHost)
			require.NoError(t, db.Exec("INSERT INTO gb_recorder_claim(resource_key,owner_kind,owner_id,state,version) VALUES ('resource','work_job','job','unknown',1)").Error)
			require.Error(t, db.Exec("INSERT INTO gb_recorder_claim(resource_key,owner_kind,owner_id,state,version) VALUES ('resource','work_job','other','starting',1)").Error)
			var state string
			require.NoError(t, db.Raw("SELECT state FROM gb_recorder_claim").Scan(&state).Error)
			require.Equal(t, "unknown", state)
			require.False(t, strings.Contains(strings.ToUpper(string(body)), "DROP TABLE"))
		})
	}
}
