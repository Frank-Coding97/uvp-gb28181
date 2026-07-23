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
	list []zlm.MediaInfo
	err  error
}

func (f fakeMediaClient) GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error) {
	return f.list, f.err
}

func TestServiceReturnsRealMediaSnapshot(t *testing.T) {
	loss := 0.025
	locations := &fakeLocations{bindings: map[string]int64{"stream-1": 1}}
	nodes := fakeNodes{items: map[int64]*node.Node{1: {ID: 1, Name: "edge-1", Host: "10.0.0.1"}}}
	tracks := []zlm.MediaTrack{
		{CodecType: 0, CodecIDName: "H264", Width: 1920, Height: 1080, FPS: 25, Frames: 100, KeyFrames: 4, GOPSize: 25, GOPIntervalMS: 1000, Loss: &loss},
		{CodecType: 1, CodecIDName: "PCMA", SampleRate: 8000, Channels: 1, SampleBit: 16, Frames: 200},
	}
	// 同一路流会拆成多 schema:rtsp 端 0 观众,rtmp/flv 端 2 观众,聚合后应为 2;
	// 码率取各 schema 的最大值(浏览器 flv 拉流带宽最高)。
	service := NewService(nodes, locations, func(*node.Node) MediaClient {
		return fakeMediaClient{list: []zlm.MediaInfo{
			{Online: true, Schema: "rtsp", AliveSecond: 42, BytesSpeed: 220000, TotalBytes: 9000000, ReaderCount: 0, TotalReaderCount: 5, Tracks: tracks},
			{Online: true, Schema: "rtmp", AliveSecond: 42, BytesSpeed: 250000, TotalBytes: 9200000, ReaderCount: 2, TotalReaderCount: 7, IsRecordingMP4: true, Tracks: tracks},
			{Online: true, Schema: "hls", AliveSecond: 42, BytesSpeed: 180000, TotalBytes: 8500000, ReaderCount: 1, TotalReaderCount: 3, Tracks: tracks},
		}}
	}, func() time.Time { return time.Unix(1700000000, 0) })

	snapshot, err := service.Get(context.Background(), "stream-1")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != StatusOnline || snapshot.Quality.BitrateKbps != 2000 || snapshot.Network.ReaderCount != 3 || snapshot.Network.AliveSecond != 42 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if snapshot.Network.TotalReaderCount != 7 || snapshot.Network.BytesSpeed != 250000 || snapshot.Network.TotalBytes != 9200000 {
		t.Fatalf("unexpected network aggregation: %+v", snapshot.Network)
	}
	if !snapshot.Recording.MP4 {
		t.Fatalf("recording flag should merge across schemas: %+v", snapshot.Recording)
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
		return fakeMediaClient{list: []zlm.MediaInfo{{Online: true, Schema: "rtsp"}}}
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
		return fakeMediaClient{list: nil}
	}, time.Now)

	if _, err := service.Get(context.Background(), "broken"); !errors.Is(err, ErrNodeUnavailable) {
		t.Fatalf("err=%v, want ErrNodeUnavailable", err)
	}
	if _, err := service.Get(context.Background(), "offline"); !errors.Is(err, ErrStreamOffline) {
		t.Fatalf("err=%v, want ErrStreamOffline", err)
	}
}
