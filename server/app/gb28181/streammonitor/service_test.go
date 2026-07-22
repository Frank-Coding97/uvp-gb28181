package streammonitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type fakeLocations struct {
	bindings map[string]int64
}

func (f *fakeLocations) Lookup(streamID string) (int64, bool) {
	id, ok := f.bindings[streamID]
	return id, ok
}
func (f *fakeLocations) Bind(streamID string, nodeID int64) { f.bindings[streamID] = nodeID }

type fakeNodes struct {
	items map[int64]*node.Node
}

func (f fakeNodes) Get(id int64) (*node.Node, bool) {
	n, ok := f.items[id]
	return n, ok
}
func (f fakeNodes) ListActive() []*node.Node {
	result := make([]*node.Node, 0, len(f.items))
	for _, n := range f.items {
		result = append(result, n)
	}
	return result
}

type fakeMediaClient struct {
	info *zlm.MediaInfo
	err  error
}

func (f fakeMediaClient) GetMediaInfo(context.Context, string, string, string, string) (*zlm.MediaInfo, error) {
	return f.info, f.err
}

func TestServiceReturnsRealMediaSnapshot(t *testing.T) {
	loss := 0.025
	locations := &fakeLocations{bindings: map[string]int64{"stream-1": 1}}
	nodes := fakeNodes{items: map[int64]*node.Node{1: {ID: 1, Name: "edge-1", Host: "10.0.0.1"}}}
	service := NewService(nodes, locations, func(*node.Node) MediaClient {
		return fakeMediaClient{info: &zlm.MediaInfo{
			Online: true, AliveSecond: 42, BytesSpeed: 250000, TotalBytes: 9000000,
			ReaderCount: 3, TotalReaderCount: 7, IsRecordingMP4: true,
			Tracks: []zlm.MediaTrack{
				{CodecType: 0, CodecIDName: "H264", Width: 1920, Height: 1080, FPS: 25, Frames: 100, KeyFrames: 4, GOPSize: 25, GOPIntervalMS: 1000, Loss: &loss},
				{CodecType: 1, CodecIDName: "PCMA", SampleRate: 8000, Channels: 1, SampleBit: 16, Frames: 200},
			},
		}}
	}, func() time.Time { return time.Unix(1700000000, 0) })

	snapshot, err := service.Get(context.Background(), "stream-1")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != StatusOnline || snapshot.Quality.BitrateKbps != 2000 || snapshot.Network.ReaderCount != 3 || snapshot.Network.AliveSecond != 42 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if snapshot.CollectedAt.Unix() != 1700000000 || snapshot.Node.ID != 1 || len(snapshot.Tracks) != 2 {
		t.Fatalf("unexpected metadata: %+v", snapshot)
	}
	if snapshot.Tracks[0].Loss == nil || *snapshot.Tracks[0].Loss != loss || snapshot.Tracks[1].Loss != nil {
		t.Fatalf("unexpected track loss: %+v", snapshot.Tracks)
	}
}

func TestServiceRecoversMissingLocationBinding(t *testing.T) {
	locations := &fakeLocations{bindings: map[string]int64{}}
	nodes := fakeNodes{items: map[int64]*node.Node{7: {ID: 7, Name: "edge-7"}}}
	service := NewService(nodes, locations, func(*node.Node) MediaClient {
		return fakeMediaClient{info: &zlm.MediaInfo{Online: true}}
	}, time.Now)

	if _, err := service.Get(context.Background(), "stream-7"); err != nil {
		t.Fatal(err)
	}
	if id, ok := locations.Lookup("stream-7"); !ok || id != 7 {
		t.Fatalf("binding not recovered: %d %v", id, ok)
	}
}

func TestServiceDistinguishesNodeUnavailableAndStreamOffline(t *testing.T) {
	locations := &fakeLocations{bindings: map[string]int64{"broken": 1, "offline": 2}}
	nodes := fakeNodes{items: map[int64]*node.Node{1: {ID: 1}, 2: {ID: 2}}}
	service := NewService(nodes, locations, func(n *node.Node) MediaClient {
		if n.ID == 1 {
			return fakeMediaClient{err: errors.New("dial failed")}
		}
		return fakeMediaClient{info: &zlm.MediaInfo{Online: false}}
	}, time.Now)

	if _, err := service.Get(context.Background(), "broken"); !errors.Is(err, ErrNodeUnavailable) {
		t.Fatalf("err=%v, want ErrNodeUnavailable", err)
	}
	if _, err := service.Get(context.Background(), "offline"); !errors.Is(err, ErrStreamOffline) {
		t.Fatalf("err=%v, want ErrStreamOffline", err)
	}
}
