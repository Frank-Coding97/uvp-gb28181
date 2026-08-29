package management

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestSessionServiceSeparatesNetworkSessionsAndMediaViewers(t *testing.T) {
	identity := t9Identity("sessions")
	runtime := &t9RuntimeReader{
		sessions: map[int64][]zlm.Session{1: {{ID: "network-1", Identifier: "network-id", PeerIP: "192.0.2.1", LocalPort: 1935, Type: "TcpSession"}}},
		players:  map[string][]zlm.MediaPlayer{t9MediaKey(1, identity): {{Identifier: "viewer-1", TypeID: "TcpSession"}}},
	}
	service := NewSessionService(SessionServiceDependencies{Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}}, Runtime: runtime})

	network, err := service.ListNetworkSessions(context.Background(), NetworkSessionListRequest{NodeID: 1, Page: PageRequest{Page: 1, PageSize: 10}})
	require.NoError(t, err)
	require.Len(t, network.List, 1)
	require.Equal(t, "network-1", network.List[0].ID)
	encoded, marshalErr := json.Marshal(network)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(encoded), "media")

	viewers, err := service.ListMediaViewers(context.Background(), MediaViewerListRequest{NodeID: 1, Media: identity, Page: PageRequest{Page: 1, PageSize: 10}})
	require.NoError(t, err)
	require.Len(t, viewers.List, 1)
	require.Equal(t, "viewer-1", viewers.List[0].Identifier)
	require.Equal(t, identity, viewers.List[0].Media)
	require.NotEqual(t, network.List[0].ID, viewers.List[0].Identifier)
}

func TestSessionServiceKickRequiresFreshTargetProofAndReread(t *testing.T) {
	identity := t9Identity("kick")
	fresh := &t9FreshReader{players: map[string][][]zlm.MediaPlayer{t9MediaKey(1, identity): {
		{{Identifier: "viewer-1", TypeID: "TcpSession"}},
		{},
	}}}
	action := &t9MediaAction{}
	service := NewSessionService(SessionServiceDependencies{
		Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}}, Fresh: fresh, Executor: action,
	})

	result, err := service.KickSession(context.Background(), KickSessionRequest{NodeID: 1, Media: identity, Identifier: "viewer-1"})
	require.NoError(t, err)
	require.True(t, result.Kicked)
	require.False(t, result.AlreadyDisconnected)
	require.Equal(t, 1, action.kickCalls)
}

func TestSessionServiceKickRejectsForeignOrDisconnectedIdentifierWithoutUpstream(t *testing.T) {
	identity := t9Identity("proof")
	fresh := &t9FreshReader{players: map[string][][]zlm.MediaPlayer{t9MediaKey(1, identity): {
		{{Identifier: "other-id", TypeID: "TcpSession"}},
	}}}
	action := &t9MediaAction{}
	service := NewSessionService(SessionServiceDependencies{Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}}, Fresh: fresh, Executor: action})

	result, err := service.KickSession(context.Background(), KickSessionRequest{NodeID: 1, Media: identity, Identifier: "foreign-id"})
	require.NoError(t, err)
	require.True(t, result.AlreadyDisconnected)
	require.Zero(t, action.kickCalls)

	fresh.players[t9MediaKey(1, identity)] = [][]zlm.MediaPlayer{{}}
	result, err = service.KickSession(context.Background(), KickSessionRequest{NodeID: 1, Media: identity, Identifier: "viewer-1"})
	require.NoError(t, err)
	require.True(t, result.AlreadyDisconnected)
	require.Zero(t, action.kickCalls)
}

func TestSessionServiceKickDoesNotMatchIdentifierAcrossSchema(t *testing.T) {
	rtsp := t9Identity("same-stream")
	rtmp := rtsp
	rtmp.Schema = "rtmp"
	fresh := &t9FreshReader{players: map[string][][]zlm.MediaPlayer{
		t9MediaKey(1, rtsp): {{
			{Identifier: "shared-id", TypeID: "TcpSession"},
		}},
		t9MediaKey(1, rtmp): {{}},
	}}
	action := &t9MediaAction{}
	service := NewSessionService(SessionServiceDependencies{Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}}, Fresh: fresh, Executor: action})

	result, err := service.KickSession(context.Background(), KickSessionRequest{NodeID: 1, Media: rtmp, Identifier: "shared-id"})
	require.NoError(t, err)
	require.True(t, result.AlreadyDisconnected)
	require.Zero(t, action.kickCalls)
}

func TestSessionServiceKickStillPresentIsUncertainAndRetryable(t *testing.T) {
	identity := t9Identity("uncertain")
	fresh := &t9FreshReader{players: map[string][][]zlm.MediaPlayer{t9MediaKey(1, identity): {
		{{Identifier: "viewer-1", TypeID: "TcpSession"}},
		{{Identifier: "viewer-1", TypeID: "TcpSession"}},
	}}}
	action := &t9MediaAction{}
	service := NewSessionService(SessionServiceDependencies{Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}}, Fresh: fresh, Executor: action})

	result, err := service.KickSession(context.Background(), KickSessionRequest{NodeID: 1, Media: identity, Identifier: "viewer-1"})
	require.Error(t, err)
	require.True(t, result.Uncertain)
	require.True(t, result.Retryable)
	require.Equal(t, 1, action.kickCalls)
}

func TestSessionServiceKickDoesNotTreatUDPAsKickable(t *testing.T) {
	identity := t9Identity("udp")
	fresh := &t9FreshReader{players: map[string][][]zlm.MediaPlayer{
		t9MediaKey(1, identity): {{
			{Identifier: "udp-1", TypeID: "UdpServer"},
		}},
	}}
	action := &t9MediaAction{}
	service := NewSessionService(SessionServiceDependencies{Registry: &t9NodeRegistry{nodes: []*node.Node{t9Node(1, node.StateActive)}}, Fresh: fresh, Executor: action})

	_, err := service.KickSession(context.Background(), KickSessionRequest{NodeID: 1, Media: identity, Identifier: "udp-1"})
	require.Error(t, err)
	require.Zero(t, action.kickCalls)
}
