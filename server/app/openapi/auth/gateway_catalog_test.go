package auth

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/catalog/store"
	openapimodels "uvplatform.com/uvp-gb28181/app/openapi/models"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

type catalogDispatchSpy struct {
	mu         sync.Mutex
	invocation catalogruntime.Invocation
}

func (spy *catalogDispatchSpy) Execute(_ context.Context, invocation catalogruntime.Invocation) (any, error) {
	spy.mu.Lock()
	spy.invocation = invocation
	spy.mu.Unlock()
	if invocation.Scope == "device:list" {
		return map[string]string{"source": "catalog"}, nil
	}
	return invocation.Value, nil
}

func (spy *catalogDispatchSpy) Invocation() catalogruntime.Invocation {
	spy.mu.Lock()
	defer spy.mu.Unlock()
	return spy.invocation
}

func TestOpenAPIGatewayUsesActiveCatalogRuntimeForMetadataRead(t *testing.T) {
	gate, _, secret := gatewayFixture(t)
	registry := catalogruntime.NewAdapterRegistry()
	spy := &catalogDispatchSpy{}
	require.NoError(t, registry.Register(catalogruntime.AdapterRegistration{
		Key: "test.catalog.device-list.v1", ContractVersion: "v1", Adapter: spy,
	}))
	catalog := catalogruntime.NewCatalogRuntime(registry, nil)
	_, report, err := catalog.Publish(context.Background(), catalogruntime.Draft{Operations: []catalogruntime.Operation{{
		Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices",
		AdapterKey: "test.catalog.device-list.v1", ContractVersion: "v1",
	}}})
	require.NoError(t, err)
	require.True(t, report.Valid())
	gate.catalog = catalog

	var legacyCalls atomic.Int32
	gate.read = func(context.Context, *gorm.DB, metadataInput) (any, error) {
		legacyCalls.Add(1)
		return map[string]string{"source": "legacy"}, nil
	}

	response := gatewayCall(t, gate, secret, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil)

	require.Equal(t, 200, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"source":"catalog"`)
	require.Zero(t, legacyCalls.Load())
	invocation := spy.Invocation()
	require.Equal(t, "device:list", invocation.Scope)
	require.Equal(t, "GET", invocation.Method)
	require.Equal(t, "/openapi/v1/devices", invocation.Path)
	request, ok := invocation.Value.(adapters.Request)
	require.True(t, ok)
	require.Equal(t, resource.DepartmentScope{OwnerDeptID: 10, DataScope: resource.DataScopeDepartment}, request.ResourceScope)
	require.Equal(t, adapters.ListOptions{Page: 1, PageSize: 20}, request.List)
}

func TestOpenAPIGatewayRejectsDurableRuntimeIdentityDrift(t *testing.T) {
	gate, db, _ := gatewayFixture(t)
	require.NoError(t, db.AutoMigrate(&openapimodels.Release{}, &openapimodels.ReleaseItem{}, &openapimodels.RuntimeState{}))
	release := openapimodels.Release{Version: 1, Status: openapimodels.ReleaseStatusPublished, SnapshotHash: "0123456789abcdef", ItemCount: 0, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	require.NoError(t, db.Create(&release).Error)
	activeRelease := release.ID
	now := time.Now().UTC()
	require.NoError(t, db.Create(&openapimodels.RuntimeState{
		ID: 1, ActiveRelease: &activeRelease, ActiveVersion: release.Version, SnapshotHash: release.SnapshotHash,
		Status: openapimodels.RuntimeCatalogReady, RuntimeEpoch: 1, LoadedAt: &now, UpdatedAt: now,
	}).Error)

	registry := catalogruntime.NewAdapterRegistry()
	adapter := &catalogDispatchSpy{}
	require.NoError(t, registry.Register(catalogruntime.AdapterRegistration{Key: "test.catalog.device-list.v1", ContractVersion: "v1", Adapter: adapter}))
	catalog := catalogruntime.NewCatalogRuntime(registry, nil)
	_, report, err := catalog.Publish(context.Background(), catalogruntime.Draft{Operations: []catalogruntime.Operation{{
		Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "test.catalog.device-list.v1", ContractVersion: "v1",
	}}})
	require.NoError(t, err)
	require.True(t, report.Valid())
	require.NoError(t, catalog.BindIdentity(catalogruntime.RuntimeIdentity{ReleaseID: release.ID, Version: release.Version, SnapshotHash: release.SnapshotHash, RuntimeEpoch: 1}))
	gate.catalog = catalog
	require.True(t, gate.catalogRuntimeReady(context.Background()))

	require.NoError(t, db.Model(&openapimodels.RuntimeState{}).Where("id = ?", 1).Update("runtime_epoch", 2).Error)
	require.False(t, gate.catalogRuntimeReady(context.Background()))
	adapter.mu.Lock()
	called := adapter.invocation.Scope != ""
	adapter.mu.Unlock()
	require.False(t, called, "a stale durable runtime must be rejected before adapter dispatch")
}

func TestOpenAPIGatewayRejectsHydratedRuntimeAfterCatalogPublicationUntilRestart(t *testing.T) {
	gate, db, _ := gatewayFixture(t)
	require.NoError(t, db.AutoMigrate(
		&openapimodels.CapabilityGroup{}, &openapimodels.Capability{}, &openapimodels.Operation{},
		&openapimodels.Release{}, &openapimodels.ReleaseItem{}, &openapimodels.RuntimeState{},
	))

	now := time.Now().UTC()
	group := openapimodels.CapabilityGroup{Code: "device-management", Name: "设备管理", Status: openapimodels.CatalogStatusActive, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&group).Error)
	capability := openapimodels.Capability{
		GroupID: group.ID, Code: "device.list", Scope: "device:list", Name: "设备列表",
		ResourceType: "device", RiskLevel: "read", Status: openapimodels.CatalogStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&capability).Error)
	operation := openapimodels.Operation{
		CapabilityID: capability.ID, Code: "list", Name: "设备列表", Method: "GET",
		ExternalPath: "/openapi/v1/devices", AdapterKey: adapters.DeviceListAdapterKey,
		AdapterContractVersion: adapters.ResourceAdapterContractVersion, ResourceType: "device", RiskLevel: "read",
		IdempotencyMode: "none", RequestSchema: `{}`, ResponseSchema: `{}`, Status: openapimodels.CatalogStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&operation).Error)

	repo := store.NewRepository(db)
	draft, err := repo.BuildDraft(context.Background())
	require.NoError(t, err)
	_, err = repo.Publish(context.Background(), draft, store.PublishOptions{Version: 1, Name: "initial"})
	require.NoError(t, err)

	registry := catalogruntime.NewAdapterRegistry()
	for _, registration := range adapters.NewDeviceChannelAdapterRegistrations(resource.New(db)) {
		require.NoError(t, registry.Register(registration))
	}
	oldRuntime := catalogruntime.NewCatalogRuntime(registry, nil)
	require.NoError(t, repo.Hydrate(context.Background(), oldRuntime))
	gate.catalog = oldRuntime
	require.True(t, gate.catalogRuntimeReady(context.Background()))

	require.NoError(t, db.Model(&openapimodels.Operation{}).Where("id = ?", operation.ID).Update("external_path", "/openapi/v1/devices-v2").Error)
	draft, err = repo.BuildDraft(context.Background())
	require.NoError(t, err)
	_, err = repo.Publish(context.Background(), draft, store.PublishOptions{Version: 2, Name: "second"})
	require.NoError(t, err)
	require.False(t, gate.catalogRuntimeReady(context.Background()), "the old process runtime must fail closed after durable publication")

	restartedRuntime := catalogruntime.NewCatalogRuntime(registry, nil)
	require.NoError(t, repo.Hydrate(context.Background(), restartedRuntime))
	gate.catalog = restartedRuntime
	require.True(t, gate.catalogRuntimeReady(context.Background()), "a restarted runtime must hydrate the new active release")
}

func TestNewReadScopesRequireExplicitGrantAndRejectResourceSubstitution(t *testing.T) {
	for _, scope := range []string{"channel:capabilities", "channel:storage-cards"} {
		for _, kind := range []string{"ungranted", "valid", "cross-device", "replay", "catalog-missing"} {
			t.Run(scope+"/"+kind, func(t *testing.T) {
				gate, db, secret := gatewayFixture(t)
				require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceStorageCard{}))
				deviceID, channelID := "34020000002000000010", "37011200001310000010"
				require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: deviceID, OwnerDeptID: 10}).Error)
				require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: deviceID, ChannelID: channelID, OwnerDeptID: 10}).Error)
				if kind == "cross-device" {
					require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "34020000002000000020", OwnerDeptID: 10}).Error)
					deviceID = "34020000002000000020"
				}
				if kind != "ungranted" {
					require.NoError(t, db.Create(&openapimodels.ClientScope{ClientID: 1, Scope: scope, Enabled: true}).Error)
				}
				if kind == "catalog-missing" {
					require.NoError(t, db.AutoMigrate(&openapimodels.RuntimeState{}))
				}
				path := strings.NewReplacer(":deviceId", deviceID, ":channelId", channelID).Replace(metadataPattern(scope))
				input := SignatureInput{Method: "GET", Path: path, AccessKey: fmt.Sprintf("uvp_%032x", 1), Timestamp: fmt.Sprint(time.Now().Unix()), Nonce: strings.Repeat("b", 32), Audience: "test-audience"}
				signature, err := Sign(input, secret)
				require.NoError(t, err)
				req := httptest.NewRequest("GET", path, nil)
				for k, v := range map[string]string{"X-UVP-Sign-Version": "1", "X-UVP-Access-Key": input.AccessKey, "X-UVP-Timestamp": input.Timestamp, "X-UVP-Nonce": input.Nonce, "X-UVP-Signature": signature} {
					req.Header.Set(k, v)
				}
				router := gin.New()
				router.GET(metadataPattern(scope), gate.Handler(scope))
				out := httptest.NewRecorder()
				router.ServeHTTP(out, req)
				expected := 200
				switch kind {
				case "ungranted":
					expected = 403
				case "cross-device":
					expected = 404
				case "catalog-missing":
					expected = 503
				}
				require.Equal(t, expected, out.Code, out.Body.String())
				require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
				if kind == "valid" && scope == "channel:capabilities" {
					require.Contains(t, out.Body.String(), `"state":"unknown"`)
				}
				if kind == "valid" && scope == "channel:storage-cards" {
					require.Contains(t, out.Body.String(), `"items":[]`)
					require.Contains(t, out.Body.String(), `"freshness":"unknown"`)
				}
				if kind == "replay" {
					out = httptest.NewRecorder()
					router.ServeHTTP(out, req.Clone(context.Background()))
					require.Equal(t, 401, out.Code, out.Body.String())
					require.Contains(t, out.Body.String(), "REQUEST_REPLAYED")
				}
			})
		}
	}
}

type liveCatalogFallbackSpy struct{ calls atomic.Int32 }

func (spy *liveCatalogFallbackSpy) Ready() bool { return true }
func (spy *liveCatalogFallbackSpy) Start(context.Context, PlayRequest) (any, error) {
	spy.calls.Add(1)
	return nil, ErrPlayUnavailable
}
func (spy *liveCatalogFallbackSpy) Stop(context.Context, PlayRequest) (any, error) {
	spy.calls.Add(1)
	return nil, ErrPlayUnavailable
}

func TestLivePlaybackUsesPublishedAdapterInsteadOfFallback(t *testing.T) {
	gate, db, secret := gatewayFixture(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	deviceID, channelID := "34020000002000000010", "37011200001310000010"
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: deviceID, OwnerDeptID: 10}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: deviceID, ChannelID: channelID, OwnerDeptID: 10}).Error)
	require.NoError(t, db.Create(&openapimodels.ClientScope{ClientID: 1, Scope: PlayLiveScope, Enabled: true}).Error)
	registry := catalogruntime.NewAdapterRegistry()
	spy := &catalogDispatchSpy{}
	require.NoError(t, registry.Register(catalogruntime.AdapterRegistration{Key: adapters.PlayLiveAdapterKey, ContractVersion: "v1", Adapter: spy}))
	runtime := catalogruntime.NewCatalogRuntime(registry, nil)
	_, report, err := runtime.Publish(context.Background(), catalogruntime.Draft{Operations: []catalogruntime.Operation{{Scope: PlayLiveScope, Method: "POST", ExternalPath: "/openapi/v1/devices/{deviceId}/channels/{channelId}/live", AdapterKey: adapters.PlayLiveAdapterKey, ContractVersion: "v1"}}})
	require.NoError(t, err)
	require.True(t, report.Valid())
	gate.catalog = runtime
	fallback := &liveCatalogFallbackSpy{}
	gate.play = fallback
	path := "/openapi/v1/devices/" + deviceID + "/channels/" + channelID + "/live"
	input := SignatureInput{Method: "POST", Path: path, AccessKey: fmt.Sprintf("uvp_%032x", 1), Timestamp: fmt.Sprint(time.Now().Unix()), Nonce: strings.Repeat("f", 32), Audience: "test-audience"}
	signature, err := Sign(input, secret)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", path, nil)
	for k, v := range map[string]string{"X-UVP-Sign-Version": "1", "X-UVP-Access-Key": input.AccessKey, "X-UVP-Timestamp": input.Timestamp, "X-UVP-Nonce": input.Nonce, "X-UVP-Signature": signature} {
		req.Header.Set(k, v)
	}
	router := gin.New()
	router.POST(playPattern(PlayLiveScope), gate.Handler(PlayLiveScope))
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	require.Equal(t, 200, out.Code, out.Body.String())
	require.Zero(t, fallback.calls.Load())
	typed, ok := spy.Invocation().Value.(PlayRequest)
	require.True(t, ok)
	require.EqualValues(t, 1, typed.ClientID)
	require.Equal(t, deviceID, typed.DeviceID)
	require.EqualValues(t, 10, typed.OwnerDeptID)
}
