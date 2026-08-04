package playback

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

const playbackZLMApp = "rtp"

type PlaybackNodeScheduler interface {
	Pick(context.Context, scheduler.InviteContext) (*node.Node, error)
}

type PlaybackNodeLookup interface {
	Get(int64) (*node.Node, bool)
}

type PlaybackLocationStore interface {
	Bind(string, int64)
	Lookup(string) (int64, bool)
	Unbind(string)
}

type PlaybackServerConfigProvider interface {
	Get(context.Context, int64) (node.ServerConfig, error)
}

type PlaybackZLMClient interface {
	OpenRtpServer(context.Context, string, int, int, int) (*zlm.OpenRtpServerResult, error)
	CloseRtpServer(context.Context, string) error
	IsMediaOnline(context.Context, string, string) (bool, error)
	GetMediaInfo(context.Context, string, string, string, string) (*zlm.MediaInfo, error)
}

type PlaybackClientFactory func(*node.Node) PlaybackZLMClient

func defaultPlaybackClientFactory(value *node.Node) PlaybackZLMClient {
	return zlm.NewClientForNode(value)
}

type zlmNodePicker struct {
	scheduler PlaybackNodeScheduler
	serverID  string
}

func NewZLMNodePicker(value PlaybackNodeScheduler, serverID string) NodePicker {
	return &zlmNodePicker{scheduler: value, serverID: serverID}
}

func (p *zlmNodePicker) Pick(ctx context.Context, request PickRequest) (NodeInfo, error) {
	if p == nil || p.scheduler == nil {
		return NodeInfo{}, ErrNodeUnavailable
	}
	selected, err := p.scheduler.Pick(ctx, scheduler.InviteContext{DeviceID: request.DeviceID,
		ChannelID: request.SIPChannelID, StreamID: request.StreamID})
	if err != nil {
		return NodeInfo{}, err
	}
	if selected == nil {
		return NodeInfo{}, ErrNodeUnavailable
	}
	return NodeInfo{ID: strconv.FormatInt(selected.ID, 10), DeviceID: request.DeviceID, ServerID: p.serverID,
		Destination: request.Destination, Transport: request.Transport, RecvIP: selected.Host, TCPMode: request.TCPMode}, nil
}

type zlmRTPOpener struct {
	nodes     PlaybackNodeLookup
	locations PlaybackLocationStore
	client    PlaybackClientFactory
}

func NewZLMRTPOpener(nodes PlaybackNodeLookup, locations PlaybackLocationStore, client PlaybackClientFactory) RTPOpener {
	if client == nil {
		client = defaultPlaybackClientFactory
	}
	return &zlmRTPOpener{nodes: nodes, locations: locations, client: client}
}

func (o *zlmRTPOpener) Open(ctx context.Context, request RTPRequest) (RTPAllocation, error) {
	if o == nil || o.nodes == nil || o.locations == nil {
		return RTPAllocation{}, ErrRTPUnavailable
	}
	nodeID, err := strconv.ParseInt(request.NodeID, 10, 64)
	if err != nil {
		return RTPAllocation{}, fmt.Errorf("%w: invalid node id", ErrRTPUnavailable)
	}
	selected, ok := o.nodes.Get(nodeID)
	if !ok {
		return RTPAllocation{}, fmt.Errorf("%w: node not found", ErrRTPUnavailable)
	}
	client := o.client(selected)
	tcpMode := 0
	if request.TCPMode {
		tcpMode = 1
	}
	opened, err := client.OpenRtpServer(ctx, request.StreamID, request.Port, tcpMode, 0)
	if err != nil {
		return RTPAllocation{}, err
	}
	return RTPAllocation{StreamID: request.StreamID, SSRC: request.SSRC, Port: opened.Port,
		Bind:   func() error { o.locations.Bind(request.StreamID, nodeID); return nil },
		Close:  func(closeCtx context.Context) error { return client.CloseRtpServer(closeCtx, request.StreamID) },
		Unbind: func() error { o.locations.Unbind(request.StreamID); return nil }}, nil
}

type zlmMediaWaiter struct {
	nodes        PlaybackNodeLookup
	locations    PlaybackLocationStore
	notifier     *stream.Notifier
	configs      PlaybackServerConfigProvider
	client       PlaybackClientFactory
	pollInterval time.Duration
}

func NewZLMMediaWaiter(nodes PlaybackNodeLookup, locations PlaybackLocationStore, notifier *stream.Notifier,
	configs PlaybackServerConfigProvider, client PlaybackClientFactory) MediaWaiter {
	if client == nil {
		client = defaultPlaybackClientFactory
	}
	return &zlmMediaWaiter{nodes: nodes, locations: locations, notifier: notifier, configs: configs,
		client: client, pollInterval: 200 * time.Millisecond}
}

func (w *zlmMediaWaiter) Wait(ctx context.Context, streamID string) (MediaReady, error) {
	if w == nil || w.nodes == nil || w.locations == nil || w.notifier == nil || w.configs == nil {
		return MediaReady{}, ErrMediaWait
	}
	nodeID, ok := w.locations.Lookup(streamID)
	if !ok {
		return MediaReady{}, fmt.Errorf("%w: stream binding missing", ErrMediaWait)
	}
	selected, ok := w.nodes.Get(nodeID)
	if !ok {
		return MediaReady{}, fmt.Errorf("%w: node not found", ErrMediaWait)
	}
	client := w.client(selected)
	poll := func(pollCtx context.Context) (bool, error) {
		return client.IsMediaOnline(pollCtx, playbackZLMApp, streamID)
	}
	if err := stream.WaitReady(ctx, w.notifier, streamID, poll, w.pollInterval); err != nil {
		return MediaReady{}, err
	}
	config, err := w.configs.Get(ctx, nodeID)
	if err != nil {
		return MediaReady{}, err
	}
	ready := MediaReady{URLs: playbackURLs(selected.Host, config, playbackZLMApp, streamID)}
	if info, infoErr := client.GetMediaInfo(ctx, "rtsp", "__defaultVhost__", playbackZLMApp, streamID); infoErr == nil && info != nil {
		for _, track := range info.Tracks {
			if track.CodecType == 1 && track.Ready {
				ready.HasAudio = true
				break
			}
		}
	}
	return ready, nil
}

func playbackURLs(host string, config node.ServerConfig, appName, streamID string) map[string]string {
	result := make(map[string]string)
	escapedApp, escapedStream := url.PathEscape(appName), url.PathEscape(streamID)
	if config.HTTPPort > 0 {
		base := fmt.Sprintf("%s:%d/%s/%s", host, config.HTTPPort, escapedApp, escapedStream)
		result["wsFlv"] = "ws://" + base + ".live.flv"
		result["httpFlv"] = "http://" + base + ".live.flv"
		result["hls"] = "http://" + base + "/hls.m3u8"
		query := url.Values{"app": {appName}, "stream": {streamID}, "type": {"play"}}
		result["webrtc"] = fmt.Sprintf("http://%s:%d/index/api/webrtc?%s", host, config.HTTPPort, query.Encode())
	}
	if config.RTMPPort > 0 {
		result["rtmp"] = fmt.Sprintf("rtmp://%s:%d/%s/%s", host, config.RTMPPort, escapedApp, escapedStream)
	}
	if config.RTSPPort > 0 {
		result["rtsp"] = fmt.Sprintf("rtsp://%s:%d/%s/%s", host, config.RTSPPort, escapedApp, escapedStream)
	}
	return result
}
