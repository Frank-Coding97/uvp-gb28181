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
	VHost  string
	App    string
	Stream string
	SSRC   string
	// RTPInfoURL identifies the local media resource for MANSRTSP RTP-Info.
	// It is independent from RemoteIP, which is the upstream receiver address.
	RTPInfoURL    string
	PayloadType   int
	RemoteIP      string
	RemotePort    int
	Transport     GBSendRTPTransport
	RTCPKeepalive bool
}

type GBSendRTPResult struct {
	LocalPort int `json:"local_port"`
	// RTPInfo is populated only by a sender implementation that has observed
	// the first packet emitted for this send operation. ZLM's current
	// startSendRtp response does not provide it, so callers must treat nil as
	// "not captured".
	RTPInfo *GBRTPInfo `json:"rtp_info,omitempty"`
}

type GBRTPInfo struct {
	URL     string `json:"url"`
	Seq     uint16 `json:"seq"`
	RTPTime uint32 `json:"rtptime"`
}

type gbRTPInfoWire struct {
	URL     string  `json:"url"`
	Seq     *uint16 `json:"seq"`
	RTPTime *uint32 `json:"rtptime"`
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
		if in.Transport == GBSendRTPUDP && in.RTCPKeepalive && in.RemotePort == 65535 {
			return nil, fmt.Errorf("GB send RTP 目标端口没有可用的相邻 RTCP 端口")
		}
		params["dst_url"] = ip.String()
		params["dst_port"] = strconv.Itoa(in.RemotePort)
		if in.Transport == GBSendRTPUDP {
			params["is_udp"] = "1"
			params["udp_rtcp_timeout"] = "0"
			if in.RTCPKeepalive {
				params["udp_rtcp_timeout"] = "1"
			}
		} else {
			params["is_udp"] = "0"
		}
	}

	var response struct {
		baseResp
		LocalPort int            `json:"local_port"`
		RTPInfo   *gbRTPInfoWire `json:"rtp_info,omitempty"`
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
	result := &GBSendRTPResult{LocalPort: response.LocalPort}
	if wire := response.RTPInfo; wire != nil && wire.Seq != nil && wire.RTPTime != nil {
		url := buildGBRTPInfoURL(in)
		if strings.TrimSpace(in.RTPInfoURL) == "" && strings.TrimSpace(wire.URL) != "" {
			url = strings.TrimSpace(wire.URL)
		}
		result.RTPInfo = &GBRTPInfo{URL: url, Seq: *wire.Seq, RTPTime: *wire.RTPTime}
	}
	return result, nil
}

// GetGBSendRTPInfo reads the optional first-packet sample exposed by the
// UVP ZLMediaKit fork. Older ZLM nodes omit rtp_info; that remains a valid
// response because RTP-Info sequence/timestamp fields are optional.
func (c *Client) GetGBSendRTPInfo(ctx context.Context, in GBSendRTPRequest) (*GBRTPInfo, error) {
	if err := validateGBSendRTPIdentity(in); err != nil {
		return nil, err
	}
	var response struct {
		Code    *int                     `json:"code"`
		Msg     string                   `json:"msg"`
		RTPInfo map[string]gbRTPInfoWire `json:"rtp_info"`
	}
	if err := c.call(ctx, "listRtpSender", map[string]string{
		"vhost": in.VHost, "app": in.App, "stream": in.Stream,
	}, &response); err != nil {
		return nil, err
	}
	if response.Code == nil {
		return nil, fmt.Errorf("listRtpSender 返回缺少 code")
	}
	if *response.Code == zlmAPINotFound {
		return nil, nil
	}
	if *response.Code != 0 {
		return nil, c.runtimeResponseError("listRtpSender", *response.Code, response.Msg)
	}
	wire, ok := response.RTPInfo[in.SSRC]
	if !ok || wire.Seq == nil || wire.RTPTime == nil {
		return nil, nil
	}
	return &GBRTPInfo{URL: buildGBRTPInfoURL(in), Seq: *wire.Seq, RTPTime: *wire.RTPTime}, nil
}

// ResetGBSendRTPInfo makes the next packet emitted by this sender the sample
// returned from GetGBSendRTPInfo. It is used before a downstream seek/resume.
// Nodes that do not implement this optional endpoint return an ordinary API
// error; callers may then omit the optional RTP-Info header.
func (c *Client) ResetGBSendRTPInfo(ctx context.Context, in GBSendRTPRequest) error {
	if err := validateGBSendRTPIdentity(in); err != nil {
		return err
	}
	var response struct {
		Code *int   `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := c.call(ctx, "resetRtpSenderInfo", map[string]string{
		"vhost": in.VHost, "app": in.App, "stream": in.Stream, "ssrc": in.SSRC,
	}, &response); err != nil {
		return err
	}
	if response.Code == nil {
		return fmt.Errorf("resetRtpSenderInfo 返回缺少 code")
	}
	if *response.Code != 0 {
		return c.runtimeResponseError("resetRtpSenderInfo", *response.Code, response.Msg)
	}
	return nil
}

func validateGBSendRTPIdentity(in GBSendRTPRequest) error {
	if strings.TrimSpace(in.VHost) == "" || strings.TrimSpace(in.App) == "" ||
		strings.TrimSpace(in.Stream) == "" || strings.TrimSpace(in.SSRC) == "" {
		return fmt.Errorf("GB send RTP 状态检查缺少流标识或 SSRC")
	}
	return nil
}

func buildGBRTPInfoURL(in GBSendRTPRequest) string {
	if url := strings.TrimSpace(in.RTPInfoURL); url != "" {
		return url
	}
	host := strings.TrimSpace(in.RemoteIP)
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return "rtsp://" + host + "/" + strings.Trim(in.App, "/") + "/" + strings.Trim(in.Stream, "/")
}

// IsGBSendRTPActive checks one SSRC, including after a UDP RTCP timeout.
// A missing source or sender is stopped; a failed check remains an error.
func (c *Client) IsGBSendRTPActive(ctx context.Context, in GBSendRTPRequest) (bool, error) {
	if err := validateGBSendRTPIdentity(in); err != nil {
		return false, err
	}
	var response struct {
		Code *int     `json:"code"`
		Msg  string   `json:"msg"`
		Data []string `json:"data"`
	}
	if err := c.call(ctx, "listRtpSender", map[string]string{
		"vhost": in.VHost, "app": in.App, "stream": in.Stream,
	}, &response); err != nil {
		return false, err
	}
	if response.Code == nil {
		return false, fmt.Errorf("listRtpSender 返回缺少 code")
	}
	if *response.Code == zlmAPINotFound {
		return false, nil
	}
	if *response.Code != 0 {
		return false, c.runtimeResponseError("listRtpSender", *response.Code, response.Msg)
	}
	for _, ssrc := range response.Data {
		if ssrc == in.SSRC {
			return true, nil
		}
	}
	return false, nil
}
