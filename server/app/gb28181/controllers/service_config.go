package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

const (
	positionHistoryConfigKey              = "gb28181.position_history.enabled"
	positionHistoryRetentionDaysConfigKey = "gb28181.position_history.retention_days"
	defaultPositionHistoryRetentionDays   = 7
	sdpExtensionConfigKey                 = gbconfig.SDPExtensionConfigKey
	ptzDefaultSpeedLevelConfigKey         = "gb28181.ptz.default_speed_level"
	defaultPTZSpeedLevel                  = 6
)

// PositionHistoryConfig 是国标服务配置页面的位置历史配置。
type PositionHistoryConfig struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retentionDays"`
}

// SDPExtensionConfig controls WVP-compatible extended media payloads in
// newly created Play and Playback SDP offers.
type SDPExtensionConfig struct {
	Enabled bool `json:"enabled"`
}

// PTZDefaultSpeedConfig 是云台控制界面初始使用的 1-10 档速度。
type PTZDefaultSpeedConfig struct {
	Level int `json:"level"`
}

// ServiceConfigController 提供国标服务配置页面使用的单项动态配置接口。
// 这里不复用 /api/config/update，避免页面提交时覆盖系统和安全配置。
type ServiceConfigController struct {
	controllers.Common
}

func NewServiceConfigController() *ServiceConfigController {
	return &ServiceConfigController{}
}

// GetSDPExtension GET /api/gb28181/sip/service-config/sdp-extension
func (sc *ServiceConfigController) GetSDPExtension(c *gin.Context) {
	sc.Success(c, SDPExtensionConfig{Enabled: gbconfig.SDPExtensionEnabled()})
}

// UpdateSDPExtension PUT /api/gb28181/sip/service-config/sdp-extension
func (sc *ServiceConfigController) UpdateSDPExtension(c *gin.Context) {
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		sc.Fail(c, "保存扩展 SDP 兼容模式失败：enabled 必须为布尔值", err, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := gbconfig.SDPExtensionEnabled()
	app.ConfigYml.Set(sdpExtensionConfigKey, *request.Enabled)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(sdpExtensionConfigKey, previous)
		sc.Fail(c, "保存扩展 SDP 兼容模式失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "扩展 SDP 兼容模式已更新", SDPExtensionConfig{Enabled: *request.Enabled})
}

// GetPTZDefaultSpeed GET /api/gb28181/sip/service-config/ptz-default-speed
func (sc *ServiceConfigController) GetPTZDefaultSpeed(c *gin.Context) {
	sc.Success(c, PTZDefaultSpeedConfig{Level: currentPTZDefaultSpeedLevel()})
}

// UpdatePTZDefaultSpeed PUT /api/gb28181/sip/service-config/ptz-default-speed
func (sc *ServiceConfigController) UpdatePTZDefaultSpeed(c *gin.Context) {
	var request struct {
		Level *int `json:"level"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Level == nil {
		sc.Fail(c, "保存云台默认速度失败：level 必须为 1-10 的整数", err, http.StatusBadRequest)
		return
	}
	if *request.Level < 1 || *request.Level > 10 {
		sc.Fail(c, "保存云台默认速度失败：速度档位需在 1-10 之间", nil, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previous := currentPTZDefaultSpeedLevel()
	app.ConfigYml.Set(ptzDefaultSpeedLevelConfigKey, *request.Level)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		app.ConfigYml.Set(ptzDefaultSpeedLevelConfigKey, previous)
		sc.Fail(c, "保存云台默认速度失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "云台默认速度已更新", PTZDefaultSpeedConfig{Level: *request.Level})
}

// GetPositionHistory GET /api/gb28181/sip/service-config/position-history
func (sc *ServiceConfigController) GetPositionHistory(c *gin.Context) {
	// 未显式配置时保持历史兼容行为：默认保存轨迹。
	enabled := true
	if app.ConfigYml != nil && app.ConfigYml.Get(positionHistoryConfigKey) != nil {
		enabled = app.ConfigYml.GetBool(positionHistoryConfigKey)
	}
	sc.Success(c, PositionHistoryConfig{Enabled: enabled, RetentionDays: currentPositionHistoryRetentionDays()})
}

// UpdatePositionHistory PUT /api/gb28181/sip/service-config/position-history
func (sc *ServiceConfigController) UpdatePositionHistory(c *gin.Context) {
	var request struct {
		Enabled       *bool `json:"enabled"`
		RetentionDays *int  `json:"retentionDays"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		sc.Fail(c, "保存移动位置历史轨迹配置失败：enabled 必须为布尔值", err, http.StatusBadRequest)
		return
	}
	if request.RetentionDays != nil && (*request.RetentionDays < 1 || *request.RetentionDays > 365) {
		sc.Fail(c, "保存移动位置历史轨迹配置失败：保留天数需在 1-365 天之间", nil, http.StatusBadRequest)
		return
	}
	if app.ConfigYml == nil {
		sc.Fail(c, "配置服务尚未初始化", nil, http.StatusServiceUnavailable)
		return
	}

	previousEnabled := true
	if app.ConfigYml.Get(positionHistoryConfigKey) != nil {
		previousEnabled = app.ConfigYml.GetBool(positionHistoryConfigKey)
	}
	previousRetentionDays := currentPositionHistoryRetentionDays()
	retentionDays := previousRetentionDays
	if request.RetentionDays != nil {
		retentionDays = *request.RetentionDays
	}
	app.ConfigYml.Set(positionHistoryConfigKey, *request.Enabled)
	app.ConfigYml.Set(positionHistoryRetentionDaysConfigKey, retentionDays)
	if err := app.ConfigYml.SaveConfig(); err != nil {
		// 保存失败时恢复内存值，避免运行时行为与配置文件不一致。
		app.ConfigYml.Set(positionHistoryConfigKey, previousEnabled)
		app.ConfigYml.Set(positionHistoryRetentionDaysConfigKey, previousRetentionDays)
		sc.Fail(c, "保存移动位置历史轨迹配置失败", err, http.StatusInternalServerError)
		return
	}

	sc.SuccessWithMessage(c, "移动位置历史轨迹配置已更新", PositionHistoryConfig{
		Enabled:       *request.Enabled,
		RetentionDays: retentionDays,
	})
}

func currentPositionHistoryRetentionDays() int {
	if app.ConfigYml != nil {
		if days := app.ConfigYml.GetInt(positionHistoryRetentionDaysConfigKey); days >= 1 && days <= 365 {
			return days
		}
	}
	return defaultPositionHistoryRetentionDays
}

func currentPTZDefaultSpeedLevel() int {
	if app.ConfigYml != nil {
		if level := app.ConfigYml.GetInt(ptzDefaultSpeedLevelConfigKey); level >= 1 && level <= 10 {
			return level
		}
	}
	return defaultPTZSpeedLevel
}
