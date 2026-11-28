package zlm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	// ErrIngressInvalidRequest means that a typed ingress request is missing a
	// required field. The ZLM endpoint is not called for this error.
	ErrIngressInvalidRequest = errors.New("zlm ingress invalid request")
	ErrIngressResponse       = errors.New("zlm ingress response error")
)

// StreamProxyRequest describes a ZLM pull-stream proxy. Optional fields are
// omitted when zero-valued so ZLM keeps its configured defaults.
type StreamProxyRequest struct {
	VHost      string
	App        string
	Stream     string
	URL        string
	RetryCount int
	RTPType    int
	TimeoutSec float64
}

// StreamPusherProxyRequest describes a ZLM push-stream proxy.
type StreamPusherProxyRequest struct {
	Schema     string
	VHost      string
	App        string
	Stream     string
	DstURL     string
	RetryCount int
	RTPType    int
	TimeoutSec float64
}

// ProxyMediaTuple identifies the source media attached to a proxy.
type ProxyMediaTuple struct {
	VHost  string `json:"vhost"`
	App    string `json:"app"`
	Stream string `json:"stream"`
	Params string `json:"params"`
}

// StreamProxyInfo is the safe typed shape returned by listStreamProxy and
// listStreamPusherProxy. URL fields are retained for backend-only callers;
// management APIs must redact them before returning them to a browser.
type StreamProxyInfo struct {
	Key              string           `json:"key"`
	URL              string           `json:"url"`
	Status           int              `json:"status"`
	StatusStr        string           `json:"status_str"`
	LiveSecs         int64            `json:"liveSecs"`
	RePullCount      int              `json:"rePullCount"`
	RePublishCount   int              `json:"rePublishCount"`
	TotalReaderCount int              `json:"totalReaderCount"`
	BytesSpeed       uint64           `json:"bytesSpeed"`
	TotalBytes       uint64           `json:"totalBytes"`
	Src              *ProxyMediaTuple `json:"src"`
}

// StreamPusherProxyInfo uses the same typed response fields as a pull proxy.
type StreamPusherProxyInfo = StreamProxyInfo

// ProxyCreateResult is returned after ZLM accepts a proxy creation request.
type ProxyCreateResult struct {
	Key string
}

// ProxyDeleteResult reports whether ZLM actually removed a proxy. A false
// Hit is an idempotent "already absent" result, not a transport failure.
type ProxyDeleteResult struct {
	Key string
	Hit bool
}

// FFmpegSourceRequest describes the type-safe subset of addFFmpegSource. The
// client deliberately has no field for arbitrary shell command text.
type FFmpegSourceRequest struct {
	SrcURL       string
	DstURL       string
	TimeoutMS    int
	FFmpegCmdKey string
	EnableHLS    bool
	EnableMP4    bool
}

// FFmpegSourceInfo is the backend-safe subset of listFFmpegSource. ZLM also
// returns the expanded command, but it is intentionally not exposed here.
type FFmpegSourceInfo struct {
	Key          string `json:"key"`
	SrcURL       string `json:"src_url"`
	DstURL       string `json:"dst_url"`
	FFmpegCmdKey string `json:"ffmpeg_cmd_key"`
}

// FFmpegSourceResult is kept distinct at the API level while sharing the
// stable key response shape with stream proxies.
type FFmpegSourceResult = ProxyCreateResult

// RtpServerInfo is the normalized listRtpServer response. SSRC is a string so
// callers do not lose identity precision or leading zeroes when ZLM changes
// between numeric and string JSON encodings.
type RtpServerInfo struct {
	Key       string `json:"key"`
	VHost     string `json:"vhost"`
	App       string `json:"app"`
	StreamID  string `json:"stream_id"`
	SSRC      string `json:"ssrc"`
	Port      int    `json:"port"`
	TCPMode   int    `json:"tcp_mode"`
	OnlyTrack int    `json:"only_track"`
	Released  bool   `json:"released"`
}

// RtpServer is a compatibility spelling for callers that use the endpoint's
// protocol name as a Go type.
type RtpServer = RtpServerInfo

// RTPServerInfo is an acronym-compatible alias for RtpServerInfo.
type RTPServerInfo = RtpServerInfo

// RtpServerCloseResult reports whether closeRtpServer removed a live entry.
type RtpServerCloseResult struct {
	StreamID string
	Hit      bool
	Released bool
}

// RTPServerCloseResult is an acronym-compatible alias.
type RTPServerCloseResult = RtpServerCloseResult

// redactIngressError keeps errors.Is/As useful while removing source and
// target URLs from transport errors that can contain the complete request URL.
type redactIngressError struct {
	err     error
	secrets []string
	urls    []string
}

func (e redactIngressError) Error() string {
	values := make([]string, 0, len(e.secrets)+len(e.urls))
	values = append(values, e.secrets...)
	values = append(values, e.urls...)
	return redactIngressText(e.err.Error(), values...)
}

func (e redactIngressError) Unwrap() error { return e.err }

var ingressURLPattern = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>]+`)

func redactIngressText(message string, values ...string) string {
	for _, value := range values {
		if value == "" {
			continue
		}
		for _, encoded := range []string{value, url.QueryEscape(value), url.PathEscape(value)} {
			if encoded != "" {
				message = strings.ReplaceAll(message, encoded, "<redacted-url>")
			}
		}
	}
	return ingressURLPattern.ReplaceAllString(message, "<redacted-url>")
}

func (c *Client) callIngress(ctx context.Context, api string, params map[string]string, out interface{}, urls ...string) error {
	if err := c.call(ctx, api, params, out); err != nil {
		return redactIngressError{err: err, secrets: []string{c.secret}, urls: urls}
	}
	return nil
}

func newIngressResponseError(api string, response baseResp, values ...string) error {
	err := fmt.Errorf("%w: %s code=%d msg=%s", ErrIngressResponse, api, response.Code, response.Msg)
	return redactIngressError{err: err, urls: values}
}

func (c *Client) ingressResponseError(api string, response baseResp, urls ...string) error {
	values := make([]string, 0, len(urls)+1)
	values = append(values, c.secret)
	values = append(values, urls...)
	return newIngressResponseError(api, response, values...)
}

func ingressInvalidRequest(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrIngressInvalidRequest, fmt.Sprintf(format, args...))
}

func requireIngressFields(fields ...string) error {
	for i := 0; i+1 < len(fields); i += 2 {
		if strings.TrimSpace(fields[i+1]) == "" {
			return ingressInvalidRequest("%s 不能为空", fields[i])
		}
	}
	return nil
}

func addOptionalProxyParams(params map[string]string, retryCount, rtpType int, timeoutSec float64) error {
	if retryCount != 0 {
		params["retry_count"] = strconv.Itoa(retryCount)
	}
	if rtpType != 0 {
		params["rtp_type"] = strconv.Itoa(rtpType)
	}
	if timeoutSec < 0 {
		return ingressInvalidRequest("timeout_sec 不能为负数")
	}
	if timeoutSec > 0 {
		params["timeout_sec"] = strconv.FormatFloat(timeoutSec, 'f', -1, 64)
	}
	return nil
}

func (c *Client) AddStreamProxy(ctx context.Context, in StreamProxyRequest) (*ProxyCreateResult, error) {
	if err := requireIngressFields("vhost", in.VHost, "app", in.App, "stream", in.Stream, "url", in.URL); err != nil {
		return nil, err
	}
	params := map[string]string{
		"vhost":  in.VHost,
		"app":    in.App,
		"stream": in.Stream,
		"url":    in.URL,
	}
	if err := addOptionalProxyParams(params, in.RetryCount, in.RTPType, in.TimeoutSec); err != nil {
		return nil, err
	}
	var response struct {
		baseResp
		Data struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	if err := c.callIngress(ctx, "addStreamProxy", params, &response, in.URL); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, c.ingressResponseError("addStreamProxy", response.baseResp, in.URL)
	}
	if strings.TrimSpace(response.Data.Key) == "" {
		return nil, fmt.Errorf("%w: addStreamProxy 缺少 data.key", ErrIngressResponse)
	}
	return &ProxyCreateResult{Key: response.Data.Key}, nil
}

func (c *Client) AddStreamPusherProxy(ctx context.Context, in StreamPusherProxyRequest) (*ProxyCreateResult, error) {
	if err := requireIngressFields("schema", in.Schema, "vhost", in.VHost, "app", in.App, "stream", in.Stream, "dst_url", in.DstURL); err != nil {
		return nil, err
	}
	params := map[string]string{
		"schema":  in.Schema,
		"vhost":   in.VHost,
		"app":     in.App,
		"stream":  in.Stream,
		"dst_url": in.DstURL,
	}
	if err := addOptionalProxyParams(params, in.RetryCount, in.RTPType, in.TimeoutSec); err != nil {
		return nil, err
	}
	var response struct {
		baseResp
		Data struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	if err := c.callIngress(ctx, "addStreamPusherProxy", params, &response, in.DstURL); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, c.ingressResponseError("addStreamPusherProxy", response.baseResp, in.DstURL)
	}
	if strings.TrimSpace(response.Data.Key) == "" {
		return nil, fmt.Errorf("%w: addStreamPusherProxy 缺少 data.key", ErrIngressResponse)
	}
	return &ProxyCreateResult{Key: response.Data.Key}, nil
}

func decodeIngressList[T any](raw json.RawMessage, setKey func(*T, string)) ([]T, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []T{}, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var list []T
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("%w: list data: %v", ErrIngressResponse, err)
		}
		return list, nil
	}
	var byKey map[string]T
	if err := json.Unmarshal(raw, &byKey); err != nil {
		return nil, fmt.Errorf("%w: list data: %v", ErrIngressResponse, err)
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	list := make([]T, 0, len(keys))
	for _, key := range keys {
		item := byKey[key]
		setKey(&item, key)
		list = append(list, item)
	}
	return list, nil
}

func listKeyParams(keys []string) (map[string]string, error) {
	if len(keys) > 1 {
		return nil, ingressInvalidRequest("最多只能指定一个 key")
	}
	params := map[string]string{}
	if len(keys) == 1 && strings.TrimSpace(keys[0]) != "" {
		params["key"] = keys[0]
	}
	return params, nil
}

func (c *Client) ListStreamProxy(ctx context.Context, key ...string) ([]StreamProxyInfo, error) {
	params, err := listKeyParams(key)
	if err != nil {
		return nil, err
	}
	var response struct {
		baseResp
		Data json.RawMessage `json:"data"`
	}
	if err := c.callIngress(ctx, "listStreamProxy", params, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, c.ingressResponseError("listStreamProxy", response.baseResp)
	}
	return decodeIngressList(response.Data, func(item *StreamProxyInfo, key string) {
		if item.Key == "" {
			item.Key = key
		}
	})
}

func (c *Client) ListStreamPusherProxy(ctx context.Context, key ...string) ([]StreamPusherProxyInfo, error) {
	params, err := listKeyParams(key)
	if err != nil {
		return nil, err
	}
	var response struct {
		baseResp
		Data json.RawMessage `json:"data"`
	}
	if err := c.callIngress(ctx, "listStreamPusherProxy", params, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, c.ingressResponseError("listStreamPusherProxy", response.baseResp)
	}
	return decodeIngressList(response.Data, func(item *StreamPusherProxyInfo, key string) {
		if item.Key == "" {
			item.Key = key
		}
	})
}

func (c *Client) DeleteStreamProxyWithResult(ctx context.Context, key string) (*ProxyDeleteResult, error) {
	if strings.TrimSpace(key) == "" {
		return nil, ingressInvalidRequest("key 不能为空")
	}
	var response struct {
		baseResp
		Data struct {
			Flag bool `json:"flag"`
			Hit  int  `json:"hit"`
		} `json:"data"`
	}
	if err := c.callIngress(ctx, "delStreamProxy", map[string]string{"key": key}, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, c.ingressResponseError("delStreamProxy", response.baseResp)
	}
	return &ProxyDeleteResult{Key: key, Hit: response.Data.Flag || response.Data.Hit != 0}, nil
}

func (c *Client) DeleteStreamProxy(ctx context.Context, key string) error {
	_, err := c.DeleteStreamProxyWithResult(ctx, key)
	return err
}

func (c *Client) DeleteStreamPusherProxyWithResult(ctx context.Context, key string) (*ProxyDeleteResult, error) {
	if strings.TrimSpace(key) == "" {
		return nil, ingressInvalidRequest("key 不能为空")
	}
	var response struct {
		baseResp
		Data struct {
			Flag bool `json:"flag"`
			Hit  int  `json:"hit"`
		} `json:"data"`
	}
	if err := c.callIngress(ctx, "delStreamPusherProxy", map[string]string{"key": key}, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, c.ingressResponseError("delStreamPusherProxy", response.baseResp)
	}
	return &ProxyDeleteResult{Key: key, Hit: response.Data.Flag || response.Data.Hit != 0}, nil
}

func (c *Client) DeleteStreamPusherProxy(ctx context.Context, key string) error {
	_, err := c.DeleteStreamPusherProxyWithResult(ctx, key)
	return err
}

func (c *Client) AddFFmpegSource(ctx context.Context, in FFmpegSourceRequest) (*FFmpegSourceResult, error) {
	if err := requireIngressFields("src_url", in.SrcURL, "dst_url", in.DstURL); err != nil {
		return nil, err
	}
	if in.TimeoutMS <= 0 {
		return nil, ingressInvalidRequest("timeout_ms 必须大于 0")
	}
	params := map[string]string{
		"src_url":    in.SrcURL,
		"dst_url":    in.DstURL,
		"timeout_ms": strconv.Itoa(in.TimeoutMS),
		"enable_hls": boolParam(in.EnableHLS),
		"enable_mp4": boolParam(in.EnableMP4),
	}
	if strings.TrimSpace(in.FFmpegCmdKey) != "" {
		params["ffmpeg_cmd_key"] = in.FFmpegCmdKey
	}
	var response struct {
		baseResp
		Data struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	if err := c.callIngress(ctx, "addFFmpegSource", params, &response, in.SrcURL, in.DstURL); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, c.ingressResponseError("addFFmpegSource", response.baseResp, in.SrcURL, in.DstURL)
	}
	if strings.TrimSpace(response.Data.Key) == "" {
		return nil, fmt.Errorf("%w: addFFmpegSource 缺少 data.key", ErrIngressResponse)
	}
	return &FFmpegSourceResult{Key: response.Data.Key}, nil
}

func boolParam(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func (c *Client) ListFFmpegSources(ctx context.Context) ([]FFmpegSourceInfo, error) {
	var response struct {
		baseResp
		Data json.RawMessage `json:"data"`
	}
	if err := c.callIngress(ctx, "listFFmpegSource", nil, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, c.ingressResponseError("listFFmpegSource", response.baseResp)
	}
	return decodeIngressList(response.Data, func(item *FFmpegSourceInfo, key string) {
		if item.Key == "" {
			item.Key = key
		}
	})
}

// ListFFmpegSource retains the endpoint's singular spelling for callers that
// mirror the ZLM API name.
func (c *Client) ListFFmpegSource(ctx context.Context) ([]FFmpegSourceInfo, error) {
	return c.ListFFmpegSources(ctx)
}

func (c *Client) DeleteFFmpegSourceWithResult(ctx context.Context, key string) (*ProxyDeleteResult, error) {
	if strings.TrimSpace(key) == "" {
		return nil, ingressInvalidRequest("key 不能为空")
	}
	var response struct {
		baseResp
		Data struct {
			Flag bool `json:"flag"`
			Hit  int  `json:"hit"`
		} `json:"data"`
	}
	if err := c.callIngress(ctx, "delFFmpegSource", map[string]string{"key": key}, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, c.ingressResponseError("delFFmpegSource", response.baseResp)
	}
	return &ProxyDeleteResult{Key: key, Hit: response.Data.Flag || response.Data.Hit != 0}, nil
}

func (c *Client) DeleteFFmpegSource(ctx context.Context, key string) error {
	_, err := c.DeleteFFmpegSourceWithResult(ctx, key)
	return err
}

func (r *RtpServerInfo) UnmarshalJSON(data []byte) error {
	type rtpWire struct {
		Key       string          `json:"key"`
		VHost     string          `json:"vhost"`
		App       string          `json:"app"`
		StreamID  string          `json:"stream_id"`
		SSRC      json.RawMessage `json:"ssrc"`
		Port      int             `json:"port"`
		TCPMode   int             `json:"tcp_mode"`
		OnlyTrack int             `json:"only_track"`
		Released  bool            `json:"released"`
		Closed    bool            `json:"closed"`
		Status    string          `json:"status"`
	}
	var wire rtpWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	r.Key, r.VHost, r.App, r.StreamID = wire.Key, wire.VHost, wire.App, wire.StreamID
	r.Port, r.TCPMode, r.OnlyTrack = wire.Port, wire.TCPMode, wire.OnlyTrack
	r.Released = wire.Released || wire.Closed || strings.EqualFold(wire.Status, "released") || strings.EqualFold(wire.Status, "closed")
	if len(wire.SSRC) == 0 || string(wire.SSRC) == "null" {
		r.SSRC = ""
		return nil
	}
	var asString string
	if err := json.Unmarshal(wire.SSRC, &asString); err == nil {
		r.SSRC = asString
		return nil
	}
	var asNumber json.Number
	if err := json.Unmarshal(wire.SSRC, &asNumber); err != nil {
		return fmt.Errorf("ssrc: %w", err)
	}
	r.SSRC = asNumber.String()
	return nil
}

func (c *Client) ListRtpServers(ctx context.Context) ([]RtpServerInfo, error) {
	var response struct {
		baseResp
		Data json.RawMessage `json:"data"`
	}
	if err := c.callIngress(ctx, "listRtpServer", nil, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, c.ingressResponseError("listRtpServer", response.baseResp)
	}
	return decodeIngressList(response.Data, func(item *RtpServerInfo, key string) {
		if item.Key == "" {
			item.Key = key
		}
	})
}

// CloseRtpServerWithResult carries the hit bit that the legacy
// CloseRtpServer wrapper intentionally discards. Released=true makes an
// already absent server distinguishable from a live resource.
func (c *Client) CloseRtpServerWithResult(ctx context.Context, vhost, appName, streamID string) (*RtpServerCloseResult, error) {
	if err := requireIngressFields("stream_id", streamID); err != nil {
		return nil, err
	}
	params := map[string]string{"stream_id": streamID}
	if strings.TrimSpace(vhost) != "" {
		params["vhost"] = vhost
	}
	if strings.TrimSpace(appName) != "" {
		params["app"] = appName
	}
	var response struct {
		baseResp
		Hit  int `json:"hit"`
		Data struct {
			Flag bool `json:"flag"`
		} `json:"data"`
	}
	if err := c.callIngress(ctx, "closeRtpServer", params, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, c.ingressResponseError("closeRtpServer", response.baseResp)
	}
	hit := response.Hit != 0 || response.Data.Flag
	return &RtpServerCloseResult{StreamID: streamID, Hit: hit, Released: !hit}, nil
}

// CloseRtpServerStatus is a descriptive alias for new management callers.
func (c *Client) CloseRtpServerStatus(ctx context.Context, vhost, appName, streamID string) (*RtpServerCloseResult, error) {
	return c.CloseRtpServerWithResult(ctx, vhost, appName, streamID)
}
