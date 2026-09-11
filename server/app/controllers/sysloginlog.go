package controllers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/middleware"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/service"
)

type loginLogQuery interface {
	List(context.Context, service.LoginLogFilter) ([]service.LoginLogListItem, int64, error)
	Detail(context.Context, uint) (*service.LoginLogDetail, error)
}

type loginLogMutation interface {
	Delete(context.Context, []uint) (int64, error)
	Clear(context.Context) (int64, error)
	Unlock(context.Context, uint) error
}

type SysLoginLogController struct {
	Common
	query    loginLogQuery
	mutation loginLogMutation
}

func NewSysLoginLogController() *SysLoginLogController {
	return &SysLoginLogController{Common: Common{}}
}

func newSysLoginLogControllerWithQuery(query loginLogQuery) *SysLoginLogController {
	return &SysLoginLogController{Common: Common{}, query: query}
}

func newSysLoginLogControllerWithDependencies(query loginLogQuery, mutation loginLogMutation) *SysLoginLogController {
	return &SysLoginLogController{Common: Common{}, query: query, mutation: mutation}
}

func (c *SysLoginLogController) queryService() loginLogQuery {
	if c.query != nil {
		return c.query
	}
	return service.NewLoginLogQueryService(app.DB())
}

func (c *SysLoginLogController) mutationService() loginLogMutation {
	if c.mutation != nil {
		return c.mutation
	}
	return service.NewLoginLogManagementService(app.DB(), app.Cache)
}

// List returns a filtered, stable page without the full user-agent value.
func (c *SysLoginLogController) List(ctx *gin.Context) {
	var req models.SysLoginLogListRequest
	if err := req.Validate(ctx); err != nil {
		c.FailAndAbort(ctx, "登录日志查询参数错误", err, http.StatusBadRequest)
	}
	startTime, err := parseLoginLogTime(req.StartTime)
	if err != nil {
		c.FailAndAbort(ctx, "登录日志开始时间格式错误", err, http.StatusBadRequest)
	}
	endTime, err := parseLoginLogTime(req.EndTime)
	if err != nil {
		c.FailAndAbort(ctx, "登录日志结束时间格式错误", err, http.StatusBadRequest)
	}
	list, total, err := c.queryService().List(ctx.Request.Context(), service.LoginLogFilter{
		PageNum: req.PageNum, PageSize: req.PageSize, Username: req.Username,
		Result: req.Result, FailureReason: req.FailureReason, IP: req.IP,
		StartTime: startTime, EndTime: endTime,
	})
	if err != nil {
		c.FailAndAbort(ctx, "查询登录日志失败", err, http.StatusInternalServerError)
	}
	c.Success(ctx, gin.H{"list": list, "total": total})
}

// Detail returns one allowlisted event, including its full user-agent value.
func (c *SysLoginLogController) Detail(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.FailAndAbort(ctx, "登录日志ID格式错误", err, http.StatusBadRequest)
	}
	detail, err := c.queryService().Detail(ctx.Request.Context(), uint(id))
	if err != nil {
		if errors.Is(err, service.ErrLoginLogNotFound) {
			c.FailAndAbort(ctx, "登录日志不存在", err, http.StatusNotFound)
		}
		c.FailAndAbort(ctx, "查询登录日志详情失败", err, http.StatusInternalServerError)
	}
	c.Success(ctx, detail)
}

func (c *SysLoginLogController) Delete(ctx *gin.Context) {
	var req models.SysLoginLogDeleteRequest
	if err := req.Validate(ctx); err != nil {
		c.FailAndAbort(ctx, "登录日志删除参数错误", err, http.StatusBadRequest)
	}
	middleware.MarkDeleteOperation(ctx)
	deletedCount, err := c.mutationService().Delete(ctx.Request.Context(), req.IDs)
	if err != nil {
		c.FailAndAbort(ctx, "删除登录日志失败", err, http.StatusInternalServerError)
	}
	c.Success(ctx, gin.H{"deletedCount": deletedCount})
}

func (c *SysLoginLogController) Clear(ctx *gin.Context) {
	middleware.MarkDeleteOperation(ctx)
	deletedCount, err := c.mutationService().Clear(ctx.Request.Context())
	if err != nil {
		c.FailAndAbort(ctx, "清空登录日志失败", err, http.StatusInternalServerError)
	}
	c.Success(ctx, gin.H{"deletedCount": deletedCount})
}

func (c *SysLoginLogController) Unlock(ctx *gin.Context) {
	var req models.SysLoginLogUnlockRequest
	if err := req.Validate(ctx); err != nil {
		c.FailAndAbort(ctx, "登录日志解锁参数错误", err, http.StatusBadRequest)
	}
	err := c.mutationService().Unlock(ctx.Request.Context(), req.ID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrLoginLogNotFound), errors.Is(err, service.ErrLoginLogUnlockUser):
			c.FailAndAbort(ctx, "登录日志关联用户不存在", err, http.StatusNotFound)
		case errors.Is(err, service.ErrLoginLogUnlockNotAllowed):
			c.FailAndAbort(ctx, "该登录日志不是账户锁定事件", err, http.StatusBadRequest)
		case errors.Is(err, service.ErrLoginLogCacheUnavailable):
			c.FailAndAbort(ctx, "解锁服务暂不可用", err, http.StatusServiceUnavailable)
		default:
			c.FailAndAbort(ctx, "解锁账号失败", err, http.StatusInternalServerError)
		}
	}
	c.SuccessWithMessage(ctx, "账号已解锁", nil)
}

func parseLoginLogTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err == nil {
			return &parsed, nil
		}
	}
	return nil, errors.New("unsupported time format")
}
