package controllers

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/streamprobe"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type StreamProbeService interface {
	Run(context.Context, string) (*streamprobe.ProbeSnapshot, error)
}

type StreamProbeController struct {
	controllers.Common
	service StreamProbeService
	dbFunc  func() *gorm.DB
}

func NewStreamProbeController(service StreamProbeService) *StreamProbeController {
	return &StreamProbeController{service: service, dbFunc: func() *gorm.DB { return app.DB() }}
}

func (c *StreamProbeController) SetDB(dbFunc func() *gorm.DB) { c.dbFunc = dbFunc }

func (c *StreamProbeController) Run(ctx *gin.Context) {
	if c.service == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "视频探针服务未装配"})
		return
	}
	streamID := ctx.Param("streamId")
	if streamID == "" {
		streamID = ctx.Param("deviceId")
	}
	var channel gbmodels.GbChannel
	result := c.dbFunc().WithContext(ctx).Scopes(ownerDeptScope(ctx)).Select("id").Where("stream_id = ?", streamID).Limit(1).Find(&channel)
	if result.Error != nil {
		c.FailAndAbort(ctx, "查询流失败", result.Error)
		return
	}
	if result.RowsAffected == 0 {
		c.FailAndAbort(ctx, "流不存在", nil)
		return
	}
	snapshot, err := c.service.Run(ctx.Request.Context(), streamID)
	switch {
	case errors.Is(err, streamprobe.ErrStreamOffline):
		ctx.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "流已离线"})
	case errors.Is(err, streamprobe.ErrNodeUnavailable):
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "媒体节点不可达"})
	case errors.Is(err, context.DeadlineExceeded):
		ctx.JSON(http.StatusGatewayTimeout, gin.H{"code": 1, "message": "视频探针超时"})
	case err != nil:
		c.FailAndAbort(ctx, "视频探针执行失败", err)
	default:
		c.Success(ctx, snapshot)
	}
}
