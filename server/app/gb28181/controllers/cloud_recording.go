package controllers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type CloudRecordingManager interface {
	Enable(context.Context, uint) (*gbmodels.GbChannel, error)
	Disable(context.Context, uint) (*gbmodels.GbChannel, error)
}

type CloudRecordingController struct {
	controllers.Common
	manager CloudRecordingManager
	dbFunc  func() *gorm.DB
}

func NewCloudRecordingController(manager CloudRecordingManager) *CloudRecordingController {
	return &CloudRecordingController{manager: manager, dbFunc: func() *gorm.DB { return app.DB() }}
}

func (c *CloudRecordingController) SetManager(manager CloudRecordingManager) {
	c.manager = manager
}

func (c *CloudRecordingController) SetDB(dbFunc func() *gorm.DB) {
	c.dbFunc = dbFunc
}

func (c *CloudRecordingController) Update(ctx *gin.Context) {
	if c.manager == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "云端录像服务未装配"})
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.FailAndAbort(ctx, "ID 不合法", err)
		return
	}
	var body struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil || body.Enabled == nil {
		c.FailAndAbort(ctx, "enabled 必须是布尔值", err)
		return
	}

	var channel gbmodels.GbChannel
	result := c.dbFunc().WithContext(ctx).
		Scopes(ownerDeptScope(ctx)).
		Select("id").
		First(&channel, uint(id))
	if errors.Is(result.Error, gorm.ErrRecordNotFound) || result.RowsAffected == 0 {
		c.FailAndAbort(ctx, "通道不存在", nil)
		return
	}
	if result.Error != nil {
		c.FailAndAbort(ctx, "查询通道失败", result.Error)
		return
	}

	var updated *gbmodels.GbChannel
	if *body.Enabled {
		updated, err = c.manager.Enable(ctx.Request.Context(), channel.ID)
	} else {
		updated, err = c.manager.Disable(ctx.Request.Context(), channel.ID)
	}
	if err != nil {
		c.FailAndAbort(ctx, "更新云端录像失败", err)
		return
	}
	c.Success(ctx, updated)
}
