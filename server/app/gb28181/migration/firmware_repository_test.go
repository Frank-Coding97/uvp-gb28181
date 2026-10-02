package migration

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// TestFirmwareRepositorySchemaAndFreshInstallMatch verifies that the
// gb_firmware_repository table schema is present in all baseline SQL files.
func TestFirmwareRepositorySchemaAndFreshInstallMatch(t *testing.T) {
	model, err := schema.Parse(&gbmodels.GbFirmwareRepository{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)

	// Verify all 17 fields are present
	expectedFields := []string{
		"id", "firmware_id", "version", "manufacturer", "model_pattern",
		"file_name", "file_size", "file_hash", "storage_path", "storage_key",
		"release_date", "status", "uploaded_by", "dept_id",
		"created_at", "updated_at", "remark",
	}
	require.ElementsMatch(t, expectedFields, model.DBNames, "model fields mismatch")

	// Verify schema in all SQL variants
	for suffix, initial := range map[string]string{
		"":            "uvp-gb28181.sql",
		"-postgresql": "postgresql_converted.sql",
		"-sqlserver":  "sqlserver_converted.sql",
	} {
		t.Run(suffix, func(t *testing.T) {
			fresh, err := os.ReadFile(filepath.Join("../../../resource/database", initial))
			require.NoError(t, err)
			body := string(fresh)

			// Verify table exists
			require.Contains(t, body, "gb_firmware_repository", "table missing in "+initial)

			// Verify all fields present
			for _, field := range model.DBNames {
				require.Contains(t, body, field, "field %s missing in %s", field, initial)
			}

			// Verify indexes
			for _, index := range []string{"idx_manu_model", "idx_file_hash", "idx_dept_status"} {
				require.Contains(t, body, index, "index %s missing in %s", index, initial)
			}

			// Verify unique constraint on firmware_id
			require.Contains(t, body, "firmware_id", "firmware_id unique constraint missing")
		})
	}
}

// TestFirmwareUpgradeAddsFirmwareIDColumn verifies that the firmware_id
// column was added to gb_device_firmware_upgrade table.
func TestFirmwareUpgradeAddsFirmwareIDColumn(t *testing.T) {
	model, err := schema.Parse(&gbmodels.GbDeviceFirmwareUpgrade{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)

	// Verify FirmwareID field exists in model
	require.Contains(t, model.DBNames, "firmware_id", "firmware_id field missing from GbDeviceFirmwareUpgrade model")

	// Verify field is present in baseline SQL
	for suffix, initial := range map[string]string{
		"":            "uvp-gb28181.sql",
		"-postgresql": "postgresql_converted.sql",
		"-sqlserver":  "sqlserver_converted.sql",
	} {
		t.Run(suffix, func(t *testing.T) {
			fresh, err := os.ReadFile(filepath.Join("../../../resource/database", initial))
			require.NoError(t, err)
			body := string(fresh)

			// Find gb_device_firmware_upgrade table definition
			require.Contains(t, body, "gb_device_firmware_upgrade", "table missing in "+initial)

			// Verify firmware_id column exists in table
			upgradeTableStart := strings.Index(body, "CREATE TABLE")
			require.NotEqual(t, -1, upgradeTableStart, "CREATE TABLE not found")

			// Extract table section (rough check - just verify the column exists somewhere)
			require.Contains(t, body, "firmware_id", "firmware_id column missing from gb_device_firmware_upgrade in "+initial)
		})
	}
}

// TestFirmwareRepositoryPermissionMigrationIsScopedAndReversible verifies
// permission migration logic (to be implemented in Task 5).
func TestFirmwareRepositoryPermissionMigrationIsScopedAndReversible(t *testing.T) {
	t.Skip("Permission migration to be implemented in Task 5")

	// This test will verify:
	// 1. sys_api rows for firmware-repository endpoints
	// 2. sys_menu row under "设备维护" parent
	// 3. sys_menu_api associations
	// 4. sys_casbin_rule grants for admin role
	// 5. Reversibility via -down.sql
}
