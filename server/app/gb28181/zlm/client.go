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

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

// Client ZLMediaKit HTTP API 客户端(控制面)
type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

// NewClient 创建 ZLM 客户端
func NewClient(cfg gbconfig.ZLMConfig) *Client {
	return &Client{
		baseURL: fmt.Sprintf("http://%s:%d/index/api", cfg.Host, cfg.HTTPPort),
		secret:  cfg.Secret,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

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
