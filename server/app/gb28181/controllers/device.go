package controllers

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbdevice "uvplatform.cn/uvp-gb28181/app/gb28181/device"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// DeviceController 国标设备管理(最小:列表 + 详情,含实时在线态)
type DeviceController struct {
	controllers.Common
}

func NewDeviceController() *DeviceController {
	return &DeviceController{Common: controllers.Common{}}
}

// deviceVO 设备列表项(DB 字段 + Redis 实时在线态)
type deviceVO struct {
	*gbmodels.GbDevice
	Online bool `json:"online"` // Redis 实时在线态(比 status 字段更实时)
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
		vos = append(vos, deviceVO{GbDevice: d, Online: gbdevice.IsOnline(c, d.DeviceID)})
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
	dc.Success(c, deviceVO{GbDevice: d, Online: gbdevice.IsOnline(c, deviceID)})
}
