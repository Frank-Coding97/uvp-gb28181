package device

import (
	"context"
	"time"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// OfflineScanner 离线扫描器:周期扫描,把 Redis 在线态已消失但 MySQL 仍在线的设备置为离线
// 作为 Redis TTL 的兜底(TTL 过期 key 自动消失,但 MySQL status 字段需要主动纠正)
type OfflineScanner struct {
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
}

// NewOfflineScanner 创建离线扫描器
func NewOfflineScanner(intervalSeconds int) *OfflineScanner {
	if intervalSeconds <= 0 {
		intervalSeconds = 30
	}
	return &OfflineScanner{
		interval: time.Duration(intervalSeconds) * time.Second,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
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

// scanOnce 执行一次扫描:遍历 MySQL 在线设备,Redis 无在线态的置离线
func (s *OfflineScanner) scanOnce() {
	ctx := context.Background()
	onlineInDB, err := gbmodels.ListOnline(ctx)
	if err != nil {
		app.ZapLog.Error("GB28181 离线扫描:查询在线设备失败", zap.Error(err))
		return
	}
	for _, d := range onlineInDB {
		if !IsOnline(ctx, d.DeviceID) {
			if err := gbmodels.UpdateStatus(ctx, d.DeviceID, gbmodels.DeviceStatusOffline); err != nil {
				app.ZapLog.Error("GB28181 离线扫描:置离线失败", zap.String("deviceId", d.DeviceID), zap.Error(err))
				continue
			}
			app.ZapLog.Info("GB28181 设备超时离线", zap.String("deviceId", d.DeviceID))
		}
	}
}

// ScanOnceForTest 导出单次扫描供测试调用
func (s *OfflineScanner) ScanOnceForTest() {
	s.scanOnce()
}
