package standalone

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestRecoveryConfigRotatesTogetherAndPreservesOtherCredentials(t *testing.T) {
	paths := configTestPaths(t)
	before, err := InitializeConfig(paths)
	require.NoError(t, err)
	require.NotEmpty(t, before.InstanceGeneration())
	lock, err := AcquireInstanceLock(paths.InstallDir)
	require.NoError(t, err)
	defer lock.Close()
	j := maintenanceTestJournal(paths.InstallDir)
	require.NoError(t, createMaintenanceJournal(paths.InstallDir, j))
	require.Error(t, rotateRecoveryCredentials(paths, j.OperationID, before.ConfigSHA256, nil))
	require.NoError(t, advanceMaintenance(paths.InstallDir, j.OperationID, MaintenanceUpgrading, MaintenanceRestoreRequired))
	require.NoError(t, advanceMaintenance(paths.InstallDir, j.OperationID, MaintenanceRestoreRequired, MaintenanceRestoring))
	old, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)
	failure := errors.New("injected config publish failure")
	require.ErrorContains(t, rotateRecoveryCredentials(paths, j.OperationID, before.ConfigSHA256, func(stage string) error {
		if stage == "publish" {
			return failure
		}
		return nil
	}), "publish hook failed")
	unchanged, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)
	require.Equal(t, old, unchanged)
	require.Error(t, rotateRecoveryCredentials(paths, "wrong-operation", before.ConfigSHA256, nil))
	require.NoError(t, rotateRecoveryCredentials(paths, j.OperationID, before.ConfigSHA256, nil))
	after, err := LoadConfig(paths)
	require.NoError(t, err)
	require.NotEqual(t, before.JWTSecret(), after.JWTSecret())
	require.NotEqual(t, before.InstanceGeneration(), after.InstanceGeneration())
	before.values["token"].(map[string]any)["jwttokensignkey"] = after.JWTSecret()
	before.values["token"].(map[string]any)["instancegeneration"] = after.InstanceGeneration()
	require.Equal(t, before.values, after.values)
	require.Error(t, rotateRecoveryCredentials(paths, j.OperationID, before.ConfigSHA256, nil))
	require.ErrorIs(t, CheckMaintenanceGate(paths.InstallDir), ErrMaintenanceRequired)
}

func TestInstanceGenerationLegacyCompatibilityAndValidation(t *testing.T) {
	values, err := newInstanceConfigValues()
	require.NoError(t, err)
	token := values["token"].(map[string]any)
	delete(token, "instancegeneration")
	raw, err := yaml.Marshal(values)
	require.NoError(t, err)
	_, err = decodeInstanceConfig(raw)
	require.NoError(t, err)
	for _, invalid := range []any{"", "short", 123, token["jwttokensignkey"]} {
		token["instancegeneration"] = invalid
		raw, err = yaml.Marshal(values)
		require.NoError(t, err)
		_, err = decodeInstanceConfig(raw)
		require.Error(t, err)
	}
}
