package device

import (
	"context"
	"time"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// OfflineScanner 离线扫描器:周期从事实(keepalive_time)重新判定,把超时的在线设备置离线
// 无状态:每轮都从 DB 事实重算,进程重启不影响正确性
type OfflineScanner struct {
	interval     time.Duration
	timeoutCount int
	grace        int
	stop         chan struct{}
	done         chan struct{}
}

// NewOfflineScanner 创建离线扫描器
func NewOfflineScanner(intervalSeconds, timeoutCount, graceSeconds int) *OfflineScanner {
	if intervalSeconds <= 0 {
		intervalSeconds = 30
	}
	if timeoutCount <= 0 {
		timeoutCount = 3
	}
	if graceSeconds < 0 {
		graceSeconds = 0
	}
	return &OfflineScanner{
		interval:     time.Duration(intervalSeconds) * time.Second,
		timeoutCount: timeoutCount,
		grace:        graceSeconds,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
}

// Start 启动周期扫描(独立 goroutine)
func (s *OfflineScanner) Start() {
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				s.scanOnce()
			}
		}
	}()
}

// Stop 停止扫描
func (s *OfflineScanner) Stop() {
	close(s.stop)
	<-s.done
}

// scanOnce 执行一次扫描:查 status=1 但心跳超时的设备,置离线
func (s *OfflineScanner) scanOnce() {
	ctx := context.Background()
	stale, err := gbmodels.ListStaleOnline(ctx, s.timeoutCount, s.grace)
	if err != nil {
		app.ZapLog.Error("GB28181 离线扫描:查询超时设备失败", zap.Error(err))
		return
	}
	for _, d := range stale {
		if err := gbmodels.MarkOffline(ctx, d.DeviceID); err != nil {
			app.ZapLog.Error("GB28181 离线扫描:置离线失败", zap.String("deviceId", d.DeviceID), zap.Error(err))
			continue
		}
		app.ZapLog.Info("GB28181 设备超时离线", zap.String("deviceId", d.DeviceID))
	}
}

// ScanOnceForTest 导出单次扫描供测试调用
func (s *OfflineScanner) ScanOnceForTest() {
	s.scanOnce()
}
