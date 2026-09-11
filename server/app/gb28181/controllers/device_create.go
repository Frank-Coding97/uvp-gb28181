package controllers

import (
	"strings"

	"github.com/gin-gonic/gin"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
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
	res := db.WithContext(c.Request.Context()).Where("device_id = ?", body.DeviceID).Limit(1).Find(&existing)
	if res.Error != nil {
		dc.FailAndAbort(c, "查询失败", res.Error)
		return
	}
	if res.RowsAffected > 0 {
		dc.FailAndAbort(c, "设备编号已存在", nil)
		return
	}

	// 获取用户信息(修复:原实现读不存在的 context 键,归属恒为 0)
	claims := common.GetClaims(c)
	if claims == nil || claims.UserID == 0 {
		dc.FailAndAbort(c, "未获取到用户身份", nil)
		return
	}
	createdBy := claims.UserID

	// 归属部门 = 创建人当前部门,必须存在且启用
	var user basemodels.User
	if err := db.WithContext(c.Request.Context()).Select("dept_id").Where("id = ?", claims.UserID).First(&user).Error; err != nil {
		dc.FailAndAbort(c, "查询创建人部门失败", err)
		return
	}
	var deptCount int64
	if err := db.WithContext(c.Request.Context()).Table("sys_department").
		Where("id = ? AND (status = 1 OR status IS NULL) AND deleted_at IS NULL", user.DeptID).Count(&deptCount).Error; err != nil {
		dc.FailAndAbort(c, "校验部门失败", err)
		return
	}
	if user.DeptID == 0 || deptCount == 0 {
		dc.FailAndAbort(c, "创建人部门无效或已停用", nil)
		return
	}
	ownerDeptID := user.DeptID

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

	if err := db.WithContext(c.Request.Context()).Create(&device).Error; err != nil {
		dc.FailAndAbort(c, "创建失败", err)
		return
	}

	dc.Success(c, gin.H{"id": device.ID, "deviceId": device.DeviceID})
}
