//go:build windows

package launcher

import (
	"os"
	"testing"
)

// TestWindowsStandaloneDeviceCoreActiveStop reuses the isolated core-device
// setup but stops the launcher while a verified HTTP-FLV response is active.
// It deliberately does not claim recording or recorder-index coverage.
func TestWindowsStandaloneDeviceCoreActiveStop(t *testing.T) {
	if os.Getenv(coreActiveStopEnv) != "1" {
		t.Skip("set UVP_CORE_ACTIVE_STOP=1 to run the active playback stop harness")
	}
	runWindowsStandaloneDeviceCorePath(t, true)
}
