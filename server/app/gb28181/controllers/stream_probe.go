package controllers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"uvplatform.cn/uvp-gb28181/app/utils/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/streamprobe"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type StreamProbeTaskService interface {
	Create(context.Context, string, int) (*streamprobe.Task, error)
	Get(context.Context, string) (*streamprobe.Task, error)
}

type streamProbeRequest struct {
	DurationMS *int `json:"durationMs"`
}

type StreamProbeController struct {
	controllers.Common
	service StreamProbeTaskService
	dbFunc  func() *gorm.DB
}

func NewStreamProbeController(service StreamProbeTaskService) *StreamProbeController {
	return &StreamProbeController{service: service, dbFunc: func() *gorm.DB { return app.DB() }}
}

func (c *StreamProbeController) SetDB(dbFunc func() *gorm.DB) { c.dbFunc = dbFunc }

func (c *StreamProbeController) Run(ctx *gin.Context) {
	if c.service == nil {
		response.SetBusinessResult(ctx, 503, false)
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "视频探针服务未装配"})
		return
	}
	streamID := ctx.Param("streamId")
	durationMS := streamprobe.DefaultDurationMS
	var request streamProbeRequest
	if err := ctx.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		c.Fail(ctx, "视频探针参数不合法", err, http.StatusBadRequest)
		return
	}
	if request.DurationMS != nil {
		durationMS = *request.DurationMS
	}
	if !streamprobe.IsSupportedDuration(durationMS) {
		c.Fail(ctx, "durationMs 仅支持 3000、10000 或 60000", nil, http.StatusBadRequest)
		return
	}
	var channel gbmodels.GbChannel
	result := c.dbFunc().WithContext(ctx.Request.Context()).Scopes(ownerDeptScope(ctx)).Select("id").Where("stream_id = ?", streamID).Limit(1).Find(&channel)
	if result.Error != nil {
		c.FailAndAbort(ctx, "查询流失败", result.Error)
		return
	}
	if result.RowsAffected == 0 {
		c.FailAndAbort(ctx, "流不存在", nil)
		return
	}
	task, err := c.service.Create(ctx.Request.Context(), streamID, durationMS)
	if err != nil {
		response.SetBusinessResult(ctx, 503, false)
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "视频探针队列不可用"})
		return
	}
	response.SetBusinessResult(ctx, 0, true)
	ctx.JSON(http.StatusAccepted, gin.H{"code": 0, "message": "", "data": task})
}

func (c *StreamProbeController) Get(ctx *gin.Context) {
	if c.service == nil {
		response.SetBusinessResult(ctx, 503, false)
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "视频探针队列未装配"})
		return
	}
	task, err := c.service.Get(ctx.Request.Context(), ctx.Param("operationId"))
	if errors.Is(err, streamprobe.ErrTaskNotFound) {
		response.SetBusinessResult(ctx, 404, false)
		ctx.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "探针任务不存在"})
		return
	}
	if err != nil {
		response.SetBusinessResult(ctx, 503, false)
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "视频探针队列不可用"})
		return
	}
	var channel gbmodels.GbChannel
	result := c.dbFunc().WithContext(ctx.Request.Context()).Scopes(ownerDeptScope(ctx)).Select("id").Where("stream_id = ?", task.StreamID).Limit(1).Find(&channel)
	if result.Error != nil {
		c.FailAndAbort(ctx, "查询流失败", result.Error)
		return
	}
	if result.RowsAffected == 0 {
		response.SetBusinessResult(ctx, 404, false)
		ctx.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "探针任务不存在"})
		return
	}
	response.SetBusinessResult(ctx, 0, true)
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "message": "", "data": task})
}
