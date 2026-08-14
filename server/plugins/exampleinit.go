package plugins

import (
	"os"
	"strings"

	_ "uvplatform.cn/uvp-gb28181/plugins/example/routes"
	"uvplatform.cn/uvp-gb28181/plugins/example/scheduler"
)

// 插件初始化时自动执行
func init() {
	// -migrate-down 运维回滚入口不初始化 JobScheduler:
	// 此时注册执行器会 nil panic,必须先短路
	if downRequested() {
		return
	}
	// 注册示例执行器
	scheduler.RegisterExampleExecutors()
}

func downRequested() bool {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-migrate-down=") && strings.TrimPrefix(arg, "-migrate-down=") != "" {
			return true
		}
	}
	return false
}
