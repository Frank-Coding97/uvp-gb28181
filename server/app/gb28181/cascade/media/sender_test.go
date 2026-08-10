package media

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestSenderStartsVideoPSOnSourceNode(t *testing.T) {
	client := &fakeGBSenderClient{startResult: &zlm.GBSendRTPResult{LocalPort: 31000}}
	sender := NewSender(fakeSenderNodes{items: map[int64]*node.Node{
		4: {ID: 4, APISecret: "node-secret"},
	}}, func(value *node.Node) GBSenderClient {
		require.Equal(t, int64(4), value.ID)
		return client
	})

	lease, err := sender.Start(context.Background(), SenderRequest{
		Source: StartResult{StreamID: "source-1", App: "rtp", NodeID: 4},
		SSRC:   "0200000001", PayloadType: 96, RemoteIP: "192.0.2.20", RemotePort: 30000,
		Transport: zlm.GBSendRTPUDP,
	})
	require.NoError(t, err)
	require.Equal(t, 31000, lease.LocalPort)
	require.Equal(t, zlm.GBSendRTPRequest{
		VHost: "__defaultVhost__", App: "rtp", Stream: "source-1", SSRC: "0200000001",
		PayloadType: 96, RemoteIP: "192.0.2.20", RemotePort: 30000, Transport: zlm.GBSendRTPUDP,
	}, client.start)
}

func TestSenderUsesExactSourceNodeAndRejectsMissingNode(t *testing.T) {
	client := &fakeGBSenderClient{startResult: &zlm.GBSendRTPResult{LocalPort: 31000}}
	sender := NewSender(fakeSenderNodes{items: map[int64]*node.Node{
		1: {ID: 1},
	}}, func(*node.Node) GBSenderClient { return client })

	_, err := sender.Start(context.Background(), SenderRequest{
		Source: StartResult{StreamID: "source-1", App: "rtp", NodeID: 2},
		SSRC:   "0200000001", PayloadType: 96, RemoteIP: "192.0.2.20", RemotePort: 30000,
		Transport: zlm.GBSendRTPUDP,
	})
	require.ErrorIs(t, err, ErrSenderNodeUnavailable)
	require.Zero(t, client.startCalls)
}

func TestSenderStopIsScopedAndIdempotent(t *testing.T) {
	client := &fakeGBSenderClient{startResult: &zlm.GBSendRTPResult{LocalPort: 31000}}
	sender := NewSender(fakeSenderNodes{items: map[int64]*node.Node{1: {ID: 1}}}, func(*node.Node) GBSenderClient { return client })
	lease, err := sender.Start(context.Background(), SenderRequest{
		Source: StartResult{StreamID: "source-1", App: "rtp", NodeID: 1},
		SSRC:   "0200000001", PayloadType: 96, RemoteIP: "192.0.2.20", RemotePort: 30000,
		Transport: zlm.GBSendRTPUDP,
	})
	require.NoError(t, err)
	require.NoError(t, lease.Stop(context.Background()))
	require.NoError(t, lease.Stop(context.Background()))
	require.Equal(t, 1, client.stopCalls)
	require.Equal(t, senderStopCall{vhost: "__defaultVhost__", app: "rtp", stream: "source-1", ssrc: "0200000001"}, client.stop)
}

func TestSenderClassifiesAndRedactsStartFailure(t *testing.T) {
	client := &fakeGBSenderClient{startErr: errors.New("ZLM request failed secret=node-secret")}
	sender := NewSender(fakeSenderNodes{items: map[int64]*node.Node{1: {ID: 1, APISecret: "node-secret"}}}, func(*node.Node) GBSenderClient { return client })

	_, err := sender.Start(context.Background(), SenderRequest{
		Source: StartResult{StreamID: "source-1", App: "rtp", NodeID: 1},
		SSRC:   "0200000001", PayloadType: 96, RemoteIP: "192.0.2.20", RemotePort: 30000,
		Transport: zlm.GBSendRTPUDP,
	})
	require.ErrorIs(t, err, ErrSenderStartFailed)
	require.NotContains(t, err.Error(), "node-secret")
}

func TestSenderRejectsInvalidInputBeforeClient(t *testing.T) {
	client := &fakeGBSenderClient{}
	sender := NewSender(fakeSenderNodes{items: map[int64]*node.Node{1: {ID: 1}}}, func(*node.Node) GBSenderClient { return client })

	_, err := sender.Start(context.Background(), SenderRequest{
		Source: StartResult{StreamID: "source-1", App: "rtp", NodeID: 1},
		SSRC:   "", PayloadType: 96, RemoteIP: "192.0.2.20", RemotePort: 30000, Transport: zlm.GBSendRTPUDP,
	})
	require.ErrorIs(t, err, ErrInvalidSenderRequest)
	require.Zero(t, client.startCalls)
}

type fakeSenderNodes struct{ items map[int64]*node.Node }

func (f fakeSenderNodes) Get(id int64) (*node.Node, bool) {
	item, ok := f.items[id]
	return item, ok
}

type senderStopCall struct {
	vhost, app, stream, ssrc string
}

type fakeGBSenderClient struct {
	start       zlm.GBSendRTPRequest
	startResult *zlm.GBSendRTPResult
	startErr    error
	startCalls  int
	stop        senderStopCall
	stopErr     error
	stopCalls   int
}

func (f *fakeGBSenderClient) StartGBSendRTP(_ context.Context, request zlm.GBSendRTPRequest) (*zlm.GBSendRTPResult, error) {
	f.startCalls++
	f.start = request
	return f.startResult, f.startErr
}

func (f *fakeGBSenderClient) StopSendRtp(_ context.Context, vhost, app, stream, ssrc string) error {
	f.stopCalls++
	f.stop = senderStopCall{vhost: vhost, app: app, stream: stream, ssrc: ssrc}
	return f.stopErr
}
