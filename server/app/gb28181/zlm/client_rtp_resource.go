package zlm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// RtpResourceSelector is an internal, fixed resource identity. Persist it with
// the node/config revision BEFORE dispatch; never replace its boot or ID during
// retry. Protocol v1 supports the canonical default vhost only.
type RtpResourceSelector struct {
	BootNonce  string `json:"bootNonce"`
	ResourceID string `json:"resourceID"`
	VHost      string `json:"vhost"`
	App        string `json:"app"`
	Stream     string `json:"stream"`
}

// v1 is non-multiplexed, reuse=false, and UDP or TCP passive only.
type RtpResourceOpenRequest struct {
	RtpResourceSelector
	Port      int    `json:"port"`
	LocalIP   string `json:"localIP"`
	TCPMode   int    `json:"tcpMode"`
	SSRC      uint32 `json:"ssrc"`
	OnlyTrack int    `json:"onlyTrack"`
}

type RtpResourceResult string

const (
	RtpResourceCreated           RtpResourceResult = "created"
	RtpResourceExisting          RtpResourceResult = "existing"
	RtpResourceShutdownScheduled RtpResourceResult = "shutdown_scheduled"
	RtpResourceClosePending      RtpResourceResult = "close_pending"
	RtpResourceNotActiveFenced   RtpResourceResult = "not_active_fenced"
	RtpResourceRetired           RtpResourceResult = "resource_retired"
	RtpResourceConflict          RtpResourceResult = "resource_conflict"
	RtpResourceMismatch          RtpResourceResult = "resource_mismatch"
	RtpResourceExpired           RtpResourceResult = "resource_expired"
	RtpResourceCapacity          RtpResourceResult = "capacity_exhausted"
	RtpResourceRuntimeMismatch   RtpResourceResult = "runtime_mismatch"
)

type RtpResourceOpenResult struct {
	Result RtpResourceResult
	Port   int
}

// NewRtpResourceID gives the caller 25 seconds to create, leaving five seconds
// below the server's 30-second limit for small clock skew. The server owns the
// deadline decision. Generate once per concrete resource, then persist; this
// helper must never be used to refresh the identity of an uncertain operation.
func NewRtpResourceID(now time.Time) (string, error) {
	return newRtpResourceID(now, rand.Reader)
}

func newRtpResourceID(now time.Time, entropy io.Reader) (string, error) {
	millis := now.UnixMilli()
	if millis < 1000000000000 || millis > 9999999999999-25000 || entropy == nil {
		return "", ErrRuntimeControlUnavailable
	}
	var nonce [16]byte
	if _, err := io.ReadFull(entropy, nonce[:]); err != nil {
		return "", ErrRuntimeControlUnavailable
	}
	return "d" + strconv.FormatInt(millis+25000, 10) + "-" + hex.EncodeToString(nonce[:]), nil
}

// OpenRtpServerIfMatch is available only on the verified, pinned TLS control
// transport. No preflight, boot refresh, retry or legacy API is invoked here.
// This experimental adapter is not wired to product playback or ledger dispatch.
func (c *OpenAPIRuntimeControl) OpenRtpServerIfMatch(ctx context.Context, request RtpResourceOpenRequest) (RtpResourceOpenResult, error) {
	if c == nil || c.client == nil || ctx == nil || !validRtpResourceSelector(request.RtpResourceSelector) ||
		request.Port < 0 || request.Port > 65534 || request.TCPMode < 0 || request.TCPMode > 1 ||
		request.OnlyTrack < 0 || request.OnlyTrack > 2 || len(request.LocalIP) > 64 || net.ParseIP(request.LocalIP) == nil {
		return RtpResourceOpenResult{}, ErrRuntimeControlUnavailable
	}
	body, _ := json.Marshal(request)
	data, err := c.client.runtimeControl(ctx, "openRtpServerIfMatch", body)
	var result RtpResourceOpenResult
	if err != nil || json.Unmarshal(data["result"], &result.Result) != nil {
		return RtpResourceOpenResult{}, ErrRuntimeControlUnavailable
	}
	switch result.Result {
	case RtpResourceCreated, RtpResourceExisting:
		if len(data) != 2 || json.Unmarshal(data["port"], &result.Port) != nil || result.Port <= 0 || result.Port > 65534 {
			return RtpResourceOpenResult{}, ErrRuntimeControlUnavailable
		}
	case RtpResourceClosePending, RtpResourceRetired, RtpResourceConflict, RtpResourceExpired, RtpResourceCapacity, RtpResourceRuntimeMismatch:
		if len(data) != 1 {
			return RtpResourceOpenResult{}, ErrRuntimeControlUnavailable
		}
	default:
		return RtpResourceOpenResult{}, ErrRuntimeControlUnavailable
	}
	return result, nil
}

// CloseRtpServerIfMatch never interprets a successful response as terminal:
// scheduled, pending and fenced all lack an actual socket termination receipt.
// Even an expired resource ID must be sent unchanged; a missing API is an error,
// never a reason to probe or fall back to stream-ID-only close.
func (c *OpenAPIRuntimeControl) CloseRtpServerIfMatch(ctx context.Context, target RtpResourceSelector) (RtpResourceResult, error) {
	if c == nil || c.client == nil || ctx == nil || !validRtpResourceSelector(target) {
		return "", ErrRuntimeControlUnavailable
	}
	body, _ := json.Marshal(target)
	data, err := c.client.runtimeControl(ctx, "closeRtpServerIfMatch", body)
	var result RtpResourceResult
	if err != nil || len(data) != 1 || json.Unmarshal(data["result"], &result) != nil {
		return "", ErrRuntimeControlUnavailable
	}
	switch result {
	case RtpResourceShutdownScheduled, RtpResourceClosePending, RtpResourceNotActiveFenced,
		RtpResourceMismatch, RtpResourceRuntimeMismatch, RtpResourceCapacity:
		return result, nil
	default:
		return "", ErrRuntimeControlUnavailable
	}
}

func validRtpResourceSelector(target RtpResourceSelector) bool {
	id := target.ResourceID
	return validRuntimeNonce(target.BootNonce) && len(id) == 47 && id[0] == 'd' && id[14] == '-' &&
		strings.Trim(id[1:14], "0123456789") == "" && validRuntimeNonce(id[15:]) &&
		target.VHost == "__defaultVhost__" && validRtpResourcePart(target.App, 64) && validRtpResourcePart(target.Stream, 256)
}

func validRtpResourcePart(value string, limit int) bool {
	return value != "" && len(value) <= limit &&
		strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-") == ""
}
