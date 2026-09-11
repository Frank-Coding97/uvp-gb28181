package playurl

import (
	"fmt"
	"net/url"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// URLs contains only protocols actually exposed by the selected node.
type URLs struct {
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

type Selection struct {
	Protocol  string
	URL       string
	ZLMWebRTC bool
}

func Build(playbackHost string, cfg node.ServerConfig, app, stream string) URLs {
	escapedApp := url.PathEscape(app)
	escapedStream := url.PathEscape(stream)
	path := escapedApp + "/" + escapedStream
	var urls URLs
	if cfg.HTTPPort > 0 {
		base := fmt.Sprintf("%s:%d", playbackHost, cfg.HTTPPort)
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
		if !cfg.RTCTransportKnown || cfg.RTCTransportEnabled {
			urls.WebRTC = stringPtr(fmt.Sprintf("http://%s/index/api/webrtc?%s", base, query.Encode()))
		}
	}
	if cfg.HTTPSPort > 0 {
		base := fmt.Sprintf("%s:%d", playbackHost, cfg.HTTPSPort)
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
		if !cfg.RTCTransportKnown || cfg.RTCTransportEnabled {
			urls.WebRTCS = stringPtr(fmt.Sprintf("https://%s/index/api/webrtc?%s", base, query.Encode()))
		}
	}
	if cfg.RTMPPort > 0 && cfg.RTMPEnabled {
		urls.RTMP = stringPtr(fmt.Sprintf("rtmp://%s:%d/%s/%s", playbackHost, cfg.RTMPPort, escapedApp, escapedStream))
	}
	if cfg.RTMPSPort > 0 && cfg.RTMPEnabled {
		urls.RTMPS = stringPtr(fmt.Sprintf("rtmps://%s:%d/%s/%s", playbackHost, cfg.RTMPSPort, escapedApp, escapedStream))
	}
	if cfg.RTSPPort > 0 && cfg.RTSPEnabled {
		urls.RTSP = stringPtr(fmt.Sprintf("rtsp://%s:%d/%s/%s", playbackHost, cfg.RTSPPort, escapedApp, escapedStream))
	}
	if cfg.RTSPSPort > 0 && cfg.RTSPEnabled {
		urls.RTSPS = stringPtr(fmt.Sprintf("rtsps://%s:%d/%s/%s", playbackHost, cfg.RTSPSPort, escapedApp, escapedStream))
	}
	return urls
}

func (u URLs) AsMap() map[string]string {
	result := make(map[string]string)
	for key, value := range map[string]*string{
		"wsFlv": u.WSFLV, "httpFlv": u.HTTPFLV, "wssFlv": u.WSSFLV, "httpsFlv": u.HTTPSFLV,
		"wsFmp4": u.WSFMP4, "httpFmp4": u.HTTPFMP4, "wssFmp4": u.WSSFMP4, "httpsFmp4": u.HTTPSFMP4,
		"hls": u.HLS, "httpsHls": u.HTTPSHLS, "wsTs": u.WSTS, "httpTs": u.HTTPSTS,
		"wssTs": u.WSSTS, "httpsTs": u.HTTPSSTS, "webrtc": u.WebRTC, "webrtcs": u.WebRTCS,
		"rtmp": u.RTMP, "rtmps": u.RTMPS, "rtsp": u.RTSP, "rtsps": u.RTSPS,
	} {
		if value != nil && *value != "" {
			result[key] = *value
		}
	}
	return result
}

func FromMap(values map[string]string) URLs {
	value := func(key string) *string {
		if values[key] == "" {
			return nil
		}
		return stringPtr(values[key])
	}
	return URLs{
		WSFLV: value("wsFlv"), HTTPFLV: value("httpFlv"), WSSFLV: value("wssFlv"), HTTPSFLV: value("httpsFlv"),
		WSFMP4: value("wsFmp4"), HTTPFMP4: value("httpFmp4"), WSSFMP4: value("wssFmp4"), HTTPSFMP4: value("httpsFmp4"),
		HLS: value("hls"), HTTPSHLS: value("httpsHls"), WSTS: value("wsTs"), HTTPSTS: value("httpTs"),
		WSSTS: value("wssTs"), HTTPSSTS: value("httpsTs"), WebRTC: value("webrtc"), WebRTCS: value("webrtcs"),
		RTMP: value("rtmp"), RTMPS: value("rtmps"), RTSP: value("rtsp"), RTSPS: value("rtsps"),
	}
}

// Select returns a browser-safe URL using a stable logical fallback order.
func Select(urls URLs, preferred string, secure bool) Selection {
	if preferred != "ws-flv" && preferred != "http-flv" && preferred != "hls" && preferred != "webrtc" {
		preferred = "ws-flv"
	}
	order := []string{preferred}
	for _, protocol := range []string{"ws-flv", "http-flv", "hls", "webrtc"} {
		if protocol != preferred {
			order = append(order, protocol)
		}
	}
	for _, protocol := range order {
		if value := urlForProtocol(urls, protocol, secure); value != "" {
			return Selection{Protocol: protocol, URL: value, ZLMWebRTC: protocol == "webrtc"}
		}
	}
	return Selection{}
}

func urlForProtocol(urls URLs, protocol string, secure bool) string {
	var primary, alternate *string
	switch protocol {
	case "ws-flv":
		primary, alternate = urls.WSFLV, urls.WSSFLV
		if secure {
			primary, alternate = urls.WSSFLV, nil
		}
	case "http-flv":
		primary, alternate = urls.HTTPFLV, urls.HTTPSFLV
		if secure {
			primary, alternate = urls.HTTPSFLV, nil
		}
	case "hls":
		primary, alternate = urls.HLS, urls.HTTPSHLS
		if secure {
			primary, alternate = urls.HTTPSHLS, nil
		}
	case "webrtc":
		primary, alternate = urls.WebRTC, urls.WebRTCS
		if secure {
			primary, alternate = urls.WebRTCS, nil
		}
	}
	if primary != nil {
		return *primary
	}
	if alternate != nil {
		return *alternate
	}
	return ""
}

func stringPtr(value string) *string { return &value }
