package routes

import (
	"bytes"
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type runtimeTestSettings struct {
	enabled     bool
	playEnabled bool
	values      map[string]string
	integers    map[string]int
}

type runtimePermissions struct{}

func (runtimePermissions) Enforce(string, string, string, ...string) (bool, error) {
	return false, nil
}

func TestOpenAPIStartupRejectsMissingSchemaAndInvalidConfiguration(t *testing.T) {
	for _, failure := range []string{"none", "client", "scope", "nonce", "audit", "grant", "viewer", "grant-column", "viewer-column", "department", "department-name", "device", "channel", "column", "master-key", "key-id", "audience", "timeout", "write-timeout", "recovery"} {
		t.Run(failure, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "startup.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = raw.Close() })
			tables := map[string]any{"client": &models.Client{}, "scope": &models.ClientScope{}, "nonce": &models.Nonce{}, "audit": &models.Audit{}, "department": &basemodels.SysDepartment{}, "device": &gbmodels.GbDevice{}, "channel": &gbmodels.GbChannel{}}
			for name, model := range tables {
				if name != failure {
					require.NoError(t, db.AutoMigrate(model))
				}
			}
			require.NoError(t, db.AutoMigrate(&models.PlayGrant{}, &models.Viewer{}))
			settings := runtimeTestSettings{enabled: true, values: map[string]string{"openapi.audience": "test-audience", "openapi.master_key_id": "test"}, integers: map[string]int{"httpserver.write_timeout": 30}}
			t.Setenv("UVP_OPENAPI_MASTER_KEY", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
			switch failure {
			case "grant":
				require.NoError(t, db.Migrator().DropTable(&models.PlayGrant{}))
			case "viewer":
				require.NoError(t, db.Migrator().DropTable(&models.Viewer{}))
			case "grant-column":
				require.NoError(t, db.Exec("ALTER TABLE gb_openapi_play_grant DROP COLUMN updated_at").Error)
			case "viewer-column":
				require.NoError(t, db.Exec("ALTER TABLE gb_openapi_viewer DROP COLUMN last_error_class").Error)
			case "department-name":
				require.NoError(t, db.Exec("ALTER TABLE sys_department DROP COLUMN name").Error)
			case "column":
				require.NoError(t, db.Exec("ALTER TABLE sys_openapi_client DROP COLUMN auth_epoch").Error)
			case "master-key":
				t.Setenv("UVP_OPENAPI_MASTER_KEY", "invalid-master-key")
			case "key-id":
				settings.values["openapi.master_key_id"] = ""
			case "audience":
				settings.values["openapi.audience"] = ""
			case "timeout":
				settings.integers["openapi.request_timeout_seconds"] = 1
			case "write-timeout":
				settings.integers["httpserver.write_timeout"] = 6
			case "recovery":
				require.NoError(t, db.Create(&models.Audit{RequestID: "interrupted", Result: "started"}).Error)
				require.NoError(t, db.Exec("CREATE TRIGGER reject_recovery BEFORE UPDATE ON sys_openapi_audit BEGIN SELECT RAISE(ABORT, 'recovery denied'); END").Error)
			}
			gate, admin, err := InitializeRuntime(context.Background(), db, runtimePermissions{}, settings, nil)
			if failure == "none" {
				require.NoError(t, err)
				require.NotNil(t, gate)
				require.NotNil(t, admin)
			} else {
				require.Error(t, err)
				require.Nil(t, gate)
				require.Nil(t, admin)
				require.NotContains(t, err.Error(), "invalid-master-key")
			}
		})
	}
}

func (s runtimeTestSettings) GetBool(key string) bool {
	if key == "openapi.play_enabled" {
		return s.playEnabled
	}
	return key == "openapi.enabled" && s.enabled
}
func (s runtimeTestSettings) GetString(key string) string    { return s.values[key] }
func (s runtimeTestSettings) GetInt(key string) int          { return s.integers[key] }
func (s runtimeTestSettings) GetStringSlice(string) []string { return nil }

func TestOpenAPIStartupDisabledDoesNotRequireOrReadSecrets(t *testing.T) {
	t.Setenv("UVP_OPENAPI_MASTER_KEY", "not-a-real-key")
	gate, admin, err := InitializeRuntime(context.Background(), nil, nil, runtimeTestSettings{}, nil)
	require.NoError(t, err)
	require.Nil(t, gate)
	require.Nil(t, admin)
	_, _, err = InitializeRuntime(context.Background(), nil, nil, runtimeTestSettings{enabled: true}, nil)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "not-a-real-key")
}
