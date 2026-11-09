package bootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
	catalogstore "uvplatform.com/uvp-gb28181/app/openapi/catalog/store"
	"uvplatform.com/uvp-gb28181/app/openapi/contracts"
	models "uvplatform.com/uvp-gb28181/app/openapi/models"
)

func TestStartupRefreshesExistingCoreContractsWithoutReplacingIdentitiesOrUserSettings(t *testing.T) {
	db := publishFixture(t)
	ctx := context.Background()
	apis := map[string]int64{}
	for _, op := range contracts.Operations() {
		method := definitionSysAPIMethod(op)
		key := method + " " + op.InternalPath
		if _, exists := apis[key]; exists {
			continue
		}
		row := appmodels.SysApi{Path: op.InternalPath, Method: method, Title: op.Name, ApiGroup: op.GroupName}
		require.NoError(t, db.Create(&row).Error)
		apis[key] = int64(row.ID)
	}
	_, err := EnsureCoreCatalog(ctx, db, 0)
	require.NoError(t, err)
	var capabilities []models.Capability
	var operations []models.Operation
	require.NoError(t, db.Find(&capabilities).Error)
	require.NoError(t, db.Find(&operations).Error)
	require.Len(t, operations, len(contracts.Operations()))
	ids := map[string]int64{}
	for _, op := range operations {
		ids[op.Code] = op.ID
	}
	capIDs := map[string]int64{}
	for _, cap := range capabilities {
		capIDs[cap.Scope] = cap.ID
	}
	require.NoError(t, db.Model(&models.Capability{}).Where("id > 0").Updates(map[string]any{"sys_api_id": nil, "sys_api_path": "/api/retired/metadata", "sys_api_method": "PATCH"}).Error)
	require.NoError(t, db.Model(&models.Operation{}).Where("id > 0").Updates(map[string]any{"sys_api_id": nil, "sys_api_path": "/api/retired/metadata", "sys_api_method": "PATCH", "request_schema": "{}", "response_schema": "{}"}).Error)
	require.NoError(t, db.Model(&models.Capability{}).Where("scope = ?", "device:list").Updates(map[string]any{"name": "operator name", "description": "operator description"}).Error)
	require.NoError(t, db.Model(&models.Operation{}).Where("code = ?", "device.list").Updates(map[string]any{"name": "operator operation", "sort": 71}).Error)
	require.NoError(t, db.Model(&models.Capability{}).Where("scope = ?", "device:statistics").Update("status", models.CatalogStatusDisabled).Error)
	require.NoError(t, db.Model(&models.Operation{}).Where("code = ?", "device.statistics").Update("status", models.CatalogStatusDisabled).Error)
	grant := models.ClientScope{ClientID: 99, Scope: "device:list", Enabled: true, ScopeEpoch: 7}
	require.NoError(t, db.Create(&grant).Error)
	publishCurrentDraft(t, db)
	repository := catalogstore.NewRepository(db)
	before, err := repository.LoadActive(ctx)
	require.NoError(t, err)
	old := before.Items[0]
	revised, err := EnsurePublishedCatalog(ctx, db, 0, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionRevised, revised.Action)
	require.EqualValues(t, before.Release.Version+1, revised.Version)
	require.NoError(t, db.Find(&operations).Error)
	expected := map[string]contracts.Operation{}
	for _, op := range contracts.Operations() {
		expected[op.Code] = op
	}
	for _, op := range operations {
		want := expected[op.Code]
		require.Equal(t, ids[op.Code], op.ID)
		require.Equal(t, want.InternalPath, op.SysAPIPath, op.Code)
		require.Equal(t, definitionSysAPIMethod(want), op.SysAPIMethod, op.Code)
		require.NotNil(t, op.SysAPIID, op.Code)
		require.Equal(t, apis[op.SysAPIMethod+" "+op.SysAPIPath], *op.SysAPIID)
		require.JSONEq(t, want.RequestSchema, op.RequestSchema, op.Code)
		require.JSONEq(t, want.ResponseSchema, op.ResponseSchema, op.Code)
	}
	require.NoError(t, db.Find(&capabilities).Error)
	for _, cap := range capabilities {
		require.Equal(t, capIDs[cap.Scope], cap.ID)
		require.NotEqual(t, "/api/retired/metadata", cap.SysAPIPath)
		require.NotNil(t, cap.SysAPIID)
		if cap.Scope == "device:list" {
			require.Equal(t, "operator name", cap.Name)
			require.Equal(t, "operator description", cap.Description)
		}
		if cap.Scope == "device:statistics" {
			require.Equal(t, models.CatalogStatusDisabled, cap.Status)
		}
	}
	var op models.Operation
	require.NoError(t, db.Where("code = ?", "device.list").First(&op).Error)
	require.Equal(t, "operator operation", op.Name)
	require.Equal(t, 71, op.Sort)
	var preserved models.ClientScope
	require.NoError(t, db.Where("client_id = ? AND scope = ?", 99, "device:list").First(&preserved).Error)
	require.True(t, preserved.Enabled)
	require.EqualValues(t, 7, preserved.ScopeEpoch)
	var oldItem models.ReleaseItem
	require.NoError(t, db.First(&oldItem, old.ID).Error)
	require.Equal(t, old, oldItem, "published snapshots remain immutable")
	again, err := EnsurePublishedCatalog(ctx, db, 0, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionExisting, again.Action)
	require.Equal(t, revised.ReleaseID, again.ReleaseID)
}

func TestStartupRefreshesSchemaOnlyDriftAndDoesNotRepublishEveryRestart(t *testing.T) {
	db := publishFixture(t)
	ctx := context.Background()
	_, err := EnsureCoreCatalog(ctx, db, 0)
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.Operation{}).Where("code = ?", "device.list").Update("response_schema", `{"type":"object","properties":{"legacy":{"type":"string"}}}`).Error)
	publishCurrentDraft(t, db)
	first, err := EnsurePublishedCatalog(ctx, db, 0, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionRevised, first.Action)
	second, err := EnsurePublishedCatalog(ctx, db, 0, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionExisting, second.Action)
	require.Equal(t, first.ReleaseID, second.ReleaseID)
}

func TestStartupRepairsOnlySourceDriftWithoutRepublishingHealthySnapshot(t *testing.T) {
	db := publishFixture(t)
	ctx := context.Background()
	first, err := EnsurePublishedCatalog(ctx, db, 0, nil)
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.Operation{}).Where("code = ?", "device.list").Updates(map[string]any{"request_schema": `{}`, "response_schema": `{}`, "sys_api_path": "/retired"}).Error)
	require.NoError(t, db.Model(&models.Capability{}).Where("scope = ?", "device:list").Update("sys_api_path", "/retired").Error)
	second, err := EnsurePublishedCatalog(ctx, db, 0, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionExisting, second.Action)
	require.Equal(t, first.ReleaseID, second.ReleaseID)
	var operation models.Operation
	require.NoError(t, db.Where("code = ?", "device.list").First(&operation).Error)
	for _, definition := range contracts.Operations() {
		if definition.Code == operation.Code {
			require.Equal(t, definition.InternalPath, operation.SysAPIPath)
			require.JSONEq(t, definition.RequestSchema, operation.RequestSchema)
			require.JSONEq(t, definition.ResponseSchema, operation.ResponseSchema)
		}
	}
}

func TestOperatorPublicationRefreshesSchemaOnlyDrift(t *testing.T) {
	db := publishFixture(t)
	ctx := context.Background()
	_, err := EnsureCoreCatalog(ctx, db, 0)
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.Operation{}).Where("code = ?", "device.list").Update("request_schema", `{"type":"object","properties":{"legacy":{"type":"string"}}}`).Error)
	publishCurrentDraft(t, db)
	first, err := PublishCurrentCatalog(ctx, db, 0, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionRevised, first.Action)
	second, err := PublishCurrentCatalog(ctx, db, 0, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionExisting, second.Action)
	require.Equal(t, first.ReleaseID, second.ReleaseID)
}
