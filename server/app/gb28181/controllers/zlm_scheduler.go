package controllers

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

// supportedAlgorithms 跟 scheduler.Factory.Build 取值对齐
var supportedAlgorithms = []string{"roundrobin", "weighted", "leastload"}

// SchedulerSettingWriter Controller 需要的最小写接口
//
// 实际实现是 repo.SchedulerSettingRepo(已有 UpdateAlgorithm 方法)。
// 抽接口便于测试时注入 fake。
type SchedulerSettingWriter interface {
	UpdateAlgorithm(ctx context.Context, algorithm string) error
}

// ZLMSchedulerController 调度算法切换 + 调度日志(M3 T3.3)
//
// 路由:
//
//	GET  /api/gb28181/zlm/scheduler          当前算法 + 可用列表
//	PUT  /api/gb28181/zlm/scheduler          切换算法(写 DB + Manager.Switch)
//	GET  /api/gb28181/zlm/scheduler/logs     最近 N 条调度日志
//
// logSvc / setting 可为 nil:bootstrap 装配失败时 GET /logs 走降级返空数组,
// PUT /scheduler 仅切内存不写 DB(避免直接 500)。
type ZLMSchedulerController struct {
	controllers.Common
	mgr     *scheduler.Manager
	logSvc  *scheduler.LogService
	setting SchedulerSettingWriter
}

// NewZLMSchedulerController 构造
//
// mgr 必填(没 Manager 则切换无意义);logSvc 和 setting 允许 nil(降级)。
func NewZLMSchedulerController(
	mgr *scheduler.Manager,
	logSvc *scheduler.LogService,
	setting SchedulerSettingWriter,
) *ZLMSchedulerController {
	return &ZLMSchedulerController{mgr: mgr, logSvc: logSvc, setting: setting}
}

// GetScheduler GET /api/gb28181/zlm/scheduler
//
// 返回 {algorithm, available[]}。
// Manager 未装配时 algorithm 返空串(前端兜底高亮 roundrobin)。
func (zc *ZLMSchedulerController) GetScheduler(c *gin.Context) {
	current := ""
	if zc.mgr != nil {
		current = zc.mgr.CurrentName()
	}
	zc.Success(c, gin.H{
		"algorithm": current,
		"available": supportedAlgorithms,
	})
}

// switchSchedulerReq PUT body
type switchSchedulerReq struct {
	Algorithm string `json:"algorithm" binding:"required"`
}

// SwitchScheduler PUT /api/gb28181/zlm/scheduler
//
// body {algorithm: "weighted"} → Manager.Switch + 写 scheduler_setting。
// 算法不在 supportedAlgorithms 中 → 400-style FailAndAbort。
// Manager 未装配 → 503-style FailAndAbort。
func (zc *ZLMSchedulerController) SwitchScheduler(c *gin.Context) {
	var req switchSchedulerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		zc.FailAndAbort(c, "请求参数非法", err)
		return
	}
	if !isSupportedAlgorithm(req.Algorithm) {
		zc.FailAndAbort(c, "不支持的算法名:"+req.Algorithm, nil)
		return
	}
	if zc.mgr == nil {
		zc.FailAndAbort(c, "Scheduler 未装配,无法切换", nil)
		return
	}
	if err := zc.mgr.Switch(req.Algorithm); err != nil {
		zc.FailAndAbort(c, "切换算法失败", err)
		return
	}
	// DB 写失败不回滚内存切换(运维角度:重启后 fallback roundrobin 也比内存切换失败强)
	if zc.setting != nil {
		if err := zc.setting.UpdateAlgorithm(c.Request.Context(), req.Algorithm); err != nil {
			zc.FailAndAbort(c, "DB 写入失败(内存已切换,重启后会回落)", err)
			return
		}
	}
	zc.Success(c, gin.H{"algorithm": req.Algorithm})
}

// ListSchedulerLogs GET /api/gb28181/zlm/scheduler/logs?limit=100
//
// limit 默认 100,上限 1000(防大表全扫)。
// logSvc 未装配返空数组(降级)。
func (zc *ZLMSchedulerController) ListSchedulerLogs(c *gin.Context) {
	limit := 100
	if q := c.Query("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}
	if zc.logSvc == nil {
		zc.Success(c, gin.H{"list": []scheduler.SchedulerLog{}, "limit": limit})
		return
	}
	rows, err := zc.logSvc.List(c.Request.Context(), limit)
	if err != nil {
		zc.FailAndAbort(c, "查询调度日志失败", err)
		return
	}
	zc.Success(c, gin.H{"list": rows, "limit": limit})
}

func isSupportedAlgorithm(name string) bool {
	for _, a := range supportedAlgorithms {
		if a == name {
			return true
		}
	}
	return false
}
