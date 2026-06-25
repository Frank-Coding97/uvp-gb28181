package controllers

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

// AggregatorProvider 解耦 controller 跟 gb28181 包,避免循环依赖。
// gb28181 包注入 MetricsAggregator() 即可。
type AggregatorProvider func() *metrics.Aggregator

// DashboardController SIP 信令看板 REST 接口
// 暴露:
//   - GET /api/gb28181/sip/dashboard/snapshot  快照(REST)
//   - GET /api/gb28181/sip/dashboard/stream    实时(SSE,T2.1 实现)
type DashboardController struct {
	controllers.Common
	provider AggregatorProvider
}

// NewDashboardController 创建看板控制器
func NewDashboardController(p AggregatorProvider) *DashboardController {
	return &DashboardController{provider: p}
}

// parsePulseParams 解析窗口 + 精度
// window=60m | 6h | 24h(默认 60m)
// precision=1s | 10s | 1m(默认 1m,前端 60 点为佳)
func parsePulseParams(c *gin.Context) (time.Duration, time.Duration) {
	win := parseDurationDefault(c.Query("window"), 60*time.Minute)
	prec := parseDurationDefault(c.Query("precision"), time.Minute)
	if win <= 0 {
		win = 60 * time.Minute
	}
	if prec <= 0 {
		prec = time.Minute
	}
	return win, prec
}

// parseDurationDefault 接受 "60m" / "6h" / "24h" / "10s" 等 Go duration 字符串
func parseDurationDefault(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	// 兼容纯数字秒
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second
	}
	return def
}

// emptySnapshot 当聚合器不可用(gb28181 未启用)时的默认响应
// 仍按规范返回 8 个空 transactions,前端不需要特殊处理 nil
func emptySnapshot() *metrics.DashboardSnapshot {
	stats := make([]metrics.TransactionStat, 0, len(metrics.AllTxKinds))
	for _, k := range metrics.AllTxKinds {
		stats = append(stats, metrics.TransactionStat{
			Kind: k, KindStr: k.String(), LabelZh: k.LabelZh(), LabelEn: k.String(),
		})
	}
	return &metrics.DashboardSnapshot{
		Health:       metrics.HealthEmpty,
		Transactions: stats,
		Pulse:        metrics.PulseData{Samples: []metrics.PulseSample{}, AbnormalWindows: []metrics.AbnormalWindow{}},
		AsOf:         time.Now().Unix(),
	}
}

// Snapshot GET /api/gb28181/sip/dashboard/snapshot
// 返回卡片完整快照(plan §4.1 JSON 契约)
func (dc *DashboardController) Snapshot(c *gin.Context) {
	win, prec := parsePulseParams(c)
	if dc.provider == nil || dc.provider() == nil {
		dc.Success(c, emptySnapshot())
		return
	}
	snap := dc.provider().Snapshot(win, prec)
	dc.Success(c, snap)
}
