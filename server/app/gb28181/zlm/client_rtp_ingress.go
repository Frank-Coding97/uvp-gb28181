package zlm

import (
	"context"
	"encoding/json"
)

// RtpIngressResult is deliberately distinct from RtpResourceResult. Even
// RtpIngressDrained proves only the original UDP RTP/RTCP ingress has drained:
// it is not source, viewer, sender, recording, SIP or device completion.
type RtpIngressResult string

const (
	RtpIngressDrained           RtpIngressResult = "rtp_ingress_drained"
	RtpIngressShutdownScheduled RtpIngressResult = "shutdown_scheduled"
	RtpIngressClosePending      RtpIngressResult = "close_pending"
	RtpIngressNotActiveFenced   RtpIngressResult = "not_active_fenced"
	RtpIngressMismatch          RtpIngressResult = "resource_mismatch"
	RtpIngressRuntimeMismatch   RtpIngressResult = "runtime_mismatch"
	RtpIngressCapacity          RtpIngressResult = "capacity_exhausted"
)

// CloseRtpIngressIfMatchV2 uses one fixed identity on the verified/pinned TLS
// transport. It never probes, refreshes a boot, retries or falls back to v1.
// TCP, partial starts and missing evidence remain pending at the media node.
// No product dispatch, recovery aggregator or DeviceCleanupStore is wired here.
func (c *OpenAPIRuntimeControl) CloseRtpIngressIfMatchV2(ctx context.Context, target RtpResourceSelector) (RtpIngressResult, error) {
	if c == nil || c.client == nil || ctx == nil || !validRtpResourceSelector(target) {
		return "", ErrRuntimeControlUnavailable
	}
	body, _ := json.Marshal(target)
	data, err := c.client.runtimeControl(ctx, "closeRtpIngressIfMatchV2", body)
	var result RtpIngressResult
	if err != nil || len(data) != 1 || json.Unmarshal(data["result"], &result) != nil {
		return "", ErrRuntimeControlUnavailable
	}
	switch result {
	case RtpIngressDrained, RtpIngressShutdownScheduled, RtpIngressClosePending, RtpIngressNotActiveFenced,
		RtpIngressMismatch, RtpIngressRuntimeMismatch, RtpIngressCapacity:
		return result, nil
	default:
		return "", ErrRuntimeControlUnavailable
	}
}
