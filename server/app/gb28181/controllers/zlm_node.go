package controllers

import (
	"errors"
	"strconv"

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

func (zc *ZLMNodeController) parseID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}
