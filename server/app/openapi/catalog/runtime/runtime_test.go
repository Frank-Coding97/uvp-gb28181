package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordingAdapter struct {
	mu    sync.Mutex
	count int
	value any
}

func (a *recordingAdapter) Execute(_ context.Context, _ Invocation) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.count++
	return a.value, nil
}

type sysAPIResolverFunc func(context.Context, string, string) (SysAPIAsset, error)

func (f sysAPIResolverFunc) Resolve(ctx context.Context, path, method string) (SysAPIAsset, error) {
	return f(ctx, path, method)
}

func TestAdapterRegistryRequiresControlledKeyAndContractVersion(t *testing.T) {
	registry := NewAdapterRegistry()
	adapter := &recordingAdapter{value: "ok"}

	require.NoError(t, registry.Register(AdapterRegistration{Key: "device.list", ContractVersion: "v1", Adapter: adapter}))
	require.ErrorIs(t, registry.Register(AdapterRegistration{Key: "device.list", ContractVersion: "v1", Adapter: adapter}), ErrAdapterAlreadyRegistered)
	require.NoError(t, registry.Register(AdapterRegistration{Key: "device.list", ContractVersion: "v2", Adapter: adapter}))

	resolved, err := registry.Resolve("device.list", "v1")
	require.NoError(t, err)
	require.Same(t, adapter, resolved)
	_, err = registry.Resolve("device.list", "v3")
	require.ErrorIs(t, err, ErrAdapterNotFound)
}

func TestPublishBuildsImmutableSnapshotAndKeepsSysAPIDriftAsWarning(t *testing.T) {
	registry := NewAdapterRegistry()
	adapter := &recordingAdapter{value: "listed"}
	require.NoError(t, registry.Register(AdapterRegistration{Key: "device.list", ContractVersion: "v1", Adapter: adapter}))
	runtime := NewCatalogRuntime(registry, sysAPIResolverFunc(func(context.Context, string, string) (SysAPIAsset, error) {
		return SysAPIAsset{Path: "/api/gb28181/changed", Method: "POST"}, nil
	}))

	release, report, err := runtime.Publish(context.Background(), Draft{Version: "2026.09.22", Operations: []Operation{
		{Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list", ContractVersion: "v1", SysAPIPath: "/api/gb28181/device-mgmt/devices", SysAPIMethod: "GET"},
	}})
	require.NoError(t, err)
	require.True(t, report.Valid())
	require.Len(t, report.Warnings, 1)
	require.Equal(t, IssueCodeSysAPIDrift, report.Warnings[0].Code)
	require.NotEmpty(t, release.ID)
	require.Equal(t, "2026.09.22", release.Version)

	operation, ok := release.Operation("device:list")
	require.True(t, ok)
	require.Equal(t, "device.list", operation.AdapterKey)
	operations := release.Operations()
	operations[0].Scope = "tampered"
	operation, ok = release.Operation("device:list")
	require.True(t, ok, "returned operation slice must not mutate the snapshot")
	require.Equal(t, "device:list", operation.Scope)

	value, err := runtime.Dispatch(context.Background(), "device:list", Invocation{Method: "GET", Path: "/openapi/v1/devices"})
	require.NoError(t, err)
	require.Equal(t, "listed", value)
}

func TestPublishIsAtomicAndRollbackRestoresPreviousSnapshot(t *testing.T) {
	registry := NewAdapterRegistry()
	require.NoError(t, registry.Register(AdapterRegistration{Key: "device.list", ContractVersion: "v1", Adapter: &recordingAdapter{value: "v1"}}))
	runtime := NewCatalogRuntime(registry, nil)
	first, _, err := runtime.Publish(context.Background(), Draft{Version: "one", Operations: []Operation{{
		Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list", ContractVersion: "v1",
	}}})
	require.NoError(t, err)

	_, report, err := runtime.Publish(context.Background(), Draft{Version: "broken", Operations: []Operation{{
		Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "missing", ContractVersion: "v1",
	}}})
	require.ErrorIs(t, err, ErrDraftInvalid)
	require.False(t, report.Valid())
	active := runtime.Active()
	require.NotNil(t, active)
	require.Equal(t, first.ID, active.ID, "invalid publication must not replace the active snapshot")

	second, _, err := runtime.Publish(context.Background(), Draft{Version: "two", Operations: []Operation{{
		Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list", ContractVersion: "v1",
	}}})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	restored, err := runtime.Rollback()
	require.NoError(t, err)
	require.Equal(t, first.ID, restored.ID)
	require.Equal(t, first.ID, runtime.Active().ID)
}

func TestRuntimeBindsDurableIdentityAndInvalidatesItOnInMemoryMutation(t *testing.T) {
	registry := NewAdapterRegistry()
	adapter := &recordingAdapter{value: "ok"}
	require.NoError(t, registry.Register(AdapterRegistration{Key: "device.list", ContractVersion: "v1", Adapter: adapter}))
	runtime := NewCatalogRuntime(registry, nil)
	first, _, err := runtime.Publish(context.Background(), Draft{Version: "one", Operations: []Operation{{
		Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list", ContractVersion: "v1",
	}}})
	require.NoError(t, err)

	identity := RuntimeIdentity{ReleaseID: 42, Version: 7, SnapshotHash: "0123456789abcdef", RuntimeEpoch: 3}
	require.NoError(t, runtime.BindIdentity(identity))
	bound, ok := runtime.Identity()
	require.True(t, ok)
	require.Equal(t, identity, bound)

	second, _, err := runtime.Publish(context.Background(), Draft{Version: "two", Operations: []Operation{{
		Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list", ContractVersion: "v1",
	}}})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	_, ok = runtime.Identity()
	require.False(t, ok, "an in-memory publication must not retain the durable identity")

	require.NoError(t, runtime.BindIdentity(identity))
	_, err = runtime.Rollback()
	require.NoError(t, err)
	_, ok = runtime.Identity()
	require.False(t, ok, "in-memory rollback is not a durable runtime transition")
}

func TestScopeTombstonesCannotBeReusedAndOrphansDoNotBreakRuntime(t *testing.T) {
	registry := NewAdapterRegistry()
	require.NoError(t, registry.Register(AdapterRegistration{Key: "device.list", ContractVersion: "v1", Adapter: &recordingAdapter{value: "ok"}}))
	runtime := NewCatalogRuntime(registry, nil)
	_, _, err := runtime.Publish(context.Background(), Draft{Version: "one", Operations: []Operation{{
		Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list", ContractVersion: "v1",
	}}})
	require.NoError(t, err)
	_, report, err := runtime.Publish(context.Background(), Draft{Version: "two", OrphanScopes: []string{"legacy:scope"}})
	require.NoError(t, err)
	require.True(t, report.Valid())
	require.Contains(t, report.Warnings, ValidationIssue{Code: IssueCodeOrphanScope, Scope: "legacy:scope", Severity: SeverityWarning, Message: "scope is retained as an orphan and is not dispatchable"})
	require.Equal(t, ScopeTombstoned, runtime.ScopeStatus("device:list"))
	require.Equal(t, ScopeOrphan, runtime.ScopeStatus("legacy:scope"))

	_, report, err = runtime.Publish(context.Background(), Draft{Version: "three", Operations: []Operation{{
		Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list", ContractVersion: "v1",
	}}})
	require.ErrorIs(t, err, ErrDraftInvalid)
	require.Contains(t, report.Errors, ValidationIssue{Code: IssueCodeScopeTombstoned, Scope: "device:list", Severity: SeverityError, Message: "scope has been tombstoned and cannot be reused"})

	_, err = runtime.Dispatch(context.Background(), "legacy:scope", Invocation{})
	require.ErrorIs(t, err, ErrScopeNotDispatchable)
}

func TestSysAPIDriftDoesNotTurnUnrelatedDispatchIntoUnavailable(t *testing.T) {
	registry := NewAdapterRegistry()
	adapter := &recordingAdapter{value: "healthy"}
	require.NoError(t, registry.Register(AdapterRegistration{Key: "device.list", ContractVersion: "v1", Adapter: adapter}))
	runtime := NewCatalogRuntime(registry, sysAPIResolverFunc(func(context.Context, string, string) (SysAPIAsset, error) {
		return SysAPIAsset{}, errors.New("sys_api lookup failed")
	}))
	_, report, err := runtime.Publish(context.Background(), Draft{Version: "one", Operations: []Operation{{
		Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list", ContractVersion: "v1", SysAPIPath: "/api/gb28181/device-mgmt/devices", SysAPIMethod: "GET",
	}}})
	require.NoError(t, err)
	require.True(t, report.Valid())
	require.Len(t, report.Warnings, 1)

	value, err := runtime.Dispatch(context.Background(), "device:list", Invocation{Method: "GET", Path: "/openapi/v1/devices"})
	require.NoError(t, err)
	require.Equal(t, "healthy", value)
}

func TestRuntimeTreatsNilContextAsBackgroundContext(t *testing.T) {
	registry := NewAdapterRegistry()
	adapter := contextCheckingAdapter{}
	require.NoError(t, registry.Register(AdapterRegistration{Key: "device.list", ContractVersion: "v1", Adapter: adapter}))
	runtime := NewCatalogRuntime(registry, sysAPIResolverFunc(func(ctx context.Context, _ string, _ string) (SysAPIAsset, error) {
		if ctx == nil {
			return SysAPIAsset{}, errors.New("nil context")
		}
		select {
		case <-ctx.Done():
		default:
		}
		return SysAPIAsset{}, nil
	}))

	_, report, err := runtime.Publish(nil, Draft{Operations: []Operation{{
		Scope: "device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list", ContractVersion: "v1", SysAPIPath: "/api/gb28181/device-mgmt/devices", SysAPIMethod: "GET",
	}}})
	require.NoError(t, err)
	require.True(t, report.Valid())
	value, err := runtime.Dispatch(nil, "device:list", Invocation{})
	require.NoError(t, err)
	require.Equal(t, "context-ok", value)
}

func TestResolveMatchesConcretePathAgainstTemplateAndExtractsParameters(t *testing.T) {
	registry := NewAdapterRegistry()
	require.NoError(t, registry.Register(AdapterRegistration{Key: "device.detail", ContractVersion: "v1", Adapter: contextCheckingAdapter{}}))
	runtime := NewCatalogRuntime(registry, nil)
	_, _, err := runtime.Publish(context.Background(), Draft{Operations: []Operation{{
		Scope: "device:detail", Method: "GET", ExternalPath: "/openapi/v1/devices/{deviceId}", AdapterKey: "device.detail", ContractVersion: "v1",
	}}})
	require.NoError(t, err)

	operation, params, err := runtime.Match("GET", "/openapi/v1/devices/34020000002000000011")
	require.NoError(t, err)
	require.Equal(t, "device:detail", operation.Scope)
	require.Equal(t, map[string]string{"deviceId": "34020000002000000011"}, params)

	resolved, err := runtime.Resolve("GET", "/openapi/v1/devices/34020000002000000011")
	require.NoError(t, err)
	require.Equal(t, operation.Scope, resolved.Scope)
	_, _, err = runtime.Match("GET", "/openapi/v1/devices")
	require.ErrorIs(t, err, ErrRouteNotFound)
}

func TestPublishRejectsStaticAndTemplateRouteOverlap(t *testing.T) {
	registry := NewAdapterRegistry()
	adapter := contextCheckingAdapter{}
	require.NoError(t, registry.Register(AdapterRegistration{Key: "device.route", ContractVersion: "v1", Adapter: adapter}))
	runtime := NewCatalogRuntime(registry, nil)
	_, report, err := runtime.Publish(context.Background(), Draft{Operations: []Operation{
		{Scope: "device:detail", Method: "GET", ExternalPath: "/openapi/v1/devices/{deviceId}", AdapterKey: "device.route", ContractVersion: "v1"},
		{Scope: "device:health", Method: "GET", ExternalPath: "/openapi/v1/devices/health", AdapterKey: "device.route", ContractVersion: "v1"},
	}})
	require.ErrorIs(t, err, ErrDraftInvalid)
	require.Contains(t, report.Errors, ValidationIssue{Code: IssueCodeDuplicateRoute, Scope: "device:health", Severity: SeverityError, Message: "method and external path overlap another declared route"})
}

func TestPublishRejectsUppercaseScope(t *testing.T) {
	registry := NewAdapterRegistry()
	require.NoError(t, registry.Register(AdapterRegistration{Key: "device.list", ContractVersion: "v1", Adapter: contextCheckingAdapter{}}))
	runtime := NewCatalogRuntime(registry, nil)

	_, report, err := runtime.Publish(context.Background(), Draft{Operations: []Operation{{
		Scope: "Device:list", Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list", ContractVersion: "v1",
	}}})
	require.ErrorIs(t, err, ErrDraftInvalid)
	require.Contains(t, report.Errors, ValidationIssue{Code: IssueCodeInvalidScope, Scope: "Device:list", Severity: SeverityError, Message: "scope is invalid"})
}

func TestPublishRejectsScopeLongerThanPersistedScopeColumn(t *testing.T) {
	registry := NewAdapterRegistry()
	require.NoError(t, registry.Register(AdapterRegistration{Key: "device.list", ContractVersion: "v1", Adapter: contextCheckingAdapter{}}))
	runtime := NewCatalogRuntime(registry, nil)
	scope := strings.Repeat("a", 65)

	_, report, err := runtime.Publish(context.Background(), Draft{Operations: []Operation{{
		Scope: scope, Method: "GET", ExternalPath: "/openapi/v1/devices", AdapterKey: "device.list", ContractVersion: "v1",
	}}})
	require.ErrorIs(t, err, ErrDraftInvalid)
	require.Contains(t, report.Errors, ValidationIssue{Code: IssueCodeInvalidScope, Scope: scope, Severity: SeverityError, Message: "scope is invalid"})
}

type contextCheckingAdapter struct{}

func (contextCheckingAdapter) Execute(ctx context.Context, _ Invocation) (any, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	return "context-ok", nil
}
