package logcleanup

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// 模板与代码默认值必须同源。
//
// ⛔⛔ 曾经的形态是「模板只写天数、不写 configured」：安装后页面显示 7 天，
// 清理任务却每晚直接跳过，操作日志与定时任务日志一条不删，磁盘被撑到
// 升级预检失败（2026-11-21 线上机）。天数一样、开关缺席，属于最难发现的
// 组合，所以这条契约必须由测试锁住，而不是靠注释提醒。
func TestConfigExampleEnablesLogCleanupByDefault(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	path := filepath.Join(filepath.Dir(file), "..", "..", "config", "config.example.yml")

	v := viper.New()
	v.SetConfigFile(path)
	require.NoError(t, v.ReadInConfig())

	require.True(t, v.GetBool(ConfiguredConfigKey),
		"config.example.yml 的 log_cleanup 段必须带 configured: true，否则新装实例的日志永远不清理")
	require.Equal(t, defaultOperationRetentionDays, v.GetInt(OperationRetentionDaysConfigKey))
	require.Equal(t, defaultLoginRetentionDays, v.GetInt(LoginRetentionDaysConfigKey))
}
