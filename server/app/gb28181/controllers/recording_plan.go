package controllers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordingplan"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
)

type RecordingPlanController struct {
	controllers.Common
	dbFunc func() *gorm.DB
}

func NewRecordingPlanController(dbFunc ...func() *gorm.DB) *RecordingPlanController {
	provider := func() *gorm.DB { return app.DB() }
	if len(dbFunc) > 0 && dbFunc[0] != nil {
		provider = dbFunc[0]
	}
	return &RecordingPlanController{dbFunc: provider}
}

func (c *RecordingPlanController) Page(ctx *gin.Context) {
	deptID, _, ok := c.identity(ctx)
	if !ok {
		return
	}
	page, pageSize := pagination(ctx)
	var enabled *bool
	switch strings.ToLower(strings.TrimSpace(ctx.Query("status"))) {
	case "enabled":
		value := true
		enabled = &value
	case "disabled":
		value := false
		enabled = &value
	}
	rows, total, err := recordingplan.NewService(c.dbFunc()).PageSummaries(ctx, deptID, ctx.Query("keyword"), enabled, page, pageSize)
	if err != nil {
		c.respondError(ctx, err)
		return
	}
	if pageSize > 100 {
		pageSize = 100
	}
	c.success(ctx, gin.H{"list": rows, "total": total, "page": page, "pageSize": pageSize})
}

func (c *RecordingPlanController) Create(ctx *gin.Context) {
	deptID, actorID, ok := c.identity(ctx)
	if !ok {
		return
	}
	var input recordingplan.PlanInput
	if err := ctx.ShouldBindJSON(&input); err != nil {
		c.failure(ctx, http.StatusBadRequest, "请求参数不合法")
		return
	}
	result, err := recordingplan.NewService(c.dbFunc()).Create(ctx, deptID, actorID, input)
	if err != nil {
		c.respondError(ctx, err)
		return
	}
	c.success(ctx, result)
}

func (c *RecordingPlanController) Detail(ctx *gin.Context) {
	deptID, _, ok := c.identity(ctx)
	if !ok {
		return
	}
	planID, ok := c.planID(ctx)
	if !ok {
		return
	}
	result, err := recordingplan.NewService(c.dbFunc()).Get(ctx, deptID, planID)
	if err != nil {
		c.respondError(ctx, err)
		return
	}
	c.success(ctx, result)
}

func (c *RecordingPlanController) Update(ctx *gin.Context) {
	deptID, actorID, ok := c.identity(ctx)
	if !ok {
		return
	}
	planID, ok := c.planID(ctx)
	if !ok {
		return
	}
	var input recordingplan.PlanInput
	if err := ctx.ShouldBindJSON(&input); err != nil {
		c.failure(ctx, http.StatusBadRequest, "请求参数不合法")
		return
	}
	result, err := recordingplan.NewService(c.dbFunc()).Update(ctx, deptID, actorID, planID, input)
	if err != nil {
		c.respondError(ctx, err)
		return
	}
	c.success(ctx, result)
}

func (c *RecordingPlanController) SetEnabled(ctx *gin.Context) {
	deptID, actorID, ok := c.identity(ctx)
	if !ok {
		return
	}
	planID, ok := c.planID(ctx)
	if !ok {
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil || input.Enabled == nil {
		c.failure(ctx, http.StatusBadRequest, "enabled 必须是布尔值")
		return
	}
	result, err := recordingplan.NewService(c.dbFunc()).SetEnabled(ctx, deptID, actorID, planID, *input.Enabled)
	if err != nil {
		c.respondError(ctx, err)
		return
	}
	c.success(ctx, result)
}

func (c *RecordingPlanController) Delete(ctx *gin.Context) {
	deptID, _, ok := c.identity(ctx)
	if !ok {
		return
	}
	planID, ok := c.planID(ctx)
	if !ok {
		return
	}
	if err := recordingplan.NewService(c.dbFunc()).Delete(ctx, deptID, planID); err != nil {
		c.respondError(ctx, err)
		return
	}
	c.success(ctx, nil)
}

func (c *RecordingPlanController) SearchDevices(ctx *gin.Context) {
	deptID, _, planID, ok := c.assignmentIdentity(ctx)
	if !ok {
		return
	}
	_ = planID
	page, pageSize := pagination(ctx)
	var online *bool
	switch strings.ToLower(strings.TrimSpace(ctx.Query("online"))) {
	case "online", "true", "1":
		value := true
		online = &value
	case "offline", "false", "0":
		value := false
		online = &value
	}
	result, err := recordingplan.NewAssignmentService(c.dbFunc()).SearchDevicesFiltered(ctx, deptID, ctx.Query("keyword"), online, page, pageSize)
	if err != nil {
		c.respondError(ctx, err)
		return
	}
	c.success(ctx, result)
}

func (c *RecordingPlanController) SearchChannels(ctx *gin.Context) {
	deptID, _, _, ok := c.assignmentIdentity(ctx)
	if !ok {
		return
	}
	var online *bool
	switch strings.ToLower(strings.TrimSpace(ctx.Query("online"))) {
	case "online", "true", "1":
		value := true
		online = &value
	case "offline", "false", "0":
		value := false
		online = &value
	}
	page, pageSize := pagination(ctx)
	result, err := recordingplan.NewAssignmentService(c.dbFunc()).SearchChannels(ctx, deptID, ctx.Query("keyword"), online, page, pageSize)
	if err != nil {
		c.respondError(ctx, err)
		return
	}
	c.success(ctx, result)
}

func (c *RecordingPlanController) Assign(ctx *gin.Context) {
	deptID, actorID, planID, ok := c.assignmentIdentity(ctx)
	if !ok {
		return
	}
	var selection recordingplan.AssignmentSelection
	if err := ctx.ShouldBindJSON(&selection); err != nil {
		c.failure(ctx, http.StatusBadRequest, "分配参数不合法")
		return
	}
	result, err := recordingplan.NewAssignmentService(c.dbFunc()).Assign(ctx, deptID, actorID, planID, selection)
	if err != nil {
		c.respondError(ctx, err)
		return
	}
	c.success(ctx, result)
}

func (c *RecordingPlanController) SetChannelMode(ctx *gin.Context) {
	deptID, _, ok := c.identity(ctx)
	if !ok {
		return
	}
	channelID64, err := strconv.ParseUint(ctx.Param("channelId"), 10, 64)
	if err != nil || channelID64 == 0 {
		c.failure(ctx, http.StatusBadRequest, "通道 ID 不合法")
		return
	}
	var input struct {
		Mode string `json:"mode"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil {
		c.failure(ctx, http.StatusBadRequest, "录像模式不合法")
		return
	}
	if err := recordingplan.NewAssignmentService(c.dbFunc()).SetMode(ctx, deptID, uint(channelID64), input.Mode); err != nil {
		c.respondError(ctx, err)
		return
	}
	c.success(ctx, nil)
}

func (c *RecordingPlanController) PlanChannels(ctx *gin.Context) {
	deptID, _, ok := c.identity(ctx)
	if !ok {
		return
	}
	planID, ok := c.planID(ctx)
	if !ok {
		return
	}
	page, pageSize := pagination(ctx)
	var online *bool
	switch strings.ToLower(strings.TrimSpace(ctx.Query("online"))) {
	case "online", "true", "1":
		value := true
		online = &value
	case "offline", "false", "0":
		value := false
		online = &value
	}
	result, err := recordingplan.NewDiagnosticService(c.dbFunc()).PagePlanChannels(ctx, deptID, planID, recordingplan.ChannelStatusQuery{
		Keyword: ctx.Query("keyword"), DeviceID: ctx.Query("deviceId"), ActualState: ctx.Query("actualState"),
		ReasonCode: ctx.Query("reasonCode"), Online: online, Page: page, PageSize: pageSize,
	})
	if err != nil {
		c.respondError(ctx, err)
		return
	}
	c.success(ctx, result)
}

func (c *RecordingPlanController) ChannelTimeline(ctx *gin.Context) {
	deptID, _, ok := c.identity(ctx)
	if !ok {
		return
	}
	channelID, ok := c.channelID(ctx)
	if !ok {
		return
	}
	page, pageSize := pagination(ctx)
	result, err := recordingplan.NewDiagnosticService(c.dbFunc()).Timeline(ctx, deptID, channelID, page, pageSize)
	if err != nil {
		c.respondError(ctx, err)
		return
	}
	c.success(ctx, result)
}

func (c *RecordingPlanController) DiagnoseChannel(ctx *gin.Context) {
	deptID, _, ok := c.identity(ctx)
	if !ok {
		return
	}
	channelID, ok := c.channelID(ctx)
	if !ok {
		return
	}
	result, err := recordingplan.NewDiagnosticService(c.dbFunc()).DiagnoseChannel(ctx, deptID, channelID)
	if err != nil {
		c.respondError(ctx, err)
		return
	}
	c.success(ctx, result)
}

func (c *RecordingPlanController) assignmentIdentity(ctx *gin.Context) (uint, uint, uint64, bool) {
	deptID, actorID, ok := c.identity(ctx)
	if !ok {
		return 0, 0, 0, false
	}
	planID, ok := c.planID(ctx)
	if !ok {
		return 0, 0, 0, false
	}
	if _, err := recordingplan.NewService(c.dbFunc()).Get(ctx, deptID, planID); err != nil {
		c.respondError(ctx, err)
		return 0, 0, 0, false
	}
	return deptID, actorID, planID, true
}

func (c *RecordingPlanController) identity(ctx *gin.Context) (uint, uint, bool) {
	actorID := c.GetCurrentUserID(ctx)
	if actorID == 0 {
		c.failure(ctx, http.StatusUnauthorized, "未登录或登录已失效")
		return 0, 0, false
	}
	var user appmodels.User
	result := c.dbFunc().WithContext(ctx).Select("id", "dept_id", "status").Where("id = ?", actorID).Limit(1).Find(&user)
	if result.Error != nil {
		c.failure(ctx, http.StatusInternalServerError, "读取用户数据范围失败")
		return 0, 0, false
	}
	if result.RowsAffected == 0 || user.Status != 1 {
		c.failure(ctx, http.StatusForbidden, "当前用户无权访问录像计划")
		return 0, 0, false
	}
	return user.DeptID, actorID, true
}

func (c *RecordingPlanController) planID(ctx *gin.Context) (uint64, bool) {
	planID, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || planID == 0 {
		c.failure(ctx, http.StatusBadRequest, "录像计划 ID 不合法")
		return 0, false
	}
	return planID, true
}

func (c *RecordingPlanController) channelID(ctx *gin.Context) (uint, bool) {
	channelID, err := strconv.ParseUint(ctx.Param("channelId"), 10, 64)
	if err != nil || channelID == 0 {
		c.failure(ctx, http.StatusBadRequest, "通道 ID 不合法")
		return 0, false
	}
	return uint(channelID), true
}

func (c *RecordingPlanController) respondError(ctx *gin.Context, err error) {
	if errors.Is(err, recordingplan.ErrChannelAlreadyBound) {
		c.failure(ctx, http.StatusConflict, err.Error())
		return
	}
	var domainErr *recordingplan.DomainError
	if errors.As(err, &domainErr) {
		status := http.StatusBadRequest
		switch domainErr.Code {
		case recordingplan.ErrPlanNotFound.Code:
			status = http.StatusNotFound
		case recordingplan.ErrPlanHasBindings.Code:
			status = http.StatusConflict
		}
		ctx.JSON(status, gin.H{"code": status, "message": domainErr.Message, "data": domainErr.Details})
		return
	}
	c.failure(ctx, http.StatusInternalServerError, "录像计划操作失败")
}

func (c *RecordingPlanController) success(ctx *gin.Context, data any) {
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": data})
}

func (c *RecordingPlanController) failure(ctx *gin.Context, status int, message string) {
	ctx.JSON(status, gin.H{"code": status, "message": message, "data": nil})
}

func pagination(ctx *gin.Context) (int, int) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("pageSize", "10"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	return page, pageSize
}
