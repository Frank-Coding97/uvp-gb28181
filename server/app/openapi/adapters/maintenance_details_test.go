package adapters

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestMaintenanceViewIncludesLifecycleAndFailureFacts(t *testing.T) {
	now := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	op := gbmodels.GbPTZOperation{DeviceCode: "34020000002000000001", ChannelCode: "34020000001320000001", ErrorCode: "TIMEOUT", SIPStatus: 200, CreatedAt: now, SentAt: &now}
	view := maintenanceOperationView(op)
	require.Equal(t, op.DeviceCode, view["deviceId"])
	require.Equal(t, "TIMEOUT", view["errorCode"])
	require.Equal(t, now, view["requestedAt"])
	require.Equal(t, false, view["deviceAcknowledged"])
	require.Contains(t, view, "completedAt")
}
