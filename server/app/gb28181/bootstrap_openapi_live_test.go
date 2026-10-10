package gb28181

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gbzlm "uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	openapiplay "uvplatform.com/uvp-gb28181/app/openapi/play"
)

type openAPIViewerNodeResolverStub struct {
	node *node.Node
}

func (s openAPIViewerNodeResolverStub) GetByUUID(string) (*node.Node, bool) {
	return s.node, s.node != nil
}

type openAPIViewerRuntimeClientStub struct {
	snapshot       gbzlm.RuntimePlayers
	err            error
	target         gbzlm.StreamTarget
	standard       []gbzlm.MediaPlayer
	standardErr    error
	standardCalled bool
}

func (s *openAPIViewerRuntimeClientStub) GetMediaPlayerList(_ context.Context, schema, vhost, app, stream string) ([]gbzlm.MediaPlayer, error) {
	s.standardCalled = true
	s.target = gbzlm.StreamTarget{Schema: schema, VHost: vhost, App: app, Stream: stream}
	return s.standard, s.standardErr
}

func (s *openAPIViewerRuntimeClientStub) GetRuntimeMediaPlayers(_ context.Context, target gbzlm.StreamTarget) (gbzlm.RuntimePlayers, error) {
	s.target = target
	return s.snapshot, s.err
}

func TestOpenAPIViewerRuntimeSnapshotterMapsExactNodeAndPlayers(t *testing.T) {
	client := &openAPIViewerRuntimeClientStub{snapshot: gbzlm.RuntimePlayers{
		BootNonce: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Players:   []gbzlm.MediaPlayer{{Identifier: "viewer-a"}, {Identifier: "viewer-b"}},
	}}
	snapshotter := &openAPIViewerRuntimeSnapshotter{
		registry:  openAPIViewerNodeResolverStub{node: &node.Node{ID: 1, MediaServerUUID: "node-a"}},
		newClient: func(*node.Node) openAPIViewerRuntimeClient { return client },
	}
	target := openapiplay.ViewerRuntimeTarget{NodeUUID: "node-a", Schema: "http", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-a"}

	snapshot, err := snapshotter.SnapshotViewerRuntime(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, target.Schema, client.target.Schema)
	require.Equal(t, target.VHost, client.target.VHost)
	require.Equal(t, target.App, client.target.App)
	require.Equal(t, target.Stream, client.target.Stream)
	require.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", snapshot.BootNonce)
	require.Contains(t, snapshot.Identifiers, "viewer-a")
	require.Contains(t, snapshot.Identifiers, "viewer-b")
}

func TestOpenAPIViewerRuntimeSnapshotterFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resolver openAPIViewerNodeResolver
		client   *openAPIViewerRuntimeClientStub
		nodeUUID string
	}{
		{name: "missing node", resolver: openAPIViewerNodeResolverStub{}, nodeUUID: "node-a"},
		{name: "mismatched node", resolver: openAPIViewerNodeResolverStub{node: &node.Node{ID: 1, MediaServerUUID: "node-b"}}, nodeUUID: "node-a"},
		{name: "runtime failure", resolver: openAPIViewerNodeResolverStub{node: &node.Node{ID: 1, MediaServerUUID: "node-a"}}, client: &openAPIViewerRuntimeClientStub{err: errors.New("unavailable"), standardErr: errors.New("unavailable")}, nodeUUID: "node-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshotter := &openAPIViewerRuntimeSnapshotter{registry: tc.resolver}
			if tc.client != nil {
				snapshotter.newClient = func(*node.Node) openAPIViewerRuntimeClient { return tc.client }
			}
			_, err := snapshotter.SnapshotViewerRuntime(context.Background(), openapiplay.ViewerRuntimeTarget{
				NodeUUID: tc.nodeUUID, Schema: "http", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-a",
			})
			require.Error(t, err)
		})
	}
}

func TestOpenAPIViewerRuntimeSnapshotterFallsBackToExactStandardTarget(t *testing.T) {
	for _, tc := range []struct {
		name        string
		players     []gbzlm.MediaPlayer
		standardErr error
	}{
		{name: "existing target", players: []gbzlm.MediaPlayer{{Identifier: "viewer-a"}}},
		{name: "missing target", standardErr: gbzlm.ErrMediaNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &openAPIViewerRuntimeClientStub{err: gbzlm.ErrRuntimeControlUnavailable, standard: tc.players, standardErr: tc.standardErr}
			snapshotter := &openAPIViewerRuntimeSnapshotter{
				registry:  openAPIViewerNodeResolverStub{node: &node.Node{ID: 1, MediaServerUUID: "node-a"}},
				newClient: func(*node.Node) openAPIViewerRuntimeClient { return client },
			}
			snapshot, err := snapshotter.SnapshotViewerRuntime(context.Background(), openapiplay.ViewerRuntimeTarget{NodeUUID: "node-a", Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-a"})
			require.NoError(t, err)
			require.True(t, client.standardCalled)
			require.True(t, snapshot.TargetAuthoritative)
			require.Empty(t, snapshot.BootNonce)
			if len(tc.players) > 0 {
				require.Contains(t, snapshot.Identifiers, "viewer-a")
			} else {
				require.Empty(t, snapshot.Identifiers)
			}
		})
	}
}
