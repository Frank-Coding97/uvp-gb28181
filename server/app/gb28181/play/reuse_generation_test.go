package play

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// plainLocationStore only implements the base LocationStore contract, modelling
// a store that cannot report generations at all.
type plainLocationStore struct {
	nodes map[string]int64
}

func (p *plainLocationStore) Bind(streamID string, nodeID int64) { p.nodes[streamID] = nodeID }
func (p *plainLocationStore) Lookup(streamID string) (int64, bool) {
	nodeID, ok := p.nodes[streamID]
	return nodeID, ok
}
func (p *plainLocationStore) Unbind(streamID string) { delete(p.nodes, streamID) }

func reuseTestService(locations LocationStore) *Service {
	return &Service{
		cfg:         testCfg(),
		sessions:    uac.NewSessionManager(),
		locationMap: locations,
		urlResolver: NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 28080}}),
	}
}

// A stream that is still online keeps the generation its start installed. Work
// recording refuses a zero generation as unattributable, so losing it here makes
// every attempt to record an already-playing channel fail.
func TestReuseResultCarriesTheCurrentLiveGeneration(t *testing.T) {
	locations := stream.NewLocationMap()
	require.True(t, locations.BindCurrent(stream.LiveRef{
		StreamID: "existing", SSRC: "0200000001", Generation: 7, NodeID: 22,
	}))
	service := reuseTestService(locations)
	channel := &gbmodels.GbChannel{StreamID: "existing", CurrentSSRC: "0200000001"}

	for _, tc := range []struct {
		name string
		node *node.Node
	}{
		{name: "node result", node: &node.Node{ID: 22, Name: "edge-22", Host: "10.0.0.22"}},
		{name: "default host result", node: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := service.buildReuseResult(context.Background(), channel, tc.node)
			require.True(t, result.Reused)
			require.EqualValues(t, 7, result.Generation, "复用的在线流必须带上当前代际")
		})
	}
}

func TestReuseResultDoesNotInventAGeneration(t *testing.T) {
	locations := stream.NewLocationMap()
	service := reuseTestService(locations)

	result := service.buildReuseResult(context.Background(), &gbmodels.GbChannel{
		StreamID: "unbound", CurrentSSRC: "0200000002",
	}, &node.Node{ID: 22, Host: "10.0.0.22"})
	require.True(t, result.Reused)
	require.Zero(t, result.Generation, "位置表没有绑定时不得凭空造一个代际")

	// A binding installed without a generation (the recovery fallback) must not
	// be reported as a usable generation either.
	locations.Bind("unbound", 22)
	result = service.buildReuseResult(context.Background(), &gbmodels.GbChannel{
		StreamID: "unbound", CurrentSSRC: "0200000002",
	}, &node.Node{ID: 22, Host: "10.0.0.22"})
	require.Zero(t, result.Generation)
}

func TestReuseResultKeepsZeroWhenTheStoreCannotReportGenerations(t *testing.T) {
	store := &plainLocationStore{nodes: map[string]int64{}}
	store.Bind("existing", 22)
	service := reuseTestService(store)

	result := service.buildReuseResult(context.Background(), &gbmodels.GbChannel{
		StreamID: "existing", CurrentSSRC: "0200000001",
	}, &node.Node{ID: 22, Host: "10.0.0.22"})
	require.True(t, result.Reused)
	require.Zero(t, result.Generation)
}
