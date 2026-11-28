package ptz

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestOperationResponsesPreserveDeviceFactsAndLifecycle(t *testing.T) {
	now := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	op := gbmodels.GbPTZOperation{OperationID: "op", DeviceCode: "34020000002000000001", ChannelCode: "34020000001320000001", TargetCode: "34020000001320000001", ProfileVersion: "2022", Status: gbmodels.PTZOperationRejected, ResponseRequired: true, SIPStatus: 200, DeviceResult: "ERROR", DeviceError: "unsupported", ErrorCode: "DEVICE_REJECTED", ErrorMessage: "device rejected", CreatedAt: now, SentAt: &now, CompletedAt: &now, DeadlineAt: &now}
	for _, view := range []any{operationResponse(op), controlOperationResponse(op)} {
		data, err := json.Marshal(view)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(data, &fields))
		require.Equal(t, op.DeviceCode, fields["deviceId"])
		require.Equal(t, "2022", fields["protocolVersion"])
		require.Equal(t, "unsupported", fields["deviceError"])
		require.Equal(t, true, fields["deviceAcknowledged"])
		require.Equal(t, "2026-11-09T08:00:00Z", fields["requestedAt"])
		require.Equal(t, "2026-11-09T08:00:00Z", fields["completedAt"])
	}
	op.ResponseRequired = false
	op.Status = gbmodels.PTZOperationSent
	data, err := json.Marshal(operationResponse(op))
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	require.Equal(t, false, fields["deviceAcknowledged"], "SIP success alone never proves device execution")
}
