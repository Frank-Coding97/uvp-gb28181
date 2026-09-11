package scheduler

import (
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/internal/standalone/runtimeexecutor"
	"uvplatform.cn/uvp-gb28181/plugins/example/scheduler/executors"
)

// RegisterExampleExecutors 注册示例插件的所有执行器
// 在插件初始化时调用此函数
func RegisterExampleExecutors() {
	// The standalone runtime creates its scheduler after first installation.
	// Queue this registration so plugin package initialization never
	// dereferences the not-yet-created global scheduler.
	runtimeexecutor.Register(func(scheduler app.JobSchedulerInterf) {
		scheduler.RegisterExecutor(&executors.ExampleExecutor{})
	})
}
