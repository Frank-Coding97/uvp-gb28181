package controllers

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
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

	list, total, err := gbmodels.ListPaged(c, page, pageSize)
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
	d, err := gbmodels.FindByDeviceID(c, deviceID)
	if err != nil {
		dc.FailAndAbort(c, "查询设备失败", err)
		return
	}
	if d == nil {
		dc.FailAndAbort(c, "设备不存在", nil)
		return
	}
	dc.Success(c, toVO(d))
}
