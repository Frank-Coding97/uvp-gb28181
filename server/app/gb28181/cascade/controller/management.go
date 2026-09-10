package controller

import (
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	"net/http"
	"strconv"
	"strings"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/service"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/middleware"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

// ManagementController is the HTTP boundary for upstream cascade management.
// The service is intentionally injected so an unconfigured runtime is visible as 503.
type ManagementController struct {
	service *service.ManagementService
	db      *gorm.DB
}

func NewManagementController(svc *service.ManagementService, db *gorm.DB) *ManagementController {
	return &ManagementController{service: svc, db: db}
}

func (c *ManagementController) List(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	items, err := c.service.List(ctx)
	if err != nil {
		c.fail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"list": items})
}

func (c *ManagementController) Get(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	item, err := c.service.Get(ctx, id)
	if err != nil {
		c.fail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, item)
}

func (c *ManagementController) Create(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var req platformRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		c.fail(ctx, service.ErrInvalidPlatformConfig)
		return
	}
	middleware.MarkSensitiveOperation(ctx, map[string]any{"resource": "cascade_platform", "operation": "create"})
	item, err := c.service.Create(ctx, req.input())
	if err != nil {
		if errors.Is(err, service.ErrRuntimeSyncFailed) && item != nil {
			// 配置已持久化,仅运行时未同步:返回已提交资源,附降级提示
			ctx.JSON(http.StatusOK, gin.H{"platform": item, "runtimeSynced": false, "warning": err.Error()})
			return
		}
		c.fail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, item)
}

func (c *ManagementController) Update(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	var req platformRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		c.fail(ctx, service.ErrInvalidPlatformConfig)
		return
	}
	middleware.MarkSensitiveOperation(ctx, map[string]any{"resource": "cascade_platform", "platformId": id, "operation": "update"})
	item, err := c.service.Update(ctx, id, req.ExpectedRevision, req.input())
	if err != nil {
		if errors.Is(err, service.ErrRuntimeSyncFailed) && item != nil {
			ctx.JSON(http.StatusOK, gin.H{"platform": item, "runtimeSynced": false, "warning": err.Error()})
			return
		}
		c.fail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, item)
}

func (c *ManagementController) Delete(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	if err := c.service.Delete(ctx, id); err != nil {
		if errors.Is(err, service.ErrRuntimeSyncFailed) {
			// 删除/禁用已提交,仅运行时未同步:返回已提交结果附降级提示
			ctx.JSON(http.StatusOK, gin.H{"ok": true, "runtimeSynced": false, "warning": err.Error()})
			return
		}
		c.fail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true})
}

func (c *ManagementController) SetEnabled(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	var req enabledRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		// POST /enable and /disable have no body; PUT /enabled uses this DTO.
		req.Enabled = strings.HasSuffix(ctx.FullPath(), "/enable")
	}
	item, err := c.service.SetEnabled(ctx, id, req.ExpectedRevision, req.Enabled)
	if err != nil {
		if errors.Is(err, service.ErrRuntimeSyncFailed) && item != nil {
			ctx.JSON(http.StatusOK, gin.H{"platform": item, "runtimeSynced": false, "warning": err.Error()})
			return
		}
		c.fail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, item)
}

func (c *ManagementController) Reconnect(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	if err := c.service.Reconnect(ctx, id); err != nil {
		c.fail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "platformId": id})
}

func (c *ManagementController) GetShares(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	snapshot, err := c.service.Projection(ctx, id)
	if err != nil {
		c.fail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, buildShareResponse(id, snapshot))
}

func (c *ManagementController) ReplaceShares(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	platform, err := c.service.Get(ctx, id)
	if err != nil {
		c.fail(ctx, err)
		return
	}
	var req shareRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		c.fail(ctx, repository.ErrInvalidProjection)
		return
	}
	devices, channels, err := req.projection(platform.PTZEnabled)
	if err != nil {
		c.fail(ctx, err)
		return
	}
	if err := c.authorizeSources(ctx, devices, channels); err != nil {
		c.fail(ctx, err)
		return
	}
	middleware.MarkSensitiveOperation(ctx, map[string]any{"resource": "cascade_projection", "platformId": id, "operation": "replace"})
	if err := c.service.ReplaceProjection(ctx, id, req.ExpectedProjectionRevision, devices, channels); err != nil {
		c.fail(ctx, err)
		return
	}
	snapshot, err := c.service.Projection(ctx, id)
	if err != nil {
		c.fail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, buildShareResponse(id, snapshot))
}

func (c *ManagementController) ready(ctx *gin.Context) bool {
	if c == nil || c.service == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "msg": "国标级联服务尚未装配"})
		return false
	}
	return true
}

func (c *ManagementController) authorizeSources(ctx *gin.Context, devices []repository.DeviceProjectionInput, channels []repository.ChannelProjectionInput) error {
	if c.db == nil {
		return nil
	}
	deviceIDs := make([]uint64, 0, len(devices)+len(channels))
	seenDevices := make(map[uint64]struct{})
	for _, item := range devices {
		if item.SourceDeviceID != 0 {
			seenDevices[item.SourceDeviceID] = struct{}{}
		}
	}
	for _, item := range channels {
		if item.SourceDeviceID != 0 {
			seenDevices[item.SourceDeviceID] = struct{}{}
		}
	}
	for id := range seenDevices {
		deviceIDs = append(deviceIDs, id)
	}
	if len(deviceIDs) > 0 {
		var rows []gbmodels.GbDevice
		query := c.db.WithContext(ctx).Model(&gbmodels.GbDevice{}).Where("id IN ?", deviceIDs).Scopes(datascope.VisibilityScopeWithDB(ctx, c.db, "owner_dept_id", "device_id"))
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(deviceIDs) {
			return repository.ErrPlatformNotFound
		}
	}
	if len(channels) > 0 {
		ids := make([]uint64, 0, len(channels))
		for _, item := range channels {
			if item.SourceChannelID != 0 {
				ids = append(ids, item.SourceChannelID)
			}
		}
		var rows []gbmodels.GbChannel
		query := c.db.WithContext(ctx).Model(&gbmodels.GbChannel{}).Where("id IN ?", ids).Scopes(datascope.VisibilityScopeWithDB(ctx, c.db, "owner_dept_id", "device_id"))
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(ids) {
			return repository.ErrPlatformNotFound
		}
	}
	return nil
}

func (c *ManagementController) fail(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	var mysqlErr *mysql.MySQLError
	duplicate := errors.Is(err, gorm.ErrDuplicatedKey) || (errors.As(err, &mysqlErr) && mysqlErr.Number == 1062)
	switch {
	case duplicate:
		status = http.StatusConflict
	case errors.Is(err, service.ErrInvalidPlatformConfig), errors.Is(err, repository.ErrInvalidProjection):
		status = http.StatusBadRequest
	case errors.Is(err, repository.ErrPlatformNotFound):
		status = http.StatusNotFound
	case errors.Is(err, repository.ErrRevisionConflict), errors.Is(err, service.ErrPlatformHasActiveSessions), errors.Is(err, service.ErrPlatformDisabled):
		status = http.StatusConflict
	case errors.Is(err, service.ErrRuntimeUnavailable), errors.Is(err, service.ErrCredentialUnavailable):
		status = http.StatusServiceUnavailable
	}
	message := "国标级联操作失败"
	if status == http.StatusBadRequest {
		message = "请求参数非法"
	}
	if status == http.StatusNotFound {
		message = "上级平台不存在"
	}
	if status == http.StatusConflict {
		message = "资源状态冲突"
	}
	if status == http.StatusServiceUnavailable {
		message = "国标级联服务暂不可用"
	}
	if errors.Is(err, service.ErrCredentialUnavailable) {
		message = "认证密码暂时无法保存，请检查平台数据目录是否可读写"
	}
	if duplicate {
		message = "平台名称或上级接入关系已存在，请检查上级地址、端口及平台身份"
	}
	if app.ZapLog != nil {
		fields := []zap.Field{zap.String("method", ctx.Request.Method), zap.String("route", ctx.FullPath()), zap.Int("status", status), zap.String("error_type", fmt.Sprintf("%T", err))}
		if mysqlErr != nil {
			// 数据库原始错误可能带字段值；只记录错误编号和 SQLSTATE。
			fields = append(fields, zap.Uint16("mysql_errno", mysqlErr.Number), zap.String("sqlstate", string(mysqlErr.SQLState[:])))
		} else {
			fields = append(fields, zap.Error(err))
		}
		app.ZapLog.Error("国标级联操作失败", fields...)
	}
	ctx.JSON(status, gin.H{"code": status, "msg": message})
}

func parseID(ctx *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "平台 ID 非法"})
		return 0, false
	}
	return id, true
}

type platformRequest struct {
	service.PlatformConfigInput
	ExpectedRevision uint64  `json:"expectedRevision"`
	Password         *string `json:"password"`
}

func (r platformRequest) input() service.PlatformConfigInput {
	input := r.PlatformConfigInput
	input.Password = r.Password
	return input
}

type enabledRequest struct {
	Enabled          bool   `json:"enabled"`
	ExpectedRevision uint64 `json:"expectedRevision"`
}

type shareRequest struct {
	Scope                      string         `json:"scope"`
	Devices                    []shareDevice  `json:"devices"`
	Channels                   []shareChannel `json:"channels"`
	ExpectedProjectionRevision uint64         `json:"expectedProjectionRevision"`
}
type shareDevice struct {
	SourceDeviceID    uint64 `json:"sourceDeviceId"`
	PublishedDeviceID string `json:"publishedDeviceId"`
	Name              string `json:"name"`
}
type shareChannel struct {
	SourceDeviceID     uint64 `json:"sourceDeviceId"`
	SourceChannelID    uint64 `json:"sourceChannelId"`
	PublishedChannelID string `json:"publishedChannelId"`
	Name               string `json:"name"`
	ParentOverride     string `json:"parentOverride"`
	PTZAllowed         *bool  `json:"ptzAllowed"`
}

func (r shareRequest) projection(platformPTZEnabled ...bool) ([]repository.DeviceProjectionInput, []repository.ChannelProjectionInput, error) {
	scope := strings.ToLower(strings.TrimSpace(r.Scope))
	if scope != "" && scope != "all" && scope != "devices" && scope != "channels" {
		return nil, nil, repository.ErrInvalidProjection
	}
	devices := make([]repository.DeviceProjectionInput, 0, len(r.Devices))
	for _, item := range r.Devices {
		if scope == "channels" {
			continue
		}
		devices = append(devices, repository.DeviceProjectionInput{SourceDeviceID: item.SourceDeviceID, PublishedDeviceID: item.PublishedDeviceID, Name: item.Name})
	}
	channels := make([]repository.ChannelProjectionInput, 0, len(r.Channels))
	for _, item := range r.Channels {
		if scope == "devices" {
			continue
		}
		ptz := len(platformPTZEnabled) > 0 && platformPTZEnabled[0]
		if item.PTZAllowed != nil {
			ptz = *item.PTZAllowed
		}
		channels = append(channels, repository.ChannelProjectionInput{SourceDeviceID: item.SourceDeviceID, SourceChannelID: item.SourceChannelID, PublishedChannelID: item.PublishedChannelID, Name: item.Name, ParentOverride: item.ParentOverride, PTZAllowed: ptz})
	}
	return devices, channels, nil
}

type shareResponse struct {
	PlatformID uint64                            `json:"platformId"`
	Revision   uint64                            `json:"revision"`
	Devices    []model.GbCascadeDeviceProjection `json:"devices"`
	Channels   []shareChannelResponse            `json:"channels"`
}

type shareChannelResponse struct {
	model.GbCascadeChannelProjection
	SourceDeviceID uint64 `json:"sourceDeviceId"`
}

func buildShareResponse(platformID uint64, snapshot *repository.ProjectionSnapshot) shareResponse {
	deviceSources := make(map[uint64]uint64, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		deviceSources[device.ID] = device.SourceDeviceID
	}
	channels := make([]shareChannelResponse, 0, len(snapshot.Channels))
	for _, channel := range snapshot.Channels {
		channels = append(channels, shareChannelResponse{
			GbCascadeChannelProjection: channel,
			SourceDeviceID:             deviceSources[channel.DeviceProjectionID],
		})
	}
	return shareResponse{PlatformID: platformID, Revision: snapshot.Revision, Devices: snapshot.Devices, Channels: channels}
}
