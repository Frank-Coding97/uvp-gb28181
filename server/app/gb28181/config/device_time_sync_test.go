package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 设备校时默认关闭：打开它会通过 SIP 应答的 Date 头实际修改现场设备的系统时间，
// 因此缺省、以及任何读不出来的配置都必须落到 false。
func TestDeviceTimeSyncDefaultsOff(t *testing.T) {
	require.False(t, DeviceTimeSyncEnabledFrom(nil), "没有配置源时必须关闭")

	source := &playAuthMutableSource{values: map[string]interface{}{}}
	require.False(t, DeviceTimeSyncEnabledFrom(source), "缺省必须是关闭")

	source.Set(DeviceTimeSyncEnabledConfigKey, true)
	require.True(t, DeviceTimeSyncEnabledFrom(source))

	source.Set(DeviceTimeSyncEnabledConfigKey, false)
	require.False(t, DeviceTimeSyncEnabledFrom(source), "显式关闭必须读回 false")
}
