package zlm

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type GBSendRTPTransport string

const (
	GBSendRTPUDP        GBSendRTPTransport = "udp"
	GBSendRTPTCPActive  GBSendRTPTransport = "tcp-active"
	GBSendRTPTCPPassive GBSendRTPTransport = "tcp-passive"
)

// GBSendRTPRequest describes one PS-over-RTP sender for an upstream GB28181 platform.
// It is deliberately separate from the audio-only talk and broadcast requests.
type GBSendRTPRequest struct {
	VHost       string
	App         string
	Stream      string
	SSRC        string
	PayloadType int
	RemoteIP    string
	RemotePort  int
	Transport   GBSendRTPTransport
}

type GBSendRTPResult struct {
	LocalPort int
}

func (c *Client) StartGBSendRTP(ctx context.Context, in GBSendRTPRequest) (*GBSendRTPResult, error) {
	if strings.TrimSpace(in.VHost) == "" || strings.TrimSpace(in.App) == "" ||
		strings.TrimSpace(in.Stream) == "" || strings.TrimSpace(in.SSRC) == "" {
		return nil, fmt.Errorf("GB send RTP 缺少流标识或 SSRC")
	}
	if in.PayloadType < 0 || in.PayloadType > 127 {
		return nil, fmt.Errorf("GB send RTP payload type 不合法")
	}
	if in.Transport != GBSendRTPUDP && in.Transport != GBSendRTPTCPActive && in.Transport != GBSendRTPTCPPassive {
		return nil, fmt.Errorf("GB send RTP transport 不支持")
	}

	params := map[string]string{
		"vhost": in.VHost, "app": in.App, "stream": in.Stream, "ssrc": in.SSRC,
		"only_audio": "0", "pt": strconv.Itoa(in.PayloadType), "use_ps": "1",
	}
	api := "startSendRtp"
	if in.Transport == GBSendRTPTCPPassive {
		api = "startSendRtpPassive"
		params["is_udp"] = "0"
	} else {
		ip := net.ParseIP(strings.TrimSpace(in.RemoteIP))
		if ip == nil || ip.IsUnspecified() || in.RemotePort <= 0 || in.RemotePort > 65535 {
			return nil, fmt.Errorf("GB send RTP 目标地址或端口不合法")
		}
		params["dst_url"] = ip.String()
		params["dst_port"] = strconv.Itoa(in.RemotePort)
		if in.Transport == GBSendRTPUDP {
			params["is_udp"] = "1"
		} else {
			params["is_udp"] = "0"
		}
	}

	var response struct {
		baseResp
		LocalPort int `json:"local_port"`
	}
	if err := c.call(ctx, api, params, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, fmt.Errorf("%s code=%d msg=%s", api, response.Code, response.Msg)
	}
	if response.LocalPort <= 0 || response.LocalPort > 65535 {
		return nil, fmt.Errorf("%s 返回无效 local_port=%d", api, response.LocalPort)
	}
	return &GBSendRTPResult{LocalPort: response.LocalPort}, nil
}
