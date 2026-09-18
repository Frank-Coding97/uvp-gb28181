package management

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

func TestRTPServiceValidatesFullOpenCombination(t *testing.T) {
	client := &t11RTPClient{}
	service := NewRTPService(RTPDependencies{Client: client, Ledger: &t11RTPLedger{}})
	base := RTPServerCreateRequest{VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1", Port: 0, TCPMode: 0, OnlyTrack: 2}

	cases := []struct {
		name   string
		mutate func(*RTPServerCreateRequest)
		field  string
	}{
		{name: "missing vhost", mutate: func(r *RTPServerCreateRequest) { r.VHost = "" }, field: "vhost"},
		{name: "missing app", mutate: func(r *RTPServerCreateRequest) { r.App = "" }, field: "app"},
		{name: "missing stream", mutate: func(r *RTPServerCreateRequest) { r.Stream = "" }, field: "stream"},
		{name: "invalid port", mutate: func(r *RTPServerCreateRequest) { r.Port = 65536 }, field: "port"},
		{name: "invalid tcp mode", mutate: func(r *RTPServerCreateRequest) { r.TCPMode = 3 }, field: "tcpMode"},
		{name: "invalid only track", mutate: func(r *RTPServerCreateRequest) { r.OnlyTrack = 3 }, field: "onlyTrack"},
		{name: "invalid ssrc", mutate: func(r *RTPServerCreateRequest) { r.SSRC = "ssrc;rm" }, field: "ssrc"},
		{name: "invalid local ip", mutate: func(r *RTPServerCreateRequest) { r.LocalIP = "not-an-ip" }, field: "localIp"},
		{name: "reuse needs explicit port", mutate: func(r *RTPServerCreateRequest) { r.Reuse = true }, field: "reuse"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			request := base
			tt.mutate(&request)
			_, err := service.Create(context.Background(), 7, 1, request)
			assertT11ValidationField(t, err, tt.field)
		})
	}
	require.Zero(t, client.openCalls)
}

func TestRTPServiceRejectsUntrustedActor(t *testing.T) {
	client := &t11RTPClient{}
	service := NewRTPService(RTPDependencies{Client: client, Ledger: &t11RTPLedger{}})
	_, err := service.Create(context.Background(), 7, 0, RTPServerCreateRequest{
		VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1", Port: 41000,
	})
	assertT11ValidationField(t, err, "actorUserId")
	require.Zero(t, client.openCalls)
}

func TestRTPServicePortZeroRegistersActualPortAndSafeDTO(t *testing.T) {
	client := &t11RTPClient{openResult: &RTPServerOpenResult{Key: "stream-1", Port: 41000}}
	ledger := &t11RTPLedger{}
	service := NewRTPService(RTPDependencies{Client: client, Ledger: ledger})
	request := RTPServerCreateRequest{
		VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1", Port: 0, TCPMode: 1,
		SSRC: "0105000005", OnlyTrack: 2, LocalIP: "192.0.2.10",
	}
	view, err := service.Create(context.Background(), 7, 99, request)
	require.NoError(t, err)
	require.Equal(t, 41000, view.Port)
	require.Equal(t, request, client.openRequest)
	require.Equal(t, "0105000005", view.SSRC)
	require.Equal(t, 1, view.TCPMode)
	require.Equal(t, 2, view.OnlyTrack)
	require.Len(t, ledger.registered, 1)
	require.Equal(t, "rtp", ledger.registered[0].Identity.Schema)
	require.Equal(t, request.VHost, ledger.registered[0].Identity.Vhost)
	require.Contains(t, ledger.registered[0].Summary, "port=41000")
	require.NotEmpty(t, ledger.registered[0].Fingerprint, "ledger must retain a fingerprint containing the actual allocation identity")
	encoded, marshalErr := json.Marshal(view)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(encoded), "secret")
}

func TestRTPServiceRegistrationFailureCompensatesTypedResource(t *testing.T) {
	client := &t11RTPClient{openResult: &RTPServerOpenResult{Key: "stream-rollback", Port: 41000}}
	ledger := &t11RTPLedger{registerErr: errors.New("ledger unavailable")}
	service := NewRTPService(RTPDependencies{Client: client, Ledger: ledger})
	request := RTPServerCreateRequest{VHost: "__defaultVhost__", App: "rtp", Stream: "stream-rollback", Port: 0}
	_, err := service.Create(context.Background(), 7, 42, request)
	require.Error(t, err)
	require.Equal(t, 1, client.closeCalls, "successful open must be compensated when registration fails")
	require.Equal(t, 1, ledger.registerCalls)
	require.NotContains(t, err.Error(), "ledger unavailable")

	client.closeErr = errors.New("rollback unavailable")
	_, err = service.Create(context.Background(), 7, 42, request)
	require.Error(t, err)
	require.Equal(t, CodeInternal, mustT11ManagementError(t, err).Code)
	require.Contains(t, err.Error(), "rollback")
}

func TestRTPServiceRegistrationFailureWithUnconfirmedRollbackIsOrphanUncertain(t *testing.T) {
	client := &t11RTPClient{
		openResult:  &RTPServerOpenResult{Key: "stream-rollback-uncertain", Port: 41000},
		closeResult: &RTPServerCloseResult{Stream: "stream-rollback-uncertain"},
	}
	ledger := &t11RTPLedger{registerErr: errors.New("ledger unavailable")}
	service := NewRTPService(RTPDependencies{Client: client, Ledger: ledger})
	request := RTPServerCreateRequest{VHost: "__defaultVhost__", App: "rtp", Stream: "stream-rollback-uncertain", Port: 0}

	_, err := service.Create(context.Background(), 7, 42, request)
	require.Error(t, err)
	require.Equal(t, CodeInternal, mustT11ManagementError(t, err).Code)
	require.Contains(t, err.Error(), "orphan")
	require.Contains(t, err.Error(), "rollback")
	require.Equal(t, 1, client.closeCalls)
	require.Equal(t, 1, ledger.registerCalls)
	require.Nil(t, ledger.rows)
}

func TestRTPServiceListPageIsBounded(t *testing.T) {
	list := make([]zlm.RtpServerInfo, MaxResponseItems+17)
	for i := range list {
		list[i] = zlm.RtpServerInfo{
			Key: "rtp-" + strconv.Itoa(i), VHost: "__defaultVhost__", App: "rtp",
			StreamID: "stream-" + strconv.Itoa(i), Port: 41000 + i%1000,
		}
	}
	service := NewRTPService(RTPDependencies{
		Client: &t11RTPClient{list: list}, Ledger: &t11RTPLedger{},
	})
	page, err := service.ListPage(context.Background(), 7, PageRequest{Page: 1, PageSize: MaxPageSize})
	require.NoError(t, err)
	require.Len(t, page.List, MaxPageSize)
	require.Equal(t, int64(MaxResponseItems), page.Total)
	require.True(t, page.Truncated)
}

func TestRTPServiceRejectsDuplicateStreamAndPortMappings(t *testing.T) {
	existing := []zlm.RtpServerInfo{{Key: "existing", VHost: "__defaultVhost__", App: "rtp", StreamID: "stream-existing", Port: 41001}}
	client := &t11RTPClient{list: existing}
	service := NewRTPService(RTPDependencies{Client: client, Ledger: &t11RTPLedger{}})

	_, err := service.Create(context.Background(), 7, 1, RTPServerCreateRequest{VHost: "__defaultVhost__", App: "rtp", Stream: "stream-existing", Port: 0})
	require.Equal(t, CodeOwnershipConflict, mustT11ManagementError(t, err).Code)
	require.Zero(t, client.openCalls)

	_, err = service.Create(context.Background(), 7, 1, RTPServerCreateRequest{VHost: "__defaultVhost__", App: "rtp", Stream: "stream-new", Port: 41001})
	require.Equal(t, CodeOwnershipConflict, mustT11ManagementError(t, err).Code)
	require.Zero(t, client.openCalls)
}

func TestRTPServiceNormalCloseRechecksOwnershipAndForceIsSeparate(t *testing.T) {
	key := rtpLedgerKey(7, "stream-1")
	ledger := &t11RTPLedger{rows: map[string]*gbmodels.GbZLMManagedResource{
		key: {NodeID: 7, ResourceType: ResourceTypeRTPServer, ResourceKey: "stream-1", Schema: "rtp", Vhost: "__defaultVhost__", App: "rtp", Stream: "stream-1"},
	}}
	client := &t11RTPClient{list: []zlm.RtpServerInfo{{Key: "stream-1", VHost: "__defaultVhost__", App: "rtp", StreamID: "stream-1", Port: 41000}}}
	presence := &t11MutablePresence{present: true}
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: presence,
		Sources: []OwnershipSource{
			t11ManagedSource{resourceType: ResourceTypeRTPServer, key: "stream-1"},
		},
	})
	service := NewRTPService(RTPDependencies{Client: client, Ledger: ledger, Ownership: resolver})

	result, err := service.Close(context.Background(), RTPServerCloseRequest{NodeID: 7, VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1"})
	require.NoError(t, err)
	require.True(t, result.Released)
	require.False(t, result.AlreadyReleased)
	require.Equal(t, 1, client.closeCalls)
	require.NotNil(t, ledger.rows[key].TombstonedAt)

	// A second close is idempotent because the live list is absent and the
	// tombstoned ledger row retains the prior management provenance.
	client.list = nil
	result, err = service.Close(context.Background(), RTPServerCloseRequest{NodeID: 7, VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1"})
	require.NoError(t, err)
	require.True(t, result.AlreadyReleased)
	require.Equal(t, 1, client.closeCalls, "idempotent close must not call ZLM again")

	// Force follows its own method and may close a business-owned resource.
	client.list = []zlm.RtpServerInfo{{Key: "stream-1", VHost: "__defaultVhost__", App: "rtp", StreamID: "stream-1", Port: 41000}}
	ledger.rows[key].TombstonedAt = nil
	resolver = NewOwnershipResolver(OwnershipDependencies{
		Presence: presence,
		Sources:  []OwnershipSource{t11BusinessSource{kind: OwnershipTypeRealtimePlayback}},
	})
	service = NewRTPService(RTPDependencies{Client: client, Ledger: ledger, Ownership: resolver})
	_, err = service.Close(context.Background(), RTPServerCloseRequest{NodeID: 7, VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1"})
	require.Equal(t, CodeOwnershipConflict, mustT11ManagementError(t, err).Code)
	forceResult, err := service.ForceClose(context.Background(), RTPServerForceCloseRequest{NodeID: 7, VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1", Reason: "incident-42"})
	require.NoError(t, err)
	require.True(t, forceResult.Released)
	require.Equal(t, 2, client.closeCalls)

	_, err = service.ForceClose(context.Background(), RTPServerForceCloseRequest{NodeID: 7, VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1"})
	assertT11ValidationField(t, err, "reason")
}

func TestRTPServiceCloseFailureOrChangedFingerprintDoesNotTombstone(t *testing.T) {
	key := rtpLedgerKey(7, "stream-1")
	ledger := &t11RTPLedger{rows: map[string]*gbmodels.GbZLMManagedResource{
		key: {NodeID: 7, ResourceType: ResourceTypeRTPServer, ResourceKey: "stream-1", Schema: "rtp", Vhost: "__defaultVhost__", App: "rtp", Stream: "stream-1"},
	}}
	client := &t11RTPClient{
		list:     []zlm.RtpServerInfo{{Key: "stream-1", VHost: "__defaultVhost__", App: "rtp", StreamID: "stream-1", Port: 41000}},
		closeErr: errors.New("close failed"),
	}
	presence := &t11MutablePresence{present: true}
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: presence,
		Sources:  []OwnershipSource{t11ManagedSource{resourceType: ResourceTypeRTPServer, key: "stream-1"}},
	})
	service := NewRTPService(RTPDependencies{Client: client, Ledger: ledger, Ownership: resolver})
	request := RTPServerCloseRequest{NodeID: 7, VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1"}

	_, err := service.Close(context.Background(), request)
	require.Error(t, err)
	require.Equal(t, 1, client.closeCalls)
	require.Nil(t, ledger.rows[key].TombstonedAt)

	client.closeErr = nil
	preflight, err := service.PreflightClose(context.Background(), request)
	require.NoError(t, err)
	presence.present = false
	_, err = service.CloseWithPreflight(context.Background(), request, preflight)
	require.Equal(t, CodeOwnershipConflict, mustT11ManagementError(t, err).Code)
	require.Equal(t, 1, client.closeCalls, "changed ownership must not reach ZLM")
	require.Nil(t, ledger.rows[key].TombstonedAt)
}

func TestRTPServiceUnconfirmedCloseDoesNotTombstone(t *testing.T) {
	for _, force := range []bool{false, true} {
		name := "ordinary"
		if force {
			name = "force"
		}
		t.Run(name, func(t *testing.T) {
			stream := "stream-release-uncertain-" + name
			key := rtpLedgerKey(7, stream)
			ledger := &t11RTPLedger{rows: map[string]*gbmodels.GbZLMManagedResource{
				key: {NodeID: 7, ResourceType: ResourceTypeRTPServer, ResourceKey: stream, Schema: "rtp", Vhost: "__defaultVhost__", App: "rtp", Stream: stream},
			}}
			client := &t11RTPClient{
				list:        []zlm.RtpServerInfo{{Key: stream, VHost: "__defaultVhost__", App: "rtp", StreamID: stream, Port: 41000}},
				closeResult: &RTPServerCloseResult{Stream: stream},
			}
			presence := &t11MutablePresence{present: true}
			resolver := NewOwnershipResolver(OwnershipDependencies{
				Presence: presence,
				Sources:  []OwnershipSource{t11ManagedSource{resourceType: ResourceTypeRTPServer, key: stream}},
			})
			service := NewRTPService(RTPDependencies{Client: client, Ledger: ledger, Ownership: resolver})

			var err error
			if force {
				_, err = service.ForceClose(context.Background(), RTPServerForceCloseRequest{
					NodeID: 7, VHost: "__defaultVhost__", App: "rtp", Stream: stream, Reason: "incident-release-uncertain",
				})
			} else {
				_, err = service.Close(context.Background(), RTPServerCloseRequest{
					NodeID: 7, VHost: "__defaultVhost__", App: "rtp", Stream: stream,
				})
			}
			require.Error(t, err)
			require.Equal(t, CodeInternal, mustT11ManagementError(t, err).Code)
			require.Contains(t, err.Error(), "release")
			require.Equal(t, 1, client.closeCalls)
			require.Nil(t, ledger.rows[key].TombstonedAt)
		})
	}
}

func TestRTPServiceUnsupportedListIs422AndNeverEmptySuccess(t *testing.T) {
	client := &t11RTPClient{listErr: errors.New("listRtpServer code=-404 msg=api not found")}
	service := NewRTPService(RTPDependencies{Client: client, Ledger: &t11RTPLedger{}})
	items, err := service.List(context.Background(), 7)
	require.Error(t, err)
	require.Nil(t, items)
	managementErr := mustT11ManagementError(t, err)
	require.Equal(t, CodeUnsupportedCapability, managementErr.Code)
	require.Equal(t, 422, managementErr.HTTPStatus())
}

func mustT11ManagementError(t *testing.T, err error) *ManagementError {
	t.Helper()
	var managementErr *ManagementError
	require.ErrorAs(t, err, &managementErr)
	return managementErr
}

type t11RTPClient struct {
	list        []zlm.RtpServerInfo
	listErr     error
	openResult  *RTPServerOpenResult
	openErr     error
	closeResult *RTPServerCloseResult
	closeErr    error
	openRequest RTPServerCreateRequest
	openCalls   int
	closeCalls  int
}

func (c *t11RTPClient) ListRtpServers(context.Context, int64) ([]zlm.RtpServerInfo, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return append([]zlm.RtpServerInfo(nil), c.list...), nil
}

func (c *t11RTPClient) OpenRtpServer(_ context.Context, _ int64, request RTPServerCreateRequest) (*RTPServerOpenResult, error) {
	c.openCalls++
	c.openRequest = request
	if c.openErr != nil {
		return nil, c.openErr
	}
	if c.openResult != nil {
		return c.openResult, nil
	}
	return &RTPServerOpenResult{Key: request.Stream, Port: request.Port}, nil
}

func (c *t11RTPClient) CloseRtpServer(_ context.Context, _ int64, _ string, _ string, stream string) (*RTPServerCloseResult, error) {
	c.closeCalls++
	if c.closeErr != nil {
		return nil, c.closeErr
	}
	if c.closeResult != nil {
		return c.closeResult, nil
	}
	return &RTPServerCloseResult{Stream: stream, Hit: true}, nil
}

type t11RTPLedger struct {
	registered []struct {
		repo.ManagedResourceRegistration
		Port int
	}
	rows          map[string]*gbmodels.GbZLMManagedResource
	registerErr   error
	registerCalls int
}

func (l *t11RTPLedger) Register(_ context.Context, input repo.ManagedResourceRegistration) (*gbmodels.GbZLMManagedResource, error) {
	l.registerCalls++
	if l.registerErr != nil {
		return nil, l.registerErr
	}
	if l.rows == nil {
		l.rows = make(map[string]*gbmodels.GbZLMManagedResource)
	}
	l.registered = append(l.registered, struct {
		repo.ManagedResourceRegistration
		Port int
	}{ManagedResourceRegistration: input})
	row := &gbmodels.GbZLMManagedResource{NodeID: input.Identity.NodeID, ResourceType: input.Identity.ResourceType, ResourceKey: input.Identity.ResourceKey, Schema: input.Identity.Schema, Vhost: input.Identity.Vhost, App: input.Identity.App, Stream: input.Identity.Stream, IdentityFingerprint: input.Fingerprint, Summary: input.Summary, CreatedBy: input.CreatedBy}
	l.rows[rtpLedgerKey(input.Identity.NodeID, input.Identity.ResourceKey)] = row
	return row, nil
}

func (l *t11RTPLedger) Find(_ context.Context, identity repo.ManagedResourceIdentity) (*gbmodels.GbZLMManagedResource, error) {
	if l.rows == nil {
		return nil, repo.ErrManagedResourceNotFound
	}
	row, ok := l.rows[rtpLedgerKey(identity.NodeID, identity.ResourceKey)]
	if !ok || row.ResourceType != identity.ResourceType {
		return nil, repo.ErrManagedResourceNotFound
	}
	copy := *row
	return &copy, nil
}

func (l *t11RTPLedger) Tombstone(_ context.Context, identity repo.ManagedResourceIdentity, at time.Time) (*gbmodels.GbZLMManagedResource, error) {
	row, err := l.Find(context.Background(), identity)
	if err != nil {
		return nil, err
	}
	if row.TombstonedAt == nil {
		row.TombstonedAt = &at
	}
	l.rows[rtpLedgerKey(identity.NodeID, identity.ResourceKey)] = row
	return row, nil
}

func rtpLedgerKey(nodeID int64, key string) string { return strconv.FormatInt(nodeID, 10) + ":" + key }

type t11MutablePresence struct{ present bool }

func (p *t11MutablePresence) IsPresent(context.Context, OwnershipTarget) (bool, error) {
	return p.present, nil
}

type t11BusinessSource struct{ kind OwnershipType }

func (s t11BusinessSource) Resolve(context.Context, OwnershipTarget) ([]OwnershipEvidence, error) {
	return []OwnershipEvidence{{Type: s.kind, Key: "business", Confidence: OwnershipConfidenceProven}}, nil
}
