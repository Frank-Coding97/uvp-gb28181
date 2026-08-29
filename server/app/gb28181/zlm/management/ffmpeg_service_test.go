package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

func TestFFmpegServiceRejectsUnknownTemplateAndInjectionBeforeZLM(t *testing.T) {
	client := &t11FFmpegClient{}
	service := NewFFmpegService(FFmpegDependencies{
		Client:    client,
		Templates: NewFFmpegTemplateSet("ffmpeg.cmd_hd"),
		Ledger:    &t11Ledger{},
	})

	_, err := service.Create(context.Background(), 7, 1, FFmpegSourceCreateRequest{
		TemplateKey: "ffmpeg.unknown",
		SrcURL:      "rtsp://camera.example/live",
		DstURL:      "rtmp://127.0.0.1/live/out",
		TimeoutMS:   1000,
	})
	assertT11ValidationField(t, err, "templateKey")
	require.Zero(t, client.addCalls)

	_, err = service.Create(context.Background(), 7, 1, FFmpegSourceCreateRequest{
		TemplateKey: "ffmpeg.cmd_hd; rm -rf /",
		SrcURL:      "rtsp://camera.example/live",
		DstURL:      "rtmp://127.0.0.1/live/out",
		TimeoutMS:   1000,
	})
	assertT11ValidationField(t, err, "templateKey")
	require.Zero(t, client.addCalls)

	_, err = service.Create(context.Background(), 7, 1, FFmpegSourceCreateRequest{
		TemplateKey: "ffmpeg.cmd_hd",
		SrcURL:      "rtsp://camera.example/live;touch /tmp/pwned",
		DstURL:      "rtmp://127.0.0.1/live/out",
		TimeoutMS:   1000,
	})
	assertT11ValidationField(t, err, "srcUrl")
	require.Zero(t, client.addCalls)
}

func TestFFmpegServiceRejectsUntrustedActor(t *testing.T) {
	client := &t11FFmpegClient{}
	service := NewFFmpegService(FFmpegDependencies{
		Client:    client,
		Templates: NewFFmpegTemplateSet("ffmpeg.cmd_hd"),
		Ledger:    &t11Ledger{},
	})
	_, err := service.Create(context.Background(), 7, 0, FFmpegSourceCreateRequest{
		TemplateKey: "ffmpeg.cmd_hd", SrcURL: "rtsp://camera.example/live",
		DstURL: "rtmp://127.0.0.1/live/out", TimeoutMS: 1000,
	})
	assertT11ValidationField(t, err, "actorUserId")
	require.Zero(t, client.addCalls)
}

func TestFFmpegServiceRegistersOnlyAfterSuccessfulAddAndRedactsURLs(t *testing.T) {
	client := &t11FFmpegClient{
		addResult: &zlm.FFmpegSourceResult{Key: "ffmpeg-1"},
	}
	ledger := &t11Ledger{}
	service := NewFFmpegService(FFmpegDependencies{
		Client:    client,
		Templates: NewFFmpegTemplateSet("ffmpeg.cmd_hd"),
		Ledger:    ledger,
	})
	src := "rtsp://user:source-secret@camera.example/live?token=source-token"
	dst := "rtmp://publish:publish-secret@media.example/live/out?auth=dst-token"
	view, err := service.Create(context.Background(), 7, 42, FFmpegSourceCreateRequest{
		TemplateKey: "ffmpeg.cmd_hd", SrcURL: src, DstURL: dst, TimeoutMS: 1000,
	})
	require.NoError(t, err)
	require.Equal(t, "ffmpeg-1", view.Key)
	require.Len(t, ledger.registered, 1)
	require.Equal(t, 1, client.addCalls)
	require.Equal(t, "ffmpeg.cmd_hd", client.addRequest.FFmpegCmdKey)
	require.Equal(t, src, client.addRequest.SrcURL)
	require.Equal(t, dst, client.addRequest.DstURL)
	body, marshalErr := json.Marshal(view)
	require.NoError(t, marshalErr)
	encoded := string(body)
	for _, secret := range []string{src, dst, "source-secret", "publish-secret", "source-token", "dst-token"} {
		require.NotContains(t, encoded, secret)
	}
	require.NotEmpty(t, view.SrcURL.Fingerprint)
	require.NotEmpty(t, view.DstURL.Fingerprint)
	require.Contains(t, view.SrcURL.Summary, "rtsp://camera.example")

	client.addErr = errors.New("upstream source unreachable: " + src)
	_, err = service.Create(context.Background(), 7, 42, FFmpegSourceCreateRequest{
		TemplateKey: "ffmpeg.cmd_hd", SrcURL: src, DstURL: dst, TimeoutMS: 1000,
	})
	require.Error(t, err)
	require.Len(t, ledger.registered, 1, "failed add must not create a ledger row")
	require.NotContains(t, err.Error(), "source-secret")
	require.NotContains(t, err.Error(), "source-token")
}

func TestFFmpegServiceRegistrationFailureCompensatesTypedResource(t *testing.T) {
	client := &t11FFmpegClient{addResult: &zlm.FFmpegSourceResult{Key: "ffmpeg-rollback"}}
	ledger := &t11Ledger{registerErr: errors.New("ledger unavailable")}
	service := NewFFmpegService(FFmpegDependencies{
		Client: client, Templates: NewFFmpegTemplateSet("ffmpeg.cmd_hd"), Ledger: ledger,
	})
	_, err := service.Create(context.Background(), 7, 42, FFmpegSourceCreateRequest{
		TemplateKey: "ffmpeg.cmd_hd", SrcURL: "rtsp://camera.example/live",
		DstURL: "rtmp://127.0.0.1/live/out", TimeoutMS: 1000,
	})
	require.Error(t, err)
	require.Equal(t, 1, client.deleteCalls, "successful add must be compensated when registration fails")
	require.Equal(t, 1, ledger.registerCalls)
	require.Equal(t, CodeInternal, mustT11ManagementError(t, err).Code)
	require.NotContains(t, err.Error(), "ledger unavailable")

	client.deleteErr = errors.New("rollback unavailable")
	_, err = service.Create(context.Background(), 7, 42, FFmpegSourceCreateRequest{
		TemplateKey: "ffmpeg.cmd_hd", SrcURL: "rtsp://user:source-secret@camera.example/live",
		DstURL: "rtmp://writer:publish-secret@media.example/live/out", TimeoutMS: 1000,
	})
	require.Error(t, err)
	require.Equal(t, CodeInternal, mustT11ManagementError(t, err).Code)
	require.Contains(t, err.Error(), "rollback")
	require.NotContains(t, err.Error(), "source-secret")
	require.NotContains(t, err.Error(), "publish-secret")
}

func TestFFmpegServiceListPageIsBounded(t *testing.T) {
	list := make([]zlm.FFmpegSourceInfo, MaxResponseItems+17)
	for i := range list {
		list[i].Key = "ffmpeg-" + strconv.Itoa(i)
	}
	service := NewFFmpegService(FFmpegDependencies{
		Client: &t11FFmpegClient{list: list}, Templates: NewFFmpegTemplateSet("ffmpeg.cmd_hd"), Ledger: &t11Ledger{},
	})
	page, err := service.ListPage(context.Background(), 7, PageRequest{Page: 1, PageSize: MaxPageSize})
	require.NoError(t, err)
	require.Len(t, page.List, MaxPageSize)
	require.Equal(t, int64(MaxResponseItems), page.Total)
	require.True(t, page.Truncated)
}

func TestFFmpegServiceUnsupportedListIs422AndNeverEmptySuccess(t *testing.T) {
	service := NewFFmpegService(FFmpegDependencies{
		Client:    &t11FFmpegClient{listErr: errors.New("listFFmpegSource code=-404 msg=api not found")},
		Templates: NewFFmpegTemplateSet("ffmpeg.cmd_hd"), Ledger: &t11Ledger{},
	})
	items, err := service.List(context.Background(), 7)
	require.Error(t, err)
	require.Nil(t, items)
	managementErr := mustT11ManagementError(t, err)
	require.Equal(t, CodeUnsupportedCapability, managementErr.Code)
	require.Equal(t, 422, managementErr.HTTPStatus())
}

func TestFFmpegServiceDeletePreflightProtectsLedgerTombstone(t *testing.T) {
	client := &t11FFmpegClient{deleteResult: &zlm.ProxyDeleteResult{Key: "ffmpeg-1", Hit: true}}
	ledger := &t11Ledger{rows: map[string]*gbmodels.GbZLMManagedResource{
		ffmpegLedgerKey(7, "ffmpeg-1"): {NodeID: 7, ResourceType: ResourceTypeFFmpegSource, ResourceKey: "ffmpeg-1", App: ffmpegLedgerApp, Stream: "ffmpeg-1"},
	}}
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: t11Presence{present: true},
		Sources:  []OwnershipSource{t11ManagedSource{resourceType: ResourceTypeFFmpegSource, key: "ffmpeg-1", app: ffmpegLedgerApp, stream: "ffmpeg-1"}},
	})
	service := NewFFmpegService(FFmpegDependencies{
		Client: client, Templates: NewFFmpegTemplateSet("ffmpeg.cmd_hd"), Ledger: ledger, Ownership: resolver,
	})

	result, err := service.Delete(context.Background(), 7, "ffmpeg-1")
	require.NoError(t, err)
	require.True(t, result.Released)
	require.Equal(t, 1, client.deleteCalls)
	require.NotNil(t, ledger.rows[ffmpegLedgerKey(7, "ffmpeg-1")].TombstonedAt)
	client.list = nil
	result, err = service.Delete(context.Background(), 7, "ffmpeg-1")
	require.NoError(t, err)
	require.True(t, result.AlreadyReleased)
	require.True(t, result.Idempotent)
	require.Equal(t, 1, client.deleteCalls, "already absent tombstoned source must not call ZLM again")
}

func TestFFmpegServiceDeleteFailureOrChangedFingerprintDoesNotTombstone(t *testing.T) {
	key := ffmpegLedgerKey(7, "ffmpeg-1")
	ledger := &t11Ledger{rows: map[string]*gbmodels.GbZLMManagedResource{
		key: {NodeID: 7, ResourceType: ResourceTypeFFmpegSource, ResourceKey: "ffmpeg-1", App: ffmpegLedgerApp, Stream: "ffmpeg-1"},
	}}
	client := &t11FFmpegClient{deleteErr: errors.New("delete failed")}
	presence := &t11MutablePresence{present: true}
	resolver := NewOwnershipResolver(OwnershipDependencies{
		Presence: presence,
		Sources:  []OwnershipSource{t11ManagedSource{resourceType: ResourceTypeFFmpegSource, key: "ffmpeg-1"}},
	})
	service := NewFFmpegService(FFmpegDependencies{Client: client, Ledger: ledger, Templates: NewFFmpegTemplateSet("ffmpeg.cmd_hd"), Ownership: resolver})

	_, err := service.Delete(context.Background(), 7, "ffmpeg-1")
	require.Error(t, err)
	require.Equal(t, 1, client.deleteCalls)
	require.Nil(t, ledger.rows[key].TombstonedAt)

	client.deleteErr = nil
	preflight, err := service.PreflightDelete(context.Background(), 7, "ffmpeg-1")
	require.NoError(t, err)
	presence.present = false
	_, err = service.DeleteWithPreflight(context.Background(), 7, "ffmpeg-1", preflight)
	require.Equal(t, CodeOwnershipConflict, mustT11ManagementError(t, err).Code)
	require.Equal(t, 1, client.deleteCalls, "changed ownership must not reach ZLM")
	require.Nil(t, ledger.rows[key].TombstonedAt)
}

func TestFFmpegServiceListRedactsRawZLMCommandAndURLCredentials(t *testing.T) {
	client := &t11FFmpegClient{list: []zlm.FFmpegSourceInfo{{
		Key: "ffmpeg-raw-1", SrcURL: "rtsp://alice:source-secret@camera.example/live?token=source-token",
		DstURL: "rtmp://writer:publish-secret@media.example/out?auth=dst-token", FFmpegCmdKey: "ffmpeg.cmd_hd",
	}}}
	service := NewFFmpegService(FFmpegDependencies{Client: client, Templates: NewFFmpegTemplateSet("ffmpeg.cmd_hd"), Ledger: &t11Ledger{}})
	items, err := service.List(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, items, 1)
	body, marshalErr := json.Marshal(items)
	require.NoError(t, marshalErr)
	encoded := string(body)
	for _, secret := range []string{"source-secret", "publish-secret", "source-token", "dst-token", "alice:", "writer:"} {
		require.NotContains(t, encoded, secret)
	}
	require.NotContains(t, encoded, "\"cmd\"")
}

func TestFFmpegServiceListRawZLMCommandAndCredentialedURLsStayOutOfDTO(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/listFFmpegSource", r.URL.Path)
		_, _ = w.Write([]byte(`{"code":0,"data":[{"key":"ffmpeg-raw","src_url":"rtsp://alice:source-secret@camera.example/live?token=source-token","dst_url":"rtmp://writer:publish-secret@media.example/out?auth=dst-token","cmd":"ffmpeg -i rtsp://alice:raw-command-secret@camera.example/live -f flv rtmp://media.example/out","ffmpeg_cmd_key":"ffmpeg.cmd_hd"}]}`))
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	n := &node.Node{ID: 7, Host: parsed.Hostname(), APIPort: port, APISecret: "node-secret", State: node.StateActive}
	executor := NewNodeExecutor(t11NodeLookup{node: n}, func(*node.Node) *zlm.Client {
		return zlm.NewClientForNode(n)
	})
	service := NewFFmpegService(FFmpegDependencies{
		Client: NewFFmpegNodeClientAdapter(executor), Templates: NewFFmpegTemplateSet("ffmpeg.cmd_hd"), Ledger: &t11Ledger{},
	})
	page, err := service.ListPage(context.Background(), 7, PageRequest{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, page.List, 1)
	body, err := json.Marshal(page.List[0])
	require.NoError(t, err)
	encoded := string(body)
	for _, secret := range []string{"source-secret", "publish-secret", "source-token", "dst-token", "raw-command-secret", "alice:", "writer:"} {
		require.NotContains(t, encoded, secret)
	}
	require.NotContains(t, encoded, "\"cmd\"")
}

func assertT11ValidationField(t *testing.T, err error, field string) {
	t.Helper()
	var validationErr *ValidationError
	require.ErrorAs(t, err, &validationErr)
	_, ok := validationErr.Fields[field]
	require.True(t, ok, "expected validation field %q in %#v", field, validationErr.Fields)
}

type t11FFmpegClient struct {
	addResult    *zlm.FFmpegSourceResult
	addErr       error
	addRequest   zlm.FFmpegSourceRequest
	list         []zlm.FFmpegSourceInfo
	listErr      error
	deleteResult *zlm.ProxyDeleteResult
	deleteErr    error
	addCalls     int
	deleteCalls  int
}

type t11NodeLookup struct{ node *node.Node }

func (l t11NodeLookup) Get(nodeID int64) (*node.Node, bool) {
	if l.node == nil || l.node.ID != nodeID {
		return nil, false
	}
	return l.node, true
}

func (c *t11FFmpegClient) AddFFmpegSource(_ context.Context, _ int64, request zlm.FFmpegSourceRequest) (*zlm.FFmpegSourceResult, error) {
	c.addCalls++
	c.addRequest = request
	if c.addErr != nil {
		return nil, c.addErr
	}
	if c.addResult == nil {
		return &zlm.FFmpegSourceResult{Key: "ffmpeg-default"}, nil
	}
	return c.addResult, nil
}

func (c *t11FFmpegClient) ListFFmpegSources(context.Context, int64) ([]zlm.FFmpegSourceInfo, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return append([]zlm.FFmpegSourceInfo(nil), c.list...), nil
}

func (c *t11FFmpegClient) DeleteFFmpegSource(_ context.Context, _ int64, key string) (*zlm.ProxyDeleteResult, error) {
	c.deleteCalls++
	if c.deleteErr != nil {
		return nil, c.deleteErr
	}
	if c.deleteResult == nil {
		return &zlm.ProxyDeleteResult{Key: key}, nil
	}
	return c.deleteResult, nil
}

type t11Ledger struct {
	registered    []repo.ManagedResourceRegistration
	rows          map[string]*gbmodels.GbZLMManagedResource
	registerErr   error
	registerCalls int
}

func (l *t11Ledger) Register(_ context.Context, input repo.ManagedResourceRegistration) (*gbmodels.GbZLMManagedResource, error) {
	l.registerCalls++
	if l.registerErr != nil {
		return nil, l.registerErr
	}
	l.registered = append(l.registered, input)
	if l.rows == nil {
		l.rows = make(map[string]*gbmodels.GbZLMManagedResource)
	}
	row := &gbmodels.GbZLMManagedResource{NodeID: input.Identity.NodeID, ResourceType: input.Identity.ResourceType, ResourceKey: input.Identity.ResourceKey, App: input.Identity.App, Stream: input.Identity.Stream, IdentityFingerprint: input.Fingerprint, Summary: input.Summary, CreatedBy: input.CreatedBy}
	l.rows[ffmpegLedgerKey(input.Identity.NodeID, input.Identity.ResourceKey)] = row
	return row, nil
}

func (l *t11Ledger) Find(_ context.Context, identity repo.ManagedResourceIdentity) (*gbmodels.GbZLMManagedResource, error) {
	if l.rows == nil {
		return nil, repo.ErrManagedResourceNotFound
	}
	row, ok := l.rows[ffmpegLedgerKey(identity.NodeID, identity.ResourceKey)]
	if !ok || row.ResourceType != identity.ResourceType {
		return nil, repo.ErrManagedResourceNotFound
	}
	copy := *row
	return &copy, nil
}

func (l *t11Ledger) Tombstone(_ context.Context, identity repo.ManagedResourceIdentity, at time.Time) (*gbmodels.GbZLMManagedResource, error) {
	row, err := l.Find(context.Background(), identity)
	if err != nil {
		return nil, err
	}
	if row.TombstonedAt == nil {
		row.TombstonedAt = &at
	}
	l.rows[ffmpegLedgerKey(identity.NodeID, identity.ResourceKey)] = row
	return row, nil
}

func ffmpegLedgerKey(nodeID int64, key string) string {
	return strconv.FormatInt(nodeID, 10) + ":" + key
}

type t11Presence struct{ present bool }

func (p t11Presence) IsPresent(context.Context, OwnershipTarget) (bool, error) { return p.present, nil }

type t11ManagedSource struct {
	resourceType string
	key          string
	app          string
	stream       string
}

func (s t11ManagedSource) Resolve(context.Context, OwnershipTarget) ([]OwnershipEvidence, error) {
	return []OwnershipEvidence{{Type: OwnershipTypeManaged, ResourceType: s.resourceType, Key: s.key, Confidence: OwnershipConfidenceProven}}, nil
}
