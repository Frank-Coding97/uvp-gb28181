package zlm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// Client ZLMediaKit HTTP API 客户端(控制面)
// 一个 Client 绑一个 node;多节点场景每节点一个 Client(无连接池,Go http.Client 自带)。
type Client struct {
	node         *node.Node
	baseURL      string
	secret       string
	http         *http.Client
	downloadHTTP *http.Client
}

// NewClientForNode 基于 Node 构造 Client
func NewClientForNode(n *node.Node) *Client {
	return &Client{
		node:    n,
		baseURL: n.HTTPEndpoint(),
		secret:  n.APISecret,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Node 返回绑定节点
func (c *Client) Node() *node.Node { return c.node }

// baseResp ZLM API 通用响应头
type baseResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type redactedTransportError struct {
	err     error
	secrets []string
}

func (e redactedTransportError) Error() string {
	message := e.err.Error()
	for _, secret := range e.secrets {
		if secret == "" {
			continue
		}
		message = strings.ReplaceAll(message, secret, "***")
		message = strings.ReplaceAll(message, url.QueryEscape(secret), "***")
	}
	return message
}

func (e redactedTransportError) Unwrap() error { return e.err }

// call 发起 GET 请求(ZLM API 多为 GET + query 参数),解析到 out
func (c *Client) call(ctx context.Context, api string, params map[string]string, out interface{}) error {
	q := url.Values{}
	q.Set("secret", c.secret)
	for k, v := range params {
		q.Set(k, v)
	}
	reqURL := c.baseURL + "/" + api + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		secrets := []string{c.secret}
		for key, rawHook := range params {
			if !strings.HasPrefix(key, "hook.") || rawHook == "" {
				continue
			}
			if parsedHook, parseErr := url.Parse(rawHook); parseErr == nil {
				secrets = append(secrets, parsedHook.Query().Get("cap"))
			}
		}
		return fmt.Errorf("ZLM 请求失败 %s: %w", api, redactedTransportError{err: err, secrets: secrets})
	}
	// defer 必须先挂:超限/读取错误等所有路径都必须关闭响应体,否则连接
	// 被持续占用直到超时
	defer resp.Body.Close()
	// 控制响应硬上限:被攻陷或误配置的节点可在超时窗口内持续发送数据,
	// 无界 io.ReadAll 会让并发请求耗尽后端内存
	const maxControlResponseBytes = 8 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxControlResponseBytes+1))
	if err != nil {
		return fmt.Errorf("ZLM 响应读取失败 %s: %w", api, err)
	}
	if len(body) > maxControlResponseBytes {
		return fmt.Errorf("ZLM 响应超出上限 %s: %d 字节", api, len(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("ZLM 响应解析失败 %s: %w, bodyLen=%d", api, err, len(body))
		}
	}
	return nil
}

// GetServerConfig 获取 ZLM 配置(也用于连通性探测)
func (c *Client) GetServerConfig(ctx context.Context) (map[string]string, error) {
	var r struct {
		baseResp
		Data []map[string]string `json:"data"`
	}
	if err := c.call(ctx, "getServerConfig", nil, &r); err != nil {
		return nil, err
	}
	if r.Code != 0 {
		return nil, fmt.Errorf("getServerConfig code=%d msg=%s", r.Code, r.Msg)
	}
	if len(r.Data) > 0 {
		return r.Data[0], nil
	}
	return map[string]string{}, nil
}

// SetServerConfig 动态下发配置(Hook 地址、超时等),params 为 ZLM 配置键值
func (c *Client) SetServerConfig(ctx context.Context, params map[string]string) error {
	var r baseResp
	if err := c.call(ctx, "setServerConfig", params, &r); err != nil {
		return err
	}
	if r.Code != 0 {
		return fmt.Errorf("setServerConfig code=%d msg=%s", r.Code, r.Msg)
	}
	return nil
}

// OpenRtpServerResult openRtpServer 返回
type OpenRtpServerResult struct {
	Port int // ZLM 实际分配的收流端口
}

// OpenRtpServerRequest describes one ZLM RTP receiver. StreamID is the media
// path identity while SSRC belongs to the current GB28181 media session.
type OpenRtpServerRequest struct {
	VHost     string
	App       string
	StreamID  string
	SSRC      string
	Port      int
	TCPMode   int
	OnlyTrack int
	LocalIP   string
	Reuse     bool
}

// OpenRtpServer preserves the legacy callers that do not need an independent
// SSRC (for example device-record playback).
func (c *Client) OpenRtpServer(ctx context.Context, streamID string, port int, tcpMode int, onlyTrack int) (*OpenRtpServerResult, error) {
	return c.OpenRtpServerWithSSRC(ctx, OpenRtpServerRequest{
		StreamID: streamID, Port: port, TCPMode: tcpMode, OnlyTrack: onlyTrack,
	})
}

// OpenRtpServerWithSSRC opens a live RTP receiver with separate media path and
// GB28181 session identities.
func (c *Client) OpenRtpServerWithSSRC(ctx context.Context, request OpenRtpServerRequest) (*OpenRtpServerResult, error) {
	var r struct {
		baseResp
		Port int `json:"port"`
	}
	params := map[string]string{
		"stream_id":   request.StreamID,
		"port":        strconv.Itoa(request.Port),
		"tcp_mode":    strconv.Itoa(request.TCPMode),   // 0=UDP 1=TCP被动
		"only_track":  strconv.Itoa(request.OnlyTrack), // 0=音视频 2=仅视频
		"re_use_port": "0",
	}
	if request.VHost != "" {
		params["vhost"] = request.VHost
	}
	if request.App != "" {
		params["app"] = request.App
	}
	if request.SSRC != "" {
		params["ssrc"] = request.SSRC
	}
	if request.LocalIP != "" {
		params["local_ip"] = request.LocalIP
	}
	if request.Reuse {
		params["re_use_port"] = "1"
	}
	if err := c.call(ctx, "openRtpServer", params, &r); err != nil {
		return nil, err
	}
	if r.Code != 0 {
		return nil, fmt.Errorf("openRtpServer code=%d msg=%s", r.Code, r.Msg)
	}
	return &OpenRtpServerResult{Port: r.Port}, nil
}

// CloseRtpServer 关闭 RTP 收流端口
func (c *Client) CloseRtpServer(ctx context.Context, streamID string) error {
	var r baseResp
	if err := c.call(ctx, "closeRtpServer", map[string]string{"stream_id": streamID}, &r); err != nil {
		return err
	}
	// 只有明确的"资源不存在"可视为幂等成功;其他非零码必须报错,
	// 否则上层会继续解绑流/回收 SSRC,留下实际未关闭的端口监听
	if r.Code != 0 && !strings.Contains(strings.ToLower(r.Msg), "not exist") && !strings.Contains(r.Msg, "不存在") {
		return fmt.Errorf("closeRtpServer code=%d msg=%s", r.Code, r.Msg)
	}
	return nil
}

// MediaTrack is one audio or video track returned by getMediaInfo.
type MediaTrack struct {
	CodecID       int      `json:"codec_id"`
	CodecIDName   string   `json:"codec_id_name"`
	Ready         bool     `json:"ready"`
	CodecType     int      `json:"codec_type"`
	Frames        int64    `json:"frames"`
	Duration      float64  `json:"duration"`
	SampleRate    int      `json:"sample_rate"`
	Channels      int      `json:"channels"`
	SampleBit     int      `json:"sample_bit"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	FPS           float64  `json:"fps"`
	KeyFrames     int64    `json:"key_frames"`
	GOPSize       int      `json:"gop_size"`
	GOPIntervalMS int      `json:"gop_interval_ms"`
	Loss          *float64 `json:"loss"`
}

// MediaInfo is the full single-stream snapshot returned by getMediaInfo.
type MediaInfo struct {
	Online           bool         `json:"online"`
	Schema           string       `json:"schema"`
	VHost            string       `json:"vhost"`
	App              string       `json:"app"`
	Stream           string       `json:"stream"`
	CreateStamp      uint64       `json:"createStamp"`
	CurrentStamp     uint64       `json:"currentStamp"`
	AliveSecond      uint64       `json:"aliveSecond"`
	BytesSpeed       uint64       `json:"bytesSpeed"`
	TotalBytes       uint64       `json:"totalBytes"`
	ReaderCount      int          `json:"readerCount"`
	TotalReaderCount int          `json:"totalReaderCount"`
	OriginType       int          `json:"originType"`
	OriginTypeStr    string       `json:"originTypeStr"`
	OriginURL        string       `json:"originUrl"`
	IsRecordingMP4   bool         `json:"isRecordingMP4"`
	IsRecordingHLS   bool         `json:"isRecordingHLS"`
	OriginSock       *SockInfo    `json:"originSock"`
	Tracks           []MediaTrack `json:"tracks"`
}

// MediaFilter is the complete optional filter accepted by getMediaList.
// Empty fields are omitted, matching ZLM's list-all semantics. Schema is
// deliberately part of the type: filtering only by app/stream can mix the
// independent protocol views of one media source.
type MediaFilter struct {
	Schema string
	VHost  string
	App    string
	Stream string
}

// StreamTarget identifies exactly one ZLM media source. All fields are
// required by close_stream and by the typed runtime detail APIs.
type StreamTarget struct {
	Schema string
	VHost  string
	App    string
	Stream string
}

func (target StreamTarget) Validate() error {
	if strings.TrimSpace(target.Schema) == "" || strings.TrimSpace(target.VHost) == "" ||
		strings.TrimSpace(target.App) == "" || strings.TrimSpace(target.Stream) == "" {
		return errors.New("invalid stream target")
	}
	return nil
}

// CloseStreamResult is the stable result of a singular close_stream call.
// ZLM uses result=0 for a successful close; Closed is kept explicit for
// callers that do not need to inspect the wire integer.
type CloseStreamResult struct {
	Result int  `json:"result"`
	Closed bool `json:"closed"`
}

// BatchCloseResult reports both the number of matching media sources and the
// number of asynchronous close requests submitted by close_streams.
type BatchCloseResult struct {
	CountHit    int `json:"count_hit"`
	CountClosed int `json:"count_closed"`
}

// ErrMediaNotFound is returned by strict media detail/close calls when ZLM
// reports code=-500. The caller may treat this as an idempotent already-absent
// result while all other non-zero codes remain errors.
var ErrMediaNotFound = errors.New("media not found")

const (
	CapabilityGetMediaList       = "getMediaList"
	CapabilityGetMediaInfo       = "getMediaInfo"
	CapabilityGetMediaPlayerList = "getMediaPlayerList"
)

type SockInfo struct {
	PeerIP     string `json:"peer_ip"`
	PeerPort   int    `json:"peer_port"`
	LocalIP    string `json:"local_ip"`
	LocalPort  int    `json:"local_port"`
	Identifier string `json:"identifier"`
}

// ProbeFrame is one frame sampled by ZLM addProbe. Timestamps are milliseconds.
type ProbeFrame struct {
	Codec       string `json:"codec"`
	TrackType   string `json:"track_type"`
	DTS         int64  `json:"dts"`
	PTS         int64  `json:"pts"`
	RecvStamp   int64  `json:"recv_stamp"`
	FrameSize   int64  `json:"frame_size"`
	Index       int    `json:"index"`
	KeyFrame    bool   `json:"key_frame"`
	ConfigFrame bool   `json:"config_frame"`
}

// AddProbe samples frames from one media source for probeMS milliseconds.
func (c *Client) AddProbe(ctx context.Context, vhost, appName, stream string, probeMS int) ([]ProbeFrame, error) {
	var response struct {
		baseResp
		Data []ProbeFrame `json:"data"`
	}
	params := map[string]string{
		"vhost":    vhost,
		"app":      appName,
		"stream":   stream,
		"probe_ms": strconv.Itoa(probeMS),
	}
	if err := c.call(ctx, "addProbe", params, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, fmt.Errorf("addProbe code=%d msg=%s", response.Code, response.Msg)
	}
	return response.Data, nil
}

// IsMediaOnline 轻量探测一路流是否就绪(返回 online 标志)
// 用于点播流就绪等待的轮询备份(hook + polling 双源)
func (c *Client) IsMediaOnline(ctx context.Context, appName, stream string) (bool, error) {
	var r struct {
		baseResp
		Online bool `json:"online"`
	}
	params := map[string]string{
		"vhost":  "__defaultVhost__",
		"app":    appName,
		"stream": stream,
		"schema": "rtsp", // rtp 推流后首先注册 rtsp,用 rtsp 探测即可
	}
	if err := c.call(ctx, "isMediaOnline", params, &r); err != nil {
		app.ZapLog.Warn("IsMediaOnline 请求失败",
			zap.String("host", c.node.Host),
			zap.String("app", appName),
			zap.String("stream", stream),
			zap.Error(err))
		return false, err
	}
	// code != 0(如 -500 流不存在)视为未就绪,不视为错误
	if r.Code != 0 {
		app.ZapLog.Info("IsMediaOnline 返回非0 code(流不存在或未就绪)",
			zap.String("host", c.node.Host),
			zap.String("app", appName),
			zap.String("stream", stream),
			zap.Int("code", r.Code),
			zap.String("msg", r.Msg))
		return false, nil
	}
	app.ZapLog.Debug("IsMediaOnline 成功",
		zap.String("host", c.node.Host),
		zap.String("app", appName),
		zap.String("stream", stream),
		zap.Bool("online", r.Online))
	return r.Online, nil
}

const recorderTypeMP4 = 1

func recordingParams(vhost, appName, stream string) map[string]string {
	return map[string]string{
		"type":   strconv.Itoa(recorderTypeMP4),
		"vhost":  vhost,
		"app":    appName,
		"stream": stream,
	}
}

// StartRecord starts MP4 recording for an existing ZLM media source.
// maxSecond=0 delegates the slice duration to ZLM's mp4_max_second setting.
func (c *Client) StartRecord(ctx context.Context, vhost, appName, stream string, maxSecond int) error {
	var response struct {
		baseResp
		Result bool `json:"result"`
	}
	params := recordingParams(vhost, appName, stream)
	params["max_second"] = strconv.Itoa(maxSecond)
	if err := c.call(ctx, "startRecord", params, &response); err != nil {
		return err
	}
	if response.Code != 0 || !response.Result {
		return fmt.Errorf("startRecord code=%d msg=%s", response.Code, response.Msg)
	}
	return nil
}

// StopRecord stops MP4 recording. Missing streams are treated as already stopped.
func (c *Client) StopRecord(ctx context.Context, vhost, appName, stream string) error {
	var response struct {
		baseResp
		Result bool `json:"result"`
	}
	if err := c.call(ctx, "stopRecord", recordingParams(vhost, appName, stream), &response); err != nil {
		return err
	}
	if response.Code == -500 {
		return nil
	}
	if response.Code != 0 || !response.Result {
		return fmt.Errorf("stopRecord code=%d msg=%s", response.Code, response.Msg)
	}
	return nil
}

// IsRecording reports ZLM's actual MP4 recorder state for a media source.
func (c *Client) IsRecording(ctx context.Context, vhost, appName, stream string) (bool, error) {
	var response struct {
		baseResp
		Status bool `json:"status"`
	}
	if err := c.call(ctx, "isRecording", recordingParams(vhost, appName, stream), &response); err != nil {
		return false, err
	}
	if response.Code == -500 {
		return false, nil
	}
	if response.Code != 0 {
		return false, fmt.Errorf("isRecording code=%d msg=%s", response.Code, response.Msg)
	}
	return response.Status, nil
}

// GetMediaInfo 查询单路流详情(verify-after-hook 用)
// 返回 online=false 表示流未就绪(包含"流不存在"和"流存在但暂无数据"两种情况)
func (c *Client) GetMediaInfo(ctx context.Context, schema, vhost, app, stream string) (*MediaInfo, error) {
	return c.getMediaInfo(ctx, schema, vhost, app, stream, false)
}

// GetMediaInfoStrict is the typed management-facing detail query. Unlike the
// legacy GetMediaInfo wrapper, it preserves ZLM NotFound and other non-zero
// response codes as errors instead of converting every failure into an
// offline-looking MediaInfo value.
func (c *Client) GetMediaInfoStrict(ctx context.Context, schema, vhost, app, stream string) (*MediaInfo, error) {
	return c.getMediaInfo(ctx, schema, vhost, app, stream, true)
}

func (c *Client) getMediaInfo(ctx context.Context, schema, vhost, app, stream string, strict bool) (*MediaInfo, error) {
	var r struct {
		baseResp
		MediaInfo
	}
	params := map[string]string{
		"schema": schema,
		"vhost":  vhost,
		"app":    app,
		"stream": stream,
	}
	if err := c.call(ctx, CapabilityGetMediaInfo, params, &r); err != nil {
		return nil, err
	}
	if r.Code != 0 {
		if strict {
			if r.Code == -500 {
				return nil, ErrMediaNotFound
			}
			return nil, c.runtimeResponseError(CapabilityGetMediaInfo, r.Code, r.Msg)
		}
		// 流不存在:online=false,不报错
		return &MediaInfo{Schema: schema, VHost: vhost, App: app, Stream: stream}, nil
	}
	info := r.MediaInfo
	info.Online = true
	for i := range info.Tracks {
		if info.Tracks[i].Loss != nil && *info.Tracks[i].Loss < 0 {
			info.Tracks[i].Loss = nil
		}
	}
	return &info, nil
}

// GetMediaList 查询节点上所有活跃媒体的列表,按 (vhost, app, stream) 过滤(留空则不过滤)。
// 同一路流在 ZLM 内部会拆成多个 schema(rtsp/rtmp/hls/ts/fmp4 等),每个 schema 独立计数,
// 想要聚合观众数、码率等指标必须走这个 API,单查 getMediaInfo 会漏统计其他 schema 的下游。
//
// This is the legacy wrapper. New management code should use
// GetMediaListFiltered so schema cannot be accidentally omitted.
func (c *Client) GetMediaList(ctx context.Context, vhost, app, stream string) ([]MediaInfo, error) {
	return c.getMediaList(ctx, MediaFilter{VHost: vhost, App: app, Stream: stream}, false)
}

// GetMediaListFiltered queries getMediaList with the complete typed filter.
// Unlike the legacy wrapper, a non-zero ZLM response is returned as an error
// so a management caller cannot mistake a failed query for an empty list.
func (c *Client) GetMediaListFiltered(ctx context.Context, filter MediaFilter) ([]MediaInfo, error) {
	return c.getMediaList(ctx, filter, true)
}

func (c *Client) getMediaList(ctx context.Context, filter MediaFilter, strict bool) ([]MediaInfo, error) {
	var r struct {
		baseResp
		Data []MediaInfo `json:"data"`
	}
	params := map[string]string{}
	if filter.Schema != "" {
		params["schema"] = filter.Schema
	}
	if filter.VHost != "" {
		params["vhost"] = filter.VHost
	}
	if filter.App != "" {
		params["app"] = filter.App
	}
	if filter.Stream != "" {
		params["stream"] = filter.Stream
	}
	if err := c.call(ctx, CapabilityGetMediaList, params, &r); err != nil {
		return nil, err
	}
	if r.Code != 0 {
		if strict {
			return nil, c.runtimeResponseError(CapabilityGetMediaList, r.Code, r.Msg)
		}
		return nil, nil
	}
	for i := range r.Data {
		r.Data[i].Online = true
		for j := range r.Data[i].Tracks {
			if r.Data[i].Tracks[j].Loss != nil && *r.Data[i].Tracks[j].Loss < 0 {
				r.Data[i].Tracks[j].Loss = nil
			}
		}
	}
	return r.Data, nil
}

// MediaPlayer describes one active player connection returned by ZLM.
// Identifier is the socket/session identifier used by kick_session when the
// deployed ZLM version exposes the same id namespace.
type MediaPlayer struct {
	PeerIP     string `json:"peer_ip"`
	PeerPort   int    `json:"peer_port"`
	LocalIP    string `json:"local_ip"`
	LocalPort  int    `json:"local_port"`
	Identifier string `json:"identifier"`
	TypeID     string `json:"typeid"`
}

// GetMediaPlayerList returns the active players for one media source.
// ZLM requires the complete media tuple, including schema.
func (c *Client) GetMediaPlayerList(ctx context.Context, schema, vhost, app, stream string) ([]MediaPlayer, error) {
	var r struct {
		baseResp
		Data []MediaPlayer `json:"data"`
	}
	params := map[string]string{
		"schema": schema,
		"vhost":  vhost,
		"app":    app,
		"stream": stream,
	}
	if err := c.call(ctx, CapabilityGetMediaPlayerList, params, &r); err != nil {
		return nil, err
	}
	if r.Code != 0 {
		if r.Code == -500 {
			return nil, ErrMediaNotFound
		}
		return nil, c.runtimeResponseError(CapabilityGetMediaPlayerList, r.Code, r.Msg)
	}
	return r.Data, nil
}

// KickSession closes exactly one ZLM TCP session.
// Callers must first prove that the identifier came from the target media
// source; IP/port matching is intentionally not accepted here.
func (c *Client) KickSession(ctx context.Context, identifier string) error {
	if strings.TrimSpace(identifier) == "" {
		return fmt.Errorf("kick_session id 不能为空")
	}
	var r baseResp
	if err := c.call(ctx, "kick_session", map[string]string{"id": identifier}, &r); err != nil {
		return err
	}
	if r.Code != 0 {
		return fmt.Errorf("kick_session code=%d msg=%s", r.Code, r.Msg)
	}
	return nil
}

// KickSessions 驱逐(可选 filter)会话,返回被踢的会话数
//
// filter 留空 → 踢全部;支持 ZLM 的 local_port / peer_ip / id 三种 filter。
// 节点驱逐场景调用方一般传 nil。
func (c *Client) KickSessions(ctx context.Context, filter map[string]string) (int, error) {
	var r struct {
		baseResp
		CountHit *int `json:"count_hit"`
		Count    *int `json:"count"` // legacy ZLM response spelling
	}
	if err := c.call(ctx, "kick_sessions", filter, &r); err != nil {
		return 0, err
	}
	if r.Code != 0 {
		return 0, fmt.Errorf("kick_sessions code=%d msg=%s", r.Code, r.Msg)
	}
	if r.CountHit != nil {
		return *r.CountHit, nil
	}
	if r.Count != nil {
		return *r.Count, nil
	}
	return 0, nil
}

// CloseStreams 关闭(可选 filter)推流,返回被关闭的流数
//
// filter 支持 ZLM 的 schema / vhost / app / stream / force,留空踢全部。
func (c *Client) CloseStreams(ctx context.Context, filter map[string]string) (int, error) {
	result, err := c.CloseStreamsDetailed(ctx, filter)
	if err != nil {
		return 0, err
	}
	return result.CountClosed, nil
}

// CloseStreamsDetailed preserves the two independent counts returned by
// close_streams: count_hit is the number of matching media sources while
// count_closed is the number of asynchronous close requests submitted.
func (c *Client) CloseStreamsDetailed(ctx context.Context, filter map[string]string) (BatchCloseResult, error) {
	var r struct {
		baseResp
		CountHit    *int `json:"count_hit"`
		CountClosed *int `json:"count_closed"`
		Count       *int `json:"count"` // legacy response spelling
	}
	if err := c.call(ctx, "close_streams", filter, &r); err != nil {
		return BatchCloseResult{}, err
	}
	if r.Code != 0 {
		return BatchCloseResult{}, fmt.Errorf("close_streams code=%d msg=%s", r.Code, r.Msg)
	}
	if r.CountHit != nil || r.CountClosed != nil {
		result := BatchCloseResult{}
		if r.CountHit != nil {
			result.CountHit = *r.CountHit
		}
		if r.CountClosed != nil {
			result.CountClosed = *r.CountClosed
		}
		return result, nil
	}
	if r.Count != nil {
		return BatchCloseResult{CountHit: *r.Count, CountClosed: *r.Count}, nil
	}
	return BatchCloseResult{}, nil
}

// CloseStream closes exactly one media source through ZLM's singular API.
// The complete four-field identity is mandatory; callers must not substitute
// close_streams because its asynchronous batch semantics cannot prove that a
// particular target was closed.
func (c *Client) CloseStream(ctx context.Context, target StreamTarget, force bool) (CloseStreamResult, error) {
	if err := target.Validate(); err != nil {
		return CloseStreamResult{}, err
	}
	params := map[string]string{
		"schema": target.Schema,
		"vhost":  target.VHost,
		"app":    target.App,
		"stream": target.Stream,
	}
	if force {
		params["force"] = "1"
	} else {
		params["force"] = "0"
	}
	var r struct {
		baseResp
		Result int `json:"result"`
	}
	if err := c.call(ctx, "close_stream", params, &r); err != nil {
		return CloseStreamResult{}, err
	}
	if r.Code == -500 {
		return CloseStreamResult{}, ErrMediaNotFound
	}
	if r.Code != 0 || r.Result != 0 {
		return CloseStreamResult{Result: r.Result}, fmt.Errorf("close_stream code=%d result=%d", r.Code, r.Result)
	}
	return CloseStreamResult{Result: r.Result, Closed: true}, nil
}

// RestartServer 重启 ZLM 服务
//
// ZLM 的 /index/api/restartServer 接口不支持 grace 参数:接到请求即刻重启,所有流被强切,
// 客户端依靠自身重连机制恢复。graceMS 当前为接口预留(对齐云原生 graceful shutdown 语义),
// M3 阶段忽略,后续如 ZLM 支持再接入。
func (c *Client) RestartServer(ctx context.Context, _graceMS int) error {
	var r baseResp
	if err := c.call(ctx, "restartServer", nil, &r); err != nil {
		return err
	}
	if r.Code != 0 {
		return fmt.Errorf("restartServer code=%d msg=%s", r.Code, r.Msg)
	}
	return nil
}

// ThreadLoad is one getThreadsLoad/getWorkThreadsLoad entry. Load is the
// integer percentage reported by ZLM; FDCount is the descriptors owned by the
// event poller and may be zero on older ZLM versions.
type ThreadLoad struct {
	Name    string `json:"name"`
	Load    int    `json:"load"`
	FDCount int    `json:"fd_count"`
}

// AverageThreadLoad returns the average as 0-1 for the existing scheduler and
// heartbeat contracts while preserving the original entries for management UI.
func AverageThreadLoad(entries []ThreadLoad) float64 {
	if len(entries) == 0 {
		return 0
	}
	sum := 0
	for _, e := range entries {
		sum += e.Load
	}
	return float64(sum) / float64(len(entries)) / 100.0
}

func (c *Client) readThreadLoads(ctx context.Context, api string) ([]ThreadLoad, error) {
	var r struct {
		baseResp
		Data []ThreadLoad `json:"data"`
	}
	if err := c.call(ctx, api, nil, &r); err != nil {
		return nil, err
	}
	if r.Code != 0 {
		return nil, fmt.Errorf("%s code=%d msg=%s", api, r.Code, r.Msg)
	}
	return append([]ThreadLoad(nil), r.Data...), nil
}

// GetThreadsLoadDetail keeps the per-event-poller name, load and fd_count
// needed by the runtime dashboard.
func (c *Client) GetThreadsLoadDetail(ctx context.Context) ([]ThreadLoad, error) {
	return c.readThreadLoads(ctx, "getThreadsLoad")
}

// GetWorkThreadsLoadDetail is the equivalent detailed work-poller snapshot.
func (c *Client) GetWorkThreadsLoadDetail(ctx context.Context) ([]ThreadLoad, error) {
	return c.readThreadLoads(ctx, "getWorkThreadsLoad")
}

// GetThreadsLoad 拉 event poller 网络 I/O 线程负载平均(0-1)
//
// ZLM `/index/api/getThreadsLoad` 返 `{"data":[{"load":0,"name":"event poller 0"},...]}`
func (c *Client) GetThreadsLoad(ctx context.Context) (float64, error) {
	loads, err := c.GetThreadsLoadDetail(ctx)
	return AverageThreadLoad(loads), err
}

// GetWorkThreadsLoad 拉 work poller 工作线程负载平均(0-1)
func (c *Client) GetWorkThreadsLoad(ctx context.Context) (float64, error) {
	loads, err := c.GetWorkThreadsLoadDetail(ctx)
	return AverageThreadLoad(loads), err
}

// GetSnap 从 ZLM /index/api/getSnap 抓取指定流的 JPEG 快照。
//
// 与其他 API 不同,getSnap 成功时直接返回 image/jpeg 二进制,失败时返回 JSON envelope,
// 所以不复用 call()。
//
// streamURL:  完整流地址,如 http://host:port/app/stream.live.flv 或 rtsp://host:port/app/stream
// timeoutSec: 单次抓帧的最长等待秒数(ZLM 侧),通常 5 秒
// expireSec:  ZLM 本地缓存这张快照的秒数,点播快照使用 1 秒避免复用历史帧
//
// 返回 bytes 是完整 JPEG 内容(含 SOI 头 0xFF 0xD8 0xFF)。
func (c *Client) GetSnap(ctx context.Context, streamURL string, timeoutSec, expireSec int) ([]byte, error) {
	q := url.Values{}
	q.Set("secret", c.secret)
	q.Set("url", streamURL)
	q.Set("timeout_sec", strconv.Itoa(timeoutSec))
	q.Set("expire_sec", strconv.Itoa(expireSec))

	reqURL := c.baseURL + "/getSnap?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	// 单独的 http.Client,timeout = timeoutSec + 5s 兜底,不复用 c.http(默认 10s)
	httpClient := &http.Client{Timeout: time.Duration(timeoutSec+5) * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ZLM getSnap 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ZLM getSnap 读取响应失败: %w", err)
	}

	// 成功时必须是 JPEG。ZLM 抓帧失败时可能返回默认 PNG 占位图，不能把它
	// 当作通道快照落盘并刷新 snapshot_at。
	ct := resp.Header.Get("Content-Type")
	mediaType, _, mediaTypeErr := mime.ParseMediaType(ct)
	if mediaTypeErr != nil || !strings.EqualFold(mediaType, "image/jpeg") {
		// 尝试解析错误 envelope 给一条可读消息
		var errResp baseResp
		if json.Unmarshal(body, &errResp) == nil && errResp.Code != 0 {
			return nil, fmt.Errorf("ZLM getSnap 返回错误: code=%d msg=%s", errResp.Code, errResp.Msg)
		}
		return nil, fmt.Errorf("ZLM getSnap 未返回 JPEG: content-type=%s body=%.200s", ct, string(body))
	}
	if len(body) < 3 || body[0] != 0xFF || body[1] != 0xD8 || body[2] != 0xFF {
		return nil, fmt.Errorf("ZLM getSnap 返回内容不是有效 JPEG")
	}
	return body, nil
}
