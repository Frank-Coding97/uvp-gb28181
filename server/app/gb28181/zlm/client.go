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

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
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

// MediaInfo 单路流元信息(getMediaInfo / isMediaOnline 部分字段)
type MediaInfo struct {
	Online      bool   // 是否已就绪(对应 ZLM online 字段)
	Schema      string // 协议:rtmp/rtsp/hls/...
	App         string // 应用名
	Stream      string // 流 id
	ReaderCount int    // 当前观众数
}

// IsMediaOnline 轻量探测一路流是否就绪(返回 online 标志)
// 用于点播流就绪等待的轮询备份(hook + polling 双源)
func (c *Client) IsMediaOnline(ctx context.Context, app, stream string) (bool, error) {
	var r struct {
		baseResp
		Online bool `json:"online"`
	}
	params := map[string]string{
		"vhost":  "__defaultVhost__",
		"app":    app,
		"stream": stream,
		// schema 不传:rtp 接入后会自动产生 rtsp/rtmp/hls 多协议,任一就绪即可
	}
	if err := c.call(ctx, "isMediaOnline", params, &r); err != nil {
		return false, err
	}
	// code != 0(如 -500 流不存在)视为未就绪,不视为错误
	if r.Code != 0 {
		return false, nil
	}
	return r.Online, nil
}

// GetMediaInfo 查询单路流详情(verify-after-hook 用)
// 返回 online=false 表示流未就绪(包含"流不存在"和"流存在但暂无数据"两种情况)
func (c *Client) GetMediaInfo(ctx context.Context, app, stream string) (*MediaInfo, error) {
	var r struct {
		baseResp
		Online      bool   `json:"online"`
		Schema      string `json:"schema"`
		App         string `json:"app"`
		Stream      string `json:"stream"`
		ReaderCount int    `json:"readerCount"`
	}
	params := map[string]string{
		"vhost":  "__defaultVhost__",
		"app":    app,
		"stream": stream,
	}
	if err := c.call(ctx, "getMediaInfo", params, &r); err != nil {
		return nil, err
	}
	if r.Code != 0 {
		// 流不存在:online=false,不报错
		return &MediaInfo{App: app, Stream: stream}, nil
	}
	return &MediaInfo{
		Online:      r.Online,
		Schema:      r.Schema,
		App:         r.App,
		Stream:      r.Stream,
		ReaderCount: r.ReaderCount,
	}, nil
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
