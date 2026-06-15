package plugins

import (
	_ "uvplatform.cn/uvp-gb28181/plugins/example/routes"
	"uvplatform.cn/uvp-gb28181/plugins/example/scheduler"
)

// 插件初始化时自动执行
func init() {
	// 注册示例执行器
	scheduler.RegisterExampleExecutors()
}
