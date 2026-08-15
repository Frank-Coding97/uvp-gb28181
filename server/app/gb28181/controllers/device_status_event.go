package controllers

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type deviceStatusEventVO struct {
	ID                uint64                           `json:"id"`
	DeviceID          uint                             `json:"deviceId"`
	DeviceCode        string                           `json:"deviceCode"`
	EventType         gbmodels.DeviceStatusEventType   `json:"eventType"`
	EventName         string                           `json:"eventName"`
	FromStatus        *int8                            `json:"fromStatus"`
	ToStatus          int8                             `json:"toStatus"`
	OccurredAt        time.Time                        `json:"occurredAt"`
	Source            gbmodels.DeviceStatusEventSource `json:"source"`
	RegisterExpires   *int                             `json:"registerExpires"`
	KeepaliveInterval *int                             `json:"keepaliveInterval"`
	IP                string                           `json:"ip"`
	Port              int                              `json:"port"`
	Transport         string                           `json:"transport"`
}

// ListDeviceStatusEvents returns the device lifecycle timeline.
// GET /device-mgmt/device/:id/status-events?page=1&pageSize=50
func (dc *DeviceMgmtController) ListDeviceStatusEvents(c *gin.Context) {
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
	deviceResult := db.WithContext(c).Scopes(visibleScope(c)).Where("id = ?", id).Limit(1).Find(&device)
	if deviceResult.Error != nil {
		dc.FailAndAbort(c, "查询设备失败", deviceResult.Error)
		return
	}
	if deviceResult.RowsAffected == 0 {
		dc.FailAndAbort(c, "设备不存在", nil)
		return
	}

	page := parsePositiveInt(c.DefaultQuery("page", "1"), 1)
	pageSize := parsePositiveInt(c.DefaultQuery("pageSize", "50"), 50)
	if pageSize > 200 {
		pageSize = 200
	}

	q := db.WithContext(c).
		Table("gb_device_status_event AS e").
		Select("e.*").
		Joins("JOIN gb_device AS d ON d.id = e.device_id AND d.deleted_at IS NULL").
		Where("e.device_id = ? AND e.deleted_at IS NULL", device.ID).
		Scopes(datascope.VisibilityScope(c, "d.owner_dept_id", "d.device_id"))

	if eventType := strings.TrimSpace(c.Query("eventType")); eventType != "" {
		parsed := gbmodels.DeviceStatusEventType(eventType)
		if !parsed.Valid() {
			dc.FailAndAbort(c, "eventType 不合法", nil)
			return
		}
		q = q.Where("e.event_type = ?", parsed)
	}
	from, err := parseOptionalTime(c.Query("from"))
	if err != nil {
		dc.FailAndAbort(c, "from 时间格式不合法", err)
		return
	}
	to, err := parseOptionalTime(c.Query("to"))
	if err != nil {
		dc.FailAndAbort(c, "to 时间格式不合法", err)
		return
	}
	if from != nil && to != nil && from.After(*to) {
		dc.FailAndAbort(c, "from 不能晚于 to", nil)
		return
	}
	if from != nil {
		q = q.Where("e.occurred_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("e.occurred_at <= ?", *to)
	}

	var total int64
	if err := q.Select("e.id").Count(&total).Error; err != nil {
		dc.FailAndAbort(c, "统计状态事件失败", err)
		return
	}
	var events []gbmodels.GbDeviceStatusEvent
	if err := q.Select("e.*").Order("e.occurred_at DESC, e.id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&events).Error; err != nil {
		dc.FailAndAbort(c, "查询状态事件失败", err)
		return
	}

	list := make([]deviceStatusEventVO, 0, len(events))
	for _, event := range events {
		list = append(list, deviceStatusEventVO{
			ID: event.ID, DeviceID: event.DeviceID, DeviceCode: event.DeviceCode,
			EventType: event.EventType, EventName: event.EventType.DisplayName(),
			FromStatus: event.FromStatus, ToStatus: event.ToStatus,
			OccurredAt: event.OccurredAt, Source: event.Source,
			RegisterExpires: event.RegisterExpires, KeepaliveInterval: event.KeepaliveInterval,
			IP: event.IP, Port: event.Port, Transport: event.Transport,
		})
	}
	dc.Success(c, gin.H{"list": list, "total": total, "page": page, "pageSize": pageSize})
}

func parsePositiveInt(value string, fallback int) int {
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func parseOptionalTime(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
