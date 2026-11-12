package schedulerhelper

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogLevel 日志级别
type LogLevel int

const (
	LevelDebug LogLevel = iota // 调试日志级别
	LevelInfo                  // 信息日志级别
	LevelWarn                  // 警告日志级别
	LevelError                 // 错误日志级别
	LevelFatal                 // 致命错误日志级别
)

var levelStrings = []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}

// JobLogger 日志记录器接口
type JobLogger interface {
	Debug(jobID, format string, args ...interface{})
	Info(jobID, format string, args ...interface{})
	Warn(jobID, format string, args ...interface{})
	Error(jobID, format string, args ...interface{})
	Fatal(jobID, format string, args ...interface{})
	LogJobExecution(result *JobResult)
	LogJobLifecycle(job *Job, action string)
	Close() error
}

// FileJobLogger 文件日志记录器
type FileJobLogger struct {
	logger    *log.Logger // 日志记录器
	level     LogLevel    // 日志级别
	logDir    string      // 日志目录
	logFile   *os.File    // 当前日志文件
	dailyFile string      // 当前日志文件名
	lastCheck time.Time   // 新增：上次检查时间
	mu        sync.Mutex
	done      chan struct{} // 用于关闭 goroutine 的通道
}

// NewFileJobLogger 创建新的文件日志记录器
func NewFileJobLogger(logDir string, level LogLevel) (*FileJobLogger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %v", err)
	}

	f := &FileJobLogger{
		level:     level,
		logDir:    logDir,
		done:      make(chan struct{}),
		lastCheck: time.Now(), // 添加这行
	}

	// 初始化日志文件
	if err := f.rotateLogFile(); err != nil {
		return nil, fmt.Errorf("初始化日志文件失败: %v", err)
	}

	// 启动每日日志轮转检查
	go f.startDailyRotateCheck()

	// 记录启动日志
	f.Info("system", "日志系统初始化完成, 日志目录: %s, 日志级别: %s", logDir, levelStrings[level])

	return f, nil
}

// rotateLogFile 轮转日志文件
func (f *FileJobLogger) rotateLogFile() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rotateLogFileUnsafe()

}
func (f *FileJobLogger) rotateLogFileUnsafe() error {
	today := time.Now().Format("2006-01-02")
	newFile := filepath.Join(f.logDir, fmt.Sprintf("job-scheduler-%s.log", today))

	if newFile == f.dailyFile && f.logFile != nil {
		return nil
	}

	// 关闭旧文件
	if f.logFile != nil {
		if err := f.logFile.Close(); err != nil {
			return fmt.Errorf("关闭旧日志文件失败: %w", err)
		}
		f.logFile = nil
	}

	// 创建新文件
	file, err := os.OpenFile(newFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("创建日志文件失败: %v", err)
	}

	// 设置多输出：文件和控制台
	writer := io.MultiWriter(file, os.Stdout)
	f.logger = log.New(writer, "", log.LstdFlags|log.Lmicroseconds)
	f.logFile = file
	f.dailyFile = newFile

	return nil
}

// startDailyRotateCheck 启动每日轮转检查
func (f *FileJobLogger) startDailyRotateCheck() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := f.rotateLogFile(); err != nil {
				f.logger.Printf("日志轮转失败: %v", err)
			}
		case <-f.done:
			return
		}
	}
}

// log 基础日志方法
func (f *FileJobLogger) log(level LogLevel, jobID, format string, args ...interface{}) {
	if level < f.level {
		return
	}
	msg := fmt.Sprintf(format, args...)
	logMsg := fmt.Sprintf("[%s] [JOB:%s] %s", levelStrings[level], jobID, msg)
	f.mu.Lock()
	defer f.mu.Unlock()
	if time.Since(f.lastCheck) > time.Minute {
		if err := f.rotateLogFileUnsafe(); err != nil {
			f.logger.Printf("日志轮转失败: %v", err)
		} else {
			f.lastCheck = time.Now()
		}
	}

	f.logger.Println(logMsg)
}

// Debug 调试日志
func (f *FileJobLogger) Debug(jobID, format string, args ...interface{}) {
	f.log(LevelDebug, jobID, format, args...)
}

// Info 信息日志
func (f *FileJobLogger) Info(jobID, format string, args ...interface{}) {
	f.log(LevelInfo, jobID, format, args...)
}

// Warn 警告日志
func (f *FileJobLogger) Warn(jobID, format string, args ...interface{}) {
	f.log(LevelWarn, jobID, format, args...)
}

// Error 错误日志
func (f *FileJobLogger) Error(jobID, format string, args ...interface{}) {
	f.log(LevelError, jobID, format, args...)
}

// Fatal 致命错误日志
func (f *FileJobLogger) Fatal(jobID, format string, args ...interface{}) {
	f.log(LevelFatal, jobID, format, args...)
	log.Panicf("Fatal error: "+format, args...)
}

// LogJobExecution 记录任务执行结果
func (f *FileJobLogger) LogJobExecution(result *JobResult) {
	var statusIcon, levelStr string
	switch result.Status {
	case "SUCCESS":
		statusIcon = "✅"
		levelStr = "INFO"
	case "FAILED":
		statusIcon = "❌"
		levelStr = "ERROR"
	case "PANIC":
		statusIcon = "💥"
		levelStr = "ERROR"
	default:
		statusIcon = "⚠️"
		levelStr = "WARN"
	}

	logMsg := fmt.Sprintf("%s 执行%s | 耗时: %v | 重试: %d次",
		statusIcon, result.Status, result.Duration, result.RetryCount)

	if result.Error != nil {
		logMsg += fmt.Sprintf(" | 错误: %v", result.Error)
	}

	// 根据日志级别记录
	switch levelStr {
	case "ERROR":
		f.Error(result.JobID, "%s", logMsg)
	case "WARN":
		f.Warn(result.JobID, "%s", logMsg)
	default:
		f.Info(result.JobID, "%s", logMsg)
	}
}

// LogJobLifecycle 记录任务生命周期事件
func (f *FileJobLogger) LogJobLifecycle(job *Job, action string) {
	var emoji string
	switch action {
	case "创建":
		emoji = "✨"
	case "更新":
		emoji = "📝"
	case "启用":
		emoji = "▶️"
	case "禁用":
		emoji = "⏸️"
	case "删除":
		emoji = "🗑️"
	case "手动触发":
		emoji = "🎯"
	case "执行中":
		emoji = "⚡"
	case "跳过":
		emoji = "⏭️"
	default:
		emoji = "📋"
	}

	msg := fmt.Sprintf("%s 任务[%s] %s | 分组: %s | 状态: %s | 执行策略: %s | 阻塞策略: %s | CRON: %s",
		emoji, job.Name, action, job.Group,
		getJobStatusName(job.Status),
		getExecutionPolicyName(job.ExecutionPolicy),
		getBlockingPolicyName(job.BlockingPolicy),
		job.CronExpression)

	f.Info(job.ID, "%s", msg)
}

// Close 关闭日志记录器，停止 goroutine 并关闭日志文件
func (f *FileJobLogger) Close() error {
	// 关闭 goroutine
	close(f.done)

	// 关闭日志文件
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.logFile != nil {
		if err := f.logFile.Close(); err != nil {
			return fmt.Errorf("关闭日志文件失败: %v", err)
		}
		f.logFile = nil
	}

	return nil
}
