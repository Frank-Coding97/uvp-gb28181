package bootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	catalogstore "uvplatform.com/uvp-gb28181/app/openapi/catalog/store"
	openapimodels "uvplatform.com/uvp-gb28181/app/openapi/models"
)

func TestStartupMigratesActionRoutesAndPreservesOldRelease(t *testing.T) {
	db := publishFixture(t)
	runtime := newTestRuntime(t, db)
	ctx := context.Background()

	_, err := EnsureCoreCatalog(ctx, db, 0)
	require.NoError(t, err)
	var operation openapimodels.Operation
	require.NoError(t, db.Where("code = ?", "device.detail").First(&operation).Error)
	oldPath := "/openapi/v1/devices/{deviceId}"
	require.NoError(t, db.Model(&operation).Update("external_path", oldPath).Error)
	var deletion openapimodels.Operation
	require.NoError(t, db.Where("code = ?", "ptz.preset.delete").First(&deletion).Error)
	oldDeletePath := "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}"
	require.NoError(t, db.Model(&deletion).Updates(map[string]any{"method": "DELETE", "external_path": oldDeletePath, "request_schema": "{}"}).Error)
	repository := catalogstore.NewRepository(db)
	draft, err := repository.BuildDraft(ctx)
	require.NoError(t, err)
	initial, err := publishDraft(ctx, repository, runtime, draft, InitialReleaseName, 0)
	require.NoError(t, err)
	first := PublishOutcome{ReleaseID: initial.Release.ID, Version: initial.Release.Version}
	second, err := EnsurePublishedCatalog(ctx, db, 0, runtime)
	require.NoError(t, err)
	require.Equal(t, PublishActionRevised, second.Action)
	require.Greater(t, second.Version, first.Version)
	var oldItem openapimodels.ReleaseItem
	require.NoError(t, db.Where("release_id = ? AND scope = ?", first.ReleaseID, "device:detail").First(&oldItem).Error)
	require.Equal(t, oldPath, oldItem.ExternalPath)
	require.NoError(t, db.First(&operation, operation.ID).Error)
	require.Equal(t, "/openapi/v1/device/info", operation.ExternalPath)
	require.NoError(t, db.First(&deletion, deletion.ID).Error)
	require.Equal(t, "POST", deletion.Method)
	require.Equal(t, "/openapi/v1/ptz/preset/delete", deletion.ExternalPath)
	require.Contains(t, deletion.RequestSchema, `"deviceId"`)
	require.Contains(t, deletion.RequestSchema, `"presetId"`)
	var oldDelete openapimodels.ReleaseItem
	require.NoError(t, db.Where("release_id = ? AND scope = ?", first.ReleaseID, "ptz:preset:delete").First(&oldDelete).Error)
	require.Equal(t, "DELETE", oldDelete.Method)
	require.Equal(t, "{}", oldDelete.RequestSchema)
	third, err := EnsurePublishedCatalog(ctx, db, 0, runtime)
	require.NoError(t, err)
	require.Equal(t, PublishActionExisting, third.Action)
	require.Equal(t, second.ReleaseID, third.ReleaseID)
}
