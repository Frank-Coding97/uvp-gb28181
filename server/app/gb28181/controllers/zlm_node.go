package controllers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

// ZLMNodeController ZLM 媒体节点 CRUD + 状态切换(M1)
type ZLMNodeController struct {
	controllers.Common
	svc *service.NodeService
}

func NewZLMNodeController(svc *service.NodeService) *ZLMNodeController {
	return &ZLMNodeController{svc: svc}
}

// List GET /api/gb28181/zlm/nodes
func (zc *ZLMNodeController) List(c *gin.Context) {
	list, err := zc.svc.List(c)
	if err != nil {
		zc.FailAndAbort(c, "获取节点列表失败", err)
		return
	}
	zc.Success(c, gin.H{"list": list})
}

// Get GET /api/gb28181/zlm/nodes/:id
func (zc *ZLMNodeController) Get(c *gin.Context) {
	id, err := zc.parseID(c)
	if err != nil {
		zc.FailAndAbort(c, "节点 ID 非法", err)
		return
	}
	n, err := zc.svc.Get(c, id)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			zc.FailAndAbort(c, "节点不存在", err)
			return
		}
		zc.FailAndAbort(c, "查询节点失败", err)
		return
	}
	zc.Success(c, n)
}

// Create POST /api/gb28181/zlm/nodes
func (zc *ZLMNodeController) Create(c *gin.Context) {
	var req service.CreateNodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		zc.FailAndAbort(c, "请求参数非法", err)
		return
	}
	n, err := zc.svc.Create(c, req)
	if err != nil {
		zc.FailAndAbort(c, "创建节点失败", err)
		return
	}
	zc.Success(c, n)
}

// Update PUT /api/gb28181/zlm/nodes/:id
func (zc *ZLMNodeController) Update(c *gin.Context) {
	id, err := zc.parseID(c)
	if err != nil {
		zc.FailAndAbort(c, "节点 ID 非法", err)
		return
	}
	var req service.UpdateNodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		zc.FailAndAbort(c, "请求参数非法", err)
		return
	}
	n, err := zc.svc.Update(c, id, req)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			zc.FailAndAbort(c, "节点不存在", err)
			return
		}
		zc.FailAndAbort(c, "更新节点失败", err)
		return
	}
	zc.Success(c, n)
}

// Delete DELETE /api/gb28181/zlm/nodes/:id
func (zc *ZLMNodeController) Delete(c *gin.Context) {
	id, err := zc.parseID(c)
	if err != nil {
		zc.FailAndAbort(c, "节点 ID 非法", err)
		return
	}
	if zc.svc.HasNodeImpactProvider() {
		fingerprint, confirmed := zc.impactConfirmation(c, id, service.NodeImpactActionDelete)
		if !confirmed {
			return
		}
		if err := zc.svc.DeleteConfirmed(c, id, fingerprint); err != nil {
			zc.handleNodeActionError(c, "删除节点失败", err)
			return
		}
		zc.Success(c, gin.H{"ok": true})
		return
	}
	if err := zc.svc.Delete(c, id); err != nil {
		if errors.Is(err, service.ErrNodeNotInMaintenance) {
			zc.FailAndAbort(c, "请先把节点切到维护态再删除", err)
			return
		}
		if errors.Is(err, service.ErrNodeNotFound) {
			zc.FailAndAbort(c, "节点不存在", err)
			return
		}
		zc.FailAndAbort(c, "删除节点失败", err)
		return
	}
	zc.Success(c, gin.H{"ok": true})
}

// SetMaintenance POST /api/gb28181/zlm/nodes/:id/maintenance
func (zc *ZLMNodeController) SetMaintenance(c *gin.Context) {
	id, err := zc.parseID(c)
	if err != nil {
		zc.FailAndAbort(c, "节点 ID 非法", err)
		return
	}
	if zc.svc.HasNodeImpactProvider() {
		fingerprint, confirmed := zc.impactConfirmation(c, id, service.NodeImpactActionMaintenance)
		if !confirmed {
			return
		}
		if err := zc.svc.SetMaintenanceConfirmed(c, id, fingerprint); err != nil {
			zc.handleNodeActionError(c, "切维护态失败", err)
			return
		}
		zc.Success(c, gin.H{"ok": true})
		return
	}
	if err := zc.svc.SetMaintenance(c, id); err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			zc.FailAndAbort(c, "节点不存在", err)
			return
		}
		zc.FailAndAbort(c, "切维护态失败", err)
		return
	}
	zc.Success(c, gin.H{"ok": true})
}

// Activate POST /api/gb28181/zlm/nodes/:id/activate
func (zc *ZLMNodeController) Activate(c *gin.Context) {
	id, err := zc.parseID(c)
	if err != nil {
		zc.FailAndAbort(c, "节点 ID 非法", err)
		return
	}
	if err := zc.svc.Activate(c, id); err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			zc.FailAndAbort(c, "节点不存在", err)
			return
		}
		zc.FailAndAbort(c, "激活失败", err)
		return
	}
	zc.Success(c, gin.H{"ok": true})
}

// KickSessions POST /api/gb28181/zlm/nodes/:id/kick
//
// 驱逐节点全部会话(高危),返回被踢的会话数。
// 节点不存在 → 404;ZLM 不可达 → 500。前端必须二次确认。
func (zc *ZLMNodeController) KickSessions(c *gin.Context) {
	id, err := zc.parseID(c)
	if err != nil {
		zc.FailAndAbort(c, "节点 ID 非法", err)
		return
	}
	if zc.svc.HasNodeImpactProvider() {
		fingerprint, confirmed := zc.impactConfirmation(c, id, service.NodeImpactActionKick)
		if !confirmed {
			return
		}
		count, err := zc.svc.KickAllSessionsConfirmed(c, id, fingerprint)
		if err != nil {
			zc.handleNodeActionError(c, "驱逐会话失败", err)
			return
		}
		zc.Success(c, gin.H{"count": count})
		return
	}
	count, err := zc.svc.KickAllSessions(c, id)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			zc.FailAndAbort(c, "节点不存在", err)
			return
		}
		zc.FailAndAbort(c, "驱逐会话失败", err)
		return
	}
	zc.Success(c, gin.H{"count": count})
}

// Impact returns the bounded, secret-free preflight snapshot for one
// high-risk node action. T14 may expose this method from a dedicated route;
// the legacy action routes also return the same snapshot when no fingerprint
// header/query value is supplied.
func (zc *ZLMNodeController) Impact(c *gin.Context) {
	id, err := zc.parseID(c)
	if err != nil {
		zc.FailAndAbort(c, "节点 ID 非法", err)
		return
	}
	action, err := service.ParseNodeImpactAction(c.Query("action"))
	if err != nil {
		zc.FailAndAbort(c, "影响预检动作非法", err)
		return
	}
	preflight, err := zc.svc.PreflightNodeImpact(c, id, action)
	if err != nil {
		zc.handleNodeActionError(c, "节点影响预检失败", err)
		return
	}
	zc.Success(c, preflight)
}

func (zc *ZLMNodeController) impactConfirmation(c *gin.Context, id int64, action service.NodeImpactAction) (string, bool) {
	fingerprint := strings.TrimSpace(c.GetHeader("X-Impact-Fingerprint"))
	if fingerprint == "" {
		fingerprint = strings.TrimSpace(c.Query("fingerprint"))
	}
	if fingerprint != "" {
		return fingerprint, true
	}
	preflight, err := zc.svc.PreflightNodeImpact(c, id, action)
	if err != nil {
		zc.handleNodeActionError(c, "节点影响预检失败", err)
		return "", false
	}
	// Returning the preflight instead of executing is deliberate: clients must
	// explicitly confirm the exact observed fingerprint in a header/query.
	zc.Success(c, preflight)
	return "", false
}

func (zc *ZLMNodeController) handleNodeActionError(c *gin.Context, message string, err error) {
	switch {
	case errors.Is(err, service.ErrNodeImpactChanged):
		zc.FailAndAbort(c, "节点影响已变化，请重新预检", err, http.StatusConflict)
	case errors.Is(err, service.ErrNodeImpactConflict):
		zc.FailAndAbort(c, "节点仍有活动影响，未执行操作", err, http.StatusConflict)
	case errors.Is(err, service.ErrNodeNotFound):
		zc.FailAndAbort(c, "节点不存在", err, http.StatusNotFound)
	case errors.Is(err, service.ErrNodeNotInMaintenance):
		zc.FailAndAbort(c, "请先把节点切到维护态再删除", err, http.StatusConflict)
	default:
		zc.FailAndAbort(c, message, err)
	}
}

// restartReq Restart 端点 body
type restartReq struct {
	GraceMS int `json:"graceMS"`
}

// Restart POST /api/gb28181/zlm/nodes/:id/restart
//
// 重启 ZLM 服务(高危,所有流中断)。body {graceMS} 当前仅接口预留,M3 阶段忽略。
// 节点不存在 → 404;ZLM 不可达 / 拒绝 → 500。前端必须二次确认。
func (zc *ZLMNodeController) Restart(c *gin.Context) {
	id, err := zc.parseID(c)
	if err != nil {
		zc.FailAndAbort(c, "节点 ID 非法", err)
		return
	}
	// body 可选:空 body 也允许(默认 graceMS=0)
	var req restartReq
	_ = c.ShouldBindJSON(&req)

	result, err := zc.svc.RestartAccepted(c, id, req.GraceMS)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			zc.FailAndAbort(c, "节点不存在", err)
			return
		}
		zc.FailAndAbort(c, "重启 ZLM 失败", err)
		return
	}
	// Accepted means only that ZLM acknowledged the restart command. The
	// operation continues through offline, heartbeat, convergence and readback.
	cz := gin.H{
		"accepted":    result.Accepted,
		"operationId": result.OperationID,
		"status":      result.Status,
	}
	zc.Success(c, cz)
}

func (zc *ZLMNodeController) parseID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}
