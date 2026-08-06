package play

import (
	"context"
	"fmt"
	"net/url"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// ServerConfigProvider returns the media ports exposed by one ZLM node.
type ServerConfigProvider interface {
	Refresh(context.Context, int64) (node.ServerConfig, error)
}

// PlaybackURLs contains only protocols actually exposed by the selected node.
type PlaybackURLs struct {
	WSFLV     *string `json:"wsFlv"`
	HTTPFLV   *string `json:"httpFlv"`
	WSSFLV    *string `json:"wssFlv"`
	HTTPSFLV  *string `json:"httpsFlv"`
	WSFMP4    *string `json:"wsFmp4"`
	HTTPFMP4  *string `json:"httpFmp4"`
	WSSFMP4   *string `json:"wssFmp4"`
	HTTPSFMP4 *string `json:"httpsFmp4"`
	HLS       *string `json:"hls"`
	HTTPSHLS  *string `json:"httpsHls"`
	WSTS      *string `json:"wsTs"`
	HTTPSTS   *string `json:"httpTs"`
	WSSTS     *string `json:"wssTs"`
	HTTPSSTS  *string `json:"httpsTs"`
	WebRTC    *string `json:"webrtc"`
	WebRTCS   *string `json:"webrtcs"`
	RTMP      *string `json:"rtmp"`
	RTMPS     *string `json:"rtmps"`
	RTSP      *string `json:"rtsp"`
	RTSPS     *string `json:"rtsps"`
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
	cfg, err := r.configs.Refresh(ctx, mediaNode.ID)
	if err != nil {
		return PlaybackURLs{}, []string{"读取节点媒体端口失败"}
	}

	escapedApp := url.PathEscape(app)
	escapedStream := url.PathEscape(stream)
	path := escapedApp + "/" + escapedStream
	var urls PlaybackURLs
	if cfg.HTTPPort > 0 {
		base := fmt.Sprintf("%s:%d", mediaNode.Host, cfg.HTTPPort)
		urls.WSFLV = stringPtr("ws://" + base + "/" + path + ".live.flv")
		urls.HTTPFLV = stringPtr("http://" + base + "/" + path + ".live.flv")
		if cfg.FMP4Enabled {
			urls.WSFMP4 = stringPtr("ws://" + base + "/" + path + ".live.mp4")
			urls.HTTPFMP4 = stringPtr("http://" + base + "/" + path + ".live.mp4")
		}
		if cfg.HLSEnabled {
			urls.HLS = stringPtr("http://" + base + "/" + escapedApp + "/" + escapedStream + "/hls.m3u8")
		}
		if cfg.TSEnabled {
			urls.WSTS = stringPtr("ws://" + base + "/" + path + ".live.ts")
			urls.HTTPSTS = stringPtr("http://" + base + "/" + path + ".live.ts")
		}
		query := url.Values{"app": {app}, "stream": {stream}, "type": {"play"}}
		urls.WebRTC = stringPtr(fmt.Sprintf("http://%s/index/api/webrtc?%s", base, query.Encode()))
	}
	if cfg.HTTPSPort > 0 {
		base := fmt.Sprintf("%s:%d", mediaNode.Host, cfg.HTTPSPort)
		urls.WSSFLV = stringPtr("wss://" + base + "/" + path + ".live.flv")
		urls.HTTPSFLV = stringPtr("https://" + base + "/" + path + ".live.flv")
		if cfg.FMP4Enabled {
			urls.WSSFMP4 = stringPtr("wss://" + base + "/" + path + ".live.mp4")
			urls.HTTPSFMP4 = stringPtr("https://" + base + "/" + path + ".live.mp4")
		}
		if cfg.HLSEnabled {
			urls.HTTPSHLS = stringPtr("https://" + base + "/" + escapedApp + "/" + escapedStream + "/hls.m3u8")
		}
		if cfg.TSEnabled {
			urls.WSSTS = stringPtr("wss://" + base + "/" + path + ".live.ts")
			urls.HTTPSSTS = stringPtr("https://" + base + "/" + path + ".live.ts")
		}
		query := url.Values{"app": {app}, "stream": {stream}, "type": {"play"}}
		urls.WebRTCS = stringPtr(fmt.Sprintf("https://%s/index/api/webrtc?%s", base, query.Encode()))
	}
	if cfg.RTMPPort > 0 && cfg.RTMPEnabled {
		urls.RTMP = stringPtr(fmt.Sprintf("rtmp://%s:%d/%s/%s", mediaNode.Host, cfg.RTMPPort, escapedApp, escapedStream))
	}
	if cfg.RTMPSPort > 0 && cfg.RTMPEnabled {
		urls.RTMPS = stringPtr(fmt.Sprintf("rtmps://%s:%d/%s/%s", mediaNode.Host, cfg.RTMPSPort, escapedApp, escapedStream))
	}
	if cfg.RTSPPort > 0 && cfg.RTSPEnabled {
		urls.RTSP = stringPtr(fmt.Sprintf("rtsp://%s:%d/%s/%s", mediaNode.Host, cfg.RTSPPort, escapedApp, escapedStream))
	}
	if cfg.RTSPSPort > 0 && cfg.RTSPEnabled {
		urls.RTSPS = stringPtr(fmt.Sprintf("rtsps://%s:%d/%s/%s", mediaNode.Host, cfg.RTSPSPort, escapedApp, escapedStream))
	}
	return urls, nil
}

func stringPtr(value string) *string { return &value }
