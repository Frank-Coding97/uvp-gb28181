package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	catalogruntime "uvplatform.cn/uvp-gb28181/app/openapi/catalog/runtime"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestRepositoryBuildDraftMapsCatalogAndRetainsClientOrphans(t *testing.T) {
	db := openCatalogStoreDB(t, true)
	createdAt := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	group := openapimodels.CapabilityGroup{
		Code: "device-management", Name: "设备管理", Status: openapimodels.CatalogStatusActive,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	require.NoError(t, db.Create(&group).Error)
	capability := openapimodels.Capability{
		GroupID: group.ID, Code: "device.list", Scope: "device:list", Name: "设备列表",
		ResourceType: "device", RiskLevel: "read", Status: openapimodels.CatalogStatusActive,
		SysAPIPath: "/api/gb28181/device-mgmt/devices", SysAPIMethod: "GET",
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	require.NoError(t, db.Create(&capability).Error)
	operation := openapimodels.Operation{
		CapabilityID: capability.ID, Code: "list", Name: "列出设备", Method: "GET",
		ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list",
		AdapterContractVersion: "2", ResourceType: "device", RiskLevel: "read",
		IdempotencyMode: "none", RequestSchema: `{}`, ResponseSchema: `{"type":"array"}`,
		SysAPIPath: "/api/gb28181/device-mgmt/devices", SysAPIMethod: "GET",
		Status: openapimodels.CatalogStatusActive, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	require.NoError(t, db.Create(&operation).Error)
	require.NoError(t, db.Create(&openapimodels.ClientScope{ClientID: 7, Scope: "legacy:scope", Enabled: false, UpdatedAt: createdAt}).Error)

	draft, err := NewRepository(db).BuildDraft(context.Background())
	require.NoError(t, err)
	require.Len(t, draft.Operations, 1)
	require.Equal(t, "device:list", draft.Operations[0].Scope)
	require.Equal(t, "列出设备", draft.Operations[0].Name)
	require.Equal(t, "v2", draft.Operations[0].ContractVersion)
	require.Zero(t, draft.Operations[0].SysAPIID)
	require.Equal(t, "/api/gb28181/device-mgmt/devices", draft.Operations[0].SysAPIPath)
	require.Contains(t, draft.OrphanScopes, "legacy:scope")
}

func TestRepositoryTreatsNilContextAsBackgroundContext(t *testing.T) {
	db := openCatalogStoreDB(t, true)
	seedCatalogStore(t, db, "v1")
	draft, err := NewRepository(db).BuildDraft(nil)
	require.NoError(t, err)
	require.Len(t, draft.Operations, 1)
}

func TestRepositoryBuildDraftDistinguishesMissingTableAndDependencyFailure(t *testing.T) {
	db := openCatalogStoreDB(t, false)
	// Source tables exist, but the compatibility client-scope table was not
	// migrated yet. This must be reported as a schema/table problem.
	require.NoError(t, db.AutoMigrate(&openapimodels.CapabilityGroup{}, &openapimodels.Capability{}, &openapimodels.Operation{}))
	_, err := NewRepository(db).BuildDraft(context.Background())
	require.ErrorIs(t, err, ErrCatalogTableMissing)

	db = openCatalogStoreDB(t, true)
	group := openapimodels.CapabilityGroup{Code: "device", Name: "设备", Status: openapimodels.CatalogStatusActive}
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&openapimodels.Operation{
		CapabilityID: 9999, Code: "broken", Name: "broken", Method: "GET",
		ExternalPath: "/openapi/v1/broken", AdapterKey: "broken", AdapterContractVersion: "1",
		RequestSchema: `{}`, ResponseSchema: `{}`, Status: openapimodels.CatalogStatusActive,
	}).Error)
	_, err = NewRepository(db).BuildDraft(context.Background())
	require.ErrorIs(t, err, ErrCatalogDependency)
}

func TestPublisherIsAtomicAndReleaseItemsAreImmutableSnapshots(t *testing.T) {
	db := openCatalogStoreDB(t, true)
	seedCatalogStore(t, db, "v1")
	repo := NewRepository(db)
	draft, err := repo.BuildDraft(context.Background())
	require.NoError(t, err)

	first, err := repo.Publish(context.Background(), draft, PublishOptions{Version: 1, Name: "initial"})
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Release.Version)
	require.Len(t, first.Items, 1)
	require.Equal(t, "v1", first.Items[0].AdapterContractVersion)

	// Mutating the editable catalog after publication cannot mutate the release
	// item, which is the runtime source of truth.
	require.NoError(t, db.Model(&openapimodels.Operation{}).Where("id = ?", first.Items[0].OperationID).Updates(map[string]any{
		"external_path": "/openapi/v1/tampered", "adapter_key": "tampered",
	}).Error)
	loaded, err := repo.LoadActive(context.Background())
	require.NoError(t, err)
	require.Equal(t, first.Release.ID, loaded.Release.ID)
	require.Equal(t, "/openapi/v1/devices", loaded.Items[0].ExternalPath)
	require.Equal(t, "device.list", loaded.Items[0].AdapterKey)

	// Fail during the second release-item insert, after the release row and the
	// first item were written. The transaction must restore the old pointer.
	broken := draft
	broken.Operations = append(broken.Operations, broken.Operations[0])
	_, err = repo.Publish(context.Background(), broken, PublishOptions{Version: 2, Name: "broken"})
	require.Error(t, err)
	activeAfterFailure, err := repo.LoadActive(context.Background())
	require.NoError(t, err)
	require.Equal(t, first.Release.ID, activeAfterFailure.Release.ID)
	var releaseCount int64
	require.NoError(t, db.Model(&openapimodels.Release{}).Count(&releaseCount).Error)
	require.Equal(t, int64(1), releaseCount)

	var capability openapimodels.Capability
	require.NoError(t, db.First(&capability, first.Items[0].CapabilityID).Error)
	require.Equal(t, openapimodels.CatalogStatusActive, capability.Status)
	require.NotNil(t, capability.PublishedAt)
}

func TestRepositoryLoadActiveFailsClosedForRuntimeAndSnapshotDrift(t *testing.T) {
	db := openCatalogStoreDB(t, true)
	seedCatalogStore(t, db, "v1")
	repo := NewRepository(db)
	draft, err := repo.BuildDraft(context.Background())
	require.NoError(t, err)
	published, err := repo.Publish(context.Background(), draft, PublishOptions{Version: 1, Name: "initial"})
	require.NoError(t, err)

	var state openapimodels.RuntimeState
	require.NoError(t, db.First(&state, 1).Error)
	require.NoError(t, db.Model(&state).Update("status", openapimodels.RuntimeCatalogUnavailable).Error)
	_, err = repo.LoadActive(context.Background())
	require.ErrorIs(t, err, ErrCatalogRuntimeUnavailable)

	require.NoError(t, db.Model(&state).Updates(map[string]any{
		"status": openapimodels.RuntimeCatalogReady, "active_version": published.Release.Version + 1,
	}).Error)
	_, err = repo.LoadActive(context.Background())
	require.ErrorIs(t, err, ErrCatalogDependency)

	require.NoError(t, db.Model(&state).Updates(map[string]any{
		"active_version": published.Release.Version, "snapshot_hash": "different",
	}).Error)
	_, err = repo.LoadActive(context.Background())
	require.ErrorIs(t, err, ErrCatalogDependency)
}

func TestRepositoryLoadActiveRequiresExplicitReadyStatus(t *testing.T) {
	db := openCatalogStoreDB(t, true)
	seedCatalogStore(t, db, "v1")
	repo := NewRepository(db)
	draft, err := repo.BuildDraft(context.Background())
	require.NoError(t, err)
	_, err = repo.Publish(context.Background(), draft, PublishOptions{Version: 1, Name: "initial"})
	require.NoError(t, err)

	var state openapimodels.RuntimeState
	require.NoError(t, db.First(&state, runtimeStateID).Error)
	require.NoError(t, db.Model(&state).Update("status", "").Error)

	_, err = repo.LoadActive(context.Background())
	require.ErrorIs(t, err, ErrCatalogRuntimeUnavailable)
}

func TestRepositoryPublishRejectsReuseOfPersistedTombstone(t *testing.T) {
	db := openCatalogStoreDB(t, true)
	seedCatalogStore(t, db, "v1")
	repo := NewRepository(db)
	draft, err := repo.BuildDraft(context.Background())
	require.NoError(t, err)
	_, err = repo.Publish(context.Background(), draft, PublishOptions{Version: 1, Name: "initial"})
	require.NoError(t, err)

	_, err = repo.Publish(context.Background(), catalogruntime.Draft{}, PublishOptions{Version: 2, Name: "removed"})
	require.NoError(t, err)

	_, err = repo.Publish(context.Background(), draft, PublishOptions{Version: 3, Name: "reused"})
	require.ErrorIs(t, err, catalogruntime.ErrDraftInvalid)
}

func TestRepositoryPublishRejectsDuplicateScopeAndRouteBeforePersistence(t *testing.T) {
	db := openCatalogStoreDB(t, true)
	seedCatalogStore(t, db, "v1")
	repo := NewRepository(db)

	// A second operation may legally have a different route in the source
	// catalog, but it cannot reuse the same externally granted scope.
	var capability openapimodels.Capability
	require.NoError(t, db.First(&capability).Error)
	require.NoError(t, db.Create(&openapimodels.Operation{
		CapabilityID: capability.ID, Code: "summary", Name: "设备摘要", Method: "GET",
		ExternalPath: "/openapi/v1/devices/summary", AdapterKey: "device.list", AdapterContractVersion: "v1",
		ResourceType: "device", RiskLevel: "read", IdempotencyMode: "none", RequestSchema: `{}`, ResponseSchema: `{}`,
		Status: openapimodels.CatalogStatusActive,
	}).Error)
	draft, err := repo.BuildDraft(context.Background())
	require.NoError(t, err)
	require.Len(t, draft.Operations, 2)
	_, err = repo.Publish(context.Background(), draft, PublishOptions{Version: 1, Name: "duplicate-scope"})
	require.ErrorIs(t, err, catalogruntime.ErrDraftInvalid)

	db = openCatalogStoreDB(t, true)
	seedCatalogStore(t, db, "v1")
	repo = NewRepository(db)
	draft, err = repo.BuildDraft(context.Background())
	require.NoError(t, err)
	draft.Operations = append(draft.Operations, draft.Operations[0])
	_, err = repo.Publish(context.Background(), draft, PublishOptions{Version: 1, Name: "duplicate-route"})
	require.ErrorIs(t, err, catalogruntime.ErrDraftInvalid)
}

func TestRepositoryRejectsTamperedSnapshotEvenWhenHashIsRecomputed(t *testing.T) {
	db := openCatalogStoreDB(t, true)
	seedCatalogStore(t, db, "v1")
	repo := NewRepository(db)
	draft, err := repo.BuildDraft(context.Background())
	require.NoError(t, err)
	published, err := repo.Publish(context.Background(), draft, PublishOptions{Version: 1, Name: "initial"})
	require.NoError(t, err)

	item := published.Items[0]
	snapshot, err := decodeSnapshot(item.SnapshotJSON)
	require.NoError(t, err)
	snapshot.AdapterKey = "tampered.adapter"
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	item.SnapshotJSON = string(raw)
	digest, err := hashReleaseItems([]openapimodels.ReleaseItem{item})
	require.NoError(t, err)
	require.NoError(t, db.Model(&openapimodels.ReleaseItem{}).Where("id = ?", item.ID).Update("snapshot_json", item.SnapshotJSON).Error)
	require.NoError(t, db.Model(&openapimodels.Release{}).Where("id = ?", published.Release.ID).Update("snapshot_hash", digest).Error)

	_, err = repo.LoadActive(context.Background())
	require.ErrorIs(t, err, ErrCatalogDependency)
}

func TestRepositoryBuildDraftAndHydrateRestoreHistoricalTombstone(t *testing.T) {
	db := openCatalogStoreDB(t, true)
	seedCatalogStore(t, db, "v1")
	repo := NewRepository(db)
	firstDraft, err := repo.BuildDraft(context.Background())
	require.NoError(t, err)
	_, err = repo.Publish(context.Background(), firstDraft, PublishOptions{Version: 1, Name: "initial"})
	require.NoError(t, err)
	require.NoError(t, db.Create(&openapimodels.ClientScope{
		ClientID: 7, Scope: "device:list", Enabled: false, UpdatedAt: time.Now().UTC(),
	}).Error)
	require.NoError(t, db.Create(&openapimodels.ClientScope{
		ClientID: 7, Scope: "legacy:scope", Enabled: false, UpdatedAt: time.Now().UTC(),
	}).Error)

	require.NoError(t, db.Model(&openapimodels.Operation{}).Where("code = ?", "list").Update("status", openapimodels.CatalogStatusDisabled).Error)
	secondDraft, err := repo.BuildDraft(context.Background())
	require.NoError(t, err)
	require.Empty(t, secondDraft.Operations)
	require.Contains(t, secondDraft.OrphanScopes, "device:list")
	require.Contains(t, secondDraft.OrphanScopes, "legacy:scope")
	_, err = repo.Publish(context.Background(), secondDraft, PublishOptions{Version: 2, Name: "removed"})
	require.NoError(t, err)
	activeSnapshot, err := repo.LoadActive(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"device:list"}, activeSnapshot.TombstoneScopes)

	registry := catalogruntime.NewAdapterRegistry()
	require.NoError(t, registry.Register(catalogruntime.AdapterRegistration{Key: "device.list", ContractVersion: "v1", Adapter: &hydrationAdapter{}}))
	runtime := catalogruntime.NewCatalogRuntime(registry, nil)
	require.NoError(t, repo.Hydrate(context.Background(), runtime))
	require.Equal(t, catalogruntime.ScopeTombstoned, runtime.ScopeStatus("device:list"))
	require.Equal(t, catalogruntime.ScopeOrphan, runtime.ScopeStatus("legacy:scope"))
}

func TestRepositoryPublishRejectsContractDriftFromEditableCatalog(t *testing.T) {
	db := openCatalogStoreDB(t, true)
	seedCatalogStore(t, db, "v1")
	repo := NewRepository(db)
	draft, err := repo.BuildDraft(context.Background())
	require.NoError(t, err)
	draft.Operations[0].AdapterKey = "untrusted.adapter"
	_, err = repo.Publish(context.Background(), draft, PublishOptions{Version: 1, Name: "drift"})
	require.ErrorIs(t, err, ErrDraftOperationNotFound)
}

func TestRepositoryHydratesRuntimeFromDurableActiveRelease(t *testing.T) {
	db := openCatalogStoreDB(t, true)
	seedCatalogStore(t, db, "v1")
	repo := NewRepository(db)
	draft, err := repo.BuildDraft(context.Background())
	require.NoError(t, err)
	published, err := repo.Publish(context.Background(), draft, PublishOptions{Version: 1, Name: "initial"})
	require.NoError(t, err)

	adapter := &hydrationAdapter{}
	registry := catalogruntime.NewAdapterRegistry()
	require.NoError(t, registry.Register(catalogruntime.AdapterRegistration{Key: "device.list", ContractVersion: "v1", Adapter: adapter}))
	runtime := catalogruntime.NewCatalogRuntime(registry, nil)
	require.NoError(t, repo.Hydrate(context.Background(), runtime))
	active := runtime.Active()
	require.NotNil(t, active)
	var state openapimodels.RuntimeState
	require.NoError(t, db.First(&state, runtimeStateID).Error)
	identity, ok := runtime.Identity()
	require.True(t, ok)
	require.Equal(t, published.Release.ID, identity.ReleaseID)
	require.Equal(t, published.Release.Version, identity.Version)
	require.Equal(t, published.Release.SnapshotHash, identity.SnapshotHash)
	require.Equal(t, state.RuntimeEpoch, identity.RuntimeEpoch)
	operation, resolveErr := runtime.Resolve("GET", "/openapi/v1/devices")
	require.NoError(t, resolveErr)
	require.Equal(t, "device:list", operation.Scope)
}

type hydrationAdapter struct{}

func (*hydrationAdapter) Execute(context.Context, catalogruntime.Invocation) (any, error) {
	return nil, nil
}

func openCatalogStoreDB(t *testing.T, withClientScope bool) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	models := []any{
		&openapimodels.CapabilityGroup{}, &openapimodels.Capability{}, &openapimodels.Operation{},
		&openapimodels.Release{}, &openapimodels.ReleaseItem{}, &openapimodels.RuntimeState{},
	}
	if withClientScope {
		models = append(models, &openapimodels.ClientScope{})
	}
	require.NoError(t, db.AutoMigrate(models...))
	return db
}

func seedCatalogStore(t *testing.T, db *gorm.DB, version string) {
	createdAt := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	group := openapimodels.CapabilityGroup{Code: "device-management", Name: "设备管理", Status: openapimodels.CatalogStatusActive, CreatedAt: createdAt, UpdatedAt: createdAt}
	require.NoError(t, db.Create(&group).Error)
	capability := openapimodels.Capability{
		GroupID: group.ID, Code: "device.list", Scope: "device:list", Name: "设备列表", ResourceType: "device", RiskLevel: "read", Status: openapimodels.CatalogStatusActive,
		SysAPIPath: "/api/gb28181/device-mgmt/devices", SysAPIMethod: "GET", CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	require.NoError(t, db.Create(&capability).Error)
	require.NoError(t, db.Create(&openapimodels.Operation{
		CapabilityID: capability.ID, Code: "list", Name: "设备列表", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list", AdapterContractVersion: version,
		ResourceType: "device", RiskLevel: "read", IdempotencyMode: "none", RequestSchema: `{}`, ResponseSchema: `{}`, SysAPIPath: capability.SysAPIPath, SysAPIMethod: "GET", Status: openapimodels.CatalogStatusActive, CreatedAt: createdAt, UpdatedAt: createdAt,
	}).Error)
}
