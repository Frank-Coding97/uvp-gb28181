package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	basecontrollers "uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/dashboard"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

const maxDashboardLayoutBody = 64 << 10

type HomeDashboardController struct {
	basecontrollers.Common
	db func() *gorm.DB
}

func NewHomeDashboardController(provider ...func() *gorm.DB) *HomeDashboardController {
	db := func() *gorm.DB { return app.DB() }
	if len(provider) > 0 && provider[0] != nil {
		db = provider[0]
	}
	return &HomeDashboardController{db: db}
}

func (controller *HomeDashboardController) GetLayout(c *gin.Context) {
	result, err := dashboard.NewLayoutService(controller.db()).Get(c.Request.Context(), controller.GetCurrentUserID(c))
	controller.writeLayoutResult(c, result, err)
}

func (controller *HomeDashboardController) SaveLayout(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDashboardLayoutBody)
	var request struct {
		Revision uint64           `json:"revision"`
		Layout   dashboard.Layout `json:"layout"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		controller.Fail(c, "仪表盘布局参数不合法", err, http.StatusBadRequest)
		return
	}
	result, err := dashboard.NewLayoutService(controller.db()).Save(c.Request.Context(), controller.GetCurrentUserID(c), request.Revision, request.Layout)
	controller.writeLayoutResult(c, result, err)
}

func (controller *HomeDashboardController) ResetLayout(c *gin.Context) {
	result, err := dashboard.NewLayoutService(controller.db()).Reset(c.Request.Context(), controller.GetCurrentUserID(c))
	controller.writeLayoutResult(c, result, err)
}

func (controller *HomeDashboardController) writeLayoutResult(c *gin.Context, result dashboard.StoredLayout, err error) {
	if err == nil {
		controller.Success(c, result)
		return
	}
	if errors.Is(err, dashboard.ErrLayoutRevisionConflict) {
		controller.Fail(c, "仪表盘布局已被其他页面更新，请刷新后重试", err, http.StatusConflict, 1, gin.H{"errorCode": "LAYOUT_REVISION_CONFLICT"})
		return
	}
	controller.Fail(c, "仪表盘布局操作失败", err, http.StatusBadRequest)
}
