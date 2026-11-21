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
	// PayloadType 是 RTP 头里的载荷类型（G.711A=8、G.711U=0）。
	// nil 保持历史默认 8；⛔ 不能用 0 当「未指定」，PCMU 的合法值就是 0。
	PayloadType *int
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
	// PayloadType 同 TalkSendRtpRequest：nil = 8，0 是 PCMU 的合法值。
	PayloadType *int
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
	pt, err := payloadTypeParam(in.PayloadType)
	if err != nil {
		return nil, err
	}
	var response struct {
		baseResp
		LocalPort int `json:"local_port"`
	}
	params := map[string]string{
		"vhost": in.VHost, "app": in.App, "stream": in.SourceStream, "ssrc": in.SSRC,
		"dst_url": ip.String(), "dst_port": strconv.Itoa(in.RemotePort), "is_udp": isUDP,
		"only_audio": "1", "pt": pt, "use_ps": "0",
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

	pt, err := payloadTypeParam(in.PayloadType)
	if err != nil {
		return nil, err
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
		"pt":                       pt,
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
	// 幂等语义：以下两种都是"已经没有发送会话可停"，等同成功——
	//   -500 not_found   流本身已注销（ZLM `can not find the stream`）；
	//   -1   other_failed 流还在，但该 ssrc 没有匹配的发送器（ZLM `stopSendRtp failed`）。
	//
	// ⛔ 别把 -1 当失败：设备从未应答的对讲会话就是这个形态（`local_port=0`）。按失败处理会把
	// 清理吊死、租约永不落终态，DELETE 变成 500「语音对讲操作失败」（2026-10-03 现场）。
	if response.Code == zlmAPIOtherFailed || response.Code == zlmAPINotFound {
		return nil
	}
	if response.Code != 0 {
		return fmt.Errorf("stopSendRtp code=%d(%s) msg=%s", response.Code, zlmCodeLabel(response.Code), response.Msg)
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
	// ZLM 的 close_streams 对不存在的流也回 code=0（只把 count_* 置 0，2026-10-03 实测），
	// 这里保留 not_found 的容忍只是防御性兜底：真正会失败的是鉴权/参数与传输层。
	if response.Code == zlmAPINotFound {
		return nil
	}
	// A protocol variant may lose its owner while the same close operation
	// unregisters the source. Only accept this race after a strict query
	// confirms that every variant of this exact source has disappeared.
	if response.Code == -400 && strings.Contains(response.Msg, "::getOwnerPoller failed:") {
		remaining, err := c.GetMediaListFiltered(ctx, MediaFilter{VHost: vhost, App: appName, Stream: stream})
		if err == nil && len(remaining) == 0 {
			return nil
		}
	}
	if response.Code != 0 {
		return fmt.Errorf("close_streams code=%d(%s) msg=%s", response.Code, zlmCodeLabel(response.Code), response.Msg)
	}
	return nil
}

// payloadTypeParam 把请求里的载荷类型转成 startSendRtp 的 `pt` 参数。
//
// ⛔ 不能用「零值」判断未指定：PCMU 的合法载荷类型就是 0。
// ⛔ 国标对讲只有两个静态值（PCMA=8 / PCMU=0）；这里仍做范围校验，
// 是为了让调用方传错时在平台侧就报错，而不是让 ZLM 发出一串没人认识的包。
func payloadTypeParam(pt *int) (string, error) {
	if pt == nil {
		return "8", nil
	}
	if *pt < 0 || *pt > 127 {
		return "", fmt.Errorf("RTP 载荷类型不合法: %d", *pt)
	}
	return strconv.Itoa(*pt), nil
}
