package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type WorkRecordingAPI interface {
	Start(context.Context, uint, workrecording.StartRequest, int) (workrecording.Snapshot, error)
	Stop(context.Context, string) (workrecording.Snapshot, error)
	Get(context.Context, string) (workrecording.Snapshot, error)
}
type WorkRecordingController struct {
	controllers.Common
	service WorkRecordingAPI
	db      func() *gorm.DB
}

func NewWorkRecordingController(service WorkRecordingAPI) *WorkRecordingController {
	return &WorkRecordingController{service: service, db: func() *gorm.DB { return app.DB() }}
}
func (c *WorkRecordingController) SetDB(provider func() *gorm.DB) { c.db = provider }
func (c *WorkRecordingController) ready(ctx *gin.Context) bool {
	if c.GetCurrentUserID(ctx) == 0 {
		workFailure(ctx, http.StatusUnauthorized, "请先登录")
		return false
	}
	if c.service == nil || c.db() == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "作业录像服务未就绪")
		return false
	}
	return true
}
func (c *WorkRecordingController) visibleChannel(ctx *gin.Context, id uint) bool {
	var channel models.GbChannel
	err := c.db().WithContext(ctx).Scopes(visibleScope(ctx)).Select("id").First(&channel, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		workFailure(ctx, http.StatusNotFound, "通道不存在")
		return false
	}
	if err != nil {
		workFailure(ctx, http.StatusServiceUnavailable, "通道权限查询失败")
		return false
	}
	return true
}
func (c *WorkRecordingController) Start(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var request workrecording.StartRequest
	decoder := json.NewDecoder(http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 2048))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		workFailure(ctx, http.StatusBadRequest, "开始参数不合法")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		workFailure(ctx, http.StatusBadRequest, "开始参数不合法")
		return
	}
	if err := request.Validate(); err != nil {
		workFailure(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !c.visibleChannel(ctx, request.ChannelID) {
		return
	}
	snapshot, err := c.service.Start(ctx.Request.Context(), c.GetCurrentUserID(ctx), request, 0)
	c.result(ctx, snapshot, err)
}
func (c *WorkRecordingController) job(ctx *gin.Context) (*models.GbWorkRecording, bool) {
	var job models.GbWorkRecording
	if err := c.db().WithContext(ctx).First(&job, "id = ?", ctx.Param("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			workFailure(ctx, http.StatusNotFound, "作业不存在")
		} else {
			workFailure(ctx, http.StatusServiceUnavailable, "作业查询失败")
		}
		return nil, false
	}
	if !c.visibleChannel(ctx, job.ChannelID) {
		return nil, false
	}
	return &job, true
}
func (c *WorkRecordingController) Stop(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	job, ok := c.job(ctx)
	if !ok {
		return
	}
	// Until a cross-user policy is agreed, a work job is stopped by its initiator.
	if job.CreatedBy != c.GetCurrentUserID(ctx) {
		workFailure(ctx, http.StatusForbidden, "仅作业发起人可以停止录像")
		return
	}
	snapshot, err := c.service.Stop(ctx.Request.Context(), job.ID)
	c.result(ctx, snapshot, err)
}
func (c *WorkRecordingController) Detail(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	job, ok := c.job(ctx)
	if !ok {
		return
	}
	snapshot, err := c.service.Get(ctx.Request.Context(), job.ID)
	c.result(ctx, snapshot, err)
}
func (c *WorkRecordingController) Status(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	values := strings.Split(ctx.Query("channelIds"), ",")
	if len(values) == 0 || len(values) > 64 {
		workFailure(ctx, http.StatusBadRequest, "通道数量不合法")
		return
	}
	ids := make([]uint, 0, len(values))
	seen := map[uint]bool{}
	for _, value := range values {
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil || parsed == 0 {
			workFailure(ctx, http.StatusBadRequest, "通道ID不合法")
			return
		}
		id := uint(parsed)
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	var channels []models.GbChannel
	if err := c.db().WithContext(ctx).Scopes(visibleScope(ctx)).Select("id").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		workFailure(ctx, http.StatusServiceUnavailable, "通道权限查询失败")
		return
	}
	if len(channels) != len(ids) {
		workFailure(ctx, http.StatusNotFound, "通道不存在")
		return
	}
	snapshots := make([]workrecording.Snapshot, 0, len(ids))
	for _, id := range ids {
		var claim models.GbRecorderClaim
		err := c.db().WithContext(ctx).First(&claim, "resource_key = ?", workrecording.ChannelResource(id)).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			workFailure(ctx, http.StatusServiceUnavailable, "录像状态查询失败")
			return
		}
		snapshot := workrecording.Snapshot{ChannelID: id, State: workrecording.StateIdle}
		if err == nil && claim.State != workrecording.StateIdle {
			snapshot.State = workrecording.StateUnknown
			snapshot.Version = claim.Version
			if claim.OwnerKind == workrecording.OwnerWork {
				snapshot, err = c.service.Get(ctx.Request.Context(), claim.OwnerID)
				if err != nil || snapshot.ChannelID != id || snapshot.ID != claim.OwnerID {
					workFailure(ctx, http.StatusServiceUnavailable, "作业状态待核实")
					return
				}
			}
		}
		snapshots = append(snapshots, snapshot)
	}
	c.Success(ctx, snapshots)
}
func (c *WorkRecordingController) result(ctx *gin.Context, snapshot workrecording.Snapshot, err error) {
	if err == nil {
		c.Success(ctx, snapshot)
		return
	}
	status := http.StatusServiceUnavailable
	if errors.Is(err, workrecording.ErrInvalidRequest) {
		status = http.StatusBadRequest
	}
	if errors.Is(err, workrecording.ErrOwnerConflict) || errors.Is(err, workrecording.ErrVersionConflict) || errors.Is(err, workrecording.ErrRequestConflict) || errors.Is(err, workrecording.ErrAttributionUnknown) {
		status = http.StatusConflict
	}
	ctx.JSON(status, gin.H{"code": status, "message": "作业录像操作未完成，请查询当前状态", "data": snapshot})
}
func workFailure(ctx *gin.Context, status int, message string) {
	ctx.JSON(status, gin.H{"code": status, "message": message})
}
