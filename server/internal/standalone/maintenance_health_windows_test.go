//go:build windows

package standalone

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestWindowsMaintenanceBackendBootstrapMigrateHealth(t *testing.T) {
	fixture := newMaintenanceBackendTestFixture(t, maintenanceBackendTestPath(t))
	server := newRecoveryRedisServer(t)
	client := server.client(0)
	defer client.Close()
	cfg, err := LoadConfig(fixture.paths)
	require.NoError(t, err)
	// This Redis belongs only to this test. Its password remains in protected
	// config and the Redis protocol, never a child argument or environment value.
	require.NoError(t, client.ConfigSet(context.Background(), "requirepass", cfg.RedisPassword()).Err())
	values := cfg.values
	values["redis"].(map[string]any)["port"] = server.port
	raw, err := yaml.Marshal(values)
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(fixture.paths.ConfigFile, raw, true, nil))

	for _, action := range []struct {
		purpose string
		args    []string
	}{
		{"bootstrap_db", []string{"-bootstrap-db"}},
		{"migrate_up", []string{"-migrate-up"}},
		{"candidate_health", nil},
	} {
		t.Run(action.purpose, func(t *testing.T) {
			frame, err := IssueMaintenancePermit(fixture.paths.InstallDir, fixture.journal.OperationID, action.purpose, fixture.candidate.manifest.Version)
			require.NoError(t, err)
			defer clear(frame)
			result := runMaintenanceBackend(t, fixture.candidateBackendPath(), fixture.paths, action.args, frame)
			assertMaintenanceBackendNoPermitLeak(t, result, frame)
			require.Equal(t, 0, result.exitCode, "offline maintenance failed at %s", action.purpose)
			decoder := json.NewDecoder(strings.NewReader(result.stdout))
			var last map[string]any
			for {
				var item map[string]any
				err := decoder.Decode(&item)
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				last = item
			}
			require.Equal(t, "maintenance_complete", last["status"])
			require.Equal(t, action.purpose, last["purpose"])
			require.Equal(t, fixture.candidate.manifest.Version, last["version"])
			assertMaintenancePermitMissing(t, fixture.paths.InstallDir)
			require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
		})
		if t.Failed() {
			return
		}
	}
}
