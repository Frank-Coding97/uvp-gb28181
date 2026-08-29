package controllers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

// supportedAlgorithms 跟 scheduler.Factory.Build 取值对齐
var supportedAlgorithms = []string{"roundrobin", "weighted", "leastload"}

var ErrInvalidSchedulerLogFilter = errors.New("invalid scheduler log filter")

var ErrSchedulerStateSplit = errors.New("scheduler state split or uncertain")

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
		zc.FailAndAbort(c, "不支持的算法名", nil)
		return
	}
	if zc.mgr == nil {
		zc.FailAndAbort(c, "Scheduler 未装配,无法切换", nil)
		return
	}
	old := zc.mgr.CurrentName()
	if err := zc.mgr.Switch(req.Algorithm); err != nil {
		zc.FailAndAbort(c, "切换算法失败", err)
		return
	}
	if zc.setting != nil {
		if err := zc.setting.UpdateAlgorithm(c.Request.Context(), req.Algorithm); err != nil {
			// The DB setting and in-memory Manager form one user-visible switch.
			// Compensate the in-memory side before returning; if that compensation
			// also fails, report the split explicitly instead of claiming success.
			if old == "" {
				zc.FailAndAbort(c, "DB 写入失败且无法回切,调度状态不确定", fmt.Errorf("%w: old algorithm is empty: %v", ErrSchedulerStateSplit, err))
				return
			}
			if compensateErr := zc.mgr.Switch(old); compensateErr != nil {
				zc.FailAndAbort(c, "DB 写入失败且回切失败,调度状态不确定", fmt.Errorf("%w: db=%v compensate=%v", ErrSchedulerStateSplit, err, compensateErr))
				return
			}
			zc.FailAndAbort(c, "DB 写入失败,内存已回切", err)
			return
		}
	}
	zc.Success(c, gin.H{"algorithm": req.Algorithm, "effectiveFrom": "next_invite"})
}

// ListSchedulerLogs GET /api/gb28181/zlm/scheduler/logs?limit=100
//
// limit 默认 100,上限 1000(防大表全扫)。
// logSvc 未装配返空数组(降级)。
func (zc *ZLMSchedulerController) ListSchedulerLogs(c *gin.Context) {
	filter, err := parseSchedulerLogFilter(c)
	if err != nil {
		zc.FailAndAbort(c, "调度日志筛选条件非法", err)
		return
	}
	if zc.logSvc == nil {
		zc.Success(c, gin.H{"list": []scheduler.SchedulerLog{}, "limit": filter.Limit})
		return
	}
	rows, err := zc.logSvc.ListFiltered(c.Request.Context(), filter)
	if err != nil {
		zc.FailAndAbort(c, "查询调度日志失败", err)
		return
	}
	zc.Success(c, gin.H{"list": rows, "limit": filter.Limit})
}

func parseSchedulerLogFilter(c *gin.Context) (scheduler.SchedulerLogFilter, error) {
	values := c.Request.URL.Query()
	for key := range values {
		switch key {
		case "from", "to", "nodeId", "algorithm", "policy", "result", "streamId", "limit":
		default:
			return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: unknown filter %q", ErrInvalidSchedulerLogFilter, key)
		}
	}
	filter := scheduler.SchedulerLogFilter{Limit: 100}
	if rawValues, present := values["limit"]; present {
		if len(rawValues) != 1 || strings.TrimSpace(rawValues[0]) == "" {
			return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: limit must be 1..1000", ErrInvalidSchedulerLogFilter)
		}
		raw := strings.TrimSpace(rawValues[0])
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 || limit > 1000 {
			return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: limit must be 1..1000", ErrInvalidSchedulerLogFilter)
		}
		filter.Limit = limit
	}
	parseTime := func(key string) (*time.Time, error) {
		rawValues, present := values[key]
		if !present {
			return nil, nil
		}
		if len(rawValues) != 1 || strings.TrimSpace(rawValues[0]) == "" {
			return nil, fmt.Errorf("%w: %s must be RFC3339", ErrInvalidSchedulerLogFilter, key)
		}
		raw := strings.TrimSpace(rawValues[0])
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %s must be RFC3339", ErrInvalidSchedulerLogFilter, key)
		}
		return &parsed, nil
	}
	var err error
	if filter.From, err = parseTime("from"); err != nil {
		return scheduler.SchedulerLogFilter{}, err
	}
	if filter.To, err = parseTime("to"); err != nil {
		return scheduler.SchedulerLogFilter{}, err
	}
	if filter.From != nil && filter.To != nil && filter.From.After(*filter.To) {
		return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: from is after to", ErrInvalidSchedulerLogFilter)
	}
	if rawValues, present := values["nodeId"]; present {
		if len(rawValues) != 1 || strings.TrimSpace(rawValues[0]) == "" {
			return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: nodeId must be positive", ErrInvalidSchedulerLogFilter)
		}
		raw := strings.TrimSpace(rawValues[0])
		nodeID, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || nodeID <= 0 {
			return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: nodeId must be positive", ErrInvalidSchedulerLogFilter)
		}
		filter.NodeID = &nodeID
	}
	algorithm, algorithmPresent := singleNonEmptyQuery(values, "algorithm")
	if algorithmPresent == false && hasQueryKey(values, "algorithm") {
		return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: algorithm must not be empty", ErrInvalidSchedulerLogFilter)
	}
	policy, policyPresent := singleNonEmptyQuery(values, "policy")
	if policyPresent == false && hasQueryKey(values, "policy") {
		return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: policy must not be empty", ErrInvalidSchedulerLogFilter)
	}
	if algorithm != "" && policy != "" && algorithm != policy {
		return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: algorithm and policy differ", ErrInvalidSchedulerLogFilter)
	}
	if algorithm == "" {
		algorithm = policy
	}
	if algorithm != "" {
		if !isSupportedAlgorithm(algorithm) {
			return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: unknown algorithm", ErrInvalidSchedulerLogFilter)
		}
		filter.Algorithm = algorithm
	}
	if rawValues, present := values["result"]; present {
		if len(rawValues) != 1 || strings.TrimSpace(rawValues[0]) == "" {
			return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: unknown result", ErrInvalidSchedulerLogFilter)
		}
		raw := strings.TrimSpace(rawValues[0])
		switch raw {
		case string(scheduler.SchedulerLogResultSuccess):
			filter.Result = scheduler.SchedulerLogResultSuccess
		case string(scheduler.SchedulerLogResultError), "failure":
			filter.Result = scheduler.SchedulerLogResultError
		default:
			return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: unknown result", ErrInvalidSchedulerLogFilter)
		}
	}
	if rawValues, present := values["streamId"]; present {
		if len(rawValues) != 1 || strings.TrimSpace(rawValues[0]) == "" {
			return scheduler.SchedulerLogFilter{}, fmt.Errorf("%w: streamId must not be empty", ErrInvalidSchedulerLogFilter)
		}
		filter.StreamID = rawValues[0]
	}
	return scheduler.NormalizeSchedulerLogFilter(filter), nil
}

func hasQueryKey(values map[string][]string, key string) bool {
	_, ok := values[key]
	return ok
}

func singleNonEmptyQuery(values map[string][]string, key string) (string, bool) {
	rawValues, ok := values[key]
	if !ok || len(rawValues) != 1 || strings.TrimSpace(rawValues[0]) == "" {
		return "", false
	}
	return strings.TrimSpace(rawValues[0]), true
}

func isSupportedAlgorithm(name string) bool {
	for _, a := range supportedAlgorithms {
		if a == name {
			return true
		}
	}
	return false
}
