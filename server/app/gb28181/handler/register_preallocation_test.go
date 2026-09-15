package handler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
)

func TestRegisterFailureResponseMapsPreallocationRejection(t *testing.T) {
	status, reason := registerFailureResponse(device.ErrDeviceNotPreallocated)
	require.Equal(t, 403, status)
	require.Equal(t, "Device not preallocated", reason)
}
