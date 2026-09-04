package controllers

import (
	"net/http"
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
