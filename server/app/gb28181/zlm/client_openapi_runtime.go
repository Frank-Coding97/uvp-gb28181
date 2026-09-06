package zlm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrRuntimeControlUnavailable = errors.New("media runtime control unavailable")

type RuntimeIdentity struct {
	ProtocolVersion int    `json:"protocolVersion"`
	BootNonce       string `json:"bootNonce"`
}

type ConditionalKickResult string

const (
	KickShutdownScheduled ConditionalKickResult = "shutdown_scheduled"
	KickNotFound          ConditionalKickResult = "not_found"
	KickRuntimeMismatch   ConditionalKickResult = "runtime_mismatch"
)

// GetRuntimeIdentity is always a fresh probe. The caller must bind its result
// to the node's immutable configuration revision before persisting it.
func (c *Client) GetRuntimeIdentity(ctx context.Context) (RuntimeIdentity, error) {
	var identity RuntimeIdentity
	data, err := c.runtimeControl(ctx, "getRuntimeIdentity", nil)
	if err != nil || len(data) != 2 || json.Unmarshal(data["protocolVersion"], &identity.ProtocolVersion) != nil || json.Unmarshal(data["bootNonce"], &identity.BootNonce) != nil || identity.ProtocolVersion != 1 || !validRuntimeNonce(identity.BootNonce) {
		return RuntimeIdentity{}, ErrRuntimeControlUnavailable
	}
	return identity, nil
}

// KickSessionIfMatch never refreshes bootNonce, retries, or falls back to an
// ID-only API. None of its three results alone is proof of disconnection.
func (c *Client) KickSessionIfMatch(ctx context.Context, bootNonce, identifier string) (ConditionalKickResult, error) {
	if !validRuntimeNonce(bootNonce) || !validRuntimeSessionID(identifier) {
		return "", ErrRuntimeControlUnavailable
	}
	body, _ := json.Marshal(struct {
		BootNonce string `json:"bootNonce"`
		ID        string `json:"id"`
	}{bootNonce, identifier})
	data, err := c.runtimeControl(ctx, "kick_session_if_match", body)
	var result ConditionalKickResult
	if err != nil || len(data) != 1 || json.Unmarshal(data["result"], &result) != nil {
		return "", ErrRuntimeControlUnavailable
	}
	switch result {
	case KickShutdownScheduled, KickNotFound, KickRuntimeMismatch:
		return result, nil
	default:
		return "", ErrRuntimeControlUnavailable
	}
}

func (c *Client) runtimeControl(ctx context.Context, api string, body []byte) (map[string]json.RawMessage, error) {
	if c == nil || c.http == nil || c.secret == "" {
		return nil, ErrRuntimeControlUnavailable
	}
	endpoint, err := url.Parse(c.baseURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "/index/api" || endpoint.RawPath != "" {
		return nil, ErrRuntimeControlUnavailable
	}
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/"+api, bytes.NewReader(body))
	if err != nil {
		return nil, ErrRuntimeControlUnavailable
	}
	request.Header.Set("secret", c.secret)
	request.Header.Set("Cache-Control", "no-store")
	request.Header.Set("Pragma", "no-cache")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	// Copy the immutable transport configuration, without changing legacy calls.
	// In particular a redirect must never forward the custom secret header.
	transport := *c.http
	transport.Jar = nil
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if transport.Timeout <= 0 || transport.Timeout > 2*time.Second {
		transport.Timeout = 2 * time.Second
	}
	response, err := transport.Do(request)
	if err != nil {
		return nil, ErrRuntimeControlUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Age") != "" {
		return nil, ErrRuntimeControlUnavailable
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(content) > 4096 {
		return nil, ErrRuntimeControlUnavailable
	}
	envelope, err := runtimeObject(content)
	var code int
	if err != nil || envelope["code"] == nil || string(envelope["code"]) == "null" || json.Unmarshal(envelope["code"], &code) != nil || code != 0 {
		return nil, ErrRuntimeControlUnavailable
	}
	return runtimeObject(envelope["data"])
}

// Duplicate keys are rejected at both the envelope and security-data levels.
func runtimeObject(body []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, ErrRuntimeControlUnavailable
	}
	result := make(map[string]json.RawMessage)
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || result[name] != nil {
			return nil, ErrRuntimeControlUnavailable
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, ErrRuntimeControlUnavailable
		}
		result[name] = value
	}
	if _, err = decoder.Token(); err != nil {
		return nil, ErrRuntimeControlUnavailable
	}
	if decoder.Decode(new(json.RawMessage)) != io.EOF {
		return nil, ErrRuntimeControlUnavailable
	}
	return result, nil
}

func validRuntimeNonce(value string) bool {
	return len(value) == 32 && strings.Trim(value, "0123456789abcdef") == ""
}

func validRuntimeSessionID(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[0] < '1' || value[0] > '9' {
		return false
	}
	sequence, fd, found := strings.Cut(value, "-")
	fd = strings.TrimPrefix(fd, "-")
	return found && fd != "" && strings.Trim(sequence, "0123456789") == "" && strings.Trim(fd, "0123456789") == ""
}
