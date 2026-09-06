package zlm

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// ProbeConfiguration checks control-plane readback without changing config or
// starting media. The caller must serialize the whole probe per node and CAS
// its result against the starting meta_node revision. A successful result is
// NOT deployment/topology qualification and is NOT proof an older process exited.
// Requires the matching ZLM build that suppresses config API debug dumps.
func (c *OpenAPIRuntimeControl) ProbeConfiguration(ctx context.Context, hookBase string) (RuntimeIdentity, error) {
	if c == nil || c.client == nil || c.client.node == nil || ctx == nil || !c.client.node.IsActive() || c.client.node.RecoveryRequired {
		return RuntimeIdentity{}, ErrRuntimeControlUnavailable
	}
	base, err := url.Parse(hookBase)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.RawPath != "" || base.Opaque != "" || base.String() != hookBase {
		return RuntimeIdentity{}, ErrRuntimeControlUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	first, err := c.GetRuntimeIdentity(ctx)
	if err != nil {
		return RuntimeIdentity{}, ErrRuntimeControlUnavailable
	}
	config, err := c.readConfiguration(ctx)
	if err != nil || config["general.mediaServerId"] != c.client.node.MediaServerUUID || config["hook.enable"] != "1" || config["general.flowThreshold"] != "0" || config["api.apiDebug"] != "0" || !validOpenAPIHookBudget(config) {
		return RuntimeIdentity{}, ErrRuntimeControlUnavailable
	}
	for _, event := range playauth.ManagedHookEvents() {
		expected, err := buildManagedHookURL(base, c.client.secret, c.client.node.MediaServerUUID, event)
		if err != nil || config["hook."+string(event)] != expected {
			return RuntimeIdentity{}, ErrRuntimeControlUnavailable
		}
	}
	sessions, err := c.GetRuntimeSessions(ctx)
	if err != nil || sessions.BootNonce != first.BootNonce {
		return RuntimeIdentity{}, ErrRuntimeControlUnavailable
	}
	last, err := c.GetRuntimeIdentity(ctx)
	if err != nil || first != last {
		return RuntimeIdentity{}, ErrRuntimeControlUnavailable
	}
	return last, nil
}

func validOpenAPIHookBudget(config map[string]string) bool {
	timeout, err := strconv.ParseFloat(config["hook.timeoutSec"], 64)
	if err != nil || math.IsNaN(timeout) || math.IsInf(timeout, 0) || timeout <= 0 || timeout >= 5 {
		return false
	}
	retries, err := strconv.Atoi(config["hook.retry"])
	if err != nil || retries < 0 || retries > 100 {
		return false
	}
	delay, err := strconv.ParseFloat(config["hook.retry_delay"], 64)
	if err != nil || math.IsNaN(delay) || math.IsInf(delay, 0) || delay < 0 {
		return false
	}
	return timeout*float64(retries+1)+delay*float64(retries) < 5
}

// Keep the raw configuration local: it contains node secrets and Hook
// capabilities, and must not be returned as a diagnostic or attached to errors.
func (c *OpenAPIRuntimeControl) readConfiguration(ctx context.Context) (map[string]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.client.baseURL+"/getServerConfig", nil)
	if err != nil {
		return nil, ErrRuntimeControlUnavailable
	}
	request.Header.Set("secret", c.client.secret)
	request.Header.Set("Cache-Control", "no-store")
	request.Header.Set("Pragma", "no-cache")
	response, err := c.client.http.Do(request)
	if err != nil {
		return nil, ErrRuntimeControlUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ProtoMajor != 1 || response.Header.Get("Age") != "" {
		return nil, ErrRuntimeControlUnavailable
	}
	const maxBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil || len(body) > maxBytes {
		return nil, ErrRuntimeControlUnavailable
	}
	envelope, err := runtimeObject(body)
	var code int
	var rows []json.RawMessage
	if err != nil || envelope["code"] == nil || string(envelope["code"]) == "null" || json.Unmarshal(envelope["code"], &code) != nil || code != 0 || json.Unmarshal(envelope["data"], &rows) != nil || len(rows) != 1 {
		return nil, ErrRuntimeControlUnavailable
	}
	fields, err := runtimeObject(rows[0])
	if err != nil {
		return nil, ErrRuntimeControlUnavailable
	}
	config := make(map[string]string, len(fields))
	for key, raw := range fields {
		var value string
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			return nil, ErrRuntimeControlUnavailable
		}
		config[key] = value
	}
	return config, nil
}
