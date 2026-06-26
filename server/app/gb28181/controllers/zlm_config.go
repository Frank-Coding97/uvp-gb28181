package controllers

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

// ZLMConfigController ZLM 节点配置查询 + 下发(M1)
type ZLMConfigController struct {
	controllers.Common
	svc *service.ConfigService
}

func NewZLMConfigController(svc *service.ConfigService) *ZLMConfigController {
	return &ZLMConfigController{svc: svc}
}

// Get GET /api/gb28181/zlm/nodes/:id/config
func (zc *ZLMConfigController) Get(c *gin.Context) {
	id, err := zc.parseID(c)
	if err != nil {
		zc.FailAndAbort(c, "节点 ID 非法", err)
		return
	}
	groups, err := zc.svc.GetGrouped(c, id)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			zc.FailAndAbort(c, "节点不存在", err)
			return
		}
		zc.FailAndAbort(c, "获取节点配置失败", err)
		return
	}
	zc.Success(c, gin.H{"groups": groups})
}

// Update PUT /api/gb28181/zlm/nodes/:id/config
func (zc *ZLMConfigController) Update(c *gin.Context) {
	id, err := zc.parseID(c)
	if err != nil {
		zc.FailAndAbort(c, "节点 ID 非法", err)
		return
	}
	var req service.UpdateConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		zc.FailAndAbort(c, "请求参数非法", err)
		return
	}
	resp, err := zc.svc.Update(c, id, req)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			zc.FailAndAbort(c, "节点不存在", err)
			return
		}
		zc.FailAndAbort(c, "下发配置失败", err)
		return
	}
	zc.Success(c, resp)
}

// TestConnection POST /api/gb28181/zlm/nodes/:id/config/test-connection
func (zc *ZLMConfigController) TestConnection(c *gin.Context) {
	id, err := zc.parseID(c)
	if err != nil {
		zc.FailAndAbort(c, "节点 ID 非法", err)
		return
	}
	res, err := zc.svc.TestConnection(c, id)
	if err != nil {
		if errors.Is(err, service.ErrNodeNotFound) {
			zc.FailAndAbort(c, "节点不存在", err)
			return
		}
		zc.FailAndAbort(c, "探测失败", err)
		return
	}
	zc.Success(c, res)
}

func (zc *ZLMConfigController) parseID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}
