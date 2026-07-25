package talk

import (
	"context"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type fakeTalkNodes struct{ items map[int64]*node.Node }

func (f fakeTalkNodes) Get(id int64) (*node.Node, bool) { n, ok := f.items[id]; return n, ok }

type fakeTalkPicker struct {
	node  *node.Node
	calls atomic.Int32
}

func (f *fakeTalkPicker) Pick(context.Context, play.PickContext) (*node.Node, error) {
	f.calls.Add(1)
	return f.node, nil
}

type fakeTalkConfigs struct{ configs map[int64]node.ServerConfig }

func (f fakeTalkConfigs) Get(_ context.Context, id int64) (node.ServerConfig, error) {
	return f.configs[id], nil
}

func talkCreateRequest(channelID uint) CreateRequest {
	caps := `{"talk":true}`
	return CreateRequest{
		Channel: &models.GbChannel{ID: channelID, DeviceID: "D", ChannelID: "C", Status: models.ChannelStatusOnline, StreamID: "live", Capabilities: &caps},
		Device:  &models.GbDevice{DeviceID: "D", Status: models.DeviceStatusOnline},
		ActorID: 7, ActorDeptID: 9,
		Mode: models.TalkSessionModeTalk,
	}
}

func TestTalkServiceCreatePrefersPlaybackNodeAndStoresOnlyTokenHash(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	locations := stream.NewLocationMap()
	locations.Bind("live", 2)
	preferred := &node.Node{ID: 2, Name: "edge-2", Host: "talk.example.test", State: node.StateActive}
	picker := &fakeTalkPicker{node: &node.Node{ID: 3, Host: "other"}}
	now := time.Unix(1700000000, 0)
	service := NewService(repo, fakeTalkNodes{items: map[int64]*node.Node{2: preferred}}, locations, picker,
		fakeTalkConfigs{configs: map[int64]node.ServerConfig{2: {HTTPSPort: 18443}}}, func() time.Time { return now })

	result, err := service.Create(context.Background(), talkCreateRequest(42))
	require.NoError(t, err)
	require.EqualValues(t, 2, result.NodeID)
	require.Zero(t, picker.calls.Load())
	require.True(t, result.ExpiresAt.Equal(now.Add(30*time.Second)))
	parsed, err := url.Parse(result.PublishURL)
	require.NoError(t, err)
	require.Equal(t, "https", parsed.Scheme)
	require.Equal(t, "talk.example.test:18443", parsed.Host)
	require.Equal(t, "talk", parsed.Query().Get("app"))
	require.Equal(t, result.SourceStream, parsed.Query().Get("stream"))
	require.Equal(t, result.PublishToken, parsed.Query().Get("token"))

	stored, err := repo.FindBySession(context.Background(), result.SessionID)
	require.NoError(t, err)
	require.NotEqual(t, result.PublishToken, stored.PublishTokenHash)
	require.NotContains(t, stored.PublishTokenHash, result.PublishToken)
	require.Equal(t, models.TalkSessionReserved, stored.State)
	require.Equal(t, result.RecvStream, stored.RecvStream)
	require.Equal(t, result.SSRC, stored.SSRC)
}

func TestTalkServiceDoesNotGateCreateOnCapabilityMetadata(t *testing.T) {
	for _, capabilities := range []*string{
		nil,
		stringPointer(`{"talk":false}`),
		stringPointer(`{"broadcast":false,"talk":false}`),
		stringPointer(`{not-json}`),
	} {
		db := newTalkRepoTestDB(t)
		repo := NewGormRepo(db)
		n := &node.Node{ID: 1, Host: "node", State: node.StateActive}
		service := NewService(repo, fakeTalkNodes{items: map[int64]*node.Node{1: n}}, stream.NewLocationMap(), &fakeTalkPicker{node: n},
			fakeTalkConfigs{configs: map[int64]node.ServerConfig{1: {HTTPSPort: 443}}}, time.Now)

		request := talkCreateRequest(1)
		request.Channel.Capabilities = capabilities
		created, err := service.Create(context.Background(), request)
		require.NoError(t, err)
		require.NotEmpty(t, created.SessionID)
	}
}

func TestTalkServicePersistsAndReturnsTalkMode(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	n := &node.Node{ID: 1, Host: "node", State: node.StateActive}
	service := NewService(repo, fakeTalkNodes{items: map[int64]*node.Node{1: n}}, stream.NewLocationMap(), &fakeTalkPicker{node: n},
		fakeTalkConfigs{configs: map[int64]node.ServerConfig{1: {HTTPSPort: 443}}}, time.Now)

	created, err := service.Create(context.Background(), talkCreateRequest(1))
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionModeTalk, created.Mode)

	stored, err := repo.FindBySession(context.Background(), created.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionModeTalk, stored.Mode)
}

func TestTalkServiceRejectsInvalidModeWithoutCreatingLease(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	n := &node.Node{ID: 1, Host: "node", State: node.StateActive}
	service := NewService(repo, fakeTalkNodes{items: map[int64]*node.Node{1: n}}, stream.NewLocationMap(), &fakeTalkPicker{node: n},
		fakeTalkConfigs{configs: map[int64]node.ServerConfig{1: {HTTPSPort: 443}}}, time.Now)

	request := talkCreateRequest(1)
	request.Mode = "invalid"
	_, err := service.Create(context.Background(), request)
	require.ErrorIs(t, err, ErrInvalidTalkSessionMode)
	sessions, err := repo.ListNonterminal(context.Background())
	require.NoError(t, err)
	require.Empty(t, sessions)
}

func TestTalkServiceRejectsBroadcastWithoutCreatingLease(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	n := &node.Node{ID: 1, Host: "node", State: node.StateActive}
	picker := &fakeTalkPicker{node: n}
	service := NewService(repo, fakeTalkNodes{items: map[int64]*node.Node{1: n}}, stream.NewLocationMap(), picker,
		fakeTalkConfigs{configs: map[int64]node.ServerConfig{1: {HTTPSPort: 443}}}, time.Now)

	request := talkCreateRequest(1)
	request.Mode = models.TalkSessionModeBroadcast
	_, err := service.Create(context.Background(), request)
	require.ErrorIs(t, err, ErrBroadcastNotImplemented)
	require.Zero(t, picker.calls.Load())
	sessions, err := repo.ListNonterminal(context.Background())
	require.NoError(t, err)
	require.Empty(t, sessions)
}

func TestTalkServiceRejectsInsecureNodeWithoutCreatingLease(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	n := &node.Node{ID: 1, Host: "node", State: node.StateActive}
	picker := &fakeTalkPicker{node: n}
	service := NewService(repo, fakeTalkNodes{items: map[int64]*node.Node{1: n}}, stream.NewLocationMap(), picker,
		fakeTalkConfigs{configs: map[int64]node.ServerConfig{1: {HTTPPort: 18080}}}, time.Now)

	_, err := service.Create(context.Background(), talkCreateRequest(1))
	require.ErrorIs(t, err, ErrSecurePublishUnavailable)
	sessions, err := repo.ListNonterminal(context.Background())
	require.NoError(t, err)
	require.Empty(t, sessions)
}

func stringPointer(value string) *string { return &value }

func TestTalkServiceRejectsOfflineTargetWithoutCreatingLease(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	n := &node.Node{ID: 1, Host: "node", State: node.StateActive}
	service := NewService(repo, fakeTalkNodes{items: map[int64]*node.Node{1: n}}, stream.NewLocationMap(), &fakeTalkPicker{node: n},
		fakeTalkConfigs{configs: map[int64]node.ServerConfig{1: {HTTPSPort: 443}}}, time.Now)
	request := talkCreateRequest(1)
	request.Device.Status = models.DeviceStatusOffline

	_, err := service.Create(context.Background(), request)
	require.ErrorIs(t, err, ErrTalkTargetOffline)
	sessions, err := repo.ListNonterminal(context.Background())
	require.NoError(t, err)
	require.Empty(t, sessions)
}

func TestTalkServiceConcurrentCreateHasSingleLease(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	n := &node.Node{ID: 1, Host: "node", State: node.StateActive}
	service := NewService(repo, fakeTalkNodes{items: map[int64]*node.Node{1: n}}, stream.NewLocationMap(), &fakeTalkPicker{node: n},
		fakeTalkConfigs{configs: map[int64]node.ServerConfig{1: {HTTPSPort: 443}}}, time.Now)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Create(context.Background(), talkCreateRequest(99)); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, successes.Load())
}

func TestTalkServiceAuthorizePublishConsumesTokenOnceButAllowsSameHookRetry(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	n := &node.Node{ID: 1, Host: "node", State: node.StateActive}
	service := NewService(repo, fakeTalkNodes{items: map[int64]*node.Node{1: n}}, stream.NewLocationMap(), &fakeTalkPicker{node: n},
		fakeTalkConfigs{configs: map[int64]node.ServerConfig{1: {HTTPSPort: 443}}}, time.Now)
	created, err := service.Create(context.Background(), talkCreateRequest(5))
	require.NoError(t, err)
	auth := PublishAuthorization{NodeID: 1, App: "talk", SourceStream: created.SourceStream, PublishToken: created.PublishToken, PublishID: "pub-1"}
	allowed, err := service.AuthorizePublish(context.Background(), auth)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = service.AuthorizePublish(context.Background(), auth)
	require.NoError(t, err)
	require.True(t, allowed)
	auth.PublishID = "pub-2"
	allowed, err = service.AuthorizePublish(context.Background(), auth)
	require.NoError(t, err)
	require.False(t, allowed)
	auth.PublishID = "pub-1"
	auth.PublishToken = "forged"
	allowed, err = service.AuthorizePublish(context.Background(), auth)
	require.NoError(t, err)
	require.False(t, allowed)
	stored, err := repo.FindBySession(context.Background(), created.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionPublishing, stored.State)
	require.Equal(t, "pub-1", stored.PublishID)
}

func TestTalkServiceAuthorizePublishRejectsExpiredToken(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	n := &node.Node{ID: 1, Host: "node", State: node.StateActive}
	now := time.Unix(1700000000, 0)
	service := NewService(repo, fakeTalkNodes{items: map[int64]*node.Node{1: n}}, stream.NewLocationMap(), &fakeTalkPicker{node: n},
		fakeTalkConfigs{configs: map[int64]node.ServerConfig{1: {HTTPSPort: 443}}}, func() time.Time { return now })
	created, err := service.Create(context.Background(), talkCreateRequest(6))
	require.NoError(t, err)
	now = now.Add(31 * time.Second)

	allowed, err := service.AuthorizePublish(context.Background(), PublishAuthorization{
		NodeID: 1, App: "talk", SourceStream: created.SourceStream, PublishToken: created.PublishToken, PublishID: "pub-expired",
	})
	require.NoError(t, err)
	require.False(t, allowed)
}
