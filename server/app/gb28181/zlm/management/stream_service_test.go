package management

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestStreamServiceListFiltersPaginatesAndRedactsRawMedia(t *testing.T) {
	registry := &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}}
	runtime := &t9RuntimeReader{media: map[int64][]zlm.MediaInfo{1: {
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "live", Stream: "camera/1", OriginURL: "rtsp://user:secret@example.com/live/camera/1", OriginSock: &zlm.SockInfo{Identifier: "secret-socket"}, Tracks: []zlm.MediaTrack{{CodecIDName: "H264"}}},
		{Online: true, Schema: "rtmp", VHost: "__defaultVhost__", App: "other", Stream: "camera/2"},
	}}}
	service := NewStreamService(StreamServiceDependencies{Registry: registry, Runtime: runtime})

	result, err := service.ListStreams(context.Background(), StreamListRequest{
		Filter: StreamFilter{App: "live"}, Page: PageRequest{Page: 1, PageSize: 1},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Total)
	require.Len(t, result.List, 1)
	require.Equal(t, MediaIdentity{Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "camera/1"}, result.List[0].Media)
	require.Equal(t, 1, runtime.listCalls)

	encoded, marshalErr := json.Marshal(result)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(encoded), "rtsp://user:secret")
	require.NotContains(t, string(encoded), "secret-socket")
}

func TestStreamServiceDetailAndViewersCarryCompleteIdentity(t *testing.T) {
	identity := MediaIdentity{Schema: "rtsp", Vhost: "vhost/with space", App: "live", Stream: "camera/1"}
	runtime := &t9RuntimeReader{
		details: map[string]*zlm.MediaInfo{t9MediaKey(1, identity): {
			Online: true, Schema: identity.Schema, VHost: identity.Vhost, App: identity.App, Stream: identity.Stream,
			ReaderCount: 2, TotalReaderCount: 3, Tracks: []zlm.MediaTrack{{CodecIDName: "H264"}},
		}},
		players: map[string][]zlm.MediaPlayer{t9MediaKey(1, identity): {{Identifier: "session-1", TypeID: "TcpSession", PeerIP: "192.0.2.10", PeerPort: 1000}}},
	}
	service := NewStreamService(StreamServiceDependencies{
		Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}}, Runtime: runtime,
	})

	detail, err := service.GetStreamDetail(context.Background(), 1, identity)
	require.NoError(t, err)
	require.Equal(t, identity, detail.Media)
	require.Len(t, detail.Tracks, 1)

	viewers, err := service.ListStreamViewers(context.Background(), 1, identity, PageRequest{})
	require.NoError(t, err)
	require.Equal(t, identity, viewers.Target)
	require.Len(t, viewers.List, 1)
	require.Equal(t, identity, viewers.List[0].Media)
	require.True(t, viewers.List[0].Kickable)
}

func TestStreamServiceDetailOwnershipViewUsesSafeManagedGBAndUnknownSources(t *testing.T) {
	cases := []struct {
		name       string
		status     OwnershipStatus
		ownerType  OwnershipType
		confidence OwnershipConfidence
	}{
		{name: "managed", status: OwnershipStatusManaged, ownerType: OwnershipTypeManaged, confidence: OwnershipConfidenceProven},
		{name: "gb", status: OwnershipStatusOwned, ownerType: OwnershipTypeRealtimePlayback, confidence: OwnershipConfidenceProven},
		{name: "unknown", status: OwnershipStatusUnknown, ownerType: OwnershipTypeUnknown, confidence: OwnershipConfidenceUncertain},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			identity := t9Identity("ownership-" + testCase.name)
			target := OwnershipTarget{NodeID: 1, Media: identity}
			runtime := &t9RuntimeReader{details: map[string]*zlm.MediaInfo{t9MediaKey(1, identity): t9MediaInfo(identity)}}
			ownership := &t9Ownership{preflight: OwnershipPreflight{
				Target: target,
				Snapshot: OwnershipSnapshot{
					Target: target, Present: true, PresenceKnown: true, Status: testCase.status,
					Owners: []OwnershipEvidence{{Type: testCase.ownerType, Confidence: testCase.confidence,
						Key: "secret-ledger-key", Owner: "user:42", Reason: "internal ownership reason"}},
					Impacts: []Impact{{ResourceType: "internal", ResourceKey: "secret-impact-key", Owner: "user:42", Reason: "internal impact reason"}},
				},
				Fingerprint: strings.Repeat("a", 64),
			}}
			service := NewStreamService(StreamServiceDependencies{
				Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}},
				Runtime:  runtime, Ownership: ownership,
			})

			detail, err := service.GetStreamDetail(context.Background(), 1, identity)
			require.NoError(t, err)
			require.Equal(t, testCase.status, detail.Ownership.Status)
			require.True(t, detail.Ownership.Present)
			require.True(t, detail.Ownership.PresenceKnown)
			require.Len(t, detail.Ownership.Sources, 1)
			require.Equal(t, testCase.ownerType, detail.Ownership.Sources[0].Type)
			require.Equal(t, testCase.confidence, detail.Ownership.Sources[0].Confidence)
			require.Len(t, detail.Ownership.Impacts, 1)
			require.Equal(t, testCase.ownerType, detail.Ownership.Impacts[0].Type)

			encoded, marshalErr := json.Marshal(detail)
			require.NoError(t, marshalErr)
			require.NotContains(t, string(encoded), "secret-ledger-key")
			require.NotContains(t, string(encoded), "internal ownership reason")
			require.NotContains(t, string(encoded), "secret-impact-key")
			require.NotContains(t, string(encoded), "user:42")
		})
	}
}

func TestStreamServiceCloseUsesFreshProofAndSingularClose(t *testing.T) {
	identity := t9Identity("close")
	target := OwnershipTarget{NodeID: 1, Media: identity}
	fresh := &t9FreshReader{infos: map[string][]*zlm.MediaInfo{t9MediaKey(1, identity): {
		t9MediaInfo(identity),
		t9MediaInfo(identity),
		nil,
	}}}
	ownership := &t9Ownership{preflight: OwnershipPreflight{
		Target: target, Snapshot: OwnershipSnapshot{Target: target, Present: true, PresenceKnown: true, Status: OwnershipStatusManaged, Fingerprint: strings.Repeat("a", 64)}, Fingerprint: strings.Repeat("a", 64),
	}}
	action := &t9MediaAction{closeResult: zlm.CloseStreamResult{Closed: true}}
	service := NewStreamService(StreamServiceDependencies{
		Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}},
		Fresh:    fresh, Executor: action, Ownership: ownership,
	})

	result, err := service.CloseStream(context.Background(), CloseStreamRequest{Target: target, Fingerprint: strings.Repeat("a", 64)})
	require.NoError(t, err)
	require.True(t, result.Closed)
	require.False(t, result.AlreadyAbsent)
	require.GreaterOrEqual(t, fresh.infoCalls, 2, "preflight and post-action proof must bypass runtime cache")
	require.Equal(t, 1, action.closeCalls)
	require.False(t, action.lastForce)
	require.Equal(t, zlm.StreamTarget{Schema: identity.Schema, VHost: identity.Vhost, App: identity.App, Stream: identity.Stream}, action.lastTarget)
}

func TestStreamServiceCloseFingerprintChangeHasZeroSideEffects(t *testing.T) {
	identity := t9Identity("changed")
	target := OwnershipTarget{NodeID: 1, Media: identity}
	ownership := &t9Ownership{preflight: OwnershipPreflight{Target: target, Snapshot: OwnershipSnapshot{Target: target, Present: true, PresenceKnown: true, Status: OwnershipStatusManaged}, Fingerprint: strings.Repeat("b", 64)}, executeErr: NewOwnershipConflictError("1", "changed")}
	service := NewStreamService(StreamServiceDependencies{
		Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}},
		Fresh:    &t9FreshReader{infos: map[string][]*zlm.MediaInfo{t9MediaKey(1, identity): {t9MediaInfo(identity)}}},
		Executor: &t9MediaAction{}, Ownership: ownership,
	})

	_, err := service.CloseStream(context.Background(), CloseStreamRequest{Target: target, Fingerprint: strings.Repeat("a", 64)})
	require.Equal(t, CodeOwnershipConflict, t9ManagementCode(err))
	require.Zero(t, service.executor.(*t9MediaAction).closeCalls)
}

func TestStreamServiceNormalCloseFailsClosedForSchemaLessLedgerProvenance(t *testing.T) {
	identity := t9Identity("ledger-schema")
	target := OwnershipTarget{NodeID: 1, Media: identity}
	fingerprint := strings.Repeat("a", 64)
	ownership := &t9Ownership{preflight: OwnershipPreflight{
		Target: target,
		Snapshot: OwnershipSnapshot{Target: target, Present: true, PresenceKnown: true, Status: OwnershipStatusManaged,
			Owners: []OwnershipEvidence{{Type: OwnershipTypeManaged, Reason: "management ledger provenance"}}, Fingerprint: fingerprint},
		Fingerprint: fingerprint,
	}}
	action := &t9MediaAction{}
	service := NewStreamService(StreamServiceDependencies{
		Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}},
		Fresh:    &t9FreshReader{infos: map[string][]*zlm.MediaInfo{t9MediaKey(1, identity): {t9MediaInfo(identity)}}},
		Executor: action, Ownership: ownership,
	})

	_, err := service.CloseStream(context.Background(), CloseStreamRequest{Target: target, Fingerprint: fingerprint})
	require.Equal(t, CodeOwnershipConflict, t9ManagementCode(err))
	require.Zero(t, action.closeCalls)
}

func TestStreamServiceForceCloseIsSeparateAndRequiresReason(t *testing.T) {
	identity := t9Identity("force")
	target := OwnershipTarget{NodeID: 1, Media: identity}
	ownership := &t9Ownership{preflight: OwnershipPreflight{Target: target, Snapshot: OwnershipSnapshot{Target: target, Present: true, PresenceKnown: true, Status: OwnershipStatusOwned}, Fingerprint: strings.Repeat("a", 64)}}
	action := &t9MediaAction{closeResult: zlm.CloseStreamResult{Closed: true}}
	fresh := &t9FreshReader{infos: map[string][]*zlm.MediaInfo{t9MediaKey(1, identity): {t9MediaInfo(identity), t9MediaInfo(identity), nil}}}
	service := NewStreamService(StreamServiceDependencies{Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}}, Fresh: fresh, Executor: action, Ownership: ownership})

	_, err := service.ForceCloseStream(context.Background(), ForceCloseStreamRequest{Target: target, Fingerprint: strings.Repeat("a", 64)})
	require.Error(t, err)
	require.Zero(t, action.closeCalls)

	result, err := service.ForceCloseStream(context.Background(), ForceCloseStreamRequest{Target: target, Fingerprint: strings.Repeat("a", 64), Reason: "operator confirmed"})
	require.NoError(t, err)
	require.True(t, result.Closed)
	require.Equal(t, 1, action.closeCalls)
	require.True(t, action.lastForce)
}

func TestStreamServiceBatchPreflightChangeHasZeroSideEffects(t *testing.T) {
	first, second := t9Identity("one"), t9Identity("two")
	targets := []OwnershipTarget{{NodeID: 1, Media: first}, {NodeID: 1, Media: second}}
	ownership := &t9Ownership{batch: OwnershipBatchPreflight{Targets: targets, Fingerprint: strings.Repeat("b", 64)}, batchExecuteErr: NewOwnershipConflictError("1", "changed")}
	fresh := &t9FreshReader{infos: map[string][]*zlm.MediaInfo{
		t9MediaKey(1, first):  {t9MediaInfo(first)},
		t9MediaKey(1, second): {t9MediaInfo(second)},
	}}
	action := &t9MediaAction{closeResult: zlm.CloseStreamResult{Closed: true}}
	service := NewStreamService(StreamServiceDependencies{Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}}, Fresh: fresh, Executor: action, Ownership: ownership})

	_, err := service.CloseStreams(context.Background(), OwnershipBatchPreflight{
		Targets: targets,
		Snapshots: []OwnershipSnapshot{
			{Target: targets[0], Present: true, PresenceKnown: true, Status: OwnershipStatusManaged},
			{Target: targets[1], Present: true, PresenceKnown: true, Status: OwnershipStatusManaged},
		},
		Fingerprint: strings.Repeat("a", 64),
	})
	require.Equal(t, CodeOwnershipConflict, t9ManagementCode(err))
	require.Zero(t, action.closeCalls)
}

func TestStreamServicePreviewUsesIndependentHookSchemasAndTokens(t *testing.T) {
	identity := t9Identity("preview")
	signer, err := NewPreviewSigner([]byte(strings.Repeat("k", 32)))
	require.NoError(t, err)
	classifier := PreviewClassifierFunc(func(_ context.Context, resource PreviewResource) (PreviewResourceClass, error) {
		if resource.Schema == "rtp" {
			return PreviewResourceGB, nil
		}
		return PreviewResourceNonGBPreviewable, nil
	})
	urls := &t9PreviewURLResolver{}
	runtime := &t9RuntimeReader{details: map[string]*zlm.MediaInfo{t9MediaKey(1, identity): t9MediaInfo(identity)}}
	service := NewStreamService(StreamServiceDependencies{
		Registry: &t9NodeRegistry{nodes: []*node.Node{{ID: 1, MediaServerUUID: "node-uuid", State: node.StateActive}}},
		Runtime:  runtime, PreviewIssuer: signer, PreviewURLs: urls, PreviewClassifier: classifier,
	})

	flv, err := service.IssuePreviewGrant(context.Background(), 42, PreviewGrantRequest{NodeID: 1, Media: identity, Protocol: string(PreviewProtocolHTTPSFLV)})
	require.NoError(t, err)
	fmp4, err := service.IssuePreviewGrant(context.Background(), 42, PreviewGrantRequest{NodeID: 1, Media: identity, Protocol: string(PreviewProtocolHTTPSFMP4)})
	require.NoError(t, err)
	require.NotEqual(t, flv.Token, fmp4.Token)
	require.NotEqual(t, flv.JTIHash, fmp4.JTIHash)
	require.Equal(t, "rtmp", flv.HookSchema)
	require.Equal(t, "fmp4", fmp4.HookSchema)
	require.Equal(t, "rtmp", urls.identities[0].Schema)
	require.Equal(t, "fmp4", urls.identities[1].Schema)

	_, err = service.IssuePreviewGrant(context.Background(), 42, PreviewGrantRequest{NodeID: 1, Media: identity, Protocol: string(PreviewProtocolWebRTCS)})
	require.ErrorIs(t, err, ErrPreviewUnsupported)
}

func TestStreamServiceGBPreviewStaysOnPlayAuthBoundary(t *testing.T) {
	identity := t9Identity("gb-preview")
	signer, err := NewPreviewSigner([]byte(strings.Repeat("p", 32)))
	require.NoError(t, err)
	urls := &t9PreviewURLResolver{}
	runtime := &t9RuntimeReader{details: map[string]*zlm.MediaInfo{t9MediaKey(1, identity): t9MediaInfo(identity)}}
	service := NewStreamService(StreamServiceDependencies{
		Registry: &t9NodeRegistry{nodes: []*node.Node{{ID: 1, MediaServerUUID: "node-uuid", State: node.StateActive}}},
		Runtime:  runtime, PreviewIssuer: signer, PreviewURLs: urls,
		PreviewClassifier: PreviewClassifierFunc(func(_ context.Context, _ PreviewResource) (PreviewResourceClass, error) {
			return PreviewResourceGB, nil
		}),
	})

	_, err = service.IssuePreviewGrant(context.Background(), 42, PreviewGrantRequest{NodeID: 1, Media: identity, Protocol: string(PreviewProtocolHTTPSFLV)})
	require.ErrorIs(t, err, ErrGBPreviewRequiresPlayAuth)
	require.ErrorIs(t, err, ErrPreviewUnsupported)
	require.Empty(t, urls.identities)
}

func t9Node(id int64, state node.State) *node.Node {
	return &node.Node{ID: id, Name: fmt.Sprintf("node-%d", id), MediaServerUUID: fmt.Sprintf("uuid-%d", id), State: state}
}

func t9Identity(stream string) MediaIdentity {
	return MediaIdentity{Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: stream}
}

func t9MediaInfo(identity MediaIdentity) *zlm.MediaInfo {
	return &zlm.MediaInfo{Online: true, Schema: identity.Schema, VHost: identity.Vhost, App: identity.App, Stream: identity.Stream}
}

func t9MediaKey(nodeID int64, identity MediaIdentity) string {
	return fmt.Sprintf("%d|%s|%s|%s|%s", nodeID, identity.Schema, identity.Vhost, identity.App, identity.Stream)
}

type t9NodeRegistry struct{ nodes []*node.Node }

func (r *t9NodeRegistry) List() []*node.Node { return r.nodes }
func (r *t9NodeRegistry) Get(id int64) (*node.Node, bool) {
	for _, current := range r.nodes {
		if current != nil && current.ID == id {
			copy := *current
			return &copy, true
		}
	}
	return nil, false
}

type t9RuntimeReader struct {
	media     map[int64][]zlm.MediaInfo
	details   map[string]*zlm.MediaInfo
	players   map[string][]zlm.MediaPlayer
	sessions  map[int64][]zlm.Session
	listCalls int
}

func (r *t9RuntimeReader) GetMediaListFiltered(_ context.Context, nodeID int64, filter zlm.MediaFilter) ([]zlm.MediaInfo, error) {
	r.listCalls++
	items := r.media[nodeID]
	result := make([]zlm.MediaInfo, 0, len(items))
	for _, item := range items {
		if filter.Schema != "" && item.Schema != filter.Schema || filter.VHost != "" && item.VHost != filter.VHost || filter.App != "" && item.App != filter.App || filter.Stream != "" && item.Stream != filter.Stream {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

func (r *t9RuntimeReader) GetMediaInfo(_ context.Context, nodeID int64, target zlm.StreamTarget) (*zlm.MediaInfo, error) {
	item := r.details[t9MediaKey(nodeID, MediaIdentity{Schema: target.Schema, Vhost: target.VHost, App: target.App, Stream: target.Stream})]
	if item == nil {
		return nil, ErrMediaNotFound
	}
	copy := *item
	return &copy, nil
}

func (r *t9RuntimeReader) GetMediaPlayerList(_ context.Context, nodeID int64, target zlm.StreamTarget) ([]zlm.MediaPlayer, error) {
	return append([]zlm.MediaPlayer(nil), r.players[t9MediaKey(nodeID, MediaIdentity{Schema: target.Schema, Vhost: target.VHost, App: target.App, Stream: target.Stream})]...), nil
}

func (r *t9RuntimeReader) GetAllSessions(_ context.Context, nodeID int64, _ zlm.SessionFilter) ([]zlm.Session, error) {
	return append([]zlm.Session(nil), r.sessions[nodeID]...), nil
}

type t9FreshReader struct {
	mu        sync.Mutex
	infos     map[string][]*zlm.MediaInfo
	players   map[string][][]zlm.MediaPlayer
	infoCalls int
}

func (r *t9FreshReader) GetMediaInfoFresh(_ context.Context, nodeID int64, target zlm.StreamTarget) (*zlm.MediaInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.infoCalls++
	key := t9MediaKey(nodeID, MediaIdentity{Schema: target.Schema, Vhost: target.VHost, App: target.App, Stream: target.Stream})
	values := r.infos[key]
	if len(values) == 0 {
		return nil, ErrMediaNotFound
	}
	value := values[0]
	r.infos[key] = values[1:]
	if value == nil {
		return nil, ErrMediaNotFound
	}
	copy := *value
	return &copy, nil
}

func (r *t9FreshReader) GetMediaPlayerListFresh(_ context.Context, nodeID int64, target zlm.StreamTarget) ([]zlm.MediaPlayer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := t9MediaKey(nodeID, MediaIdentity{Schema: target.Schema, Vhost: target.VHost, App: target.App, Stream: target.Stream})
	values := r.players[key]
	if len(values) == 0 {
		return nil, ErrMediaNotFound
	}
	result := append([]zlm.MediaPlayer(nil), values[0]...)
	r.players[key] = values[1:]
	return result, nil
}

type t9MediaAction struct {
	closeResult zlm.CloseStreamResult
	closeErr    error
	closeCalls  int
	lastForce   bool
	lastTarget  zlm.StreamTarget
	kickCalls   int
}

func (a *t9MediaAction) CloseStream(_ context.Context, _ int64, target zlm.StreamTarget, force bool) (zlm.CloseStreamResult, error) {
	a.closeCalls++
	a.lastTarget, a.lastForce = target, force
	return a.closeResult, a.closeErr
}

func (a *t9MediaAction) KickSession(_ context.Context, _ int64, _ string) error {
	a.kickCalls++
	return nil
}

type t9Ownership struct {
	preflight       OwnershipPreflight
	executeErr      error
	batch           OwnershipBatchPreflight
	batchExecuteErr error
}

func (o *t9Ownership) Preflight(context.Context, OwnershipTarget) (OwnershipPreflight, error) {
	return o.preflight, nil
}
func (o *t9Ownership) Execute(ctx context.Context, preflight OwnershipPreflight, action func(context.Context, OwnershipTarget) error) error {
	if o.executeErr != nil {
		return o.executeErr
	}
	return action(ctx, preflight.Target)
}
func (o *t9Ownership) ExecuteForce(ctx context.Context, preflight OwnershipPreflight, _ string, action func(context.Context, OwnershipTarget) error) error {
	if o.executeErr != nil {
		return o.executeErr
	}
	return action(ctx, preflight.Target)
}
func (o *t9Ownership) PreflightBatch(context.Context, []OwnershipTarget) (OwnershipBatchPreflight, error) {
	return o.batch, nil
}
func (o *t9Ownership) ExecuteBatch(ctx context.Context, preflight OwnershipBatchPreflight, action func(context.Context, OwnershipTarget) error) error {
	if o.batchExecuteErr != nil {
		return o.batchExecuteErr
	}
	for _, target := range preflight.Targets {
		if err := action(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

type t9PreviewURLResolver struct {
	identities []MediaIdentity
}

func (r *t9PreviewURLResolver) Resolve(_ context.Context, _ *node.Node, identity MediaIdentity, _ string, token string) (string, error) {
	r.identities = append(r.identities, identity)
	return "https://media.example/stream?media_access_token=" + token, nil
}

func t9ManagementCode(err error) ManagementErrorCode {
	managementErr, ok := AsManagementError(err)
	if !ok {
		return ""
	}
	return managementErr.Code
}

var _ = errors.Is
var _ = time.Second
