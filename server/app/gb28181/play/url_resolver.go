package play

import (
	"context"
	"fmt"
	"net/url"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// ServerConfigProvider returns the media ports exposed by one ZLM node.
type ServerConfigProvider interface {
	Get(context.Context, int64) (node.ServerConfig, error)
}

// PlaybackURLs contains only protocols actually exposed by the selected node.
type PlaybackURLs struct {
	WSFLV   *string `json:"wsFlv"`
	HTTPFLV *string `json:"httpFlv"`
	HLS     *string `json:"hls"`
	WebRTC  *string `json:"webrtc"`
	RTMP    *string `json:"rtmp"`
	RTSP    *string `json:"rtsp"`
}

// URLResolver builds browser and diagnostic playback URLs from ZLM runtime config.
type URLResolver struct {
	configs ServerConfigProvider
}

func NewURLResolver(configs ServerConfigProvider) *URLResolver {
	return &URLResolver{configs: configs}
}

func (r *URLResolver) Resolve(ctx context.Context, mediaNode *node.Node, app, stream string) (PlaybackURLs, []string) {
	if r == nil || r.configs == nil || mediaNode == nil {
		return PlaybackURLs{}, []string{"播放地址解析器未配置"}
	}
	cfg, err := r.configs.Get(ctx, mediaNode.ID)
	if err != nil {
		return PlaybackURLs{}, []string{"读取节点媒体端口失败"}
	}

	escapedApp := url.PathEscape(app)
	escapedStream := url.PathEscape(stream)
	var urls PlaybackURLs
	if cfg.HTTPPort > 0 {
		base := fmt.Sprintf("%s:%d/%s/%s", mediaNode.Host, cfg.HTTPPort, escapedApp, escapedStream)
		urls.WSFLV = stringPtr("ws://" + base + ".live.flv")
		urls.HTTPFLV = stringPtr("http://" + base + ".live.flv")
		urls.HLS = stringPtr("http://" + base + "/hls.m3u8")
		query := url.Values{"app": {app}, "stream": {stream}, "type": {"play"}}
		urls.WebRTC = stringPtr(fmt.Sprintf("http://%s:%d/index/api/webrtc?%s", mediaNode.Host, cfg.HTTPPort, query.Encode()))
	}
	if cfg.RTMPPort > 0 {
		urls.RTMP = stringPtr(fmt.Sprintf("rtmp://%s:%d/%s/%s", mediaNode.Host, cfg.RTMPPort, escapedApp, escapedStream))
	}
	if cfg.RTSPPort > 0 {
		urls.RTSP = stringPtr(fmt.Sprintf("rtsp://%s:%d/%s/%s", mediaNode.Host, cfg.RTSPPort, escapedApp, escapedStream))
	}
	return urls, nil
}

func stringPtr(value string) *string { return &value }
