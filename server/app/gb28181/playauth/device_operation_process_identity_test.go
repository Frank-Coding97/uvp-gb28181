package playauth

import (
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
)

func TestDeviceOperationProcessIdentityMatchesRootGeneration(t *testing.T) {
	rootID, err := processauthority.ProcessID()
	require.NoError(t, err)
	ownerID, err := sipCleanupProcessID()
	require.NoError(t, err)
	require.Equal(t, rootID, ownerID, "original RTP and SIP INFO/cleanup must identify the same actual process as root registration")
}
