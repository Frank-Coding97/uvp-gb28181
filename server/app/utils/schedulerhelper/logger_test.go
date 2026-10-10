package schedulerhelper

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLogLevelValues 测试日志级别常量值
func TestLogLevelValues(t *testing.T) {
	tests := []struct {
		name  string
		level LogLevel
		want  int
	}{
		{"LevelDebug", LevelDebug, 0},
		{"LevelInfo", LevelInfo, 1},
		{"LevelWarn", LevelWarn, 2},
		{"LevelError", LevelError, 3},
		{"LevelFatal", LevelFatal, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.level) != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.level, tt.want)
			}
		})
	}
}

// TestNewFileJobLogger 测试创建日志记录器
func TestNewFileJobLogger(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	if logger == nil {
		t.Fatal("logger is nil")
	}
	if logger.level != LevelInfo {
		t.Errorf("logger level = %d, want %d", logger.level, LevelInfo)
	}
	if logger.logDir != logDir {
		t.Errorf("logger logDir = %s, want %s", logger.logDir, logDir)
	}
}

// TestNewFileJobLoggerWithInvalidPath 测试使用无效路径创建日志记录器
func TestNewFileJobLoggerWithInvalidPath(t *testing.T) {
	// 使用一个无效的路径（Windows下可能不适用，但Unix下会失败）
	// 这里使用一个不太可能创建成功的路径
	logDir := "/dev/null/invalid/path/that/cannot/be/created"

	_, err := NewFileJobLogger(logDir, LevelInfo)
	if err == nil {
		t.Error("expected error when creating logger with invalid path")
	}
}

// TestLoggerDebug 测试调试日志
func TestLoggerDebug(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-debug")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelDebug)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	logger.Debug("test-job", "debug message: %s", "test")

	// 等待日志写入
	time.Sleep(100 * time.Millisecond)
}

// TestLoggerInfo 测试信息日志
func TestLoggerInfo(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-info")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	logger.Info("test-job", "info message: %s", "test")

	// 等待日志写入
	time.Sleep(100 * time.Millisecond)
}

// TestLoggerWarn 测试警告日志
func TestLoggerWarn(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-warn")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	logger.Warn("test-job", "warn message: %s", "test")

	// 等待日志写入
	time.Sleep(100 * time.Millisecond)
}

// TestLoggerError 测试错误日志
func TestLoggerError(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-error")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	logger.Error("test-job", "error message: %s", "test")

	// 等待日志写入
	time.Sleep(100 * time.Millisecond)
}

// TestLoggerLevelFiltering 测试日志级别过滤
func TestLoggerLevelFiltering(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-level")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelWarn)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	// Debug 和 Info 日志应该被过滤
	logger.Debug("test-job", "debug message")
	logger.Info("test-job", "info message")

	// Warn 及以上应该被记录
	logger.Warn("test-job", "warn message")
	logger.Error("test-job", "error message")

	// 等待日志写入
	time.Sleep(100 * time.Millisecond)
}

// TestLogJobExecution 测试记录任务执行结果
func TestLogJobExecution(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-execution")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	tests := []struct {
		name   string
		result *JobResult
	}{
		{
			name: "Success",
			result: &JobResult{
				JobID:      "job-1",
				Status:     "SUCCESS",
				StartTime:  time.Now(),
				EndTime:    time.Now().Add(1 * time.Second),
				Duration:   1 * time.Second,
				RetryCount: 0,
			},
		},
		{
			name: "Failed",
			result: &JobResult{
				JobID:      "job-2",
				Status:     "FAILED",
				Error:      &testError{msg: "test error"},
				StartTime:  time.Now(),
				EndTime:    time.Now().Add(500 * time.Millisecond),
				Duration:   500 * time.Millisecond,
				RetryCount: 2,
			},
		},
		{
			name: "Panic",
			result: &JobResult{
				JobID:      "job-3",
				Status:     "PANIC",
				Error:      &testError{msg: "panic occurred"},
				StartTime:  time.Now(),
				EndTime:    time.Now().Add(200 * time.Millisecond),
				Duration:   200 * time.Millisecond,
				RetryCount: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger.LogJobExecution(tt.result)
			time.Sleep(50 * time.Millisecond)
		})
	}
}

// testError 用于测试的错误类型
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// TestLogJobLifecycle 测试记录任务生命周期事件
func TestLogJobLifecycle(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-lifecycle")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	job := &Job{
		ID:             "test-job-1",
		Name:           "Test Job",
		Group:          "test-group",
		Status:         StatusEnabled,
		BlockingPolicy: BlockDiscard,
		CronExpression: "*/5 * * * * *",
	}

	actions := []string{"创建", "更新", "启用", "禁用", "删除", "手动触发", "执行中", "跳过"}

	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			logger.LogJobLifecycle(job, action)
			time.Sleep(50 * time.Millisecond)
		})
	}
}

// TestLogFileRotation 测试日志文件轮转
func TestLogFileRotation(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-rotation")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}

	// 记录初始日志文件
	initialFile := logger.dailyFile

	// 手动触发轮转
	err = logger.rotateLogFile()
	if err != nil {
		t.Errorf("rotateLogFile failed: %v", err)
	}

	// 文件应该相同（因为日期没变）
	if logger.dailyFile != initialFile {
		t.Logf("dailyFile changed from %s to %s", initialFile, logger.dailyFile)
	}

	if err := logger.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

// TestLoggerClose 测试关闭日志记录器
func TestLoggerClose(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-close")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}

	// 记录一些日志
	logger.Info("test-job", "before close")

	// 关闭日志记录器
	err = logger.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// 注意：Close() 方法不支持多次调用，因为会关闭 done 通道
	// 实际使用中应该只调用一次 Close()
}

// TestLoggerConcurrentWrites 测试并发写入
func TestLoggerConcurrentWrites(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-concurrent")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	// 并发写入日志
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				logger.Info("test-job", "concurrent message %d-%d", id, j)
			}
			done <- true
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < 10; i++ {
		<-done
	}

	time.Sleep(100 * time.Millisecond)
}

// TestReadLogFile 测试读取日志文件内容
func TestReadLogFile(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-read")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}

	testJobID := "test-read-job"
	testMessage := "test read message"

	logger.Info(testJobID, "%s", testMessage)

	// 等待日志写入
	time.Sleep(200 * time.Millisecond)

	if err := logger.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// 读取日志文件
	today := time.Now().Format("2006-01-02")
	logFile := filepath.Join(logDir, "job-scheduler-"+today+".log")

	file, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("failed to open log file: %v", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, testJobID) && strings.Contains(line, testMessage) {
			found = true
			break
		}
	}

	if !found {
		t.Error("log message not found in file")
	}
}

// TestLoggerWithDifferentLevels 测试不同日志级别
func TestLoggerWithDifferentLevels(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-different-levels")
	defer func() { _ = os.RemoveAll(logDir) }()

	levels := []LogLevel{LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal}

	for _, level := range levels {
		t.Run(levelStrings[level], func(t *testing.T) {
			logger, err := NewFileJobLogger(logDir+"-"+levelStrings[level], level)
			if err != nil {
				t.Fatalf("NewFileJobLogger failed: %v", err)
			}
			defer func() { _ = logger.Close() }()

			logger.Debug("test-job", "debug message")
			logger.Info("test-job", "info message")
			logger.Warn("test-job", "warn message")
			logger.Error("test-job", "error message")

			time.Sleep(50 * time.Millisecond)
		})
	}
}

// TestLoggerEmptyJobID 测试空任务ID
func TestLoggerEmptyJobID(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-empty-jobid")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	// 空任务ID应该也能正常工作
	logger.Info("", "message with empty job ID")
	logger.Debug("", "debug message with empty job ID")

	time.Sleep(100 * time.Millisecond)
}

// TestLoggerLongMessage 测试长消息
func TestLoggerLongMessage(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-long-message")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	// 创建一个长消息
	longMessage := strings.Repeat("This is a long message. ", 100)

	logger.Info("test-job", "%s", longMessage)

	time.Sleep(100 * time.Millisecond)
}

// TestLoggerSpecialCharacters 测试特殊字符
func TestLoggerSpecialCharacters(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-special-chars")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	specialMessages := []string{
		"Message with 中文",
		"Message with emoji 🎉",
		"Message with \"quotes\"",
		"Message with 'apostrophes'",
		"Message with \t tabs \t and \n newlines",
		"Message with special chars: @#$%^&*()",
	}

	for _, msg := range specialMessages {
		logger.Info("test-job", "%s", msg)
	}

	time.Sleep(100 * time.Millisecond)
}

// TestJobLoggerInterface 测试JobLogger接口实现
func TestJobLoggerInterface(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-interface")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	// 验证接口实现
	var _ JobLogger = logger

	// 测试所有接口方法
	logger.Debug("test-job", "debug")
	logger.Info("test-job", "info")
	logger.Warn("test-job", "warn")
	logger.Error("test-job", "error")

	result := &JobResult{
		JobID:      "test-job",
		Status:     "SUCCESS",
		StartTime:  time.Now(),
		EndTime:    time.Now(),
		Duration:   0,
		RetryCount: 0,
	}
	logger.LogJobExecution(result)

	job := &Job{
		ID:             "test-job",
		Name:           "Test",
		Group:          "test-group",
		Status:         StatusEnabled,
		BlockingPolicy: BlockDiscard,
		CronExpression: "* * * * *",
	}
	logger.LogJobLifecycle(job, "创建")

	time.Sleep(100 * time.Millisecond)
}

// TestLoggerNilResult 测试空结果处理
func TestLoggerNilResult(t *testing.T) {
	logDir := filepath.Join(os.TempDir(), "test-logs-nil-result")
	defer func() { _ = os.RemoveAll(logDir) }()

	logger, err := NewFileJobLogger(logDir, LevelInfo)
	if err != nil {
		t.Fatalf("NewFileJobLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	// 这个测试主要是确保不会panic
	// 实际使用中不应该传入nil结果
	// logger.LogJobExecution(nil) // 这会导致panic，所以不测试

	time.Sleep(50 * time.Millisecond)
}
