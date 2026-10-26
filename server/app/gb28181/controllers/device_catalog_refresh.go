package controllers

import (
	"context"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"uvplatform.com/uvp-gb28181/app/gb28181/catalogprogress"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// RefreshDeviceCatalog 手动触发一次 Catalog 查询,拉取通道树
// POST /device-mgmt/device/:id/catalog/refresh
//
// 复用注册成功后自动触发的同一路径(handler.CatalogTrigger),
// 异步发 SIP MESSAGE(Catalog Query XML),响应结果由设备的 catalog notify 走 MessageHandler 落库。
// 前端接到 200 表示"命令已下发",不等设备回执。
func (dc *DeviceMgmtController) RefreshDeviceCatalog(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	if dc.catalogTrigger == nil {
		dc.FailAndAbort(c, "SIP UAC 未就绪,无法下发 Catalog 查询", nil)
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		dc.FailAndAbort(c, "ID 不合法", err)
		return
	}

	var d gbmodels.GbDevice
	res := db.WithContext(c.Request.Context()).Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&d)
	if res.Error != nil {
		dc.FailAndAbort(c, "查询失败", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		dc.FailAndAbort(c, "设备不存在或无权限", nil)
		return
	}
	if d.Status != gbmodels.DeviceStatusOnline {
		dc.FailAndAbort(c, "设备离线,无法下发 Catalog 查询", nil)
		return
	}
	if d.IP == "" || d.Port <= 0 {
		dc.FailAndAbort(c, "设备来源地址缺失,无法下发", nil)
		return
	}

	dest := fmt.Sprintf("%s:%d", d.IP, d.Port)
	started := catalogprogress.Default.Start(d.DeviceID)
	// ⛔ 手动刷新是唯一允许"按快照差集剪枝"的入口：对端撤掉/改编号的通道只有
	//    完整快照能自愈（自动 Catalog 应答不标记，行为不变）。
	//    去重命中也照样标记：用户点的是刷新，命中的是哪次任务不影响"要一次权威快照"。
	catalogprogress.Default.RequestPrune(d.DeviceID)
	if !started.Deduplicated {
		triggerContext := catalogprogress.WithOperationID(context.WithoutCancel(c.Request.Context()), started.Snapshot.OperationID)
		dc.catalogTrigger.Trigger(triggerContext, d.DeviceID, dest, d.Transport)
	}
	dc.Success(c, gin.H{"deviceId": d.DeviceID, "dest": dest, "transport": d.Transport, "operationId": started.Snapshot.OperationID, "deduplicated": started.Deduplicated, "ok": true})
}

// GetDeviceCatalogRefreshProgress returns the asynchronous Catalog refresh state.
// GET /device-mgmt/device/:id/catalog/refresh/:operationId
func (dc *DeviceMgmtController) GetDeviceCatalogRefreshProgress(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		dc.FailAndAbort(c, "ID 不合法", err)
		return
	}
	var device gbmodels.GbDevice
	res := db.WithContext(c.Request.Context()).Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&device)
	if res.Error != nil {
		dc.FailAndAbort(c, "查询失败", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		dc.FailAndAbort(c, "设备不存在或无权限", nil)
		return
	}
	operationID := c.Param("operationId")
	progress, ok := catalogprogress.Default.Get(operationID)
	if !ok {
		dc.FailAndAbort(c, "刷新任务不存在或已过期", nil)
		return
	}
	if progress.DeviceID != device.DeviceID {
		dc.FailAndAbort(c, "刷新任务不存在或无权限", nil)
		return
	}
	dc.Success(c, progress)
}
