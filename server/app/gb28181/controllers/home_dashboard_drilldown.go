package controllers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/dashboard"
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
