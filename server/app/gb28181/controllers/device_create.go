package controllers

import (
	"strings"

	"github.com/gin-gonic/gin"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// CreateDevice 手动创建设备(预分配模式 / 一设备一密码)
// POST /device  body: {deviceId, name, password?, transport?}
func (dc *DeviceMgmtController) CreateDevice(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}

	var body struct {
		DeviceID  string `json:"deviceId" binding:"required"`
		Name      string `json:"name"`
		Password  string `json:"password"`
		Transport string `json:"transport"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		dc.FailAndAbort(c, "请求体不合法", err)
		return
	}

	// 校验 deviceId 格式:20位数字
	body.DeviceID = strings.TrimSpace(body.DeviceID)
	if len(body.DeviceID) != 20 {
		dc.FailAndAbort(c, "设备编号必须为20位", nil)
		return
	}
	for _, r := range body.DeviceID {
		if r < '0' || r > '9' {
			dc.FailAndAbort(c, "设备编号只能包含数字", nil)
			return
		}
	}

	// 校验 transport
	if body.Transport == "" {
		body.Transport = "UDP"
	}
	if body.Transport != "UDP" && body.Transport != "TCP" {
		dc.FailAndAbort(c, "传输模式只能是 UDP 或 TCP", nil)
		return
	}

	// 检查是否已存在
	var existing gbmodels.GbDevice
	res := db.WithContext(c).Where("device_id = ?", body.DeviceID).Limit(1).Find(&existing)
	if res.Error != nil {
		dc.FailAndAbort(c, "查询失败", res.Error)
		return
	}
	if res.RowsAffected > 0 {
		dc.FailAndAbort(c, "设备编号已存在", nil)
		return
	}

	// 获取用户信息
	var createdBy uint
	var ownerDeptID uint
	if userID, exists := c.Get("userId"); exists {
		if uid, ok := userID.(float64); ok {
			createdBy = uint(uid)
		} else if uid, ok := userID.(uint); ok {
			createdBy = uid
		}
	}
	if deptID, exists := c.Get("deptId"); exists {
		if did, ok := deptID.(float64); ok {
			ownerDeptID = uint(did)
		} else if did, ok := deptID.(uint); ok {
			ownerDeptID = did
		}
	}

	// 创建设备
	device := gbmodels.GbDevice{
		DeviceID:            body.DeviceID,
		Name:                strings.TrimSpace(body.Name),
		Password:            strings.TrimSpace(body.Password),
		Transport:           body.Transport,
		Status:              gbmodels.DeviceStatusOffline,
		KeepaliveInterval:   60,
		CreatedBy:           createdBy,
		OwnerDeptID:         ownerDeptID,
		SubscribeCapability: gbmodels.SubscribeUnknown,
	}

	if err := db.WithContext(c).Create(&device).Error; err != nil {
		dc.FailAndAbort(c, "创建失败", err)
		return
	}

	dc.Success(c, gin.H{"id": device.ID, "deviceId": device.DeviceID})
}
