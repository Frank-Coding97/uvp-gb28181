package schedulerhelper

import (
	"log"

	"github.com/robfig/cron/v3"
)

// Option 定义调度器配置选项函数类型
type Option func(*JobScheduler)

// WithLogger 注入调用者拥有的日志记录器，应用调度器应使用共享根 logger。
func WithLogger(logger JobLogger) Option {
	return func(s *JobScheduler) {
		s.logger = logger
	}
}

// WithLoggerConfig 是独立工具显式选择文件日志时的兼容入口。
// 默认 NewJobScheduler 不创建独立文件；此选项保留原有失败即退出语义。
func WithLoggerConfig(logDir string, level LogLevel) Option {
	return func(s *JobScheduler) {
		logger, err := NewFileJobLogger(logDir, level)
		if err != nil {
			log.Fatalf("Failed to create logger: %v", err)
		}
		s.logger = logger
	}
}

// WithJobResultsBufferSize 设置任务结果通道缓冲大小
func WithJobResultsBufferSize(size int) Option {
	return func(s *JobScheduler) {
		s.jobResults = make(chan *JobResult, size)
	}
}

// WithCronOptions 设置自定义 cron 选项
func WithCronOptions(opts ...cron.Option) Option {
	return func(s *JobScheduler) {
		s.cron = cron.New(opts...)
	}
}
