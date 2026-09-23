package zlm

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const (
	minTalkCloseDelayMS = 10000
	maxTalkCloseDelayMS = 15000
)

// TalkSendRtpRequest identifies the browser-published audio source and the
// stream that ZLM will receive back from the device on the passive TCP socket.
type TalkSendRtpRequest struct {
	VHost        string
	App          string
	SourceStream string
	RecvStreamID string
	SSRC         string
	CloseDelayMS int
}

type StartSendRtpPassiveResult struct {
	LocalPort int
}

type BroadcastSendRtpRequest struct {
	VHost        string
	App          string
	SourceStream string
	SSRC         string
	RemoteIP     string
	RemotePort   int
	IsUDP        bool
}

type StartBroadcastSendRtpResult struct {
	LocalPort int
}

func (c *Client) StartBroadcastSendRtp(ctx context.Context, in BroadcastSendRtpRequest) (*StartBroadcastSendRtpResult, error) {
	if strings.TrimSpace(in.VHost) == "" || strings.TrimSpace(in.App) == "" ||
		strings.TrimSpace(in.SourceStream) == "" || strings.TrimSpace(in.SSRC) == "" {
		return nil, fmt.Errorf("startSendRtp 缺少流标识或 SSRC")
	}
	ip := net.ParseIP(strings.TrimSpace(in.RemoteIP))
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() || in.RemotePort <= 0 || in.RemotePort > 65535 {
		return nil, fmt.Errorf("startSendRtp 目标地址或端口不合法")
	}
	isUDP := "0"
	if in.IsUDP {
		isUDP = "1"
	}
	var response struct {
		baseResp
		LocalPort int `json:"local_port"`
	}
	params := map[string]string{
		"vhost": in.VHost, "app": in.App, "stream": in.SourceStream, "ssrc": in.SSRC,
		"dst_url": ip.String(), "dst_port": strconv.Itoa(in.RemotePort), "is_udp": isUDP,
		"only_audio": "1", "pt": "8", "use_ps": "0",
	}
	if err := c.call(ctx, "startSendRtp", params, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, fmt.Errorf("startSendRtp code=%d msg=%s", response.Code, response.Msg)
	}
	if response.LocalPort <= 0 || response.LocalPort > 65535 {
		return nil, fmt.Errorf("startSendRtp 返回无效 local_port=%d", response.LocalPort)
	}
	return &StartBroadcastSendRtpResult{LocalPort: response.LocalPort}, nil
}

// StartSendRtpPassive opens the TCP/RTP passive sender used by first-phase
// talk. Its local port must be known before the TALK INVITE SDP is built.
func (c *Client) StartSendRtpPassive(ctx context.Context, in TalkSendRtpRequest) (*StartSendRtpPassiveResult, error) {
	if strings.TrimSpace(in.VHost) == "" || strings.TrimSpace(in.App) == "" ||
		strings.TrimSpace(in.SourceStream) == "" || strings.TrimSpace(in.RecvStreamID) == "" ||
		strings.TrimSpace(in.SSRC) == "" {
		return nil, fmt.Errorf("startSendRtpPassive 缺少流标识或 SSRC")
	}
	if in.CloseDelayMS < minTalkCloseDelayMS || in.CloseDelayMS > maxTalkCloseDelayMS {
		return nil, fmt.Errorf("startSendRtpPassive close_delay_ms 必须在 %d..%d", minTalkCloseDelayMS, maxTalkCloseDelayMS)
	}

	var response struct {
		baseResp
		LocalPort int `json:"local_port"`
	}
	params := map[string]string{
		"vhost":                    in.VHost,
		"app":                      in.App,
		"stream":                   in.SourceStream,
		"ssrc":                     in.SSRC,
		"type":                     "0",
		"only_audio":               "1",
		"pt":                       "8",
		"is_udp":                   "0",
		"recv_stream_id":           in.RecvStreamID,
		"enable_origin_recv_limit": "1",
		"close_delay_ms":           strconv.Itoa(in.CloseDelayMS),
	}
	if err := c.call(ctx, "startSendRtpPassive", params, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, fmt.Errorf("startSendRtpPassive code=%d msg=%s", response.Code, response.Msg)
	}
	if response.LocalPort <= 0 {
		return nil, fmt.Errorf("startSendRtpPassive 返回无效 local_port=%d", response.LocalPort)
	}
	return &StartSendRtpPassiveResult{LocalPort: response.LocalPort}, nil
}

// StopSendRtp stops exactly one RTP sender. An SSRC is mandatory so cleanup
// cannot accidentally stop unrelated senders attached to the same source.
func (c *Client) StopSendRtp(ctx context.Context, vhost, appName, stream, ssrc string) error {
	if strings.TrimSpace(vhost) == "" || strings.TrimSpace(appName) == "" ||
		strings.TrimSpace(stream) == "" || strings.TrimSpace(ssrc) == "" {
		return fmt.Errorf("stopSendRtp 缺少流标识或 SSRC")
	}
	var response baseResp
	if err := c.call(ctx, "stopSendRtp", map[string]string{
		"vhost": vhost, "app": appName, "stream": stream, "ssrc": ssrc,
	}, &response); err != nil {
		return err
	}
	if response.Code == -500 {
		return nil
	}
	if response.Code != 0 {
		return fmt.Errorf("stopSendRtp code=%d msg=%s", response.Code, response.Msg)
	}
	return nil
}

// CloseTalkSource forcibly closes all protocol variants of one browser talk
// source. Missing sources are already closed and therefore idempotent.
func (c *Client) CloseTalkSource(ctx context.Context, vhost, appName, stream string) error {
	if strings.TrimSpace(vhost) == "" || strings.TrimSpace(appName) == "" || strings.TrimSpace(stream) == "" {
		return fmt.Errorf("close_streams 缺少流标识")
	}
	var response struct {
		baseResp
		Count int `json:"count"`
	}
	if err := c.call(ctx, "close_streams", map[string]string{
		"vhost": vhost, "app": appName, "stream": stream, "force": "1",
	}, &response); err != nil {
		return err
	}
	if response.Code == -500 {
		return nil
	}
	if response.Code != 0 {
		return fmt.Errorf("close_streams code=%d msg=%s", response.Code, response.Msg)
	}
	return nil
}
