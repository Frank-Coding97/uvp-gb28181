package adapters

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
)

func TestDelegatedPlaneRegistrationsCoverTheWholeDelegatedSurface(t *testing.T) {
	registrations := NewDelegatedPlaneRegistrations()
	require.Len(t, registrations, 16)

	registry := catalogruntime.NewAdapterRegistry()
	expected := map[string]string{
		PlayLiveAdapterKey:             DelegatedPlayPlane,
		PlayLiveStopAdapterKey:         DelegatedPlayPlane,
		PTZPresetListAdapterKey:        DelegatedPTZPlane,
		PTZPresetSaveAdapterKey:        DelegatedPTZPlane,
		PTZPresetCallAdapterKey:        DelegatedPTZPlane,
		PTZPresetDeleteAdapterKey:      DelegatedPTZPlane,
		PTZOperationReadAdapterKey:     DelegatedPTZPlane,
		PTZMoveAdapterKey:              DelegatedPTZPlane,
		PTZPreciseAdapterKey:           DelegatedPTZPlane,
		PTZExtendedAdapterKey:          DelegatedPTZPlane,
		PTZWiperAdapterKey:             DelegatedPTZPlane,
		PTZHomePositionSetAdapterKey:   DelegatedPTZPlane,
		PTZCruiseControlAdapterKey:     DelegatedPTZPlane,
		PTZCruiseTrackCreateAdapterKey: DelegatedPTZPlane,
		PTZCruiseTrackReadAdapterKey:   DelegatedPTZPlane,
		PTZTargetTrackAdapterKey:       DelegatedPTZPlane,
	}
	require.Len(t, registrations, len(expected))
	for _, registration := range registrations {
		require.NoError(t, registry.Register(registration))
		require.Equal(t, DelegatedPlaneContractVersion, registration.ContractVersion)
		plane, ok := expected[registration.Key]
		require.True(t, ok, "意外的 adapter key %s", registration.Key)
		require.Equal(t, plane, registration.Adapter.(*delegatedPlaneAdapter).plane)
	}
	// The delegated keys must not collide with the resource read surface that
	// shares this registry.
	for _, registration := range NewDeviceChannelAdapterRegistrations(nil) {
		require.False(t, registry.Has(registration.Key, registration.ContractVersion), "delegated key 与 %s 冲突", registration.Key)
	}
}

// These scopes are consumed by handleMedia/handlePTZ before the metadata
// dispatcher, so reaching an adapter means a routing regression. Fail closed
// with a classification that says so, instead of pretending the operation ran.
func TestDelegatedPlaneAdapterFailsClosedWhenItReachesTheCatalogDispatcher(t *testing.T) {
	registry := catalogruntime.NewAdapterRegistry()
	for _, registration := range NewDelegatedPlaneRegistrations() {
		require.NoError(t, registry.Register(registration))
	}

	adapter, err := registry.Resolve(PTZPresetCallAdapterKey, DelegatedPlaneContractVersion)
	require.NoError(t, err)
	_, err = adapter.Execute(context.Background(), catalogruntime.Invocation{Scope: "ptz:preset:call", Method: "POST"})
	require.ErrorIs(t, err, ErrDelegatedPlane)
	require.Contains(t, err.Error(), "ptz:ptz:preset:call")
}

// The registry is the gate publication runs through: a draft that names a
// delegated PTZ scope is only publishable because this adapter exists.
func TestDelegatedPlaneAdapterMakesTheScopesPublishable(t *testing.T) {
	registry := catalogruntime.NewAdapterRegistry()
	for _, registration := range NewDelegatedPlaneRegistrations() {
		require.NoError(t, registry.Register(registration))
	}
	catalog := catalogruntime.NewCatalogRuntime(registry, nil)
	_, report, err := catalog.Publish(context.Background(), catalogruntime.Draft{Operations: []catalogruntime.Operation{
		{
			Scope: "ptz:preset:call", Name: "调用预置位", Method: "POST",
			ExternalPath:    "/openapi/v1/devices/{deviceId}/channels/{channelId}/ptz/presets/{presetId}/call",
			AdapterKey:      PTZPresetCallAdapterKey,
			ContractVersion: DelegatedPlaneContractVersion,
			Risk:            "control",
		},
	}})
	require.NoError(t, err)
	require.True(t, report.Valid())
	active, err := catalog.Resolve("POST", "/openapi/v1/devices/34020000002000000001/channels/34020000001320000001/ptz/presets/1/call")
	require.NoError(t, err)
	require.Equal(t, "ptz:preset:call", active.Scope)
}

func TestDelegatedBindingsExecuteOnlyTheCodeRegisteredAdapter(t *testing.T) {
	calls := 0
	callback := testDelegatedAdapter(func(_ context.Context, invocation catalogruntime.Invocation) (any, error) {
		calls++
		return invocation.Value, nil
	})
	registry := catalogruntime.NewAdapterRegistry()
	for _, registration := range NewDelegatedPlaneRegistrations(map[string]catalogruntime.Adapter{PlayLiveAdapterKey: callback}) {
		require.NoError(t, registry.Register(registration))
	}
	adapter, err := registry.Resolve(PlayLiveAdapterKey, "v1")
	require.NoError(t, err)
	value, err := adapter.Execute(context.Background(), catalogruntime.Invocation{Scope: "play:live", Value: "owned-session"})
	require.NoError(t, err)
	require.Equal(t, "owned-session", value)
	require.Equal(t, 1, calls)
	unbound, err := registry.Resolve(PTZPresetCallAdapterKey, "v1")
	require.NoError(t, err)
	_, err = unbound.Execute(context.Background(), catalogruntime.Invocation{Scope: "ptz:preset:call"})
	require.ErrorIs(t, err, ErrDelegatedPlane)
}

type testDelegatedAdapter func(context.Context, catalogruntime.Invocation) (any, error)

func (f testDelegatedAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	return f(ctx, invocation)
}
