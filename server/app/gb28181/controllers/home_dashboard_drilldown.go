package controllers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/dashboard"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

func (controller *HomeDashboardController) SIPHistory(c *gin.Context) {
	if len(c.QueryArray("range")) > 1 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 1, "message": "range 只能传一次"})
		return
	}
	now := time.Now()
	window, err := dashboard.ResolveHistoryWindow(c.Query("range"), now, time.Local)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	db := controller.db()
	if db == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 1, "message": "仪表盘聚合服务未初始化"})
		return
	}
	history, err := dashboard.NewSIPMetricQuery(db, time.Local).History(c.Request.Context(), window)
	if err != nil {
		controller.Fail(c, "SIP 历史查询失败", err, http.StatusServiceUnavailable)
		return
	}
	controller.Success(c, dashboard.SectionEnvelope[dashboard.SIPHistory]{
		Status: history.Status, AsOf: now.Format(time.RFC3339Nano),
		Scope: dashboard.Scope{Type: dashboard.ScopePlatform}, Coverage: history.Coverage, Data: history,
	})
}

func (controller *HomeDashboardController) TrafficHistory(c *gin.Context) {
	if len(c.QueryArray("range")) > 1 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 1, "message": "range 只能传一次"})
		return
	}
	now := time.Now()
	window, err := dashboard.ResolveTrafficHistoryWindow(c.Query("range"), now, time.Local)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	page, err := positiveQueryInt(c, "page", 1)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	pageSize, err := positiveQueryInt(c, "pageSize", 20)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	db := controller.db()
	if db == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 1, "message": "仪表盘聚合服务未初始化"})
		return
	}
	trafficScope := datascope.VisibilityScope(c.Copy(), "traffic.owner_dept_id", "traffic.device_code")
	history, err := dashboard.NewAssetSummaryService(db, time.Local).TrafficHistory(c.Request.Context(), window, page, pageSize, trafficScope)
	if err != nil {
		controller.Fail(c, "媒体流量历史查询失败", err, http.StatusServiceUnavailable)
		return
	}
	controller.Success(c, dashboard.SectionEnvelope[dashboard.TrafficHistory]{
		Status: history.Status, AsOf: now.Format(time.RFC3339Nano),
		Scope: dashboard.Scope{Type: dashboard.ScopeUserVisible}, Coverage: history.Coverage, Data: history,
	})
}

func positiveQueryInt(c *gin.Context, name string, fallback int) (int, error) {
	raw := c.Query(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, &dashboardQueryError{name: name}
	}
	return value, nil
}

type dashboardQueryError struct{ name string }

func (err *dashboardQueryError) Error() string { return err.name + " 必须为正整数" }

func (controller *HomeDashboardController) PlayHistory(c *gin.Context) {
	if len(c.QueryArray("range")) > 1 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 1, "message": "range 只能传一次"})
		return
	}
	now := time.Now()
	window, err := dashboard.ResolveHistoryWindow(c.Query("range"), now, time.Local)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	db := controller.db()
	if db == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 1, "message": "仪表盘聚合服务未初始化"})
		return
	}
	playScope := datascope.VisibilityScope(c.Copy(), "gb_device.owner_dept_id", "gb_device.device_id")
	history, err := dashboard.NewPlayAttemptStore(db).HistoryScoped(c.Request.Context(), window, playScope)
	if err != nil {
		controller.Fail(c, "点播历史查询失败", err, http.StatusServiceUnavailable)
		return
	}
	controller.Success(c, dashboard.SectionEnvelope[dashboard.PlayHistory]{
		Status: history.Status, AsOf: now.Format(time.RFC3339Nano),
		Scope: dashboard.Scope{Type: dashboard.ScopeUserVisible}, Coverage: history.Coverage, Data: history,
	})
}
