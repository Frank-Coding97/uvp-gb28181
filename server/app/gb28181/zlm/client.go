package zlm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// Client ZLMediaKit HTTP API 客户端(控制面)
// 一个 Client 绑一个 node;多节点场景每节点一个 Client(无连接池,Go http.Client 自带)。
type Client struct {
	node    *node.Node
	baseURL string
	secret  string
	http    *http.Client
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
		return fmt.Errorf("ZLM 请求失败 %s: %w", api, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("ZLM 响应解析失败 %s: %w, body=%s", api, err, string(body))
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

// OpenRtpServer 申请一个 RTP 收流端口
// streamID = ZLM 内 stream 标识;ssrc 用于单端口模式按 SSRC 分流(传 "" 则不限);port=0 让 ZLM 自选
func (c *Client) OpenRtpServer(ctx context.Context, streamID string, port int, tcpMode int) (*OpenRtpServerResult, error) {
	var r struct {
		baseResp
		Port int `json:"port"`
	}
	params := map[string]string{
		"stream_id": streamID,
		"port":      strconv.Itoa(port),
		"tcp_mode":  strconv.Itoa(tcpMode), // 0=UDP 1=TCP被动
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
	// code!=0 不一定是错误(可能流已关),仅记录
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
	FPS           int      `json:"fps"`
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
	Tracks           []MediaTrack `json:"tracks"`
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
	if err := c.call(ctx, "getMediaInfo", params, &r); err != nil {
		return nil, err
	}
	if r.Code != 0 {
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

// KickSessions 驱逐(可选 filter)会话,返回被踢的会话数
//
// filter 留空 → 踢全部;支持 ZLM 的 local_port / peer_ip / id 三种 filter。
// 节点驱逐场景调用方一般传 nil。
func (c *Client) KickSessions(ctx context.Context, filter map[string]string) (int, error) {
	var r struct {
		baseResp
		Count int `json:"count"`
	}
	if err := c.call(ctx, "kick_sessions", filter, &r); err != nil {
		return 0, err
	}
	if r.Code != 0 {
		return 0, fmt.Errorf("kick_sessions code=%d msg=%s", r.Code, r.Msg)
	}
	return r.Count, nil
}

// CloseStreams 关闭(可选 filter)推流,返回被关闭的流数
//
// filter 支持 ZLM 的 schema / vhost / app / stream / force,留空踢全部。
func (c *Client) CloseStreams(ctx context.Context, filter map[string]string) (int, error) {
	var r struct {
		baseResp
		Count int `json:"count"`
	}
	if err := c.call(ctx, "close_streams", filter, &r); err != nil {
		return 0, err
	}
	if r.Code != 0 {
		return 0, fmt.Errorf("close_streams code=%d msg=%s", r.Code, r.Msg)
	}
	return r.Count, nil
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

// threadLoadEntry getThreadsLoad / getWorkThreadsLoad 单条
type threadLoadEntry struct {
	Load int `json:"load"`
}

// avgLoad 取多线程负载平均(load 是 int 0-100,返 0-1 float)
func avgLoad(entries []threadLoadEntry) float64 {
	if len(entries) == 0 {
		return 0
	}
	sum := 0
	for _, e := range entries {
		sum += e.Load
	}
	return float64(sum) / float64(len(entries)) / 100.0
}

// GetThreadsLoad 拉 event poller 网络 I/O 线程负载平均(0-1)
//
// ZLM `/index/api/getThreadsLoad` 返 `{"data":[{"load":0,"name":"event poller 0"},...]}`
func (c *Client) GetThreadsLoad(ctx context.Context) (float64, error) {
	var r struct {
		baseResp
		Data []threadLoadEntry `json:"data"`
	}
	if err := c.call(ctx, "getThreadsLoad", nil, &r); err != nil {
		return 0, err
	}
	if r.Code != 0 {
		return 0, fmt.Errorf("getThreadsLoad code=%d msg=%s", r.Code, r.Msg)
	}
	return avgLoad(r.Data), nil
}

// GetWorkThreadsLoad 拉 work poller 工作线程负载平均(0-1)
func (c *Client) GetWorkThreadsLoad(ctx context.Context) (float64, error) {
	var r struct {
		baseResp
		Data []threadLoadEntry `json:"data"`
	}
	if err := c.call(ctx, "getWorkThreadsLoad", nil, &r); err != nil {
		return 0, err
	}
	if r.Code != 0 {
		return 0, fmt.Errorf("getWorkThreadsLoad code=%d msg=%s", r.Code, r.Msg)
	}
	return avgLoad(r.Data), nil
}

// GetSnap 从 ZLM /index/api/getSnap 抓取指定流的 JPEG 快照。
//
// 与其他 API 不同,getSnap 成功时直接返回 image/jpeg 二进制,失败时返回 JSON envelope,
// 所以不复用 call()。
//
// streamURL:  完整流地址,如 http://host:port/app/stream.live.flv 或 rtsp://host:port/app/stream
// timeoutSec: 单次抓帧的最长等待秒数(ZLM 侧),通常 5 秒
// expireSec:  ZLM 本地缓存这张快照的秒数,建议跟 dedup TTL 对齐(30 秒)
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

	// 成功时 Content-Type 是 image/jpeg 或 image/png(ZLM 抓帧失败时返回 PNG 占位图);
	// 失败时是 application/json envelope
	ct := resp.Header.Get("Content-Type")
	if !isImageContentType(ct) {
		// 尝试解析错误 envelope 给一条可读消息
		var errResp baseResp
		if json.Unmarshal(body, &errResp) == nil && errResp.Code != 0 {
			return nil, fmt.Errorf("ZLM getSnap 返回错误: code=%d msg=%s", errResp.Code, errResp.Msg)
		}
		return nil, fmt.Errorf("ZLM getSnap 返回非图片内容: content-type=%s body=%.200s", ct, string(body))
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("ZLM getSnap 返回空 body")
	}
	return body, nil
}

// isImageContentType 宽松匹配 image/*(ZLM 抓帧失败会返回默认 PNG 占位图,也需要接受)
func isImageContentType(ct string) bool {
	const prefix = "image/"
	if len(ct) < len(prefix) {
		return false
	}
	return ct[:len(prefix)] == prefix
}
