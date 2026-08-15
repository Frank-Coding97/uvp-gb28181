package controllers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

// DeviceController 国标设备管理(最小:列表 + 详情,含实时在线态)
type DeviceController struct {
	controllers.Common
}

func NewDeviceController() *DeviceController {
	return &DeviceController{Common: controllers.Common{}}
}

// deviceVO 设备列表项(DB 字段 + 从事实派生的实时在线态)
type deviceVO struct {
	*gbmodels.GbDevice
	Online bool `json:"online"` // 从 keepalive_time 事实派生,比 status 缓存更实时
}

// toVO 构造 VO,在线态按设备 keepalive_interval + 全局容忍/宽限派生
func toVO(d *gbmodels.GbDevice) deviceVO {
	dev := gbconfig.Load().Device
	return deviceVO{GbDevice: d, Online: d.IsOnlineByFact(dev.KeepaliveTimeoutCount, dev.KeepaliveGraceSeconds)}
}

// List 设备列表(分页)
// GET /api/gb28181/device/list?page=1&pageSize=20
func (dc *DeviceController) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}

	list, total, err := gbmodels.ListPaged(c, page, pageSize, datascope.VisibilityScope(c, "owner_dept_id", "device_id"))
	if err != nil {
		dc.FailAndAbort(c, "获取设备列表失败", err)
		return
	}

	vos := make([]deviceVO, 0, len(list))
	for _, d := range list {
		vos = append(vos, toVO(d))
	}

	dc.Success(c, gin.H{
		"list":     vos,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// GetByDeviceID 设备详情
// GET /api/gb28181/device/:deviceId
func (dc *DeviceController) GetByDeviceID(c *gin.Context) {
	deviceID := c.Param("deviceId")
	var d gbmodels.GbDevice
	result := app.DB().WithContext(c).
		Scopes(datascope.VisibilityScope(c, "owner_dept_id", "device_id")).
		Where("device_id = ?", deviceID).
		Limit(1).
		Find(&d)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询设备失败", result.Error)
		return
	}
	if result.RowsAffected == 0 {
		dc.FailAndAbort(c, "设备不存在", nil)
		return
	}
	dc.Success(c, toVO(&d))
}

// Update 更新设备信息
// PATCH /api/gb28181/device/:deviceId
// 注意:alias 是用户自定义别名,不会被设备上报的 name 覆盖
func (dc *DeviceController) Update(c *gin.Context) {
	deviceID := c.Param("deviceId")
	var body struct {
		Alias            *string `json:"alias"`
		Manufacturer     *string `json:"manufacturer"`
		Model            *string `json:"model"`
		Firmware         *string `json:"firmware"`
		ProtocolOverride *string `json:"protocolOverride"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		dc.FailAndAbort(c, "请求体不合法", err)
		return
	}

	// 查询设备（带权限）
	var device gbmodels.GbDevice
	result := app.DB().WithContext(c).
		Scopes(datascope.VisibilityScope(c, "owner_dept_id", "device_id")).
		Where("device_id = ?", deviceID).
		Limit(1).
		Find(&device)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询设备失败", result.Error)
		return
	}
	if result.RowsAffected == 0 {
		dc.FailAndAbort(c, "设备不存在", nil)
		return
	}

	// 构造更新字段（只更新传入的字段）
	updates := make(map[string]interface{})
	if body.Alias != nil {
		updates["alias"] = *body.Alias
	}
	if body.Manufacturer != nil {
		updates["manufacturer"] = *body.Manufacturer
	}
	if body.Model != nil {
		updates["model"] = *body.Model
	}
	if body.Firmware != nil {
		updates["firmware"] = *body.Firmware
	}
	if body.ProtocolOverride != nil {
		override := strings.ToLower(strings.TrimSpace(*body.ProtocolOverride))
		if override != gbmodels.ProtocolOverrideAuto && override != gbmodels.ProtocolVersion2016 && override != gbmodels.ProtocolVersion2022 {
			app.Response.Fail(c, "协议版本覆盖只能是 auto、2016 或 2022", http.StatusUnprocessableEntity)
			return
		}
		history := ""
		if device.EffectiveVersionSource == gbmodels.ProtocolVersionSourceRegister || device.EffectiveVersionSource == gbmodels.ProtocolVersionSourceHistory {
			history = device.EffectiveVersion
		}
		resolution := protocol.Resolve(protocol.ResolveInput{
			Override: override,
			Register: device.ReportedVersion,
			History:  history,
		})
		now := time.Now()
		updates["protocol_override"] = override
		updates["effective_version"] = string(resolution.Profile.Version)
		updates["effective_version_source"] = string(resolution.Source)
		updates["effective_version_at"] = &now
	}

	if len(updates) == 0 {
		dc.FailAndAbort(c, "没有可更新的字段", nil)
		return
	}

	// 执行更新
	if err := app.DB().WithContext(c).Model(&device).Updates(updates).Error; err != nil {
		dc.FailAndAbort(c, "更新失败", err)
		return
	}

	dc.Success(c, gin.H{"deviceId": deviceID})
}

// ListChannels 列出某设备的通道(供前端拉通道树二级)
// GET /api/gb28181/device/:deviceId/channels
func (dc *DeviceController) ListChannels(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		dc.FailAndAbort(c, "deviceId 不能为空", nil)
		return
	}
	var device gbmodels.GbDevice
	deviceResult := app.DB().WithContext(c).
		Scopes(datascope.VisibilityScope(c, "owner_dept_id", "device_id")).
		Where("device_id = ?", deviceID).
		Limit(1).
		Find(&device)
	if deviceResult.Error != nil {
		dc.FailAndAbort(c, "查询设备失败", deviceResult.Error)
		return
	}
	if deviceResult.RowsAffected == 0 {
		dc.FailAndAbort(c, "设备不存在", nil)
		return
	}

	var list gbmodels.GbChannelList
	err := app.DB().WithContext(c).
		Scopes(datascope.VisibilityScope(c, "owner_dept_id", "device_id")).
		Where("device_id = ?", deviceID).
		Order("channel_id").
		Find(&list).Error
	if err != nil {
		dc.FailAndAbort(c, "查通道列表失败", err)
		return
	}
	dc.Success(c, gin.H{
		"list":  list,
		"total": len(list),
	})
}
