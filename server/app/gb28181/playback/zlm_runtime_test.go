package playback

import (
	"context"
	"sync/atomic"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

type fakePlaybackScheduler struct{ selected *node.Node }

func (f fakePlaybackScheduler) Pick(context.Context, scheduler.InviteContext) (*node.Node, error) {
	return f.selected, nil
}

type fakePlaybackNodes struct{ selected *node.Node }

func (f fakePlaybackNodes) Get(id int64) (*node.Node, bool) {
	return f.selected, f.selected != nil && f.selected.ID == id
}

type fakePlaybackZLMClient struct {
	openCalls, closeCalls atomic.Int32
}

func (f *fakePlaybackZLMClient) OpenRtpServer(context.Context, string, int, int, int) (*zlm.OpenRtpServerResult, error) {
	f.openCalls.Add(1)
	return &zlm.OpenRtpServerResult{Port: 30000}, nil
}
func (f *fakePlaybackZLMClient) CloseRtpServer(context.Context, string) error {
	f.closeCalls.Add(1)
	return nil
}
func (f *fakePlaybackZLMClient) IsMediaOnline(context.Context, string, string) (bool, error) {
	return true, nil
}
func (f *fakePlaybackZLMClient) GetMediaInfo(context.Context, string, string, string, string) (*zlm.MediaInfo, error) {
	return &zlm.MediaInfo{Online: true, Tracks: []zlm.MediaTrack{{CodecType: 1, Ready: true}}}, nil
}

type fakePlaybackServerConfigs struct{}

func (fakePlaybackServerConfigs) Get(context.Context, int64) (node.ServerConfig, error) {
	return node.ServerConfig{HTTPPort: 8080, RTSPPort: 554}, nil
}

func TestZLMRuntimeAdaptersPickAllocateWaitAndCleanup(t *testing.T) {
	selected := &node.Node{ID: 7, Host: "192.0.2.10", ReceiveHost: "198.51.100.10", PlaybackHost: "play.example.com", State: node.StateActive}
	client := &fakePlaybackZLMClient{}
	clientForNode := func(*node.Node) PlaybackZLMClient { return client }
	locations := stream.NewLocationMap()
	picker := NewZLMNodePicker(fakePlaybackScheduler{selected: selected}, "34020000002000000001")
	picked, err := picker.Pick(context.Background(), PickRequest{DeviceID: "device-1", SIPChannelID: "channel-1", StreamID: "pb-1", Destination: "192.0.2.20:5060", Transport: "UDP"})
	if err != nil || picked.ID != "7" || picked.RecvIP != selected.ReceiveHost || picked.Destination == "" {
		t.Fatalf("picked=%+v err=%v", picked, err)
	}

	opener := NewZLMRTPOpener(fakePlaybackNodes{selected: selected}, locations, clientForNode)
	allocation, err := opener.Open(context.Background(), RTPRequest{NodeID: picked.ID, StreamID: "pb-1", SSRC: "1000000001"})
	if err != nil || allocation.Port != 30000 {
		t.Fatalf("allocation=%+v err=%v", allocation, err)
	}
	if err := allocation.Bind(); err != nil {
		t.Fatal(err)
	}
	if nodeID, ok := locations.Lookup("pb-1"); !ok || nodeID != selected.ID {
		t.Fatalf("binding node=%d ok=%v", nodeID, ok)
	}

	waiter := NewZLMMediaWaiter(fakePlaybackNodes{selected: selected}, locations, stream.NewNotifier(),
		fakePlaybackServerConfigs{}, clientForNode)
	ready, err := waiter.Wait(context.Background(), "pb-1")
	if err != nil || ready.URLs["wsFlv"] != "ws://play.example.com:8080/rtp/pb-1.live.flv" || ready.URLs["rtsp"] != "rtsp://play.example.com:554/rtp/pb-1" || !ready.HasAudio {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	if err := allocation.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := allocation.Unbind(); err != nil {
		t.Fatal(err)
	}
	if client.openCalls.Load() != 1 || client.closeCalls.Load() != 1 {
		t.Fatalf("open=%d close=%d", client.openCalls.Load(), client.closeCalls.Load())
	}
}

var _ PlaybackServerConfigProvider = fakePlaybackServerConfigs{}
