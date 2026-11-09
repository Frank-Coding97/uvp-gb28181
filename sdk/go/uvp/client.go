package uvp

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client 是最小可用客户端：签名、发请求、拆信封、映射错误。
// 不做自动重试 —— GET 超时可以用新 nonce 重签重试，
// POST 超时**不要**自动重试，结果未知时用同一个 Idempotency-Key 决定是否重放。
type Client struct {
	BaseURL   string
	AccessKey string
	SecretKey string
	Audience  string
	HTTP      *http.Client
	Timeout   time.Duration
}

// ApiError 是平台返回的非 2xx。RequestID 是排障的唯一线索，务必打日志。
type ApiError struct {
	Status    int
	Code      string
	Message   string
	RequestID string
	Body      string
}

func (e *ApiError) Error() string {
	return fmt.Sprintf("%d %s (requestId=%s)", e.Status, e.Message, e.RequestID)
}

// NewClient 校验 SK 可以在本地解码，避免第一次请求才发现配置错了。
func NewClient(baseURL, accessKey, secretKey, audience string) (*Client, error) {
	if _, err := DecodeSecretKey(secretKey); err != nil {
		return nil, fmt.Errorf("secret key 必须解码成 32 字节: %w", err)
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return nil, fmt.Errorf("baseURL 形如 http://192.168.1.220:51002")
	}
	return &Client{
		BaseURL:   strings.TrimSuffix(baseURL, "/"),
		AccessKey: accessKey,
		SecretKey: secretKey,
		Audience:  audience,
		HTTP:      &http.Client{Timeout: 15 * time.Second},
		Timeout:   15 * time.Second,
	}, nil
}

// Get 发起一次 GET，返回 data 部分（非 2xx 返回 *ApiError）。
func (c *Client) Get(path string, query map[string]string) (json.RawMessage, error) {
	return c.Do("GET", path, BuildQuery(query), nil, "")
}

// Post 发起一次 POST。body 必须是最终字节，不要传结构体让框架再序列化。
func (c *Client) Post(path string, body []byte, idempotencyKey string) (json.RawMessage, error) {
	return c.Do("POST", path, "", body, idempotencyKey)
}

// Do 是所有请求的入口。
func (c *Client) Do(method, path, rawQuery string, body []byte, idempotencyKey string) (json.RawMessage, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	headers, err := SignRequest(c.AccessKey, c.SecretKey, c.Audience, method, path,
		rawQuery, body, idempotencyKey, timestamp, nonce)
	if err != nil {
		return nil, err
	}

	target := c.BaseURL + path
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequest(method, target, reader)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, toApiError(response.StatusCode, payload)
	}

	var envelope struct {
		Code      string          `json:"code"`
		Message   string          `json:"message"`
		RequestID string          `json:"requestId"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("响应不是 JSON 信封: %s", truncate(string(payload), 200))
	}
	return envelope.Data, nil
}

// BuildQuery 拼 rawQuery（值已 RFC3986 编码，不含 +）。
func BuildQuery(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	parts := make([]string, 0, len(params))
	for key, value := range params {
		parts = append(parts, EncodeValue(key)+"="+EncodeValue(value))
	}
	return strings.Join(parts, "&")
}

func newNonce() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func toApiError(status int, payload []byte) *ApiError {
	apiError := &ApiError{Status: status, Body: truncate(string(payload), 2000)}
	var envelope struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil {
		apiError.Code = envelope.Code
		apiError.Message = envelope.Message
		apiError.RequestID = envelope.RequestID
	}
	if apiError.Message == "" {
		apiError.Message = truncate(string(payload), 200)
	}
	return apiError
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
