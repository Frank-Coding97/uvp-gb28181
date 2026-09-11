package controllers

import (
	"context"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
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
	dc.catalogTrigger.Trigger(context.WithoutCancel(c.Request.Context()), d.DeviceID, dest, d.Transport)
	dc.Success(c, gin.H{"deviceId": d.DeviceID, "dest": dest, "transport": d.Transport, "ok": true})
}
