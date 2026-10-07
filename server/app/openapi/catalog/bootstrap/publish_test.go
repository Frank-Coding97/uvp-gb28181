package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	appmodels "uvplatform.com/uvp-gb28181/app/models"
	catalogstore "uvplatform.com/uvp-gb28181/app/openapi/catalog/store"
	openapimodels "uvplatform.com/uvp-gb28181/app/openapi/models"
)

func TestPublishedScopeSetNilSnapshot(t *testing.T) {
	require.Empty(t, publishedScopeSet(nil))
}

func TestEnsurePublishedCatalogPublishesOnceAndNeverReSeeds(t *testing.T) {
	db := newPublishTestDB(t)

	first, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionPublished, first.Action)
	require.EqualValues(t, 1, first.Version)
	require.Equal(t, len(CoreCatalogScopes()), first.ItemCount)
	require.Equal(t, len(CoreCatalogScopes()), first.Seeded.OperationsCreated)
	require.Equal(t, len(CoreCatalogScopes()), first.Seeded.CapabilitiesCreated)
	require.Equal(t, 2, first.Seeded.GroupsCreated)
	require.Empty(t, first.CoreScopesMissing)

	var state openapimodels.RuntimeState
	require.NoError(t, db.First(&state, 1).Error)
	require.NotNil(t, state.ActiveRelease)
	require.Equal(t, first.ReleaseID, *state.ActiveRelease)
	require.Equal(t, openapimodels.RuntimeCatalogReady, state.Status)
	require.EqualValues(t, 1, state.RuntimeEpoch)

	// The whole point of the startup guard: a restart of an installation whose
	// release already covers the code-owned surface must not seed again and must
	// not add a release.
	second, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionExisting, second.Action)
	require.Equal(t, first.ReleaseID, second.ReleaseID)
	require.EqualValues(t, first.Version, second.Version)
	require.Zero(t, second.Seeded.GroupsCreated)
	require.Zero(t, second.Seeded.OperationsCreated)
	require.Empty(t, second.CoreScopesMissing)

	var releases, items int64
	require.NoError(t, db.Model(&openapimodels.Release{}).Count(&releases).Error)
	require.Equal(t, int64(1), releases)
	require.NoError(t, db.Model(&openapimodels.ReleaseItem{}).Count(&items).Error)
	require.EqualValues(t, first.ItemCount, items)
	require.NoError(t, db.First(&state, 1).Error)
	require.EqualValues(t, 1, state.RuntimeEpoch, "第二次调用不该推进 runtime epoch")
}

func TestCoreCatalogGroupDriftIgnoresRemovedPlaybackSnapshot(t *testing.T) {
	db := newPublishTestDB(t)
	_, err := EnsureCoreCatalog(context.Background(), db, SystemActorID)
	require.NoError(t, err)
	drift, err := coreCatalogGroupDrift(context.Background(), db, &catalogstore.ReleaseSnapshot{
		Items: []openapimodels.ReleaseItem{{Scope: "play:live:apply", GroupCode: "playback"}},
	})
	require.NoError(t, err)
	require.False(t, drift)
}

func TestEnsurePublishedCatalogPublishesTheWholeCoreSurface(t *testing.T) {
	db := newPublishTestDB(t)
	_, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, nil)
	require.NoError(t, err)

	var published []string
	require.NoError(t, db.Model(&openapimodels.ReleaseItem{}).Order("scope").Pluck("scope", &published).Error)
	require.Equal(t, len(CoreCatalogScopes()), len(published))
	for _, scope := range CoreCatalogScopes() {
		require.Contains(t, published, scope)
	}
	// PTZ scopes have to be in the release even though no catalog adapter
	// dispatches them: client.ScopePublished reads the release.
	require.Contains(t, published, "ptz:preset:call")
}

// A release that predates part of the code-owned surface is repaired on the
// next startup, but only additively. This is the state every installation that
// published before this binary learned about the delegated scopes is in.
func TestEnsurePublishedCatalogRepairsAReleaseMissingCoreScopes(t *testing.T) {
	db := newPublishTestDB(t)
	_, err := EnsureCoreCatalog(context.Background(), db, SystemActorID)
	require.NoError(t, err)
	deleteDelegatedCatalogRows(t, db)
	publishCurrentDraft(t, db)

	partial, err := catalogstore.NewRepository(db).LoadActive(context.Background())
	require.NoError(t, err)
	require.Equal(t, len(coreReadDefinitions), len(partial.Items))

	repaired, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionRevised, repaired.Action)
	require.Equal(t, int64(1), repaired.RevisedFrom)
	require.EqualValues(t, 2, repaired.Version)
	require.Empty(t, repaired.CoreScopesMissing)
	require.Equal(t, len(CoreCatalogScopes()), repaired.ItemCount)
	require.Equal(t, len(coreDelegatedDefinitions), repaired.Seeded.OperationsCreated)

	active, err := catalogstore.NewRepository(db).LoadActive(context.Background())
	require.NoError(t, err)
	published := make(map[string]struct{}, len(active.Items))
	for _, item := range active.Items {
		published[item.Scope] = struct{}{}
	}
	for _, scope := range CoreCatalogScopes() {
		require.Contains(t, published, scope)
	}

	// And the repair is itself idempotent: the next startup finds nothing to do.
	again, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionExisting, again.Action)
	require.EqualValues(t, 2, again.Version)
	var releases int64
	require.NoError(t, db.Model(&openapimodels.Release{}).Count(&releases).Error)
	require.Equal(t, int64(2), releases)
}

// The startup repair must never shrink the contract. When the editable catalog
// no longer covers a published scope, the release is left untouched and the gap
// is reported instead of silently dropped.
func TestEnsurePublishedCatalogRefusesToDropAPublishedScope(t *testing.T) {
	db := newPublishTestDB(t)
	_, err := EnsureCoreCatalog(context.Background(), db, SystemActorID)
	require.NoError(t, err)
	createCustomCapability(t, db, "custom:thing", "/openapi/v1/custom-things")
	deleteDelegatedCatalogRows(t, db)
	publishCurrentDraft(t, db)
	// The custom capability is removed from the editable catalog after it was
	// published, so the next draft cannot cover the active release.
	deleteCapabilityByScope(t, db, "custom:thing")

	outcome, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionExisting, outcome.Action)
	require.EqualValues(t, 1, outcome.Version)
	require.ElementsMatch(t, delegatedScopes(), outcome.CoreScopesMissing)
	require.Equal(t, len(coreDelegatedDefinitions), outcome.Seeded.OperationsCreated, "补种仍然应该发生")

	var releases int64
	require.NoError(t, db.Model(&openapimodels.Release{}).Count(&releases).Error)
	require.Equal(t, int64(1), releases, "宁可少发一版,也不能悄悄砍掉一个已发布的 scope")
}

// -publish-catalog is the deliberate operator override: it publishes the
// editable catalog as it stands, including a shrink, and is still idempotent
// when nothing changed.
func TestPublishCurrentCatalogAppliesAnOperatorEditAndStaysIdempotent(t *testing.T) {
	db := newPublishTestDB(t)
	first, err := PublishCurrentCatalog(context.Background(), db, SystemActorID, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionPublished, first.Action)
	require.Equal(t, len(CoreCatalogScopes()), first.ItemCount)

	unchanged, err := PublishCurrentCatalog(context.Background(), db, SystemActorID, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionExisting, unchanged.Action)
	require.Equal(t, first.ReleaseID, unchanged.ReleaseID)

	// Disabling a core operation keeps its row but removes it from the draft,
	// which is exactly the edit an operator publication has to honour.
	require.NoError(t, db.Model(&openapimodels.Operation{}).
		Where("external_path = ?", "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}").
		Update("status", openapimodels.CatalogStatusDisabled).Error)

	edited, err := PublishCurrentCatalog(context.Background(), db, SystemActorID, nil)
	require.NoError(t, err)
	require.Equal(t, PublishActionRevised, edited.Action)
	require.EqualValues(t, 1, edited.RevisedFrom)
	require.EqualValues(t, 2, edited.Version)
	require.Equal(t, len(CoreCatalogScopes())-1, edited.ItemCount)

	var published []string
	require.NoError(t, db.Model(&openapimodels.ReleaseItem{}).Where("release_id = ?", edited.ReleaseID).Pluck("scope", &published).Error)
	require.NotContains(t, published, "ptz:preset:delete")
}

func TestEnsurePublishedCatalogRefusesMissingSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	_, err = EnsurePublishedCatalog(context.Background(), db, SystemActorID, nil)
	require.ErrorIs(t, err, catalogstore.ErrCatalogTableMissing, "未迁移的库不能被当成「已发布」静默放过")
}

// A declared active pointer that no longer resolves is not repaired: only the
// "never published at all" state is safe to seed automatically. Silently
// republishing here would replace a contract someone deliberately published.
func TestEnsurePublishedCatalogKeepsDeclaredPointerFailuresFatal(t *testing.T) {
	db := newPublishTestDB(t)
	active := int64(9)
	require.NoError(t, db.Create(&openapimodels.RuntimeState{
		ID: 1, ActiveRelease: &active, ActiveVersion: 3, SnapshotHash: "hash",
		Status: openapimodels.RuntimeCatalogUnavailable, RuntimeEpoch: 2, UpdatedAt: time.Now().UTC(),
	}).Error)

	_, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, nil)
	require.Error(t, err)
	var releases int64
	require.NoError(t, db.Model(&openapimodels.Release{}).Count(&releases).Error)
	require.Zero(t, releases)
	var groups int64
	require.NoError(t, db.Model(&openapimodels.CapabilityGroup{}).Count(&groups).Error)
	require.Zero(t, groups, "fatal 状态下不该顺手播种")
}

func newPublishTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&appmodels.SysApi{},
		&openapimodels.CapabilityGroup{}, &openapimodels.Capability{}, &openapimodels.Operation{},
		&openapimodels.Release{}, &openapimodels.ReleaseItem{}, &openapimodels.RuntimeState{},
		&openapimodels.ClientScope{},
	))
	return db
}

func delegatedScopes() []string {
	scopes := make([]string, 0, len(coreDelegatedDefinitions))
	for _, definition := range coreDelegatedDefinitions {
		scopes = append(scopes, definition.scope)
	}
	return scopes
}

// deleteDelegatedCatalogRows hard-deletes the delegated plane rows. It
// reproduces what an older binary's seed produced: rows that simply never
// existed, as opposed to rows that were deliberately disabled or soft-deleted.
func deleteDelegatedCatalogRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	var ids []int64
	require.NoError(t, db.Unscoped().Model(&openapimodels.Capability{}).Where("scope IN ?", delegatedScopes()).Pluck("id", &ids).Error)
	require.Len(t, ids, len(coreDelegatedDefinitions))
	require.NoError(t, db.Unscoped().Where("capability_id IN ?", ids).Delete(&openapimodels.Operation{}).Error)
	require.NoError(t, db.Unscoped().Where("id IN ?", ids).Delete(&openapimodels.Capability{}).Error)
}

func deleteCapabilityByScope(t *testing.T, db *gorm.DB, scope string) {
	t.Helper()
	var capability openapimodels.Capability
	require.NoError(t, db.Where("scope = ?", scope).First(&capability).Error)
	require.NoError(t, db.Unscoped().Where("capability_id = ?", capability.ID).Delete(&openapimodels.Operation{}).Error)
	require.NoError(t, db.Unscoped().Delete(&capability).Error)
}

// publishCurrentDraft writes whatever the editable catalog currently holds,
// without seeding. It is how the tests build a release that predates the
// code-owned surface.
func publishCurrentDraft(t *testing.T, db *gorm.DB) {
	t.Helper()
	repository := catalogstore.NewRepository(db)
	draft, err := repository.BuildDraft(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, draft.Operations)
	_, err = repository.Publish(context.Background(), draft, catalogstore.PublishOptions{Name: "legacy"})
	require.NoError(t, err)
}

func createCustomCapability(t *testing.T, db *gorm.DB, scope, externalPath string) {
	t.Helper()
	var group openapimodels.CapabilityGroup
	require.NoError(t, db.Where("code = ?", "device-management").First(&group).Error)
	capability := openapimodels.Capability{
		GroupID: group.ID, Code: "custom.thing", Scope: scope, Name: "自定义能力",
		ResourceType: "device", RiskLevel: "read", Status: openapimodels.CatalogStatusDraft,
	}
	require.NoError(t, db.Create(&capability).Error)
	require.NoError(t, db.Create(&openapimodels.Operation{
		CapabilityID: capability.ID, Code: "custom.thing", Name: "自定义能力", Method: "GET",
		ExternalPath: externalPath, AdapterKey: "resource.device.list.v1",
		AdapterContractVersion: "v1", ResourceType: "device", RiskLevel: "read",
		IdempotencyMode: "none", RequestSchema: "{}", ResponseSchema: "{}",
		Status: openapimodels.CatalogStatusDraft,
	}).Error)
}
