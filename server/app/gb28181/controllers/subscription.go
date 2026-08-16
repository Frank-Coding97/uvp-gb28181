package controllers

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type SubscriptionManager interface {
	Enable(context.Context, *gbmodels.GbDevice, gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error)
	Disable(context.Context, *gbmodels.GbDevice, gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error)
	Configure(context.Context, *gbmodels.GbDevice, gbmodels.SubscriptionKind, *bool, *int, *int) (*gbmodels.GbDeviceSubscription, error)
	Renew(context.Context, *gbmodels.GbDevice, gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error)
}

const (
	minSubscriptionExpiresSeconds = 60
	maxSubscriptionExpiresSeconds = 7 * 24 * 60 * 60
	minPositionIntervalSeconds    = 1
	maxPositionIntervalSeconds    = 24 * 60 * 60
)

type subscriptionVO struct {
	Kind            gbmodels.SubscriptionKind   `json:"kind"`
	Enabled         bool                        `json:"enabled"`
	Status          gbmodels.SubscriptionStatus `json:"status"`
	ExpiresSeconds  int                         `json:"expiresSeconds"`
	IntervalSeconds int                         `json:"intervalSeconds"`
	ExpiresAt       *time.Time                  `json:"expiresAt"`
	LastNotifyAt    *time.Time                  `json:"lastNotifyAt"`
	LastError       string                      `json:"lastError"`
}

func subscriptionDefaults(kind gbmodels.SubscriptionKind) subscriptionVO {
	vo := subscriptionVO{Kind: kind, Status: gbmodels.SubscriptionStatusDisabled, ExpiresSeconds: 3600}
	if kind == gbmodels.SubscriptionKindMobilePosition {
		vo.IntervalSeconds = 30
	}
	return vo
}

func (dc *DeviceMgmtController) findSubscriptionDevice(c *gin.Context) (*gbmodels.GbDevice, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 || dc.db() == nil {
		dc.FailAndAbort(c, "设备不存在", nil)
		return nil, false
	}
	var device gbmodels.GbDevice
	result := dc.db().WithContext(c).Scopes(visibleScope(c)).Where("id = ?", id).Limit(1).Find(&device)
	if result.Error != nil || result.RowsAffected == 0 {
		dc.FailAndAbort(c, "设备不存在", result.Error)
		return nil, false
	}
	return &device, true
}

// ListSubscriptions always returns every supported kind in a stable order.
func (dc *DeviceMgmtController) ListSubscriptions(c *gin.Context) {
	device, ok := dc.findSubscriptionDevice(c)
	if !ok {
		return
	}
	var rows []gbmodels.GbDeviceSubscription
	if err := dc.db().WithContext(c).Where("device_id = ?", device.ID).Find(&rows).Error; err != nil {
		dc.FailAndAbort(c, "查询订阅失败", err)
		return
	}
	byKind := make(map[gbmodels.SubscriptionKind]gbmodels.GbDeviceSubscription, len(rows))
	for _, row := range rows {
		byKind[row.Kind] = row
	}
	kinds := []gbmodels.SubscriptionKind{
		gbmodels.SubscriptionKindCatalog,
		gbmodels.SubscriptionKindMobilePosition,
		gbmodels.SubscriptionKindAlarm,
		gbmodels.SubscriptionKindPTZPrecisePosition,
	}
	list := make([]subscriptionVO, 0, len(kinds))
	for _, kind := range kinds {
		vo := subscriptionDefaults(kind)
		if row, exists := byKind[kind]; exists {
			vo.Enabled, vo.Status = row.Enabled, row.Status
			vo.ExpiresSeconds, vo.IntervalSeconds = row.ExpiresSeconds, row.IntervalSeconds
			vo.ExpiresAt, vo.LastNotifyAt, vo.LastError = row.ExpiresAt, row.LastNotifyAt, row.LastError
		}
		list = append(list, vo)
	}
	dc.Success(c, gin.H{"list": list})
}

func (dc *DeviceMgmtController) ListAlarms(c *gin.Context) {
	device, ok := dc.findSubscriptionDevice(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	query := dc.db().WithContext(c).Model(&gbmodels.GbAlarmEvent{}).Where("device_id = ?", device.ID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		dc.FailAndAbort(c, "查询报警失败", err)
		return
	}
	var list []gbmodels.GbAlarmEvent
	if err := query.Order("alarm_time DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		dc.FailAndAbort(c, "查询报警失败", err)
		return
	}
	dc.Success(c, gin.H{"list": list, "total": total, "page": page, "pageSize": pageSize})
}

func parseSubscriptionKind(c *gin.Context) (gbmodels.SubscriptionKind, bool) {
	kind := gbmodels.SubscriptionKind(c.Param("kind"))
	if !kind.Valid() {
		c.JSON(400, gin.H{"code": 400, "message": "订阅类型不合法"})
		return "", false
	}
	return kind, true
}

func (dc *DeviceMgmtController) UpdateSubscription(c *gin.Context) {
	device, ok := dc.findSubscriptionDevice(c)
	if !ok {
		return
	}
	kind, ok := parseSubscriptionKind(c)
	if !ok {
		return
	}
	var body struct {
		Enabled         *bool `json:"enabled"`
		ExpiresSeconds  *int  `json:"expiresSeconds"`
		IntervalSeconds *int  `json:"intervalSeconds"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || (body.Enabled == nil && body.ExpiresSeconds == nil && body.IntervalSeconds == nil) {
		dc.FailAndAbort(c, "请求体不合法", err)
		return
	}
	if body.ExpiresSeconds != nil && (*body.ExpiresSeconds < minSubscriptionExpiresSeconds || *body.ExpiresSeconds > maxSubscriptionExpiresSeconds) {
		dc.FailAndAbort(c, "订阅有效期需在 60-604800 秒之间", nil)
		return
	}
	if body.IntervalSeconds != nil {
		if kind != gbmodels.SubscriptionKindMobilePosition {
			dc.FailAndAbort(c, "仅位置订阅支持上报间隔", nil)
			return
		}
		if *body.IntervalSeconds < minPositionIntervalSeconds || *body.IntervalSeconds > maxPositionIntervalSeconds {
			dc.FailAndAbort(c, "位置上报间隔需在 1-86400 秒之间", nil)
			return
		}
	}
	if dc.subscriptionManager == nil {
		c.JSON(503, gin.H{"code": 503, "message": "订阅服务未就绪"})
		return
	}
	sub, err := dc.subscriptionManager.Configure(c, device, kind, body.Enabled, body.ExpiresSeconds, body.IntervalSeconds)
	if err != nil || sub == nil {
		if err == nil {
			err = fmt.Errorf("订阅状态未返回")
		}
		dc.FailAndAbort(c, "更新订阅失败", err)
		return
	}
	dc.Success(c, subscriptionVO{Kind: sub.Kind, Enabled: sub.Enabled, Status: sub.Status, ExpiresSeconds: sub.ExpiresSeconds, IntervalSeconds: sub.IntervalSeconds, ExpiresAt: sub.ExpiresAt, LastNotifyAt: sub.LastNotifyAt, LastError: sub.LastError})
}

func (dc *DeviceMgmtController) RenewSubscription(c *gin.Context) {
	device, ok := dc.findSubscriptionDevice(c)
	if !ok {
		return
	}
	kind, ok := parseSubscriptionKind(c)
	if !ok {
		return
	}
	if dc.subscriptionManager == nil {
		c.JSON(503, gin.H{"code": 503, "message": "订阅服务未就绪"})
		return
	}
	sub, err := dc.subscriptionManager.Renew(c, device, kind)
	if err != nil || sub == nil {
		dc.FailAndAbort(c, "续订失败", err)
		return
	}
	dc.Success(c, subscriptionVO{Kind: sub.Kind, Enabled: sub.Enabled, Status: sub.Status, ExpiresSeconds: sub.ExpiresSeconds, IntervalSeconds: sub.IntervalSeconds, ExpiresAt: sub.ExpiresAt, LastNotifyAt: sub.LastNotifyAt, LastError: sub.LastError})
}
