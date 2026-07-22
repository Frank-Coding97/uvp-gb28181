package controllers

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/streammonitor"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type StreamMonitorService interface {
	Get(context.Context, string) (*streammonitor.Snapshot, error)
}

type StreamMonitorController struct {
	controllers.Common
	service StreamMonitorService
	dbFunc  func() *gorm.DB
}

func NewStreamMonitorController(service StreamMonitorService) *StreamMonitorController {
	return &StreamMonitorController{service: service, dbFunc: func() *gorm.DB { return app.DB() }}
}

func (c *StreamMonitorController) SetDB(dbFunc func() *gorm.DB) { c.dbFunc = dbFunc }

func (c *StreamMonitorController) Get(ctx *gin.Context) {
	if c.service == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "流概况服务未装配"})
		return
	}
	streamID := ctx.Param("streamId")
	if streamID == "" {
		c.FailAndAbort(ctx, "streamId 不能为空", nil)
		return
	}
	var channel gbmodels.GbChannel
	result := c.dbFunc().WithContext(ctx).
		Scopes(ownerDeptScope(ctx)).
		Select("id").
		Where("stream_id = ?", streamID).
		Limit(1).
		Find(&channel)
	if result.Error != nil {
		c.FailAndAbort(ctx, "查询流失败", result.Error)
		return
	}
	if result.RowsAffected == 0 {
		c.FailAndAbort(ctx, "流不存在", nil)
		return
	}

	snapshot, err := c.service.Get(ctx.Request.Context(), streamID)
	if errors.Is(err, streammonitor.ErrStreamOffline) {
		ctx.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "流已离线"})
		return
	}
	if errors.Is(err, streammonitor.ErrNodeUnavailable) {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "媒体节点不可达"})
		return
	}
	if err != nil {
		c.FailAndAbort(ctx, "获取流概况失败", err)
		return
	}
	c.Success(ctx, snapshot)
}
