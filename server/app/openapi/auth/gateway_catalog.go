package auth

import (
	"context"
	"strings"

	catalogruntime "uvplatform.cn/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.cn/uvp-gb28181/app/openapi/catalog/store"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// catalogRuntimeReady is the last durable-state gate before an adapter or
// media/PTZ business call. An installation without the runtime catalog schema
// keeps the legacy static route behavior; once any catalog runtime table is
// present, only a complete and explicitly ready state may dispatch.
func (g *Gateway) catalogRuntimeReady(ctx context.Context) bool {
	if g == nil || g.db == nil {
		return false
	}
	schema, err := store.InspectCatalogSchema(g.db)
	if err != nil {
		return false
	}
	if schema == store.CatalogSchemaAbsent {
		return true
	}
	if schema != store.CatalogSchemaComplete || g.catalog == nil {
		return false
	}
	identity, ok := g.catalog.ReadyIdentity()
	if !ok {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var state openapimodels.RuntimeState
	if result := g.db.WithContext(ctx).Where("id = ?", 1).First(&state); result.Error != nil {
		return false
	}
	return runtimeIdentityMatchesState(identity, &state)
}

func runtimeIdentityMatchesState(identity catalogruntime.RuntimeIdentity, state *openapimodels.RuntimeState) bool {
	if state == nil || state.Status != openapimodels.RuntimeCatalogReady || state.ActiveRelease == nil {
		return false
	}
	return *state.ActiveRelease == identity.ReleaseID &&
		state.ActiveVersion == identity.Version &&
		state.RuntimeEpoch == identity.RuntimeEpoch &&
		strings.EqualFold(strings.TrimSpace(state.SnapshotHash), strings.TrimSpace(identity.SnapshotHash)) &&
		strings.TrimSpace(identity.SnapshotHash) != ""
}
