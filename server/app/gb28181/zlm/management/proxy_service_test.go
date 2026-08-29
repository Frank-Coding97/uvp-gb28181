package management

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

func TestPullProxyValidatesProtocolAndRedactsURL(t *testing.T) {
	const sourceURL = "rtsps://user:p%40ss@example.com:8554/live/camera%2F1?token=secret-token&x=%E4%B8%AD"
	executor := &proxyExecutorFake{addPullResult: &zlm.ProxyCreateResult{Key: "pull-key"}}
	ledger := &proxyLedgerFake{}
	service := newProxyTestService(executor, ledger, nil, proxyCapabilitiesForAll())

	view, err := service.CreatePullProxy(context.Background(), 42, PullProxyRequest{
		NodeID:     7,
		Media:      MediaIdentity{Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "camera/1"},
		SourceURL:  sourceURL,
		RetryCount: 2,
		TimeoutSec: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, view)
	require.Equal(t, "pull-key", view.Key)
	require.Equal(t, ProxyKindPull, view.Kind)
	require.Equal(t, uint64(42), view.CreatedBy)
	require.Equal(t, "rtsps://example.com:8554", view.Source.Display)
	require.Equal(t, "example.com", view.Source.Host)
	require.Equal(t, 8554, view.Source.Port)
	require.True(t, view.Source.HasUserInfo)
	require.True(t, view.Source.HasSensitiveQuery)
	require.Len(t, view.Source.Fingerprint, 64)
	for _, secret := range []string{"user", "p%40ss", "secret-token", "camera%2F1", sourceURL} {
		require.NotContains(t, view.Source.Display, secret)
	}
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	for _, secret := range []string{"user", "p%40ss", "secret-token", sourceURL} {
		require.NotContains(t, string(encoded), secret)
	}
	require.Equal(t, sourceURL, executor.addPullRequest.URL)
	require.Equal(t, []string{"zlm:add-pull", "ledger:register"}, ledger.events)

	_, err = service.CreatePullProxy(context.Background(), 42, PullProxyRequest{
		NodeID:    7,
		Media:     MediaIdentity{Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "camera/1"},
		SourceURL: "ftp://example.com/live",
	})
	require.Error(t, err)
	validationErr := &ValidationError{}
	require.ErrorAs(t, err, &validationErr)
	require.Equal(t, CodeInvalidRequest, validationErr.Code)
	require.Equal(t, 1, executor.addPullCalls)
}

func TestPushProxyValidatesIdentityAndTargetFormat(t *testing.T) {
	executor := &proxyExecutorFake{addPushResult: &zlm.ProxyCreateResult{Key: "push-key"}}
	ledger := &proxyLedgerFake{}
	service := newProxyTestService(executor, ledger, nil, proxyCapabilitiesForAll())
	req := PushProxyRequest{
		NodeID:    7,
		Media:     MediaIdentity{Schema: "rtmp", Vhost: "__defaultVhost__", App: "live", Stream: "camera/1"},
		TargetURL: "rtmps://user:pass@example.net:443/live/camera?token=third-party",
	}
	view, err := service.CreatePushProxy(context.Background(), 8, req)
	require.NoError(t, err)
	require.NotNil(t, view)
	require.Equal(t, ProxyKindPush, view.Kind)
	require.Equal(t, "rtmps://example.net:443", view.Target.Display)
	require.Equal(t, "rtmp", executor.addPushRequest.Schema)
	require.Equal(t, req.TargetURL, executor.addPushRequest.DstURL)

	bad := req
	bad.TargetURL = "https://example.net/live/camera"
	_, err = service.CreatePushProxy(context.Background(), 8, bad)
	require.Error(t, err)
	require.Equal(t, CodeInvalidRequest, validationCode(err))

	bad = req
	bad.Media.Schema = "ftp"
	_, err = service.CreatePushProxy(context.Background(), 8, bad)
	require.Error(t, err)
	require.Equal(t, CodeInvalidRequest, validationCode(err))
	require.Equal(t, 1, executor.addPushCalls)
}

func TestProxyCapabilityUnsupportedReturns422AndUnknownAttemptsOnce(t *testing.T) {
	executor := &proxyExecutorFake{addPullResult: &zlm.ProxyCreateResult{Key: "unknown-key"}}
	ledger := &proxyLedgerFake{}
	unsupported := newProxyTestService(executor, ledger, nil, proxyCapabilitiesWithAPIs([]string{"listStreamProxy"}))
	_, err := unsupported.CreatePullProxy(context.Background(), 1, validPullRequest())
	require.Equal(t, CodeUnsupportedCapability, managementCode(err))
	require.Equal(t, 0, executor.addPullCalls)
	require.Empty(t, ledger.registrations)

	executor = &proxyExecutorFake{addPullResult: &zlm.ProxyCreateResult{Key: "unknown-key"}}
	ledger = &proxyLedgerFake{}
	unknown := newProxyTestService(executor, ledger, nil, proxyCapabilityFake{err: errors.New("probe unavailable")})
	view, err := unknown.CreatePullProxy(context.Background(), 1, validPullRequest())
	require.NoError(t, err)
	require.Equal(t, zlm.CapabilityUnknown, view.Capability)
	require.Equal(t, 1, executor.addPullCalls)
}

func TestProxyLedgerIsWrittenOnlyAfterExplicitZLMSuccess(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		key  string
	}{
		{name: "failure", err: errors.New("upstream failed")},
		{name: "timeout", err: context.DeadlineExceeded},
		{name: "missing key", key: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &proxyExecutorFake{addPullResult: &zlm.ProxyCreateResult{Key: test.key}, addPullErr: test.err}
			ledger := &proxyLedgerFake{}
			service := newProxyTestService(executor, ledger, nil, proxyCapabilitiesForAll())
			_, err := service.CreatePullProxy(context.Background(), 1, validPullRequest())
			require.Error(t, err)
			require.Empty(t, ledger.registrations)
		})
	}

	executor := &proxyExecutorFake{addPullResult: &zlm.ProxyCreateResult{Key: "success-key"}}
	ledger := &proxyLedgerFake{}
	service := newProxyTestService(executor, ledger, nil, proxyCapabilitiesForAll())
	_, err := service.CreatePullProxy(context.Background(), 1, validPullRequest())
	require.NoError(t, err)
	require.Len(t, ledger.registrations, 1)
	require.Equal(t, "success-key", ledger.registrations[0].Identity.ResourceKey)
	require.Equal(t, "rtsp", ledger.registrations[0].Identity.Schema)
	require.Equal(t, "__defaultVhost__", ledger.registrations[0].Identity.Vhost)
	require.Len(t, ledger.registrations[0].Fingerprint, 64)
}

func TestProxyLedgerFailureCompensatesSuccessfulZLMCreate(t *testing.T) {
	const sourceURL = "rtsp://user:pass@example.com/live/camera?token=ledger-failure"
	executor := &proxyExecutorFake{
		addPullResult:    &zlm.ProxyCreateResult{Key: "created-key"},
		deletePullResult: &zlm.ProxyDeleteResult{Key: "created-key", Hit: true},
	}
	ledger := &proxyLedgerFake{registerErr: errors.New("database failed for " + sourceURL)}
	service := newProxyTestService(executor, ledger, nil, proxyCapabilitiesForAll())
	request := validPullRequest()
	request.SourceURL = sourceURL
	_, err := service.CreatePullProxy(context.Background(), 1, request)
	require.Equal(t, CodeInternal, managementCode(err))
	require.Equal(t, 1, executor.addPullCalls)
	require.Equal(t, 1, executor.deletePullCalls)
	require.Empty(t, ledger.registrations)
	require.NotContains(t, err.Error(), sourceURL)
	require.NotContains(t, err.Error(), "user")
	require.NotContains(t, err.Error(), "ledger-failure")
	require.Equal(t, []string{"zlm:add-pull", "ledger:register", "zlm:delete-pull"}, ledger.events)

	executor.deletePullResult = &zlm.ProxyDeleteResult{Key: "different-key", Hit: true}
	executor.deletePullErr = nil
	ledger.events = nil
	_, err = service.CreatePullProxy(context.Background(), 1, request)
	require.Equal(t, CodeInternal, managementCode(err))
	require.Equal(t, 2, executor.addPullCalls)
	require.Equal(t, 2, executor.deletePullCalls)
	require.NotContains(t, err.Error(), sourceURL)
	require.Contains(t, err.Error(), "uncertain")
	require.Equal(t, []string{"zlm:add-pull", "ledger:register", "zlm:delete-pull"}, ledger.events)

	executor.deletePullResult = nil
	executor.deletePullErr = errors.New("cleanup failed for " + sourceURL)
	ledger.events = nil
	_, err = service.CreatePullProxy(context.Background(), 1, request)
	require.Equal(t, CodeInternal, managementCode(err))
	require.Equal(t, 3, executor.addPullCalls)
	require.Equal(t, 3, executor.deletePullCalls)
	require.NotContains(t, err.Error(), sourceURL)
	require.Contains(t, err.Error(), "uncertain")
	require.Equal(t, []string{"zlm:add-pull", "ledger:register", "zlm:delete-pull"}, ledger.events)
}

func TestProxyDeleteUsesPreflightAndTombstonesOnlyOnSuccessOrAbsent(t *testing.T) {
	executor := &proxyExecutorFake{deletePullResult: &zlm.ProxyDeleteResult{Key: "pull-key", Hit: true}}
	ledger := &proxyLedgerFake{rows: []gbmodels.GbZLMManagedResource{{NodeID: 7, ResourceType: string(ProxyKindPull), ResourceKey: "pull-key", Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "camera/1"}}}
	ownership := &proxyOwnershipFake{}
	service := newProxyTestService(executor, ledger, ownership, proxyCapabilitiesForAll())
	req := validDeleteRequest()
	result, err := service.DeletePullProxy(context.Background(), 1, req)
	require.NoError(t, err)
	require.True(t, result.Removed)
	require.False(t, result.AlreadyAbsent)
	require.True(t, result.Tombstoned)
	require.Equal(t, 1, ownership.preflightCalls)
	require.Equal(t, 1, ownership.executeCalls)
	require.Equal(t, []string{"zlm:delete-pull", "ledger:tombstone"}, ledger.events)

	executor.deletePullResult = &zlm.ProxyDeleteResult{Key: "pull-key", Hit: false}
	ledger.events = nil
	result, err = service.DeletePullProxy(context.Background(), 1, req)
	require.NoError(t, err)
	require.False(t, result.Removed)
	require.True(t, result.AlreadyAbsent)
	require.True(t, result.Tombstoned)
	require.Equal(t, []string{"zlm:delete-pull", "ledger:tombstone"}, ledger.events)

	ownership.changed = true
	ledger.events = nil
	_, err = service.DeletePullProxy(context.Background(), 1, req)
	require.Equal(t, CodeOwnershipConflict, managementCode(err))
	require.Empty(t, ledger.events)
	require.Equal(t, 2, executor.deletePullCalls)

	ownership.changed = false
	executor.deletePullErr = errors.New("delete failed")
	ledger.events = nil
	_, err = service.DeletePullProxy(context.Background(), 1, req)
	require.Equal(t, CodeInternal, managementCode(err))
	require.Equal(t, []string{"zlm:delete-pull"}, ledger.events)
}

func TestProxyDeleteRequiresExactActiveLedgerProvenance(t *testing.T) {
	executor := &proxyExecutorFake{deletePullResult: &zlm.ProxyDeleteResult{Key: "proxy-a", Hit: true}}
	ledger := &proxyLedgerFake{rows: []gbmodels.GbZLMManagedResource{
		{NodeID: 7, ResourceType: string(ProxyKindPull), ResourceKey: "proxy-a", Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "same-stream", IdentityFingerprint: strings.Repeat("a", 64)},
		{NodeID: 7, ResourceType: string(ProxyKindPull), ResourceKey: "proxy-b", Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "same-stream", IdentityFingerprint: strings.Repeat("b", 64)},
	}}
	service := newProxyTestService(executor, ledger, &proxyOwnershipFake{}, proxyCapabilitiesForAll())
	unknown := validDeleteRequest()
	unknown.Key = "proxy-missing"
	unknown.Media.Stream = "same-stream"
	_, err := service.DeletePullProxy(context.Background(), 1, unknown)
	require.Equal(t, CodeOwnershipConflict, managementCode(err))
	require.Equal(t, 0, executor.deletePullCalls)
	require.Empty(t, ledger.tombstones)

	known := unknown
	known.Key = "proxy-a"
	preview, err := service.PreviewDeletePullProxy(context.Background(), known)
	require.NoError(t, err)
	require.NotEmpty(t, preview.Fingerprint)
	require.NotEqual(t, preview.Fingerprint, "stable")
	known.Fingerprint = preview.Fingerprint
	result, err := service.DeletePullProxy(context.Background(), 1, known)
	require.NoError(t, err)
	require.True(t, result.Removed)
	require.Equal(t, 1, executor.deletePullCalls)
	require.Len(t, ledger.tombstones, 1)

	other := known
	other.Key = "proxy-b"
	other.Fingerprint = ""
	otherPreview, err := service.PreviewDeletePullProxy(context.Background(), other)
	require.NoError(t, err)
	require.NotEqual(t, preview.Fingerprint, otherPreview.Fingerprint)
}

func TestProxyListUsesZLMTruthAndRedactsRawURL(t *testing.T) {
	const secretURL = "rtsp://user:pass@example.com/live/camera?token=list-secret&x=1"
	executor := &proxyExecutorFake{listPull: []zlm.StreamProxyInfo{{
		Key: "live-key", URL: secretURL, Status: 1, StatusStr: "online", Src: &zlm.ProxyMediaTuple{VHost: "__defaultVhost__", App: "live", Stream: "camera/1"},
	}}}
	ledger := &proxyLedgerFake{rows: []gbmodels.GbZLMManagedResource{
		{NodeID: 7, ResourceType: string(ProxyKindPull), ResourceKey: "stale-key", App: "live", Stream: "stale"},
		{NodeID: 7, ResourceType: string(ProxyKindPull), ResourceKey: "live-key", Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "camera/1", Summary: "source " + secretURL, IdentityFingerprint: strings.Repeat("a", 64)},
	}}
	service := newProxyTestService(executor, ledger, nil, proxyCapabilitiesForAll())
	page, err := service.ListPullProxies(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, page.List, 1)
	require.Equal(t, "live-key", page.List[0].Key)
	require.True(t, page.List[0].Managed)
	require.Equal(t, "rtsp://example.com", page.List[0].Source.Display)
	require.NotContains(t, page.List[0].Source.Display, "/live/camera")
	require.NotContains(t, page.List[0].Source.Display, "list-secret")
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	for _, secret := range []string{"user", "pass", "list-secret", secretURL} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestProxyListRestoresSchemaOnlyFromUniqueLedgerProvenance(t *testing.T) {
	executor := &proxyExecutorFake{listPull: []zlm.StreamProxyInfo{{
		Key: "live-key", URL: "rtmp://source.example/live", Status: 1,
		Src: &zlm.ProxyMediaTuple{VHost: "__defaultVhost__", App: "live", Stream: "camera/1"},
	}}}
	ledger := &proxyLedgerFake{rows: []gbmodels.GbZLMManagedResource{{
		NodeID: 7, ResourceType: string(ProxyKindPull), ResourceKey: "live-key",
		Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "camera/1",
	}}}
	service := newProxyTestService(executor, ledger, nil, proxyCapabilitiesForAll())
	page, err := service.ListPullProxies(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, page.List, 1)
	require.True(t, page.List[0].Managed)
	require.Equal(t, "rtsp", page.List[0].Media.Schema, "schema must come from the unique exact ledger row, not the rtmp source URL")

	ledger.rows = append(ledger.rows, gbmodels.GbZLMManagedResource{
		NodeID: 7, ResourceType: string(ProxyKindPull), ResourceKey: "live-key",
		Schema: "rtmp", Vhost: "__defaultVhost__", App: "live", Stream: "camera/1",
	})
	page, err = service.ListPullProxies(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, page.List, 1)
	require.False(t, page.List[0].Managed, "same key/media with multiple schemas is ambiguous and must fail closed")
	require.Empty(t, page.List[0].Media.Schema)
}

func validPullRequest() PullProxyRequest {
	return PullProxyRequest{
		NodeID:    7,
		Media:     MediaIdentity{Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "camera/1"},
		SourceURL: "rtsp://example.com/live/camera",
	}
}

func validDeleteRequest() ProxyDeleteRequest {
	return ProxyDeleteRequest{
		NodeID: 7,
		Key:    "pull-key",
		Media:  validPullRequest().Media,
	}
}

func newProxyTestService(executor ProxyExecutor, ledger ProxyLedger, ownership ProxyOwnership, capability ProxyCapabilityReader) *ProxyService {
	if fake, ok := executor.(*proxyExecutorFake); ok {
		if ledgerFake, ok := ledger.(*proxyLedgerFake); ok {
			fake.events = &ledgerFake.events
		}
	}
	return NewProxyService(ProxyDependencies{
		Executor:   executor,
		Ledger:     ledger,
		Ownership:  ownership,
		Capability: capability,
	}, WithProxyOperationTimeout(time.Second))
}

func proxyCapabilitiesForAll() ProxyCapabilityReader {
	return proxyCapabilitiesWithAPIs([]string{
		"addStreamProxy", "listStreamProxy", "delStreamProxy",
		"addStreamPusherProxy", "listStreamPusherProxy", "delStreamPusherProxy",
	})
}

func proxyCapabilitiesWithAPIs(apis []string) ProxyCapabilityReader {
	return proxyCapabilityFake{profile: zlm.CapabilityProfile{APIs: apis}}
}

func validationCode(err error) ManagementErrorCode {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) && validationErr != nil {
		return validationErr.Code
	}
	return managementCode(err)
}

func managementCode(err error) ManagementErrorCode {
	var managementErr *ManagementError
	if errors.As(err, &managementErr) && managementErr != nil {
		return managementErr.Code
	}
	return ""
}

type proxyExecutorFake struct {
	mu sync.Mutex

	addPullRequest   zlm.StreamProxyRequest
	addPushRequest   zlm.StreamPusherProxyRequest
	addPullResult    *zlm.ProxyCreateResult
	addPushResult    *zlm.ProxyCreateResult
	addPullErr       error
	addPushErr       error
	addPullCalls     int
	addPushCalls     int
	deletePullResult *zlm.ProxyDeleteResult
	deletePushResult *zlm.ProxyDeleteResult
	deletePullErr    error
	deletePushErr    error
	deletePullCalls  int
	deletePushCalls  int
	listPull         []zlm.StreamProxyInfo
	listPush         []zlm.StreamPusherProxyInfo
	events           *[]string
}

func (f *proxyExecutorFake) AddPull(_ context.Context, _ int64, request zlm.StreamProxyRequest) (*zlm.ProxyCreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addPullCalls++
	f.addPullRequest = request
	if f.events != nil {
		*f.events = append(*f.events, "zlm:add-pull")
	}
	return f.addPullResult, f.addPullErr
}

func (f *proxyExecutorFake) AddPush(_ context.Context, _ int64, request zlm.StreamPusherProxyRequest) (*zlm.ProxyCreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addPushCalls++
	f.addPushRequest = request
	if f.events != nil {
		*f.events = append(*f.events, "zlm:add-push")
	}
	return f.addPushResult, f.addPushErr
}

func (f *proxyExecutorFake) ListPull(context.Context, int64, ...string) ([]zlm.StreamProxyInfo, error) {
	return append([]zlm.StreamProxyInfo(nil), f.listPull...), nil
}

func (f *proxyExecutorFake) ListPush(context.Context, int64, ...string) ([]zlm.StreamPusherProxyInfo, error) {
	return append([]zlm.StreamPusherProxyInfo(nil), f.listPush...), nil
}

func (f *proxyExecutorFake) DeletePull(_ context.Context, _ int64, _ string) (*zlm.ProxyDeleteResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletePullCalls++
	if f.events != nil {
		*f.events = append(*f.events, "zlm:delete-pull")
	}
	return f.deletePullResult, f.deletePullErr
}

func (f *proxyExecutorFake) DeletePush(_ context.Context, _ int64, _ string) (*zlm.ProxyDeleteResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletePushCalls++
	if f.events != nil {
		*f.events = append(*f.events, "zlm:delete-push")
	}
	return f.deletePushResult, f.deletePushErr
}

type proxyLedgerFake struct {
	mu            sync.Mutex
	rows          []gbmodels.GbZLMManagedResource
	registrations []repo.ManagedResourceRegistration
	tombstones    []repo.ManagedResourceIdentity
	events        []string
	registerErr   error
	tombstoneErr  error
}

func (f *proxyLedgerFake) Register(_ context.Context, input repo.ManagedResourceRegistration) (*gbmodels.GbZLMManagedResource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "ledger:register")
	if f.registerErr != nil {
		return nil, f.registerErr
	}
	f.registrations = append(f.registrations, input)
	return &gbmodels.GbZLMManagedResource{NodeID: input.Identity.NodeID, ResourceType: input.Identity.ResourceType, ResourceKey: input.Identity.ResourceKey, Schema: input.Identity.Schema, Vhost: input.Identity.Vhost, App: input.Identity.App, Stream: input.Identity.Stream, IdentityFingerprint: input.Fingerprint}, nil
}

func (f *proxyLedgerFake) List(_ context.Context, filter repo.ManagedResourceFilter) ([]gbmodels.GbZLMManagedResource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := make([]gbmodels.GbZLMManagedResource, 0, len(f.rows))
	for _, row := range f.rows {
		if filter.NodeID != 0 && row.NodeID != filter.NodeID {
			continue
		}
		if filter.ResourceType != "" && row.ResourceType != filter.ResourceType {
			continue
		}
		if !filter.IncludeTombstoned && row.TombstonedAt != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (f *proxyLedgerFake) Tombstone(_ context.Context, identity repo.ManagedResourceIdentity, _ time.Time) (*gbmodels.GbZLMManagedResource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "ledger:tombstone")
	if f.tombstoneErr != nil {
		return nil, f.tombstoneErr
	}
	f.tombstones = append(f.tombstones, identity)
	return &gbmodels.GbZLMManagedResource{NodeID: identity.NodeID, ResourceType: identity.ResourceType, ResourceKey: identity.ResourceKey, Schema: identity.Schema, Vhost: identity.Vhost, App: identity.App, Stream: identity.Stream}, nil
}

type proxyCapabilityFake struct {
	profile zlm.CapabilityProfile
	err     error
}

func (f proxyCapabilityFake) GetCapabilityProfile(context.Context, int64) (zlm.CapabilityProfile, error) {
	return f.profile, f.err
}

type proxyOwnershipFake struct {
	preflightCalls int
	executeCalls   int
	changed        bool
}

func (f *proxyOwnershipFake) Preflight(_ context.Context, target OwnershipTarget) (OwnershipPreflight, error) {
	f.preflightCalls++
	if f.changed {
		return OwnershipPreflight{Target: target, Snapshot: OwnershipSnapshot{Target: target, Status: OwnershipStatusOwned, PresenceKnown: true, Present: true}, Fingerprint: "changed"}, nil
	}
	return OwnershipPreflight{Target: target, Snapshot: OwnershipSnapshot{Target: target, Status: OwnershipStatusManaged, PresenceKnown: true, Present: true}, Fingerprint: "stable"}, nil
}

func (f *proxyOwnershipFake) Execute(_ context.Context, preflight OwnershipPreflight, action func(context.Context, OwnershipTarget) error) error {
	f.executeCalls++
	if f.changed || preflight.Fingerprint != "stable" {
		return NewOwnershipConflictError("7", "ownership fingerprint changed; reconfirm required")
	}
	return action(context.Background(), preflight.Target)
}
